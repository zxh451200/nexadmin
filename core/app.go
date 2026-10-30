// Package core 是 NexAdmin 的框架入口：Module 接口、App、路由装配。
//
// 模块注册约定（框架唯一扩展点）：
//
//	// internal/modules/article/module.go
//	func init() { core.RegisterBuiltin(module{}) }
//	type module struct{}
//	func (module) Name() string { return "article" }
//	func (module) Register(r *gin.RouterGroup, d *core.Deps) {
//	    r.GET("/articles", core.Perm("article:list"), list(d.DB)) // 最终路由 /api/v1/articles
//	}
//
// 然后在 internal/modules/modules.go 里空导入该包（nexadmin add module 会自动维护）。
// 框架内置模块（modules/*）与业务模块（internal/modules/*）都在各自包的 init() 里调用 core.RegisterBuiltin，
// cmd/server/main.go 只空导入这两个汇总包，不需要改。App.Register 仅用于测试或特殊装配。
package core

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
	"gorm.io/gorm"

	"nexadmin/core/apidoc"
	"nexadmin/core/auth"
	"nexadmin/core/cache"
	"nexadmin/core/cluster"
	"nexadmin/core/config"
	"nexadmin/core/ctl"
	"nexadmin/core/i18n"
	"nexadmin/core/mcp"
	"nexadmin/core/migrate"
	"nexadmin/core/notify"
	"nexadmin/core/setup"
	"nexadmin/core/tenant"
)

// Module 是框架的唯一扩展点：一个业务模块 = 一个目录 = 一个 Module。
type Module interface {
	Name() string
	// Register 向 r 注册路由。r 对应 /api/v1 且默认要求登录；匿名接口注册到 d.Public，
	// 超管端接口注册到 d.Admin（/api/v1/admin，跳过租户隔离）。权限用 core.Perm("模块:动作")。
	Register(r *gin.RouterGroup, d *Deps)
}

// Deps 是框架交给模块的依赖。
type Deps struct {
	Config *config.Config
	DB     *gorm.DB
	Cache  cache.Cache
	Auth   *auth.Service
	Log    *zap.Logger
	// Notify 是统一通知入口（站内 / 邮件 / 短信 / 机器人），见 core/notify。
	// nil 安全：测试里手工构造的 Deps 不填也不会 panic，Send 直接空操作。
	Notify *notify.Service
	Public *gin.RouterGroup // /api/v1，不要求登录
	Admin  *gin.RouterGroup // /api/v1/admin，仅超管，跳过租户隔离
	Root   *gin.Engine      // 站点根路由，供标准协议端点使用
	// RootGroup 是根引擎上带插件授权闸门的分组（"" 前缀）：插件要在根路径提供公开页面
	// （如官网插件的 /、/news）时必须用它注册，而不是直接用 Root（后者不过授权闸门，
	// 且热挂载时运行中的引擎不允许加路由）。启动挂载时由插件管理器创建；热挂载时为 nil，
	// 插件应跳过根路径注册（授权导入后重启生效）。
	RootGroup *gin.RouterGroup
	// OnClose 登记服务停止时要执行的清理函数（后注册的先执行），后台 goroutine 型模块用它优雅退出。
	OnClose func(func())
	// Bus 是跨实例事件广播（基于 core/cache 发布订阅）。nil 安全：测试里手工构造的 Deps 不填也不会 panic，
	// 所有方法都是空操作；memory 缓存（单实例）下事件也不会离开本进程。
	Bus *cluster.Bus
	// APIDoc 是运行时接口文档（路由清单 → OpenAPI），见 core/apidoc。
	APIDoc *apidoc.Doc
	// MCP is the router-owned server; confirmations must use its original dispatch chain.
	MCP *mcp.Server
	// Modules 是本次启动启用的模块（已按 disabled_modules 过滤），供插件按接口发现业务侧扩展
	// （例如导入中心收集实现了 imports.SourceProvider 的模块）。只暴露 Module 接口。
	Modules []Module
	// PluginActive 由插件管理器在传给插件的 Deps 副本中注入，实时判断当前插件是否可用。
	// 非 HTTP 扩展点应在执行前及成功返回前检查；Register / Start 装配期间不要调用。
	// nil 表示手工装配时未提供 guard，插件可保留原有行为；生产管理器始终注入。
	PluginActive func() bool
}

