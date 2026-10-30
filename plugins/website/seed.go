package website

import (
	"context"
	"time"

	"go.uber.org/zap"
	"gorm.io/gorm"
)

// 出厂内容：插件首次启动时种入 NexAdmin 官网的默认文案。之后一切以库里数据为准，
// 管理端改过的内容不会被 seed 覆盖（只在空表时写入）。每种内容都带 en-US 出厂译文；
// 老版本升级上来的库由 ensureSeedI18n 只补空白译文（已有译文不动）。

// seedVersion 是当前出厂内容版本：新增出厂条目时 +1，并在 ensureSeedBackfill 里补种。
const seedVersion = 5

func enText(fields map[string]string) string {
	return i18nJSON(map[string]map[string]string{"en-US": fields})
}

func defaultSetting() Setting {
	return Setting{
		SiteName:  "NexAdmin",
		SiteDesc:  "以源码交付的后台管理框架：后端 Gin + GORM，前端 React + shadcn/ui，生产构建为单一二进制。",
		HomeTitle: "为生产级后台系统" + "\n" + "而生的全栈框架",
		HomeSubtitle: "多租户、RBAC 权限、开放平台与插件体系，出厂即备。" +
			"每一项能力都以源码交付——可读、可改、可升级。",
		HomeCTALabel:  "开始使用",
		HomeCTAHref:   "#quickstart",
		SeoKeywords:   "NexAdmin,后台管理框架,Gin,React,多租户,RBAC,插件,开源",
		FooterNote:    "框架区源码以 MIT 许可开源；业务与插件代码归各自作者。",
		MarketEnabled: true,
		SeedVersion:   seedVersion,
		I18nJSON: enText(map[string]string{
			"siteDesc":     "A source-delivered admin framework: Gin + GORM on the backend, React + shadcn/ui on the frontend, shipped as a single production binary.",
			"homeTitle":    "The full-stack framework" + "\n" + "built for production admin",
			"homeSubtitle": "Multi-tenancy, RBAC, an open platform, and a plugin system — included. Every capability ships as source you can read, change, and upgrade.",
			"homeCtaLabel": "Get started",
			"seoKeywords":  "NexAdmin,admin framework,Gin,React,multi-tenant,RBAC,plugins,open source",
			"footerNote":   "The framework is open source under MIT; business and plugin code belongs to its authors.",
		}),
	}
}

func defaultNavs() []Nav {
	return []Nav{
		{Label: "首页", Href: "/", Sort: 5, Enabled: true, I18nJSON: enText(map[string]string{"label": "Home"})},
		{Label: "动态", Href: "/news", Sort: 20, Enabled: true, I18nJSON: enText(map[string]string{"label": "News"})},
		{Label: "关于", Href: "/p/about", Sort: 30, Enabled: true, I18nJSON: enText(map[string]string{"label": "About"})},
		{Label: "联系", Href: "/contact", Sort: 40, Enabled: true, I18nJSON: enText(map[string]string{"label": "Contact"})},
	}
}

// defaultNavByHref 取出厂导航里指向 href 的那一条。
func defaultNavByHref(href string) (Nav, bool) {
	for _, n := range defaultNavs() {
		if n.Href == href {
			return n, true
		}
	}
	return Nav{}, false
}

