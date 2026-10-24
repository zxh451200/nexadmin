package core

import (
	"context"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"

	"nexadmin/core/apidoc"
	"nexadmin/core/auth"
	"nexadmin/core/cache"
	"nexadmin/core/cluster"
	"nexadmin/core/config"
	"nexadmin/core/database"
	"nexadmin/core/i18n"
	"nexadmin/core/mcp"
	"nexadmin/core/metrics"
	"nexadmin/core/middleware"
	"nexadmin/core/notify"
	"nexadmin/core/openapi"
	"nexadmin/core/response"
	"nexadmin/core/secret"
	"nexadmin/core/setup"
	"nexadmin/core/tenant"
	"nexadmin/pkg/idgen"
)

const contractAPIPrefix = "/api/v1"
const contractAdminPrefix = "/admin"

// Perm 是普通 Gin 中间件：要求登录并拥有权限点，同时把权限点登记到框架。
func Perm(code string) gin.HandlerFunc {
	auth.RegisterPerm(code)
	h := middleware.Perm(code)
	apidoc.RecordPerm(code, h) // 接口文档按中间件实例找回路由的权限码
	return h
}

// Desc 为路由提供面向 OpenAPI、MCP 和 AI 工具目录的简短描述。
func Desc(summary string) gin.HandlerFunc {
	h := func(c *gin.Context) { c.Next() }
	apidoc.RecordDesc(strings.TrimSpace(summary), h)
	return h
}

func (a *App) newEngine() *gin.Engine {
	e := gin.New()
	ConfigureClientIP(e, a.cfg.Server.TrustedProxies, a.log)
	e.Use(metrics.Middleware(), middleware.Recovery(a.log), middleware.Logger(a.log), middleware.CORS(!a.cfg.IsProduction(), a.cfg.Server.CORSOrigins...), middleware.I18n(i18n.Default()))
	return e
}

// ConfigureClientIP 决定 c.ClientIP()（“真实客户端 IP”，限流 / 登录日志 / IP 白名单都用它）的取值：
// trusted 为空 = 不信任任何代理，只用 TCP 对端地址；否则对端地址在 trusted 内时才采信
// X-Forwarded-For / X-Real-IP，并取其中最右一个不在 trusted 内的地址。
// 注意：需要“对端是不是本机”的判定（setup、/metrics、关停接口）一律直接读 RemoteAddr，不要用 ClientIP。
func ConfigureClientIP(e *gin.Engine, trusted []string, log *zap.Logger) {
	e.ForwardedByClientIP = true
	e.RemoteIPHeaders = []string{"X-Forwarded-For", "X-Real-IP"}
	var list []string
	for _, s := range trusted {
		if s = strings.TrimSpace(s); s != "" {
			list = append(list, s)
		}
	}
	if err := e.SetTrustedProxies(list); err != nil { // 配置加载时已校验过格式，这里只是兜底
		if log != nil {
			log.Error("invalid server.trusted_proxies, trusting no proxy", zap.Error(err))
		}
		_ = e.SetTrustedProxies(nil)
	}
}

// commonRoutes 是两种模式都有的公开路由：/healthz、系统状态、语言清单。
func (a *App) commonRoutes(e *gin.Engine, api *gin.RouterGroup, probes []*healthProbe) {
	registerHealth(e, probes, a.log)
	a.registerMetricsRoute(e)
	a.registerShutdown(e)
	api.GET("/system/status", func(c *gin.Context) {
		response.OK(c, gin.H{"initialized": a.initialized.Load()})
	})
	api.GET("/i18n/languages", func(c *gin.Context) {
		response.OK(c, i18n.Default().Languages())
	})
}

