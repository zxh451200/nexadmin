package plugin

// 运行期单个插件启停（SetEnabled）的不变式测试。
// 判据一律走真实的 HTTP 闸门（httptest 引擎 + mounted.Register 挂上的 gate 中间件），
// 不只断言 Status 字符串：停用必须真的 403，启用必须真的 200。

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"

	"nexadmin/core"
)

// switchModule 只有一条走闸门的路由，没有 worker：停用/启用应当立刻反映到接口上。
type switchModule struct{ path string }

func (p *switchModule) Name() string { return "cron" }
func (p *switchModule) Register(r *gin.RouterGroup, _ *core.Deps) {
	r.GET(p.path, func(c *gin.Context) { c.String(200, "ok") })
}

// switchWorkerModule 同上，但实现了 Starter 且**不是** RestartableStarter（真实插件的现状）。
type switchWorkerModule struct {
	switchModule
	starts chan context.Context
}

func (p *switchWorkerModule) Start(ctx context.Context, d *core.Deps) error {
	_ = d.PluginActive()
	p.starts <- ctx
	return nil
}

func switchRoot(t *testing.T, id string, edition Edition) string {
	t.Helper()
	root := t.TempDir()
	dir := filepath.Join(root, "plugins", id)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	y := "id: " + id + "\nname: " + id + "\nversion: 1.0.0\nedition: " + string(edition) + "\n"
	if err := os.WriteFile(filepath.Join(dir, "plugin.yaml"), []byte(y), 0o644); err != nil {
		t.Fatal(err)
	}
	return root
}

// mountGate 走真实挂载路径（mounted.Register）：路由挂到 httptest 引擎上并带 gate 中间件，
// 顺带按启动流程调用 Starter，所以这里的 403/200 与生产同一条判定链。
func mountGate(t *testing.T, m *Manager, id string) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	api := engine.Group("/api/v1")
	m.mu.Lock()
	e := m.entries[id]
	m.mu.Unlock()
	if e == nil {
		t.Fatalf("没有插件 %q", id)
	}
	(&mounted{m: m, e: e}).Register(api, &core.Deps{Log: zap.NewNop(), Public: api, Admin: api})
	return engine
}

func hit(engine *gin.Engine, path string) (int, string) {
	w := httptest.NewRecorder()
	engine.ServeHTTP(w, httptest.NewRequest(http.MethodGet, path, nil))
	return w.Code, strings.TrimSpace(w.Body.String())
}

func readOverlay(t *testing.T, root string) []string {
	t.Helper()
	raw, err := os.ReadFile(DisabledPath(root))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		t.Fatal(err)
	}
	var f disabledFile
	if err := json.Unmarshal(raw, &f); err != nil {
		t.Fatalf("停用清单格式不对: %v (%s)", err, raw)
	}
	return f.IDs
}

func newSwitchManager(t *testing.T, root string, pub ed25519.PublicKey, plugins []Plugin, disabled []string) *Manager {
	t.Helper()
	m := newManager(plugins, Options{Root: root, PublicKey: pub, Now: func() time.Time { return now }, DisabledPlugins: disabled})
	t.Cleanup(m.stop)
	return m
}

