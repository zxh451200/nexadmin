package plugin

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"

	"nexadmin/core"
	"nexadmin/core/auth"
)

type runtimeModule struct {
	root       bool
	registered atomic.Int32
}

func (*runtimeModule) Name() string               { return "cron" }
func (p *runtimeModule) RequiresRootRoutes() bool { return p.root }
func (p *runtimeModule) Register(r *gin.RouterGroup, d *core.Deps) {
	p.registered.Add(1)
	if d.Root != nil || d.RootGroup != nil {
		panic("hot plugin received the live root engine")
	}
	_ = d.PluginActive() // Register must not run under the manager lock.
	d.Public.GET("/plugins/cron/public", func(c *gin.Context) { c.String(200, "public") })
	r.GET("/plugins/cron/private", func(c *gin.Context) { c.String(200, "private") })
}

func TestPublicHotActivation(t *testing.T) {
	gin.SetMode(gin.TestMode)
	pub, priv, _ := ed25519.GenerateKey(rand.Reader)
	root := licenseRoot(t, priv, nil, now.Add(-time.Hour))
	var clock atomic.Int64
	clock.Store(now.UnixNano())
	p := &runtimeModule{}
	m := newManager([]Plugin{{ID: "cron", Edition: Pro, Module: p}}, Options{
		Root: root, PublicKey: pub, Now: func() time.Time { return time.Unix(0, clock.Load()) },
	})
	t.Cleanup(m.stop)
	sqlDB, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	db, err := gorm.Open(postgres.New(postgres.Config{Conn: sqlDB}), &gorm.Config{DisableAutomaticPing: true})
	if err != nil {
		t.Fatal(err)
	}
	mock.ExpectQuery("SELECT count\\(\\*\\) FROM information_schema.tables").WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(0))
	engine := gin.New()
	api := engine.Group("/api/v1", func(c *gin.Context) {
		if c.GetHeader("X-Test-User") != "" {
			auth.SetClaims(c, &auth.Claims{UserID: 1})
		}
		c.Next()
	})
	api.Any("/plugins/cron/*rest", func(c *gin.Context) { m.Dispatch(c, "cron") })
	before := len(engine.Routes())
	m.Attach(&core.Deps{DB: db, Log: zap.NewNop(), Public: api, Root: engine, RootGroup: engine.Group("")})
	res, err := m.ImportLicense("cron", string(signed(t, priv, "cron", now.Add(time.Hour))))
	if err != nil || res.NeedRestart || res.Plugin.Status != StatusActive {
		t.Fatalf("import: %+v %v", res, err)
	}
	if p.registered.Load() != 1 || len(engine.Routes()) != before {
		t.Fatal("hot mount must only register once on the child engine")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
	request := func(path string, login bool, want int, body string) {
		t.Helper()
		req := httptest.NewRequest("GET", "/api/v1/plugins/cron/"+path, nil)
		if login {
			req.Header.Set("X-Test-User", "1")
		}
		w := httptest.NewRecorder()
		engine.ServeHTTP(w, req)
		if w.Code != want || (body != "" && w.Body.String() != body) {
			t.Fatalf("%s login=%v: %d %s", path, login, w.Code, w.Body.String())
		}
	}
	request("public", false, 200, "public")
	request("private", false, 401, "")
	request("private", true, 200, "private")
	clock.Store(now.Add(2 * time.Hour).UnixNano())
	request("public", false, 403, "")
	request("private", true, 403, "")
	if ids := m.ActiveIDs(); len(ids) != 0 {
		t.Fatalf("expired plugin advertised as active: %v", ids)
	}
}

