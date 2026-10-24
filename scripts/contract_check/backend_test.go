package main

import (
	"os"
	"path/filepath"
	"testing"
)

// writeTree 在临时目录里按 相对路径 -> 内容 写出 fixture，并补一个最小 go.mod。
func writeTree(t *testing.T, files map[string]string) string {
	t.Helper()
	root := t.TempDir()
	files["go.mod"] = "module fixture\n\ngo 1.25\n"
	for rel, body := range files {
		p := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

const goFixture = `package demo

import "github.com/gin-gonic/gin"

const base = "/things"

func Register(r *gin.RouterGroup, d *Deps) {
	g := r.Group(base)
	g.GET("", core.Perm("thing:list"), list)
	g.GET("/:id", get)
	g.PUT("/{id}/state", core.Perm("thing:edit"), put)
	sub := g.Group("/:id/parts")
	sub.DELETE("/:pid", del)
	d.Public.POST("/pub/hello", hello)
	d.Admin.GET("/ops", ops)
	for _, id := range ids {
		r.POST("/plugins/"+id+"/license", lic)
	}
	r.GET("/files/*rest", star)
}
`

func routeSet(rs []Route) map[string]Route {
	m := map[string]Route{}
	for _, r := range rs {
		m[r.Method+" "+r.Path] = r
	}
	return m
}

func TestBackendGroupChain(t *testing.T) {
	root := writeTree(t, map[string]string{"modules/demo/module.go": goFixture})
	be := scanBackend(root)
	got := routeSet(be.Routes)
	for _, want := range []string{
		"GET /api/v1/things", "GET /api/v1/things/:id", "PUT /api/v1/things/:id/state",
		"DELETE /api/v1/things/:id/parts/:pid", "POST /api/v1/pub/hello", "GET /api/v1/admin/ops",
	} {
		if _, ok := got[want]; !ok {
			t.Errorf("缺少路由 %q，实际: %v", want, keysOf(got))
		}
	}
	if r := got["GET /api/v1/things"]; r.Perm != "thing:list" || r.Module != "demo" {
		t.Errorf("权限码/模块不对: %+v", r)
	}
	dyn := got["POST /api/v1/plugins/?/license"]
	if !dyn.Dynamic {
		t.Errorf("动态拼接应标 Dynamic 并带 ?: %+v", keysOf(got))
	}
	if !got["GET /api/v1/files/*rest"].Dynamic {
		t.Error("通配路由应标 Dynamic")
	}
	if len(be.Unres) < 2 {
		t.Errorf("动态/通配路由必须列入无法解析清单: %+v", be.Unres)
	}
	if len(be.Perms) != 2 {
		t.Errorf("权限码应有 2 个: %v", be.Perms)
	}
}

func keysOf(m map[string]Route) []string { return sortedKeys(m) }

func TestPlaceholderNormalization(t *testing.T) {
	if a, b := cleanPath("/x/{id}/y/"), cleanPath("//x/:id/y"); a != b || a != "/x/:id/y" {
		t.Errorf("规范化不一致: %q %q", a, b)
	}
	if pathKey("/x/:id") != pathKey("/x/:name") {
		t.Error("占位名不同也应视为同一占位")
	}
	if matchPath("/a/:param", "/a/:id") != matchStrong || matchPath("/a/export", "/a/:id") != matchWeak || matchPath("/a/b", "/a/c") != matchNone {
		t.Error("matchPath 强弱判定不对")
	}
}
