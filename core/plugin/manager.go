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
	"nexadmin/core/i18n"
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

	// DisabledPlugins 是停用的插件 id 列表（等同配置 disabled_plugins）：命中的插件完全不挂载，
	// 插件管理页显示 disabled。nil/空 = 从配置读取。free 插件没有授权闸门，停用走这里。
	DisabledPlugins []string
}

// RuntimeRoutes 声明静态路由需求；未实现或返回 true 的插件只在启动时挂载。
// 返回 false 的插件只能在 /api/v1/plugins/<id>/ 下注册路由，不得修改宿主路由。
type RuntimeRoutes interface {
	RequiresRootRoutes() bool
}

// RestartableStarter 的 Done 必须在 Start 返回后可用，且仅在本轮所有任务退出后关闭。
// ctx 取消后任务必须停止接收新工作并取消在途工作；未提供退出确认的 Starter 不会再次启动。
type RestartableStarter interface {
	Starter
	Done() <-chan struct{}
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
	// ReasonKey/ReasonArgs 是 Reason 的字典来源；List(ctx) 会按语言填好 Reason 再返回。
	ReasonKey   string `json:"-"`
	ReasonArgs  []any  `json:"-"`
	NeedRestart bool   `json:"needRestart"`
	PendingHost bool   `json:"pendingHost"`
	Stopping    bool   `json:"stopping"`
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

	hotSafe   bool
	mounting  bool
	startDeps *core.Deps
	run       *pluginRun

	exp         time.Time // 授权到期时间（零值 = 无授权）
	domains     []string  // 授权域名（空 = 不限）
	pendingHost bool      // 授权限定了域名，但当前域名还未知，等第一次带 Host 的请求
	serial      string    // 授权序列号（空 = 旧授权或无授权，不能被吊销清单命中）
}

type pluginRun struct {
	ctx    context.Context
	cancel context.CancelFunc
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
	revSrc       revocationSource // 吊销清单的探测/读取，可注入以便单测；默认读磁盘
	revPending   string           // 正在锁外验签的清单指纹（"" = 无），用于让并发 admit 等待生效结果
	revPendingCh chan struct{}    // revPending 完成（成功或忽略）后关闭，唤醒等待者
	pendingWarns []pendingWarn    // Attach 之前产生的告警，Attach 时补发

	closeOnce sync.Once // 把插件 ctx 取消登记进框架关闭钩子，只做一次
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
	// 在基础 ctx 之上叠加信号取消：收到 SIGINT/SIGTERM 时 m.ctx 取消（原有行为），
	// m.stop 同时取消信号 ctx 与基础 ctx，供框架关闭钩子（见 Attach）在程序内关闭时调用。
	sigCtx, stopSig := signal.NotifyContext(m.ctx, os.Interrupt, syscall.SIGTERM)
	base := m.stop
	m.ctx = sigCtx
	m.stop = sync.OnceFunc(func() { stopSig(); base() })
	for _, id := range m.order {
		if e := m.entries[id]; e.info.Status == StatusActive {
			e.viaApp = true
			app.Register(&mounted{m: m, e: e})
		}
	}
	curMu.Lock()
	cur = m
	curMu.Unlock()
	go m.watch(m.ctx, m.opts.CheckEvery)
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
	if len(o.DisabledPlugins) == 0 {
		o.DisabledPlugins = LoadDisabledPlugins(o.Root)
	}
	ctx, cancel := context.WithCancel(context.Background())
	m := &Manager{opts: o, entries: map[string]*entry{}, ctx: ctx, stop: sync.OnceFunc(cancel), revSrc: &osRevocationSource{path: RevocationsPath(o.Root)}}
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
		e.info.Status, e.info.ReasonKey = StatusInvalid, "plugin.reason.duplicate_id"
		m.order = append(m.order, p.ID+"#dup")
		m.entries[p.ID+"#dup"] = e
		return
	}
	m.order = append(m.order, p.ID)
	m.entries[p.ID] = e

	bad := func(key string, a ...any) {
		e.fatal = true
		e.info.Status, e.info.ReasonKey, e.info.ReasonArgs = StatusInvalid, key, a
	}
	if !ValidID(p.ID) {
		bad("plugin.reason.invalid_id", p.ID)
		return
	}
	if p.Module == nil {
		bad("plugin.reason.no_module")
		return
	}
	man, err := LoadManifest(filepath.Join(m.opts.Root, "plugins", p.ID))
	if err != nil {
		bad("plugin.reason.manifest", err.Error())
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
		bad("plugin.reason.unsatisfied", err.Error())
		return
	}
	if e.info.Name == "" {
		e.info.Name = man.Name
	}
	if e.info.Version == "" {
		e.info.Version = man.Version
	}
	e.info.Description = man.Description
	if routes, ok := p.Module.(RuntimeRoutes); ok {
		e.hotSafe = !routes.RequiresRootRoutes()
	}

	// 配置里停用的插件：不校验授权、不挂载、不迁移，列表里显示 disabled。
	for _, d := range m.opts.DisabledPlugins {
		if d == p.ID {
			e.info.Status, e.info.ReasonKey = StatusDisabled, "plugin.reason.disabled_by_config"
			return
		}
	}

	switch p.Edition {
	case Free:
		e.info.Status = StatusActive
	case Pro:
		l, err := checkFile(m.opts.Root, p.ID, m.opts.Now(), m.opts.Keys)
		m.applyLic(e, l, err)
	default:
		bad("plugin.reason.unknown_edition", p.Edition)
	}
}

