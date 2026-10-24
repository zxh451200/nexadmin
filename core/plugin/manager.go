package plugin

import (
	"context"
	"crypto/ed25519"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"

	"nexadmin/core"
	"nexadmin/core/middleware"
	"nexadmin/core/response"
	"nexadmin/core/seed"
	"nexadmin/core/tenant"
)

// 错误。
var (
	ErrNotFound = errors.New("plugin: not found")
	ErrFree     = errors.New("plugin: free edition needs no license")
)

// Options 定制 Manager；零值即生产配置。
type Options struct {
	Root      string            // 项目根目录（含 plugins/ 与 data/），默认当前目录
	PublicKey ed25519.PublicKey // 单把授权公钥（测试用）；设置后不再使用内置公钥集合
	Keys      []Key             // 授权公钥集合；默认内置公钥 + config/license-keys.pub
	Now       func() time.Time  // 默认 time.Now

	// FrameworkVersion 用于校验 plugin.yaml 的 requires_nexadmin；通常是 -ldflags 注入的 version。
	// 空或不可解析（如 "dev"）时回落到 <Root>/.nexadmin.manifest.json 的 framework_version，仍取不到则不校验。
	FrameworkVersion string

	Hosts         []string      // 当前服务的域名；nil = 从配置读取（site.domain(s) / sso.issuer 的 host）
	AllowDevHosts bool          // 等同配置 license.allow_dev_hosts：localhost / 127.0.0.1 视为匹配
	CheckEvery    time.Duration // 到期检查间隔，默认 1 小时
}

// expireSoonWindow 是“即将到期”的提示窗口。
const expireSoonWindow = 30 * 24 * time.Hour

// Info 是“插件管理”页的列表项。
type Info struct {
	ID          string  `json:"id"`
	Name        string  `json:"name"`
	Version     string  `json:"version"`
	Edition     Edition `json:"edition"`
	Description string  `json:"description"`
	Status      string  `json:"status"` // active | unlicensed | expired | invalid | revoked
	Customer    string  `json:"customer"`
	Serial      string  `json:"serial,omitempty"` // 授权序列号；旧授权没有
	ExpiresAt   string  `json:"expiresAt"`
	ExpiresSoon bool    `json:"expiresSoon"` // 有效但 30 天内到期
	Reason      string  `json:"reason"`
}

// ImportResult 是导入授权的结果。
type ImportResult struct {
	Plugin      Info `json:"plugin"`
	NeedRestart bool `json:"needRestart"`
}

type entry struct {
	p      Plugin
	man    *Manifest
	info   Info
	fatal  bool                       // 清单/注册信息有问题，授权解决不了
	viaApp bool                       // 启动时已 app.Register（路由在主引擎上）
	live   bool                       // viaApp 的路由已注册成功
	sub    atomic.Pointer[gin.Engine] // 热挂载后的子引擎

	exp         time.Time // 授权到期时间（零值 = 无授权）
	domains     []string  // 授权域名（空 = 不限）
	pendingHost bool      // 授权限定了域名，但当前域名还未知，等第一次带 Host 的请求
	serial      string    // 授权序列号（空 = 旧授权或无授权，不能被吊销清单命中）
}

// Manager 管理所有已 Register 的插件。
type Manager struct {
	mu      sync.Mutex
	opts    Options
	entries map[string]*entry
	order   []string
	deps    *core.Deps
	ctx     context.Context
	stop    context.CancelFunc
	host    string // 第一次请求观察到的域名（配置里没有域名时使用）

	// 吊销清单（见 revoke.go）：revBy 是当前已验签采信的清单，revSeq 是它的版本，revFP 是清单文件上次处理时的指纹。
	revBy        map[string]RevokedEntry
	revSeq       int64
	revFP        string
	pendingWarns []pendingWarn // Attach 之前产生的告警，Attach 时补发
}

var (
	curMu sync.Mutex
	cur   *Manager
)

// Default 返回 Mount 创建的 Manager；没调用过 Mount 时返回一个空的 Manager。
func Default() *Manager {
	curMu.Lock()
	defer curMu.Unlock()
	if cur == nil {
		cur = newManager(nil, Options{})
	}
	return cur
}

