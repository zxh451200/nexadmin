// Package exportcenter 是导出中心（免费内置）：异步任务（core/job）的查询、取消、重试、删除与结果下载。
// 表：sys_job（租户隔离）。登录用户只能看自己的任务；拥有 job:all 的管理员可以看到租户内全部任务。
//
// 业务模块新增一种异步任务，在自己的 Register 里一行：
//
//	job.Register("order.export", h.exportJob) // func(ctx, *job.Job, job.Report) (job.ResultFile, error)
//
// 提交：job.Default().Submit(ctx, job.Submit{Type: "order.export", Owner: uid, Params: filters})。
package exportcenter

import (
	"fmt"
	"path/filepath"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"

	"nexadmin/core"
	"nexadmin/core/auth"
	"nexadmin/core/job"
	"nexadmin/core/metrics"
	"nexadmin/core/storage"
)

func init() { core.RegisterBuiltin(module{}) }

// PermAll 是“查看 / 管理全部任务”的按钮权限点；其余接口登录即可。
const PermAll = "job:all"

type module struct{}

func (module) Name() string { return "exportcenter" }

// Register 创建并启动任务管理器，注册路由。固定路径写在 /:id 之前。
func (module) Register(r *gin.RouterGroup, d *core.Deps) {
	auth.RegisterPerm(PermAll)
	files, err := storage.New(d.Config.Storage, filepath.Join(d.Config.DataDir, "uploads", "files"))
	if err != nil {
		d.Log.Error("exportcenter disabled: storage init failed", zap.Error(err))
		return
	}
	cfg := d.Config
	m := job.New(job.Options{
		Store: job.NewGormStore(d.DB), Files: files, Log: d.Log,
		Node:    fmt.Sprintf("node-%d", cfg.Database.NodeID), // 重启恢复只处理本节点遗留的 running，多实例 node_id 必须各不相同
		Workers: cfg.Job.Workers, KeepDays: cfg.Job.KeepDays, MaxPerUser: cfg.Job.MaxPerUser,
	})
	job.SetDefault(m)
	if err := m.Start(); err != nil {
		d.Log.Error("exportcenter: job runner start failed", zap.Error(err))
	}
	metrics.SetQueueSource(m.QueueLengths) // 登记在这里而不是 core/metrics，避免 metrics import job
	if d.OnClose != nil {
		d.OnClose(func() { metrics.SetQueueSource(nil); job.SetDefault(nil); m.Stop() })
	}

	h := &handler{m: m, db: d.DB, log: d.Log}
	r.GET("/jobs", h.list)
	r.GET("/jobs/:id", h.get)
	r.POST("/jobs/:id/cancel", h.cancel)
	r.POST("/jobs/:id/retry", h.retry)
	r.GET("/jobs/:id/download", h.download)
	r.DELETE("/jobs/:id", h.remove)
}