func defaultFeatures() []Feature {
	return []Feature{
		{Icon: "building", Title: "多租户", Description: "共享库 + tenant_id 数据层隔离；用户、角色、存储与 AI 调用的额度及到期，在登录入口即强制执行。", Sort: 10, Enabled: true,
			I18nJSON: enText(map[string]string{
				"title":       "Multi-tenancy",
				"description": "Shared database with tenant_id isolation at the data layer. User, role, storage, and AI quotas are enforced at sign-in, including expiry.",
			})},
		{Icon: "shield", Title: "权限与数据范围", Description: "用户到角色到按钮级权限码；五档数据范围在服务端裁剪列表与写操作，不信前端。", Sort: 20, Enabled: true,
			I18nJSON: enText(map[string]string{
				"title":       "Permissions & data scope",
				"description": "Role-based permission codes down to individual buttons. Five data-scope levels are enforced server-side on reads and writes; client input is never trusted.",
			})},
		{Icon: "key", Title: "登录与 SSO", Description: "账号密码、短信，以及企业微信、钉钉、飞书、GitHub 等 OAuth2；本站亦可作为 OIDC Provider 供下游接入。", Sort: 30, Enabled: true,
			I18nJSON: enText(map[string]string{
				"title":       "Sign-in & SSO",
				"description": "Password and SMS sign-in plus OAuth2 for WeCom, DingTalk, Feishu, and GitHub. NexAdmin can also act as an OIDC provider for downstream apps.",
			})},
		{Icon: "plug", Title: "开放平台", Description: "个人访问令牌、HMAC 应用签名、事件目录与 Webhook、运行时 OpenAPI 文档、MCP 工具入口。", Sort: 40, Enabled: true,
			I18nJSON: enText(map[string]string{
				"title":       "Open platform",
				"description": "Personal access tokens, HMAC-signed apps, an event catalog with webhooks, runtime OpenAPI docs, and an MCP tool endpoint.",
			})},
		{Icon: "file", Title: "文件与导出", Description: "tus 断点续传上传，本地或 S3 兼容存储；导出走异步任务中心，流式写出。", Sort: 50, Enabled: true,
			I18nJSON: enText(map[string]string{
				"title":       "Files & exports",
				"description": "Resumable tus uploads to local or S3-compatible storage. Exports run as async jobs and stream their output.",
			})},
		{Icon: "chart", Title: "监控可观测", Description: "服务、缓存、接口与前端错误的监控页面；/metrics 输出 Prometheus 文本。", Sort: 60, Enabled: true,
			I18nJSON: enText(map[string]string{
				"title":       "Observability",
				"description": "Monitoring pages for services, cache, APIs, and frontend errors; /metrics exposes Prometheus text.",
			})},
		{Icon: "globe", Title: "国际化与移动端", Description: "zh-CN、en-US 出厂，可自行增加语言；PWA 与移动端适配开箱即用。", Sort: 70, Enabled: true,
			I18nJSON: enText(map[string]string{
				"title":       "i18n & mobile",
				"description": "zh-CN and en-US out of the box, with room for your own locales. PWA support and responsive layouts work from day one.",
			})},
		{Icon: "puzzle", Title: "源码插件体系", Description: "插件即源码包：拷文件、重新编译即安装；pro 插件离线签名授权，导入授权热挂载。", Sort: 80, Enabled: true,
			I18nJSON: enText(map[string]string{
				"title":       "Source-based plugins",
				"description": "A plugin is a source package: copy files and rebuild to install. Pro plugins use offline signed licenses that hot-mount on import.",
			})},
	}
}

