// Package audit 是 NexAdmin 官方付费插件「数据变更审计」：按表开启后，通过 GORM 回调记录
// 新增/修改/删除的字段级前后对比（只存变化字段，敏感字段脱敏），异步批量写入，不阻塞业务事务。
// 遵守设计文档 §19：只依赖 core/ 与 pkg/，表前缀 plg_audit_，权限码前缀 audit:，
// 接口路径 /api/v1/plugins/audit/...。
//
// 已知边界（也写在交付说明里）：
//   - 回调只覆盖走 GORM 模型/Table+Model 的写入；db.Exec/db.Raw 原生 SQL 不经过回调，不会被记录。
//   - 记录在语句执行后入队，不等事务提交；事务回滚时对应记录仍会写入。
//   - 操作人来自 core 放进 request context 的登录用户（auth.ClaimsFromContext）。模块里必须用
//     db.WithContext(c.Request.Context()) 发起写入才取得到；不带 ctx 的写入、后台任务记为「系统」，
//     业务代码可用 audit.WithOperator 补上。
package audit

import (
	"context"
	"sync"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"

	"nexadmin/core"
	"nexadmin/core/plugin"
)

// 建表由运行时执行 plugins/audit/migrations/<引擎>/*.sql（安装时 CLI 拷贝），插件自己不做迁移。
var _ plugin.Starter = (*module)(nil)

func init() {
	plugin.Register(plugin.Plugin{
		ID: "audit", Name: "数据变更审计", Version: "1.0.0", Edition: plugin.Pro,
		Module: &module{},
	})
}

type module struct {
	once sync.Once
	svc  *Service
	rec  *Recorder

	startOnce sync.Once
}

func (m *module) init(d *core.Deps) {
	m.once.Do(func() {
		scope := NewScope()
		m.rec = NewRecorder(d.DB, d.Log, scope)
		m.svc = NewService(d.DB, d.Log, scope)
	})
}

func (*module) Name() string { return "audit" }

// Register 注册路由。
func (m *module) Register(r *gin.RouterGroup, d *core.Deps) {
	m.init(d)
	r.Use(operatorMiddleware())
	h := &handler{svc: m.svc}
	g := r.Group("/plugins/audit")
	g.GET("/logs", core.Perm("audit:list"), h.listLogs)
	g.GET("/logs/:id", core.Perm("audit:list"), h.getLog)
	g.GET("/log-tables", core.Perm("audit:list"), h.logTables)
	g.GET("/config", core.Perm("audit:config"), h.getConfig)
	g.PUT("/config", core.Perm("audit:config"), h.saveConfig)
	g.DELETE("/logs", core.Perm("audit:clean"), h.clean)
}

// Start 实现 plugin.Starter：在 d.DB 上登记回调、加载审计范围、启动写入与刷新循环。非阻塞。
func (m *module) Start(ctx context.Context, d *core.Deps) error {
	m.init(d)
	var err error
	m.startOnce.Do(func() {
		if err = m.rec.Register(d.DB); err != nil {
			return
		}
		if lerr := m.rec.LoadScope(ctx); lerr != nil {
			d.Log.Warn("audit: initial scope load failed, will retry", zap.Error(lerr))
		}
		go m.rec.Run(ctx)
		go m.rec.RefreshLoop(ctx)
	})
	return err
}
