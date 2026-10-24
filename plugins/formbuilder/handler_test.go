package formbuilder

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"nexadmin/core/auth"
	"nexadmin/core/config"
	"nexadmin/core/middleware"
	"nexadmin/core/tenant"
	"nexadmin/pkg/idgen"
)

// permProvider 按用户返回固定权限，用来驱动真实的 middleware.Perm。
type permProvider struct{ perms map[idgen.ID][]string }

func (p permProvider) Login(context.Context, auth.LoginRequest) (*auth.User, error) { return nil, nil }
func (p permProvider) Profile(_ context.Context, id idgen.ID) (*auth.User, error) {
	return &auth.User{ID: id, Perms: p.perms[id]}, nil
}

// 期望的"路由 -> 权限码"。新增路由而忘了在这里登记（或忘了加 core.Perm）会被 TestEveryRouteHasPerm 抓到。
var routePerms = map[string]string{
	"GET /api/v1/plugins/formbuilder/roles":                         "formbuilder:design",
	"GET /api/v1/plugins/formbuilder/users":                         "formbuilder:design",
	"GET /api/v1/plugins/formbuilder/forms":                         "formbuilder:design",
	"POST /api/v1/plugins/formbuilder/forms":                        "formbuilder:design",
	"POST /api/v1/plugins/formbuilder/forms/import":                 "formbuilder:design",
	"GET /api/v1/plugins/formbuilder/forms/:id":                     "formbuilder:design",
	"PUT /api/v1/plugins/formbuilder/forms/:id":                     "formbuilder:design",
	"DELETE /api/v1/plugins/formbuilder/forms/:id":                  "formbuilder:design",
	"GET /api/v1/plugins/formbuilder/forms/:id/definition":          "formbuilder:design",
	"GET /api/v1/plugins/formbuilder/forms/:id/versions":            "formbuilder:design",
	"GET /api/v1/plugins/formbuilder/forms/:id/versions/:v":         "formbuilder:design",
	"POST /api/v1/plugins/formbuilder/forms/:id/publish":            "formbuilder:publish",
	"POST /api/v1/plugins/formbuilder/forms/:id/offline":            "formbuilder:publish",
	"POST /api/v1/plugins/formbuilder/forms/:id/online":             "formbuilder:publish",
	"GET /api/v1/plugins/formbuilder/fill/stats":                    "formbuilder:fill",
	"GET /api/v1/plugins/formbuilder/fill/forms":                    "formbuilder:fill",
	"GET /api/v1/plugins/formbuilder/fill/forms/:id":                "formbuilder:fill",
	"POST /api/v1/plugins/formbuilder/fill/forms/:id/submit":        "formbuilder:fill",
	"GET /api/v1/plugins/formbuilder/forms/:id/submission-meta":     "formbuilder:submission",
	"GET /api/v1/plugins/formbuilder/forms/:id/submissions":         "formbuilder:submission",
	"GET /api/v1/plugins/formbuilder/forms/:id/submissions/:sid":    "formbuilder:submission",
	"DELETE /api/v1/plugins/formbuilder/forms/:id/submissions/:sid": "formbuilder:submission",
	"GET /api/v1/plugins/formbuilder/forms/:id/export":              "formbuilder:export",
	"POST /api/v1/plugins/formbuilder/forms/:id/export-job":         "formbuilder:export",
}

var allPerms = []string{"formbuilder:design", "formbuilder:publish", "formbuilder:fill", "formbuilder:submission", "formbuilder:export"}