func TestRootRoutesRequireRestartBeforeSideEffects(t *testing.T) {
	pub, priv, _ := ed25519.GenerateKey(rand.Reader)
	for _, declared := range []bool{false, true} {
		t.Run(map[bool]string{false: "undeclared", true: "root-routes"}[declared], func(t *testing.T) {
			root := licenseRoot(t, priv, nil, now.Add(-time.Hour))
			p := &runtimeModule{root: true}
			var mod core.Module = p
			if !declared {
				mod = &undeclaredRuntime{p: p}
			}
			m := newManager([]Plugin{{ID: "cron", Edition: Pro, Module: mod}}, Options{Root: root, PublicKey: pub, Now: func() time.Time { return now }})
			t.Cleanup(m.stop)
			engine := gin.New()
			engine.GET("/existing", func(c *gin.Context) { c.Status(204) })
			// A nil DB would panic if migration or menu preparation were reached.
			m.Attach(&core.Deps{Log: zap.NewNop(), Root: engine, RootGroup: engine.Group("")})
			for i := 0; i < 2; i++ {
				res, err := m.ImportLicense("cron", string(signed(t, priv, "cron", now.Add(time.Hour))))
				if err != nil || !res.NeedRestart || !res.Plugin.NeedRestart || res.Plugin.Status != StatusActive {
					t.Fatalf("import: %+v %v", res, err)
				}
			}
			if p.registered.Load() != 0 || len(engine.Routes()) != 1 || m.entries["cron"].sub.Load() != nil {
				t.Fatal("root plugin must not be partially mounted")
			}
			if _, err := os.Stat(filepath.Join(root, "data", "licenses", "cron.lic")); err != nil {
				t.Fatal(err)
			}
			if len(m.ActiveIDs()) != 0 {
				t.Fatal("restart-required plugin must not be advertised as usable")
			}
		})
	}
}

type undeclaredRuntime struct{ p *runtimeModule }

func (*undeclaredRuntime) Name() string                                { return "cron" }
func (p *undeclaredRuntime) Register(r *gin.RouterGroup, d *core.Deps) { p.p.Register(r, d) }

type workerRound struct {
	ctx  context.Context
	done chan struct{}
}

type lifecycleModule struct {
	starts chan workerRound
	done   <-chan struct{}
	block  <-chan struct{}
}

func (*lifecycleModule) Name() string                          { return "cron" }
func (*lifecycleModule) Register(*gin.RouterGroup, *core.Deps) {}
func (p *lifecycleModule) Start(ctx context.Context, d *core.Deps) error {
	_ = d.PluginActive() // A reentrant guard must never deadlock.
	r := workerRound{ctx: ctx, done: make(chan struct{})}
	p.done = r.done
	p.starts <- r
	if p.block != nil {
		<-p.block
	}
	return nil
}
func (p *lifecycleModule) Done() <-chan struct{} { return p.done }

func receiveRound(t *testing.T, p *lifecycleModule) workerRound {
	t.Helper()
	select {
	case r := <-p.starts:
		return r
	case <-time.After(3 * time.Second):
		t.Fatal("Starter did not run (possibly deadlocked)")
		return workerRound{}
	}
}
func noRound(t *testing.T, p *lifecycleModule) {
	t.Helper()
	select {
	case <-p.starts:
		t.Fatal("duplicate or premature Starter call")
	case <-time.After(30 * time.Millisecond):
	}
}

func startTestWorker(m *Manager) {
	e := m.entries["cron"]
	e.viaApp, e.live = true, true
	m.start(e, &core.Deps{Log: zap.NewNop(), PluginActive: m.activeGuard(e)})
}

func TestStarterExpiryRenewalWaitsForExit(t *testing.T) {
	pub, priv, _ := ed25519.GenerateKey(rand.Reader)
	root := licenseRoot(t, priv, nil, now.Add(time.Hour))
	var clock atomic.Int64
	clock.Store(now.UnixNano())
	block := make(chan struct{})
	p := &lifecycleModule{starts: make(chan workerRound, 10), block: block}
	m := newManager([]Plugin{{ID: "cron", Edition: Pro, Module: p}}, Options{Root: root, PublicKey: pub, Now: func() time.Time { return time.Unix(0, clock.Load()) }})
	t.Cleanup(m.stop)
	startTestWorker(m)
	first := receiveRound(t, p)
	clock.Store(now.Add(2 * time.Hour).UnixNano())
	m.checkExpiry()
	if first.ctx.Err() != context.Canceled {
		t.Fatal("expiry must cancel the per-plugin context immediately")
	}
	for i := 0; i < 3; i++ {
		res, err := m.ImportLicense("cron", string(signed(t, priv, "cron", now.Add(3*time.Hour))))
		if err != nil || res.NeedRestart {
			t.Fatalf("renewal: %+v %v", res, err)
		}
	}
	noRound(t, p) // Start itself is still in flight.
	if !m.List()[0].Stopping || len(m.ActiveIDs()) != 0 || m.activeGuard(m.entries["cron"])() {
		t.Fatal("a canceled worker awaiting exit must not be advertised as active")
	}
	close(block)
	noRound(t, p) // Start returned, but its workers have not acknowledged exit.
	close(first.done)
	second := receiveRound(t, p)
	if second.ctx == first.ctx || second.ctx.Err() != nil {
		t.Fatal("renewal must use a fresh live context")
	}
	for i := 0; i < 3; i++ {
		m.checkExpiry()
		m.ObserveHost("example.com")
		_ = m.List()
	}
	noRound(t, p)
}