// Mount 在 main.go 里调用（app.Run 之前）：逐个评估已 Register 的插件，
// free 直接挂载，pro 授权有效才挂载；无效的只记录状态，不挂载。
func Mount(app *core.App, opts ...Options) *Manager {
	var o Options
	if len(opts) > 0 {
		o = opts[0]
	}
	m := newManager(registered(), o)
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	m.ctx, m.stop = ctx, stop
	for _, id := range m.order {
		if e := m.entries[id]; e.info.Status == StatusActive {
			e.viaApp = true
			app.Register(&mounted{m: m, e: e})
		}
	}
	curMu.Lock()
	cur = m
	curMu.Unlock()
	go m.watch(ctx, m.opts.CheckEvery)
	return m
}

func newManager(plugins []Plugin, o Options) *Manager {
	if o.Root == "" {
		o.Root = "."
	}
	if len(o.Keys) == 0 {
		if o.PublicKey != nil {
			o.Keys = []Key{{KID: KeyID(o.PublicKey), Pub: o.PublicKey}}
		} else {
			o.Keys = PublicKeys(o.Root)
		}
	}
	if o.Now == nil {
		o.Now = time.Now
	}
	o.FrameworkVersion = FrameworkVersion(o.Root, o.FrameworkVersion)
	if o.CheckEvery <= 0 {
		o.CheckEvery = time.Hour
	}
	if o.Hosts == nil {
		hc := loadHostConfig(o.Root)
		o.Hosts = hc.Hosts
		o.AllowDevHosts = o.AllowDevHosts || hc.AllowDevHosts
	}
	for i, h := range o.Hosts {
		o.Hosts[i] = normalizeHost(h)
	}
	ctx, stop := context.WithCancel(context.Background())
	m := &Manager{opts: o, entries: map[string]*entry{}, ctx: ctx, stop: stop}
	m.refreshRevocationsLocked() // 先载入吊销清单，再评估各插件的授权
	for _, p := range plugins {
		m.evaluate(p)
	}
	return m
}

// evaluate 决定一个插件启动时的状态。
func (m *Manager) evaluate(p Plugin) {
	e := &entry{p: p, info: Info{ID: p.ID, Name: p.Name, Version: p.Version, Edition: p.Edition}}
	if _, dup := m.entries[p.ID]; dup {
		e.fatal = true
		e.info.Status, e.info.Reason = StatusInvalid, "插件 id 重复注册"
		m.order = append(m.order, p.ID+"#dup")
		m.entries[p.ID+"#dup"] = e
		return
	}
	m.order = append(m.order, p.ID)
	m.entries[p.ID] = e

	bad := func(format string, a ...any) {
		e.fatal = true
		e.info.Status, e.info.Reason = StatusInvalid, fmt.Sprintf(format, a...)
	}
	if !ValidID(p.ID) {
		bad("插件 id %q 不合法", p.ID)
		return
	}
	if p.Module == nil {
		bad("未提供 Module")
		return
	}
	man, err := LoadManifest(filepath.Join(m.opts.Root, "plugins", p.ID))
	if err != nil {
		bad("%v", err)
		return
	}
	e.man = man
	if man.ID != p.ID {
		bad("plugin.yaml 的 id %q 与注册的 id %q 不一致", man.ID, p.ID)
		return
	}
	if man.Edition != p.Edition {
		bad("plugin.yaml 的 edition %q 与注册的 %q 不一致", man.Edition, p.Edition)
		return
	}
	if err := CheckRequires(man.RequiresNexadmin, m.opts.FrameworkVersion); err != nil {
		// 不挂载（状态 invalid），原因显示在插件管理页；授权再有效也没用，所以标 fatal。
		if e.info.Name == "" {
			e.info.Name = man.Name
		}
		if e.info.Version == "" {
			e.info.Version = man.Version
		}
		bad("%v", err)
		return
	}
	if e.info.Name == "" {
		e.info.Name = man.Name
	}
	if e.info.Version == "" {
		e.info.Version = man.Version
	}
	e.info.Description = man.Description

	switch p.Edition {
	case Free:
		e.info.Status = StatusActive
	case Pro:
		l, err := checkFile(m.opts.Root, p.ID, m.opts.Now(), m.opts.Keys)
		m.applyLic(e, l, err)
	default:
		bad("未知的 edition %q", p.Edition)
	}
}

