// Package report 是 NexAdmin 官方付费插件「报表中心 + 数据大屏」：结构化数据集（表/列白名单）、
// 图表（柱/折/饼/表格/指标卡）、报表页（筛选器联动、xlsx 导出）、画布式数据大屏（按角色授权）。
//
// 安全设计要点（详见 query.go / deny.go / sources.go）：
//   - 没有任何原始 SQL 入口：请求只能引用白名单元数据里的列，SQL 里的标识符全部来自元数据，值全部参数化；
//   - 白名单默认只有少量安全业务表，另有硬编码拒绝列表兜底，配置无法放宽；
//   - 每次动态查询自动附加租户条件，过滤条件只能 AND 追加；数据范围能复用 core/datascope 的就复用；
//   - 查询有超时和行数上限，错误不回显 SQL/库细节。
//
// 遵守插件约定：只依赖 core/ 与 pkg/，表前缀 plg_report_，权限码前缀 report:，接口 /api/v1/plugins/report/...。
package report

import (
	"io"
	"net/url"
	"strings"

	"github.com/gin-gonic/gin"

	"nexadmin/core"
	"nexadmin/core/plugin"
)

// 建表由运行时执行 plugins/report/migrations/<引擎>/*.sql，插件自己不做迁移。
func init() {
	plugin.Register(plugin.Plugin{
		ID: "report", Name: "报表中心", Version: "1.0.0", Edition: plugin.Pro,
		Module: &module{},
	})
}

type module struct{}

func (*module) Name() string { return "report" }

// Register 注册路由。r 已经是 /api/v1 且默认要求登录。
func (m *module) Register(r *gin.RouterGroup, d *core.Deps) {
	reg, set := loadRegistry(d.Config, d.Log)
	seedBuiltins(d.DB, d.Log)
	registerRoutes(r, newHandler(NewService(d.DB, d.Log, reg, set), d.DB))
}

// registerRoutes 挂载全部路由；每个接口都有 core.Perm。
func registerRoutes(r *gin.RouterGroup, h *handler) {
	g := r.Group("/plugins/report", limitBody())

	// 数据集（含白名单表元数据）
	g.GET("/sources", core.Perm("report:dataset"), h.sources)
	g.GET("/datasets", core.Perm("report:dataset"), h.listDatasets)
	g.POST("/datasets", core.Perm("report:dataset"), h.saveDataset(0))
	g.POST("/datasets/preview", core.Perm("report:dataset"), h.previewDataset)
	g.GET("/datasets/:id", core.Perm("report:dataset"), h.getDataset)
	g.PUT("/datasets/:id", core.Perm("report:dataset"), h.updateDataset)
	g.DELETE("/datasets/:id", core.Perm("report:dataset"), h.deleteDataset)

	// 图表
	g.GET("/chart-options/datasets", core.Perm("report:chart"), h.datasetOptions)
	g.GET("/charts", core.Perm("report:chart"), h.listCharts)
	g.POST("/charts", core.Perm("report:chart"), h.saveChart(0))
	g.POST("/charts/preview", core.Perm("report:chart"), h.previewChart)
	g.GET("/charts/:id", core.Perm("report:chart"), h.getChart)
	g.PUT("/charts/:id", core.Perm("report:chart"), h.updateChart)
	g.DELETE("/charts/:id", core.Perm("report:chart"), h.deleteChart)
	g.POST("/charts/:id/query", core.Perm("report:chart"), h.queryChart)

	// 报表页
	g.GET("/report-options/datasets", core.Perm("report:report"), h.datasetOptions)
	g.GET("/report-options/charts", core.Perm("report:report"), h.chartOptions)
	g.GET("/reports", core.Perm("report:report"), h.listReports)
	g.POST("/reports", core.Perm("report:report"), h.saveReport(0))
	g.GET("/reports/:id", core.Perm("report:report"), h.getReport)
	g.PUT("/reports/:id", core.Perm("report:report"), h.updateReport)
	g.DELETE("/reports/:id", core.Perm("report:report"), h.deleteReport)

	// 数据大屏（管理）
	g.GET("/screen-options/charts", core.Perm("report:screen"), h.chartOptions)
	g.GET("/screen-options/roles", core.Perm("report:screen"), h.roles)
	g.GET("/screens", core.Perm("report:screen"), h.listScreens)
	g.POST("/screens", core.Perm("report:screen"), h.saveScreen(0))
	g.GET("/screens/:id", core.Perm("report:screen"), h.getScreen)
	g.PUT("/screens/:id", core.Perm("report:screen"), h.updateScreen)
	g.DELETE("/screens/:id", core.Perm("report:screen"), h.deleteScreen)

	// 查看端
	g.GET("/overview", core.Perm("report:view"), h.overview) // 工作台卡片 report-overview
	g.GET("/view/reports", core.Perm("report:view"), h.viewReports)
	g.GET("/view/reports/:id", core.Perm("report:view"), h.viewReport)
	g.POST("/view/reports/:id/charts/:chartId/query", core.Perm("report:view"), h.viewReportQuery)
	g.POST("/view/reports/:id/charts/:chartId/export", core.Perm("report:view"), h.viewReportExport)
	g.POST("/view/reports/:id/filters/:key/options", core.Perm("report:view"), h.viewReportOptions)
	g.GET("/view/screens", core.Perm("report:view"), h.viewScreens)
	g.GET("/view/screens/:id", core.Perm("report:view"), h.viewScreen)
	g.POST("/view/screens/:id/charts/:chartId/query", core.Perm("report:view"), h.viewScreenQuery)
}

type ioWriter = io.Writer

const xlsxContentType = "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet"

func urlEscape(s string) string { return url.PathEscape(s) }

// asciiName 给 filename= 回退值用：非 ASCII 字符换成下划线（完整名字走 filename*=UTF-8”）。
func asciiName(s string) string {
	var b strings.Builder
	for _, r := range s {
		if r < 0x20 || r > 0x7e {
			b.WriteByte('_')
		} else {
			b.WriteRune(r)
		}
	}
	return b.String()
}
