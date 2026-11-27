package openapi

import "nexadmin/core/apidoc"

// APIDoc 实现 apidoc.Describer，为开放平台中稳定的管理端点补充字段级说明。
func (module) APIDoc() []apidoc.Operation {
	appRequest := apidoc.Obj(map[string]any{
		"name":        apidoc.Str("应用名称，必填，去除首尾空白后 1..100 个字符"),
		"description": apidoc.Str("应用描述，最多 500 个字符"),
		"roleIds":     apiDocStringArray("绑定的角色 ID；须属于当前租户且操作者可分配，最多 50 个；空数组表示不绑定角色。应用合并所选角色的权限与数据范围；应用没有用户和部门，self / dept / deptAndChild 不展开部门，部门访问使用 custom 范围"),
		"ipAllow":     apiDocStringArray("调用方 IP 白名单，支持 IPv4/IPv6 地址或 CIDR，最多 50 个；空数组表示不限制"),
		"enabled":     apidoc.Bool("是否启用；创建时省略默认启用，更新时省略保持原值"),
	}, "name")
	apiDocAddRateLimitFields(appRequest)

	app := apiDocAppObject(false)
	appCreated := apiDocAppObject(true)
	webhookRequest := apidoc.Obj(map[string]any{
		"name":    apidoc.Str("订阅名称，必填，去除首尾空白后 1..100 个字符"),
		"url":     apidoc.Str("接收 URL，必填，最多 500 个字符；仅允许 HTTPS，开发环境允许 http://localhost 或 http://127.0.0.1"),
		"events":  apiDocStringArray("订阅的事件名，必填且至少一项；事件须已注册，重复项自动去重"),
		"enabled": apidoc.Bool("是否启用；创建时省略默认启用，更新时省略保持原值"),
	}, "name", "url", "events")
	webhook := apiDocWebhookObject(false)
	webhookCreated := apiDocWebhookObject(true)

	tokenRequest := apidoc.Obj(map[string]any{
		"name":      apidoc.Str("令牌名称，必填，去除首尾空白后 1..100 个字符"),
		"scope":     apiDocScopeSchema(),
		"expiresAt": apiDocNullableString("可空；空值或 null 表示永不过期。支持 RFC3339、YYYY-MM-DD HH:mm:ss、YYYY-MM-DD，且必须晚于当前时间"),
	}, "name", "scope")
	apiDocAddRateLimitFields(tokenRequest)

	return []apidoc.Operation{
		{
			Method:  "GET",
			Path:    "/openapi/apps",
			Summary: "开放应用列表（分页）",
			Description: "按创建时间倒序返回当前租户的开放应用；不返回应用密钥。" +
				"pageSize 省略时每页返回 200 条，显式传入时默认 10 条，上限 200 条。",
			Query: []apidoc.Param{
				{Name: "keyword", Description: "按应用名称或 appKey 模糊搜索"},
				{Name: "page", Type: "integer", Description: "页码，默认 1"},
				{Name: "pageSize", Type: "integer", Description: "每页条数，省略时为 200，上限 200"},
			},
			Response: apiDocPage(app),
		},
		{
			Method:      "POST",
			Path:        "/openapi/apps",
			Summary:     "创建开放应用",
			Description: "创建服务账号并返回 appKey 与一次性明文 appSecret；appSecret 仅在本次响应返回，请立即保存，后续查询和更新不再返回。",
			Request:     appRequest,
			Response:    appCreated,
		},
		{
			Method:      "PUT",
			Path:        "/openapi/apps/:id",
			Summary:     "更新开放应用",
			Description: "更新应用名称、描述、角色、IP 白名单与限流配置；enabled 与配额字段省略时保持原值。不修改也不返回应用密钥。",
			Request:     appRequest,
			Response:    app,
		},
		{
			Method:  "GET",
			Path:    "/openapi/webhooks",
			Summary: "Webhook 订阅列表（分页）",
			Description: "返回当前租户的 Webhook 订阅；不返回签名密钥。" +
				"pageSize 省略时每页返回 200 条，显式传入时默认 10 条，上限 200 条。",
			Query: []apidoc.Param{
				{Name: "keyword", Description: "按订阅名称或接收 URL 模糊搜索"},
				{Name: "page", Type: "integer", Description: "页码，默认 1"},
				{Name: "pageSize", Type: "integer", Description: "每页条数，省略时为 200，上限 200"},
				{Name: "sort", Description: "排序字段，支持 name、created_at；其他值回退 created_at"},
				{Name: "desc", Type: "boolean", Description: "是否降序，默认 false；同名字段下按 id 倒序"},
			},
			Response: apiDocPage(webhook),
		},
		{
			Method:      "POST",
			Path:        "/openapi/webhooks",
			Summary:     "创建 Webhook 订阅",
			Description: "创建订阅并返回一次性明文 secret；secret 仅在本次响应返回，请立即保存，后续查询和更新不再返回。",
			Request:     webhookRequest,
			Response:    webhookCreated,
		},
		{
			Method:      "PUT",
			Path:        "/openapi/webhooks/:id",
			Summary:     "更新 Webhook 订阅",
			Description: "更新订阅名称、接收 URL 与事件列表；enabled 省略时保持原值。不修改也不返回签名密钥。",
			Request:     webhookRequest,
			Response:    webhook,
		},
		{
			Method:      "GET",
			Path:        "/openapi/deliveries",
			Summary:     "Webhook 投递记录列表（分页）",
			Description: "返回当前租户的投递记录，默认每页 10 条，上限 200 条。",
			Query: []apidoc.Param{
				{Name: "webhookId", Description: "Webhook ID，支持逗号分隔多值"},
				{Name: "status", Description: "投递状态，支持 success、failed、pending，可用逗号分隔多值；任一值非法时整个请求拒绝"},
				{Name: "event", Description: "按事件名精确筛选"},
				{Name: "page", Type: "integer", Description: "页码，默认 1"},
				{Name: "pageSize", Type: "integer", Description: "每页条数，默认 10，上限 200"},
				{Name: "sort", Description: "排序字段，支持 event、status、created_at；其他值回退 created_at"},
				{Name: "desc", Type: "boolean", Description: "是否降序，默认 false；同名字段下按 id 倒序"},
			},
			Response: apiDocPage(apiDocDeliveryObject()),
		},
		{
			Method:      "GET",
			Path:        "/openapi/audits",
			Summary:     "开放 API 调用审计列表（分页）",
			Description: "返回当前租户的凭证调用元数据，保留 90 天；不保存令牌、签名、查询参数或请求/响应内容。仅真实登录会话可调用，PAT 与应用主体不可访问。",
			Query: []apidoc.Param{
				{Name: "principalType", Description: "凭证类型：pat 或 app"},
				{Name: "credentialId", Description: "按凭证 ID 精确筛选"},
				{Name: "method", Description: "按 HTTP 方法精确筛选，如 GET、POST"},
				{Name: "authOutcome", Description: "认证结果：authenticated 或 rejected"},
				{Name: "status", Type: "integer", Description: "HTTP 状态码，100..599"},
				{Name: "start", Description: "请求时间下界，支持 RFC3339、YYYY-MM-DD HH:mm:ss 或 YYYY-MM-DD"},
				{Name: "end", Description: "请求时间上界，支持 RFC3339、YYYY-MM-DD HH:mm:ss 或 YYYY-MM-DD"},
				{Name: "keyword", Description: "按凭证名称、凭证标识、用户名、路由模板或 Request ID 模糊搜索"},
				{Name: "page", Type: "integer", Description: "页码，默认 1"},
				{Name: "pageSize", Type: "integer", Description: "每页条数，默认 10，上限 200"},
				{Name: "sort", Description: "排序字段：requestedAt、durationMs、statusCode、credentialName；默认 requestedAt"},
				{Name: "desc", Type: "boolean", Description: "是否降序，默认 false"},
			},
			Response: apiDocPage(apiDocAuditObject()),
		},
		{
			Method:      "GET",
			Path:        "/openapi/events",
			Summary:     "Webhook 事件目录",
			Description: "返回当前已注册的事件名称与说明，不分页；插件注册的事件也会包含在结果中。",
			Response: map[string]any{
				"type":  "array",
				"items": apiDocEventObject(),
			},
		},
		{
			Method:      "GET",
			Path:        "/profile/tokens",
			Summary:     "个人访问令牌列表",
			Description: "返回当前登录用户的个人访问令牌，按创建时间倒序排列，不分页；仅真实登录会话可调用，不返回令牌明文。",
			Response: map[string]any{
				"type":  "array",
				"items": apiDocTokenObject(false),
			},
		},
		{
			Method:      "POST",
			Path:        "/profile/tokens",
			Summary:     "创建个人访问令牌",
			Description: "为当前登录用户创建令牌；应用或 PAT 主体不可调用。明文 token 仅在本次响应返回一次，后续查询不可见，请立即保存。",
			Request:     tokenRequest,
			Response:    apiDocTokenObject(true),
		},
	}
}

