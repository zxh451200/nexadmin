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
	"nexadmin/core/event"
	"nexadmin/core/job"
	"nexadmin/core/middleware"
	"nexadmin/core/plugin"
	"nexadmin/core/plugin/market"
)

func init() {
	event.Register(CatalogInfos()...)
	plugin.Register(plugin.Plugin{
		ID: "website", Name: "企业官网", Version: "0.6.0", Edition: plugin.Pro,
		Module: &module{},
	})
}

type module struct {
	once sync.Once
	svc  *Service
	sub  sync.Once
}

func (m *module) service(d *core.Deps) *Service {
	m.once.Do(func() {
		m.svc = NewService(d.DB)
		// 图片大小上限的兜底值：系统参数「上传限制」没设时用 config.yaml 的 upload.max_size_image_mb。
		if d.Config != nil {
			m.svc.SetConfigImageMaxMB(d.Config.Upload.MaxSizeImageMB)
		}
		// 共享缓存（阿里类目树等外部数据）：cache 未装配时服务层自动降级直连。
		m.svc.SetCache(d.Cache)
	})
	return m.svc
}

func (*module) Name() string { return "website" }

// Register 注册管理端与公开官网路由。
func (m *module) Register(r *gin.RouterGroup, d *core.Deps) {
	h := &handler{svc: m.service(d), gw: newAIGateway(d), previewKey: previewKeyOf(d)}

	// 语言清单来自平台（sys_language，core 在迁移后已加载）：官网不再自持一份。
	// 公开站路由随后按它注册，所以改动清单要重启才生效。
	if err := LoadLanguages(core.Languages()); err != nil && d.Log != nil {
		d.Log.Warn("website: 平台语言清单应用失败，沿用内置语言", zap.Error(err))
	}

	g := r.Group("/plugins/website")
	// 官网 12 张表全嵌 PlatformModel，没有 tenant_id：导航/页面/新闻代表整个部署主体的对外窗口
	// （plugins/website/service.go:372 就写着「官网导航是平台级数据」），而 plg_website_message
	// 是访客留言——匿名表单写进来的客户数据，任何持 website:message 的租户都能整批读走并导出。
	// 因此后台只对平台调用方开放；公开站与留言提交在下面的根引擎分组，匿名照常可用。
	// 按租户开官网需要给这 12 张表补 tenant_id 并让匿名表单知道自己在哪个租户（Host→租户解析已有）。
	g.Use(core.RequirePlatformFeature())
	g.GET("/settings", core.Perm("website:setting"), core.Desc("读取站点设置"), h.getSettings)
	g.PUT("/settings", core.Perm("website:setting"), core.Desc("更新站点设置"), h.saveSettings)

	// 主题：公开站外观由主题插件注册（RegisterTheme），设置页下拉切换；
	// 演示内容导入挂在主题 id 上（insert-only，可重复调用）。
	g.GET("/themes", core.Perm("website:setting"), core.Desc("已注册主题清单"), h.listThemes)
	g.POST("/themes/:id/seed-demo", core.Perm("website:setting"), core.Desc("导入主题演示内容"), h.seedThemeDemo)

	g.GET("/messages", core.Perm("website:message"), core.Desc("留言列表"), h.listMessages)
	g.GET("/messages/unread-count", core.Perm("website:message"), core.Desc("留言未读数"), h.unreadMessageCount)
	g.GET("/messages/export", core.Perm("website:message"), core.Desc("导出留言"), h.exportMessages)
	g.PUT("/messages/status", core.Perm("website:message"), core.Desc("批量修改留言状态"), h.setMessageStatus)
	g.DELETE("/messages", core.Perm("website:message"), core.Desc("批量删除留言"), h.deleteMessages)

	g.GET("/navs", core.Perm("website:nav"), core.Desc("导航列表"), h.listNavs)
	g.POST("/navs", core.Perm("website:nav"), core.Desc("新增导航"), h.createNav)
	g.PUT("/navs/reorder", core.Perm("website:nav"), core.Desc("导航拖拽排序"), h.reorderNavs)
	g.PUT("/navs/:id", core.Perm("website:nav"), core.Desc("更新导航"), h.updateNav)
	g.DELETE("/navs", core.Perm("website:nav"), core.Desc("批量删除导航"), h.deleteNavs)
	g.DELETE("/navs/:id", core.Perm("website:nav"), core.Desc("删除导航"), h.deleteNav)

	g.GET("/features", core.Perm("website:feature"), core.Desc("特性卡片列表"), h.listFeatures)
	g.GET("/feature-icons", core.Perm("website:feature"), core.Desc("特性卡片可选图标"), h.listFeatureIcons)
	g.POST("/features", core.Perm("website:feature"), core.Desc("新增特性卡片"), h.createFeature)
	g.PUT("/features/:id", core.Perm("website:feature"), core.Desc("更新特性卡片"), h.updateFeature)
	g.DELETE("/features", core.Perm("website:feature"), core.Desc("批量删除特性卡片"), h.deleteFeatures)
	g.DELETE("/features/:id", core.Perm("website:feature"), core.Desc("删除特性卡片"), h.deleteFeature)

	g.GET("/pages", core.Perm("website:page"), core.Desc("页面列表"), h.listPages)
	g.POST("/pages", core.Perm("website:page"), core.Desc("新增页面"), h.createPage)
	g.PUT("/pages/status", core.Perm("website:page"), core.Desc("批量发布/下线页面"), h.setPagesStatus)
	g.PUT("/pages/:id", core.Perm("website:page"), core.Desc("更新页面"), h.updatePage)
	g.GET("/pages/:id/preview", core.Perm("website:page"), core.Desc("签发页面预览链接"), h.pagePreview)
	g.POST("/pages/:id/copy", core.Perm("website:page"), core.Desc("复制页面"), h.copyPage)
	g.DELETE("/pages", core.Perm("website:page"), core.Desc("批量删除页面"), h.deletePages)
	g.DELETE("/pages/:id", core.Perm("website:page"), core.Desc("删除页面"), h.deletePage)

	g.GET("/posts", core.Perm("website:post"), core.Desc("新闻列表"), h.listPosts)
	g.GET("/posts/export", core.Perm("website:post"), core.Desc("导出新闻"), h.exportPosts)
	g.PUT("/posts/status", core.Perm("website:post"), core.Desc("批量发布/下线新闻"), h.setPostStatus)
	g.DELETE("/posts", core.Perm("website:post"), core.Desc("批量删除新闻"), h.deletePosts)
	g.POST("/posts", core.Perm("website:post"), core.Desc("新增新闻"), h.createPost)
	g.PUT("/posts/:id", core.Perm("website:post"), core.Desc("更新新闻"), h.updatePost)
	g.GET("/posts/:id/preview", core.Perm("website:post"), core.Desc("签发新闻预览链接"), h.postPreview)
	g.POST("/posts/:id/copy", core.Perm("website:post"), core.Desc("复制新闻"), h.copyPost)
	g.DELETE("/posts/:id", core.Perm("website:post"), core.Desc("删除新闻"), h.deletePost)
	g.GET("/posts/:id/versions", core.Perm("website:post"), core.Desc("新闻版本历史"), h.listPostVersions)
	g.GET("/posts/:id/versions/:ver", core.Perm("website:post"), core.Desc("新闻历史版本详情"), h.postVersion)
	g.POST("/posts/:id/versions/:ver/rollback", core.Perm("website:post"), core.Desc("回滚新闻到历史版本"), h.rollbackPostVersion)

	g.GET("/categories", core.Perm("website:post"), core.Desc("新闻分类列表"), h.listCategories)
	g.POST("/categories", core.Perm("website:post"), core.Desc("新增新闻分类"), h.createCategory)
	g.PUT("/categories/:id", core.Perm("website:post"), core.Desc("更新新闻分类"), h.updateCategory)
	g.DELETE("/categories/:id", core.Perm("website:post"), core.Desc("删除新闻分类"), h.deleteCategory)

	g.GET("/tags", core.Perm("website:post"), core.Desc("新闻标签列表"), h.listTags)
	g.POST("/tags", core.Perm("website:post"), core.Desc("新增新闻标签"), h.createTag)
	g.PUT("/tags/:id", core.Perm("website:post"), core.Desc("更新新闻标签"), h.updateTag)
	g.DELETE("/tags/:id", core.Perm("website:post"), core.Desc("删除新闻标签"), h.deleteTag)

	g.GET("/products", core.Perm("website:product"), core.Desc("产品列表"), h.listProducts)
	g.GET("/products/export", core.Perm("website:product"), core.Desc("导出产品"), h.exportProducts)
	g.GET("/products/export-plan", core.Perm("website:product"), core.Desc("产品导出计划（同步/异步分流）"), h.productExportPlan)
	g.POST("/products/export-async", core.Perm("website:product"), core.Desc("提交产品异步导出任务"), h.productExportAsync)
	g.PUT("/products/status", core.Perm("website:product"), core.Desc("批量上架/下线产品"), h.setProductStatus)
	g.DELETE("/products", core.Perm("website:product"), core.Desc("批量删除产品"), h.deleteProducts)
	g.POST("/products", core.Perm("website:product"), core.Desc("新增产品"), h.createProduct)
	g.PUT("/products/:id", core.Perm("website:product"), core.Desc("更新产品"), h.updateProduct)
	g.GET("/products/:id/preview", core.Perm("website:product"), core.Desc("签发产品预览链接"), h.productPreview)
	g.POST("/products/:id/copy", core.Perm("website:product"), core.Desc("复制产品"), h.copyProduct)
	g.DELETE("/products/:id", core.Perm("website:product"), core.Desc("删除产品"), h.deleteProduct)

	// 类目清单挂在 website:product 下：商品表单的类目复选框要读它；
	// 变更动作走独立的 website:product-category（后台单独一个菜单项）。
	g.GET("/product-categories", core.Perm("website:product"), core.Desc("产品类目列表"), h.listProductCategories)
	g.POST("/product-categories", core.Perm("website:product-category"), core.Desc("新增产品类目"), h.createProductCategory)
	g.PUT("/product-categories/reorder", core.Perm("website:product-category"), core.Desc("产品类目拖拽排序"), h.reorderProductCategories)
	g.PUT("/product-categories/:id", core.Perm("website:product-category"), core.Desc("更新产品类目"), h.updateProductCategory)
	g.DELETE("/product-categories/:id", core.Perm("website:product-category"), core.Desc("删除产品类目"), h.deleteProductCategory)

	g.POST("/media", core.Perm("website:setting"), core.Desc("上传 logo/封面图片"), h.uploadMedia)

	// 阿里国际站同步（二期）：凭据管理挂 website:setting（改的是全站凭据），
	// 发品动作挂 website:product（和商品编辑同一动作面）。
	g.GET("/ali-config", core.Perm("website:setting"), core.Desc("读取阿里开放平台凭据"), h.getAliConfig)
	g.PUT("/ali-config", core.Perm("website:setting"), core.Desc("保存阿里开放平台凭据"), h.saveAliConfig)
	g.POST("/ali/test", core.Perm("website:setting"), core.Desc("阿里开放平台连接测试"), h.testAliConnection)
	g.GET("/ali/categories", core.Perm("website:product"), core.Desc("阿里类目子级"), h.listAliCategories)
	g.GET("/ali/sync-status", core.Perm("website:product"), core.Desc("批量同步进度"), h.syncProductStatus)
	g.POST("/products/:id/sync", core.Perm("website:product"), core.Desc("同步商品到阿里国际站"), h.syncProduct)
	g.POST("/products/:id/ali-category", core.Perm("website:product"), core.Desc("绑定商品阿里类目"), h.bindAliCategory)
	g.POST("/products/sync-batch", core.Perm("website:product"), core.Desc("批量同步商品到阿里国际站"), h.syncProductBatch)

	// 批量同步的任务体：闭包捕获 svc。注册是覆盖写，热挂载下幂等。
	job.Register("website.ali-sync", func(ctx context.Context, j *job.Job, report job.Report) (job.ResultFile, error) {
		return m.service(d).RunAliSyncJob(ctx, j, report)
	})
	// 产品异步导出的任务体：worker 流式写 xlsx，进度与下载在导出中心。
	job.Register(jobTypeProductExport, func(ctx context.Context, j *job.Job, report job.Report) (job.ResultFile, error) {
		return m.service(d).RunProductExportJob(ctx, j, report)
	})
	g.GET("/limits", core.Perm("website:setting"), core.Desc("读取官网生效上限"), h.limits)
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
		site := &siteHandler{svc: m.service(d), log: d.Log, pkg: newPackageCache(), signKey: loadMarketSignKey(d.Log), previewKey: previewKeyOf(d)}
		// 留言是匿名写入口：阈值比普通页面紧（每 IP 每分钟 5 条），和页面浏览分开计数。
		// 计数走共享 cache，多实例部署时是同一个桶；cache 没装配才退回进程内滑动窗口。
		if d.Cache != nil {
			site.contactHits = middleware.NewLimiter(d.Cache, d.Log, "website-contact", contactRateMax, contactRateWindow)
		} else {
			site.contactLocal = newIPLimiter(contactRateMax, contactRateWindow)
		}
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
		pub.GET("/rss.xml", site.rss)
		pub.HEAD("/rss.xml", site.rss)
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
	// 产品中心：列表 /products（?category=<slug> 归档），详情 /products/:slug。
	g.GET("/products", h.productsList)
	g.HEAD("/products", h.productsList)
	g.GET("/products/:slug", h.productDetail)
	g.HEAD("/products/:slug", h.productDetail)
	g.GET("/p/:slug", h.pageDetail)
	g.HEAD("/p/:slug", h.pageDetail)
	// 在线留言：GET 是联系页，POST 是同一个地址的投稿入口（无 JS 也能直接表单提交）。
	// 投稿限流在 handler 内判定（contactAllowed），这样超限时无 JS 的表单提交
	// 也能 303 回联系页带错误提示，而不是被中间件直接吐一段 JSON。
	g.GET("/contact", h.contact)
	g.HEAD("/contact", h.contact)
	g.POST("/contact", h.contactSubmit)
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

// Start 实现 plugin.Starter：首次启动种入出厂内容（NexAdmin 官网默认文案），
// 并挂上新留言通知（站内信 + 可选邮件抄送）。
func (m *module) Start(ctx context.Context, d *core.Deps) error {
	svc := m.service(d)
	// 品牌主题激活时官网内容归主题与管理员所有：出厂营销内容（导航/特性/新闻/页面）
	// 不再补种，否则出厂卡片会在每次重启后混回品牌站。设置行仍要存在（渲染依赖它）。
	if !themeActive(ctx, svc) {
		ensureSeed(ctx, svc, d.Log)
		ensureSeedBackfill(ctx, svc, d.Log)
		ensureSeedI18n(ctx, svc, d.Log)
	}
	// 只订阅一次：重复挂载会把同一个事件挂两遍，一条留言就发多封通知。
	m.sub.Do(func() {
		n := newNotifier(ctx, d, svc)
		if n == nil {
			return
		}
		event.Subscribe(event.SubscriberFunc(n.handle))
		go n.run()
	})
	return nil
}
