package online

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"

	"nexadmin/core/auth"
	"nexadmin/core/cache"
	"nexadmin/core/config"
	"nexadmin/core/tenant"
	"nexadmin/modules/internal/session"
	"nexadmin/pkg/idgen"
)

const ua = "Mozilla/5.0 (X11; Linux x86_64) Chrome/120 Safari/537"

type env struct {
	store *session.Store
	cache cache.Cache
}

func newEnv(t *testing.T) env {
	t.Helper()
	if err := idgen.Init(1); err != nil {
		t.Fatal(err)
	}
	c, err := cache.New(config.Cache{Driver: "memory"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = c.Close() })
	return env{store: session.NewStore(c, time.Hour), cache: c}
}

func claimsOf(uid int64, name, jti string) *auth.Claims {
	return &auth.Claims{UserID: idgen.ID(uid), Username: name, RID: "r-" + jti, RegisteredClaims: jwt.RegisteredClaims{ID: jti}}
}

// login 在 tenantID 下登记一个会话，返回会话 ID。
func (e env) login(t *testing.T, tenantID idgen.ID, uid int64, name, ip string) string {
	t.Helper()
	ctx := tenant.With(context.Background(), tenantID)
	cl := claimsOf(uid, name, fmt.Sprintf("jti-%d-%s", tenantID, name))
	if err := e.store.Touch(ctx, cl, ip, ua, func() string { return name }); err != nil {
		t.Fatal(err)
	}
	return session.SID(cl.UserID, ip, ua)
}

// serve 以 tenantID 租户、uid 用户（来自 192.0.2.1）发请求。
func (e env) serve(tenantID idgen.ID, uid int64, method, path string) *httptest.ResponseRecorder {
	gin.SetMode(gin.TestMode)
	h := &Handler{store: e.store}
	r := gin.New()
	r.Use(func(c *gin.Context) {
		c.Request = c.Request.WithContext(tenant.With(c.Request.Context(), tenantID))
		auth.SetClaims(c, claimsOf(uid, "me", "jti-me"))
	})
	r.GET("/online-users", h.List)
	r.DELETE("/online-users/:id", h.Kick)
	w := httptest.NewRecorder()
	req := httptest.NewRequest(method, path, nil)
	req.RemoteAddr = "192.0.2.1:5555"
	req.Header.Set("User-Agent", ua)
	r.ServeHTTP(w, req)
	return w
}

type listResp struct {
	Data struct {
		List  []item `json:"list"`
		Total int64  `json:"total"`
	} `json:"data"`
}

func decodeList(t *testing.T, w *httptest.ResponseRecorder) listResp {
	t.Helper()
	var out listResp
	if w.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	return out
}

func TestListIsTenantScopedAndFiltered(t *testing.T) {
	e := newEnv(t)
	e.login(t, 1, 101, "alice", "10.0.0.1")
	e.login(t, 1, 102, "bob", "10.0.0.2")
	e.login(t, 2, 201, "mallory", "10.9.9.9") // 另一个租户

	all := decodeList(t, e.serve(1, 1, http.MethodGet, "/online-users"))
	if all.Data.Total != 2 {
		t.Fatalf("租户 1 只应看到自己的 2 个会话，得到 %d: %+v", all.Data.Total, all.Data.List)
	}
	for _, it := range all.Data.List {
		if it.Username == "mallory" {
			t.Fatal("租户 1 看到了租户 2 的在线用户")
		}
	}
	other := decodeList(t, e.serve(2, 1, http.MethodGet, "/online-users"))
	if other.Data.Total != 1 || other.Data.List[0].Username != "mallory" {
		t.Fatalf("租户 2 应只看到 mallory: %+v", other.Data.List)
	}

	// 关键字匹配用户名 / 昵称 / IP，不区分大小写
	for kw, want := range map[string]string{"ALI": "alice", "10.0.0.2": "bob", "nomatch": ""} {
		got := decodeList(t, e.serve(1, 1, http.MethodGet, "/online-users?keyword="+kw))
		switch {
		case want == "" && got.Data.Total != 0:
			t.Errorf("keyword=%s 不应有结果: %+v", kw, got.Data.List)
		case want != "" && (got.Data.Total != 1 || got.Data.List[0].Username != want):
			t.Errorf("keyword=%s 期望 %s: %+v", kw, want, got.Data.List)
		}
	}
}