func apiDocAuditObject() map[string]any {
	principal := apidoc.Str("凭证类型：pat 或 app")
	principal["enum"] = []string{"pat", "app"}
	outcome := apidoc.Str("认证结果：authenticated 或 rejected")
	outcome["enum"] = []string{"authenticated", "rejected"}
	return apidoc.Obj(map[string]any{
		"id":              apidoc.Str("审计记录 ID"),
		"principalType":   principal,
		"credentialId":    apidoc.Str("凭证 ID；应用凭证为应用 ID，PAT 为令牌 ID"),
		"credentialName":  apidoc.Str("凭证名称快照"),
		"credentialLabel": apidoc.Str("非机密标识快照：AppKey 或令牌前缀"),
		"userId":          apidoc.Str("PAT 所属用户 ID；应用主体为 0"),
		"username":        apidoc.Str("PAT 所属用户名；应用主体为空字符串"),
		"authOutcome":     outcome,
		"failureKey":      apidoc.Str("认证或授权拒绝时的稳定错误码；成功时为空字符串"),
		"method":          apidoc.Str("HTTP 方法"),
		"path":            apidoc.Str("路由模板，不包含查询参数"),
		"statusCode":      apidoc.Int("HTTP 状态码"),
		"durationMs":      apidoc.Int("请求处理耗时（毫秒）"),
		"clientIp":        apidoc.Str("客户端 IP"),
		"userAgent":       apidoc.Str("User-Agent，最长 500 个字符"),
		"requestId":       apidoc.Str("请求关联 ID"),
		"requestedAt":     apidoc.Str("请求开始时间（RFC3339）"),
		"createdAt":       apidoc.Str("审计写入时间（RFC3339）"),
	}, "id", "principalType", "credentialId", "credentialName", "credentialLabel", "userId", "username", "authOutcome", "failureKey", "method", "path", "statusCode", "durationMs", "clientIp", "userAgent", "requestId", "requestedAt", "createdAt")
}

