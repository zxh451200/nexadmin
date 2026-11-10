// Package mqtt 实现订阅推送型驱动：设备/边缘网关把测点值发到 MQTT broker，
// devicehub 作为客户端订阅上行主题；指令下发 publish 到下行主题。
//
// 主题约定（配置页写清楚，双方照着配）：
//
//	上行（设备 → 平台）：{upstreamPrefix}/{pointAddress}，payload = {"v":123.5} 或裸数字/字符串
//	下行（平台 → 设备）：{upstreamPrefix}/down/{pointAddress}，payload = {"v":<raw>}
//
// point.address 是相对地址（如 "motor/frequency"），prefix 在 device.config 里配（默认 devicehub/up/{deviceNo}）。
// 离线判定：broker 连接断开（Ping 失败），或上行静默超过 offlineAfterSeconds（默认 60s）。
package mqtt

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	paho "github.com/eclipse/paho.mqtt.golang"

	"nexadmin/plugins/devicehub/driver"
)

func init() {
	driver.Register("mqtt", newClient)
}

// Config 是 device.config JSON 的 mqtt 形态。
type Config struct {
	Username            string `json:"username"`
	Password            string `json:"password"`
	UpstreamPrefix      string `json:"upstreamPrefix"`      // 默认 devicehub/up
	OfflineAfterSeconds int    `json:"offlineAfterSeconds"` // 上行静默判定秒数，0 = 60
}

type client struct {
	mu       sync.Mutex
	cfg      Config
	endpoint string
	prefix   string

	client    paho.Client
	onFrame   func(address string, v driver.Value)
	lastMsgAt time.Time
	connected bool
}

func newClient(endpoint string, raw []byte) (driver.Driver, error) {
	cfg := Config{UpstreamPrefix: "devicehub/up", OfflineAfterSeconds: 60}
	if len(raw) > 0 {
		_ = json.Unmarshal(raw, &cfg)
	}
	if cfg.OfflineAfterSeconds <= 0 {
		cfg.OfflineAfterSeconds = 60
	}
	return &client{
		cfg:      cfg,
		endpoint: strings.TrimSpace(endpoint),
		prefix:   strings.TrimSuffix(strings.TrimSpace(cfg.UpstreamPrefix), "/"),
		// lastMsgAt 从建连时刻起算，而不是留零值：Ping 的静默判定要先看 lastMsgAt 非零
		// （零值代表「从未收到帧」，直接跳过判断）。留零值的话，broker 连上但设备一帧不发
		// 会被永远当成 online；从 create 时刻起算，offlineAfterSeconds（默认 60s）内
		// 无上行即判离线。
		lastMsgAt: time.Now(),
	}, nil
}

// UpstreamPrefix 返回主题前缀（collector 拼测点完整主题用，也供 service 校验地址合法）。
func (c *client) UpstreamPrefix() string { return c.prefix }

// Subscribe 建连并订阅整个前缀（单订阅通配符，一次连接收全部测点）。
// 实现了 driver.PushDriver；collector 对 mqtt 设备只调这个，不轮询。
func (c *client) Subscribe(ctx context.Context, fn func(address string, v driver.Value)) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.onFrame = fn
	if c.client != nil && c.client.IsConnectionOpen() {
		return nil
	}
	opts := paho.NewClientOptions().
		AddBroker(c.brokerURL()).
		SetCleanSession(false).
		SetAutoReconnect(true).
		SetConnectRetry(true).
		SetConnectRetryInterval(5 * time.Second).
		SetKeepAlive(30 * time.Second)
	if c.cfg.Username != "" {
		opts = opts.SetUsername(c.cfg.Username).SetPassword(c.cfg.Password)
	}
	opts = opts.SetOnConnectHandler(func(cl paho.Client) {
		c.mu.Lock()
		defer c.mu.Unlock()
		c.connected = true
		// 与 newClient 同理：断线重连会先清空 lastMsgAt（见 ConnectionLostHandler），
		// 重连后留零值就又会跳过静默判定。以重连时刻为新起点，重连后依然一帧不发的设备
		// 一个静默窗口后照常判离线。
		c.lastMsgAt = time.Now()
		// 断线重连后重订（CleanSession=false 时 broker 会补发 QoS1，这里兜底重订一次）
		_ = cl.Subscribe(c.prefix+"/#", 1, c.onMessage)
	}).SetConnectionLostHandler(func(_ paho.Client, err error) {
		c.mu.Lock()
		defer c.mu.Unlock()
		c.connected = false
		c.lastMsgAt = time.Time{}
		_ = err // 断线由 Ping 上报，这里没有 logger 可用
	})
	c.client = paho.NewClient(opts)
	if token := c.client.Connect(); token.WaitTimeout(5*time.Second) && token.Error() != nil {
		return fmt.Errorf("mqtt: 连接 broker 失败: %w", token.Error())
	}
	return nil
}

