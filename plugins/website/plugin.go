// Package website 是 NexAdmin 企业官网插件：匿名公开的官网（根路径服务端渲染，SEO 友好）
// + 登录墙内的内容管理（站点设置、导航、特性、页面、新闻）。遵守插件约定：只依赖 core/ 与 pkg/，
// 表前缀 plg_website_，权限码前缀 website:，管理接口路径 /api/v1/plugins/website/...。
package website

import (
	"context"
	"crypto/ed25519"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"

	"nexadmin/core"
	"nexadmin/core/plugin"
	"nexadmin/core/plugin/market"
)

func init() {
	plugin.Register(plugin.Plugin{
		ID: "website", Name: "企业官网", Version: "0.6.0", Edition: plugin.Pro,
		Module: &module{},
	})
}

type module struct {
	once sync.Once
	svc  *Service
}

func (m *module) service(d *core.Deps) *Service {
	m.once.Do(func() { m.svc = NewService(d.DB) })
	return m.svc
}

func (*module) Name() string { return "website" }

// Register 注册管理端与公开官网路由。
func (m *module) Register(r *gin.RouterGroup, d *core.Deps) {
	h := &handler{svc: m.service(d), gw: newAIGateway(d)}

	// 语言清单来自平台（sys_language，core 在迁移后已加载）：官网不再自持一份。
	// 公开站路由随后按它注册，所以改动清单要重启才生效。
	if err := LoadLanguages(core.Languages()); err != nil && d.Log != nil {
		d.Log.Warn("website: 平台语言清单应用失败，沿用内置语言", zap.Error(err))
	}

	g := r.Group("/plugins/website")
	g.GET("/settings", core.Perm("website:setting"), core.Desc("读取站点设置"), h.getSettings)
	g.PUT("/settings", core.Perm("website:setting"), core.Desc("更新站点设置"), h.saveSettings)

	g.GET("/messages", core.Perm("website:message"), core.Desc("留言列表"), h.listMessages)
	g.GET("/messages/unread", core.Perm("website:message"), core.Desc("未读留言数"), h.unreadMessages)
	g.PUT("/messages/:id/read", core.Perm("website:message"), core.Desc("标记留言已读"), h.setMessageRead)
	g.DELETE("/messages/:id", core.Perm("website:message"), core.Desc("删除留言"), h.deleteMessage)

	g.GET("/navs", core.Perm("website:nav"), core.Desc("导航列表"), h.listNavs)
	g.POST("/navs", core.Perm("website:nav"), core.Desc("新增导航"), h.createNav)
	g.PUT("/navs/reorder", core.Perm("website:nav"), core.Desc("导航拖拽排序"), h.reorderNavs)
	g.PUT("/navs/:id", core.Perm("website:nav"), core.Desc("更新导航"), h.updateNav)
	g.DELETE("/navs/:id", core.Perm("website:nav"), core.Desc("删除导航"), h.deleteNav)

	g.GET("/features", core.Perm("website:feature"), core.Desc("特性卡片列表"), h.listFeatures)
	g.GET("/feature-icons", core.Perm("website:feature"), core.Desc("特性卡片可选图标"), h.listFeatureIcons)
	g.POST("/features", core.Perm("website:feature"), core.Desc("新增特性卡片"), h.createFeature)
	g.PUT("/features/:id", core.Perm("website:feature"), core.Desc("更新特性卡片"), h.updateFeature)
	g.DELETE("/features/:id", core.Perm("website:feature"), core.Desc("删除特性卡片"), h.deleteFeature)

	g.GET("/pages", core.Perm("website:page"), core.Desc("页面列表"), h.listPages)
	g.POST("/pages", core.Perm("website:page"), core.Desc("新增页面"), h.createPage)
	g.PUT("/pages/:id", core.Perm("website:page"), core.Desc("更新页面"), h.updatePage)
	g.DELETE("/pages/:id", core.Perm("website:page"), core.Desc("删除页面"), h.deletePage)

	g.GET("/posts", core.Perm("website:post"), core.Desc("新闻列表"), h.listPosts)
	g.POST("/posts", core.Perm("website:post"), core.Desc("新增新闻"), h.createPost)
	g.PUT("/posts/:id", core.Perm("website:post"), core.Desc("更新新闻"), h.updatePost)
	g.DELETE("/posts/:id", core.Perm("website:post"), core.Desc("删除新闻"), h.deletePost)

	g.POST("/media", core.Perm("website:setting"), core.Desc("上传 logo/封面图片"), h.uploadMedia)
	// 语言清单的管理接口在框架（/api/v1/languages，见 modules/system/language）：
	// 官网不再重复提供一份，否则又变成两份互不可见的清单。
	// 翻译表单的字段元数据由后端下发：白名单在 Go 里，前端不再抄一份。
	g.GET("/i18n/schema/:entity", h.i18nSchema)
	// 机翻按实体各挂自己的编辑权限，避免把插件权限码写进 core 的 translatePerms。
	for _, e := range translateRoutes {
		g.POST("/i18n/translate/"+e.entity, core.Perm(e.perm), core.Desc("机翻官网内容译文"), h.makeTranslator(e.entity))
	}

	// 公开官网：注册在根引擎的 RootGroup（带授权闸门）上，匿名可访问。
	// 热挂载时 RootGroup 为 nil——运行中的引擎不能加根路由，此时只挂管理端 API，
	// 官网页面在授权导入后重启生效。
	if d.RootGroup != nil {
		site := &siteHandler{svc: m.service(d), log: d.Log, pkg: newPackageCache(), signKey: loadMarketSignKey(d.Log)}
		// 留言是匿名写入口：比普通页面更紧的阈值（每 IP 每分钟 5 条），和页面浏览分开计数。
		site.contactLimit = newIPLimiter(5, time.Minute).Middleware()
		pub := d.RootGroup.Group("", newIPLimiter(600, time.Minute).Middleware())
		// 默认语言固定渲染在无前缀路径上；cookie 只影响首页回跳与语言中立端点。
		pub.Use(LanguageContext(DefaultLanguage()), langNegotiate)
		// 文本资源 gzip：放在最外层，语言页面与静态资源一起受益（zip 下载与图片/字体自动跳过）。
		pub.Use(gzipFilter())
		downloadLimit := newIPLimiter(30, time.Minute).Middleware()
		rateLimit := newIPLimiter(20, time.Minute).Middleware()

		// 页面路由按语言各注册一份：默认语言无前缀，其余语言 /<prefix>（不带尾斜杠）。
		site.registerPages(pub, false)
		// 后台只占 /admin：未注册的页面路径由官网渲染 404，而不是落进后台 SPA。
		core.SetSiteNotFoundHandler(site.notFoundPage)
		for _, l := range Languages() {
			if l.Default {
				continue
			}
			g := pub.Group("/" + l.Prefix)
			g.Use(LanguageMiddleware(l))
			site.registerPages(g, true)
		}

		// 每个 GET 都配上 HEAD：gin 不会把 GET 自动映射到 HEAD，不接的话监控/探活
		// 工具发 HEAD 会拿到 404 页（net/http 会自动省略 HEAD 响应体）。主下载不接 HEAD（打包昂贵且无意义）。
		pub.GET("/plugins/index.json", site.marketIndexJSON)
		pub.HEAD("/plugins/index.json", site.marketIndexJSON)
		pub.GET("/plugins/signing-key.pub", site.marketPublicKey)
		pub.HEAD("/plugins/signing-key.pub", site.marketPublicKey)
		// 下载会现场拉源码并重打包，比普通页面贵，单独收紧阈值（每 IP 每分钟 30 次）。
		// 包文件与语言无关，只保留无前缀端点；页面里的下载链接也不会加语言前缀。
		pub.GET("/plugins/:id/download", downloadLimit, site.marketDownload)
		// sha256 与签名是现成小文本，HEAD 一并接上，供监控探活。
		pub.GET("/plugins/:id/download.sha256", downloadLimit, site.marketChecksum)
		pub.HEAD("/plugins/:id/download.sha256", downloadLimit, site.marketChecksum)
		pub.GET("/plugins/:id/download.sig", downloadLimit, site.marketSignature)
		pub.HEAD("/plugins/:id/download.sig", downloadLimit, site.marketSignature)
		pub.POST("/plugins/:id/rate", rateLimit, site.marketRate)
		pub.GET("/website-assets/*path", site.asset)
		pub.HEAD("/website-assets/*path", site.asset)
		pub.GET("/robots.txt", site.robots)
		pub.HEAD("/robots.txt", site.robots)
		pub.GET("/sitemap.xml", site.sitemap)
		pub.HEAD("/sitemap.xml", site.sitemap)
	}
}

