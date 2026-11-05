// Package opcua 实现 OPC UA 驱动（中高端 PLC / 上位机 / SCADA 的统一口）。
//
// 地址 = OPC UA NodeID 字符串，直接透传 gopcua：
//
//	ns=2;s=Channel1.Device1.Tag1   字符串 NodeID（KEPServerEX 等网关的命名惯例）
//	ns=3;i=1001                    数字 NodeID
//
// 安全：P0 用 None 安全策略（内网/工控网段的常见形态），device.config 可配用户名密码
// 与安全策略（None/Sign/SignAndEncrypt）。读走 Read 服务，写下发走 Write 服务
// （值类型用 ua.NewVariant 自动推断：数字→浮点，true/false→布尔，其余→字符串）。
// 采集形态是轮询（collector 通用路径）；OPC UA 原生订阅留到需要毫秒级推送时再加。
package opcua

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/gopcua/opcua"
	"github.com/gopcua/opcua/ua"

	"nexadmin/plugins/devicehub/driver"
)

func init() {
	driver.Register("opcua", New)
}

// Config 是 device.config JSON 的 opcua 形态。
type Config struct {
	SecurityPolicy string `json:"securityPolicy"` // None（默认）| Sign | SignAndEncrypt
	Username       string `json:"username"`
	Password       string `json:"password"`
	PollIntervalMs int    `json:"pollIntervalMs"` // 0 = 5000
	TimeoutMs      int    `json:"timeoutMs"`      // 0 = 5000
}

type session struct {
	mu       sync.Mutex
	cfg      Config
	endpoint string
	client   *opcua.Client
}

// New 构造 OPC UA 驱动；endpoint 形如 opc.tcp://192.168.1.40:4840。
func New(endpoint string, raw []byte) (driver.Driver, error) {
	cfg := Config{SecurityPolicy: "None", PollIntervalMs: 5000, TimeoutMs: 5000}
	if len(raw) > 0 {
		_ = json.Unmarshal(raw, &cfg)
	}
	if cfg.PollIntervalMs <= 0 {
		cfg.PollIntervalMs = 5000
	}
	if cfg.TimeoutMs <= 0 {
		cfg.TimeoutMs = 5000
	}
	opts := []opcua.Option{opcua.SecurityMode(securityMode(cfg.SecurityPolicy))}
	if cfg.Username != "" {
		opts = append(opts, opcua.AuthUsername(cfg.Username, cfg.Password))
	}
	client, err := opcua.NewClient(strings.TrimSpace(endpoint), opts...)
	if err != nil {
		return nil, fmt.Errorf("opcua: 构造客户端失败: %w", err)
	}
	return &session{cfg: cfg, endpoint: strings.TrimSpace(endpoint), client: client}, nil
}

func securityMode(s string) ua.MessageSecurityMode {
	switch strings.ToUpper(strings.TrimSpace(s)) {
	case "SIGN":
		return ua.MessageSecurityModeSign
	case "SIGNANDENCRYPT":
		return ua.MessageSecurityModeSignAndEncrypt
	default:
		return ua.MessageSecurityModeNone
	}
}

func (s *session) Connect(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.client.Connect(ctx)
}

func (s *session) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.client.Close(context.Background())
}

// Ping 探活：读 Server_ServerStatus_CurrentTime（所有 OPC UA 服务器必有，NodeID i=2258）。
func (s *session) Ping() error {
	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(s.cfg.TimeoutMs)*time.Millisecond)
	defer cancel()
	s.mu.Lock()
	defer s.mu.Unlock()
	node := s.client.Node(ua.MustParseNodeID("i=2258"))
	_, err := node.Value(ctx)
	return err
}

// Read 读一个节点：值经 variant 统一转 float / string。
func (s *session) Read(ctx context.Context, address, _ string) (driver.Value, error) {
	id, err := ua.ParseNodeID(strings.TrimSpace(address))
	if err != nil {
		return driver.Value{}, fmt.Errorf("opcua: NodeID 非法 %q（ns=2;s=Tag1 或 ns=3;i=1001）", address)
	}
	s.mu.Lock()
	node := s.client.Node(id)
	s.mu.Unlock()
	v, err := node.Value(ctx)
	if err != nil {
		return driver.Value{}, err
	}
	return variantToValue(v), nil
}

// Write 下发：raw 经 ua.NewVariant 自动推断类型（服务器对类型不匹配会回 BadTypeMismatch）。
func (s *session) Write(ctx context.Context, address, _, raw string) error {
	id, err := ua.ParseNodeID(strings.TrimSpace(address))
	if err != nil {
		return fmt.Errorf("opcua: NodeID 非法 %q", address)
	}
	v, err := ua.NewVariant(raw)
	if err != nil {
		return fmt.Errorf("opcua: 值非法 %q", raw)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	req := &ua.WriteRequest{
		NodesToWrite: []*ua.WriteValue{{
			NodeID:      id,
			AttributeID: ua.AttributeIDValue,
			Value:       &ua.DataValue{EncodingMask: ua.DataValueValue, Value: v},
		}},
	}
	resp, err := s.client.Write(ctx, req)
	if err != nil {
		return err
	}
	if len(resp.Results) > 0 && resp.Results[0] != ua.StatusOK {
		return fmt.Errorf("opcua: 写入被拒绝: %v", resp.Results[0])
	}
	return nil
}

// variantToValue 把 OPC UA variant 收敛成驱动的数值/字符串二形态。
func variantToValue(v *ua.Variant) driver.Value {
	if v == nil {
		return driver.Value{}
	}
	switch x := v.Value().(type) {
	case bool:
		if x {
			return driver.Value{Num: 1}
		}
		return driver.Value{Num: 0}
	case float32:
		return driver.Value{Num: float64(x)}
	case float64:
		return driver.Value{Num: x}
	case int8:
		return driver.Value{Num: float64(x)}
	case int16:
		return driver.Value{Num: float64(x)}
	case int32:
		return driver.Value{Num: float64(x)}
	case int64:
		return driver.Value{Num: float64(x)}
	case uint8:
		return driver.Value{Num: float64(x)}
	case uint16:
		return driver.Value{Num: float64(x)}
	case uint32:
		return driver.Value{Num: float64(x)}
	case uint64:
		return driver.Value{Num: float64(x)}
	case string:
		return driver.Value{Str: x}
	default:
		// 数组/结构体等复杂类型转字符串表示，保证不丢
		return driver.Value{Str: fmt.Sprintf("%v", v.Value())}
	}
}
