// Package s7 实现西门子 S7 协议驱动（S7-200 Smart / 300 / 400 / 1200 / 1500）。
//
// 地址语法（西门子 STEP 7 惯例，前缀/区域字面照抄）：
//
//	DB1.DBX0.0  数据块位（bool）
//	DB1.DBW2    数据块字（int16/uint16）
//	DB1.DBD4    数据块双字（int32/float32，dataType 决定解码）
//	M0.0 / MB10 / MW10 / MD20   Merker（位/字节/字/双字）
//	I0.0 / IB64 / IW66 / ID68   过程输入（PE）
//	Q0.0 / QB0 / QW2 / QD4      过程输出（PA）
//	T1 / C1      定时器 / 计数器（16 位）
//
// 读写实现：全部落在字节级 AG*（AGReadDB/MB/EB/AB/TM/CT）——bool 是「读 1 字节本地取位」、
// 「读-改-写回」，word/real 本地编解码。这样字节级原语覆盖全部区域，不用碰 gos7 的位语义。
// Rack/Slot 在 device.config：1200/1500 通常 rack=0 slot=0，300/400 常见 rack=0 slot=1 或 2。
package s7

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

	gos7 "github.com/robinson/gos7"

	"nexadmin/plugins/devicehub/driver"
)

func init() {
	driver.Register("s7", New)
}

// Config 是 device.config JSON 的 s7 形态。
type Config struct {
	Rack           int `json:"rack"`           // 机架号，0 = 常见
	Slot           int `json:"slot"`           // 槽号：1200/1500 填 0，300/400 填 1 或 2
	PollIntervalMs int `json:"pollIntervalMs"` // 0 = 5000
	TimeoutMs      int `json:"timeoutMs"`      // 0 = 2000
}

type session struct {
	mu      sync.Mutex
	handler *gos7.TCPClientHandler
	client  gos7.Client
	cfg     Config
}

// New 按endpoint构造 S7 驱动；endpoint 形如 192.168.1.30:102（102 是 S7 标准端口）。
func New(endpoint string, raw []byte) (driver.Driver, error) {
	cfg := Config{Rack: 0, Slot: 1, PollIntervalMs: 5000, TimeoutMs: 2000}
	if len(raw) > 0 {
		_ = json.Unmarshal(raw, &cfg)
	}
	if cfg.PollIntervalMs <= 0 {
		cfg.PollIntervalMs = 5000
	}
	if cfg.TimeoutMs <= 0 {
		cfg.TimeoutMs = 2000
	}
	handler := gos7.NewTCPClientHandler(strings.TrimSpace(endpoint), cfg.Rack, cfg.Slot)
	handler.Timeout = time.Duration(cfg.TimeoutMs) * time.Millisecond
	return &session{handler: handler, client: gos7.NewClient(handler), cfg: cfg}, nil
}

func (s *session) Connect(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	// gos7 传输器的 ConnectContext 建立ISO-on-TCP 会话（原生支持 ctx 取消）。
	return s.handler.ConnectContext(ctx)
}

func (s *session) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.handler.Close()
}

func (s *session) Ping() error {
	buf := make([]byte, 1)
	s.mu.Lock()
	defer s.mu.Unlock()
	// Merker 0 字节是伤害最小的探针（读 EB/DB 需要具体设备约定，M 区恒存在）
	return s.client.AGReadMB(0, 1, buf)
}

// area 是一个解析后的 S7 地址。
type area struct {
	kind byte // d=/* m i q t c（内部标记，见 parse）
	db   int  // DB 号（kind=d）
	byte int  // 字节偏移
	bit  uint // 位号（bool 用）
	size int  // 字节数（bool=1 int16/uint16=2 int32/float32=4）
	wl   int  // gos7 word length（位语义在上层处理，这里全是 byte 级读写）
}

