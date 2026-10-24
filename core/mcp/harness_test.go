package mcp_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"

	"nexadmin/core"
	"nexadmin/core/apidoc"
	"nexadmin/core/auth"
	"nexadmin/core/cache"
	"nexadmin/core/config"
	"nexadmin/core/mcp"
	"nexadmin/core/middleware"
	"nexadmin/core/tenant"
	"nexadmin/pkg/idgen"
)

// 测试用户：alice(5, 租户 9) 拥有 mcp:list；bob(6, 租户 9)；carol(7, 租户 10)；nobody(8) 没有 mcp:list。
const (
	alice  idgen.ID = 5
	bob    idgen.ID = 6
	carol  idgen.ID = 7
	nobody idgen.ID = 8
)

type provider struct{}

func (provider) Login(context.Context, auth.LoginRequest) (*auth.User, error) { return nil, nil }
func (provider) Profile(_ context.Context, id idgen.ID) (*auth.User, error) {
	perms := []string{"mcp:list", "message:list", "message:read"}
	if id == nobody {
		perms = []string{"message:list"}
	}
	return &auth.User{ID: id, Perms: perms}, nil
}
func (provider) VerifyPassword(_ context.Context, id idgen.ID, pw string) error {
	if pw != "pw-"+id.String() {
		return errors.New("bad password")
	}
	return nil
}

type marked struct {
	User idgen.ID
	ID   string
}

type env struct {
	t     *testing.T
	e     *gin.Engine
	srv   *mcp.Server
	cache cache.Cache
	logs  *observer.ObservedLogs
	mu    sync.Mutex
	done  []marked
}

func (v *env) executed() []marked {
	v.mu.Lock()
	defer v.mu.Unlock()
	return append([]marked(nil), v.done...)
}

// claimsFor 把测试令牌翻译成 Claims，模拟 ParseToken + Tenant 中间件。
func claimsFor(token string) (*auth.Claims, idgen.ID) {
	reg := func(id string) jwt.RegisteredClaims {
		return jwt.RegisteredClaims{ID: id, ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour))}
	}
	switch token {
	case "alice":
		return &auth.Claims{UserID: alice, TenantID: 9, RegisteredClaims: reg("jti-alice")}, 9
	case "alice2": // 同一用户的另一份凭据（例如刷新后的令牌）
		return &auth.Claims{UserID: alice, TenantID: 9, RegisteredClaims: reg("jti-alice-2")}, 9
	case "bob":
		return &auth.Claims{UserID: bob, TenantID: 9, RegisteredClaims: reg("jti-bob")}, 9
	case "carol":
		return &auth.Claims{UserID: carol, TenantID: 10, RegisteredClaims: reg("jti-carol")}, 10
	case "nobody":
		return &auth.Claims{UserID: nobody, TenantID: 9, RegisteredClaims: reg("jti-nobody")}, 9
	case "pat":
		return &auth.Claims{UserID: alice, TenantID: 9, Principal: auth.PrincipalPAT, RegisteredClaims: reg("jti-pat")}, 9
	case "app":
		return &auth.Claims{UserID: 0, TenantID: 9, Principal: auth.PrincipalApp, Perms: []string{"mcp:list"}, RegisteredClaims: reg("jti-app")}, 9
	}
	return nil, 0
}

