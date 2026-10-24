package auth

// KnownProviders 是框架支持的第三方登录 provider 键，顺序即登录页/个人中心的展示顺序。
var KnownProviders = []string{"wecom", "dingtalk", "feishu", "google", "github", "oidc"}

// KnownProvider 判断键是否是支持的 provider。
func KnownProvider(key string) bool {
	for _, k := range KnownProviders {
		if k == key {
			return true
		}
	}
	return false
}

// ProviderDisplay 是 provider 的默认展示名（i18n 里没有 auth.provider.<key> 时使用）。
func ProviderDisplay(key string) string {
	switch key {
	case "wecom":
		return "企业微信"
	case "dingtalk":
		return "钉钉"
	case "feishu":
		return "飞书"
	case "google":
		return "Google"
	case "github":
		return "GitHub"
	}
	return "OIDC"
}