// 停用 -> 该插件路由立刻 403；启用 -> 403 消失。没有 worker 的插件不需要重启。
func TestSetEnabledGateTakesEffectImmediately(t *testing.T) {
	pub, _, _ := ed25519.GenerateKey(rand.Reader)
	root := switchRoot(t, "cron", Free)
	m := newSwitchManager(t, root, pub, []Plugin{{ID: "cron", Edition: Free, Module: &switchModule{path: "/plugins/cron/private"}}}, nil)
	engine := mountGate(t, m, "cron")
	if code, body := hit(engine, "/api/v1/plugins/cron/private"); code != 200 || body != "ok" {
		t.Fatalf("启用状态应该放行，得到 %d %q", code, body)
	}

	info, err := m.SetEnabled("cron", false)
	if err != nil {
		t.Fatalf("停用: %v", err)
	}
	if info.Status != StatusDisabled || info.ReasonKey != reasonDisabledByOperator || info.ConfigDisabled {
		t.Fatalf("停用后的状态形状要与配置停用一致（原因区分来源）: %+v", info)
	}
	if code, _ := hit(engine, "/api/v1/plugins/cron/private"); code != http.StatusForbidden {
		t.Fatalf("停用后 gate 必须 403，得到 %d", code)
	}
	if ids := m.ActiveIDs(); len(ids) != 0 {
		t.Fatalf("停用后仍被当作可用: %v", ids)
	}
	if m.MenuVisible("cron") {
		t.Fatal("停用后侧栏菜单仍可见")
	}
	if got := readOverlay(t, root); len(got) != 1 || got[0] != "cron" {
		t.Fatalf("停用清单没落盘: %v", got)
	}
	// 403 的文案要说人话并给下一步，而不是只回一个 status。
	LocalizeInfo(context.Background(), &info)
	if info.Reason == "" || !strings.Contains(info.Reason, "插件管理页") {
		t.Fatalf("停用原因文案为空或不含指引: %q", info.Reason)
	}

	info, err = m.SetEnabled("cron", true)
	if err != nil {
		t.Fatalf("启用: %v", err)
	}
	if info.Status != StatusActive || info.NeedRestart {
		t.Fatalf("无 worker 的插件启用后不该留 NeedRestart: %+v", info)
	}
	if code, body := hit(engine, "/api/v1/plugins/cron/private"); code != 200 || body != "ok" {
		t.Fatalf("启用后 403 应消失，得到 %d %q", code, body)
	}
	if ids := m.ActiveIDs(); len(ids) != 1 || ids[0] != "cron" {
		t.Fatalf("启用后 ActiveIDs=%v", ids)
	}
	if !m.MenuVisible("cron") {
		t.Fatal("启用后侧栏菜单应可见")
	}
	if got := readOverlay(t, root); len(got) != 0 {
		t.Fatalf("启用后停用清单应为空: %v", got)
	}

	// 未知 id：ErrNotFound，且不写盘。
	if _, err := m.SetEnabled("nope", false); !errors.Is(err, ErrNotFound) {
		t.Fatalf("未知 id 应回 ErrNotFound，得到 %v", err)
	}
	if got := readOverlay(t, root); len(got) != 0 {
		t.Fatalf("未知 id 不该改停用清单: %v", got)
	}
}

// 诚实性检查：带 worker（未实现 RestartableStarter）的插件停用再启用后，
// worker 不会重新起来，NeedRestart 为真，接口与菜单**保持停用的真实状态**。
// 这条用例钉住「不伪造恢复」：任何把它做成 200 的改动都会红。
func TestSetEnabledWorkerNeedsRestartAndStaysHonest(t *testing.T) {
	pub, _, _ := ed25519.GenerateKey(rand.Reader)
	root := switchRoot(t, "cron", Free)
	p := &switchWorkerModule{switchModule: switchModule{path: "/plugins/cron/private"}, starts: make(chan context.Context, 10)}
	m := newSwitchManager(t, root, pub, []Plugin{{ID: "cron", Edition: Free, Module: p}}, nil)
	engine := mountGate(t, m, "cron")

	var first context.Context
	select {
	case first = <-p.starts:
	case <-time.After(3 * time.Second):
		t.Fatal("Starter 没被调用（可能死锁）")
	}

	info, err := m.SetEnabled("cron", false)
	if err != nil || info.Status != StatusDisabled {
		t.Fatalf("停用: %+v %v", info, err)
	}
	if first.Err() == nil {
		t.Fatal("停用必须立刻取消 worker 的 ctx")
	}
	if !info.NeedRestart {
		t.Fatalf("非 RestartableStarter 停用后要标 NeedRestart: %+v", info)
	}
	if code, _ := hit(engine, "/api/v1/plugins/cron/private"); code != http.StatusForbidden {
		t.Fatalf("停用后应 403，得到 %d", code)
	}

	info, err = m.SetEnabled("cron", true)
	if err != nil {
		t.Fatalf("启用: %v", err)
	}
	// 授权/配置层面它是启用态（Status=active），但 NeedRestart 让闸门继续拒绝——这才是真实情况。
	if info.Status != StatusActive || !info.NeedRestart {
		t.Fatalf("重新启用后应保留 NeedRestart（worker 起不来）: %+v", info)
	}
	if code, _ := hit(engine, "/api/v1/plugins/cron/private"); code != http.StatusForbidden {
		t.Fatalf("NeedRestart 期间接口必须仍是 403（不能伪造恢复），得到 %d", code)
	}
	if ids := m.ActiveIDs(); len(ids) != 0 {
		t.Fatalf("NeedRestart 期间不能对外宣称可用: %v", ids)
	}
	if m.MenuVisible("cron") {
		t.Fatal("NeedRestart 期间侧栏菜单不应可见")
	}
	select {
	case <-p.starts:
		t.Fatal("不允许在线重新拉起未实现 RestartableStarter 的 worker")
	case <-time.After(50 * time.Millisecond):
	}
}