func (c *client) brokerURL() string {
	// endpoint 允许省略 scheme；tcp:// 是 paho 的默认端口形态
	if strings.Contains(c.endpointURL(), "://") {
		return c.endpointURL()
	}
	return "tcp://" + c.endpointURL()
}

func (c *client) endpointURL() string { return c.endpoint }

func (c *client) onMessage(_ paho.Client, msg paho.Message) {
	// 完整主题 → 去掉前缀得到相对地址（测点注册时的 address）
	topic := msg.Topic()
	rel, ok := strings.CutPrefix(topic, c.prefix+"/")
	if !ok || rel == "" || strings.HasPrefix(rel, "down/") {
		return // 前缀外或平台自己下发的回显，忽略
	}
	v := decodePayload(msg.Payload())
	c.mu.Lock()
	c.lastMsgAt = time.Now()
	fn := c.onFrame
	c.mu.Unlock()
	if fn != nil {
		fn(rel, v)
	}
}

// decodePayload 兼容三种形态：{"v":1.5} JSON、裸数字、裸字符串。
//
// {"v":...} 里的值按类型收敛：数字走 Num；布尔走 1/0（以前 json.Number 解不了 true，
// 会把整段 JSON 当字符串存进读数）；字符串先按数字试一次再落 Str（设备端把数值点位
// 发成 "1.5" 是常态）。null 与解不动的形态回零值，不猜。
func decodePayload(raw []byte) driver.Value {
	var envelope struct {
		V json.RawMessage `json:"v"`
	}
	if err := json.Unmarshal(raw, &envelope); err == nil && len(envelope.V) > 0 {
		var num json.Number
		if err := json.Unmarshal(envelope.V, &num); err == nil {
			if f, err := num.Float64(); err == nil {
				return driver.Value{Num: f}
			}
			return driver.Value{Str: num.String()}
		}
		var b bool
		if err := json.Unmarshal(envelope.V, &b); err == nil {
			if b {
				return driver.Value{Num: 1}
			}
			return driver.Value{Num: 0}
		}
		var s string
		if err := json.Unmarshal(envelope.V, &s); err == nil {
			s = strings.TrimSpace(s)
			if f, err := strconv.ParseFloat(s, 64); err == nil {
				return driver.Value{Num: f}
			}
			return driver.Value{Str: s}
		}
		return driver.Value{}
	}
	s := strings.TrimSpace(string(raw))
	if s == "" {
		return driver.Value{}
	}
	if n, err := strconv.ParseFloat(s, 64); err == nil {
		return driver.Value{Num: n}
	}
	return driver.Value{Str: s}
}

func (c *client) Ping() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.connected || c.client == nil || !c.client.IsConnectionOpen() {
		return fmt.Errorf("mqtt: broker 未连接")
	}
	if !c.lastMsgAt.IsZero() && time.Since(c.lastMsgAt) > time.Duration(c.cfg.OfflineAfterSeconds)*time.Second {
		return fmt.Errorf("mqtt: 上行静默超过 %ds，设备判定离线", c.cfg.OfflineAfterSeconds)
	}
	return nil
}

// Read 对 MQTT 设备不可用（上行是推送的）：返回错误由采集层跳过——
// mqtt 设备的「最近值」直接来自 onFrame 缓存，不走 Read 轮询。
func (c *client) Read(_ context.Context, _, _ string) (driver.Value, error) {
	return driver.Value{}, fmt.Errorf("mqtt: 上行是推送型，不支持轮询读取（最近值见测点缓存）")
}

// Write 下行：publish {"v":<raw>} 到 {prefix}/down/{address}；设备/网关订阅该主题执行。
// 平台侧 publish 成功即回执 done（QoS1 保证送达 broker）——设备执行结果由设备方再发上行帧体现。
func (c *client) Write(_ context.Context, address, _, raw string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.client == nil || !c.client.IsConnectionOpen() {
		return fmt.Errorf("mqtt: broker 未连接，指令无法下发")
	}
	payload, _ := json.Marshal(map[string]any{"v": raw})
	token := c.client.Publish(c.prefix+"/down/"+strings.TrimPrefix(address, "/"), 1, false, payload)
	if !token.WaitTimeout(5 * time.Second) {
		return fmt.Errorf("mqtt: 下行 publish 超时")
	}
	return token.Error()
}

func (c *client) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.client == nil {
		return nil
	}
	c.client.Disconnect(500)
	c.client = nil
	c.connected = false
	return nil
}

// Connect 实现接口要求（非推送路径不用它）；推送路径的建连在 Subscribe 里。
func (c *client) Connect(_ context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.connected && c.client != nil && c.client.IsConnectionOpen() {
		return nil
	}
	return fmt.Errorf("mqtt: 请先 Subscribe 建立订阅连接")
}
