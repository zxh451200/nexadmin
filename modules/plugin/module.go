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
	"strings"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
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
	// PermEdit 是「插件管理页的启用/停用开关」。没有复用到 PermLicense：那个码的语义是
	// 「导入授权文件」（发行侧动作），启停是运营侧动作，两者授予范围不该绑在一起。
	PermEdit = "plugin:edit"
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
		{Parent: PermList, Type: "button", Title: "plugin.btn.toggle", Perm: PermEdit, Sort: 15},
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
		response.OK(c, listPlugins(c.Request.Context(), m))
	})

	// 已启用（有授权并已挂载）的插件 ID：前端据此隐藏不可用的插件页面、跳过插件语言包加载。
	// 只回 ID，不含授权细节，所以登录用户都能读，不需要 plugin:list。
	r.GET("/plugins/active", middleware.RequireLogin(), func(c *gin.Context) {
		m.ObserveHost(c.Request.Host)
		response.OK(c, m.ActiveIDs())
	})

	viaApp, dormant := m.Routes()
	registerLicenseRoutes(r, d.Public, m, viaApp, dormant)
	registerEnabled(r, m)
	registerMarket(r, m)
}

// registerEnabled 挂运行期启停开关：POST /plugins/:id/enabled，body {"enabled":true|false}。
//
// 用一条参数路由而不是逐个插件静态注册：启动时被停用（配置 disabled_plugins 或上次点过开关）的
// 插件在 Routes() 里既不在 viaApp 也不在 dormant，一个静态路由都不会为它预留，那样就再也打不开了。
// 例外是休眠 pro 插件：它们的 /plugins/<id>/*rest 通配会盖住参数路由（gin 里静态子树优先，
// 而且静态路径与通配兄弟会直接 panic），所以那部分在 registerLicenseRoutes 的转发端点里内部分派，
// 与 /license 同一处理。
func registerEnabled(r *gin.RouterGroup, m *plugin.Manager) {
	r.POST("/plugins/:id/enabled", requirePlatformUser, core.Perm(PermEdit), toggleEnabled(m, ""))
}

// enabledInput 的 enabled 用指针：省略字段 ≠ false，不能让一个写坏的 body 把插件停了。
type enabledInput struct {
	Enabled *bool `json:"enabled"`
}

// toggleEnabled 是启停处理函数。id 非空时固定用它（休眠插件的转发端点里没有 :id 参数），
// 为空时从参数路由的 :id 取。
func toggleEnabled(m *plugin.Manager, id string) gin.HandlerFunc {
	return func(c *gin.Context) {
		pluginID := id
		if pluginID == "" {
			pluginID = c.Param("id")
		}
		var in enabledInput
		if !response.BindJSON(c, &in) {
			return
		}
		if in.Enabled == nil {
			response.Fail(c, response.BadRequest("plugin.enabled_required"))
			return
		}
		info, err := m.SetEnabled(pluginID, *in.Enabled)
		switch {
		case err == nil:
			// 回完整 Info：前端据此立刻知道是「已经生效」还是「needRestart，重启前不会恢复」。
			plugin.LocalizeInfo(c.Request.Context(), &info)
			response.OK(c, info)
		case errors.Is(err, plugin.ErrNotFound):
			response.Fail(c, response.NotFound("plugin.not_found"))
		case errors.Is(err, plugin.ErrDisabledByConfig):
			response.Fail(c, response.BadRequest("plugin.disabled_by_config"))
		case errors.Is(err, plugin.ErrNotManageable):
			response.Fail(c, response.BadRequest("plugin.not_switchable"))
		default:
			response.Fail(c, err)
		}
	}
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
	editPerm := core.Perm(PermEdit)
	for _, id := range viaApp {
		r.POST("/plugins/"+id+"/license", requirePlatformUser, licPerm, importLicense(m, id))
	}
	if public == nil {
		public = r
	}
	for _, id := range dormant {
		id := id
		imp := importLicense(m, id)
		tog := toggleEnabled(m, id)
		public.Any("/plugins/"+id+"/*rest", func(c *gin.Context) {
			// 休眠插件的 /plugins/<id>/*rest 会盖住 /plugins/:id/enabled（通配与静态兄弟不能共存，
			// 见 registerEnabled），所以启停请求在这里内部分派，与 /license 同一处理。
			if c.Request.Method == http.MethodPost && (c.Param("rest") == "/license" || c.Param("rest") == "/enabled") {
				if requirePlatformUser(c); c.IsAborted() {
					return
				}
				lic := c.Param("rest") == "/license"
				perm := licPerm
				if !lic {
					perm = editPerm
				}
				if perm(c); c.IsAborted() {
					return
				}
				if lic {
					imp(c)
				} else {
					tog(c)
				}
				return
			}
			m.Dispatch(c, id)
		})
	}
}