// 并集语义：配置 disabled_plugins 停用的插件不能用运行期开关打开；没被配置停用的可以。
func TestSetEnabledUnionWithConfig(t *testing.T) {
	pub, _, _ := ed25519.GenerateKey(rand.Reader)
	root := switchRoot(t, "cron", Free)
	if err := os.MkdirAll(filepath.Join(root, "plugins", "other"), 0o755); err != nil {
		t.Fatal(err)
	}
	y := "id: other\nname: other\nversion: 1.0.0\nedition: free\n"
	if err := os.WriteFile(filepath.Join(root, "plugins", "other", "plugin.yaml"), []byte(y), 0o644); err != nil {
		t.Fatal(err)
	}
	m := newSwitchManager(t, root, pub, []Plugin{
		{ID: "cron", Edition: Free, Module: &switchModule{path: "/plugins/cron/private"}},
		{ID: "other", Edition: Free, Module: &switchModule{path: "/plugins/other/private"}},
	}, []string{"Cron"}) // 配置里的 id 大小写/空白都要并到同一口径

	// 反例 1：配置停用的插件，启用被明确拒绝，状态不变。
	info, err := m.SetEnabled("cron", true)
	if !errors.Is(err, ErrDisabledByConfig) {
		t.Fatalf("配置停用的插件应回 ErrDisabledByConfig，得到 %v", err)
	}
	if info.Status != StatusDisabled || info.ReasonKey != reasonDisabledByConfig || !info.ConfigDisabled {
		t.Fatalf("被拒绝的启用不能改变状态: %+v", info)
	}
	// 反例 2：运行期「停用」一个已被配置停用的插件，之后再启用依旧打不开（并集不会因叠加层清空而失效）。
	if _, err := m.SetEnabled("cron", false); err != nil {
		t.Fatalf("叠加层记录一个配置已停用的插件不该报错: %v", err)
	}
	if _, err := m.SetEnabled("cron", true); !errors.Is(err, ErrDisabledByConfig) {
		t.Fatalf("叠加层清空后配置停用仍必须生效，得到 %v", err)
	}
	if infos := m.List(context.Background()); infos[0].Status != StatusDisabled || !infos[0].ConfigDisabled {
		t.Fatalf("配置停用的状态被运行期开关改掉了: %+v", infos)
	}

	// 正例：没被配置停用的插件，停用/启用都正常，且停用清单里只有它。
	engine := mountGate(t, m, "other")
	if code, _ := hit(engine, "/api/v1/plugins/other/private"); code != 200 {
		t.Fatalf("other 应放行，得到 %d", code)
	}
	if info, err := m.SetEnabled("other", false); err != nil || info.Status != StatusDisabled || info.ConfigDisabled {
		t.Fatalf("停用 other: %+v %v", info, err)
	}
	if code, _ := hit(engine, "/api/v1/plugins/other/private"); code != http.StatusForbidden {
		t.Fatalf("other 停用后应 403，得到 %d", code)
	}
	if info, err := m.SetEnabled("other", true); err != nil || info.Status != StatusActive {
		t.Fatalf("启用 other: %+v %v", info, err)
	}
	if code, _ := hit(engine, "/api/v1/plugins/other/private"); code != 200 {
		t.Fatalf("other 启用后应 200，得到 %d", code)
	}
	if got := readOverlay(t, root); len(got) != 1 || got[0] != "cron" {
		t.Fatalf("停用清单里该留下的只有配置之外被叠加记录的那个: %v", got)
	}
}