// parse 解析西门子地址。dataType 决定 DBD/MD/ID/QD 的解码宽度（缺省 float32）。
func parse(address, dataType string) (area, error) {
	a := area{}
	addr := strings.TrimSpace(address)
	// 空地址以前直接 up[0] 越界 panic：点位配错时不是报一条可读的错误，而是把采集协程打挂。
	if addr == "" {
		return a, fmt.Errorf("s7: 地址为空")
	}
	up := strings.ToUpper(addr)
	switch {
	case strings.HasPrefix(up, "DB") && strings.Contains(up, "."):
		// DB<n>.DBX<b>.<bit> | DB<n>.DBW<b> | DB<n>.DBD<b>
		dbPart, rest, _ := strings.Cut(up, ".")
		db, err := strconv.Atoi(strings.TrimPrefix(dbPart, "DB"))
		if err != nil || db < 1 {
			return a, fmt.Errorf("s7: DB 号非法 %q", address)
		}
		a.db = db
		a.kind = 'd'
		switch {
		case strings.HasPrefix(rest, "DBX"):
			body := strings.TrimPrefix(rest, "DBX")
			byteStr, bitStr, _ := strings.Cut(body, ".")
			b, err1 := strconv.Atoi(byteStr)
			bit, err2 := strconv.Atoi(bitStr)
			if err1 != nil || err2 != nil || bit > 7 {
				return a, fmt.Errorf("s7: DBX 位地址非法 %q（DB1.DBX0.0~7）", address)
			}
			a.byte, a.bit, a.size = b, uint(bit), 1
			a.wl = 1
			return a, nil
		case strings.HasPrefix(rest, "DBW"):
			b, err := strconv.Atoi(strings.TrimPrefix(rest, "DBW"))
			if err != nil {
				return a, fmt.Errorf("s7: DBW 偏移非法 %q", address)
			}
			a.byte, a.size, a.wl = b, 2, 1
			return a, nil
		case strings.HasPrefix(rest, "DBD"):
			b, err := strconv.Atoi(strings.TrimPrefix(rest, "DBD"))
			if err != nil {
				return a, fmt.Errorf("s7: DBD 偏移非法 %q", address)
			}
			a.byte, a.size, a.wl = b, 4, 1
			return a, nil
		default:
			return a, fmt.Errorf("s7: DB 区只支持 DBX/DBW/DBD，收到 %q", address)
		}
	}
	// DB 开头却没有点号（"DB1"）不能落到下面那条通用分支：那里会把 B 当成字节宽度，
	// 解析成「DB0 偏移 1」——读回来的是另一个区域的真数据，比报错更坏。
	if strings.HasPrefix(up, "DB") {
		return a, fmt.Errorf("s7: DB 地址要写成 DB<n>.DBX<b>.<bit> / DB<n>.DBW<b> / DB<n>.DBD<b>，收到 %q", address)
	}
	// M / I / Q / T / C：首字母 + 类型字母（B/W/D）或位号。
	// kind 必须归一成小写：readBytes / writeBytes 与「I 区只读」的守卫都按小写匹配，
	// 留着大写（"M0.0" → 'M'）会让非 DB 区每次都在运行时报「未知区域」，I 区的只读保护也形同不存在。
	kind := byte(strings.ToLower(string(up[0]))[0])
	switch kind {
	case 'm', 'i', 'q', 't', 'c':
	default:
		return a, fmt.Errorf("s7: 无法识别的区域 %q（支持 DB / M / I / Q / T / C）", address)
	}
	body := up[1:]
	size := 1
	switch {
	case strings.HasPrefix(body, "W"):
		body, size = body[1:], 2
	case strings.HasPrefix(body, "D"):
		body, size = body[1:], 4
	case strings.HasPrefix(body, "B"):
		body, size = body[1:], 1
	default:
		// M0.0 位形态
		if b, bit, ok := strings.Cut(body, "."); ok {
			byteN, err1 := strconv.Atoi(b)
			bitN, err2 := strconv.Atoi(bit)
			if err1 != nil || err2 != nil || bitN > 7 {
				return a, fmt.Errorf("s7: 位地址非法 %q", address)
			}
			a.byte, a.bit, a.size = byteN, uint(bitN), 1
			a.kind = kind
			return a, nil
		}
	}
	byteN, err := strconv.Atoi(strings.TrimSpace(body))
	if err != nil || byteN < 0 {
		return a, fmt.Errorf("s7: 偏移非法 %q", address)
	}
	a.byte, a.size, a.kind = byteN, size, kind
	switch kind {
	case 't', 'c':
		// 定时器/计数器固定 16 位（kind 已归一小写，这里比较也必须用小写，否则这条永远不生效）
		a.size = 2
	}
	return a, nil
}