// registerPages 注册官网页面路由（每种语言各一份，前缀由外层分组决定）。
// bareHome 用于语言分组：首页是 /en 而不是 /en/，避免 canonical 与请求路径不一致。
func (h *siteHandler) registerPages(g *gin.RouterGroup, bareHome bool) {
	if bareHome {
		g.GET("", h.home)
		g.HEAD("", h.home)
	} else {
		g.GET("/", h.home)
		g.HEAD("/", h.home)
	}
	g.GET("/plugins", h.marketList)
	g.HEAD("/plugins", h.marketList)
	g.GET("/plugins/:id", h.marketDetail)
	g.HEAD("/plugins/:id", h.marketDetail)
	g.GET("/news", h.newsList)
	g.HEAD("/news", h.newsList)
	g.GET("/news/:slug", h.postDetail)
	g.HEAD("/news/:slug", h.postDetail)
	g.GET("/p/:slug", h.pageDetail)
	g.HEAD("/p/:slug", h.pageDetail)
	// 在线留言：GET 是联系页，POST 是同一个地址的投稿入口（无 JS 也能直接表单提交）。
	g.GET("/contact", h.contact)
	g.HEAD("/contact", h.contact)
	if h.contactLimit != nil {
		g.POST("/contact", h.contactLimit, h.contactSubmit)
	}
	// 文档区：渲染 nexadmin/docs 内嵌的框架使用文档，内容中文为主，随语言分组走前缀。
	g.GET("/docs", h.docsIndex)
	g.HEAD("/docs", h.docsIndex)
	g.GET("/docs/:slug", h.docPage)
	g.HEAD("/docs/:slug", h.docPage)
}

// loadMarketSignKey 读取 NEXADMIN_MARKET_SIGN_KEY 指向的 ed25519 私钥；未配置则不签名。
func loadMarketSignKey(log *zap.Logger) ed25519.PrivateKey {
	path := strings.TrimSpace(os.Getenv("NEXADMIN_MARKET_SIGN_KEY"))
	if path == "" {
		return nil
	}
	key, err := market.LoadSigningKey(path)
	if err != nil {
		if log != nil {
			log.Error("website: 市场签名私钥加载失败，包下载将不签名", zap.String("path", path), zap.Error(err))
		}
		return nil
	}
	if log != nil {
		log.Info("website: 市场包签名已启用", zap.String("kid", plugin.KeyID(key.Public().(ed25519.PublicKey))))
	}
	return key
}

// Start 实现 plugin.Starter：首次启动种入出厂内容（NexAdmin 官网默认文案）。
func (m *module) Start(ctx context.Context, d *core.Deps) error {
	svc := m.service(d)
	ensureSeed(ctx, svc, d.Log)
	ensureSeedBackfill(ctx, svc, d.Log)
	ensureSeedI18n(ctx, svc, d.Log)
	return nil
}