// testEngine 返回挂载了全部插件路由的引擎。请求头 X-Uid 决定登录用户，X-Super / X-App 模拟超管与应用令牌。
func testEngine(t *testing.T, svc *Service, perms map[idgen.ID][]string) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)
	as, err := auth.NewService(&config.Config{Auth: config.Auth{JWTSecret: strings.Repeat("s", 32)}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	as.SetProvider(permProvider{perms})
	prev := auth.Default()
	auth.SetDefault(as)
	t.Cleanup(func() { auth.SetDefault(prev) })

	e := gin.New()
	e.Use(func(c *gin.Context) {
		c.Request = c.Request.WithContext(tenant.With(c.Request.Context(), 1))
		if uid := c.GetHeader("X-Uid"); uid != "" {
			n, _ := strconv.ParseInt(uid, 10, 64)
			cl := &auth.Claims{UserID: idgen.ID(n), TenantID: 1, Username: "u" + uid, Super: c.GetHeader("X-Super") != ""}
			if c.GetHeader("X-App") != "" {
				cl.UserID, cl.Principal, cl.Perms = 0, auth.PrincipalApp, allPerms
			}
			auth.SetClaims(c, cl)
		}
		c.Next()
	})
	_ = middleware.Perm // core.Perm 内部使用
	routes(e.Group("/api/v1"), &handler{svc: svc})
	return e
}

func call(e *gin.Engine, method, path, uid string, body string, hdr ...string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	if uid != "" {
		req.Header.Set("X-Uid", uid)
	}
	for i := 0; i+1 < len(hdr); i += 2 {
		req.Header.Set(hdr[i], hdr[i+1])
	}
	w := httptest.NewRecorder()
	e.ServeHTTP(w, req)
	return w
}

func concrete(route string) (method, path string) {
	method, path, _ = strings.Cut(route, " ")
	path = strings.NewReplacer(":id", "42", ":sid", "9", ":v", "1").Replace(path)
	return
}

func TestEveryRouteHasPerm(t *testing.T) {
	s, _, _ := newSvc()
	e := testEngine(t, s, nil)
	seen := map[string]bool{}
	for _, r := range e.Routes() {
		key := r.Method + " " + r.Path
		if _, ok := routePerms[key]; !ok {
			t.Errorf("路由 %s 没有登记权限码（或忘了加 core.Perm）", key)
		}
		seen[key] = true
		if !strings.HasPrefix(r.Path, "/api/v1/plugins/formbuilder/") {
			t.Errorf("路由前缀不对: %s", r.Path)
		}
	}
	for key := range routePerms {
		if !seen[key] {
			t.Errorf("登记了但未注册的路由: %s", key)
		}
	}
}

func TestPermissionEnforcement(t *testing.T) {
	s, _, _ := newSvc()
	perms := map[idgen.ID][]string{
		1: nil, // 登录但没有任何权限
		2: {"formbuilder:design"}, 3: {"formbuilder:publish"}, 4: {"formbuilder:fill"}, 5: {"formbuilder:submission"}, 6: {"formbuilder:export"},
	}
	// 拥有"除了某一个以外的全部权限"的用户：用来证明权限码是分开的
	for i, p := range allPerms {
		var others []string
		for _, q := range allPerms {
			if q != p {
				others = append(others, q)
			}
		}
		perms[idgen.ID(100+i)] = others
	}
	e := testEngine(t, s, perms)
	for route, need := range routePerms {
		method, path := concrete(route)
		// 未登录 -> 401
		if w := call(e, method, path, "", "{}"); w.Code != http.StatusUnauthorized {
			t.Errorf("%s 未登录应 401，实际 %d", route, w.Code)
		}
		// 登录但没有任何权限 -> 403
		if w := call(e, method, path, "1", "{}"); w.Code != http.StatusForbidden {
			t.Errorf("%s 无权限应 403，实际 %d", route, w.Code)
		}
		// 拥有其他全部权限但缺这一个 -> 403
		var missing string
		for i, p := range allPerms {
			if p == need {
				missing = strconv.Itoa(100 + i)
			}
		}
		if w := call(e, method, path, missing, "{}"); w.Code != http.StatusForbidden {
			t.Errorf("%s 缺少 %s 应 403，实际 %d", route, need, w.Code)
		}
		// 只拥有这一个权限 -> 通过权限拦截（后续可能是 400/404 等业务结果，但不是 401/403）
		var only string
		for i, p := range allPerms {
			if p == need {
				only = strconv.Itoa(2 + i)
			}
		}
		if w := call(e, method, path, only, "{}"); w.Code == http.StatusForbidden || w.Code == http.StatusUnauthorized {
			t.Errorf("%s 拥有 %s 时不应被拦截，实际 %d", route, need, w.Code)
		}
	}
}

func TestAppPrincipalCannotFill(t *testing.T) {
	s, _, _ := newSvc()
	id := publish(t, s, ctxT1, simpleDef("f", fa))
	e := testEngine(t, s, nil)
	path := "/api/v1/plugins/formbuilder/fill/forms/" + id.String() + "/submit"
	// 应用令牌有权限码，但没有用户身份，不能代表任何人填写
	w := call(e, "POST", path, "7", `{"data":{"a":"x"}}`, "X-App", "1")
	if w.Code != http.StatusForbidden {
		t.Fatalf("应用令牌不能填写: %d %s", w.Code, w.Body.String())
	}
}

func TestSubmitOverHTTP(t *testing.T) {
	s, st, _ := newSvc()
	id := publish(t, s, ctxT1, simpleDef("f", fa))
	e := testEngine(t, s, map[idgen.ID][]string{7: {"formbuilder:fill"}})
	path := "/api/v1/plugins/formbuilder/fill/forms/" + id.String() + "/submit"

	if w := call(e, "POST", path, "7", `{"data":{"a":""}}`); w.Code != http.StatusBadRequest || !strings.Contains(w.Body.String(), "必填") {
		t.Fatalf("必填校验: %d %s", w.Code, w.Body.String())
	}
	if w := call(e, "POST", path, "7", `{"data":`); w.Code != http.StatusBadRequest {
		t.Fatalf("损坏的 JSON: %d", w.Code)
	}
	if w := call(e, "POST", path, "7", `{"data":{"a":"`+strings.Repeat("x", MaxRequestBytes)+`"}}`); w.Code != http.StatusBadRequest {
		t.Fatalf("超大请求体应被拒绝: %d", w.Code)
	}
	w := call(e, "POST", path, "7", `{"data":{"a":"hi"}}`)
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"version":1`) {
		t.Fatalf("正常提交: %d %s", w.Code, w.Body.String())
	}
	if w := call(e, "POST", path, "7", `{"data":{"a":"hi"}}`); w.Code != http.StatusConflict {
		t.Fatalf("重复提交: %d", w.Code)
	}
	if f, _ := st.GetForm(ctxT1, id); f.SubmissionCount != 1 {
		t.Fatalf("计数 %d", f.SubmissionCount)
	}
	// ID 路径参数必须是合法数字
	if w := call(e, "GET", "/api/v1/plugins/formbuilder/fill/forms/abc", "7", ""); w.Code != http.StatusBadRequest {
		t.Fatalf("非法 ID: %d", w.Code)
	}
}