func defaultPosts() []Post {
	now := time.Now().UTC()
	return []Post{
		{
			Slug: "hello-nexadmin", Title: "NexAdmin 上线：以源码交付的后台管理框架",
			Summary: "后端 Gin + GORM、前端 React + shadcn/ui，生产构建为单一二进制。" +
				"框架区与用户区在目录上分开，`nexadmin upgrade` 升级不丢定制。",
			Content:   "<p> NexAdmin 以源码交付：后端 Gin + GORM，前端 React + shadcn/ui，生产构建为单一二进制（前端产物通过 <code>-tags embedweb</code> 嵌入）。</p><p>多租户、RBAC 与五档数据范围、开放平台（访问令牌、应用签名、Webhook、MCP）、监控与国际化，全部出厂即备。</p><p>十分钟上手：构建、启动、打开 <code>/setup</code> 向导四步完成初始化。</p>",
			Published: true, PublishedAt: &now,
			I18nJSON: enText(map[string]string{
				"title":   "NexAdmin is live: an admin framework delivered as source",
				"summary": "Gin + GORM on the backend, React + shadcn/ui on the frontend, built into a single binary. Framework and user code live apart, so nexadmin upgrade never drops your customizations.",
				"content": "<p>NexAdmin ships as source: Gin + GORM on the backend, React + shadcn/ui on the frontend, and a single production binary with the frontend embedded via <code>-tags embedweb</code>.</p><p>Multi-tenancy, RBAC with five data-scope levels, the open platform (access tokens, app signatures, webhooks, MCP), monitoring, and internationalization are all included.</p><p>Get started in ten minutes: build, start, and finish the four-step wizard at <code>/setup</code>.</p>",
			}),
		},
		{
			Slug: "website-plugin", Title: "官网插件发布：企业官网即插即用",
			Summary: "匿名公开官网（服务端渲染、SEO 友好）+ 后台内容管理。" +
				"你正在浏览的页面，就是它的默认模板。",
			Content:   "<p>企业官网插件把「官网」变成 NexAdmin 的一个源码插件：匿名访客看到服务端渲染的官网页面，管理员在后台维护站点设置、导航、特性、页面与新闻。</p><p>本页即该插件的默认内容——购买授权、安装、改文案，就是你的官网。</p>",
			Published: true, PublishedAt: &now,
			I18nJSON: enText(map[string]string{
				"title":   "Website plugin released: a corporate site, ready to install",
				"summary": "A public, anonymous, server-rendered site with SEO support, plus content management in the admin. You're reading its default template right now.",
				"content": "<p>The website plugin turns “corporate site” into a NexAdmin source plugin: anonymous visitors get server-rendered pages, while admins manage settings, navigation, features, pages, and news.</p><p>This page is the plugin's default content. Buy a license, install it, change the copy — and it's your site.</p>",
			}),
		},
		{
			Slug: "upgrade-without-fork", Title: "升级不丢定制：框架区与用户区的边界",
			Summary:   "升级只覆盖框架区；用户区的业务改动以 *.bak 保留并逐个报告，upgrade --dry-run 先看清单再执行。",
			Content:   "<p>NexAdmin 把代码分成框架区与用户区：框架区随版本升级，业务代码与插件写在用户区。</p><p><code>nexadmin upgrade --from &lt;路径&gt; --dry-run</code> 先列出将要变更的文件；正式升级后被改写的用户区文件会留出 <code>*.bak</code>，输出里逐条给出 <code>WARN</code>。升级前先 <code>git commit</code>，升级后跑构建与测试即可。</p>",
			Published: true, PublishedAt: &now,
			I18nJSON: enText(map[string]string{
				"title":   "Upgrade without forking: the framework/user boundary",
				"summary": "Upgrades only touch the framework directory; changed files in user code are kept as *.bak and reported. Run upgrade --dry-run first to see the list.",
				"content": "<p>NexAdmin splits code into a framework area and a user area. The framework follows releases; business code and plugins live in the user area.</p><p><code>nexadmin upgrade --from &lt;path&gt; --dry-run</code> lists every file that would change. If a user-area file is rewritten during a real upgrade, the old content is kept as <code>*.bak</code> and a <code>WARN</code> is printed for each one. Commit before upgrading, then run the build and tests.</p>",
			}),
		},
	}
}

func defaultPages() []Page {
	return []Page{
		{
			Slug: "about", Title: "关于 NexAdmin", Published: true,
			SeoDesc: "NexAdmin 是以源码交付的后台管理框架。",
			Content: "<p> NexAdmin 是一个以源码交付的后台管理框架：后端 Gin + GORM，前端 React + shadcn/ui。</p><p>它把交付、隔离与演进当作一等公民：框架区与用户区在目录上分开，升级只动框架区；多租户从数据层开始；模块可停用、插件可装卸。数据库支持 MySQL、MariaDB 与 PostgreSQL，缓存支持 memory 与 Redis（Valkey 兼容）。</p><p>联系方式请使用站点设置中配置的邮箱。</p>",
			I18nJSON: enText(map[string]string{
				"title":   "About NexAdmin",
				"seoDesc": "NexAdmin is an admin framework delivered as source.",
				"content": "<p>NexAdmin is an admin framework delivered as source: Gin + GORM on the backend, React + shadcn/ui on the frontend.</p><p>Delivery, isolation, and evolution are first-class concerns: framework and user code live in separate directories so upgrades only touch the framework; multi-tenancy starts at the data layer; modules can be disabled and plugins installed or removed. It supports MySQL, MariaDB, and PostgreSQL, with memory or Redis-compatible (Valkey) caching.</p><p>For contact details, use the email configured in site settings.</p>",
			}),
		},
	}
}

