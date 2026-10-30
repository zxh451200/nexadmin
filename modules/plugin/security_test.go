package plugin

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"

	"nexadmin/core"
	"nexadmin/core/auth"
	"nexadmin/core/middleware"
	"nexadmin/core/plugin/market"
)

func securityClaims() map[string]*auth.Claims {
	perms := []string{PermList, PermLicense, PermInstall, "*"}
	return map[string]*auth.Claims{
		"tenant-with-all-permissions": {UserID: 9, TenantID: 9, Perms: perms},
		"app-with-all-permissions":    {Principal: auth.PrincipalApp, Perms: perms},
		"app-with-super-flag":         {UserID: 1, Principal: auth.PrincipalApp, Super: true, Perms: perms},
		"super-without-user":          {Super: true, Perms: perms},
	}
}

func securityRequest(e *gin.Engine, cl *auth.Claims, method, path, body string) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	req := httptest.NewRequest(method, "/api/v1"+path, bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	if cl != nil {
		req.Header.Set("X-Test-Identity", "yes")
	}
	e.ServeHTTP(w, req)
	return w
}

func securityAPI(cl *auth.Claims) (*gin.Engine, *gin.RouterGroup) {
	gin.SetMode(gin.TestMode)
	e := gin.New()
	api := e.Group("/api/v1", func(c *gin.Context) {
		if cl != nil && c.GetHeader("X-Test-Identity") != "" {
			auth.SetClaims(c, cl)
		}
		c.Next()
	})
	return e, api
}

func TestLicensePlatformIdentityBothRouteModes(t *testing.T) {
	m := mount(t)
	license := signLicense(t, otherID, time.Now().Add(time.Hour))
	payload, _ := json.Marshal(map[string]string{"content": license})
	file := filepath.Join(m.Root(), "data", "licenses", otherID+".lic")
	for _, dormant := range []bool{false, true} {
		t.Run(map[bool]string{false: "via-app", true: "dormant"}[dormant], func(t *testing.T) {
			before, _ := os.ReadFile(file)
			beforeInfo := m.List(context.Background())
			for name, cl := range securityClaims() {
				t.Run(name, func(t *testing.T) {
					e, public := securityAPI(cl)
					var via, idle []string
					if dormant {
						idle = []string{otherID}
					} else {
						via = []string{otherID}
					}
					registerLicenseRoutes(public.Group("", middleware.RequireLogin()), public, m, via, idle)
					w := securityRequest(e, cl, "POST", "/plugins/"+otherID+"/license", string(payload))
					if w.Code != 403 {
						t.Fatalf("want 403, got %d %s", w.Code, w.Body.String())
					}
				})
			}
			after, _ := os.ReadFile(file)
			if !bytes.Equal(before, after) {
				t.Fatal("forbidden import changed the license file")
			}
			a, _ := json.Marshal(beforeInfo)
			b, _ := json.Marshal(m.List(context.Background()))
			if !bytes.Equal(a, b) {
				t.Fatal("forbidden import changed plugin state")
			}
			cl := &auth.Claims{UserID: 1, Super: true}
			e, public := securityAPI(cl)
			var via, idle []string
			if dormant {
				idle = []string{otherID}
			} else {
				via = []string{otherID}
			}
			registerLicenseRoutes(public.Group("", middleware.RequireLogin()), public, m, via, idle)
			w := securityRequest(e, cl, "POST", "/plugins/"+otherID+"/license", string(payload))
			if w.Code != 200 {
				t.Fatalf("platform import: %d %s", w.Code, w.Body.String())
			}
			saved, err := os.ReadFile(file)
			if err != nil || string(saved) != strings.TrimSpace(license)+"\n" {
				t.Fatalf("platform import was not saved: %v", err)
			}
		})
	}
}