// applyLicense 把校验结果写进 e.info。
func applyLicense(e *entry, l *License, err error) {
	e.info.Customer, e.info.ExpiresAt = "", ""
	if l != nil {
		e.info.Customer, e.info.ExpiresAt = l.Customer, l.ExpiresAt
	}
	if err == nil {
		e.info.Status, e.info.Reason = StatusActive, ""
		return
	}
	var le *LicenseError
	if errors.As(err, &le) {
		e.info.Status, e.info.Reason = le.Status, le.Reason
		return
	}
	e.info.Status, e.info.Reason = StatusInvalid, err.Error()
}

func (m *Manager) log() *zap.Logger {
	if m.deps != nil && m.deps.Log != nil {
		return m.deps.Log
	}
	return zap.NewNop()
}

// List 返回所有插件的状态。
func (m *Manager) List() []Info {
	m.mu.Lock()
	defer m.mu.Unlock()
	now := m.opts.Now()
	m.refreshRevocationsLocked()
	m.expireLocked(now)
	out := make([]Info, 0, len(m.order))
	for _, k := range m.order {
		e := m.entries[k]
		i := e.info
		i.ExpiresSoon = i.Status == StatusActive && !e.exp.IsZero() && e.exp.Sub(now) <= expireSoonWindow
		out = append(out, i)
	}
	return out
}

// applyLic 在 applyLicense 之外记录到期时间、授权域名与序列号，做域名校验，最后对照吊销清单。
// 调用方需持有 m.mu（或在构造阶段）。
func (m *Manager) applyLic(e *entry, l *License, err error) {
	m.applyLic0(e, l, err)
	e.serial = ""
	if l != nil {
		e.serial = l.Serial
	}
	e.info.Serial = e.serial
	m.applyRevokedLocked(e)
}

func (m *Manager) applyLic0(e *entry, l *License, err error) {
	applyLicense(e, l, err)
	e.exp, e.domains, e.pendingHost = time.Time{}, nil, false
	if l != nil {
		if t, perr := time.Parse(time.RFC3339, l.ExpiresAt); perr == nil {
			e.exp = t
		}
		e.domains = l.Domains
	}
	if err != nil || len(e.domains) == 0 {
		return
	}
	hosts := m.hostsLocked()
	if len(hosts) == 0 {
		e.pendingHost = true // 第一次带 Host 的请求时再判定
		return
	}
	if !m.hostsMatch(e.domains, hosts) {
		e.info.Status, e.info.Reason = StatusInvalid, domainMismatch(hosts, e.domains)
	}
}

func domainMismatch(hosts, domains []string) string {
	return fmt.Sprintf("授权域名不匹配：当前域名 %s 不在授权域名 %s 内", strings.Join(hosts, ","), strings.Join(domains, ","))
}

// hostsLocked 返回当前服务的域名：配置里的优先，否则是第一次请求观察到的。
func (m *Manager) hostsLocked() []string {
	if len(m.opts.Hosts) > 0 {
		return m.opts.Hosts
	}
	if m.host != "" {
		return []string{m.host}
	}
	return nil
}

// hostsMatch 判断是否有任一当前域名匹配授权域名（allow_dev_hosts 时本机地址视为匹配）。
func (m *Manager) hostsMatch(domains, hosts []string) bool {
	for _, h := range hosts {
		if m.opts.AllowDevHosts && isDevHost(h) {
			return true
		}
		if MatchDomain(domains, h) {
			return true
		}
	}
	return false
}

// ObserveHost 在配置里没有域名时，用第一次带 Host 的请求判定待定的域名授权。
func (m *Manager) ObserveHost(host string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.observeLocked(host)
}