// Option 用于定制框架行为，而不是修改 core/。
type Option func(*App)

// WithTenantResolver 整体替换租户解析链。
func WithTenantResolver(r tenant.Resolver) Option { return func(a *App) { a.resolver = r } }

// WithDomainLookup 提供域名到租户的映射，启用默认解析链里的“子域名”一级。
func WithDomainLookup(l tenant.DomainLookup) Option { return func(a *App) { a.lookup = l } }

// WithAuth 注册登录提供者（也可以让某个模块直接实现 auth.Provider）。
func WithAuth(p auth.Provider) Option { return func(a *App) { a.provider = p } }

var builtin []Module
var processInstanceID = ctl.NewToken()

func InstanceID() string { return processInstanceID }

// RegisterBuiltin 由框架内置模块（modules/*）和业务模块（internal/modules/*）在 init() 里调用。
// 模块名全局唯一，重名启动失败；停用某个模块用 config 的 disabled_modules（按 Name() 匹配）。
func RegisterBuiltin(m Module) { builtin = append(builtin, m) }

// LoadConfig 读取 default.yaml + config.yaml。
func LoadConfig(defaultPath, userPath string) (*config.Config, error) {
	return config.Load(defaultPath, userPath)
}

// App 是一个运行中的应用。
type App struct {
	cfg      *config.Config
	log      *zap.Logger
	mods     []Module
	resolver tenant.Resolver
	lookup   tenant.DomainLookup
	provider auth.Provider
	hooks    setup.Hooks
	static   fs.FS // 前端构建产物（nil = 不托管，见 static.go）

	startupSeeders []setup.StartupSeeder // 每次启动补缺失出厂项的种子
	migSources     []migrate.Source      // 模块（已授权的插件）声明的迁移目录

	engine      atomic.Pointer[gin.Engine]
	initialized atomic.Bool

	shutdownToken string // 非空 = 注册本机关停接口（见 shutdown.go）
	stopc         chan struct{}
	stopOnce      sync.Once

	mu      sync.Mutex
	closers []func()
}

// New 创建应用。
func New(cfg *config.Config, opts ...Option) *App {
	a := &App{cfg: cfg, stopc: make(chan struct{})}
	a.hooks.Migrate = a.runMigrations
	for _, o := range opts {
		o(a)
	}
	return a
}

// Register 手动注册模块（测试或特殊装配用）；业务模块请在自己包的 init() 里 RegisterBuiltin，不要改 main.go。
func (a *App) Register(mods ...Module) { a.mods = append(a.mods, mods...) }

// activeModules 合并内置与业务模块，去掉 disabled_modules，并拒绝重名。
func (a *App) activeModules() ([]Module, error) {
	disabled := map[string]bool{}
	for _, n := range a.cfg.DisabledModules {
		disabled[n] = true
	}
	seen := map[string]bool{}
	var out []Module
	for _, m := range append(append([]Module{}, builtin...), a.mods...) {
		name := m.Name()
		if disabled[name] {
			continue
		}
		if seen[name] {
			return nil, fmt.Errorf("core: duplicate module name %q", name)
		}
		seen[name] = true
		out = append(out, m)
	}
	return out, nil
}

// collect 扫描模块实现的可选接口：登录提供者与初始化钩子。
func (a *App) collect(mods []Module) {
	for _, m := range mods {
		if p, ok := m.(auth.Provider); ok && a.provider == nil {
			a.provider = p
		}
		if v, ok := m.(migrate.SourceProvider); ok {
			a.migSources = append(a.migSources, v.MigrationSources()...)
		}
		if v, ok := m.(setup.Seeder); ok {
			a.hooks.Seeders = append(a.hooks.Seeders, v)
		}
		if v, ok := m.(setup.StartupSeeder); ok {
			a.startupSeeders = append(a.startupSeeders, v)
		}
		if v, ok := m.(setup.AdminCreator); ok && a.hooks.AdminCreator == nil {
			a.hooks.AdminCreator = v
		}
		if v, ok := m.(setup.AdminChecker); ok && a.hooks.AdminChecker == nil {
			a.hooks.AdminChecker = v
		}
		if v, ok := m.(setup.SiteWriter); ok && a.hooks.SiteWriter == nil {
			a.hooks.SiteWriter = v
		}
	}
}

