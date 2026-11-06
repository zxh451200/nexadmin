package config

import (
	"testing"
)

// 验证码位数源的解析与回退：非法值（非数字 / 空）回退兜底，合法值透传（越界钳制在 core/auth 侧）。
func TestParseCaptchaLength(t *testing.T) {
	cases := []struct {
		v        string
		fallback int
		want     int
	}{
		{"5", 5, 5},
		{"4", 5, 4},
		{"8", 5, 8},
		{" 6 ", 5, 6}, // 带空格：TrimSpace 后解析
		{"junk", 5, 5},
		{"", 5, 5},
		{"0", 5, 0}, // 越界值原样透传：钳制在 core/auth 侧（两侧同一口径，见 captchaLengthSourceFn 注释）
		{"99", 5, 99},
	}
	for _, c := range cases {
		if got := parseCaptchaLength(c.v, c.fallback); got != c.want {
			t.Errorf("parseCaptchaLength(%q, fallback=%d) = %d, want %d", c.v, c.fallback, got, c.want)
		}
	}
}
