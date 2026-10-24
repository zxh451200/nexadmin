package apidoc

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"hash/fnv"
	"sort"
	"strings"
	"sync"

	"github.com/gin-gonic/gin"

	"nexadmin/core/openapi"
	"nexadmin/pkg/idgen"
)

// APIPrefix 是业务接口前缀；清单只收录这个前缀下的路由。
const APIPrefix = "/api/v1"

// Route 是扁平路由清单的一项（GET /openapi/routes 的元素）。
type Route struct {
	Method string `json:"method"`
	Path   string `json:"path"` // 完整路径，含 /api/v1，路径参数保持 :id 写法
	Module string `json:"module"`
	Perm   string `json:"perm"` // 多个权限点用逗号连接；无则为空
	// Auth 为 true 表示需要登录；Public 组的路由为 false。
	Auth      bool `json:"auth"`
	SuperOnly bool `json:"superOnly"`
	// OpenapiCallable = 需要登录且令牌或应用签名至少一种可以调用（沿用 core/openapi 的禁用规则）。
	OpenapiCallable bool   `json:"openapiCallable"`
	TokenCallable   bool   `json:"tokenCallable"`
	AppCallable     bool   `json:"appCallable"`
	Summary         string `json:"summary,omitempty"`
}

// Tool 是给 MCP/AI 工具目录使用的最小路由描述。
type Tool struct {
	Name                 string                                                   `json:"name"`
	Method               string                                                   `json:"method"`
	Path                 string                                                   `json:"path"`
	Description          string                                                   `json:"description,omitempty"`
	Permission           string                                                   `json:"permission,omitempty"`
	Write                bool                                                     `json:"write,omitempty"`
	ConfirmationRequired bool                                                     `json:"confirmationRequired,omitempty"`
	Validate             func(context.Context, idgen.ID, map[string]string) error `json:"-"`
}

// Tools 返回只读和显式注册的写工具候选；目录不是授权结果，鉴权仍由原路由执行。
func (d *Doc) Tools() []Tool {
	routes := d.Routes()
	d.mu.Lock()
	writes := append([]Tool(nil), d.writes...)
	d.mu.Unlock()
	out := make([]Tool, 0, len(routes))
	for _, r := range routes {
		for _, w := range writes {
			if r.Method == w.Method && r.Path == w.Path && r.Auth && r.TokenCallable {
				w.Permission = r.Perm
				out = append(out, w)
			}
		}
		if r.Method != "GET" || !r.Auth || !r.OpenapiCallable ||
			r.Path == APIPrefix+"/mcp" || strings.HasPrefix(r.Path, APIPrefix+"/mcp/") {
			continue
		}
		// 完整路由的摘要消除参数名、标点清洗和截断导致的同名。
		sum := sha256.Sum256([]byte(r.Method + " " + r.Path))
		name := "get_" + hex.EncodeToString(sum[:28]) // 60 个合法 ASCII 字符，稳定且不依赖注册顺序
		duplicate := false
		for _, tool := range out {
			if tool.Name == name {
				duplicate = true
				break
			}
		}
		if duplicate {
			continue
		}
		out = append(out, Tool{Name: name, Method: r.Method, Path: r.Path, Description: r.Summary, Permission: r.Perm})
	}
	return out
}

// RegisterWriteTool explicitly opts one authenticated route into MCP. Write
// routes are never inferred. Initial write tools accept an empty JSON body only.
func (d *Doc) RegisterWriteTool(name, method, path, description string, validate func(context.Context, idgen.ID, map[string]string) error) {
	if d == nil {
		return
	}
	if name == "" || validate == nil {
		panic("apidoc: MCP write tool requires a name and validator")
	}
	if method != "POST" && method != "PUT" && method != "PATCH" && method != "DELETE" {
		panic("apidoc: invalid MCP write method")
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	for _, w := range d.writes {
		if w.Name == name || (w.Method == method && w.Path == path) {
			panic("apidoc: duplicate MCP write tool")
		}
	}
	d.writes = append(d.writes, Tool{Name: name, Method: method, Path: path,
		Description: description, Write: true, ConfirmationRequired: true, Validate: validate})
}

// CoreModule 是不属于任何 Module 的框架自带路由（登录、系统状态等）的模块名。
const CoreModule = "core"

// Options 配置文档生成。
type Options struct {
	Title    string
	Version  string
	Disabled []string // disabled_modules：这些模块的路由不出现在文档里
}

type source struct {
	module string
	e      *gin.Engine
}

type snapshot struct {
	fp     uint64
	routes []Route
	spec   []byte
}

// Doc 持有主引擎与（热挂载的）子引擎，按需生成并缓存路由清单与 OpenAPI 文档。
type Doc struct {
	opts Options
	main *gin.Engine

	mu     sync.Mutex
	owners map[string]string // "METHOD path" → 模块名（主引擎）
	extra  []source
	ops    map[string]Operation // "METHOD rel-path" → 补充说明
	rev    int                  // 说明/来源变化计数，参与缓存键
	cache  *snapshot
	builds int // 重建次数（测试用）
	writes []Tool
}

// New 为主引擎创建 Doc。
func New(e *gin.Engine, opts Options) *Doc {
	if opts.Title == "" {
		opts.Title = "NexAdmin API"
	}
	if opts.Version == "" {
		opts.Version = "1.0.0"
	}
	return &Doc{opts: opts, main: e, owners: map[string]string{}, ops: map[string]Operation{}}
}

func routeKey(method, path string) string { return method + " " + path }

// Track 执行 fn（通常是 m.Register），并把 fn 期间新增的路由记到模块 name 名下；
// m 实现了 Describer 时一并收集补充说明。
func (d *Doc) Track(name string, m any, fn func()) {
	before := map[string]bool{}
	for _, r := range d.main.Routes() {
		before[routeKey(r.Method, r.Path)] = true
	}
	fn()
	d.mu.Lock()
	defer d.mu.Unlock()
	for _, r := range d.main.Routes() {
		if k := routeKey(r.Method, r.Path); !before[k] {
			d.owners[k] = name
		}
	}
	if ds, ok := m.(Describer); ok {
		d.describeLocked(ds.APIDoc()...)
	}
}

// Describe 登记补充说明（框架自带路由用它；模块优先实现 Describer）。
func (d *Doc) Describe(ops ...Operation) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.describeLocked(ops...)
}

