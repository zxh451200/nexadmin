package plugin

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
)

// serialRoot 在 licenseRoot 的基础上，把授权换成带序列号的（serial 为空则是旧式无序列号授权）。
func serialRoot(t *testing.T, priv ed25519.PrivateKey, serial string) string {
	t.Helper()
	exp := now.Add(90 * 24 * time.Hour)
	root := licenseRoot(t, priv, nil, exp)
	raw, err := Sign(priv, License{Plugin: "cron", Customer: "acme", IssuedAt: now.Format(time.RFC3339),
		ExpiresAt: exp.Format(time.RFC3339), Serial: serial})
	if err != nil {
		t.Fatal(err)
	}
	if err := writeLicense(root, "cron", raw); err != nil {
		t.Fatal(err)
	}
	return root
}

// writeRev 写吊销清单；显式设置递增的 mtime，避免同一时钟粒度内的两次写入被指纹判为“没变”。
var revTick time.Duration

func writeRev(t *testing.T, root string, raw []byte) {
	t.Helper()
	p := RevocationsPath(root)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, raw, 0o644); err != nil {
		t.Fatal(err)
	}
	revTick += time.Second
	mt := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC).Add(revTick)
	if err := os.Chtimes(p, mt, mt); err != nil {
		t.Fatal(err)
	}
}

func newCron(root string, pub ed25519.PublicKey) *Manager {
	return newManager([]Plugin{{ID: "cron", Edition: Pro, Module: fakeModule{}}},
		Options{Root: root, PublicKey: pub, Now: func() time.Time { return now }})
}

func gateCode(m *Manager) int {
	r := gin.New()
	r.Group("", m.gate(m.entries["cron"])).GET("/x", func(c *gin.Context) { c.String(200, "ok") })
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest("GET", "/x", nil))
	return w.Code
}

