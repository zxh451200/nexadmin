package mqtt

import (
	"context"
	"testing"
	"time"

	paho "github.com/eclipse/paho.mqtt.golang"

	"nexadmin/plugins/devicehub/driver"
)

// fakeMessage 只实现 paho.Message 里 onMessage 真用到的方法，其余是接口要求的形式签名。
type fakeMessage struct {
	topic   string
	payload []byte
	acked   bool
}

func (m *fakeMessage) Duplicate() bool   { return false }
func (m *fakeMessage) Qos() byte         { return 1 }
func (m *fakeMessage) Retained() bool    { return false }
func (m *fakeMessage) Topic() string     { return m.topic }
func (m *fakeMessage) MessageID() uint16 { return 0 }
func (m *fakeMessage) Payload() []byte   { return m.payload }
func (m *fakeMessage) Ack()              { m.acked = true }

func TestDecodePayload(t *testing.T) {
	cases := []struct {
		name    string
		raw     string
		wantNum float64
		wantStr string
	}{
		{"信封数字", `{"v":123.5}`, 123.5, ""},
		{"信封布尔 true", `{"v":true}`, 1, ""},
		{"信封布尔 false", `{"v":false}`, 0, ""},
		{"信封数字字符串", `{"v":"1.5"}`, 1.5, ""},
		{"信封文本", `{"v":"RUN"}`, 0, "RUN"},
		{"信封整数", `{"v":7}`, 7, ""},
		{"信封 null", `{"v":null}`, 0, ""},
		{"没有 v 字段", `{"value":1}`, 0, "{\"value\":1}"},
		{"裸数字", "42.5", 42.5, ""},
		{"裸数字带空白", " 42 ", 42, ""},
		{"裸文本", "OK", 0, "OK"},
		{"空 payload", "", 0, ""},
	}
	for _, tc := range cases {
		got := decodePayload([]byte(tc.raw))
		if got.Num != tc.wantNum || got.Str != tc.wantStr {
			t.Errorf("%s: decodePayload(%q) = %+v, want Num=%v Str=%q", tc.name, tc.raw, got, tc.wantNum, tc.wantStr)
		}
	}
}

// 数组/对象/null 没有单点数值语义，必须回零值而不是把整段 JSON 当读数——
// 存进读数的假字符串会让「设备没数据」和「设备报了个怪东西」在现场看起来一样。
func TestDecodePayloadUnknownEnvelopeFallsBack(t *testing.T) {
	for _, raw := range []string{`{"v":[1,2]}`, `{"v":{"a":1}}`, `{"v":null}`} {
		if got := decodePayload([]byte(raw)); got != (driver.Value{}) {
			t.Errorf("%s 应当收敛成零值，实际 %+v", raw, got)
		}
	}
}

func TestOnMessageRoutesByPrefix(t *testing.T) {
	cases := []struct {
		name      string
		topic     string
		wantAddrs []string
	}{
		{"前缀内的相对地址", "devicehub/up/motor/frequency", []string{"motor/frequency"}},
		{"前缀外", "other/topic", nil},
		{"平台自己的下行回显", "devicehub/up/down/motor/frequency", nil},
		{"只有前缀没有地址", "devicehub/up", nil},
	}
	for _, tc := range cases {
		c := &client{prefix: "devicehub/up"}
		var got []string
		c.onFrame = func(address string, _ driver.Value) { got = append(got, address) }
		c.onMessage(nil, &fakeMessage{topic: tc.topic, payload: []byte(`{"v":1}`)})
		if len(got) != len(tc.wantAddrs) || (len(got) > 0 && got[0] != tc.wantAddrs[0]) {
			t.Errorf("%s: 投递 %v, want %v", tc.name, got, tc.wantAddrs)
		}
	}
}

