// Package modbus 实现 Modbus TCP / RTU 驱动（变频器 / PLC 的通用从站口）。
//
// 地址语法（与工控习惯对齐，前缀区分功能区）：
//   hr:40001  保持寄存器（读写，float32/int32 占两个连续寄存器）
//   ir:30001  输入寄存器（只读）
//   co:1      线圈（读写，bool）
//   di:10001  离散输入（只读，bool）
// 0x 前缀十六进制也认（hr:0x10B4）；寄存器编号按协议惯例 1 起算，内部转 0 基偏移。
//
// 连接：grid-x/modbus 客户端 + 互斥锁（Modbus 单连接请求/应答，不能并发）。
// handler.Connect(ctx) 保持会话（TCP/RTU 传输器都实现 Connector）；任何请求报错都
// 把连接置失效，下一次调用自动重连（grid-x 的 TCP/串口传输器自身也会断线报错）。
package modbus

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"math"
	"strconv"
	"strings"
	"sync"
	"time"

	gmb "github.com/grid-x/modbus"

	"nexadmin/plugins/devicehub/driver"
)

func init() {
	driver.Register("modbus-tcp", newTCP)
	// Modbus RTU（RS485 直连串口）：变频器最常见的口就是它，与 TCP 共享同一套地址语法。
	// endpoint 形如 COM3:9600,8,E,1（串口名:波特率,数据位,校验,停止位）。
	driver.Register("modbus-rtu", newRTU)
}

// Config 是 device.config JSON 的 modbus 形态。
type Config struct {
	UnitID         byte `json:"unitId"`         // 从站地址
	PollIntervalMs int  `json:"pollIntervalMs"` // 默认轮询间隔（毫秒），0 = 5000
	TimeoutMs      int  `json:"timeoutMs"`      // 单次读写超时，0 = 2000
}

// parseConfigRaw 解出协议参数；坏 JSON 按零值走默认，不让一格配置错配瘫掉采集。
// 默认值（unitId=1、5s 轮询、2s 超时）由 parseConfig 补齐。
func parseConfigRaw(raw []byte) (cfg Config) {
	if len(raw) > 0 {
		_ = json.Unmarshal(raw, &cfg)
	}
	return cfg
}

func parseConfig(raw []byte) Config {
	cfg := parseConfigRaw(raw)
	if cfg.UnitID == 0 {
		cfg.UnitID = 1
	}
	if cfg.PollIntervalMs <= 0 {
		cfg.PollIntervalMs = 5000
	}
	if cfg.TimeoutMs <= 0 {
		cfg.TimeoutMs = 2000
	}
	return cfg
}

type session struct {
	mu      sync.Mutex
	handler gmb.ClientHandler
	client  gmb.Client
	cfg     Config
}

func newTCP(endpoint string, raw []byte) (driver.Driver, error) {
	cfg := parseConfig(raw)
	handler := gmb.NewTCPClientHandler(endpoint)
	handler.Timeout = time.Duration(cfg.TimeoutMs) * time.Millisecond
	handler.SlaveID = cfg.UnitID
	return &session{handler: handler, client: gmb.NewClient(handler), cfg: cfg}, nil
}

func newRTU(endpoint string, raw []byte) (driver.Driver, error) {
	cfg := parseConfig(raw)
	// endpoint 形如 COM3:9600,8,E,1（串口名:波特率,数据位,校验,停止位）
	name, params, _ := strings.Cut(endpoint, ":")
	baud, dataBits, parity, stopBits := 9600, 8, 'N', 1
	if params != "" {
		parts := strings.Split(params, ",")
		if len(parts) > 0 {
			if v, err := strconv.Atoi(strings.TrimSpace(parts[0])); err == nil {
				baud = v
			}
		}
		if len(parts) > 1 {
			if v, err := strconv.Atoi(strings.TrimSpace(parts[1])); err == nil {
				dataBits = v
			}
		}
		if len(parts) > 2 && len(strings.TrimSpace(parts[2])) == 1 {
			parity = rune(strings.ToUpper(strings.TrimSpace(parts[2]))[0])
		}
		if len(parts) > 3 {
			if v, err := strconv.Atoi(strings.TrimSpace(parts[3])); err == nil {
				stopBits = v
			}
		}
	}
	handler := gmb.NewRTUClientHandler(name)
	handler.BaudRate = baud
	handler.DataBits = dataBits
	handler.Parity = string(parity)
	handler.StopBits = stopBits
	handler.Timeout = time.Duration(cfg.TimeoutMs) * time.Millisecond
	handler.SlaveID = cfg.UnitID
	return &session{handler: handler, client: gmb.NewClient(handler), cfg: cfg}, nil
}

