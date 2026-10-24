package core

import (
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"nexadmin/core/config"
)

type stubModule struct {
	name   string
	routes []string // 注册的 GET 路径
}

func (m stubModule) Name() string { return m.name }
func (m stubModule) Register(r *gin.RouterGroup, _ *Deps) {
	for _, p := range m.routes {
		r.GET(p, func(*gin.Context) {})
	}
}

func namesOf(t *testing.T, a *App) []string {
	t.Helper()
	mods, err := a.activeModules()
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, m := range mods {
		out = append(out, m.Name())
	}
	return out
}

// 替换内置模块：disabled_modules 停用原模块 + 提供不同名字的替代模块，二者同名则会被一起停用。
func TestActiveModulesReplaceBuiltin(t *testing.T) {
	saved := builtin
	t.Cleanup(func() { builtin = saved })
	builtin = []Module{stubModule{name: "post"}}

	a := New(&config.Config{DisabledModules: []string{"post"}})
	a.Register(stubModule{name: "my_post"})
	if got := strings.Join(namesOf(t, a), ","); got != "my_post" {
		t.Fatalf("停用 post 后应只剩 my_post, got %q", got)
	}

	same := New(&config.Config{DisabledModules: []string{"post"}})
	same.Register(stubModule{name: "post"})
	if got := namesOf(t, same); len(got) != 0 {
		t.Fatalf("同名替代模块会被一起停用, got %v", got)
	}
}

// 业务模块与内置模块重名：启动前明确报错（不是 panic）。
func TestActiveModulesDuplicateName(t *testing.T) {
	saved := builtin
	t.Cleanup(func() { builtin = saved })
	builtin = []Module{stubModule{name: "post"}}

	a := New(&config.Config{})
	a.Register(stubModule{name: "post"})
	if _, err := a.activeModules(); err == nil || !strings.Contains(err.Error(), "duplicate module name") {
		t.Fatalf("重名应报错, got %v", err)
	}
}

// 两个模块注册同一 method+path：Gin 在装配路由时 panic，所以替换内置模块必须先停用原模块。
func TestDuplicateRoutePanics(t *testing.T) {
	gin.SetMode(gin.TestMode)
	g := gin.New().Group("/api/v1")
	stubModule{name: "a", routes: []string{"/posts"}}.Register(g, nil)
	defer func() {
		if recover() == nil {
			t.Fatal("同 path 重复注册应 panic")
		}
	}()
	stubModule{name: "b", routes: []string{"/posts"}}.Register(g, nil)
}