// ensureSeed 在空表时写入出厂内容；任何一步失败只记日志，不阻塞启动。
func ensureSeed(ctx context.Context, s *Service, log *zap.Logger) {
	var n int64
	if err := s.db.WithContext(ctx).Model(&Setting{}).Count(&n).Error; err == nil && n == 0 {
		setting := defaultSetting()
		if err := s.db.WithContext(ctx).Create(&setting).Error; err != nil && log != nil {
			log.Warn("website: seed setting failed", zap.Error(err))
		}
	}
	if err := s.db.WithContext(ctx).Model(&Nav{}).Count(&n).Error; err == nil && n == 0 {
		navs := defaultNavs()
		if err := s.db.WithContext(ctx).Create(&navs).Error; err != nil && log != nil {
			log.Warn("website: seed navs failed", zap.Error(err))
		}
	}
	if err := s.db.WithContext(ctx).Model(&Feature{}).Count(&n).Error; err == nil && n == 0 {
		features := defaultFeatures()
		if err := s.db.WithContext(ctx).Create(&features).Error; err != nil && log != nil {
			log.Warn("website: seed features failed", zap.Error(err))
		}
	}
	if err := s.db.WithContext(ctx).Model(&Post{}).Count(&n).Error; err == nil && n == 0 {
		posts := defaultPosts()
		if err := s.db.WithContext(ctx).Create(&posts).Error; err != nil && log != nil {
			log.Warn("website: seed posts failed", zap.Error(err))
		}
	}
	if err := s.db.WithContext(ctx).Model(&Page{}).Count(&n).Error; err == nil && n == 0 {
		pages := defaultPages()
		if err := s.db.WithContext(ctx).Create(&pages).Error; err != nil && log != nil {
			log.Warn("website: seed pages failed", zap.Error(err))
		}
	}
}

// ensureSeedI18n 给老库补出厂译文：只填 i18n_json 为空、且原文仍等于出厂文案的行。
// 管理端已翻译或已改写原文的内容一概不碰，可重复执行。
func ensureSeedI18n(ctx context.Context, s *Service, log *zap.Logger) {
	warn := func(what string, err error) {
		if err != nil && log != nil {
			log.Warn("website: seed i18n failed", zap.String("entity", what), zap.Error(err))
		}
	}
	empty := func(tx *gorm.DB) *gorm.DB {
		return tx.Where("i18n_json IS NULL OR i18n_json = ''")
	}

	def := defaultSetting()
	if set, err := s.Settings(ctx); err == nil && set.I18nJSON == "" && set.HomeTitle == def.HomeTitle && def.I18nJSON != "" {
		warn("setting", empty(s.db.WithContext(ctx).Model(&Setting{})).Where("id = ?", int64(set.ID)).Update("i18n_json", def.I18nJSON).Error)
	}

	var navs []Nav
	if err := s.db.WithContext(ctx).Find(&navs).Error; err == nil {
		want := map[string]string{}
		for _, n := range defaultNavs() {
			want[n.Href] = n.I18nJSON
		}
		for _, n := range navs {
			if n.I18nJSON != "" {
				continue
			}
			if v := want[n.Href]; v != "" && n.Label == labelOfDefaultNav(n.Href) {
				warn("nav", empty(s.db.WithContext(ctx).Model(&Nav{})).Where("id = ?", int64(n.ID)).Update("i18n_json", v).Error)
			}
		}
	}

	var features []Feature
	if err := s.db.WithContext(ctx).Find(&features).Error; err == nil {
		want := map[string]string{}
		for _, f := range defaultFeatures() {
			want[f.Icon+"|"+f.Title] = f.I18nJSON
		}
		for _, f := range features {
			if f.I18nJSON == "" {
				if v := want[f.Icon+"|"+f.Title]; v != "" {
					warn("feature", empty(s.db.WithContext(ctx).Model(&Feature{})).Where("id = ?", int64(f.ID)).Update("i18n_json", v).Error)
				}
			}
		}
	}

	wantPosts := map[string]string{}
	for _, p := range defaultPosts() {
		wantPosts[p.Slug] = p.I18nJSON
	}
	var posts []Post
	if err := s.db.WithContext(ctx).Find(&posts).Error; err == nil {
		for _, p := range posts {
			if p.I18nJSON == "" {
				if v := wantPosts[p.Slug]; v != "" {
					warn("post", empty(s.db.WithContext(ctx).Model(&Post{})).Where("id = ?", int64(p.ID)).Update("i18n_json", v).Error)
				}
			}
		}
	}

	wantPages := map[string]string{}
	for _, p := range defaultPages() {
		wantPages[p.Slug] = p.I18nJSON
	}
	var pages []Page
	if err := s.db.WithContext(ctx).Find(&pages).Error; err == nil {
		for _, p := range pages {
			if p.I18nJSON == "" {
				if v := wantPages[p.Slug]; v != "" {
					warn("page", empty(s.db.WithContext(ctx).Model(&Page{})).Where("id = ?", int64(p.ID)).Update("i18n_json", v).Error)
				}
			}
		}
	}
}

