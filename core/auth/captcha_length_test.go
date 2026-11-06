package auth

import (
	"context"
	"testing"
)

// 位数从参数设置进来，中间隔着一层运行时源：越界值必须回落到出厂值，
// 而不是拿去生成一张 0 位或 99 位的验证码。这一层没有任何测试时，
// 「设置页存了 99、登录页出 5 位」这种分家只能靠现场报上来。
func TestCaptchaLengthClampsToConfigurableRange(t *testing.T) {
	cases := []struct {
		name   string
		source func(context.Context) int
		want   int
	}{
		{"源未注入回出厂值", nil, CaptchaLenDefault},
		{"出厂值原样生效", func(context.Context) int { return 5 }, 5},
		{"下限", func(context.Context) int { return CaptchaLenMin }, CaptchaLenMin},
		{"上限", func(context.Context) int { return CaptchaLenMax }, CaptchaLenMax},
		{"低于下限回落", func(context.Context) int { return CaptchaLenMin - 1 }, CaptchaLenDefault},
		{"高于上限回落", func(context.Context) int { return CaptchaLenMax + 1 }, CaptchaLenDefault},
		{"零回落（参数被清空）", func(context.Context) int { return 0 }, CaptchaLenDefault},
		{"负数回落", func(context.Context) int { return -3 }, CaptchaLenDefault},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			SetLoginCaptchaLengthSource(tc.source)
			t.Cleanup(func() { SetLoginCaptchaLengthSource(nil) })
			s := &Service{}
			if got := s.captchaLength(context.Background()); got != tc.want {
				t.Errorf("captchaLength = %d, want %d", got, tc.want)
			}
		})
	}
}

// 清除源之后必须回到出厂口径：装配层在优雅关闭时清源，之后再生成的验证码不能沿用旧值。
func TestCaptchaLengthSourceCleared(t *testing.T) {
	SetLoginCaptchaLengthSource(func(context.Context) int { return CaptchaLenMax })
	if got := (&Service{}).captchaLength(context.Background()); got != CaptchaLenMax {
		t.Fatalf("注入后应取到 %d，实际 %d", CaptchaLenMax, got)
	}
	SetLoginCaptchaLengthSource(nil)
	if got := (&Service{}).captchaLength(context.Background()); got != CaptchaLenDefault {
		t.Errorf("清源后应回出厂值 %d，实际 %d", CaptchaLenDefault, got)
	}
}

// 生成的验证码位数必须跟配置一致——上面两条只测了取值函数，这条把「位数真的落到答案上」
// 也钉住：改了常量却忘了改渲染，前面两条仍然是绿的。
func TestNewCaptchaUsesConfiguredLength(t *testing.T) {
	SetLoginCaptchaLengthSource(func(context.Context) int { return CaptchaLenMax })
	t.Cleanup(func() { SetLoginCaptchaLengthSource(nil) })
	s := newCaptchaSvc(t)
	key, _, err := s.NewCaptcha(context.Background(), testIP)
	if err != nil {
		t.Fatal(err)
	}
	if got := len(answerOf(t, s, key)); got != CaptchaLenMax {
		t.Errorf("配置的位数没有落到验证码答案上：生成 %d 位，期望 %d 位", got, CaptchaLenMax)
	}
}