// 落盘往返：写盘 -> 用同一个 Root 重建 Manager -> 状态一致（重启后开关不丢）。
func TestDisabledOverlayRoundTrip(t *testing.T) {
	pub, _, _ := ed25519.GenerateKey(rand.Reader)
	root := switchRoot(t, "cron", Free)
	m := newSwitchManager(t, root, pub, []Plugin{{ID: "cron", Edition: Free, Module: &switchModule{path: "/plugins/cron/private"}}}, nil)
	if info, err := m.SetEnabled("cron", false); err != nil || info.Status != StatusDisabled {
		t.Fatalf("停用: %+v %v", info, err)
	}

	restarted := newSwitchManager(t, root, pub, []Plugin{{ID: "cron", Edition: Free, Module: &switchModule{path: "/plugins/cron/private"}}}, nil)
	list := restarted.List(context.Background())
	if len(list) != 1 {
		t.Fatalf("重建后列表异常: %+v", list)
	}
	if list[0].Status != StatusDisabled || list[0].ReasonKey != reasonDisabledByOperator || list[0].ConfigDisabled {
		t.Fatalf("重启后状态没读回叠加层: %+v", list[0])
	}
	if len(restarted.ActiveIDs()) != 0 || restarted.MenuVisible("cron") {
		t.Fatal("重启后停用的插件仍被当作可用")
	}
	if ids := restarted.disabledOverlayIDs(); len(ids) != 1 || ids[0] != "cron" {
		t.Fatalf("叠加层读回不一致: %v", ids)
	}
	// 重建后依然可以启用，且启用会同步清掉磁盘上的记录。
	if info, err := restarted.SetEnabled("cron", true); err != nil || info.Status != StatusActive {
		t.Fatalf("重启后启用: %+v %v", info, err)
	}
	if got := readOverlay(t, root); len(got) != 0 {
		t.Fatalf("启用后磁盘上还留着停用记录: %v", got)
	}

	// 停用清单读坏了：告警并按「没人停用」继续启动，不能把服务拖死。
	if err := os.WriteFile(DisabledPath(root), []byte("{ not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	broken := newSwitchManager(t, root, pub, []Plugin{{ID: "cron", Edition: Free, Module: &switchModule{path: "/plugins/cron/private"}}}, nil)
	if got := broken.List(context.Background()); len(got) != 1 || got[0].Status != StatusActive {
		t.Fatalf("坏文件应按未停用处理: %+v", got)
	}
}

// 停用的插件导授权不会让它「复活」：回明确错误，也不落盘授权文件。
func TestImportLicenseRefusedWhileOperatorDisabled(t *testing.T) {
	pub, priv, _ := ed25519.GenerateKey(rand.Reader)
	root := switchRoot(t, "cron", Pro)
	exp := now.Add(time.Hour)
	raw, err := Sign(priv, License{Plugin: "cron", Customer: "acme", IssuedAt: now.Format(time.RFC3339), ExpiresAt: exp.Format(time.RFC3339)})
	if err != nil {
		t.Fatal(err)
	}
	if err := writeLicense(root, "cron", raw); err != nil {
		t.Fatal(err)
	}
	m := newSwitchManager(t, root, pub, []Plugin{{ID: "cron", Edition: Pro, Module: &switchModule{path: "/plugins/cron/private"}}}, nil)
	if got := m.List(context.Background()); got[0].Status != StatusActive {
		t.Fatalf("带授权应 active: %+v", got[0])
	}
	if info, err := m.SetEnabled("cron", false); err != nil || info.Status != StatusDisabled {
		t.Fatalf("停用: %+v %v", info, err)
	}
	if _, err := m.ImportLicense("cron", string(raw)); !errors.Is(err, ErrDisabledByOperator) {
		t.Fatalf("停用期间导授权应回 ErrDisabledByOperator，得到 %v", err)
	}
	// 停用期间授权文件仍在（撤授权不是停用的副作用），恢复后立刻可用。
	if _, err := os.Stat(filepath.Join(root, "data", "licenses", "cron.lic")); err != nil {
		t.Fatal(err)
	}
	if info, err := m.SetEnabled("cron", true); err != nil || info.Status != StatusActive {
		t.Fatalf("启用后用原授权应恢复: %+v %v", info, err)
	}
}

// 清单/注册有致命问题的插件：开关不适用（不写盘），免得留下一条永远不生效的停用记录。
func TestSetEnabledRejectsFatalPlugin(t *testing.T) {
	pub, _, _ := ed25519.GenerateKey(rand.Reader)
	root := switchRoot(t, "cron", Free)
	// 清单 edition 与注册不一致 -> evaluate 走 bad()，fatal。
	if err := os.WriteFile(filepath.Join(root, "plugins", "cron", "plugin.yaml"),
		[]byte("id: cron\nname: cron\nversion: 1.0.0\nedition: pro\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	m := newSwitchManager(t, root, pub, []Plugin{{ID: "cron", Edition: Free, Module: &switchModule{path: "/plugins/cron/private"}}}, nil)
	if got := m.List(context.Background()); got[0].Status != StatusInvalid {
		t.Fatalf("前置条件：清单不一致应 invalid: %+v", got[0])
	}
	for _, on := range []bool{false, true} {
		if _, err := m.SetEnabled("cron", on); !errors.Is(err, ErrNotManageable) {
			t.Fatalf("SetEnabled(on=%v) 应回 ErrNotManageable，得到 %v", on, err)
		}
	}
	if _, err := os.Stat(DisabledPath(root)); !os.IsNotExist(err) {
		t.Fatal("被拒绝的切换不该写停用清单")
	}
}