func mustRevoke(t *testing.T, priv ed25519.PrivateKey, prev []byte, serial, reason string) []byte {
	t.Helper()
	raw, err := AddRevocation(priv, prev, RevokedEntry{Serial: serial, Plugin: "cron", Reason: reason}, now)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func TestRevokedLicenseHit(t *testing.T) {
	gin.SetMode(gin.TestMode)
	pub, priv, _ := ed25519.GenerateKey(rand.Reader)
	root := serialRoot(t, priv, "aaaa11112222")
	writeRev(t, root, mustRevoke(t, priv, nil, "aaaa11112222", "chargeback"))

	m := newCron(root, pub)
	i := cronInfo(m)
	if i.Status != StatusRevoked || i.ReasonKey != "license.revoked" || len(i.ReasonArgs) != 2 || !strings.Contains(i.ReasonArgs[1].(string), "chargeback") || i.Serial != "aaaa11112222" {
		t.Fatalf("got %+v", i)
	}
	if i.Customer != "acme" {
		t.Fatalf("customer should still be shown: %+v", i)
	}
	if gateCode(m) != http.StatusForbidden {
		t.Fatal("revoked plugin routes must return 403")
	}
	// CLI 使用的 CheckFile 也要报 revoked（使用内置公钥集合，这里单独验证信封层逻辑）。
	if l, err := checkFile(root, "cron", now, []Key{{KID: KeyID(pub), Pub: pub}}); err != nil || l.Serial != "aaaa11112222" {
		t.Fatalf("license itself is still valid, only the list revokes it: %v", err)
	}
}

func TestRevocationNoHitNoEffect(t *testing.T) {
	gin.SetMode(gin.TestMode)
	pub, priv, _ := ed25519.GenerateKey(rand.Reader)
	root := serialRoot(t, priv, "aaaa11112222")
	writeRev(t, root, mustRevoke(t, priv, nil, "bbbb33334444", "other customer"))
	m := newCron(root, pub)
	if i := cronInfo(m); i.Status != StatusActive || i.Reason != "" {
		t.Fatalf("got %+v", i)
	}
	if gateCode(m) != http.StatusOK {
		t.Fatal("non-matching list must not affect the plugin")
	}
}

func TestLegacyLicenseWithoutSerialUnaffected(t *testing.T) {
	pub, priv, _ := ed25519.GenerateKey(rand.Reader)
	root := serialRoot(t, priv, "") // 旧授权：载荷里没有 serial 字段
	raw, _ := os.ReadFile(filepath.Join(root, "data", "licenses", "cron.lic"))
	var env envelope
	if err := json.Unmarshal(raw, &env); err != nil {
		t.Fatal(err)
	}
	payload, _ := base64DecodeStd(env.Payload)
	if strings.Contains(string(payload), "serial") {
		t.Fatalf("legacy payload must not carry a serial field: %s", payload)
	}
	// 清单里放一个空串以外的各种序列号，旧授权都不能被命中。
	writeRev(t, root, mustRevoke(t, priv, mustRevoke(t, priv, nil, "aaaa11112222", "x"), "bbbb33334444", "y"))
	m := newCron(root, pub)
	if i := cronInfo(m); i.Status != StatusActive || i.Serial != "" {
		t.Fatalf("legacy license must stay active: %+v", i)
	}
	// 手工构造一份“序列号为空串”的签名清单也不能命中空序列号（VerifyRevocations 拒绝非法序列号）。
	bad, _ := SignRevocations(priv, Revocations{Seq: 9, Entries: []RevokedEntry{{Serial: ""}}})
	if _, err := VerifyRevocations(bad, []Key{{KID: KeyID(pub), Pub: pub}}); err == nil {
		t.Fatal("empty serial entry must be rejected")
	}
	if _, ok := (&Revocations{Entries: []RevokedEntry{{Serial: ""}}}).Find(""); ok {
		t.Fatal("empty serial must never match")
	}
}

func TestTamperedRevocationIgnored(t *testing.T) {
	gin.SetMode(gin.TestMode)
	pub, priv, _ := ed25519.GenerateKey(rand.Reader)
	_, otherPriv, _ := ed25519.GenerateKey(rand.Reader)
	root := serialRoot(t, priv, "aaaa11112222")
	good := mustRevoke(t, priv, nil, "bbbb33334444", "unrelated")

	// 1) 篡改载荷想“误吊销”：把清单里的序列号改成买家自己的。
	var env envelope
	_ = json.Unmarshal(good, &env)
	p, _ := base64DecodeStd(env.Payload)
	env.Payload = base64Std([]byte(strings.Replace(string(p), "bbbb33334444", "aaaa11112222", 1)))
	forged, _ := json.Marshal(env)
	// 2) 别的钥签的清单（kid 不在集合里）。
	foreign := mustRevoke(t, otherPriv, nil, "aaaa11112222", "foreign")
	// 3) 把授权文件当清单、垃圾内容、签名对但 type 不对。
	lic, _ := os.ReadFile(filepath.Join(root, "data", "licenses", "cron.lic"))
	wrongType, _ := signRaw(priv, []byte(`{"type":"something-else","seq":1,"entries":[{"serial":"aaaa11112222"}]}`))

	for name, raw := range map[string][]byte{"forged": forged, "foreign": foreign, "license-as-list": lic, "garbage": []byte("{{{"), "wrong-type": wrongType} {
		writeRev(t, root, raw)
		m := newCron(root, pub)
		if i := cronInfo(m); i.Status != StatusActive {
			t.Fatalf("%s: invalid list must be ignored, got %+v", name, i)
		}
		if len(m.pendingWarns) == 0 {
			t.Fatalf("%s: ignoring an invalid list must be warned about", name)
		}
		if gateCode(m) != http.StatusOK {
			t.Fatalf("%s: routes must stay reachable", name)
		}
	}
}

func TestRevocationHotReload(t *testing.T) {
	gin.SetMode(gin.TestMode)
	pub, priv, _ := ed25519.GenerateKey(rand.Reader)
	root := serialRoot(t, priv, "aaaa11112222")
	m := newCron(root, pub) // 启动时没有清单
	if gateCode(m) != http.StatusOK || cronInfo(m).Status != StatusActive {
		t.Fatal("starts active")
	}

	// 运行中放入清单：请求前检查立即生效，不需要重启、也不需要定时器。
	v1 := mustRevoke(t, priv, nil, "aaaa11112222", "fraud")
	writeRev(t, root, v1)
	if gateCode(m) != http.StatusForbidden {
		t.Fatal("list dropped in at runtime must take effect on the next request")
	}
	if i := cronInfo(m); i.Status != StatusRevoked {
		t.Fatalf("got %+v", i)
	}

	// 被篡改的新文件：忽略并沿用已验证的清单（仍是 revoked，不会因为篡改而“解除”）。
	var env envelope
	_ = json.Unmarshal(v1, &env)
	env.Signature = base64Std(make([]byte, ed25519.SignatureSize))
	broken, _ := json.Marshal(env)
	writeRev(t, root, broken)
	m.checkExpiry()
	if cronInfo(m).Status != StatusRevoked {
		t.Fatal("tampered file must not un-revoke")
	}
	// 删除文件：同样沿用内存中的清单。
	if err := os.Remove(RevocationsPath(root)); err != nil {
		t.Fatal(err)
	}
	m.checkExpiry()
	if cronInfo(m).Status != StatusRevoked {
		t.Fatal("deleting the file at runtime keeps the last verified list until restart")
	}

	// 回退到更老版本的清单（seq 更小）：忽略。先升到 seq 2，再放回 seq 1。
	v2 := mustRevoke(t, priv, v1, "bbbb33334444", "other")
	writeRev(t, root, v2)
	m.checkExpiry()
	writeRev(t, root, v1)
	m.checkExpiry()
	if cronInfo(m).Status != StatusRevoked || m.revSeq != 2 {
		t.Fatalf("older list must be ignored, seq=%d", m.revSeq)
	}

	// 发行方签发更高 seq、不再包含该序列号的清单：恢复为授权文件本身的状态（active）。
	v3, err := SignRevocations(priv, Revocations{Seq: 3, UpdatedAt: now.Format(time.RFC3339),
		Entries: []RevokedEntry{{Serial: "bbbb33334444", RevokedAt: now.Format(time.RFC3339)}}})
	if err != nil {
		t.Fatal(err)
	}
	writeRev(t, root, v3)
	m.checkExpiry() // 定时检查路径
	if i := cronInfo(m); i.Status != StatusActive || i.Reason != "" {
		t.Fatalf("un-revoked license should be active again: %+v", i)
	}
}

func TestImportRejectsRevokedLicense(t *testing.T) {
	pub, priv, _ := ed25519.GenerateKey(rand.Reader)
	root := serialRoot(t, priv, "aaaa11112222")
	m := newCron(root, pub)
	writeRev(t, root, mustRevoke(t, priv, nil, "cccc55556666", "stolen"))
	exp := now.Add(24 * time.Hour)
	raw, _ := Sign(priv, License{Plugin: "cron", Customer: "acme", ExpiresAt: exp.Format(time.RFC3339), Serial: "cccc55556666"})
	before, _ := os.ReadFile(filepath.Join(root, "data", "licenses", "cron.lic"))
	if _, err := m.ImportLicense("cron", string(raw)); status(err) != StatusRevoked {
		t.Fatalf("want revoked, got %v", err)
	}
	after, _ := os.ReadFile(filepath.Join(root, "data", "licenses", "cron.lic"))
	if string(before) != string(after) {
		t.Fatal("a revoked license must not overwrite the stored one")
	}
}

func TestRevokedAtStartupDispatchIs403(t *testing.T) {
	gin.SetMode(gin.TestMode)
	pub, priv, _ := ed25519.GenerateKey(rand.Reader)
	root := serialRoot(t, priv, "aaaa11112222")
	writeRev(t, root, mustRevoke(t, priv, nil, "aaaa11112222", "fraud"))
	m := newCron(root, pub)
	r := gin.New()
	r.GET("/x", func(c *gin.Context) { m.Dispatch(c, "cron") })
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest("GET", "/x", nil))
	if w.Code != http.StatusForbidden || !strings.Contains(w.Body.String(), "fraud") {
		t.Fatalf("got %d %s", w.Code, w.Body.String())
	}
}

