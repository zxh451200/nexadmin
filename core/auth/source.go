package auth

import (
	"context"
	"sync/atomic"
)

// registerEnabledSource 是自助注册的运行时开关源，由 config 模块启动时注入
// （读设置页「参数设置」的 user.register_enabled，默认关闭）；未注入（模块停用 / 单测）恒为关。
// 之前这个开关只认 YAML auth.allow_register，改一次要重启；现在后台参数页切换即时生效。
var registerEnabledSource atomic.Value // func(context.Context) bool

// SetRegisterEnabledSource 注入 / 清除自助注册的运行时开关源；传 nil 清除。
func SetRegisterEnabledSource(fn func(ctx context.Context) bool) {
	if fn == nil {
		registerEnabledSource.Store((func(context.Context) bool)(nil))
		return
	}
	registerEnabledSource.Store(fn)
}

// RegisterEnabled 自助注册当前是否开放。开关未注入（config 模块未运行）视为关闭：
// 注册入口默认关闭是产品语义，宁可少开不能误开。
func RegisterEnabled(ctx context.Context) bool {
	if fn, ok := registerEnabledSource.Load().(func(context.Context) bool); ok && fn != nil {
		return fn(ctx)
	}
	return false
}

// captchaEnabledSource 是「验证码」的运行时总开关源（设置页「安全与登录 → 登录安全」的
// captchaEnabled，出厂 true）：开启时密码登录按自适应策略要求验证码（连续失败达到阈值才出现，
// 再达硬阈值升点选），自助注册与匿名获取短信验证码则必须先验证码；关闭时三条链路都不再出现、
// 也不再校验验证码。由 config 模块启动时注入。
var captchaEnabledSource atomic.Value // func(context.Context) bool

// SetCaptchaEnabledSource 注入 / 清除「验证码」运行时开关源；传 nil 清除。
func SetCaptchaEnabledSource(fn func(ctx context.Context) bool) {
	if fn == nil {
		captchaEnabledSource.Store((func(context.Context) bool)(nil))
		return
	}
	captchaEnabledSource.Store(fn)
}

// CaptchaEnabled 人机校验当前是否启用。未注入时返回 false：config 模块停用 / 单测环境下
// 不额外要求验证码（与注入前的行为一致）。
func CaptchaEnabled(ctx context.Context) bool {
	if fn, ok := captchaEnabledSource.Load().(func(context.Context) bool); ok && fn != nil {
		return fn(ctx)
	}
	return false
}

// captchaLengthSource 是「登录验证码位数」的运行时源（设置页「参数设置」的 login.captcha_length，
// 出厂 5，范围 4–8）：改完参数页即刻生效，只影响之后新生成的验证码。由 config 模块启动时注入。
var captchaLengthSource atomic.Value // func(context.Context) int

// SetLoginCaptchaLengthSource 注入 / 清除「验证码位数」运行时源；传 nil 清除。
func SetLoginCaptchaLengthSource(fn func(ctx context.Context) int) {
	if fn == nil {
		captchaLengthSource.Store((func(context.Context) int)(nil))
		return
	}
	captchaLengthSource.Store(fn)
}
