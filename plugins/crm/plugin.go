// Package crm 是免费基础客户主数据插件（nex 生态收敛方案第 3 步的最小骨架）：
// 客户主数据 + 线索商机两张表 + 事件契约，让 hotel/mall 的 member 认同同一个
// 客户 ID。只做主数据与商机台账，不做销售自动化（无管线/评分/营销）。
// 遵守插件规范：只依赖 core/ 与 pkg/，表前缀 plg_crm_，权限码前缀 crm:，
// 接口路径 /api/v1/plugins/crm/...。
package crm

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
		ID: "crm", Name: "客户主数据", Version: "0.1.0", Edition: plugin.Free,
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

func (*module) Name() string { return "crm" }

// Register 注册管理端路由。
func (m *module) Register(r *gin.RouterGroup, d *core.Deps) {
	h := &handler{svc: m.service(d)}

	g := r.Group("/plugins/crm")
	g.GET("/customers", core.Perm("crm:list"), h.listCustomers)
	g.GET("/customers/:id", core.Perm("crm:list"), h.getCustomer)
	g.POST("/customers", core.Perm("crm:edit"), h.createCustomer)
	g.PUT("/customers/:id", core.Perm("crm:edit"), h.updateCustomer)
	g.DELETE("/customers/:id", core.Perm("crm:edit"), h.deleteCustomer)
	g.POST("/customers/bind-external", core.Perm("crm:edit"), h.bindExternal)
	g.GET("/customers/resolve-external", core.Perm("crm:list"), h.resolveExternal)
	g.GET("/opportunities", core.Perm("crm:list"), h.listOpportunities)
	g.GET("/opportunities/:id", core.Perm("crm:list"), h.getOpportunity)
	g.POST("/opportunities", core.Perm("crm:opp"), h.createOpportunity)
	g.PUT("/opportunities/:id", core.Perm("crm:opp"), h.updateOpportunity)
	g.DELETE("/opportunities/:id", core.Perm("crm:opp"), h.deleteOpportunity)
}

// Start 实现 plugin.Starter：crm 无后台任务，保留扩展点。
func (m *module) Start(ctx context.Context, d *core.Deps) error {
	m.service(d)
	return nil
}