// connect 保证 handler 已建立会话（Connector 是幂等的：重复调用安全）。
func (s *session) connect(ctx context.Context) error {
	if connector, ok := s.handler.(interface{ Connect(context.Context) error }); ok {
		return connector.Connect(ctx)
	}
	return nil
}

// invalidate 请求报错后把会话作废：下次调用 Connect 重来（串口/TCP 断线的重连路径）。
func (s *session) invalidate() {
	_ = s.handler.Close()
}

func (s *session) Connect(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.connect(ctx)
}

func (s *session) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.handler.Close()
}

// Ping 探活：读一次保持寄存器比 TCP 拨号更真（协议层通了才算通）。
func (s *session) Ping() error {
	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(s.cfg.TimeoutMs)*time.Millisecond)
	defer cancel()
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.connect(ctx); err != nil {
		return err
	}
	if _, err := s.client.ReadHoldingRegisters(ctx, 0, 1); err != nil {
		s.invalidate()
		return err
	}
	return nil
}

// addr 解析 "hr:40001" → (功能前缀, 0 基偏移)。寄存器编号 1 起算（协议惯例），内部转 0 基。
func addr(address string) (string, uint16, error) {
	prefix, num, ok := strings.Cut(strings.TrimSpace(address), ":")
	if !ok {
		return "", 0, fmt.Errorf("modbus: 地址要写成 hr:40001 / ir:30001 / co:1 / di:1 形态，收到 %q", address)
	}
	prefix = strings.ToLower(prefix)
	var base int64
	if strings.HasPrefix(num, "0x") || strings.HasPrefix(num, "0X") {
		v, err := strconv.ParseUint(num[2:], 16, 32)
		if err != nil {
			return "", 0, fmt.Errorf("modbus: 十六进制地址非法 %q", num)
		}
		base = int64(v)
	} else {
		v, err := strconv.ParseInt(num, 10, 32)
		if err != nil {
			return "", 0, fmt.Errorf("modbus: 地址非法 %q", num)
		}
		base = v
	}
	switch prefix {
	case "hr", "ir", "co", "di":
		if base < 1 {
			return "", 0, fmt.Errorf("modbus: %s 地址从 1 开始（协议惯例），收到 %q", prefix, num)
		}
		// hr/ir/co/di 的 1 起算编号都对应 0 基偏移 = 编号-1。
		return prefix, uint16(base - 1), nil
	default:
		return "", 0, fmt.Errorf("modbus: 不认识的功能前缀 %q（hr/ir/co/di）", prefix)
	}
}

// decode 把寄存器字节解码成值；scale 由上层做（驱动只还原原始值）。
func decode(dt string, raw []byte) (driver.Value, error) {
	switch dt {
	case "bool":
		if len(raw) < 1 {
			return driver.Value{}, fmt.Errorf("modbus: bool 需要 1 字节")
		}
		if raw[0] != 0 {
			return driver.Value{Num: 1}, nil
		}
		return driver.Value{Num: 0}, nil
	case "int16":
		if len(raw) < 2 {
			return driver.Value{}, fmt.Errorf("modbus: int16 需要 2 字节")
		}
		return driver.Value{Num: float64(int16(binary.BigEndian.Uint16(raw)))}, nil
	case "uint16":
		if len(raw) < 2 {
			return driver.Value{}, fmt.Errorf("modbus: uint16 需要 2 字节")
		}
		return driver.Value{Num: float64(binary.BigEndian.Uint16(raw))}, nil
	case "int32":
		if len(raw) < 4 {
			return driver.Value{}, fmt.Errorf("modbus: int32 需要 4 字节")
		}
		return driver.Value{Num: float64(int32(binary.BigEndian.Uint32(raw)))}, nil
	case "float32", "":
		if len(raw) < 4 {
			return driver.Value{}, fmt.Errorf("modbus: float32 需要 4 字节")
		}
		return driver.Value{Num: float64(math.Float32frombits(binary.BigEndian.Uint32(raw)))}, nil
	default:
		return driver.Value{}, fmt.Errorf("modbus: 不支持的数据类型 %q（bool/int16/uint16/int32/float32）", dt)
	}
}