func TestAddRevocation(t *testing.T) {
	pub, priv, _ := ed25519.GenerateKey(rand.Reader)
	_, otherPriv, _ := ed25519.GenerateKey(rand.Reader)
	keys := []Key{{KID: KeyID(pub), Pub: pub}}

	a := mustRevoke(t, priv, nil, "aaaa11112222", "r1")
	b := mustRevoke(t, priv, a, "bbbb33334444", "r2")
	r, err := VerifyRevocations(b, keys)
	if err != nil || r.Seq != 2 || len(r.Entries) != 2 {
		t.Fatalf("got %+v, %v", r, err)
	}
	if e, ok := r.Find("aaaa11112222"); !ok || e.Reason != "r1" || e.RevokedAt == "" {
		t.Fatalf("got %+v", e)
	}
	if _, err := AddRevocation(priv, b, RevokedEntry{Serial: "aaaa11112222", Reason: "dup"}, now); err == nil {
		t.Fatal("duplicate serial must be rejected")
	}
	if _, err := AddRevocation(otherPriv, a, RevokedEntry{Serial: "cccc55556666", Reason: "x"}, now); err == nil {
		t.Fatal("must not append to a list this key cannot verify")
	}
	if _, err := AddRevocation(priv, nil, RevokedEntry{Serial: "BAD SERIAL", Reason: "x"}, now); err == nil {
		t.Fatal("invalid serial must be rejected")
	}
	if !ValidSerial(NewSerial()) {
		t.Fatal("NewSerial must satisfy ValidSerial")
	}
}

// signRaw 用与 SignRevocations 相同的信封签任意载荷（构造“签名对但内容不对”的清单）。
func signRaw(priv ed25519.PrivateKey, payload []byte) ([]byte, error) {
	env := envelope{
		KID:       KeyID(priv.Public().(ed25519.PublicKey)),
		Payload:   base64Std(payload),
		Signature: base64Std(ed25519.Sign(priv, payload)),
	}
	return json.Marshal(env)
}

func base64DecodeStd(s string) ([]byte, error) { return base64.StdEncoding.DecodeString(s) }
