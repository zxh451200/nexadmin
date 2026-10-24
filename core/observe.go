package core

import (
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"nexadmin/core/database"
	"nexadmin/core/metrics"
)

// registerMetricsRoute 在根路径挂 /metrics（不在 /api/v1 下，不走登录）。访问控制见 metrics.Handler：
// 未配置 metrics.token 时只允许回环地址；配置后必须带 Bearer token。metrics.enabled=false 时不注册。
func (a *App) registerMetricsRoute(e *gin.Engine) {
	if !a.cfg.Metrics.Enabled {
		return
	}
	e.GET("/metrics", metrics.Handler(metrics.Options{Token: a.cfg.Metrics.Token}))
}

// registerMetricSources 在完整模式下接入依赖数据库的指标来源：连接池与待执行迁移数。
// 异步任务队列与插件授权由各自的包通过 metrics.SetQueueSource / SetPluginSource 登记，core 不 import 它们。
func (a *App) registerMetricSources(db *gorm.DB) {
	if sqlDB, err := database.Platform(db).DB(); err == nil {
		metrics.SetDBStats(sqlDB.Stats)
	}
	metrics.SetMigrationSource(func() (int, error) {
		rn, err := a.migrator(db)
		if err != nil {
			return 0, err
		}
		items, err := rn.Plan()
		if err != nil {
			return 0, err
		}
		n := len(items)
		runners, _, err := a.tenantMigrators(db)
		if err != nil {
			return 0, err
		}
		for _, tr := range runners {
			items, err := tr.Plan()
			if err != nil {
				return 0, err
			}
			n += len(items)
		}
		return n, nil
	})
}