func TestListPagination(t *testing.T) {
	e := newEnv(t)
	for i := 0; i < 5; i++ {
		e.login(t, 1, int64(100+i), "u"+strconv.Itoa(i), "10.0.0."+strconv.Itoa(i))
	}
	p2 := decodeList(t, e.serve(1, 1, http.MethodGet, "/online-users?page=2&pageSize=2"))
	if p2.Data.Total != 5 || len(p2.Data.List) != 2 {
		t.Fatalf("第 2 页应有 2 条、总数 5: %+v", p2.Data)
	}
	beyond := decodeList(t, e.serve(1, 1, http.MethodGet, "/online-users?page=9&pageSize=2"))
	if beyond.Data.Total != 5 || len(beyond.Data.List) != 0 {
		t.Fatalf("越界页应返回空列表而不是 panic: %+v", beyond.Data)
	}
}

func TestKickOnlyAffectsTargetSession(t *testing.T) {
	e := newEnv(t)
	target := e.login(t, 1, 101, "alice", "10.0.0.1")
	bystander := e.login(t, 1, 102, "bob", "10.0.0.2")

	w := e.serve(1, 1, http.MethodDelete, "/online-users/"+target)
	if w.Code != http.StatusOK {
		t.Fatalf("强退应成功: %d %s", w.Code, w.Body.String())
	}
	left := decodeList(t, e.serve(1, 1, http.MethodGet, "/online-users"))
	if left.Data.Total != 1 || left.Data.List[0].ID != bystander {
		t.Fatalf("只应剩下无关会话: %+v", left.Data.List)
	}
	ctx := tenant.With(context.Background(), 1)
	for _, jti := range []string{"jti-1-alice", "r-jti-1-alice"} {
		if _, err := e.cache.Get(ctx, cache.GlobalKey("auth", "revoked", jti)); err != nil {
			t.Errorf("目标会话的令牌 %s 应被吊销: %v", jti, err)
		}
	}
	for _, jti := range []string{"jti-1-bob", "r-jti-1-bob"} {
		if _, err := e.cache.Get(ctx, cache.GlobalKey("auth", "revoked", jti)); err == nil {
			t.Errorf("无关会话的令牌 %s 不应被吊销", jti)
		}
	}
	if w := e.serve(1, 1, http.MethodDelete, "/online-users/"+target); w.Code != http.StatusNotFound {
		t.Errorf("重复强退同一会话应 404，得到 %d", w.Code)
	}
}

func TestKickCannotCrossTenant(t *testing.T) {
	e := newEnv(t)
	victim := e.login(t, 2, 201, "victim", "10.1.1.1")

	if w := e.serve(1, 1, http.MethodDelete, "/online-users/"+victim); w.Code != http.StatusNotFound {
		t.Fatalf("租户 1 强退租户 2 的会话应 404，得到 %d", w.Code)
	}
	still := decodeList(t, e.serve(2, 1, http.MethodGet, "/online-users"))
	if still.Data.Total != 1 {
		t.Fatalf("跨租户强退不能影响对方会话: %+v", still.Data.List)
	}
	ctx := tenant.With(context.Background(), 2)
	if _, err := e.cache.Get(ctx, cache.GlobalKey("auth", "revoked", "jti-2-victim")); err == nil {
		t.Error("跨租户强退不能吊销对方令牌")
	}
}

func TestKickRejectsCurrentSessionAndBadIDs(t *testing.T) {
	e := newEnv(t)
	// 当前请求的会话 = SID(uid, 192.0.2.1, ua)；先登记它，确认被拒绝后仍在线
	me := e.login(t, 1, 1, "me", "192.0.2.1")
	if w := e.serve(1, 1, http.MethodDelete, "/online-users/"+me); w.Code != http.StatusBadRequest {
		t.Fatalf("强退当前会话应 400，得到 %d", w.Code)
	}
	if got := decodeList(t, e.serve(1, 1, http.MethodGet, "/online-users")); got.Data.Total != 1 {
		t.Errorf("当前会话应仍在线: %+v", got.Data.List)
	}
	for _, id := range []string{"abc", "1.5", "0x10"} {
		w := e.serve(1, 1, http.MethodDelete, "/online-users/"+id)
		if w.Code != http.StatusBadRequest {
			t.Errorf("id=%q 应 400，得到 %d", id, w.Code)
		}
	}
}
