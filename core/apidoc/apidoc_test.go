package apidoc_test

import (
	"encoding/json"
	"testing"

	"github.com/gin-gonic/gin"

	"nexadmin/core"
	"nexadmin/core/apidoc"
	"nexadmin/core/middleware"
)

func noop(*gin.Context) {}

type mod struct {
	routes func(r, pub, adm *gin.RouterGroup)
}

func (mod) APIDoc() []apidoc.Operation {
	return []apidoc.Operation{{Method: "GET", Path: "/users", Summary: "用户列表", Query: []apidoc.Param{{Name: "page", Type: "integer"}}}}
}

// build 装配一个与 core/router.go 同形的引擎：Public 不要求登录，protected 要求登录，admin 仅超管。
func build(t *testing.T, disabled ...string) (*gin.Engine, *apidoc.Doc, *gin.RouterGroup, *gin.RouterGroup) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	e := gin.New()
	api := e.Group("/api/v1")
	doc := apidoc.New(e, apidoc.Options{Disabled: disabled})
	api.POST("/auth/login", noop)
	api.POST("/auth/logout", middleware.RequireLogin(), noop)
	e.GET("/healthz", noop)
	prot := api.Group("", middleware.RequireLogin())
	adm := api.Group("/admin", middleware.RequireSuper())
	doc.Track("user", mod{}, func() {
		prot.GET("/users", core.Perm("user:list"), noop)
		prot.GET("/users/:id", core.Perm("user:list"), noop)
		prot.PUT("/users/:id/status", core.Perm("user:update"), noop)
	})
	doc.Track("openapi", nil, func() {
		prot.GET("/profile/tokens", noop)
		prot.GET("/openapi/apps", core.Perm("openapi:list"), noop)
	})
	doc.Track("tenant", nil, func() { adm.GET("/tenants", noop) })
	return e, doc, prot, adm
}

func find(rs []apidoc.Route, method, path string) (apidoc.Route, bool) {
	for _, r := range rs {
		if r.Method == method && r.Path == path {
			return r, true
		}
	}
	return apidoc.Route{}, false
}

func TestRoutesFlags(t *testing.T) {
	_, doc, _, _ := build(t)
	rs := doc.Routes()
	if _, ok := find(rs, "GET", "/healthz"); ok {
		t.Fatal("non /api/v1 route must be excluded")
	}
	login, _ := find(rs, "POST", "/api/v1/auth/login")
	if login.Auth || login.OpenapiCallable || login.Module != apidoc.CoreModule {
		t.Fatalf("login: %+v", login)
	}
	u, _ := find(rs, "GET", "/api/v1/users/:id")
	if !u.Auth || u.Perm != "user:list" || u.Module != "user" || !u.TokenCallable || !u.AppCallable || !u.OpenapiCallable || u.SuperOnly {
		t.Fatalf("users/:id: %+v", u)
	}
	ten, _ := find(rs, "GET", "/api/v1/admin/tenants")
	if !ten.Auth || !ten.SuperOnly || ten.Module != "tenant" {
		t.Fatalf("admin: %+v", ten)
	}
	if lo, _ := find(rs, "POST", "/api/v1/auth/logout"); !lo.Auth || lo.Perm != "" {
		t.Fatalf("logout: %+v", lo)
	}
}

func TestOpenapiCallableRules(t *testing.T) {
	_, doc, _, _ := build(t)
	rs := doc.Routes()
	tok, _ := find(rs, "GET", "/api/v1/profile/tokens")
	if tok.TokenCallable || tok.AppCallable || tok.OpenapiCallable {
		t.Fatalf("profile/tokens: %+v", tok)
	}
	apps, _ := find(rs, "GET", "/api/v1/openapi/apps")
	if !apps.TokenCallable || apps.AppCallable || !apps.OpenapiCallable {
		t.Fatalf("openapi/apps: token ok, app forbidden: %+v", apps)
	}
	lo, _ := find(rs, "POST", "/api/v1/auth/logout")
	if lo.TokenCallable || lo.AppCallable {
		t.Fatalf("/auth/* forbidden for both: %+v", lo)
	}
}

func spec(t *testing.T, doc *apidoc.Doc) map[string]any {
	t.Helper()
	b, err := doc.JSON()
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatal(err)
	}
	return m
}

