package apidoc

import (
	"encoding/json"
	"sort"
	"strings"
	"unicode"
)

const signDesc = "开放应用签名：请求头 X-App-Key、X-Timestamp（Unix 秒，与服务器偏差不超过 5 分钟）、X-Nonce（每次唯一）、" +
	"X-Signature = hex(HMAC-SHA256(appSecret, METHOD\\npath\\n规范化query\\nsha256hex(body)\\ntimestamp\\nnonce))。" +
	"OpenAPI 的 apiKey 类型无法完整表达签名流程，此处仅用 X-Signature 头占位。"

// toOpenAPIPath 把 gin 的 :id / *rest 写法转成 OpenAPI 的 {id}，并返回参数名。
func toOpenAPIPath(p string) (string, []string) {
	segs := strings.Split(p, "/")
	var params []string
	for i, s := range segs {
		if len(s) > 1 && (s[0] == ':' || s[0] == '*') {
			segs[i] = "{" + s[1:] + "}"
			params = append(params, s[1:])
		}
	}
	return strings.Join(segs, "/"), params
}

// operationID 由方法与路径生成：GET /users/:id/status → getUsersByIdStatus。
func operationID(method, p string) string {
	var sb strings.Builder
	sb.WriteString(strings.ToLower(method))
	for _, s := range strings.Split(p, "/") {
		if s == "" {
			continue
		}
		if s[0] == ':' || s[0] == '*' {
			sb.WriteString("By")
			s = s[1:]
		}
		up := true
		for _, r := range s {
			if !unicode.IsLetter(r) && !unicode.IsDigit(r) {
				up = true
				continue
			}
			if up {
				r = unicode.ToUpper(r)
				up = false
			}
			sb.WriteRune(r)
		}
	}
	return sb.String()
}

func envelopeWith(data map[string]any) map[string]any {
	if data == nil {
		return map[string]any{"$ref": "#/components/schemas/Envelope"}
	}
	return map[string]any{"allOf": []any{
		map[string]any{"$ref": "#/components/schemas/Envelope"},
		map[string]any{"type": "object", "properties": map[string]any{"data": data}},
	}}
}

func errRef(name string) map[string]any {
	return map[string]any{"$ref": "#/components/responses/" + name}
}

func errResponse(desc string) map[string]any {
	return map[string]any{
		"description": desc,
		"content": map[string]any{"application/json": map[string]any{
			"schema": map[string]any{"$ref": "#/components/schemas/Envelope"},
		}},
	}
}

func buildSpec(opts Options, routes []Route, ops map[string]Operation) ([]byte, error) {
	paths := map[string]any{}
	tagSet := map[string]bool{}
	usedID := map[string]int{}
	for _, r := range routes {
		rel := strings.TrimPrefix(r.Path, APIPrefix)
		oaPath, params := toOpenAPIPath(rel)
		item, _ := paths[oaPath].(map[string]any)
		if item == nil {
			item = map[string]any{}
			paths[oaPath] = item
		}
		id := operationID(r.Method, rel)
		if usedID[id]++; usedID[id] > 1 {
			id += string(rune('A' + usedID[id] - 2))
		}
		o, described := ops[routeKey(r.Method, rel)]
		op := map[string]any{
			"operationId": id,
			"tags":        []string{r.Module},
			"summary":     r.Method + " " + rel,
			"x-module":    r.Module,
		}
		desc := []string{}
		if described && o.Summary != "" {
			op["summary"] = o.Summary
		}
		if described && o.Description != "" {
			desc = append(desc, o.Description)
		}
		if r.Perm != "" {
			desc = append(desc, "所需权限码："+r.Perm)
			op["x-permission"] = r.Perm
		}
		if r.SuperOnly {
			desc = append(desc, "仅超级管理员可调用。")
			op["x-super-only"] = true
		}
		if len(desc) > 0 {
			op["description"] = strings.Join(desc, "\n\n")
		}
		var plist []any
		for _, name := range params {
			plist = append(plist, map[string]any{"name": name, "in": "path", "required": true, "schema": map[string]any{"type": "string"}})
		}
		if described {
			for _, q := range o.Query {
				typ := q.Type
				if typ == "" {
					typ = "string"
				}
				p := map[string]any{"name": q.Name, "in": "query", "required": q.Required, "schema": map[string]any{"type": typ}}
				if q.Description != "" {
					p["description"] = q.Description
				}
				plist = append(plist, p)
			}
			if o.Request != nil {
				op["requestBody"] = map[string]any{"required": true, "content": map[string]any{
					"application/json": map[string]any{"schema": o.Request},
				}}
			}
		}
		if plist != nil {
			op["parameters"] = plist
		}
		resp := map[string]any{"200": map[string]any{
			"description": "成功（统一响应体）",
			"content":     map[string]any{"application/json": map[string]any{"schema": envelopeWith(o.Response)}},
		}}
		if r.Auth {
			sec := []any{map[string]any{"bearerAuth": []string{}}}
			if r.AppCallable {
				sec = append(sec, map[string]any{"appSignature": []string{}})
			}
			op["security"] = sec
			resp["401"], resp["403"] = errRef("Unauthorized"), errRef("Forbidden")
		} else {
			op["security"] = []any{}
		}
		resp["429"], resp["503"] = errRef("TooManyRequests"), errRef("Unavailable")
		op["responses"] = resp
		op["x-openapi-callable"] = r.OpenapiCallable
		op["x-token-callable"] = r.TokenCallable
		op["x-app-callable"] = r.AppCallable
		item[strings.ToLower(r.Method)] = op
		tagSet[r.Module] = true
	}
	names := make([]string, 0, len(tagSet))
	for t := range tagSet {
		names = append(names, t)
	}
	sort.Strings(names)
	tags := make([]any, 0, len(names))
	for _, n := range names {
		tags = append(tags, map[string]any{"name": n})
	}
	doc := map[string]any{
		"openapi": "3.0.3",
		"info": map[string]any{
			"title": opts.Title, "version": opts.Version,
			"description": "由运行时路由清单自动生成：路径、方法、权限码、认证方式准确；请求/响应字段仅在模块提供说明时给出。",
		},
		"servers": []any{map[string]any{"url": APIPrefix}},
		"tags":    tags,
		"paths":   paths,
		"components": map[string]any{
			"securitySchemes": map[string]any{
				"bearerAuth": map[string]any{
					"type": "http", "scheme": "bearer",
					"description": "登录后的访问令牌；个人访问令牌（nxp_ 开头）同样放在 Authorization: Bearer 里。",
				},
				"appSignature": map[string]any{"type": "apiKey", "in": "header", "name": "X-Signature", "description": signDesc},
			},
			"schemas": map[string]any{"Envelope": map[string]any{
				"type":     "object",
				"required": []string{"code", "message"},
				"properties": map[string]any{
					"code":    map[string]any{"type": "integer", "description": "0 = 成功；失败时与 HTTP 状态码相同"},
					"message": map[string]any{"type": "string"},
					"data":    map[string]any{"nullable": true, "description": "业务数据；分页接口为 {list,total,page,pageSize}"},
					"reason":  map[string]any{"type": "string", "description": "机器可识别原因，如 NOT_INITIALIZED"},
				},
			}},
			"responses": map[string]any{
				"Unauthorized":    errResponse("未登录或令牌无效/过期"),
				"Forbidden":       errResponse("没有权限，或该凭证不允许调用此接口"),
				"TooManyRequests": errResponse("请求过于频繁"),
				"Unavailable":     errResponse("服务暂不可用（含未初始化）"),
			},
		},
	}
	return json.MarshalIndent(doc, "", "  ")
}
