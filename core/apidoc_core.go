package core

import "nexadmin/core/apidoc"

// coreAPIDocs 是框架自带路由（不属于任何 Module）的接口文档补充，字段取自 auth.LoginRequest。
func coreAPIDocs() []apidoc.Operation {
	return []apidoc.Operation{{
		Method:      "POST",
		Path:        "/auth/login",
		Summary:     "登录",
		Description: "type 为空或 password 时用 username + password（连续失败后需要 captcha + captchaKey）；type=phone 时用 phone + code。",
		Request: apidoc.Obj(map[string]any{
			"type":       apidoc.Str("password（默认）或 phone"),
			"username":   apidoc.Str("用户名"),
			"password":   apidoc.Str("密码"),
			"captcha":    apidoc.Str("图形验证码（连续失败后必填）"),
			"captchaKey": apidoc.Str("验证码标识，来自 GET /auth/captcha"),
			"phone":      apidoc.Str("手机号（type=phone）"),
			"code":       apidoc.Str("短信验证码（type=phone）"),
		}),
	}, {
		Method:      "POST",
		Path:        "/ws/ticket",
		Summary:     "换取 WebSocket 入场票",
		Description: "需登录。返回一次性 ticket（15 秒有效），用 ws://…/ws?ticket=… 建连；访问令牌不出现在握手 URL。",
	}}
}
