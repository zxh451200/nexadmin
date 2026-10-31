package log

import (
	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
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
	svc.adoptAsDefault()
	// 操作日志队列随模块注册启动，并登记优雅关闭：core 的 closers 逆序执行，本钩子晚于
	// 连接池登记，所以排空时连接池还在。OnClose 为 nil（手工装配）时不启动，沿用直写。
	if d.OnClose != nil {
		svc.startOperQueue()
		d.OnClose(func() {
			if n := svc.stopOperQueue(); n > 0 {
				d.Log.Warn("log: oper log records still queued at shutdown", zap.Int("records", n))
			}
		})
	}
	// 注入 core 的操作日志提供方：业务模块用 d.OperLogMiddleware(module, action) 挂中间件，
	// 无需关心本模块的注册时序，也不需要 import 本包（见 core/operlog.go）。
	core.SetOperationLogProvider(operLogProvider{svc: svc})
	h := &Handler{svc: svc}
	auth.RegisterPerm("operLog:export")
	auth.RegisterPerm("loginLog:export")

	exportLimit := middleware.RateLimitRule(d.Cache, d.Log, d.Config.RateLimit, "log_export", middleware.ByUser())

	r.GET("/oper-logs", core.Perm("operLog:list"), h.ListOper)
	r.GET("/oper-logs/stats", core.Perm("operLog:list"), h.StatsOper)
	r.GET("/oper-logs/export", core.Perm("operLog:export"), exportLimit, h.ExportOper)
	r.DELETE("/oper-logs", core.Perm("operLog:delete"), h.DeleteOper)
	r.POST("/oper-logs/clean", core.Perm("operLog:delete"), h.CleanOper)

	r.GET("/login-logs", core.Perm("loginLog:list"), h.ListLogin)
	r.GET("/login-logs/stats", core.Perm("loginLog:list"), h.StatsLogin)
	r.GET("/login-logs/export", core.Perm("loginLog:export"), exportLimit, h.ExportLogin)
	r.DELETE("/login-logs", core.Perm("loginLog:delete"), h.DeleteLogin)
}
