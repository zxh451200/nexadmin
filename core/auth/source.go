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

// loginCaptchaSource 是「登录验证码」的运行时开关源（设置页「参数设置」的 login.captcha_enabled，
// 出厂 true）：开启时密码登录必须先过图形验证码；关闭时回落到默认的自适应策略
// （同一用户名 / IP 连续失败达到阈值后才要求）。由 config 模块启动时注入。
var loginCaptchaSource atomic.Value // func(context.Context) bool

// SetLoginCaptchaSource 注入 / 清除「登录验证码」运行时开关源；传 nil 清除。
func SetLoginCaptchaSource(fn func(ctx context.Context) bool) {
	if fn == nil {
		loginCaptchaSource.Store((func(context.Context) bool)(nil))
		return
	}
	loginCaptchaSource.Store(fn)
}

// LoginCaptcha 「登录验证码」是否处于强制开启状态。未注入时返回 false（自适应策略），
// 保证 config 模块停用 / 单测环境的行为与从前一致。
func LoginCaptcha(ctx context.Context) bool {
	if fn, ok := loginCaptchaSource.Load().(func(context.Context) bool); ok && fn != nil {
		return fn(ctx)
	}
	return false
}
