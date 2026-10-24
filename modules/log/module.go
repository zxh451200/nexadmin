package log

import (
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"nexadmin/core"
	"nexadmin/core/auth"
	"nexadmin/core/middleware"
)

func init() { core.RegisterBuiltin(module{}) }

// 日志模块：操作日志（sys_oper_log）与登录日志（sys_login_log）。
type module struct{}

func (module) Name() string { return "log" }

// Seed 种入演示日志（setup.Seeder）。
func (module) Seed(db *gorm.DB) error {
	if err := seedOper(db); err != nil {
		return err
	}
	return seedLogin(db)
}

// Register 注册路由，并设置全局 Default() 供其他模块写日志。
// 导出走服务端接口 GET /oper-logs/export、/login-logs/export（xlsx 流式写出），强制 operLog:export / loginLog:export，
// 复用列表的筛选与数据范围，行数受 log.export_max_rows 限制，按用户限流（ratelimit.log_export），并记一条 type=export 的操作日志。
// 列表接口的 pageSize 上限（5000）只服务于页面分页，不再承担导出。
func (module) Register(r *gin.RouterGroup, d *core.Deps) {
	svc := NewService(d.DB, d.Config.App.Timezone, d.Log).WithExportMaxRows(d.Config.Log.ExportMaxRows)
	defaultSvc.Store(svc)
	h := &Handler{svc: svc}
	auth.RegisterPerm("operLog:export")
	auth.RegisterPerm("loginLog:export")

	exportLimit := middleware.RateLimitRule(d.Cache, d.Log, d.Config.RateLimit, "log_export", middleware.ByUser())

	r.GET("/oper-logs", core.Perm("operLog:list"), h.ListOper)
	r.GET("/oper-logs/export", core.Perm("operLog:export"), exportLimit, h.ExportOper)
	r.DELETE("/oper-logs", core.Perm("operLog:delete"), h.DeleteOper)
	r.POST("/oper-logs/clean", core.Perm("operLog:delete"), h.CleanOper)

	r.GET("/login-logs", core.Perm("loginLog:list"), h.ListLogin)
	r.GET("/login-logs/export", core.Perm("loginLog:export"), exportLimit, h.ExportLogin)
	r.DELETE("/login-logs", core.Perm("loginLog:delete"), h.DeleteLogin)
}