// installedEntry 是已安装插件列表项：插件信息 + 市场目录里的分类。
// 分类只用于界面筛选，目录缺失或写坏时留空，不影响授权管理。
type installedEntry struct {
	plugin.Info
	Category string `json:"category"`
	// Bundled 表示随二进制编译进来、不是本机通过市场/CLI 安装的插件。
	// 后台据此把「已安装」与「内置」分开列：全新项目已安装为空，内置插件照常可用。
	Bundled bool `json:"bundled"`
}

func listPlugins(ctx context.Context, m *plugin.Manager) []installedEntry {
	infos := m.List(ctx)
	categories := map[string]string{}
	if entries, err := market.LoadCatalog(m.Root()); err == nil {
		for _, e := range entries {
			categories[e.ID] = e.Category
		}
	}
	ledger, err := market.LoadLedger(m.Root())
	if err != nil {
		// 台账损坏按「全部内置」处理：宁可少列，也不把内置插件误标成用户安装。
		zap.L().Warn("plugin: 读取安装台账失败", zap.Error(err))
	}
	return classifyInstalled(infos, categories, ledger)
}

// classifyInstalled 把注册表里的插件分成「用户装过」与「内置」两类。
func classifyInstalled(infos []plugin.Info, categories map[string]string, ledger []market.LedgerEntry) []installedEntry {
	userInstalled := make(map[string]bool, len(ledger))
	for _, e := range ledger {
		userInstalled[e.ID] = true
	}
	out := make([]installedEntry, 0, len(infos))
	for _, p := range infos {
		out = append(out, installedEntry{Info: p, Category: categories[p.ID], Bundled: !userInstalled[p.ID]})
	}
	return out
}

// marketEntry 是市场页的列表项：目录条目 + 安装状态 + 运行期授权状态。
//
// Installed 与「已安装」页同一口径——只认安装台账（本机装过的）。
// Bundled 表示插件随二进制编译进来（母版仓库里的源码插件全是这种）：它没在台账里，
// 市场页因此不给「重装覆盖 / 卸载」，那两个动作在源码树上是删目录，不是装插件。
type marketEntry struct {
	market.Entry
	Installed        bool   `json:"installed"`
	Bundled          bool   `json:"bundled"`
	InstalledVersion string `json:"installedVersion"`
	Status           string `json:"status"` // 运行期状态；未注册为空
	SourceVersion    string `json:"sourceVersion"`
	NeedRestart      bool   `json:"needRestart"`
}

type marketActions struct {
	install     func(string, []market.Entry, market.InstallOptions) string
	upload      func(string, string) string
	remove      func(string, string, bool) string
	rebuild     func(string, bool) string
	environment func(string) market.Environment
}

func registerMarket(r *gin.RouterGroup, m *plugin.Manager) {
	registerMarketWith(r, m, marketActions{
		install: startInstall, upload: startUploadZip, remove: startRemove,
		rebuild: startRebuild, environment: market.CheckEnvironment,
	})
}

