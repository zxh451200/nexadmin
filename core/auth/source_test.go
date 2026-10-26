package auth

import (
	"context"
	"testing"
)

// 开关源未注入时恒为关（默认关闭是产品语义，不能误开）。
func TestRegisterEnabledDefaultOff(t *testing.T) {
	SetRegisterEnabledSource(nil)
	if RegisterEnabled(context.Background()) {
		t.Fatal("register must default to off when no source is set")
	}
}

// 「登录验证码」开关源：未注入 = false（自适应策略，行为与从前一致）；注入后读源。
func TestLoginCaptchaSource(t *testing.T) {
	SetLoginCaptchaSource(nil)
	if LoginCaptcha(context.Background()) {
		t.Fatal("login captcha must default to adaptive (false) when no source is set")
	}
	t.Cleanup(func() { SetLoginCaptchaSource(nil) })
	on := true
	SetLoginCaptchaSource(func(context.Context) bool { return on })
	if !LoginCaptcha(context.Background()) {
		t.Fatal("source says on")
	}
	on = false
	if LoginCaptcha(context.Background()) {
		t.Fatal("source says off")
	}
}

// 注入后读源；清除后回到默认关。
func TestRegisterEnabledSourceToggle(t *testing.T) {
	t.Cleanup(func() { SetRegisterEnabledSource(nil) })
	ctx := context.Background()
	on := false
	SetRegisterEnabledSource(func(context.Context) bool { return on })
	if RegisterEnabled(ctx) {
		t.Fatal("source says off")
	}
	on = true
	if !RegisterEnabled(ctx) {
		t.Fatal("source says on")
	}
	SetRegisterEnabledSource(nil)
	if RegisterEnabled(ctx) {
		t.Fatal("cleared source must fall back to off")
	}
}