// readBytes 按 area 读 size 字节（AG* 字节级原语）。
func (s *session) readBytes(ctx context.Context, a area) ([]byte, error) {
	buf := make([]byte, a.size)
	var err error
	done := make(chan struct{})
	go func() {
		defer close(done)
		switch a.kind {
		case 'd':
			err = s.client.AGReadDB(a.db, a.byte, a.size, buf)
		case 'm':
			err = s.client.AGReadMB(a.byte, a.size, buf)
		case 'i':
			err = s.client.AGReadEB(a.byte, a.size, buf)
		case 'q':
			err = s.client.AGReadAB(a.byte, a.size, buf)
		case 't':
			err = s.client.AGReadTM(a.byte, 1, buf)
		case 'c':
			err = s.client.AGReadCT(a.byte, 1, buf)
		default:
			err = fmt.Errorf("s7: 未知区域 %q", string(a.kind))
		}
	}()
	select {
	case <-done:
		return buf, err
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// writeBytes 按 area 写 size 字节。
func (s *session) writeBytes(ctx context.Context, a area, buf []byte) error {
	done := make(chan error, 1)
	go func() {
		switch a.kind {
		case 'd':
			done <- s.client.AGWriteDB(a.db, a.byte, a.size, buf)
		case 'm':
			done <- s.client.AGWriteMB(a.byte, a.size, buf)
		case 'q':
			done <- s.client.AGWriteAB(a.byte, a.size, buf)
		default:
			done <- fmt.Errorf("s7: 区域 %q 不可写（输入区 I 与定时/计数不支持写入）", string(a.kind))
		}
	}()
	select {
	case err := <-done:
		return err
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (s *session) Read(ctx context.Context, address, dataType string) (driver.Value, error) {
	a, err := parse(address, dataType)
	if err != nil {
		return driver.Value{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	raw, err := s.readBytes(ctx, a)
	if err != nil {
		return driver.Value{}, err
	}
	if a.bit != 0 || dataType == "bool" {
		bit := a.bit
		if dataType == "bool" && a.bit == 0 && strings.Contains(strings.ToUpper(address), ".") && !strings.Contains(strings.ToUpper(address), "DB") {
			// M0.0 形态已在 parse 里填 bit；这里防御
			_ = bit
		}
		if getBit(raw[0], a.bit) {
			return driver.Value{Num: 1}, nil
		}
		return driver.Value{Num: 0}, nil
	}
	switch dataType {
	case "int16":
		return driver.Value{Num: float64(int16(binary.BigEndian.Uint16(raw)))}, nil
	case "uint16":
		return driver.Value{Num: float64(binary.BigEndian.Uint16(raw))}, nil
	case "int32":
		return driver.Value{Num: float64(int32(binary.BigEndian.Uint32(raw)))}, nil
	default: // float32
		return driver.Value{Num: float64(math.Float32frombits(binary.BigEndian.Uint32(raw)))}, nil
	}
}

func getBit(b byte, pos uint) bool { return b&(1<<pos) != 0 }

func (s *session) Write(ctx context.Context, address, dataType, raw string) error {
	a, err := parse(address, dataType)
	if err != nil {
		return err
	}
	if a.kind == 'i' {
		return fmt.Errorf("s7: 输入区 I 是只读的，不能下发指令")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if a.bit != 0 || dataType == "bool" {
		v, perr := strconv.ParseFloat(raw, 64)
		if perr != nil || (v != 0 && v != 1) {
			return fmt.Errorf("s7: 位只接受 0/1，收到 %q", raw)
		}
		// 读-改-写：只翻转目标位，不动同字节其他位
		cur, err := s.readBytes(ctx, a)
		if err != nil {
			return err
		}
		if v == 1 {
			cur[0] |= 1 << a.bit
		} else {
			cur[0] &^= 1 << a.bit
		}
		return s.writeBytes(ctx, a, cur)
	}
	switch dataType {
	case "int16", "uint16":
		v, perr := strconv.ParseInt(raw, 10, 64)
		if perr != nil {
			return fmt.Errorf("s7: %s 需要 16 位整数，收到 %q", dataType, raw)
		}
		if dataType == "int16" && (v < math.MinInt16 || v > math.MaxInt16) {
			return fmt.Errorf("s7: int16 超出范围 %d", v)
		}
		if dataType == "uint16" && (v < 0 || v > math.MaxUint16) {
			return fmt.Errorf("s7: uint16 超出范围 %d", v)
		}
		buf := make([]byte, 2)
		binary.BigEndian.PutUint16(buf, uint16(v))
		return s.writeBytes(ctx, a, buf)
	case "int32":
		v, perr := strconv.ParseInt(raw, 10, 64)
		if perr != nil || v < math.MinInt32 || v > math.MaxInt32 {
			return fmt.Errorf("s7: int32 需要 32 位整数，收到 %q", raw)
		}
		buf := make([]byte, 4)
		binary.BigEndian.PutUint32(buf, uint32(int32(v)))
		return s.writeBytes(ctx, a, buf)
	default: // float32
		v, perr := strconv.ParseFloat(raw, 64)
		if perr != nil {
			return fmt.Errorf("s7: float32 需要数字，收到 %q", raw)
		}
		buf := make([]byte, 4)
		binary.BigEndian.PutUint32(buf, math.Float32bits(float32(v)))
		return s.writeBytes(ctx, a, buf)
	}
}
