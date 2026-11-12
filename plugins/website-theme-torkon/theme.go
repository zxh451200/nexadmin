// Package websitetorkon 是官网主题插件：TORKON 工业工具（深红黑工业风）。
// 纯资源插件：模板与静态资源全部 embed，init() 里向 website 注册主题与演示内容，
// 没有自己的路由、菜单与迁移。安装后需与 website 插件配合，官网设置里切换主题。
package websitetorkon

import (
	"context"
	"embed"
	"encoding/json"
	"log"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"

	"nexadmin/core"
	"nexadmin/core/plugin"
	"nexadmin/plugins/website"
)

//go:embed tpl static demo
var contentFS embed.FS

// loadDemo 读出该品牌的演示内容（产品/新闻/导航/设置覆盖），JSON 结构即 website.DemoData。
func loadDemo() website.DemoData {
	var data website.DemoData
	b, err := contentFS.ReadFile("demo/torkon.json")
	if err != nil {
		// 演示数据是编译期 embed 的，走到这里只可能是构建坏了：直接炸出来，不要静默降级。
		log.Fatalf("website-theme-torkon: demo data missing: %v", err)
	}
	if err := json.Unmarshal(b, &data); err != nil {
		log.Fatalf("website-theme-torkon: demo data invalid: %v", err)
	}
	return data
}

func seedDemo(ctx context.Context, s *website.Service, lg *zap.Logger) (website.DemoReport, error) {
	return website.SeedDemo(ctx, s, "torkon", loadDemo(), lg)
}

type module struct{}

func (*module) Name() string { return "website-theme-torkon" }

// Register 无管理端路由：主题的一切交互都发生在 website 插件的设置页。
func (*module) Register(*gin.RouterGroup, *core.Deps) {}

func init() {
	if err := website.RegisterTheme("torkon", "TORKON 工业工具", "1.0.0", contentFS, seedDemo); err != nil {
		// 模板坏了必须立刻暴露（编译期就能修），静默降级会让公开站悄悄退回默认主题。
		panic(err)
	}
	plugin.Register(plugin.Plugin{
		ID: "website-theme-torkon", Name: "官网主题 · TORKON 工业工具",
		Version: "1.0.0", Edition: plugin.Free, Module: &module{},
	})
}
