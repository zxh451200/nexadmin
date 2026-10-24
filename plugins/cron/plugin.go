// Package cron 是 NexAdmin 官方示例付费插件「定时任务」：任务 CRUD、5 段 cron 调度、
// 立即执行、失败重试、执行日志。遵守设计文档 §19：只依赖 core/ 与 pkg/，
// 表前缀 plg_cron_，权限码前缀 cron:，接口路径 /api/v1/plugins/cron/...。
package cron

import (
	"context"
	"sync"

	"github.com/gin-gonic/gin"

	"nexadmin/core"
	"nexadmin/core/plugin"
)

// 建表由运行时执行 plugins/cron/migrations/<引擎>/*.sql（安装时 CLI 拷贝），插件自己不做迁移。
var _ plugin.Starter = (*module)(nil)

func init() {
	plugin.Register(plugin.Plugin{
		ID: "cron", Name: "定时任务", Version: "1.0.0", Edition: plugin.Pro,
		Module: &module{},
	})
}

type module struct {
	once sync.Once
	svc  *Service
}

func (m *module) service(d *core.Deps) *Service {
	m.once.Do(func() { m.svc = NewService(d.DB, d.Log) })
	return m.svc
}

func (*module) Name() string { return "cron" }

// Register 注册路由。
func (m *module) Register(r *gin.RouterGroup, d *core.Deps) {
	h := &handler{svc: m.service(d)}
	g := r.Group("/plugins/cron")
	g.GET("/handlers", core.Perm("cron:list"), h.handlers)
	g.GET("/stats", core.Perm("cron:list"), h.stats) // 工作台卡片 cron-overview
	g.GET("/jobs", core.Perm("cron:list"), h.listJobs)
	g.POST("/jobs", core.Perm("cron:edit"), h.createJob)
	g.PUT("/jobs/:id", core.Perm("cron:edit"), h.updateJob)
	g.PUT("/jobs/:id/enabled", core.Perm("cron:edit"), h.setEnabled)
	g.DELETE("/jobs/:id", core.Perm("cron:edit"), h.deleteJob)
	g.POST("/jobs/:id/run", core.Perm("cron:run"), h.runJob)
	g.GET("/logs", core.Perm("cron:log"), h.listLogs)
	g.GET("/logs/:id", core.Perm("cron:log"), h.getLog)
	g.DELETE("/logs", core.Perm("cron:clean"), h.cleanLogs)
}

// Start 实现 plugin.Starter：启动调度循环。非阻塞，ctx 结束时调度循环退出。
func (m *module) Start(ctx context.Context, d *core.Deps) error {
	s := m.service(d)
	go s.Run(ctx)
	return nil
}