// regsOf 按 dataType 算要读的寄存器数量。
func regsOf(dt string) uint16 {
	switch dt {
	case "bool", "int16", "uint16":
		return 1
	default: // int32 / float32
		return 2
	}
}

func (s *session) Read(ctx context.Context, address, dataType string) (driver.Value, error) {
	prefix, off, err := addr(address)
	if err != nil {
		return driver.Value{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.connect(ctx); err != nil {
		return driver.Value{}, err
	}
	// bool 型走位区（co/di），数值型走寄存器区；前缀决定区，dt 决定长度。
	var raw []byte
	switch prefix {
	case "co":
		raw, err = s.client.ReadCoils(ctx, off, 1)
	case "di":
		raw, err = s.client.ReadDiscreteInputs(ctx, off, 1)
	case "ir":
		raw, err = s.client.ReadInputRegisters(ctx, off, regsOf(dataType))
	default:
		raw, err = s.client.ReadHoldingRegisters(ctx, off, regsOf(dataType))
	}
	if err != nil {
		s.invalidate()
		return driver.Value{}, err
	}
	return decode(dataType, raw)
}

func (s *session) Write(ctx context.Context, address, dataType, raw string) error {
	prefix, off, err := addr(address)
	if err != nil {
		return err
	}
	if prefix == "di" || prefix == "ir" {
		return fmt.Errorf("modbus: %s 是只读区，不能下发指令", prefix)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.connect(ctx); err != nil {
		return err
	}
	switch {
	case prefix == "co":
		v, perr := strconv.ParseFloat(raw, 64)
		if perr != nil || (v != 0 && v != 1) {
			return fmt.Errorf("modbus: 线圈只接受 0/1，收到 %q", raw)
		}
		_, err = s.client.WriteSingleCoil(ctx, off, uint16(v))
	case dataType == "int16" || dataType == "uint16":
		v, perr := strconv.ParseInt(raw, 10, 64)
		if perr != nil {
			return fmt.Errorf("modbus: %s 需要 16 位整数，收到 %q", dataType, raw)
		}
		if dataType == "int16" && (v < math.MinInt16 || v > math.MaxInt16) {
			return fmt.Errorf("modbus: int16 超出范围 %d", v)
		}
		if dataType == "uint16" && (v < 0 || v > math.MaxUint16) {
			return fmt.Errorf("modbus: uint16 超出范围 %d", v)
		}
		_, err = s.client.WriteSingleRegister(ctx, off, uint16(v))
	case dataType == "int32":
		v, perr := strconv.ParseInt(raw, 10, 64)
		if perr != nil || v < math.MinInt32 || v > math.MaxInt32 {
			return fmt.Errorf("modbus: int32 需要 32 位整数，收到 %q", raw)
		}
		buf := make([]byte, 4)
		binary.BigEndian.PutUint32(buf, uint32(int32(v)))
		_, err = s.client.WriteMultipleRegisters(ctx, off, 2, buf)
	default: // float32
		v, perr := strconv.ParseFloat(raw, 64)
		if perr != nil {
			return fmt.Errorf("modbus: float32 需要数字，收到 %q", raw)
		}
		buf := make([]byte, 4)
		binary.BigEndian.PutUint32(buf, math.Float32bits(float32(v)))
		_, err = s.client.WriteMultipleRegisters(ctx, off, 2, buf)
	}
	if err != nil {
		s.invalidate()
	}
	return err
}
