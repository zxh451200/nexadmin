package config

import "testing"

// 布尔类参数（user.register_enabled / login.captcha_enabled）的取值解析：行缺失 / 空值回退兜底，其余按布尔语义。
func TestRegisterEnabledValue(t *testing.T) {
	cases := []struct {
		v        string
		present  bool
		fallback bool
		want     bool
	}{
		{"", false, false, false}, // 默认：关
		{"", false, true, true},   // 行缺失：回退 YAML
		{"", true, true, true},    // 空值：视为未配置，回退 YAML
		{"false", true, true, false},
		{"true", true, false, true},
		{"TRUE", true, false, true}, // 大小写不敏感
		{"1", true, false, true},
		{"on", true, false, true},
		{"yes", true, false, true},
		{"0", true, true, false},
		{"junk", true, true, false}, // 非法值不当作开启
	}
	for _, c := range cases {
		if got := boolConfigValue(c.v, c.present, c.fallback); got != c.want {
			t.Errorf("boolConfigValue(%q, present=%v, fallback=%v) = %v, want %v", c.v, c.present, c.fallback, got, c.want)
		}
	}
}
