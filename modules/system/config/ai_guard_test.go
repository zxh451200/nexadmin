package config

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/gin-gonic/gin"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"

	"nexadmin/core/auth"
	"nexadmin/core/tenant"
	"nexadmin/pkg/idgen"
)

type fakeDNS struct{ ip string }

func (f fakeDNS) LookupIPAddr(context.Context, string) ([]net.IPAddr, error) {
	return []net.IPAddr{{IP: net.ParseIP(f.ip)}}, nil
}

func mockService(t *testing.T) (*Service, sqlmock.Sqlmock) {
	t.Helper()
	sqlDB, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	db, err := gorm.Open(mysql.New(mysql.Config{Conn: sqlDB, SkipInitializeWithVersion: true}), &gorm.Config{DisableAutomaticPing: true})
	if err != nil {
		t.Fatal(err)
	}
	s := NewService(db)
	s.aiResolver = fakeDNS{"93.184.216.34"}
	return s, mock
}

func TestValidateAIBaseURL(t *testing.T) {
	useTestKey(t)
	s, _ := mockService(t)
	cur := map[string]any{"baseUrl": "https://old.example.com/v1", "apiKey": "OLD"}
	merge := func(base string) map[string]any { return map[string]any{"baseUrl": base, "apiKey": "OLD"} }
	ctx := context.Background()

	// 地址不合法：scheme、元数据、默认私网 / 回环。
	for _, u := range []string{"ftp://x.example.com", "http://169.254.169.254/v1", "http://127.0.0.1:11434/v1", "http://10.0.0.5/v1"} {
		body := map[string]any{"baseUrl": u, "apiKey": "NEW"}
		if err := s.validateAI(ctx, cur, body, merge(u)); err == nil {
			t.Errorf("%s must be rejected", u)
		}
	}
	// 修改地址但没带 apiKey，或带的是占位符：拒绝（不得沿用旧密钥发往新地址）。
	nu := "https://new.example.com/v1"
	if err := s.validateAI(ctx, cur, map[string]any{"baseUrl": nu}, merge(nu)); err == nil {
		t.Error("changing baseUrl without apiKey must be rejected")
	}
	if err := s.validateAI(ctx, cur, map[string]any{"baseUrl": nu, "apiKey": secretMask}, merge(nu)); err == nil {
		t.Error("changing baseUrl with the mask placeholder must be rejected")
	}
	// 改地址并重新提交 apiKey：通过；地址没变时不要求 apiKey；末尾斜杠不算改动。
	if err := s.validateAI(ctx, cur, map[string]any{"baseUrl": nu, "apiKey": "NEW"}, merge(nu)); err != nil {
		t.Errorf("new url + new key: %v", err)
	}
	if err := s.validateAI(ctx, cur, map[string]any{"model": "x"}, merge("https://old.example.com/v1")); err != nil {
		t.Errorf("unchanged url: %v", err)
	}
	if err := s.validateAI(ctx, cur, map[string]any{"baseUrl": "https://old.example.com/v1/"}, merge("https://old.example.com/v1/")); err != nil {
		t.Errorf("trailing slash is not a change: %v", err)
	}
	// DNS 解析到元数据地址的主机名被拒绝。
	s.aiResolver = fakeDNS{"169.254.169.254"}
	if err := s.validateAI(ctx, cur, map[string]any{"baseUrl": nu, "apiKey": "K"}, merge(nu)); err == nil {
		t.Error("hostname resolving to metadata must be rejected")
	}
	// 部署方开启 ai.allow_private_base_url 后放行私网 / 回环，但元数据仍拒绝。
	s.aiResolver, s.aiAllowPrivate = fakeDNS{"93.184.216.34"}, true
	for _, u := range []string{"http://127.0.0.1:11434/v1", "http://10.0.0.5/v1"} {
		if err := s.validateAI(ctx, cur, map[string]any{"baseUrl": u, "apiKey": ""}, merge(u)); err != nil {
			t.Errorf("%s with allow_private_base_url: %v", u, err)
		}
	}
	if err := s.validateAI(ctx, cur, map[string]any{"baseUrl": "http://169.254.169.254/", "apiKey": "K"}, merge("http://169.254.169.254/")); err == nil {
		t.Error("metadata must stay rejected with allow_private_base_url")
	}
}

