// Package aigateway 是 AI 模型网关插件（收敛方案第 4 步，从 nexRag ragmodel 剥离）：
// 模型密钥集中（加密存储、读侧打码）、多供应商路由（kind → 默认启用模型解析）、
// 按租户 token 用量台账与事件。引擎适配（RAG 检索、流式对话）留在各应用侧，
// 应用通过 client.go 取解密后的调用配置自组请求。
// 遵守插件规范：只依赖 core/ 与 pkg/，表前缀 plg_aigateway_，权限码前缀 aigateway:，
// 接口路径 /api/v1/plugins/aigateway/...。
package aigateway

import (
	"context"
	"sync"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"

	"nexadmin/core"
	"nexadmin/core/event"
	"nexadmin/core/plugin"
)

var _ plugin.Starter = (*module)(nil)

func init() {
	event.Register(CatalogInfos()...)
	plugin.Register(plugin.Plugin{
		ID: "aigateway", Name: "AI 网关", Version: "0.1.0", Edition: plugin.Pro,
		Module: &module{},
	})
}

// zapLog 把 *zap.Logger 适配成 Service 需要的最小日志面。
type zapLog struct{ l *zap.Logger }

func (z zapLog) Info(msg string, kv ...any)  { z.l.Sugar().Infow(msg, kv...) }
func (z zapLog) Warn(msg string, kv ...any)  { z.l.Sugar().Warnw(msg, kv...) }
func (z zapLog) Error(msg string, kv ...any) { z.l.Sugar().Errorw(msg, kv...) }

type module struct {
	once sync.Once
	svc  *Service
}

func (m *module) service(d *core.Deps) *Service {
	m.once.Do(func() {
		l := d.Log
		if l == nil {
			l = zap.NewNop()
		}
		m.svc = NewService(d.DB, zapLog{l})
		SetDefault(m.svc)
	})
	return m.svc
}

func (*module) Name() string { return "aigateway" }

// Register 注册管理端路由。
func (m *module) Register(r *gin.RouterGroup, d *core.Deps) {
	h := &handler{svc: m.service(d)}

	g := r.Group("/plugins/aigateway")
	g.GET("/models", core.Perm("aigateway:list"), h.listModels)
	g.GET("/models/:id", core.Perm("aigateway:list"), h.getModel)
	g.POST("/models", core.Perm("aigateway:edit"), h.saveModel)
	g.POST("/models/:id", core.Perm("aigateway:edit"), h.saveModel)
	g.DELETE("/models/:id", core.Perm("aigateway:edit"), h.deleteModel)
	g.POST("/models/:id/ping", core.Perm("aigateway:edit"), h.pingModel)
	g.GET("/usage", core.Perm("aigateway:list"), h.listUsage)
	g.GET("/usage/summary", core.Perm("aigateway:list"), h.usageSummary)
}

// Start 实现 plugin.Starter：aigateway 无后台任务，保留扩展点。
func (m *module) Start(ctx context.Context, d *core.Deps) error {
	m.service(d)
	return nil
}
