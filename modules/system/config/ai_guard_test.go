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

	"nexadmin/core"
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
	// 与真实注册同形（module.go）：sys_setting 整表没有 tenant_id，任何分区都是部署级的一行，
	// 所以写与测试统一挂 core.RequirePlatform()，不再按分区名特判。
	e.PUT("/settings/:section", core.RequirePlatform(), h.SaveSection)
	e.POST("/settings/:section/test", core.RequirePlatform(), h.TestSection)
	return e
}

func call(e *gin.Engine, method, path, body string) (int, string) {
	w := httptest.NewRecorder()
	r := httptest.NewRequest(method, path, strings.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	e.ServeHTTP(w, r)
	return w.Code, w.Body.String()
}

// 租户管理员（非超管、非平台租户）改不动任何设置分区，且请求不会碰数据库。
// 原先只有 ai 分区受限，而 security / auth / sms / notify 同样是 sys_setting 里那一份全局行：
// 一个租户把 maxAttempts 设成 1 就能把别人全锁在门外，或把出站邮件指向自己的网关。
func TestSettingsWritesArePlatformOnly(t *testing.T) {
	s, mock := mockService(t)
	h := &Handler{svc: s}
	tenantAdmin := settingsEngine(h, auth.Claims{UserID: 5, TenantID: 9}, 9)
	body := `{"baseUrl":"https://evil.example.com/v1","apiKey":"x"}`
	for _, tc := range [][2]string{
		{"PUT", "/settings/ai"}, {"POST", "/settings/ai/test"},
		// 非 ai 分区也一起收口：这一条以前是 200，正是本轮要堵的洞。
		{"POST", "/settings/cache/test"}, {"PUT", "/settings/security"}, {"PUT", "/settings/sms"},
	} {
		if code, raw := call(tenantAdmin, tc[0], tc[1], body); code != http.StatusForbidden {
			t.Errorf("%s %s = %d %s, want 403", tc[0], tc[1], code, raw)
		}
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("forbidden requests must not touch the database: %v", err)
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

// 非平台调用方读设置页时，凭据字段不许原样下发：smtp 密码、短信 access key、SSO client secret
// 都是部署级的，整分区不返回会让界面变成一片空白（那是另一种入口蒸发），所以只抹值不删键。
func TestRestrictSecretsMasksCredentialFields(t *testing.T) {
	in := map[string]any{
		"notify": map[string]any{
			"mail": map[string]any{"host": "mail.example.com", "password": "smtp-pass", "username": "ops@example.com"},
			// webhook 地址自带 access_token：字段名是 url，按名字表永远漏，所以要按上下文码。
			"robots": []any{map[string]any{"name": "值班群", "type": "dingtalk", "url": "https://oapi.dingtalk.com/robot/send?access_token=AAA", "secretKey": "SEC", "markdown": true}},
		},
		"sms": map[string]any{
			"providers": []any{map[string]any{"name": "ali", "accessKey": "AK-1", "accessSecret": "SK-1"}},
		},
		"security": map[string]any{"maxAttempts": 5, "loginTokenSecret": "jwt-key"},
		// 列表外的 url 是普通展示字段，不许误伤。
		"site": map[string]any{"siteName": "NexAdmin", "logoUrl": "/icon.svg"},
	}
	restrictSecrets(in)
	mail := in["notify"].(map[string]any)["mail"].(map[string]any)
	if mail["password"] != secretMask {
		t.Errorf("mail.password = %v, want 掩码 %q", mail["password"], secretMask)
	}
	if mail["host"] != "mail.example.com" || mail["username"] != "ops@example.com" {
		t.Errorf("非凭据字段被动过：%v", mail)
	}
	robot := in["notify"].(map[string]any)["robots"].([]any)[0].(map[string]any)
	if robot["url"] != secretMask {
		t.Errorf("robots[].url = %v, want 掩码：webhook 地址自带 access_token，拿到就能往群里发消息", robot["url"])
	}
	if robot["secretKey"] != secretMask {
		t.Errorf("robots[].secretKey = %v, want 掩码", robot["secretKey"])
	}
	if robot["name"] != "值班群" || robot["markdown"] != true {
		t.Errorf("机器人条目的结构被删坏了：%v", robot)
	}
	if site := in["site"].(map[string]any); site["logoUrl"] != "/icon.svg" {
		t.Errorf("site.logoUrl 被误伤：%v", site["logoUrl"])
	}
	prov := in["sms"].(map[string]any)["providers"].([]any)[0].(map[string]any)
	if prov["accessKey"] != secretMask || prov["accessSecret"] != secretMask {
		t.Errorf("嵌套里的密钥没抹掉：%v", prov)
	}
	if prov["name"] != "ali" {
		t.Errorf("结构被删坏了：%v", prov)
	}
	if in["security"].(map[string]any)["maxAttempts"] != 5 {
		t.Error("数字型非凭据字段不该被删")
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