func apiDocPage(item map[string]any) map[string]any {
	return apidoc.Obj(map[string]any{
		"list":     map[string]any{"type": "array", "items": item},
		"total":    apidoc.Int("符合条件的总条数"),
		"page":     apidoc.Int("当前页码"),
		"pageSize": apidoc.Int("每页条数"),
	}, "list", "total", "page", "pageSize")
}

func apiDocStringArray(desc string) map[string]any {
	return map[string]any{
		"type":        "array",
		"description": desc,
		"items":       map[string]any{"type": "string"},
	}
}

func apiDocNullableString(desc string) map[string]any {
	s := apidoc.Str(desc)
	s["nullable"] = true
	return s
}

func apiDocScopeSchema() map[string]any {
	s := apidoc.Str("权限范围：read 为只读，write 为读写")
	s["enum"] = []string{"read", "write"}
	return s
}

func apiDocRateLimitSchema() map[string]any {
	return apidoc.Int("rateWindowSec 窗口内允许的最大请求数；0 表示不限流，上限 1000000")
}

func apiDocRateWindowSchema() map[string]any {
	return apidoc.Int("限流窗口时长（秒）；rateLimit 大于 0 时须为 1..86400，rateLimit 为 0 时不生效")
}

func apiDocAddRateLimitFields(props map[string]any) {
	props["rateLimit"] = apiDocRateLimitSchema()
	props["rateWindowSec"] = apiDocRateWindowSchema()
}