// applyLicense 把校验结果写进 e.info。
func applyLicense(e *entry, l *License, err error) {
	e.info.Customer, e.info.ExpiresAt = "", ""
	if l != nil {
		e.info.Customer, e.info.ExpiresAt = l.Customer, l.ExpiresAt
	}
	if err == nil {
		e.info.Status, e.info.Reason, e.info.ReasonKey, e.info.ReasonArgs = StatusActive, "", "", nil
		return
	}
	var le *LicenseError
	if errors.As(err, &le) {
		e.info.Status, e.info.ReasonKey, e.info.ReasonArgs, e.info.Reason = le.Status, le.Key, le.Args, ""
		return
	}
	e.info.Status, e.info.Reason, e.info.ReasonKey, e.info.ReasonArgs = StatusInvalid, err.Error(), "", nil
}

func (m *Manager) log() *zap.Logger {
	if m.deps != nil && m.deps.Log != nil {
		return m.deps.Log
	}
	return zap.NewNop()
}

// List 返回所有插件的状态；Reason 按 ctx 的语言填好。
func (m *Manager) List(ctx context.Context) []Info {
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
		LocalizeInfo(ctx, &i)
		out = append(out, i)
	}
	return out
}

// ActiveIDs 只返回已挂载且可用的插件，不暴露授权细节。
func (m *Manager) ActiveIDs() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.refreshRevocationsLocked()
	m.expireLocked(m.opts.Now())
	ids := []string{}
	for _, id := range m.order {
		e := m.entries[id]
		if m.enabledLocked(e) && (e.live || e.sub.Load() != nil) {
			ids = append(ids, id)
		}
	}
	return ids
}

// MenuVisible 报告属于插件 id 的侧边栏菜单是否该显示。未注册的 id 返回 true（沿用现状：
// 不在二进制里的插件菜单仍按库里的数据展示）；已注册的与 ActiveIDs（/plugins/active）同一判据——
// 授权有效且已挂载。否则停用 / 过期 / 吊销之后，侧栏会留一个点进去 403 的入口。
func (m *Manager) MenuVisible(id string) bool {
	if !ValidID(id) {
		return true
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	e, ok := m.entries[id]
	if !ok {
		return true
	}
	m.refreshRevocationsLocked()
	m.expireLocked(m.opts.Now())
	return m.enabledLocked(e) && (e.live || e.sub.Load() != nil)
}

// Root 返回插件管理使用的项目根目录（市场安装等文件操作需要）。
func (m *Manager) Root() string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.opts.Root
}

// applyLic 在 applyLicense 之外记录到期时间、授权域名与序列号，做域名校验，最后对照吊销清单。
// 调用方需持有 m.mu（或在构造阶段）。
func (m *Manager) applyLic(e *entry, l *License, err error) {
	m.applyLic0(e, l, err)
	e.info.PendingHost = e.pendingHost
	e.serial = ""
	if l != nil {
		e.serial = l.Serial
	}
	e.info.Serial = e.serial
	m.applyRevokedLocked(e)
	m.syncLifecycleLocked(e)
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
		key, args := domainMismatch(hosts, e.domains)
		e.info.Status, e.info.ReasonKey, e.info.ReasonArgs, e.info.Reason = StatusInvalid, key, args, ""
	}
}