// ensureSeedBackfill 给升级上来的库补种新增的出厂新闻（目前是第 3 篇）。
// 只在 seed_version 落后时执行一次：管理员之后删掉的条目不会被反复复活。
func ensureSeedBackfill(ctx context.Context, s *Service, log *zap.Logger) {
	warn := func(what string, err error) {
		if err != nil && log != nil {
			log.Warn("website: seed backfill failed", zap.String("step", what), zap.Error(err))
		}
	}
	set, err := s.Settings(ctx)
	if err != nil {
		warn("settings", err)
		return
	}
	if set.SeedVersion >= seedVersion {
		return
	}
	// v3：顶层导航加「首页」，并把出厂的「能力」改名为「产品」（管理员改过则不动）。
	var homes int64
	if err := s.db.WithContext(ctx).Model(&Nav{}).Where("href = ?", "/").Count(&homes).Error; err == nil && homes == 0 {
		home := defaultNavs()[0]
		home.ID = 0
		warn("nav home", s.db.WithContext(ctx).Create(&home).Error)
	}
	var feat Nav
	if err := s.db.WithContext(ctx).Where("href = ? AND label IN ?", "#features", []string{"能力", "产品"}).First(&feat).Error; err == nil {
		// 首页内锚点导航没有意义：删掉出厂的「#features」项（管理员改过名称的保留）。
		warn("nav remove-anchor", s.db.WithContext(ctx).Delete(&Nav{}, "id = ?", int64(feat.ID)).Error)
	}
	now := time.Now().UTC()
	for _, p := range defaultPosts() {
		var n int64
		if err := s.db.WithContext(ctx).Model(&Post{}).Where("slug = ?", p.Slug).Count(&n).Error; err != nil {
			warn("count "+p.Slug, err)
			continue
		}
		if n > 0 {
			continue
		}
		p.ID = 0
		if p.Published && p.PublishedAt == nil {
			p.PublishedAt = &now
		}
		if err := s.db.WithContext(ctx).Create(&p).Error; err != nil {
			warn("insert "+p.Slug, err)
		}
	}
	// v5：新增「联系」页（在线留言），补一条出厂导航。只在升级时补一次，管理员删掉后不再复活。
	var contactNavs int64
	if err := s.db.WithContext(ctx).Model(&Nav{}).Where("href = ?", "/contact").Count(&contactNavs).Error; err == nil && contactNavs == 0 {
		if n, ok := defaultNavByHref("/contact"); ok {
			n.ID = 0
			warn("nav contact", s.db.WithContext(ctx).Create(&n).Error)
		}
	}
	warn("seed_version", s.db.WithContext(ctx).Model(&Setting{}).Where("id = ?", int64(set.ID)).
		Update("seed_version", seedVersion).Error)
}

func labelOfDefaultNav(href string) string {
	for _, n := range defaultNavs() {
		if n.Href == href {
			return n.Label
		}
	}
	return ""
}