func op(t *testing.T, m map[string]any, path, method string) map[string]any {
	t.Helper()
	o, ok := m["paths"].(map[string]any)[path].(map[string]any)[method].(map[string]any)
	if !ok {
		t.Fatalf("missing %s %s", method, path)
	}
	return o
}

func TestSpecConversion(t *testing.T) {
	_, doc, _, _ := build(t)
	m := spec(t, doc)
	if m["openapi"] != "3.0.3" {
		t.Fatalf("version %v", m["openapi"])
	}
	get := op(t, m, "/users/{id}", "get")
	if get["operationId"] != "getUsersById" || get["tags"].([]any)[0] != "user" {
		t.Fatalf("op: %v", get)
	}
	ps := get["parameters"].([]any)
	if len(ps) != 1 || ps[0].(map[string]any)["name"] != "id" || ps[0].(map[string]any)["in"] != "path" {
		t.Fatalf("params: %v", ps)
	}
	if sec := get["security"].([]any); len(sec) != 2 {
		t.Fatalf("security: %v", sec)
	}
	if _, ok := get["responses"].(map[string]any)["401"]; !ok {
		t.Fatal("auth route needs 401")
	}
	if op(t, m, "/users/{id}/status", "put")["operationId"] != "putUsersByIdStatus" {
		t.Fatal("operationId")
	}
	// 公开接口：security 为空数组，无 401/403；没有 Describer 的接口不编造请求体
	login := op(t, m, "/auth/login", "post")
	if len(login["security"].([]any)) != 0 {
		t.Fatalf("public security: %v", login["security"])
	}
	if _, ok := login["responses"].(map[string]any)["401"]; ok {
		t.Fatal("public route must not list 401")
	}
	if _, ok := op(t, m, "/users/{id}/status", "put")["requestBody"]; ok {
		t.Fatal("requestBody must not be fabricated")
	}
	// token-only 接口不带应用签名
	if sec := op(t, m, "/openapi/apps", "get")["security"].([]any); len(sec) != 1 {
		t.Fatalf("app-forbidden security: %v", sec)
	}
	// Describer 补充
	list := op(t, m, "/users", "get")
	if list["summary"] != "用户列表" || list["parameters"].([]any)[0].(map[string]any)["name"] != "page" {
		t.Fatalf("describer: %v", list)
	}
	if op(t, m, "/admin/tenants", "get")["x-super-only"] != true {
		t.Fatal("super only flag")
	}
	tags := m["tags"].([]any)
	if len(tags) != 4 {
		t.Fatalf("tags: %v", tags)
	}
}

func TestCacheInvalidation(t *testing.T) {
	_, doc, prot, _ := build(t)
	a, _ := doc.JSON()
	b, _ := doc.JSON()
	if &a[0] != &b[0] {
		t.Fatal("second call should hit the cache")
	}
	// 路由集合变化（如插件热挂载）后缓存自动失效
	doc.Track("plugin", nil, func() { prot.GET("/demo/hello", noop) })
	c, _ := doc.JSON()
	if string(c) == string(a) {
		t.Fatal("cache not invalidated after new route")
	}
	if _, ok := spec(t, doc)["paths"].(map[string]any)["/demo/hello"]; !ok {
		t.Fatal("new route missing")
	}
	// 子引擎热挂载
	sub := gin.New()
	sub.GET("/api/v1/plug/x", middleware.RequireLogin(), noop)
	doc.AddEngine("plug", sub)
	if r, ok := find(doc.Routes(), "GET", "/api/v1/plug/x"); !ok || r.Module != "plug" {
		t.Fatalf("sub engine route: %+v %v", r, ok)
	}
	d1, _ := doc.JSON()
	doc.Invalidate()
	d2, _ := doc.JSON()
	if string(d1) != string(d2) {
		t.Fatal("rebuild must be deterministic")
	}
}

func TestDisabledModuleAndCatchAll(t *testing.T) {
	_, doc, prot, _ := build(t, "user")
	prot.Any("/plugins/pro/*rest", noop)
	for _, r := range doc.Routes() {
		if r.Module == "user" {
			t.Fatalf("disabled module leaked: %+v", r)
		}
		if r.Path == "/api/v1/plugins/pro/*rest" {
			t.Fatal("dormant plugin catch-all must be hidden")
		}
	}
	if _, ok := spec(t, doc)["paths"].(map[string]any)["/users"]; ok {
		t.Fatal("disabled module in spec")
	}
}
