package auth

import (
	"context"
	"sync/atomic"

	"nexadmin/core/config"
)

// 本文件是「登录方式」的运行时数据源：设置页（sys_setting 的 auth 分区）保存的开关、凭据与代理
// 由此注入，config 模块启动时注册（modules/system/config）。未注入时一切回落到 config.yaml，
// 与注入前的行为完全一致——模块停用 / 单测环境都走这条老路。

// ProviderOverride 是设置页对单个第三方 provider 的覆盖：
//   - Disabled=true：管理员显式关闭，该 provider 不再出现（优先于 config.yaml）；
//   - Fields：config 形状（snake_case）的字段，只含非空值——空字段沿用 config.yaml
//     （例如设置页留空 client_secret，就继续用配置文件 / 环境变量里的那个）。
type ProviderOverride struct {
	Disabled bool
	Fields   map[string]any
}

var providerOverrideSource atomic.Value // func(context.Context) map[string]ProviderOverride

// SetProviderOverrideSource 注入 / 清除设置页对 auth.providers 的覆盖；传 nil 清除。
func SetProviderOverrideSource(fn func(context.Context) map[string]ProviderOverride) {
	if fn == nil {
		providerOverrideSource.Store((func(context.Context) map[string]ProviderOverride)(nil))
		return
	}
	providerOverrideSource.Store(fn)
}

// ProviderOverrides 返回设置页当前的覆盖；未注入或读取失败时为空。
func ProviderOverrides(ctx context.Context) map[string]ProviderOverride {
	if fn, ok := providerOverrideSource.Load().(func(context.Context) map[string]ProviderOverride); ok && fn != nil {
		return fn(ctx)
	}
	return nil
}

// MergeProviders 把覆盖叠加到 config.yaml 的 provider 表上，返回新 map（不改入参）。
// 字段级合并：覆盖里的非空字段替换原值，其余字段（含空值代表的“沿用”）保持原样。
func MergeProviders(base map[string]any, over map[string]ProviderOverride) map[string]any {
	if len(over) == 0 {
		return base
	}
	out := make(map[string]any, len(base)+len(over))
	for k, v := range base {
		out[k] = v
	}
	for k, o := range over {
		if o.Disabled {
			delete(out, k)
			continue
		}
		merged := make(map[string]any)
		if cur, ok := out[k].(map[string]any); ok {
			for f, v := range cur {
				merged[f] = v
			}
		}
		for f, v := range o.Fields {
			merged[f] = v
		}
		out[k] = merged
	}
	return out
}

// EffectiveProviders 返回运行时生效的 auth.providers：config.yaml 叠加设置页覆盖。
func EffectiveProviders(ctx context.Context, base map[string]any) map[string]any {
	return MergeProviders(base, ProviderOverrides(ctx))
}

// EffectiveProviderConf 返回运行时生效的单个 provider 配置（校验规则与 config.Auth.ProviderConf 一致）。
func EffectiveProviderConf(ctx context.Context, a config.Auth, name string) (config.ProviderConf, bool) {
	if !KnownProvider(name) {
		return config.ProviderConf{}, false
	}
	return a.ProviderConfFrom(EffectiveProviders(ctx, a.Providers), name)
}

// EffectiveProviderNames 返回运行时生效且可用的 provider 键（按字母序，与 /auth/methods 一致）。
func EffectiveProviderNames(ctx context.Context, a config.Auth) []string {
	return a.ProviderNamesFrom(EffectiveProviders(ctx, a.Providers))
}

var loginMethodsSource atomic.Value // func(context.Context) []string

// SetLoginMethodsSource 注入 / 清除「登录方式」运行时源（设置页的账号密码 / 手机号开关）；传 nil 清除。
// 返回 nil 表示设置页没有保存过，沿用 config.yaml 的 auth.methods。
func SetLoginMethodsSource(fn func(context.Context) []string) {
	if fn == nil {
		loginMethodsSource.Store((func(context.Context) []string)(nil))
		return
	}
	loginMethodsSource.Store(fn)
}

// LoginMethods 返回运行时生效的登录方式。设置页没保存过（或模块未运行）时用 fallback（config.yaml）。
func LoginMethods(ctx context.Context, fallback []string) []string {
	fn, ok := loginMethodsSource.Load().(func(context.Context) []string)
	if !ok || fn == nil {
		return fallback
	}
	if v := fn(ctx); v != nil {
		return v
	}
	return fallback
}

var oauthProxySource atomic.Value // func(context.Context) (string, bool)

// SetOAuthProxySource 注入 / 清除第三方登录出站代理的运行时源；传 nil 清除。
// 第二个返回值报告设置页是否管理了这一项（false = 沿用 config.yaml 的 auth.proxy）。
func SetOAuthProxySource(fn func(context.Context) (string, bool)) {
	if fn == nil {
		oauthProxySource.Store((func(context.Context) (string, bool))(nil))
		return
	}
	oauthProxySource.Store(fn)
}

// OAuthProxy 返回运行时生效的出站代理地址（空 = 直连）。
func OAuthProxy(ctx context.Context, fallback string) string {
	fn, ok := oauthProxySource.Load().(func(context.Context) (string, bool))
	if !ok || fn == nil {
		return fallback
	}
	if v, managed := fn(ctx); managed {
		return v
	}
	return fallback
}
