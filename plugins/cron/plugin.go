// Package cron 是 NexAdmin 官方免费插件「定时任务」：任务 CRUD、5 段 cron 调度、
// 立即执行、失败重试、执行日志。遵守设计文档 §19：只依赖 core/ 与 pkg/，
// 表前缀 plg_cron_，权限码前缀 cron:，接口路径 /api/v1/plugins/cron/...。
package cron

import (
	"context"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"

	"nexadmin/core"
	"nexadmin/core/job"
	"nexadmin/core/plugin"
	"nexadmin/core/tenant"
)

// 建表由运行时执行 plugins/cron/migrations/<引擎>/*.sql（安装时 CLI 拷贝），插件自己不做迁移。
var _ plugin.Starter = (*module)(nil)

func init() {
	plugin.Register(plugin.Plugin{
		ID: "cron", Name: "定时任务", Version: "1.0.0", Edition: plugin.Free,
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
	svc := m.service(d)
	job.Register(jobTypeLogExport, svc.runLogExportJob) // 导出中心任务处理器
	h := &handler{svc: svc}
	g := r.Group("/plugins/cron")
	g.GET("/handlers", core.Perm("cron:list"), h.handlers)
	g.GET("/stats", core.Perm("cron:list"), h.stats) // 工作台卡片 cron-overview
	g.GET("/jobs", core.Perm("cron:list"), h.listJobs)
	g.POST("/jobs", core.Perm("cron:edit"), h.createJob)
	g.PUT("/jobs/:id", core.Perm("cron:edit"), h.updateJob)
	g.PUT("/jobs/:id/enabled", core.Perm("cron:edit"), h.setEnabled)
	// 批量启停：静态段 /jobs/enabled 先于 :id 注册；复用单条启停的 cron:edit，不新增权限点。
	g.POST("/jobs/enabled", core.Perm("cron:edit"), d.OperLogMiddleware("cron", "update"), h.setJobsEnabled)
	g.DELETE("/jobs/:id", core.Perm("cron:edit"), h.deleteJob)
	// 批量删：复用单条删除的 cron:edit，不新增权限点
	g.DELETE("/jobs", core.Perm("cron:edit"), h.deleteJobs)
	g.POST("/jobs/:id/run", core.Perm("cron:run"), h.runJob)
	g.GET("/logs", core.Perm("cron:log"), h.listLogs)
	g.POST("/logs/export-async", core.Perm("cron:log"), core.Desc("导出执行日志"), h.exportLogsAsync)
	g.GET("/logs/:id", core.Perm("cron:log"), h.getLog)
	g.DELETE("/logs", core.Perm("cron:clean"), h.cleanLogs)
}

// logRetentionDays 是执行日志的默认保留期：高频任务一天就能写上千行，
// 没有清理入口的话 plg_cron_log 会无界增长。手动清理接口（cron:clean）仍可用，
// 想更短/更长时改这个常量即可（需要更细的运营配置时再升级成设置项）。
const logRetentionDays = 90

// Start 实现 plugin.Starter：启动调度循环、失败告警与日志保留清扫。
// 非阻塞，ctx 结束时调度循环退出。
func (m *module) Start(ctx context.Context, d *core.Deps) error {
	s := m.service(d)
	s.EnableAlerts(ctx, d.Notify)
	go s.Run(ctx)
	go sweepLogsLoop(ctx, s)
	return nil
}

// sweepLogsLoop 每天清一次超过保留期的执行日志（数据范围跳过：日志按租户隔离，逐租户删）。
func sweepLogsLoop(ctx context.Context, s *Service) {
	ticker := time.NewTicker(24 * time.Hour)
	defer ticker.Stop()
	sweepOnce := func() {
		cutoff := time.Now().UTC().AddDate(0, 0, -logRetentionDays)
		res := s.db.WithContext(tenant.Skip(ctx)).Where("started_at < ?", cutoff).Delete(&Log{})
		if res.Error != nil {
			s.log.Warn("cron: log retention sweep failed", zap.String("err", res.Error.Error()))
			return
		}
		if res.RowsAffected > 0 {
			s.log.Info("cron: log retention swept", zap.Int64("rows", res.RowsAffected))
		}
	}
	sweepOnce()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			sweepOnce()
		}
	}
}
