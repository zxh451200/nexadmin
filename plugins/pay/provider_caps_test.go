package pay

import (
	"reflect"
	"testing"
)

// 下单方式别名归一：渠道的 Create 宽松接受一串别名，能力矩阵只声明规范名。
// 不归一的话「用 scan 在只声明 native 的渠道下单」会被门禁误拒——这是引入能力门禁时
// 自己造出来的回归，所以单独钉住。

func TestNormalizePayMethod(t *testing.T) {
	cases := map[string]string{
		"scan":      "native",
		"precreate": "native",
		"offline":   "native",
		"qr":        "native",
		"native":    "native",
		"WEB":       "page",
		"pc":        "page",
		"mweb":      "wap",
		"h5":        "wap",
		"mini":      "jsapi",
		"mp":        "jsapi",
		"authcode":  "micropay",
		"facepay":   "micropay",
		"":          "",
		"unknown":   "unknown",
	}
	for in, want := range cases {
		if got := normalizePayMethod(in); got != want {
			t.Errorf("normalizePayMethod(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestCapabilitiesSupportsMethodAcrossAliases(t *testing.T) {
	caps := Capabilities{Methods: []string{"native", "jsapi", "h5", "app"}}
	for _, m := range []string{"native", "qr", "scan", "jsapi", "mini", "h5", "wap", "app", ""} {
		if !caps.supportsMethod(m) {
			t.Errorf("声明 native/jsapi/h5/app 时，别名 %q 必须命中", m)
		}
	}
	for _, m := range []string{"micropay", "page", "unknown"} {
		if caps.supportsMethod(m) {
			t.Errorf("%q 不在声明范围内，应判为不支持", m)
		}
	}
	// 未声明方式的渠道一律放行（由渠道自己判），避免能力矩阵缺省变成准入闸。
	if !(Capabilities{}).supportsMethod("micropay") {
		t.Error("未声明 Methods 的渠道必须放行，而不是拒绝所有方式")
	}
}

func TestCapabilitiesCanonicalMethodsDedupes(t *testing.T) {
	caps := Capabilities{Methods: []string{"native", "qr", "scan", "page", "wap", "h5", "app", "jsapi", "micropay"}}
	want := []string{"app", "jsapi", "micropay", "native", "page", "wap"}
	if got := caps.canonicalMethods(); !reflect.DeepEqual(got, want) {
		t.Fatalf("canonicalMethods() = %v, want %v", got, want)
	}
	if got := (Capabilities{}).canonicalMethods(); got != nil {
		t.Errorf("未声明时应返回 nil（前端据此不限制方式），got %v", got)
	}
}

func TestCapabilitiesSupportsCurrency(t *testing.T) {
	// 未声明币种 = 只 CNY（真实国内渠道口径），空币种按 CNY 处理。
	if !(Capabilities{}).supportsCurrency("") || !(Capabilities{}).supportsCurrency("cny") {
		t.Error("未声明币种时 CNY 必须放行")
	}
	if (Capabilities{}).supportsCurrency("USD") {
		t.Error("未声明币种时外币必须判不支持")
	}
	multi := Capabilities{Currencies: []string{"CNY", "USD"}}
	if !multi.supportsCurrency("usd") || multi.supportsCurrency("EUR") {
		t.Error("声明了币种列表时按列表判")
	}
}

// 每个渠道实现声明的方式与币种都必须能通过自己的判定函数——防止「声明了却把自己判否」
// 这类自相矛盾（能力矩阵下发给前端，前端照它禁用入口）。
func TestRegisteredProvidersCapsAreSelfConsistent(t *testing.T) {
	for _, name := range ProviderNames() {
		p, err := LookupProvider(name)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		cp, ok := p.(CapabilityProvider)
		if !ok {
			t.Errorf("%s 没有声明 CapabilityProvider：能力矩阵只能给最小集，前端无从判断", name)
			continue
		}
		caps := cp.Capabilities()
		for _, m := range caps.Methods {
			if !caps.supportsMethod(m) {
				t.Errorf("%s 声明了方式 %q 却被自己的判定函数拒绝", name, m)
			}
		}
		for _, cur := range caps.Currencies {
			if !caps.supportsCurrency(cur) {
				t.Errorf("%s 声明了币种 %q 却被自己的判定函数拒绝", name, cur)
			}
		}
	}
}
