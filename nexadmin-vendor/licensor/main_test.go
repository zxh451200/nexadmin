package main

import (
	"bytes"
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
)

func newTestServer(t *testing.T, now time.Time) (*server, ed25519.PublicKey, string) {
	t.Helper()
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	st, err := openStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	s := newServer(priv, nil, "s3cret", st)
	s.now = func() time.Time { return now }
	return s, pub, dir
}

func post(s *server, path, body, token string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
	req.Header.Set("X-Requested-With", "licensor")
	if token != "" {
		req.Header.Set("X-Licensor-Token", token)
	}
	w := httptest.NewRecorder()
	s.routes().ServeHTTP(w, req)
	return w
}

func TestIssueVerifiesAndRecords(t *testing.T) {
	now := time.Date(2026, 10, 17, 0, 0, 0, 0, time.UTC)
	s, pub, dir := newTestServer(t, now)
	w := post(s, "/api/issue", `{"plugin":"cron","customer":"ACME","expires":"2027-01-01","domains":"b.com, a.com","note":"=cmd"}`, "s3cret")
	if w.Code != 200 {
		t.Fatalf("code=%d body=%s", w.Code, w.Body)
	}
	var out struct {
		License string `json:"license"`
		Record  Record `json:"record"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	l, err := verifyLicense([]byte(out.License), "cron", now, pub)
	if err != nil {
		t.Fatalf("verifyLicense: %v", err)
	}
	if l.Customer != "ACME" || strings.Join(l.Domains, ",") != "a.com,b.com" {
		t.Fatalf("license=%+v", l)
	}
	raw, err := os.ReadFile(filepath.Join(dir, "issued.json"))
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(raw, []byte("signature")) || bytes.Contains(raw, []byte("payload")) {
		t.Fatal("记录里不应含授权全文")
	}
	var recs []Record
	if err := json.Unmarshal(raw, &recs); err != nil || len(recs) != 1 || recs[0].KID != keyID(pub) || recs[0].Note != "=cmd" {
		t.Fatalf("recs=%+v err=%v", recs, err)
	}
	if fi, _ := os.Stat(filepath.Join(dir, "issued.json")); fi.Mode().Perm() != 0o600 {
		t.Fatalf("perm=%v", fi.Mode().Perm())
	}
	if c := csvCell("=1+1"); c != "'=1+1" {
		t.Fatal(c)
	}
}

func TestIssueRejectsBadInput(t *testing.T) {
	now := time.Date(2026, 10, 17, 0, 0, 0, 0, time.UTC)
	s, _, _ := newTestServer(t, now)
	for _, b := range []string{
		`{"plugin":"../x","customer":"A","expires":"2027-01-01"}`,
		`{"plugin":"cron","customer":"","expires":"2027-01-01"}`,
		`{"plugin":"cron","customer":"A","expires":"2020-01-01"}`,
		`{"plugin":"cron","customer":"A","expires":"bad"}`,
	} {
		if w := post(s, "/api/issue", b, "s3cret"); w.Code != 400 {
			t.Errorf("%s => %d", b, w.Code)
		}
	}
	if len(s.st.List()) != 0 {
		t.Fatal("失败请求不应写记录")
	}
}

func TestExpiringSoon(t *testing.T) {
	now := time.Date(2026, 10, 17, 12, 0, 0, 0, time.UTC)
	s, _, _ := newTestServer(t, now)
	at := func(d int) string { return now.Add(time.Duration(d) * 24 * time.Hour).Format(time.RFC3339) }
	for i, d := range []int{-3, 10, 30, 31, 200} {
		if err := s.st.Add(Record{ID: string(rune('a' + i)), IssuedAt: at(-100 + i), Plugin: "cron", Customer: "C", ExpiresAt: at(d)}); err != nil {
			t.Fatal(err)
		}
	}
	got := s.st.Expiring(now)
	if len(got) != 2 || got[0].ExpiresAt != at(10) || got[1].ExpiresAt != at(30) {
		t.Fatalf("expiring=%+v", got)
	}
	if d := (Record{ExpiresAt: at(10)}).DaysLeft(now); d != 10 {
		t.Fatalf("days=%d", d)
	}
}

func TestTokenAuth(t *testing.T) {
	s, _, _ := newTestServer(t, time.Now())
	get := func(mod func(*http.Request)) int {
		req := httptest.NewRequest(http.MethodGet, "/api/records", nil)
		mod(req)
		w := httptest.NewRecorder()
		s.routes().ServeHTTP(w, req)
		return w.Code
	}
	if c := get(func(*http.Request) {}); c != 401 {
		t.Fatalf("无口令=%d", c)
	}
	if c := get(func(r *http.Request) { r.Header.Set("X-Licensor-Token", "wrong") }); c != 401 {
		t.Fatalf("错口令=%d", c)
	}
	if c := get(func(r *http.Request) { r.Header.Set("X-Licensor-Token", "s3cret") }); c != 200 {
		t.Fatalf("头=%d", c)
	}
	if c := get(func(r *http.Request) { r.Header.Set("Authorization", "Bearer s3cret") }); c != 200 {
		t.Fatalf("bearer=%d", c)
	}
	if c := get(func(r *http.Request) { r.AddCookie(&http.Cookie{Name: tokenCookie, Value: "s3cret"}) }); c != 200 {
		t.Fatalf("cookie=%d", c)
	}
	if w := post(s, "/api/login", `{"token":"nope"}`, ""); w.Code != 401 {
		t.Fatalf("login 错=%d", w.Code)
	}
	if w := post(s, "/api/login", `{"token":"s3cret"}`, ""); w.Code != 200 || len(w.Result().Cookies()) != 1 {
		t.Fatalf("login 对=%d", w.Code)
	}
	if w := post(s, "/api/issue", `{}`, ""); w.Code != 401 {
		t.Fatalf("issue 无口令=%d", w.Code)
	}
}

func TestLoadKeyWarnsOnPerm(t *testing.T) {
	// 生成与 nexadmin keygen 相同格式的私钥文件，权限 0644。
	p := filepath.Join(t.TempDir(), "k.pem")
	_, priv, _ := ed25519.GenerateKey(rand.Reader)
	if err := os.WriteFile(p, pemOf(t, priv), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(p, 0o644); err != nil {
		t.Fatal(err)
	}
	k, warns, err := loadKey(p)
	if err != nil || !k.Equal(priv) || len(warns) != 1 {
		t.Fatalf("err=%v warns=%v", err, warns)
	}
	if err := os.Chmod(p, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, warns, _ = loadKey(p); len(warns) != 0 {
		t.Fatalf("0600 不应警告: %v", warns)
	}
}

// 黄金样本：固定 seed（仅测试用，绝不可用于任何真实授权）+ 固定 payload，签名必须与 testdata/golden.lic 逐字节相等。
// 框架侧 core/plugin/testdata/ 有同一份样本并校验通过，两边格式因此一致且互不引用。
func goldenKey() ed25519.PrivateKey {
	seed := make([]byte, 32)
	for i := range seed {
		seed[i] = byte(i)
	}
	return ed25519.NewKeyFromSeed(seed)
}

func TestGoldenSample(t *testing.T) {
	priv := goldenKey()
	raw, err := signLicense(priv, License{Plugin: "cron", Customer: "Golden Test Customer 测试客户",
		IssuedAt: "2026-01-01T00:00:00Z", ExpiresAt: "2027-01-01T00:00:00Z", Domains: []string{"a.example.com", "*.example.org"}})
	if err != nil {
		t.Fatal(err)
	}
	want, err := os.ReadFile(filepath.Join("testdata", "golden.lic"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(append(raw, '\n'), want) {
		t.Fatalf("与 golden.lic 不一致:\n got %s\nwant %s", raw, want)
	}
	pubB64, err := os.ReadFile(filepath.Join("testdata", "golden.pub"))
	if err != nil {
		t.Fatal(err)
	}
	if got := base64.StdEncoding.EncodeToString(priv.Public().(ed25519.PublicKey)); got != strings.TrimSpace(string(pubB64)) {
		t.Fatalf("golden.pub 不一致: %s", got)
	}
	if _, err := verifyLicense(want, "cron", time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC), priv.Public().(ed25519.PublicKey)); err != nil {
		t.Fatal(err)
	}
}
