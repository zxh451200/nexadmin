// Package plugin 是“插件管理”后台接口（内置免费模块）：列出已装插件与授权状态、导入授权文件，
// 以及插件市场：浏览目录、从 GitHub 一键安装/移除、重建并自重启（见 core/plugin/market）。
//
// 对已安装但启动时未挂载（未授权/过期/无效）的 pro 插件，这里预留 /plugins/<id>/* 转发；
// 授权导入成功后插件被热挂载到独立的子引擎上，立即生效，不用重启。
package plugin

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"nexadmin/core"
	"nexadmin/core/auth"
	"nexadmin/core/middleware"
	"nexadmin/core/plugin"
	"nexadmin/core/plugin/market"
	"nexadmin/core/response"
)

func init() { core.RegisterBuiltin(module{}) }

// 权限点。
const (
	PermList    = "plugin:list"
	PermLicense = "plugin:license"
	PermInstall = "plugin:install"
)

// marketRunner 是进程级唯一任务执行器：构建任务必须全序列化。
var marketRunner = market.NewRunner()

type module struct{}

func (module) Name() string { return "plugin" }

// menus 是本模块的菜单与按钮种子，挂在“系统管理”目录下；每次启动幂等写入（已存在的不动）。
func menus() []plugin.MenuSpec {
	return []plugin.MenuSpec{
		{Parent: "/system", Type: "menu", Title: "plugin.menu", Icon: "Puzzle", Path: "/system/plugin",
			Component: "plugin/PluginPage", Perm: PermList, Sort: 100, KeepAlive: true},
		{Parent: PermList, Type: "button", Title: "plugin.btn.license", Perm: PermLicense, Sort: 10},
		{Parent: PermList, Type: "button", Title: "plugin.btn.install", Perm: PermInstall, Sort: 20},
	}
}

// SeedOnStartup 实现 setup.StartupSeeder：每次启动由 core 统一补齐本模块的菜单（已存在的不动）。
func (module) SeedOnStartup(db *gorm.DB) error { return plugin.SeedMenus(db, menus()) }

func (module) Register(r *gin.RouterGroup, d *core.Deps) {
	m := plugin.Default()
	m.Attach(d)

	r.GET("/plugins", requirePlatformUser, core.Perm(PermList), func(c *gin.Context) {
		m.ObserveHost(c.Request.Host)
		response.OK(c, m.List(c.Request.Context()))
	})

	// 已启用（有授权并已挂载）的插件 ID：前端据此隐藏不可用的插件页面、跳过插件语言包加载。
	// 只回 ID，不含授权细节，所以登录用户都能读，不需要 plugin:list。
	r.GET("/plugins/active", middleware.RequireLogin(), func(c *gin.Context) {
		m.ObserveHost(c.Request.Host)
		response.OK(c, m.ActiveIDs())
	})

	viaApp, dormant := m.Routes()
	registerLicenseRoutes(r, d.Public, m, viaApp, dormant)
	registerMarket(r, m)
}

func requirePlatformUser(c *gin.Context) {
	cl := auth.ClaimsFrom(c)
	if cl == nil {
		response.Fail(c, response.Unauthorized("error.unauthorized"))
		return
	}
	if !cl.Super || cl.UserID == 0 || (cl.Principal != "" && cl.Principal != auth.PrincipalPAT) {
		response.Fail(c, response.Forbidden("error.forbidden"))
		return
	}
	c.Next()
}