func apiDocAppObject(includeSecret bool) map[string]any {
	props := map[string]any{
		"id":          apidoc.Str("应用 ID"),
		"name":        apidoc.Str("应用名称"),
		"appKey":      apidoc.Str("开放应用标识，用于 X-App-Key 请求头"),
		"description": apidoc.Str("应用描述"),
		"roleIds":     apiDocStringArray("绑定的角色 ID"),
		"enabled":     apidoc.Bool("是否启用"),
		"ipAllow":     apiDocStringArray("调用方 IP 白名单；空数组表示不限制"),
		"createdAt":   apidoc.Str("创建时间（RFC3339）"),
		"lastUsedAt":  apiDocNullableString("最近调用时间（RFC3339）；从未调用时为 null"),
	}
	apiDocAddRateLimitFields(props)
	required := []string{"id", "name", "appKey", "description", "roleIds", "enabled", "ipAllow", "createdAt", "lastUsedAt", "rateLimit", "rateWindowSec"}
	if includeSecret {
		props["appSecret"] = apidoc.Str("一次性明文应用密钥；仅在创建响应中返回")
		required = append(required, "appSecret")
	}
	return apidoc.Obj(props, required...)
}

func apiDocWebhookObject(includeSecret bool) map[string]any {
	props := map[string]any{
		"id":        apidoc.Str("Webhook ID"),
		"name":      apidoc.Str("订阅名称"),
		"url":       apidoc.Str("接收 URL"),
		"events":    apiDocStringArray("订阅的事件名"),
		"enabled":   apidoc.Bool("是否启用"),
		"createdAt": apidoc.Str("创建时间（RFC3339）"),
	}
	required := []string{"id", "name", "url", "events", "enabled", "createdAt"}
	if includeSecret {
		props["secret"] = apidoc.Str("一次性明文签名密钥；仅在创建响应中返回")
		required = append(required, "secret")
	}
	return apidoc.Obj(props, required...)
}

func apiDocDeliveryObject() map[string]any {
	return apidoc.Obj(map[string]any{
		"id":             apidoc.Str("投递记录 ID"),
		"webhookId":      apidoc.Str("Webhook ID"),
		"eventId":        apidoc.Str("事件 ID"),
		"event":          apidoc.Str("事件名"),
		"status":         apidoc.Str("投递状态：pending、success 或 failed"),
		"attempts":       apidoc.Int("已尝试投递次数"),
		"payload":        apidoc.Str("实际发送的 JSON 请求体"),
		"responseStatus": apidoc.Int("接收端 HTTP 状态码；尚未收到响应时为 0"),
		"responseBody":   apidoc.Str("接收端响应体，最多保存 2048 字节"),
		"error":          apidoc.Str("投递错误信息；无错误时为空字符串"),
		"durationMs":     apidoc.Int("本次投递耗时（毫秒）"),
		"nextAttemptAt":  apiDocNullableString("下次重试时间（RFC3339）；仅 pending 状态有值，其余为 null"),
		"createdAt":      apidoc.Str("投递记录创建时间（RFC3339）"),
		"deliveredAt":    apiDocNullableString("成功投递时间（RFC3339）；尚未成功时为 null"),
	}, "id", "webhookId", "eventId", "event", "status", "attempts", "payload", "responseStatus", "responseBody", "error", "durationMs", "nextAttemptAt", "createdAt", "deliveredAt")
}

func apiDocEventObject() map[string]any {
	return apidoc.Obj(map[string]any{
		"name":        apidoc.Str("事件名"),
		"description": apidoc.Str("事件说明"),
	}, "name", "description")
}

func apiDocTokenObject(includeToken bool) map[string]any {
	props := map[string]any{
		"id":        apidoc.Str("令牌 ID"),
		"name":      apidoc.Str("令牌名称"),
		"scope":     apiDocScopeSchema(),
		"prefix":    apidoc.Str("令牌前缀，用于识别令牌；不是完整凭证"),
		"expiresAt": apiDocNullableString("过期时间（RFC3339）；null 表示永不过期"),
		"createdAt": apidoc.Str("创建时间（RFC3339）"),
	}
	apiDocAddRateLimitFields(props)
	required := []string{"id", "name", "scope", "prefix", "expiresAt", "createdAt", "rateLimit", "rateWindowSec"}
	if !includeToken {
		props["lastUsedAt"] = apiDocNullableString("最近使用时间（RFC3339）；从未使用时为 null")
		required = append(required, "lastUsedAt")
	}
	if includeToken {
		props["token"] = apidoc.Str("完整明文令牌，仅在本次创建响应返回一次")
		required = append(required, "token")
	}
	return apidoc.Obj(props, required...)
}
