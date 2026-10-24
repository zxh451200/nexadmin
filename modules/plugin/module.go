// Package plugin 是“插件管理”后台接口（内置免费模块）：列出已装插件与授权状态、导入授权文件。
//
// 对已安装但启动时未挂载（未授权/过期/无效）的 pro 插件，这里预留 /plugins/<id>/* 转发；
// 授权导入成功后插件被热挂载到独立的子引擎上，立即生效，不用重启。
package plugin

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"nexadmin/core"
	"nexadmin/core/plugin"
	"nexadmin/core/response"
)

func init() { core.RegisterBuiltin(module{}) }

// 权限点。
const (
	PermList    = "plugin:list"
	PermLicense = "plugin:license"
)

type module struct{}

func (module) Name() string { return "plugin" }

// menus 是本模块的菜单与按钮种子，挂在“系统管理”目录下；每次启动幂等写入（已存在的不动）。
func menus() []plugin.MenuSpec {
	return []plugin.MenuSpec{
		{Parent: "/system", Type: "menu", Title: "plugin.menu", Icon: "Puzzle", Path: "/system/plugin",
			Component: "plugin/PluginPage", Perm: PermList, Sort: 100, KeepAlive: true},
		{Parent: PermList, Type: "button", Title: "plugin.btn.license", Perm: PermLicense, Sort: 10},
	}
}

// SeedOnStartup 实现 setup.StartupSeeder：每次启动由 core 统一补齐本模块的菜单（已存在的不动）。
func (module) SeedOnStartup(db *gorm.DB) error { return plugin.SeedMenus(db, menus()) }

func (module) Register(r *gin.RouterGroup, d *core.Deps) {
	m := plugin.Default()
	m.Attach(d)

	r.GET("/plugins", core.Perm(PermList), func(c *gin.Context) {
		m.ObserveHost(c.Request.Host)
		response.OK(c, m.List())
	})

	licPerm := core.Perm(PermLicense)
	viaApp, dormant := m.Routes()
	for _, id := range viaApp {
		r.POST("/plugins/"+id+"/license", licPerm, importLicense(m, id))
	}
	for _, id := range dormant {
		id := id
		imp := importLicense(m, id)
		r.Any("/plugins/"+id+"/*rest", func(c *gin.Context) {
			if c.Request.Method == http.MethodPost && c.Param("rest") == "/license" {
				if licPerm(c); c.IsAborted() {
					return
				}
				imp(c)
				return
			}
			m.Dispatch(c, id)
		})
	}
}

type licenseInput struct {
	Content string `json:"content" binding:"required"`
}

func importLicense(m *plugin.Manager, id string) gin.HandlerFunc {
	return func(c *gin.Context) {
		var in licenseInput
		if !response.BindJSON(c, &in) {
			return
		}
		res, err := m.ImportLicense(id, in.Content)
		var le *plugin.LicenseError
		switch {
		case err == nil:
			response.OK(c, res)
		case errors.Is(err, plugin.ErrNotFound):
			response.Fail(c, response.NotFound("error.not_found").WithMsg("插件不存在"))
		case errors.Is(err, plugin.ErrFree):
			response.Fail(c, response.BadRequest("error.bad_request").WithMsg("免费插件无需授权"))
		case errors.As(err, &le):
			response.Fail(c, response.BadRequest("error.bad_request").WithMsg(le.Reason))
		default:
			response.Fail(c, err)
		}
	}
}