func (m *Manager) observeLocked(host string) {
	if len(m.opts.Hosts) > 0 || m.host != "" {
		return
	}
	h := normalizeHost(host)
	if h == "" {
		return
	}
	m.host = h
	for _, e := range m.entries {
		if !e.pendingHost {
			continue
		}
		e.pendingHost = false
		if e.info.Status == StatusActive && !m.hostsMatch(e.domains, []string{h}) {
			e.info.Status, e.info.Reason = StatusInvalid, domainMismatch([]string{h}, e.domains)
		}
	}
}

// expireLocked 把已到期的 active 插件标为 expired，返回是否有变化。调用方需持有 m.mu。
func (m *Manager) expireLocked(now time.Time) bool {
	changed := false
	for _, e := range m.entries {
		if e.info.Status == StatusActive && !e.exp.IsZero() && !now.Before(e.exp) {
			e.info.Status, e.info.Reason = StatusExpired, "授权已于 "+e.info.ExpiresAt+" 到期"
			m.log().Warn("plugin license expired, routes disabled", zap.String("plugin", e.p.ID))
			changed = true
		}
	}
	return changed
}

// checkExpiry 做一次到期检查，并顺带检查吊销清单文件有没有变化。
func (m *Manager) checkExpiry() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.refreshRevocationsLocked()
	m.expireLocked(m.opts.Now())
}

// watch 定时检查到期与吊销清单（很轻：比较内存里的时间 + 一次 stat），ctx 结束时退出。
func (m *Manager) watch(ctx context.Context, every time.Duration) {
	if every <= 0 {
		every = time.Hour
	}
	t := time.NewTicker(every)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			m.checkExpiry()
		}
	}
}

// admit 判断请求能否进入插件路由：先用请求域名判定待定的域名授权，再检查吊销清单与到期；
// 状态为 expired / invalid / unlicensed / revoked 时返回 403。
func (m *Manager) admit(c *gin.Context, e *entry) bool {
	m.mu.Lock()
	m.observeLocked(c.Request.Host)
	m.refreshRevocationsLocked()
	m.expireLocked(m.opts.Now())
	st, reason := e.info.Status, e.info.Reason
	m.mu.Unlock()
	switch st {
	case StatusExpired, StatusInvalid, StatusUnlicensed, StatusRevoked:
		response.Fail(c, response.Forbidden("error.forbidden").WithMsg(reason))
		return false
	}
	return true
}

// gate 是挂在插件路由组上的中间件。
func (m *Manager) gate(e *entry) gin.HandlerFunc {
	return func(c *gin.Context) {
		if m.admit(c, e) {
			c.Next()
		}
	}
}

// activeGuard lets registered authenticators and workers recheck their license
// outside plugin routes. It grants nothing until the domain has been resolved.
func (m *Manager) activeGuard(e *entry) func() bool {
	return func() bool {
		m.mu.Lock()
		defer m.mu.Unlock()
		m.refreshRevocationsLocked()
		m.expireLocked(m.opts.Now())
		return !e.fatal && !e.pendingHost && e.info.Status == StatusActive
	}
}

// Attach 保存框架依赖，热挂载需要它。由“插件管理”模块在 Register 时调用。
func (m *Manager) Attach(d *core.Deps) {
	m.mu.Lock()
	m.deps = d
	m.flushWarnsLocked()
	m.mu.Unlock()
}

// Routes 返回需要在路由上预留位置的插件：viaApp 的插件路由自己注册，授权接口单独挂；
// 其余（启动时未挂载的）统一走 /plugins/<id>/* 的转发，授权后可以热挂载。
func (m *Manager) Routes() (viaApp, dormant []string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, k := range m.order {
		e := m.entries[k]
		if e.p.ID != k || e.fatal {
			continue
		}
		if e.viaApp {
			viaApp = append(viaApp, k)
		} else if e.p.Edition == Pro {
			dormant = append(dormant, k)
		}
	}
	return viaApp, dormant
}

// mounted 是交给 app.Register 的包装模块：注册插件自己的路由，最后调 Starter。
// 它同时实现 migrate.SourceProvider 与 setup.StartupSeeder，所以迁移和菜单随 core 的启动流程执行。
type mounted struct {
	m *Manager
	e *entry
}

