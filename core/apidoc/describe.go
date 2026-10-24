package apidoc

// Param 描述一个查询参数。
type Param struct {
	Name        string
	Description string
	Type        string // string | integer | boolean，空 = string
	Required    bool
}

// Operation 是对单个接口的补充说明；只在确实知道字段时填写，没有的部分一律不编造。
type Operation struct {
	Method      string // GET / POST ...
	Path        string // 相对 /api/v1，gin 写法，如 /users/:id
	Summary     string
	Description string
	Query       []Param
	Request     map[string]any // JSON Schema：application/json 请求体
	Response    map[string]any // JSON Schema：统一响应 data 字段
}

// Describer 是模块的可选接口：为少数稳定接口补充摘要、查询参数、请求体 schema。
// 其余接口由路由清单兜底。
type Describer interface {
	APIDoc() []Operation
}

// Obj 构造 object schema；required 为必填字段名。
func Obj(props map[string]any, required ...string) map[string]any {
	s := map[string]any{"type": "object", "properties": props}
	if len(required) > 0 {
		s["required"] = required
	}
	return s
}

// Str / Int / Bool 构造基础类型 schema。
func Str(desc string) map[string]any  { return map[string]any{"type": "string", "description": desc} }
func Int(desc string) map[string]any  { return map[string]any{"type": "integer", "description": desc} }
func Bool(desc string) map[string]any { return map[string]any{"type": "boolean", "description": desc} }
