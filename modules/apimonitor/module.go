// Package apimonitor 是接口监控：最近 30 秒各接口的 QPS、平均耗时、错误率（HTTP 状态码 >= 400 计为错误）。
// 数据来自 pkg/apistat 的进程内统计，重启清零。
package apimonitor

import (
	"time"

	"github.com/gin-gonic/gin"

	"nexadmin/core"
	"nexadmin/core/response"
	"nexadmin/modules/internal/monitorsnap"
	"nexadmin/pkg/apistat"
)

func init() { core.RegisterBuiltin(module{}) }

// PermList 与前端 route.tsx 一致。
const PermList = "apiMonitor:list"

type module struct{}

func (module) Name() string { return "api-monitor" }

// snapshot 与前端 ApiSnapshot 一致。
type snapshot struct {
	TS        int64          `json:"ts"`
	QPS       float64        `json:"qps"`
	AvgMs     float64        `json:"avgMs"`
	ErrorRate float64        `json:"errorRate"`
	APIs      []apistat.Stat `json:"apis"`
}

// Register 挂上统计中间件：Gin 的 Use 只对其后注册的路由生效，
// 所以排在本模块之前注册的路由（含 core 的 /auth/*）不在统计内；要全覆盖需要 core 在引擎上挂 apistat.Middleware。
func (module) Register(r *gin.RouterGroup, d *core.Deps) {
	apistat.Install(r, d.Public)
	store := monitorsnap.NewStore(d.DB)
	stop := make(chan struct{})
	if d.OnClose != nil {
		d.OnClose(func() { close(stop) })
	}
	go monitorsnap.Loop(stop, store, monitorsnap.KindAPI, func() monitorsnap.Metrics {
		s := apistat.Default.Snapshot()
		return monitorsnap.Metrics{"qps": s.QPS, "avgMs": s.AvgMs, "errorRate": s.ErrorRate}
	}, d.Log)
	r.GET("/monitor/api", core.Perm(PermList), func(c *gin.Context) {
		s := apistat.Default.Snapshot()
		if s.APIs == nil {
			s.APIs = []apistat.Stat{}
		}
		response.OK(c, snapshot{TS: time.Now().UnixMilli(), QPS: s.QPS, AvgMs: s.AvgMs, ErrorRate: s.ErrorRate, APIs: s.APIs})
	})
	r.GET("/monitor/api/history", core.Perm(PermList), monitorsnap.HandleHistory(store, monitorsnap.KindAPI))
}