func (w *mounted) Name() string { return w.e.p.Module.Name() }

func (w *mounted) Register(r *gin.RouterGroup, d *core.Deps) {
	m, e := w.m, w.e
	m.Attach(d) // 迁移与菜单已由 core 在启动时统一完成（见 MigrationSources / SeedOnStartup）
	// 插件路由都挂在带 gate 的子分组上：授权到期 / 域名不匹配后立即 403，不用等重启。
	gate := m.gate(e)
	dd := *d
	dd.PluginActive = m.activeGuard(e)
	if d.Public != nil {
		dd.Public = d.Public.Group("", gate)
	}
	if d.Admin != nil {
		dd.Admin = d.Admin.Group("", gate)
	}
	e.p.Module.Register(r.Group("", gate), &dd)
	m.mu.Lock()
	e.live = true
	m.mu.Unlock()
	m.start(e, &dd)
}

// prepare 在运行中热挂载插件（授权文件刚导入）时执行迁移并补菜单；启动时这两步由 core 统一完成。
func (m *Manager) prepare(d *core.Deps, e *entry) error {
	if err := RunMigrations(d.DB, m.opts.Root, e.p.ID); err != nil {
		return fmt.Errorf("迁移失败: %w", err)
	}
	rep := &seed.Report{}
	db := d.DB.WithContext(seed.WithReport(tenant.Skip(context.Background()), rep))
	if err := SeedMenus(db, e.man.Menus); err != nil {
		return fmt.Errorf("写入菜单失败: %w", err)
	}
	if n := rep.Menus(); n > 0 {
		d.Log.Info(fmt.Sprintf("插件 %s 新增 %d 个菜单，请到角色管理分配", e.p.ID, n))
	}
	if sk := rep.Skipped(); len(sk) > 0 {
		d.Log.Warn("插件菜单的父节点不存在，已跳过", zap.String("plugin", e.p.ID), zap.Strings("menus", sk))
	}
	return nil
}

// start 调用可选的 Starter；panic 与错误只记日志，不影响宿主。
func (m *Manager) start(e *entry, d *core.Deps) {
	s, ok := e.p.Module.(Starter)
	if !ok {
		return
	}
	defer func() {
		if r := recover(); r != nil {
			d.Log.Error("plugin Start panic", zap.String("plugin", e.p.ID), zap.Any("panic", r))
		}
	}()
	if err := s.Start(m.ctx, d); err != nil {
		d.Log.Error("plugin Start failed", zap.String("plugin", e.p.ID), zap.Error(err))
	}
}

type keysCtxKey struct{}

// hotMount 在运行中的进程里挂载插件：gin 不允许给运行中的引擎加路由，
// 所以插件路由注册到一个独立的子引擎上，由 /plugins/<id>/* 的转发处理（登录、租户、权限仍由主引擎的中间件完成）。
func (m *Manager) hotMount(e *entry) (err error) {
	d := m.deps
	if d == nil {
		return errors.New("框架尚未就绪")
	}
	if err := m.prepare(d, e); err != nil {
		return err
	}
	return m.buildSub(e, d)
}

// buildSub 创建子引擎、注册插件路由、调 Starter。
func (m *Manager) buildSub(e *entry, d *core.Deps) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("挂载时 panic: %v", r)
		}
	}()
	sub := gin.New()
	// 子引擎默认信任所有代理（gin 的默认值），必须与主引擎采用同一份 server.trusted_proxies，否则插件路由里的 ClientIP 可被伪造。
	var trusted []string
	if d.Config != nil {
		trusted = d.Config.Server.TrustedProxies
	}
	core.ConfigureClientIP(sub, trusted, d.Log)
	sub.Use(func(c *gin.Context) {
		if keys, ok := c.Request.Context().Value(keysCtxKey{}).(map[string]any); ok {
			for k, v := range keys {
				c.Set(k, v)
			}
		}
		c.Next()
	})
	sub.NoRoute(func(c *gin.Context) { response.Fail(c, response.NotFound("error.not_found")) })
	grp := sub.Group("/api/v1")
	dd := *d
	dd.PluginActive = m.activeGuard(e)
	dd.Public = grp // 热挂载的路由都在登录之后
	dd.Admin = grp.Group("/admin", middleware.RequireSuper(), middleware.SkipTenant())
	e.p.Module.Register(grp, &dd)
	if d.APIDoc != nil {
		d.APIDoc.AddEngine(e.p.Module.Name(), sub) // 热挂载后接口文档立即包含这些路由
	}
	m.start(e, &dd)
	e.sub.Store(sub)
	return nil
}