func TestStarterRevocationAndRestoration(t *testing.T) {
	pub, priv, _ := ed25519.GenerateKey(rand.Reader)
	root := serialRoot(t, priv, "aaaa11112222")
	p := &lifecycleModule{starts: make(chan workerRound, 10)}
	m := newManager([]Plugin{{ID: "cron", Edition: Pro, Module: p}}, Options{Root: root, PublicKey: pub, Now: func() time.Time { return now }})
	t.Cleanup(m.stop)
	startTestWorker(m)
	first := receiveRound(t, p)
	writeRev(t, root, mustRevoke(t, priv, nil, "aaaa11112222", "test"))
	if m.List()[0].Status != StatusRevoked || first.ctx.Err() != context.Canceled {
		t.Fatal("revocation must cancel the worker")
	}
	clear, err := SignRevocations(priv, Revocations{Seq: 2})
	if err != nil {
		t.Fatal(err)
	}
	writeRev(t, root, clear)
	m.checkExpiry()
	noRound(t, p)
	close(first.done)
	second := receiveRound(t, p)
	if second.ctx.Err() != nil {
		t.Fatal("restored worker must have a live context")
	}
	for i := 0; i < 3; i++ {
		m.checkExpiry()
	}
	noRound(t, p)
}

func TestPendingHostDoesNotStartWorkers(t *testing.T) {
	pub, priv, _ := ed25519.GenerateKey(rand.Reader)
	for _, host := range []string{"allowed.example", "other.example"} {
		t.Run(host, func(t *testing.T) {
			root := licenseRoot(t, priv, []string{"allowed.example"}, now.Add(time.Hour))
			p := &lifecycleModule{starts: make(chan workerRound, 10)}
			m := newManager([]Plugin{{ID: "cron", Edition: Pro, Module: p}}, Options{Root: root, PublicKey: pub, Hosts: []string{}, Now: func() time.Time { return now }})
			t.Cleanup(m.stop)
			startTestWorker(m)
			noRound(t, p)
			if len(m.ActiveIDs()) != 0 {
				t.Fatal("pending-host plugin must not be usable")
			}
			m.ObserveHost(host)
			if host == "allowed.example" {
				_ = receiveRound(t, p)
				m.ObserveHost(host)
			}
			noRound(t, p)
		})
	}
}

type legacyStarter struct{ starts chan context.Context }

func (*legacyStarter) Name() string                          { return "cron" }
func (*legacyStarter) Register(*gin.RouterGroup, *core.Deps) {}
func (p *legacyStarter) Start(ctx context.Context, d *core.Deps) error {
	_ = d.PluginActive()
	p.starts <- ctx
	return nil
}

func TestLegacyStarterCancellationRequiresRestart(t *testing.T) {
	pub, priv, _ := ed25519.GenerateKey(rand.Reader)
	root := licenseRoot(t, priv, nil, now.Add(time.Hour))
	var clock atomic.Int64
	clock.Store(now.UnixNano())
	p := &legacyStarter{starts: make(chan context.Context, 10)}
	m := newManager([]Plugin{{ID: "cron", Edition: Pro, Module: p}}, Options{Root: root, PublicKey: pub, Now: func() time.Time { return time.Unix(0, clock.Load()) }})
	t.Cleanup(m.stop)
	startTestWorker(m)
	var first context.Context
	select {
	case first = <-p.starts:
	case <-time.After(3 * time.Second):
		t.Fatal("Starter did not run")
	}
	clock.Store(now.Add(2 * time.Hour).UnixNano())
	m.checkExpiry()
	if first.Err() != context.Canceled {
		t.Fatal("legacy Starter must still receive cancellation")
	}
	for i := 0; i < 3; i++ {
		res, err := m.ImportLicense("cron", string(signed(t, priv, "cron", now.Add(3*time.Hour))))
		if err != nil || !res.NeedRestart || !res.Plugin.NeedRestart {
			t.Fatalf("legacy renewal: %+v %v", res, err)
		}
	}
	if m.activeGuard(m.entries["cron"])() || len(m.ActiveIDs()) != 0 {
		t.Fatal("legacy worker renewal cannot claim to be active")
	}
	select {
	case <-p.starts:
		t.Fatal("legacy Starter must never overlap a canceled worker")
	case <-time.After(30 * time.Millisecond):
	}
}
