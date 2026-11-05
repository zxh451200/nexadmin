package opcua

import (
	"testing"

	"github.com/gopcua/opcua/ua"
)

// variantToValue 是 OPC UA 读数进本框架的唯一收敛点：数值一律走 Num、文本走 Str，
// 复杂类型不能丢（转字符串保底）。这段没有网络依赖，所以能真判。
func TestVariantToValue(t *testing.T) {
	cases := []struct {
		name    string
		in      any
		wantNum float64
		wantStr string
	}{
		{"bool true", true, 1, ""},
		{"bool false", false, 0, ""},
		{"float32", float32(1.5), 1.5, ""},
		{"float64", 2.25, 2.25, ""},
		{"int8", int8(-8), -8, ""},
		{"int16", int16(-300), -300, ""},
		{"int32", int32(-70000), -70000, ""},
		{"int64", int64(-5000000000), -5000000000, ""},
		{"uint8", uint8(200), 200, ""},
		{"uint16", uint16(65000), 65000, ""},
		{"uint32", uint32(4000000000), 4000000000, ""},
		{"uint64", uint64(12345), 12345, ""},
		{"string", "RUN", 0, "RUN"},
	}
	for _, tc := range cases {
		v, err := ua.NewVariant(tc.in)
		if err != nil {
			t.Fatalf("%s: ua.NewVariant 失败：%v", tc.name, err)
		}
		got := variantToValue(v)
		if tc.wantStr != "" {
			if got.Str != tc.wantStr {
				t.Errorf("%s: Str = %q, want %q", tc.name, got.Str, tc.wantStr)
			}
			continue
		}
		if got.Num != tc.wantNum {
			t.Errorf("%s: Num = %v, want %v", tc.name, got.Num, tc.wantNum)
		}
	}
}

// 复杂类型（数组 / 结构体）没有数值语义，必须落到字符串而不是被丢掉——
// 现场遇到数组点位时，"看不到值" 比 "看到一串文本" 更难排查。
func TestVariantToValueKeepsComplexTypes(t *testing.T) {
	v, err := ua.NewVariant([]int32{1, 2, 3})
	if err != nil {
		t.Fatalf("ua.NewVariant 失败：%v", err)
	}
	got := variantToValue(v)
	if got.Str == "" {
		t.Errorf("数组变体应当转成字符串保底，实际 %+v", got)
	}
}

func TestVariantToValueNil(t *testing.T) {
	if got := variantToValue(nil); got.Num != 0 || got.Str != "" {
		t.Errorf("nil variant 应当返回零值，实际 %+v", got)
	}
}

// SecurityPolicy 的字符串大小写与空格在配置文件里很随意，映射不能因此退化成 None。
func TestSecurityModeMapping(t *testing.T) {
	cases := []struct {
		in   string
		want ua.MessageSecurityMode
	}{
		{"", ua.MessageSecurityModeNone},
		{"None", ua.MessageSecurityModeNone},
		{"Sign", ua.MessageSecurityModeSign},
		{"sign", ua.MessageSecurityModeSign},
		{" SignAndEncrypt ", ua.MessageSecurityModeSignAndEncrypt},
		{"signandencrypt", ua.MessageSecurityModeSignAndEncrypt},
		{"Basic256Sha256", ua.MessageSecurityModeNone}, // 认不出的写法回 None，不猜成加密
	}
	for _, tc := range cases {
		if got := securityMode(tc.in); got != tc.want {
			t.Errorf("securityMode(%q) = %v, want %v", tc.in, got, tc.want)
		}
	}
}

func TestNewAppliesDefaults(t *testing.T) {
	d, err := New("opc.tcp://127.0.0.1:4840", nil)
	if err != nil {
		t.Fatal(err)
	}
	s := d.(*session)
	if s.cfg.PollIntervalMs != 5000 || s.cfg.TimeoutMs != 5000 || s.cfg.SecurityPolicy != "None" {
		t.Errorf("缺省配置不对：%+v", s.cfg)
	}
	if s.endpoint != "opc.tcp://127.0.0.1:4840" {
		t.Errorf("endpoint 未原样保留：%q", s.endpoint)
	}
	// 显式 0 也要回默认：0 毫秒超时会让每次请求立刻失败
	d2, _ := New(" opc.tcp://127.0.0.1:4841 ", []byte(`{"securityPolicy":"Sign","pollIntervalMs":0,"timeoutMs":0,"username":"u","password":"p"}`))
	s2 := d2.(*session)
	if s2.cfg.PollIntervalMs != 5000 || s2.cfg.TimeoutMs != 5000 {
		t.Errorf("0 没有回到默认值：%+v", s2.cfg)
	}
	if s2.endpoint != "opc.tcp://127.0.0.1:4841" {
		t.Errorf("endpoint 没去掉空格：%q", s2.endpoint)
	}
	if securityMode(s2.cfg.SecurityPolicy) != ua.MessageSecurityModeSign {
		t.Errorf("SecurityPolicy 没落到选项里：%+v", s2.cfg)
	}
}