func newEnv(t *testing.T) *env {
	t.Helper()
	gin.SetMode(gin.TestMode)
	ch, err := cache.New(config.Cache{Driver: "memory"})
	if err != nil {
		t.Fatal(err)
	}
	svc, err := auth.NewService(&config.Config{Auth: config.Auth{JWTSecret: strings.Repeat("k", 32)}}, ch)
	if err != nil {
		t.Fatal(err)
	}
	svc.SetProvider(provider{})
	prev := auth.Default()
	auth.SetDefault(svc)
	t.Cleanup(func() { auth.SetDefault(prev) })

	obs, logs := observer.New(zap.InfoLevel)
	v := &env{t: t, cache: ch, logs: logs}
	e := gin.New()
	api := e.Group("/api/v1")
	api.Use(func(c *gin.Context) {
		token := strings.TrimPrefix(c.GetHeader("Authorization"), "Bearer ")
		if cl, tid := claimsFor(token); cl != nil {
			auth.SetClaims(c, cl)
			c.Request = c.Request.WithContext(tenant.With(c.Request.Context(), tid))
		}
		c.Next()
	}, mcp.EnforceDispatchIdentity())
	doc := apidoc.New(e, apidoc.Options{})
	protected := api.Group("", middleware.RequireLogin())
	protected.GET("/messages/unread-count", coreperm("message:list"), func(c *gin.Context) {
		cl := auth.ClaimsFrom(c)
		tid, _ := tenant.From(c.Request.Context())
		c.JSON(200, gin.H{"user": cl.UserID.String(), "tenant": tid.String()})
	})
	protected.PUT("/messages/:id/read", coreperm("message:read"), func(c *gin.Context) {
		v.mu.Lock()
		v.done = append(v.done, marked{auth.ClaimsFrom(c).UserID, c.Param("id")})
		v.mu.Unlock()
		c.JSON(200, gin.H{"ok": true})
	})
	doc.RegisterWriteTool("message_mark_read", "PUT", "/api/v1/messages/:id/read", "mark read",
		func(_ context.Context, uid idgen.ID, path map[string]string) error {
			switch path["id"] {
			case "boom":
				return errors.New("pq: relation SECRET_TABLE_DUMP does not exist")
			case "1001":
				if uid == alice {
					return nil
				}
			case "1002":
				if uid == bob {
					return nil
				}
			}
			return errors.New("not found")
		})
	v.srv = mcp.New(e, doc).WithConfirmation(ch, svc, zap.New(obs)).WithConfirmLimit(3, time.Minute)
	t.Cleanup(v.srv.Close)
	api.POST("/mcp", coreperm("mcp:list"), v.srv.Handle)
	api.GET("/mcp", coreperm("mcp:list"), v.srv.Handle)
	api.DELETE("/mcp", coreperm("mcp:list"), v.srv.Handle)
	api.GET("/mcp/confirmations/:id", coreperm("mcp:list"), v.srv.Confirmation)
	api.POST("/mcp/confirmations/:id/approve", coreperm("mcp:list"), v.srv.Approve)
	v.e = e
	return v
}

func coreperm(code string) gin.HandlerFunc { return core.Perm(code) }

type reply struct {
	Code   int
	Header http.Header
	Body   string
}

func (r reply) json() map[string]any {
	var m map[string]any
	_ = json.Unmarshal([]byte(r.Body), &m)
	return m
}

// do 发请求；headers 为 "Name: value" 形式。
func (v *env) do(method, path, body string, headers ...string) reply {
	r := httptest.NewRequest(method, path, strings.NewReader(body))
	r.Host = "example.com"
	for _, h := range headers {
		k, val, _ := strings.Cut(h, ": ")
		r.Header.Set(k, val)
	}
	if method == "POST" && r.Header.Get("Content-Type") == "" {
		r.Header.Set("Content-Type", "application/json")
	}
	w := httptest.NewRecorder()
	v.e.ServeHTTP(w, r)
	return reply{w.Code, w.Header(), w.Body.String()}
}

func bearer(tok string) string { return "Authorization: Bearer " + tok }

func rpc(id int, method, params string) string {
	if params == "" {
		params = "{}"
	}
	return `{"jsonrpc":"2.0","id":` + string(rune('0'+id)) + `,"method":"` + method + `","params":` + params + `}`
}

const initParams = `{"protocolVersion":"2025-03-26","capabilities":{},"clientInfo":{"name":"t","version":"1"}}`

// toolResult 取 tools/call 结果的 text 与 isError。
func toolResult(t *testing.T, r reply) (string, bool) {
	t.Helper()
	if r.Code != 200 {
		t.Fatalf("status %d body %s", r.Code, r.Body)
	}
	res, _ := r.json()["result"].(map[string]any)
	if res == nil {
		t.Fatalf("no result: %s", r.Body)
	}
	content, _ := res["content"].([]any)
	text, _ := content[0].(map[string]any)["text"].(string)
	isErr, _ := res["isError"].(bool)
	return text, isErr
}