func registerMarketWith(r *gin.RouterGroup, m *plugin.Manager, actions marketActions) {
	r = r.Group("", requirePlatformUser)
	list := core.Perm(PermList)
	install := core.Perm(PermInstall)
	environmentReady := func(c *gin.Context) bool {
		e := actions.environment(m.Root())
		if !e.Ready {
			response.Fail(c, response.BadRequest("plugin.environment_unavailable", strings.Join(e.Issues, ", ")))
			return false
		}
		return true
	}
	r.GET("/plugins/market/environment", list, func(c *gin.Context) {
		response.OK(c, actions.environment(m.Root()))
	})
	r.POST("/plugins/market/check", install, func(c *gin.Context) {
		var in struct {
			IDs []string `json:"ids"`
		}
		if !response.BindJSON(c, &in) {
			return
		}
		if err := validateMarketIDs(in.IDs); err != nil {
			response.Fail(c, err)
			return
		}
		out := make(map[string]market.UpdateStatus, len(in.IDs))
		for _, id := range in.IDs {
			status, err := market.InspectUpdate(m.Root(), id)
			if err != nil {
				response.Fail(c, err)
				return
			}
			out[id] = status
		}
		response.OK(c, out)
	})

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
		// 台账读不到就按「全部内置」处理，与 listPlugins 同一取舍：宁可少给按钮，
		// 也不给一个会删源码目录的入口。
		ledger, ledgerErr := market.LoadLedger(root)
		if ledgerErr != nil {
			zap.L().Warn("plugin: 读取安装台账失败", zap.Error(ledgerErr))
		}
		userInstalled := make(map[string]string, len(ledger))
		for _, l := range ledger {
			userInstalled[l.ID] = l.Version
		}
		out := make([]marketEntry, 0, len(entries))
		for _, e := range entries {
			sourceVersion, installed := userInstalled[e.ID]
			item := marketEntry{Entry: e, Installed: installed, SourceVersion: sourceVersion}
			if p, ok := byID[e.ID]; ok {
				item.InstalledVersion = p.Version
				item.Status = p.Status
				item.Bundled = !item.Installed
				item.NeedRestart = p.NeedRestart || (installed && sourceVersion != p.Version)
			} else {
				item.NeedRestart = installed
			}
			out = append(out, item)
		}
		response.OK(c, out)
	})

	r.POST("/plugins/market/install", install, func(c *gin.Context) {
		var in struct {
			ID            string `json:"id" binding:"required"`
			Force         bool   `json:"force"`
			AllowModified bool   `json:"allowModified"`
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
		if !environmentReady(c) {
			return
		}
		response.OK(c, gin.H{"jobId": actions.install(root, []market.Entry{entry}, market.InstallOptions{Overwrite: in.Force, AllowModified: in.AllowModified})})
	})

	r.POST("/plugins/market/install-batch", install, func(c *gin.Context) {
		var in struct {
			IDs           []string `json:"ids"`
			AllowModified bool     `json:"allowModified"`
		}
		if !response.BindJSON(c, &in) {
			return
		}
		if err := validateMarketIDs(in.IDs); err != nil {
			response.Fail(c, err)
			return
		}
		root := m.Root()
		catalog, err := market.LoadCatalog(root)
		if err != nil {
			response.Fail(c, err)
			return
		}
		entries := make([]market.Entry, 0, len(in.IDs))
		for _, id := range in.IDs {
			entry, ok := market.FindEntry(catalog, id)
			if !ok {
				response.Fail(c, response.NotFound("plugin.market_not_found"))
				return
			}
			entries = append(entries, entry)
		}
		if !environmentReady(c) {
			return
		}
		response.OK(c, gin.H{"jobId": actions.install(root, entries, market.InstallOptions{Overwrite: true, AllowModified: in.AllowModified})})
	})

	r.POST("/plugins/market/upload", install, func(c *gin.Context) {
		if !environmentReady(c) {
			return
		}
		fh, err := c.FormFile("file")
		if err != nil {
			response.Fail(c, response.BadRequest("plugin.upload_file_required"))
			return
		}
		if fh.Size > maxUploadBytes {
			response.Fail(c, response.BadRequest("plugin.upload_too_large", maxUploadBytes>>20))
			return
		}
		if !strings.HasSuffix(strings.ToLower(fh.Filename), ".zip") {
			response.Fail(c, response.BadRequest("plugin.upload_zip_only"))
			return
		}
		tmp, err := os.CreateTemp("", "nexadmin-plugin-upload-*.zip")
		if err != nil {
			response.Fail(c, err)
			return
		}
		tmpPath := tmp.Name()
		handedOff := false
		defer func() {
			if !handedOff {
				_ = os.Remove(tmpPath)
			}
		}()
		if err := saveUpload(c, tmp, fh, maxUploadBytes); err != nil {
			tmp.Close()
			response.Fail(c, err)
			return
		}
		if err := tmp.Close(); err != nil {
			response.Fail(c, err)
			return
		}
		jobID := actions.upload(m.Root(), tmpPath)
		handedOff = true
		response.OK(c, gin.H{"jobId": jobID})
	})

	r.POST("/plugins/market/remove", install, func(c *gin.Context) {
		var in struct {
			ID    string `json:"id" binding:"required"`
			Purge bool   `json:"purge"`
		}
		if !response.BindJSON(c, &in) {
			return
		}
		if !plugin.ValidID(in.ID) {
			response.Fail(c, response.BadRequest("plugin.market_ids_invalid"))
			return
		}
		if !environmentReady(c) {
			return
		}
		response.OK(c, gin.H{"jobId": actions.remove(m.Root(), in.ID, in.Purge)})
	})

	r.POST("/plugins/market/rebuild", install, func(c *gin.Context) {
		if !environmentReady(c) {
			return
		}
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

// startRemove 移除 → 重建 → 自重启。purge 已经删库，无法回滚，因此不做源码恢复。
func startRemove(root, id string, purge bool) string {
	return marketRunner.Run("remove", id, func(x *market.Ctx) error {
		if err := requireInstallEnvironment(root, productionInstallOperations()); err != nil {
			return err
		}
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
		if err := requireInstallEnvironment(root, productionInstallOperations()); err != nil {
			return err
		}
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
		case errors.Is(err, plugin.ErrDisabledByOperator):
			// 运行期开关停用的插件：导授权不会让它复活，请先在插件管理页启用。
			response.Fail(c, response.BadRequest("plugin.disabled_by_operator"))
		case errors.As(err, &le):
			response.Fail(c, response.BadRequest(le.Key, le.Args...))
		default:
			response.Fail(c, err)
		}
	}
}