func domainMismatch(hosts, domains []string) (string, []any) {
	return "plugin.reason.domain_mismatch", []any{strings.Join(hosts, ","), strings.Join(domains, ",")}
}

// LocalizeInfo 按请求语言把 ReasonKey 翻成 Reason（没 key 时保留原始文本）。
func LocalizeInfo(ctx context.Context, in *Info) {
	if in.ReasonKey != "" {
		in.Reason = i18n.T(ctx, in.ReasonKey, in.ReasonArgs...)
	}
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
		e.pendingHost, e.info.PendingHost = false, false
		if e.info.Status == StatusActive && !m.hostsMatch(e.domains, []string{h}) {
			key, args := domainMismatch([]string{h}, e.domains)
			e.info.Status, e.info.ReasonKey, e.info.ReasonArgs, e.info.Reason = StatusInvalid, key, args, ""
		}
		m.syncLifecycleLocked(e)
	}
}

// expireLocked 把已到期的 active 插件标为 expired，返回是否有变化。调用方需持有 m.mu。
func (m *Manager) expireLocked(now time.Time) bool {
	changed := false
	for _, e := range m.entries {
		if e.info.Status == StatusActive && !e.exp.IsZero() && !now.Before(e.exp) {
			e.info.Status, e.info.ReasonKey, e.info.ReasonArgs, e.info.Reason = StatusExpired, "plugin.reason.expired", []any{e.info.ExpiresAt}, ""
			m.syncLifecycleLocked(e)
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
// 文件探测与验签都在 m.mu 之外完成，只在需要应用结果时短暂加锁；一个清单版本只由首个 admit
// 验签，其余并发 admit 等待其生效结果，保证吊销仍能即时生效（不因移出锁而放行刚被吊销的插件）。
func (m *Manager) admit(c *gin.Context, e *entry) bool {
	fp, size, serr := m.revSrc.probe()
	var enabled bool
	var info Info
	for {
		m.mu.Lock()
		m.observeLocked(c.Request.Host)
		switch {
		case serr != nil:
			m.handleRevocationMissingLocked()
			m.expireLocked(m.opts.Now())
			enabled, info = m.enabledLocked(e), e.info
			m.mu.Unlock()
		case fp == m.revFP:
			// 已是最新版本；若正有别的 admit 在验签同一版本，则等它应用完再看结果。
			wait := m.revWaitLocked(fp)
			if wait == nil {
				m.expireLocked(m.opts.Now())
				enabled, info = m.enabledLocked(e), e.info
				m.mu.Unlock()
			} else {
				m.mu.Unlock()
				<-wait
				continue
			}
		default:
			// 首个发现清单变化的 admit 认领本轮验签。
			m.revFP = fp
			ch := make(chan struct{})
			m.revPending, m.revPendingCh = fp, ch
			m.mu.Unlock()
			rev, fail, verr := m.verifyRevocationOutside(size)
			m.mu.Lock()
			m.applyRevocationLocked(rev, fail, verr)
			m.expireLocked(m.opts.Now())
			enabled, info = m.enabledLocked(e), e.info
			m.revPending, m.revPendingCh = "", nil
			m.mu.Unlock()
			close(ch)
		}
		break
	}
	if !enabled {
		LocalizeInfo(c.Request.Context(), &info)
		if info.ReasonKey != "" {
			response.Fail(c, response.Forbidden(info.ReasonKey, info.ReasonArgs...))
		} else {
			response.Fail(c, response.Forbidden("error.forbidden").WithMsg(info.Reason))
		}
		return false
	}
	return true
}

// verifyRevocationOutside 在 m.mu 之外做文件读取与验签（两者都不改 m 的状态，可安全并发）。
func (m *Manager) verifyRevocationOutside(size int64) (*Revocations, revFail, error) {
	if size > MaxRevocationSize {
		return nil, revTooLarge, nil
	}
	raw, err := m.revSrc.read()
	if err != nil {
		return nil, revUnreadable, err
	}
	rev, err := VerifyRevocations(raw, m.opts.Keys)
	if err != nil {
		return nil, revVerify, err
	}
	return rev, revOK, nil
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
		return m.enabledLocked(e)
	}
}

func (m *Manager) enabledLocked(e *entry) bool {
	return !e.fatal && !e.pendingHost && !e.info.NeedRestart && !e.info.Stopping && e.info.Status == StatusActive && m.ctx.Err() == nil
}

// Attach 保存框架依赖，热挂载需要它。由“插件管理”模块在 Register 时调用。
func (m *Manager) Attach(d *core.Deps) {
	m.mu.Lock()
	m.deps = d
	m.flushWarnsLocked()
	m.mu.Unlock()
	// 把插件 worker 的 ctx 取消接到框架关闭钩子上：App.closeAll（程序内关闭，如向导切换）
	// 时也会取消 m.ctx，让插件 worker 在库/缓存被关闭前收到取消并 drain。收到 SIGTERM 的取消
	// 走 m.ctx 本身（signal.NotifyContext），两条链路都指向同一个 m.ctx，互不冲突。
	if d != nil && d.OnClose != nil {
		m.closeOnce.Do(func() { d.OnClose(m.stop) })
	}
}

// Routes 返回需要在路由上预留位置的插件：viaApp 的插件路由自己注册，授权接口单独挂；
// 其余（启动时未挂载的）统一走 /plugins/<id>/* 的转发，授权后可以热挂载。
func (m *Manager) Routes() (viaApp, dormant []string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, k := range m.order {
		e := m.entries[k]
		if e.p.ID != k || e.fatal || e.info.Status == StatusDisabled {
			continue // 停用的插件连转发路由都不留
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

// Unwrap 暴露被包装的插件模块：按接口发现扩展点（core.UnwrapModule）时用。
func (w *mounted) Unwrap() core.Module { return w.e.p.Module }

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
	if d.Root != nil {
		// 根路径插件路由（官网类插件）同样过授权闸门。热挂载（buildSub）不设置：
		// 运行中的引擎不允许加路由，授权导入后需重启才在根路径生效。
		dd.RootGroup = d.Root.Group("", gate)
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

func (m *Manager) start(e *entry, d *core.Deps) {
	m.mu.Lock()
	defer m.mu.Unlock()
	e.startDeps = d
	m.refreshRevocationsLocked()
	m.expireLocked(m.opts.Now())
	m.syncLifecycleLocked(e)
}

// 只在锁内预留一轮；插件回调始终在锁外运行。
func (m *Manager) syncLifecycleLocked(e *entry) {
	s, ok := e.p.Module.(Starter)
	if !ok {
		return
	}
	if !m.enabledLocked(e) {
		if e.run != nil {
			e.run.cancel()
			if _, restartable := s.(RestartableStarter); !restartable {
				e.info.NeedRestart = true
			} else {
				e.info.Stopping = true
			}
		}
		return
	}
	if e.run != nil || e.startDeps == nil || (!e.live && e.sub.Load() == nil) {
		return
	}
	ctx, cancel := context.WithCancel(m.ctx)
	run := &pluginRun{ctx: ctx, cancel: cancel}
	e.run = run
	go m.runStarter(e, run, s, e.startDeps)
}

func (m *Manager) runStarter(e *entry, run *pluginRun, s Starter, d *core.Deps) {
	if run.ctx.Err() != nil {
		m.finishRun(e)
		return
	}
	dd := *d
	guard := m.activeGuard(e)
	dd.PluginActive = func() bool { return run.ctx.Err() == nil && guard() }
	var done <-chan struct{}
	err := func() (err error) {
		defer func() {
			if r := recover(); r != nil {
				err = fmt.Errorf("Start panic: %v", r)
			}
		}()
		if err := s.Start(run.ctx, &dd); err != nil {
			return err
		}
		if rs, ok := s.(RestartableStarter); ok {
			done = rs.Done()
			if done == nil {
				return errors.New("RestartableStarter.Done returned nil")
			}
		}
		return nil
	}()
	if err != nil {
		m.mu.Lock()
		e.info.NeedRestart = true
		run.cancel()
		m.log().Error("plugin Start failed; restart required", zap.String("plugin", e.p.ID), zap.Error(err))
		m.mu.Unlock()
		return
	}
	<-run.ctx.Done()
	if done == nil {
		return // 无退出确认，保留本轮占位，禁止重复启动。
	}
	select {
	case <-done:
	case <-m.ctx.Done():
		return
	}
	m.finishRun(e)
}

func (m *Manager) finishRun(e *entry) {
	m.mu.Lock()
	defer m.mu.Unlock()
	e.run = nil
	e.info.Stopping = false
	m.refreshRevocationsLocked()
	m.expireLocked(m.opts.Now())
	m.syncLifecycleLocked(e)
}

type keysCtxKey struct{}

// hotMount 只装配声明了 API-only 契约的插件，绝不修改运行中的主引擎。
func (m *Manager) hotMount(e *entry, d *core.Deps) (err error) {
	if !e.hotSafe {
		return errors.New("插件未声明支持热挂载，需要重启")
	}
	if d == nil {
		return errors.New("框架尚未就绪")
	}
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("挂载时 panic: %v", r)
		}
	}()
	if err := m.prepare(d, e); err != nil {
		return err
	}
	return m.buildSub(e, d)
}

// buildSub 创建子引擎、注册插件路由、调 Starter。
func (m *Manager) buildSub(e *entry, d *core.Deps) (err error) {
	if !e.hotSafe {
		return errors.New("插件未声明支持热挂载，需要重启")
	}
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
	dd.Root, dd.RootGroup = nil, nil
	dd.PluginActive = m.activeGuard(e)
	dd.Public = grp
	dd.Admin = grp.Group("/admin", middleware.RequireSuper(), middleware.SkipTenant())
	e.p.Module.Register(grp.Group("", middleware.RequireLogin()), &dd)
	for _, route := range sub.Routes() {
		if !strings.HasPrefix(route.Path, "/api/v1/plugins/"+e.p.ID+"/") {
			return fmt.Errorf("热挂载路由超出插件命名空间: %s", route.Path)
		}
	}
	if d.APIDoc != nil {
		d.APIDoc.AddEngine(e.p.Module.Name(), sub) // 热挂载后接口文档立即包含这些路由
	}
	e.sub.Store(sub)
	m.start(e, &dd)
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
			st, info := e.info.Status, e.info
			m.mu.Unlock()
			if st == StatusRevoked {
				LocalizeInfo(c.Request.Context(), &info)
				response.Fail(c, response.Forbidden(info.ReasonKey, info.ReasonArgs...))
				return
			}
		}
		response.Fail(c, response.NotFound("plugin.disabled"))
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
	c.Abort() // HandleContext 替换了 handlers，不能让外层 Next 再执行子路由。
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
	if e.info.Status == StatusDisabled {
		return ImportResult{}, errors.New("插件已在配置 disabled_plugins 中停用，请先从配置移除再导入授权")
	}
	if e.mounting {
		return ImportResult{}, errors.New("插件正在挂载，请稍后重试")
	}
	l, err := VerifyKeys([]byte(content), id, m.opts.Now(), m.opts.Keys)
	if err != nil {
		return ImportResult{}, err
	}
	m.refreshRevocationsLocked()
	m.expireLocked(m.opts.Now())
	if r, ok := m.revBy[l.Serial]; ok && l.Serial != "" { // 已吊销的授权不落盘
		return ImportResult{}, revokedError(r)
	}
	if hosts := m.hostsLocked(); len(l.Domains) > 0 && len(hosts) > 0 && !m.hostsMatch(l.Domains, hosts) {
		key, args := domainMismatch(hosts, l.Domains)
		return ImportResult{}, invalid(key, args...)
	}
	if err := writeLicense(m.opts.Root, id, []byte(content)); err != nil {
		return ImportResult{}, fmt.Errorf("保存授权文件失败: %w", err)
	}
	if e.fatal {
		e.info.NeedRestart = true
		return ImportResult{Plugin: e.info, NeedRestart: true}, nil
	}
	if e.viaApp || e.sub.Load() != nil {
		if e.viaApp && !e.live {
			e.info.NeedRestart = true
		}
		m.applyLic(e, l, nil)
		return ImportResult{Plugin: e.info, NeedRestart: e.info.NeedRestart}, nil
	}
	if !e.hotSafe || e.info.NeedRestart {
		e.info.NeedRestart = true
		m.applyLic(e, l, nil)
		return ImportResult{Plugin: e.info, NeedRestart: true}, nil
	}
	e.mounting = true
	d := m.deps
	m.mu.Unlock()
	err = m.hotMount(e, d)
	m.mu.Lock()
	e.mounting = false
	if err != nil {
		m.log().Error("plugin hot mount failed", zap.String("plugin", id), zap.Error(err))
		e.info.NeedRestart = true
		e.info.Status, e.info.ReasonKey, e.info.ReasonArgs, e.info.Reason = StatusInvalid, "plugin.reason.hot_mount_failed", []any{err.Error()}, ""
		m.syncLifecycleLocked(e)
		return ImportResult{Plugin: e.info, NeedRestart: true}, nil
	}
	m.applyLic(e, l, nil)
	return ImportResult{Plugin: e.info, NeedRestart: e.info.NeedRestart}, nil
}