func registerLicenseRoutes(r, public *gin.RouterGroup, m *plugin.Manager, viaApp, dormant []string) {
	licPerm := core.Perm(PermLicense)
	for _, id := range viaApp {
		r.POST("/plugins/"+id+"/license", requirePlatformUser, licPerm, importLicense(m, id))
	}
	if public == nil {
		public = r
	}
	for _, id := range dormant {
		id := id
		imp := importLicense(m, id)
		public.Any("/plugins/"+id+"/*rest", func(c *gin.Context) {
			if c.Request.Method == http.MethodPost && c.Param("rest") == "/license" {
				if requirePlatformUser(c); c.IsAborted() {
					return
				}
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

// marketEntry 是市场页的列表项：目录条目 + 安装状态 + 运行期授权状态。
type marketEntry struct {
	market.Entry
	Installed        bool   `json:"installed"`
	InstalledVersion string `json:"installedVersion"`
	Status           string `json:"status"` // 运行期状态；未装或未注册为空
}

type marketActions struct {
	install func(string, market.Entry, bool) string
	remove  func(string, string, bool) string
	rebuild func(string, bool) string
}

func registerMarket(r *gin.RouterGroup, m *plugin.Manager) {
	registerMarketWith(r, m, marketActions{startInstall, startRemove, startRebuild})
}

func registerMarketWith(r *gin.RouterGroup, m *plugin.Manager, actions marketActions) {
	r = r.Group("", requirePlatformUser)
	list := core.Perm(PermList)
	install := core.Perm(PermInstall)

	r.GET("/plugins/market", list, func(c *gin.Context) {
		root := m.Root()
		entries, err := market.LoadCatalog(root)
		if err != nil {
			response.Fail(c, err)
			return
		}
		byID := map[string]plugin.Info{}
		for _, p := range m.List(c.Request.Context()) {
			byID[p.ID] = p
		}
		out := make([]marketEntry, 0, len(entries))
		for _, e := range entries {
			item := marketEntry{Entry: e}
			if item.Installed = market.Installed(root, e.ID); item.Installed {
				if p, ok := byID[e.ID]; ok {
					item.InstalledVersion = p.Version
					item.Status = p.Status
				}
			}
			out = append(out, item)
		}
		response.OK(c, out)
	})

	r.POST("/plugins/market/install", install, func(c *gin.Context) {
		var in struct {
			ID    string `json:"id" binding:"required"`
			Force bool   `json:"force"`
		}
		if !response.BindJSON(c, &in) {
			return
		}
		root := m.Root()
		entries, err := market.LoadCatalog(root)
		if err != nil {
			response.Fail(c, err)
			return
		}
		entry, ok := market.FindEntry(entries, in.ID)
		if !ok {
			response.Fail(c, response.NotFound("plugin.market_not_found"))
			return
		}
		response.OK(c, gin.H{"jobId": actions.install(root, entry, in.Force)})
	})

	r.POST("/plugins/market/remove", install, func(c *gin.Context) {
		var in struct {
			ID    string `json:"id" binding:"required"`
			Purge bool   `json:"purge"`
		}
		if !response.BindJSON(c, &in) {
			return
		}
		response.OK(c, gin.H{"jobId": actions.remove(m.Root(), in.ID, in.Purge)})
	})

	r.POST("/plugins/market/rebuild", install, func(c *gin.Context) {
		response.OK(c, gin.H{"jobId": actions.rebuild(m.Root(), true)})
	})

	r.GET("/plugins/market/jobs/:jobId", list, func(c *gin.Context) {
		j, ok := marketRunner.Get(c.Param("jobId"))
		if !ok {
			// 自重启后任务消失；前端按“重启完成”分支处理。
			response.Fail(c, response.NotFound("error.not_found"))
			return
		}
		response.OK(c, j)
	})
}

// startInstall 一键安装全流程：下载 → 安装 → 重建 → 自重启。
// 构建失败时恢复安装前的源码、依赖清单与前端产物，避免留下装了一半、无法回退的源码树。
func startInstall(root string, e market.Entry, force bool) string {
	return marketRunner.Run("install", e.ID, func(x *market.Ctx) error {
		err := market.ChangeSources(root, e.ID, func() error {
			source := e.Repo
			if e.URL != "" {
				source = e.URL
			}
			x.Log("从 %s 拉取插件包…", source)
			zipPath, err := market.Download(context.Background(), e)
			if err != nil {
				return err
			}
			defer os.Remove(zipPath) //nolint:errcheck
			withWeb := market.HasWeb(zipPath)
			x.Log("下载完成，开始安装…")
			if _, err := market.InstallSub(root, zipPath, e.Subdir, force); err != nil {
				return err
			}
			x.Log("插件 %s 已装入源码树，开始重建…", e.ID)
			if err := market.Rebuild(context.Background(), root, withWeb, x.LogPlain); err != nil {
				return fmt.Errorf("重建失败: %w", err)
			}
			return nil
		})
		if err != nil {
			x.Log("失败，源码已恢复到安装前状态；可修正后重试")
			return err
		}
		return triggerRestart(root, x)
	})
}

// startRemove 移除 → 重建 → 自重启。purge 已经删库，无法回滚，因此不做源码恢复。
func startRemove(root, id string, purge bool) string {
	return marketRunner.Run("remove", id, func(x *market.Ctx) error {
		change := func() error {
			if purge {
				x.Log("清理插件数据表与菜单…")
			}
			if err := market.Remove(root, id, purge, ctxWriter{x}); err != nil {
				return err
			}
			x.Log("开始重建…")
			if err := market.Rebuild(context.Background(), root, true, x.LogPlain); err != nil {
				return fmt.Errorf("重建失败: %w", err)
			}
			return nil
		}
		var err error
		if purge {
			err = change()
		} else {
			err = market.ChangeSources(root, id, change)
		}
		if err != nil {
			if !purge {
				x.Log("失败，源码已恢复到移除前状态；可修正后重试")
			}
			return err
		}
		return triggerRestart(root, x)
	})
}

// startRebuild 仅重建 + 自重启（不装不删）。
func startRebuild(root string, withWeb bool) string {
	return marketRunner.Run("rebuild", "", func(x *market.Ctx) error {
		if err := market.Rebuild(context.Background(), root, withWeb, x.LogPlain); err != nil {
			return err
		}
		return triggerRestart(root, x)
	})
}

// triggerRestart 交给独立进程重启。无法自动重启时返回错误，让任务明确失败，
// 而不是把「产物已换、旧进程仍在」记成重启中或成功。
func triggerRestart(root string, x *market.Ctx) error {
	x.MarkRestarting()
	if err := market.Restart(root, x.LogPlain); err != nil {
		x.Log("%v", err)
		x.Log("产物已替换完成，但服务未自动重启；请手动重启后核验版本与插件状态。")
		return err
	}
	return nil
}

// ctxWriter 把 io.Writer 风格日志接到任务日志（market.Remove 的输出走这里）。
type ctxWriter struct{ x *market.Ctx }

func (w ctxWriter) Write(p []byte) (int, error) {
	w.x.Log("%s", string(p))
	return len(p), nil
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
			plugin.LocalizeInfo(c.Request.Context(), &res.Plugin)
			response.OK(c, res)
		case errors.Is(err, plugin.ErrNotFound):
			response.Fail(c, response.NotFound("plugin.not_found"))
		case errors.Is(err, plugin.ErrFree):
			response.Fail(c, response.BadRequest("plugin.free_no_license"))
		case errors.As(err, &le):
			response.Fail(c, response.BadRequest(le.Key, le.Args...))
		default:
			response.Fail(c, err)
		}
	}
}
