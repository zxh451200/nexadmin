package imports

import (
	"context"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"nexadmin/core"
)

// fakeProvider 模拟业务模块：只实现 SourceProvider，不调用 RegisterSource。
type fakeProvider struct {
	name    string
	sources []Source
}

func (f fakeProvider) Name() string                          { return f.name }
func (f fakeProvider) Register(*gin.RouterGroup, *core.Deps) {}
func (f fakeProvider) ImportSources(*gorm.DB) []Source       { return f.sources }

// plainModule 没有实现 SourceProvider，不应贡献任何源。
type plainModule struct{}

func (plainModule) Name() string                          { return "plain" }
func (plainModule) Register(*gin.RouterGroup, *core.Deps) {}

func TestCollectRegistersProviderSources(t *testing.T) {
	called := 0
	var warned []string
	mods := []core.Module{
		fakeProvider{name: "collect-a", sources: []Source{{
			Name: "collect-a.rows", Title: "A",
			Columns: []Column{{Key: "k", Label: "K", Required: true}},
			Handler: func(context.Context, int, map[string]string) error { called++; return nil },
		}}},
		plainModule{},
	}
	collect(mods, nil, func(msg string, err error) { warned = append(warned, msg+": "+err.Error()) })

	src, err := getSource("collect-a.rows")
	if err != nil {
		t.Fatalf("来源未被自动收集: %v", err)
	}
	if err := src.Handler(context.Background(), 1, map[string]string{"k": "v"}); err != nil || called != 1 {
		t.Fatalf("收集到的处理器不可用: err=%v called=%d", err, called)
	}
	if len(warned) != 0 {
		t.Fatalf("不该有警告: %v", warned)
	}
}

// 同名来源重复收集（例如模块既自己注册又实现了接口）只应警告，不应 panic 或覆盖。
func TestCollectWarnsOnDuplicate(t *testing.T) {
	src := Source{
		Name: "collect-dup.rows", Title: "Dup",
		Columns: []Column{{Key: "k", Label: "K"}},
		Handler: func(context.Context, int, map[string]string) error { return nil },
	}
	if err := RegisterSource(src); err != nil {
		t.Fatal(err)
	}
	var warned []string
	collect([]core.Module{fakeProvider{name: "collect-dup", sources: []Source{src}}}, nil, func(msg string, err error) {
		warned = append(warned, msg+"|"+err.Error())
	})
	if len(warned) != 1 || !strings.Contains(warned[0], "collect-dup") {
		t.Fatalf("重复注册应记一条带模块名的警告，实际: %v", warned)
	}
}