func (d *Doc) describeLocked(ops ...Operation) {
	for _, o := range ops {
		d.ops[routeKey(strings.ToUpper(o.Method), o.Path)] = o
	}
	d.rev++
}

// AddEngine 登记热挂载插件的子引擎，其路由归入 module。
func (d *Doc) AddEngine(module string, e *gin.Engine) {
	d.mu.Lock()
	defer d.mu.Unlock()
	for i := range d.extra {
		if d.extra[i].module == module {
			d.extra[i].e = e
			d.rev++
			return
		}
	}
	d.extra = append(d.extra, source{module, e})
	d.rev++
}

// Invalidate 丢弃缓存。路由集合变化本身会让缓存自动失效，这个方法给“说明内容变了”之类的场合用。
func (d *Doc) Invalidate() {
	d.mu.Lock()
	d.cache = nil
	d.mu.Unlock()
}

// fingerprint 是当前路由集合 + 说明版本的指纹；只读 Routes()，比重建文档便宜得多。
func (d *Doc) fingerprint() uint64 {
	var keys []string
	for _, r := range d.main.Routes() {
		keys = append(keys, routeKey(r.Method, r.Path))
	}
	for _, s := range d.extra {
		for _, r := range s.e.Routes() {
			keys = append(keys, s.module+"|"+routeKey(r.Method, r.Path))
		}
	}
	sort.Strings(keys)
	h := fnv.New64a()
	for _, k := range keys {
		_, _ = h.Write([]byte(k))
		_, _ = h.Write([]byte{0})
	}
	_, _ = h.Write([]byte{byte(d.rev), byte(d.rev >> 8), byte(d.rev >> 16)})
	return h.Sum64()
}

func (d *Doc) snap() (*snapshot, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	fp := d.fingerprint()
	if d.cache != nil && d.cache.fp == fp {
		return d.cache, nil
	}
	routes := d.buildRoutes()
	spec, err := buildSpec(d.opts, routes, d.ops)
	if err != nil {
		return nil, err
	}
	d.builds++
	d.cache = &snapshot{fp: fp, routes: routes, spec: spec}
	return d.cache, nil
}

// Routes 返回扁平路由清单（已按路径、方法排序）。
func (d *Doc) Routes() []Route {
	s, err := d.snap()
	if err != nil {
		return []Route{}
	}
	return append([]Route(nil), s.routes...)
}

// JSON 返回 OpenAPI 3.0.3 文档（缓存）。
func (d *Doc) JSON() ([]byte, error) {
	s, err := d.snap()
	if err != nil {
		return nil, err
	}
	return s.spec, nil
}

func (d *Doc) buildRoutes() []Route {
	disabled := map[string]bool{}
	for _, n := range d.opts.Disabled {
		disabled[n] = true
	}
	var out []Route
	add := func(e *gin.Engine, owner func(method, path string) string) {
		chains, _ := readChains(e) // 失败只会缺 perm / 登录信息
		for _, r := range e.Routes() {
			if !strings.HasPrefix(r.Path, APIPrefix+"/") || strings.Contains(r.Path, "/*") {
				continue // 非业务接口，或休眠付费插件的 /plugins/<id>/*rest 转发入口
			}
			mod := owner(r.Method, r.Path)
			if disabled[mod] {
				continue
			}
			ci := summarize(chains[routeKey(r.Method, r.Path)])
			rel := strings.TrimPrefix(r.Path, APIPrefix)
			rt := Route{
				Method: r.Method, Path: r.Path, Module: mod, Perm: strings.Join(ci.perms, ","),
				Auth: ci.login, SuperOnly: ci.superOnly,
			}
			if rt.Auth {
				rt.TokenCallable, rt.AppCallable = openapi.TokenCallable(rel), openapi.AppCallable(rel)
				rt.OpenapiCallable = rt.TokenCallable || rt.AppCallable
			}
			if ci.summary != "" {
				rt.Summary = ci.summary
			}
			if o, ok := d.ops[routeKey(r.Method, rel)]; ok {
				rt.Summary = o.Summary
			}
			out = append(out, rt)
		}
	}
	add(d.main, func(m, p string) string {
		if n, ok := d.owners[routeKey(m, p)]; ok {
			return n
		}
		return CoreModule
	})
	for _, s := range d.extra {
		mod := s.module
		add(s.e, func(string, string) string { return mod })
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Path != out[j].Path {
			return out[i].Path < out[j].Path
		}
		return out[i].Method < out[j].Method
	})
	if out == nil {
		out = []Route{}
	}
	return out
}