func settingsEngine(h *Handler, cl auth.Claims, tid int64) *gin.Engine {
	gin.SetMode(gin.TestMode)
	e := gin.New()
	e.Use(func(c *gin.Context) {
		auth.SetClaims(c, &cl)
		c.Request = c.Request.WithContext(tenant.With(c.Request.Context(), idgen.ID(tid)))
	})
	e.GET("/settings", h.Settings)
	e.PUT("/settings/:section", h.SaveSection)
	e.POST("/settings/:section/test", h.TestSection)
	return e
}

func call(e *gin.Engine, method, path, body string) (int, string) {
	w := httptest.NewRecorder()
	r := httptest.NewRequest(method, path, strings.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	e.ServeHTTP(w, r)
	return w.Code, w.Body.String()
}

// 租户管理员（非超管、非平台租户）既不能保存也不能测试 ai 分区，且请求不会碰数据库。
func TestAISectionIsPlatformOnly(t *testing.T) {
	s, mock := mockService(t)
	h := &Handler{svc: s}
	tenantAdmin := settingsEngine(h, auth.Claims{UserID: 5, TenantID: 9}, 9)
	body := `{"baseUrl":"https://evil.example.com/v1","apiKey":"x"}`
	for _, tc := range [][2]string{{"PUT", "/settings/ai"}, {"POST", "/settings/ai/test"}} {
		if code, raw := call(tenantAdmin, tc[0], tc[1], body); code != http.StatusForbidden {
			t.Errorf("%s %s = %d %s, want 403", tc[0], tc[1], code, raw)
		}
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("forbidden requests must not touch the database: %v", err)
	}
	// 其他分区不受影响（仍走原有 settings:update 权限，由路由中间件负责）。
	mock.ExpectQuery("SELECT").WillReturnRows(sqlmock.NewRows([]string{"id"}))
	if code, _ := call(tenantAdmin, "POST", "/settings/cache/test", `{}`); code != http.StatusOK {
		t.Errorf("non-ai section test = %d, want 200", code)
	}
	// 超管和平台租户通过守卫（这里用非法地址让请求停在校验，证明确实进入了服务层）。
	for name, e := range map[string]*gin.Engine{
		"super":    settingsEngine(h, auth.Claims{UserID: 1, TenantID: 9, Super: true}, 9),
		"platform": settingsEngine(h, auth.Claims{UserID: 2, TenantID: 1}, 1),
	} {
		mock.ExpectQuery("SELECT").WillReturnRows(sqlmock.NewRows([]string{"id"}))
		code, raw := call(e, "PUT", "/settings/ai", `{"baseUrl":"http://169.254.169.254/v1","apiKey":"x"}`)
		if code != http.StatusBadRequest {
			t.Errorf("%s: %d %s, want 400 from validation", name, code, raw)
		}
	}
}

// 租户读取设置页时 ai 分区只剩 enabled / model，没有地址和密钥。
func TestAISectionHiddenFromTenantAdmins(t *testing.T) {
	s, mock := mockService(t)
	h := &Handler{svc: s}
	for i := 0; i < 2*len(sections); i++ {
		mock.ExpectQuery("SELECT").WillReturnRows(sqlmock.NewRows([]string{"id"}))
	}
	read := func(e *gin.Engine) map[string]any {
		code, raw := call(e, "GET", "/settings", "")
		if code != http.StatusOK {
			t.Fatalf("GET /settings = %d %s", code, raw)
		}
		var out struct{ Data map[string]map[string]any }
		_ = json.Unmarshal([]byte(raw), &out)
		return out.Data["ai"]
	}
	tenantView := read(settingsEngine(h, auth.Claims{UserID: 5, TenantID: 9}, 9))
	if _, ok := tenantView["baseUrl"]; ok || tenantView["apiKey"] != nil || len(tenantView) != 2 {
		t.Fatalf("tenant view leaks AI settings: %v", tenantView)
	}
	platformView := read(settingsEngine(h, auth.Claims{UserID: 1, TenantID: 1, Super: true}, 1))
	if platformView["baseUrl"] == nil {
		t.Fatalf("platform view must keep baseUrl: %v", platformView)
	}
}