// Dispatch 把请求交给热挂载的子引擎。
func (m *Manager) Dispatch(c *gin.Context, id string) {
	m.mu.Lock()
	e := m.entries[id]
	m.mu.Unlock()
	var sub *gin.Engine
	if e != nil {
		sub = e.sub.Load()
	}
	if sub == nil {
		// 启动时就已被吊销的插件从未挂载过；同样返回 403 和原因，而不是笼统的“未启用”。
		if e != nil {
			m.mu.Lock()
			m.refreshRevocationsLocked()
			st, reason := e.info.Status, e.info.Reason
			m.mu.Unlock()
			if st == StatusRevoked {
				response.Fail(c, response.Forbidden("error.forbidden").WithMsg(reason))
				return
			}
		}
		response.Fail(c, response.NotFound("error.not_found").WithMsg("插件未启用"))
		return
	}
	if !m.admit(c, e) {
		return
	}
	keys := make(map[string]any, len(c.Keys))
	for k, v := range c.Keys {
		keys[k] = v
	}
	c.Request = c.Request.WithContext(context.WithValue(c.Request.Context(), keysCtxKey{}, keys))
	sub.HandleContext(c)
}

// ImportLicense 校验并保存授权文件，随后尽量立即生效（热挂载）。
// 校验失败不落盘；返回 *LicenseError。
func (m *Manager) ImportLicense(id, content string) (ImportResult, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	e, ok := m.entries[id]
	if !ok || e.p.ID != id {
		return ImportResult{}, ErrNotFound
	}
	if e.p.Edition != Pro {
		return ImportResult{}, ErrFree
	}
	l, err := VerifyKeys([]byte(content), id, m.opts.Now(), m.opts.Keys)
	if err != nil {
		return ImportResult{}, err
	}
	m.refreshRevocationsLocked()
	if r, ok := m.revBy[l.Serial]; ok && l.Serial != "" { // 已吊销的授权不落盘
		return ImportResult{}, revokedError(r)
	}
	if hosts := m.hostsLocked(); len(l.Domains) > 0 && len(hosts) > 0 && !m.hostsMatch(l.Domains, hosts) {
		return ImportResult{}, invalid("%s", domainMismatch(hosts, l.Domains))
	}
	if err := writeLicense(m.opts.Root, id, []byte(content)); err != nil {
		return ImportResult{}, fmt.Errorf("保存授权文件失败: %w", err)
	}
	if e.fatal {
		return ImportResult{Plugin: e.info, NeedRestart: true}, nil
	}
	if e.viaApp {
		if e.live || e.info.Status == StatusActive { // 续期：路由已在，只更新授权信息
			m.applyLic(e, l, nil)
			return ImportResult{Plugin: e.info}, nil
		}
		return ImportResult{Plugin: e.info, NeedRestart: true}, nil
	}
	if e.sub.Load() != nil {
		m.applyLic(e, l, nil)
		return ImportResult{Plugin: e.info}, nil
	}
	if err := m.hotMount(e); err != nil {
		m.log().Error("plugin hot mount failed", zap.String("plugin", id), zap.Error(err))
		e.info.Status, e.info.Reason = StatusInvalid, "授权已保存，但热挂载失败（重启后重试）: "+err.Error()
		return ImportResult{Plugin: e.info, NeedRestart: true}, nil
	}
	m.applyLic(e, l, nil)
	return ImportResult{Plugin: e.info}, nil
}