func TestMarketPlatformIdentityHasNoSideEffects(t *testing.T) {
	m := mount(t)
	config := filepath.Join(m.Root(), "config")
	if err := os.MkdirAll(config, 0o755); err != nil {
		t.Fatal(err)
	}
	catalog := filepath.Join(config, "plugin-market.yaml")
	if err := os.WriteFile(catalog, []byte("plugins:\n  - id: demo-other\n    edition: pro\n    repo: test/never-downloaded\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Remove(catalog) })
	jobID := marketRunner.Run("test", "", func(*market.Ctx) error { return nil })
	cases := []struct{ method, path, body string }{
		{"GET", "/plugins/market", ""},
		{"GET", "/plugins/market/jobs/" + jobID, ""},
		{"POST", "/plugins/market/install", `{"id":"demo-other","force":true}`},
		{"POST", "/plugins/market/remove", `{"id":"demo-other","purge":true}`},
		{"POST", "/plugins/market/rebuild", `{}`},
	}
	identities := securityClaims()
	identities["anonymous"] = nil
	identities["platform"] = &auth.Claims{UserID: 1, Super: true}
	for name, cl := range identities {
		t.Run(name, func(t *testing.T) {
			calls := 0
			actions := marketActions{
				install: func(string, market.Entry, bool) string { calls++; return "test-job" },
				remove:  func(string, string, bool) string { calls++; return "test-job" },
				rebuild: func(string, bool) string { calls++; return "test-job" },
			}
			e, public := securityAPI(cl)
			registerMarketWith(public.Group("", middleware.RequireLogin()), m, actions)
			want := 403
			if cl == nil {
				want = 401
			}
			if name == "platform" {
				want = 200
			}
			for _, tc := range cases {
				w := securityRequest(e, cl, tc.method, tc.path, tc.body)
				if w.Code != want {
					t.Fatalf("%s %s: want %d got %d %s", tc.method, tc.path, want, w.Code, w.Body.String())
				}
			}
			if name == "platform" {
				if calls != 3 {
					t.Fatalf("platform actions=%d, want 3", calls)
				}
			} else if calls != 0 {
				t.Fatalf("forbidden identity started %d market actions", calls)
			}
		})
	}
}

func TestActiveIDsRemainLoginOnly(t *testing.T) {
	if w := api(t, nil, "GET", "/plugins/active", ""); w.Code != 401 {
		t.Fatalf("anonymous active IDs: %d", w.Code)
	}
	for _, cl := range []*auth.Claims{{UserID: 8, TenantID: 9}, {Principal: auth.PrincipalApp}} {
		w := api(t, cl, "GET", "/plugins/active", "")
		if w.Code != 200 {
			t.Fatalf("ordinary identity active IDs: %d %s", w.Code, w.Body.String())
		}
		var out struct {
			Data []string `json:"data"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
			t.Fatal("active IDs must not include license metadata", err)
		}
	}
}

type httpPlugin struct{}

func (httpPlugin) Name() string             { return hotID }
func (httpPlugin) RequiresRootRoutes() bool { return false }
func (httpPlugin) Register(r *gin.RouterGroup, d *core.Deps) {
	d.Public.GET("/plugins/"+hotID+"/public", func(c *gin.Context) { c.String(200, "public") })
	r.GET("/plugins/"+hotID+"/private", func(c *gin.Context) { c.String(200, "private") })
}

func TestDormantForwarderPreservesPublicAndBusinessAccess(t *testing.T) {
	mount(t)
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
	cl := &auth.Claims{UserID: 1, Super: true}
	e, public := securityAPI(cl)
	module{}.Register(public.Group("", middleware.RequireLogin()), &core.Deps{Public: public, DB: db, Log: zap.NewNop()})
	payload, _ := json.Marshal(map[string]string{"content": signLicense(t, hotID, time.Now().Add(time.Hour))})
	w := securityRequest(e, cl, "POST", "/plugins/"+hotID+"/license", string(payload))
	if w.Code != 200 {
		t.Fatalf("import: %d %s", w.Code, w.Body.String())
	}
	var result struct {
		Data struct {
			NeedRestart bool `json:"needRestart"`
		} `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil || result.Data.NeedRestart {
		t.Fatalf("hot activation failed: %s %v", w.Body.String(), err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
	if w := securityRequest(e, nil, "GET", "/plugins/"+hotID+"/public", ""); w.Code != 200 || w.Body.String() != "public" {
		t.Fatalf("anonymous public: %d %s", w.Code, w.Body.String())
	}
	if w := securityRequest(e, nil, "GET", "/plugins/"+hotID+"/private", ""); w.Code != 401 {
		t.Fatalf("anonymous private: %d", w.Code)
	}
	cl.Super = false
	if w := securityRequest(e, cl, "GET", "/plugins/"+hotID+"/private", ""); w.Code != 200 || w.Body.String() != "private" {
		t.Fatalf("ordinary business request: %d %s", w.Code, w.Body.String())
	}
}
