package main

import (
	"bytes"
	"crypto/ed25519"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func issueOne(t *testing.T, s *server, plugin string) Record {
	t.Helper()
	w := post(s, "/api/issue", `{"plugin":"`+plugin+`","customer":"ACME","expires":"2027-01-01"}`, "s3cret")
	if w.Code != 200 {
		t.Fatalf("issue: %d %s", w.Code, w.Body)
	}
	var out struct {
		License string `json:"license"`
		Record  Record `json:"record"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	return out.Record
}

func get(s *server, path, token string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodGet, path, nil)
	if token != "" {
		req.Header.Set("X-Licensor-Token", token)
	}
	w := httptest.NewRecorder()
	s.routes().ServeHTTP(w, req)
	return w
}

func TestIssueWritesSerialIntoLicense(t *testing.T) {
	now := time.Date(2026, 10, 17, 0, 0, 0, 0, time.UTC)
	s, pub, _ := newTestServer(t, now)
	w := post(s, "/api/issue", `{"plugin":"cron","customer":"ACME","expires":"2027-01-01"}`, "s3cret")
	var out struct {
		License string `json:"license"`
		Record  Record `json:"record"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	l, err := verifyLicense([]byte(out.License), "cron", now, pub)
	if err != nil {
		t.Fatal(err)
	}
	if l.Serial == "" || l.Serial != out.Record.ID || l.Serial != out.Record.Serial {
		t.Fatalf("serial must equal record ID: license=%q record=%+v", l.Serial, out.Record)
	}
}

func TestRevokeAndExport(t *testing.T) {
	now := time.Date(2026, 10, 17, 0, 0, 0, 0, time.UTC)
	s, pub, dir := newTestServer(t, now)
	a := issueOne(t, s, "cron")
	b := issueOne(t, s, "audit")
	// 旧记录：没有序列号（授权载荷里也没有），不能被吊销。
	if err := s.st.Add(Record{ID: "legacy000001", IssuedAt: "2025-01-01T00:00:00Z", Plugin: "cron", Customer: "Old", ExpiresAt: "2027-01-01T00:00:00Z", KID: s.kid}); err != nil {
		t.Fatal(err)
	}

	// 鉴权：无口令 401（吊销和导出都一样）；缺自定义请求头 400。
	if w := post(s, "/api/revoke", `{"serial":"`+a.Serial+`","reason":"x"}`, ""); w.Code != 401 {
		t.Fatalf("unauthenticated revoke => %d", w.Code)
	}
	if w := get(s, "/api/revoked.json", ""); w.Code != 401 {
		t.Fatalf("unauthenticated export => %d", w.Code)
	}
	req := httptest.NewRequest(http.MethodPost, "/api/revoke", strings.NewReader(`{"serial":"`+a.Serial+`","reason":"x"}`))
	req.Header.Set("X-Licensor-Token", "s3cret")
	if w := httptest.NewRecorder(); func() int { s.routes().ServeHTTP(w, req); return w.Code }() != 400 {
		t.Fatal("missing X-Requested-With must be rejected")
	}
	// 参数校验。
	for name, body := range map[string]string{
		"no reason":      `{"serial":"` + a.Serial + `","reason":"  "}`,
		"long reason":    `{"serial":"` + a.Serial + `","reason":"` + strings.Repeat("长", maxReasonRunes+1) + `"}`,
		"unknown serial": `{"serial":"ffffffffffff","reason":"x"}`,
		"legacy record":  `{"serial":"","reason":"x"}`,
		"legacy by id":   `{"serial":"legacy000001","reason":"x"}`,
	} {
		if w := post(s, "/api/revoke", body, "s3cret"); w.Code != 400 && w.Code != 404 {
			t.Errorf("%s => %d", name, w.Code)
		}
	}
	if w := get(s, "/api/revoked.json", "s3cret"); w.Code != 404 {
		t.Fatalf("no revocations yet => %d", w.Code)
	}
	if len(s.st.Revoked().Entries) != 0 {
		t.Fatal("failed requests must not write revocations")
	}

	// 吊销 a。
	if w := post(s, "/api/revoke", `{"serial":"`+a.Serial+`","reason":"退款"}`, "s3cret"); w.Code != 200 {
		t.Fatalf("revoke: %d %s", w.Code, w.Body)
	}
	if w := post(s, "/api/revoke", `{"serial":"`+a.Serial+`","reason":"again"}`, "s3cret"); w.Code != 409 {
		t.Fatalf("duplicate => %d", w.Code)
	}
	w1 := get(s, "/api/revoked.json", "s3cret")
	if w1.Code != 200 || !strings.Contains(w1.Header().Get("Content-Disposition"), "license-revoked.json") {
		t.Fatalf("export: %d %v", w1.Code, w1.Header())
	}
	r1, err := verifyRevocations(bytes.TrimSpace(w1.Body.Bytes()), pub)
	if err != nil || r1.Seq != 1 || len(r1.Entries) != 1 || r1.Entries[0].Serial != a.Serial || r1.Entries[0].Reason != "退款" || r1.Entries[0].Plugin != "cron" {
		t.Fatalf("got %+v, %v", r1, err)
	}
	// 同一份记录反复导出字节相同（签名确定性），不会无故涨 seq。
	if w := get(s, "/api/revoked.json", "s3cret"); !bytes.Equal(w.Body.Bytes(), w1.Body.Bytes()) {
		t.Fatal("re-export must be byte-identical")
	}
	// 篡改后验签失败。
	if _, err := verifyRevocations(bytes.Replace(w1.Body.Bytes(), []byte(`"payload": "`), []byte(`"payload": "AA`), 1), pub); err == nil {
		t.Fatal("tampered list must not verify")
	}

	// 再吊销 b：清单是全量的，seq 加一。
	post(s, "/api/revoke", `{"serial":"`+b.Serial+`","reason":"滥用"}`, "s3cret")
	r2, err := verifyRevocations(bytes.TrimSpace(get(s, "/api/revoked.json", "s3cret").Body.Bytes()), pub)
	if err != nil || r2.Seq != 2 || len(r2.Entries) != 2 {
		t.Fatalf("got %+v, %v", r2, err)
	}

	// 记录视图与 CSV 带吊销信息；旧记录不是 revoked。
	var rv struct {
		Records []recView `json:"records"`
	}
	_ = json.Unmarshal(get(s, "/api/records", "s3cret").Body.Bytes(), &rv)
	got := map[string]recView{}
	for _, v := range rv.Records {
		got[v.ID] = v
	}
	if !got[a.ID].Revoked || got[a.ID].RevokeReason != "退款" || got["legacy000001"].Revoked {
		t.Fatalf("views: %+v", got)
	}
	if csv := get(s, "/api/export.csv", "s3cret").Body.String(); !strings.Contains(csv, "吊销原因") || !strings.Contains(csv, "退款") {
		t.Fatalf("csv: %s", csv)
	}

	// 持久化：权限 0600，重新打开后仍在；不含私钥/签名。
	p := filepath.Join(dir, "revoked.json")
	if fi, err := os.Stat(p); err != nil || fi.Mode().Perm() != 0o600 {
		t.Fatalf("revoked.json perm: %v %v", fi, err)
	}
	if raw, _ := os.ReadFile(p); bytes.Contains(raw, []byte("signature")) {
		t.Fatal("revoked.json must not contain signatures")
	}
	st2, err := openStore(dir)
	if err != nil || st2.Revoked().Seq != 2 || len(st2.Revoked().Entries) != 2 {
		t.Fatalf("reopen: %+v %v", st2, err)
	}
}

// 黄金样本：固定 seed + 固定载荷。framework 侧 core/plugin/testdata/ 放有同一份并校验通过，两边格式因此一致且互不引用。
// 重新生成：UPDATE_GOLDEN=1 go test -run TestGoldenRevocation（然后把两个文件复制到框架的 testdata）。
func TestGoldenRevocation(t *testing.T) {
	priv := goldenKey()
	lic, err := signLicense(priv, License{Plugin: "cron", Customer: "Golden Test Customer 测试客户",
		IssuedAt: "2026-01-01T00:00:00Z", ExpiresAt: "2027-01-01T00:00:00Z", Domains: []string{"a.example.com"}, Serial: "golden000001"})
	if err != nil {
		t.Fatal(err)
	}
	rev, err := signRevocations(priv, revData{Seq: 3, UpdatedAt: "2026-06-01T00:00:00Z", Entries: []RevokedEntry{
		{Serial: "golden000001", Plugin: "cron", RevokedAt: "2026-05-01T00:00:00Z", Reason: "退款 refund"},
		{Serial: "golden000002", RevokedAt: "2026-06-01T00:00:00Z"},
	}})
	if err != nil {
		t.Fatal(err)
	}
	files := map[string][]byte{"golden-serial.lic": lic, "golden-revoked.json": rev}
	for name, raw := range files {
		p := filepath.Join("testdata", name)
		if os.Getenv("UPDATE_GOLDEN") == "1" {
			if err := os.WriteFile(p, append(raw, '\n'), 0o644); err != nil {
				t.Fatal(err)
			}
			continue
		}
		want, err := os.ReadFile(p)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(append(raw, '\n'), want) {
			t.Fatalf("与 %s 不一致:\n got %s\nwant %s", name, raw, want)
		}
	}
	pub := priv.Public().(ed25519.PublicKey)
	if _, err := verifyRevocations(rev, pub); err != nil {
		t.Fatal(err)
	}
}
