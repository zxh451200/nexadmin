package sso

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
	"go.uber.org/zap"

	"nexadmin/core/middleware"
	"nexadmin/pkg/idgen"
)

func TestKeyRotationKeepsOldTokensVerifiable(t *testing.T) {
	ks, err := loadKeyStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	clock := time.Now()
	ks.now = func() time.Time { return clock }

	claims := func() jwt.MapClaims { return jwt.MapClaims{"sub": "42", "exp": clock.Add(24 * time.Hour * 30).Unix()} }
	oldTok, err := ks.sign(claims(), "at+jwt")
	if err != nil {
		t.Fatal(err)
	}
	oldKid := ks.active().kid

	clock = clock.Add(time.Hour)
	if err := ks.Rotate(); err != nil {
		t.Fatal(err)
	}
	if ks.active().kid == oldKid {
		t.Fatal("rotation must change the signing key")
	}
	newTok, _ := ks.sign(claims(), "at+jwt")
	if _, _, err := ks.parse(oldTok); err != nil {
		t.Fatalf("token signed by the previous key must still verify after rotation: %v", err)
	}
	if _, _, err := ks.parse(newTok); err != nil {
		t.Fatalf("new token must verify: %v", err)
	}
	if n := len(ks.jwks()["keys"].([]map[string]string)); n != 2 {
		t.Fatalf("JWKS should publish both keys, got %d", n)
	}
	infos := ks.list()
	if len(infos) != 2 || infos[0].Status != "active" || infos[1].Status != "retiring" || infos[1].RetiresAt == nil || infos[1].Kid != oldKid {
		t.Fatalf("unexpected key list: %+v", infos)
	}

	// 取代 7 天内仍保留；满 7 天后旧 key 从 JWKS 移除，旧令牌不再可验证，当前 key 不受影响
	clock = clock.Add(retireAfter - time.Minute)
	if _, _, err := ks.parse(oldTok); err != nil {
		t.Fatalf("old key must be kept until 7 days after rotation: %v", err)
	}
	clock = clock.Add(2 * time.Minute)
	if _, _, err := ks.parse(oldTok); err == nil {
		t.Fatal("old key must be gone after 7 days")
	}
	if n := len(ks.jwks()["keys"].([]map[string]string)); n != 1 {
		t.Fatalf("JWKS should only publish the active key, got %d", n)
	}
	if infos := ks.list(); len(infos) != 1 || infos[0].Status != "active" {
		t.Fatalf("expired key must disappear from the list: %+v", infos)
	}
	if _, _, err := ks.parse(newTok); err != nil {
		t.Fatalf("active key never expires: %v", err)
	}
}

type fakeOrg struct {
	tenant idgen.ID
	size   int
	since  *time.Time
}

func (f *fakeOrg) orgUsers(_ context.Context, tid idgen.ID, since *time.Time, _ int, size int) ([]orgUser, int64, error) {
	f.tenant, f.size, f.since = tid, size, since
	return []orgUser{{ID: "1", Username: "alice"}, {ID: "2", Username: "gone", Deleted: true}}, 2, nil
}
func (f *fakeOrg) orgDepts(_ context.Context, tid idgen.ID) ([]orgDept, error) {
	f.tenant = tid
	return []orgDept{{ID: "9", Name: "研发部"}}, nil
}

func orgEngine(p *provider, org orgStore) *gin.Engine {
	gin.SetMode(gin.TestMode)
	h := &handler{p: p, log: zap.NewNop(), lim: middleware.NewLimiter(nil, nil, "sso-token", 0, time.Minute), org: org}
	e := gin.New()
	e.GET("/org/users", h.orgUsersHandler)
	e.GET("/org/depts", h.orgDeptsHandler)
	return e
}

func getJSON(e *gin.Engine, path, token string) (int, map[string]any) {
	req := httptest.NewRequest(http.MethodGet, path, nil)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	w := httptest.NewRecorder()
	e.ServeHTTP(w, req)
	var out map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &out)
	return w.Code, out
}