func (a *App) addCloser(f func()) {
	a.mu.Lock()
	a.closers = append(a.closers, f)
	a.mu.Unlock()
}

func (a *App) closeAll() {
	a.mu.Lock()
	defer a.mu.Unlock()
	for i := len(a.closers) - 1; i >= 0; i-- {
		a.closers[i]()
	}
	a.closers = nil
}

func (a *App) loadI18n() error {
	b, err := i18n.New(a.cfg.Languages, a.cfg.DefaultLanguage(), a.cfg.LocalesDir)
	if err != nil {
		return err
	}
	i18n.SetDefault(b)
	return nil
}

// ServeHTTP 让 App 可作为 http.Handler 使用（也方便测试）。
func (a *App) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if e := a.engine.Load(); e != nil {
		e.ServeHTTP(w, r)
		return
	}
	http.Error(w, "starting", http.StatusServiceUnavailable)
}

// Run 启动服务并阻塞，直到收到 SIGINT / SIGTERM。
// 已初始化：装配完整路由。未初始化：只开放向导与状态接口，业务 API 统一 503 NOT_INITIALIZED，
// 向导成功后原地切换为完整路由，无需重启。
func (a *App) Run() error {
	a.shutdownToken = os.Getenv(ctl.TokenEnv)
	_ = os.Unsetenv(ctl.TokenEnv) // 不让令牌留在进程环境里被子进程继承
	log, err := newLogger(a.cfg)
	if err != nil {
		return err
	}
	a.log = log
	defer log.Sync() //nolint:errcheck
	if a.cfg.IsProduction() {
		gin.SetMode(gin.ReleaseMode)
	}

	if err := a.loadI18n(); err != nil {
		return err
	}
	mods, err := a.activeModules()
	if err != nil {
		return err
	}
	a.collect(mods)

	if setup.IsInitialized(a.cfg, a.hooks.AdminChecker, log) {
		eng, err := a.buildFull()
		if err != nil {
			a.closeAll()
			return err
		}
		a.initialized.Store(true)
		a.engine.Store(eng)
	} else {
		runner := setup.NewRunner(a.cfg, a.hooks, log, a.activate)
		a.engine.Store(a.buildSetup(runner))
		runner.PrintBanner(os.Stdout)
	}

	srv := &http.Server{
		Addr:              ":" + strconv.Itoa(a.cfg.Server.Port),
		Handler:           a,
		ReadHeaderTimeout: 10 * time.Second,
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	errc := make(chan error, 1)
	go func() { errc <- srv.ListenAndServe() }()
	log.Info("server listening", zap.String("addr", srv.Addr), zap.Bool("initialized", a.initialized.Load()))

	select {
	case err := <-errc:
		a.closeAll()
		// 监听失败（十有八九是端口被占用）时给出可操作的指引，而不是裸的 net/http 错误。
		if errors.Is(err, net.ErrClosed) {
			return err
		}
		var oe *net.OpError
		if errors.As(err, &oe) && oe.Op == "listen" {
			log.Error("server listen failed",
				zap.Int("port", a.cfg.Server.Port),
				zap.String("port_source", "config.yaml 的 server.port（可用环境变量 NEXADMIN_SERVER_PORT 覆盖）"),
				zap.Error(err))
			return fmt.Errorf("监听端口 %d 失败：端口可能已被其他进程占用。可修改 config/config.yaml 的 server.port，或设置环境变量 NEXADMIN_SERVER_PORT 后重试（原始错误：%w）",
				a.cfg.Server.Port, err)
		}
		return err
	case <-ctx.Done():
	case <-a.stopc:
	}
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	err = srv.Shutdown(shutdownCtx)
	a.closeAll()
	if err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

// activate 在向导成功后被调用：重新读取配置，装配完整路由并原子切换。
func (a *App) activate() error {
	cfg, err := config.Load(a.cfg.DefaultConfigPath, a.cfg.UserConfigPath)
	if err != nil {
		return err
	}
	a.cfg = cfg
	if err := a.loadI18n(); err != nil {
		return err
	}
	eng, err := a.buildFull()
	if err != nil {
		a.closeAll()
		return err
	}
	a.initialized.Store(true)
	a.engine.Store(eng)
	return nil
}