// 收到帧要刷新静默时间：否则 Ping 会把明明在上报的设备判成离线。
func TestOnMessageRefreshesLastMsgAt(t *testing.T) {
	c := &client{prefix: "devicehub/up", cfg: Config{OfflineAfterSeconds: 60}}
	c.connected = true
	c.client = &stubClient{open: true}
	if err := c.Ping(); err != nil {
		t.Fatalf("从未收帧的连接不应因静默判离线：%v", err)
	}
	c.onMessage(nil, &fakeMessage{topic: "devicehub/up/a/b", payload: []byte("1")})
	if c.lastMsgAt.IsZero() {
		t.Fatal("收帧后 lastMsgAt 仍为零值")
	}
	c.lastMsgAt = time.Now().Add(-2 * time.Minute)
	if err := c.Ping(); err == nil {
		t.Error("静默超过 offlineAfterSeconds 应判定离线")
	}
}

func TestNewClientAppliesDefaults(t *testing.T) {
	d, err := newClient("  tcp://127.0.0.1:1883  ", nil)
	if err != nil {
		t.Fatal(err)
	}
	c := d.(*client)
	if c.prefix != "devicehub/up" {
		t.Errorf("缺省前缀不对：%q", c.prefix)
	}
	if c.endpoint != "tcp://127.0.0.1:1883" {
		t.Errorf("endpoint 没去掉空格：%q", c.endpoint)
	}
	if c.cfg.OfflineAfterSeconds != 60 {
		t.Errorf("缺省静默判定不是 60s：%d", c.cfg.OfflineAfterSeconds)
	}
	if got := c.brokerURL(); got != "tcp://127.0.0.1:1883" {
		t.Errorf("已有 scheme 还被再加一层：%q", got)
	}

	d2, _ := newClient("127.0.0.1:1883", []byte(`{"upstreamPrefix":"plant/row1/","offlineAfterSeconds":0}`))
	c2 := d2.(*client)
	if c2.prefix != "plant/row1" {
		t.Errorf("前缀末尾斜杠没去掉：%q", c2.prefix)
	}
	if c2.cfg.OfflineAfterSeconds != 60 {
		t.Errorf("显式 0 没有回到默认值：%d", c2.cfg.OfflineAfterSeconds)
	}
	if got := c2.brokerURL(); got != "tcp://127.0.0.1:1883" {
		t.Errorf("省略 scheme 时没有补 tcp://：%q", got)
	}
}

// MQTT 是推送型：轮询读取与未订阅时的 Connect 都必须明确失败，不能让采集层以为读到了零值。
func TestReadAndConnectAreExplicitFailures(t *testing.T) {
	d, _ := newClient("127.0.0.1:1883", nil)
	c := d.(*client)
	if _, err := c.Read(context.Background(), "motor/frequency", ""); err == nil {
		t.Error("Read 应当明确拒绝轮询")
	}
	if err := c.Connect(context.Background()); err == nil {
		t.Error("未 Subscribe 的 Connect 应当报错")
	}
	if err := c.Write(context.Background(), "motor/frequency", "", "1"); err == nil {
		t.Error("broker 未连接时 Write 应当报错，而不是丢指令")
	}
	if err := c.Close(); err != nil {
		t.Errorf("未建连的 Close 应当幂等：%v", err)
	}
}

// stubClient 只覆盖 Ping/Close 走到的方法，其余按 paho.Client 接口签名补齐（永不被调用）。
type stubClient struct{ open bool }

func (s *stubClient) IsConnected() bool                                  { return s.open }
func (s *stubClient) IsConnectionOpen() bool                             { return s.open }
func (s *stubClient) Connect() paho.Token                                { return nil }
func (s *stubClient) Disconnect(_ uint)                                  {}
func (s *stubClient) Publish(_ string, _ byte, _ bool, _ any) paho.Token { return nil }
func (s *stubClient) Subscribe(_ string, _ byte, _ paho.MessageHandler) paho.Token {
	return nil
}
func (s *stubClient) SubscribeMultiple(_ map[string]byte, _ paho.MessageHandler) paho.Token {
	return nil
}
func (s *stubClient) Unsubscribe(_ ...string) paho.Token       { return nil }
func (s *stubClient) AddRoute(_ string, _ paho.MessageHandler) {}
func (s *stubClient) OptionsReader() paho.ClientOptionsReader  { return paho.ClientOptionsReader{} }
