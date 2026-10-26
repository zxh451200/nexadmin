// Package plugin 是“插件管理”后台接口（内置免费模块）：列出已装插件与授权状态、导入授权文件，
// 以及插件市场：浏览目录、从 GitHub 一键安装/移除、重建并自重启（见 core/plugin/market）。
//
// 对已安装但启动时未挂载（未授权/过期/无效）的 pro 插件，这里预留 /plugins/<id>/* 转发；
// 授权导入成功后插件被热挂载到独立的子引擎上，立即生效，不用重启。
package plugin

import (
	"context"
	"errors"
	"net/http"
	"os"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"nexadmin/core"
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

	registerMarket(r, m)
}

// marketEntry 是市场页的列表项：目录条目 + 安装状态 + 运行期授权状态。
type marketEntry struct {
	market.Entry
	Installed        bool   `json:"installed"`
	InstalledVersion string `json:"installedVersion"`
	Status           string `json:"status"` // 运行期状态；未装或未注册为空
}

func registerMarket(r *gin.RouterGroup, m *plugin.Manager) {
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
		for _, p := range m.List() {
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
			response.Fail(c, response.NotFound("error.not_found").WithMsg("市场目录里没有该插件"))
			return
		}
		response.OK(c, gin.H{"jobId": startInstall(root, entry, in.Force)})
	})

	r.POST("/plugins/market/remove", install, func(c *gin.Context) {
		var in struct {
			ID    string `json:"id" binding:"required"`
			Purge bool   `json:"purge"`
		}
		if !response.BindJSON(c, &in) {
			return
		}
		response.OK(c, gin.H{"jobId": startRemove(m.Root(), in.ID, in.Purge)})
	})

	r.POST("/plugins/market/rebuild", install, func(c *gin.Context) {
		response.OK(c, gin.H{"jobId": startRebuild(m.Root(), true)})
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
func startInstall(root string, e market.Entry, force bool) string {
	return marketRunner.Run("install", e.ID, func(x *market.Ctx) error {
		x.Log("从 GitHub %s 拉取插件包…", e.Repo)
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
			x.Log("重建失败：%v（源码已装好，可手动 scripts/build.sh 后重启）", err)
			return err
		}
		return triggerRestart(root, x)
	})
}

// startRemove 移除 → 重建 → 自重启。
func startRemove(root, id string, purge bool) string {
	return marketRunner.Run("remove", id, func(x *market.Ctx) error {
		if purge {
			x.Log("清理插件数据表与菜单…")
		}
		if err := market.Remove(root, id, purge, ctxWriter{x}); err != nil {
			return err
		}
		x.Log("开始重建…")
		if err := market.Rebuild(context.Background(), root, true, x.LogPlain); err != nil {
			x.Log("重建失败：%v（文件已移除，可手动 scripts/build.sh 后重启）", err)
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

// triggerRestart 尝试自重启；非托管运行形态退化为提示手动重启，任务仍算成功（产物已换好）。
func triggerRestart(root string, x *market.Ctx) error {
	x.MarkRestarting()
	if err := market.Restart(root, x.LogPlain); err != nil {
		x.Log("%v", err)
		x.Log("请手动重启服务生效（二进制已替换完成）。")
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