// buildSetup 装配“未初始化”模式：只有向导、状态和健康检查，其余 /api 一律 503 NOT_INITIALIZED。
func (a *App) buildSetup(r *setup.Runner) *gin.Engine {
	e := a.newEngine()
	api := e.Group(contractAPIPrefix)
	a.commonRoutes(e, api, nil)
	// 向导阶段没有正式缓存，限流计数放一个进程内的 memory cache。
	if sc, err := cache.New(config.Cache{Driver: "memory"}); err == nil {
		a.addCloser(func() { _ = sc.Close() })
		r.Register(api.Group("/setup", middleware.RateLimitRule(sc, a.log, a.cfg.RateLimit, "setup", middleware.ByIP())))
	} else {
		r.Register(api.Group("/setup"))
	}
	serve := staticHandler(a.static)
	e.NoRoute(func(c *gin.Context) {
		if serve(c) {
			return
		}
		if strings.HasPrefix(c.Request.URL.Path, "/api/") {
			response.Fail(c, response.NotInitialized())
			return
		}
		response.Fail(c, response.NotFound("error.not_found"))
	})
	return e
}

// buildFull 装配完整路由：连库、缓存、鉴权，再逐个注册模块。
// 向导路由在这里根本不注册，所以已初始化后 /setup/* 一律 404。
func (a *App) buildFull() (*gin.Engine, error) {
	cfg := a.cfg
	if err := idgen.Init(int64(cfg.Database.NodeID)); err != nil {
		return nil, err
	}
	// 缓存与 node_id 租约放在连库、迁移之前：迁移 / 补种出厂数据也会生成雪花 ID，要先确认 node_id 没有被别的实例占用。
	ch, err := cache.New(cfg.Cache)
	if err != nil {
		return nil, err
	}
	a.addCloser(func() { _ = ch.Close() })
	ch = metrics.WrapCache(ch) // 命中 / 错误计数；仍实现 cache.Counter
	lease, err := cluster.AcquireNodeLease(context.Background(), ch, cfg.Database.NodeID, a.log)
	if err != nil {
		return nil, err
	}
	a.addCloser(lease.Release) // 后注册先执行：先释放租约，再关缓存
	bus := cluster.New(ch, a.log)
	a.addCloser(bus.Close)
	a.addCloser(cluster.WireTenant(bus))
	db, err := database.Open(cfg.Database)
	if err != nil {
		return nil, err
	}
	a.addCloser(func() { _ = database.Close(db) })
	if err := a.prepareData(db); err != nil { // 迁移 + 补种出厂数据，升级后重启即可用
		return nil, err
	}
	a.registerMetricSources(db)
	if err := secret.InitDefault(cfg.DataDir); err != nil { // 静态加密主密钥：NEXADMIN_SECRET_KEY 或 data/secret.key
		return nil, err
	}
	svc, err := auth.NewService(cfg, ch)
	if err != nil {
		return nil, err
	}
	if a.provider != nil {
		svc.SetProvider(a.provider)
	}
	auth.SetDefault(svc)

	resolver := a.resolver
	if resolver == nil {
		lookup := a.lookup
		if lookup == nil { // 默认装配：按租户 domain 字段查（租户模块注册来源后生效）；WithDomainLookup 可覆盖
			lookup = tenant.DefaultDomainLookup
		}
		resolver = middleware.DefaultTenantResolver(lookup)
	}

	e := a.newEngine()
	api := e.Group(contractAPIPrefix)
	api.Use(middleware.ParseToken(svc), openapi.Authenticate(openapi.Options{
		Store: openapi.GormStore{DB: db}, Cache: ch, Log: a.log,
		Limiter: middleware.NewLimiter(ch, a.log, "openapi-sign", cfg.RateLimit.Rule("openapi_sign").Limit, cfg.RateLimit.Rule("openapi_sign").Window),
	}), middleware.Tenant(resolver), middleware.RuntimeTenant(), mcp.EnforceDispatchIdentity())
	a.commonRoutes(e, api, []*healthProbe{dbHealthProbe(db), cacheHealthProbe(ch), storageHealthProbe(cfg)})

	nf := notify.New(cfg, ch, a.log)
	a.addCloser(nf.Close)
	h := &auth.Handler{Svc: svc, Cfg: cfg, ForgotEnabled: nf.MailConfigured}
	rl := func(rule string, key middleware.KeyFunc) gin.HandlerFunc {
		return middleware.RateLimitRule(ch, a.log, cfg.RateLimit, rule, key)
	}
	api.POST("/auth/login", rl("login", middleware.ByIPAndPath()), h.Login)
	api.GET("/auth/captcha", rl("captcha", middleware.ByIPAndPath()), h.Captcha)
	api.POST("/auth/refresh", rl("refresh", middleware.ByIPAndPath()), h.Refresh)
	api.GET("/auth/methods", h.Methods)
	api.POST("/auth/logout", middleware.RequireLogin(), h.Logout)
	api.GET("/auth/me", middleware.RequireLogin(), h.Me)

	deps := &Deps{
		Config: cfg, DB: db, Cache: ch, Bus: bus, Auth: svc, Log: a.log, Notify: nf,
		Public: api,
		Admin:  api.Group(contractAdminPrefix, middleware.RequireSuper(), middleware.SkipTenant()),
		Root:   e,
		APIDoc: apidoc.New(e, apidoc.Options{Disabled: cfg.DisabledModules}),
	}
	api.GET("/mcp/tools", Perm("mcp:list"), func(c *gin.Context) {
		response.OK(c, gin.H{"tools": deps.APIDoc.Tools()})
	})
	confirmRule := cfg.RateLimit.Rule("mcp_confirm")
	mcpServer := mcp.New(e, deps.APIDoc).WithConfirmation(ch, svc, a.log).WithConfirmLimit(confirmRule.Limit, confirmRule.Window)
	deps.MCP = mcpServer
	a.addCloser(mcpServer.Close)
	// /mcp 按用户限流（ratelimit.mcp）；Perm 在前，限流时才有登录用户可用。
	mcpLimit := rl("mcp", middleware.ByUser())
	api.POST("/mcp", Perm("mcp:list"), mcpLimit, mcpServer.Handle)
	api.GET("/mcp", Perm("mcp:list"), mcpLimit, mcpServer.Handle)
	api.DELETE("/mcp", Perm("mcp:list"), mcpLimit, mcpServer.Handle)
	api.GET("/mcp/confirmations/:id", Perm("mcp:list"), mcpServer.Confirmation)
	// 确认接口：按 IP 限流之外，Approve 内部还按「用户+确认 ID」统计密码失败次数（ratelimit.mcp_confirm），超限作废该确认。
	api.POST("/mcp/confirmations/:id/approve", Perm("mcp:list"),
		rl("login", middleware.ByIPAndPath()), mcpServer.Approve)
	deps.APIDoc.Describe(coreAPIDocs()...)
	deps.OnClose = a.addCloser
	protected := api.Group("", middleware.RequireLogin())
	mods, err := a.activeModules()
	if err != nil {
		return nil, err
	}
	for _, m := range mods {
		m := m
		deps.APIDoc.Track(m.Name(), m, func() { m.Register(protected, deps) })
	}

	serve := staticHandlerWith(a.static, func(c *gin.Context) (SiteInfo, bool) {
		src := siteSource.Load()
		if src == nil {
			return SiteInfo{}, false
		}
		id, ok := resolver(c) // 根路径不经过 /api/v1 的租户中间件，这里按同一解析链取租户
		if !ok {
			id = tenant.DefaultID
		}
		ctx := tenant.With(c.Request.Context(), id)
		ctx = i18n.WithRuntimeTenant(ctx, strconv.FormatInt(int64(id), 10))
		ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
		defer cancel()
		info, err := (*src)(ctx)
		return info, err == nil
	})
	e.NoRoute(func(c *gin.Context) {
		if serve(c) {
			return
		}
		response.Fail(c, response.NotFound("error.not_found"))
	})
	return e, nil
}
