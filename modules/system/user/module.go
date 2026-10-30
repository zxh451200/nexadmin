// Package user 是用户模块（按租户隔离）：用户 CRUD、导出、重置密码、解锁，
// 同时实现登录提供者 auth.Provider（登录/刷新/me/连续错误锁定）和初始化向导的建表、种子、超管创建。
package user

import (
	"sync/atomic"

	"github.com/gin-gonic/gin"

	"nexadmin/core"
	"nexadmin/core/apidoc"
	"nexadmin/core/auth"
	"nexadmin/core/job"
)

func init() { core.RegisterBuiltin(Module()) }

// Module 返回用户模块。模块本身就是 auth.Provider 与 setup 钩子的实现，core 会自动扫描到。
func Module() core.Module { return &module{} }

type module struct {
	deps atomic.Pointer[core.Deps] // Register 时保存，登录提供者用它取 DB / 缓存
}

func (*module) Name() string { return "user" }

// Register 注册路由。固定路径写在 /:id 之前。
func (m *module) Register(r *gin.RouterGroup, d *core.Deps) {
	m.deps.Store(d)
	// 会话时限（闲置退出 / 最长会话）以设置页为准，登录与刷新时实时读取，管理员改完即时生效
	if d.Auth != nil {
		d.Auth.SetSessionPolicyProvider(func() auth.SessionPolicy { return sessionPolicy(d.DB) })
	}
	m.registerOAuth(d.Public, d) // /auth/providers、/auth/oauth/*（第三方快捷登录）
	m.registerLoginVerify(d)     // POST /auth/login/verify（二次验证换令牌；无 LoginGate 注册时没有挑战，端点恒返回统一错误）
	h := &handler{svc: &service{db: d.DB, cache: d.Cache, notify: d.Notify}, log: d.Log}
	svc := h.svc
	m.provisioner(svc)
	d.Notify.SetDirectory(directory{svc: h.svc}) // 邮件 / 短信渠道按用户资料里的邮箱 / 手机号发送
	m.registerForgot(d)                          // POST /auth/forgot/send-code、/auth/forgot/reset（匿名）
	m.registerSelf(d)                            // POST /auth/register（匿名，auth.allow_register 开启时才有）
	r.GET("/users", core.Perm("user:list"), h.list)
	r.GET("/users/export", core.Perm("user:export"), h.export)
	ah := &asyncHandler{svc: h.svc, syncLimit: syncLimitOf(d.Config.Export.SyncLimit)}
	job.Register(jobTypeUserExport, h.svc.exportJob) // 异步导出：处理器注册只需这一行
	r.GET("/users/export-plan", core.Perm("user:export"), ah.plan)
	r.POST("/users/export-async", core.Perm("user:export"), ah.create)
	r.GET("/users/import-template", core.Perm("user:import"), h.importTemplate)
	r.POST("/users/import", core.Perm("user:import"), h.importFile)
	r.DELETE("/users", core.Perm("user:delete"), h.removeMany)
	r.GET("/users/:id", core.Perm("user:list"), h.get)
	r.POST("/users", core.Perm("user:create"), h.create)
	r.PUT("/users/:id", core.Perm("user:update"), h.update)
	r.DELETE("/users/:id", core.Perm("user:delete"), h.remove)
	r.PUT("/users/:id/status", core.Perm("user:update"), h.setStatus)
	r.POST("/users/:id/reset-password", core.Perm("user:resetPwd"), h.resetPassword)
	r.POST("/users/:id/unlock", core.Perm("user:unlock"), h.unlock)
}

func (m *module) provisioner(s *service) {
	core.SetUserProvisioner(userProvisioner{svc: s})
}

// APIDoc 实现 apidoc.Describer：给用户列表补充摘要与查询参数（字段取自 queryOf）。
func (*module) APIDoc() []apidoc.Operation {
	return []apidoc.Operation{{
		Method:  "GET",
		Path:    "/users",
		Summary: "用户列表（分页）",
		Query: []apidoc.Param{
			{Name: "page", Type: "integer", Description: "页码，默认 1"},
			{Name: "pageSize", Type: "integer", Description: "每页条数，默认 10，上限 200"},
			{Name: "keyword", Description: "关键字"},
			{Name: "status", Description: "状态"},
			{Name: "deptId", Description: "部门 ID"},
			{Name: "gender", Description: "性别"},
			{Name: "createdFrom", Description: "创建时间起"},
			{Name: "createdTo", Description: "创建时间止"},
		},
		Response: apidoc.Obj(map[string]any{
			"list":     map[string]any{"type": "array", "items": map[string]any{"type": "object"}},
			"total":    apidoc.Int("总数"),
			"page":     apidoc.Int("当前页"),
			"pageSize": apidoc.Int("每页条数"),
		}),
	}}
}
