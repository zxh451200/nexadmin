// Package formbuilder 是 NexAdmin 官方付费插件「表单构建器」：可视化设计表单、发布不可变版本、
// 登录用户填写、服务端按发布版本重新校验、收集与导出（xlsx）。
// 遵守插件约定：只依赖 core/ 与 pkg/，表前缀 plg_formbuilder_，权限码前缀 formbuilder:，
// 接口路径 /api/v1/plugins/formbuilder/...。
//
// 有意的取舍：不提供匿名公开填写。所有写入口都要求登录并通过 formbuilder:fill 权限与表单可见范围，
// 这样不会引入未登录的写入面（垃圾提交、容量攻击、绕过幂等/次数限制）。
package formbuilder

import (
	"sync"

	"github.com/gin-gonic/gin"

	"nexadmin/core"
	"nexadmin/core/job"
	"nexadmin/core/plugin"
)

// 建表由运行时执行 plugins/formbuilder/migrations/<引擎>/*.sql（安装时 CLI 拷贝），插件自己不做迁移。
func init() {
	plugin.Register(plugin.Plugin{
		ID: "formbuilder", Name: "表单构建器", Version: "1.0.0", Edition: plugin.Pro,
		Module: &module{},
	})
}

type module struct {
	once sync.Once
	svc  *Service
}

func (m *module) service(d *core.Deps) *Service {
	m.once.Do(func() { m.svc = NewService(NewStore(d.DB), NewDirectory(d.DB), d.Log) })
	return m.svc
}

func (*module) Name() string { return "formbuilder" }

// Register 注册路由。r 已经是 /api/v1 且默认要求登录。固定路径写在 /:id 之前。
func (m *module) Register(r *gin.RouterGroup, d *core.Deps) {
	svc := m.service(d)
	job.Register(JobType, svc.exportJob) // 导出中心任务处理器
	routes(r, &handler{svc: svc})
}

// routes 注册全部接口（与 Register 分开，便于测试直接挂载到 gin 路由组上）。
func routes(r *gin.RouterGroup, h *handler) {
	g := r.Group("/plugins/formbuilder")

	// 设计
	g.GET("/roles", core.Perm("formbuilder:design"), h.roles)
	g.GET("/users", core.Perm("formbuilder:design"), h.users)
	g.GET("/forms", core.Perm("formbuilder:design"), h.listForms)
	g.POST("/forms", core.Perm("formbuilder:design"), h.createForm)
	g.POST("/forms/import", core.Perm("formbuilder:design"), h.importForm)
	g.GET("/forms/:id", core.Perm("formbuilder:design"), h.getForm)
	g.PUT("/forms/:id", core.Perm("formbuilder:design"), h.saveForm)
	g.DELETE("/forms/:id", core.Perm("formbuilder:design"), h.deleteForm)
	g.GET("/forms/:id/definition", core.Perm("formbuilder:design"), h.exportDefinition)
	g.GET("/forms/:id/versions", core.Perm("formbuilder:design"), h.listVersions)
	g.GET("/forms/:id/versions/:v", core.Perm("formbuilder:design"), h.getVersion)

	// 发布
	g.POST("/forms/:id/publish", core.Perm("formbuilder:publish"), h.publish)
	g.POST("/forms/:id/offline", core.Perm("formbuilder:publish"), h.setOnline(false))
	g.POST("/forms/:id/online", core.Perm("formbuilder:publish"), h.setOnline(true))

	// 填写
	g.GET("/fill/stats", core.Perm("formbuilder:fill"), h.fillStats) // 工作台卡片 formbuilder-pending
	g.GET("/fill/forms", core.Perm("formbuilder:fill"), h.fillList)
	g.GET("/fill/forms/:id", core.Perm("formbuilder:fill"), h.fillGet)
	g.POST("/fill/forms/:id/submit", core.Perm("formbuilder:fill"), h.submit)

	// 收集
	g.GET("/forms/:id/submission-meta", core.Perm("formbuilder:submission"), h.subMeta)
	g.GET("/forms/:id/submissions", core.Perm("formbuilder:submission"), h.listSubmissions)
	g.GET("/forms/:id/submissions/:sid", core.Perm("formbuilder:submission"), h.getSubmission)
	g.DELETE("/forms/:id/submissions/:sid", core.Perm("formbuilder:submission"), h.deleteSubmission)

	// 导出
	g.GET("/forms/:id/export", core.Perm("formbuilder:export"), h.exportSync)
	g.POST("/forms/:id/export-job", core.Perm("formbuilder:export"), h.exportJobSubmit)
}