func clientCredentials(p *provider, scope string) (*tokenResponse, *oauthErr) {
	return p.token(context.Background(), testIss, tokenRequest{GrantType: "client_credentials", Scope: scope,
		ClientID: "nxsso_test", ClientSecret: testSecret})
}

func TestClientCredentialsOnlyForCheckedApps(t *testing.T) {
	p, st := newTestProvider(t)

	// 未勾选 org.read：拒绝签发
	if _, e := clientCredentials(p, ""); e == nil || e.Code != "unauthorized_client" {
		t.Fatalf("unchecked app must be refused, got %v", e)
	}
	// 密钥错误仍是 invalid_client
	if _, e := p.token(context.Background(), testIss, tokenRequest{GrantType: "client_credentials", ClientID: "nxsso_test", ClientSecret: "nope"}); e == nil || e.Code != "invalid_client" {
		t.Fatalf("bad secret must be invalid_client, got %v", e)
	}

	st.app.AllowOrgRead = true
	if _, e := clientCredentials(p, "openid"); e == nil || e.Code != "invalid_scope" {
		t.Fatalf("only org.read may be requested, got %v", e)
	}
	resp, e := clientCredentials(p, "org.read")
	if e != nil {
		t.Fatalf("checked app must get a token: %v", e)
	}
	if resp.IDToken != "" || resp.RefreshToken != "" || resp.Scope != "org.read" {
		t.Fatalf("client_credentials must not issue id/refresh tokens: %+v", resp)
	}

	org := &fakeOrg{}
	eng := orgEngine(p, org)
	if code, _ := getJSON(eng, "/org/users", ""); code != http.StatusUnauthorized {
		t.Fatalf("no token must be 401, got %d", code)
	}
	code, body := getJSON(eng, "/org/users?updated_since=2025-01-01T00:00:00Z&page_size=9999", resp.AccessToken)
	if code != 200 || org.tenant != st.app.TenantID || org.size != orgMaxPage || org.since == nil {
		t.Fatalf("users: code=%d tenant=%v size=%d since=%v", code, org.tenant, org.size, org.since)
	}
	list := body["list"].([]any)
	if len(list) != 2 || list[1].(map[string]any)["deleted"] != true {
		t.Fatalf("soft-deleted users must carry deleted:true: %v", body)
	}
	if code, _ := getJSON(eng, "/org/users?updated_since=garbage", resp.AccessToken); code != http.StatusBadRequest {
		t.Fatalf("bad updated_since must be 400, got %d", code)
	}
	if code, _ := getJSON(eng, "/org/depts", resp.AccessToken); code != 200 {
		t.Fatalf("depts: %d", code)
	}

	// 用户授权码流程签发的令牌不能读通讯录
	code0, ver, _ := authorizeCode(t, p, st, testRedirect)
	userTok, e := exchange(p, code0, ver, testRedirect)
	if e != nil {
		t.Fatal(e)
	}
	if code, _ := getJSON(eng, "/org/users", userTok.AccessToken); code != http.StatusUnauthorized {
		t.Fatalf("user access token must not read org data, got %d", code)
	}

	// 取消勾选后，已签发的令牌立即失效；停用应用同理
	st.app.AllowOrgRead = false
	if code, _ := getJSON(eng, "/org/users", resp.AccessToken); code != http.StatusUnauthorized {
		t.Fatalf("revoked org.read must invalidate tokens, got %d", code)
	}
	st.app.AllowOrgRead, st.app.Enabled = true, false
	if code, _ := getJSON(eng, "/org/depts", resp.AccessToken); code != http.StatusUnauthorized {
		t.Fatalf("disabled app must be refused, got %d", code)
	}
}

func TestParseSince(t *testing.T) {
	if v, ok := parseSince(""); !ok || v != nil {
		t.Fatal("empty is allowed")
	}
	if v, ok := parseSince("1700000000"); !ok || v.Unix() != 1700000000 {
		t.Fatal("unix seconds")
	}
	if _, ok := parseSince(url.QueryEscape("x y")); ok || strings.Contains("", "x") {
		t.Fatal("garbage must fail")
	}
}
