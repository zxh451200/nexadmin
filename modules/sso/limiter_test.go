package sso

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"

	"nexadmin/core/cache"
	"nexadmin/core/config"
	"nexadmin/core/middleware"
)

func postToken(e *gin.Engine, remote, clientID, secret string) *httptest.ResponseRecorder {
	form := url.Values{"grant_type": {"client_credentials"}, "client_id": {clientID}, "client_secret": {secret}}
	req := httptest.NewRequest(http.MethodPost, "/token", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.RemoteAddr = remote
	w := httptest.NewRecorder()
	e.ServeHTTP(w, req)
	return w
}

// 行为保持：同 IP（或 IP+client）失败 10 次后返回 429，其他 IP 不受影响。
func TestTokenFailureLimit(t *testing.T) {
	p, _ := newTestProvider(t)
	ch, err := cache.New(config.Cache{Driver: "memory"})
	if err != nil {
		t.Fatal(err)
	}
	defer ch.Close()
	gin.SetMode(gin.TestMode)
	h := &handler{p: p, log: zap.NewNop(), lim: middleware.NewLimiter(ch, zap.NewNop(), "sso-token", 10, time.Minute)}
	e := gin.New()
	e.POST("/token", h.token)

	for i := 0; i < 10; i++ {
		if w := postToken(e, "10.0.0.1:1234", "nxsso_test", "wrong"); w.Code != http.StatusUnauthorized {
			t.Fatalf("attempt %d: want 401, got %d (%s)", i+1, w.Code, w.Body.String())
		}
	}
	w := postToken(e, "10.0.0.1:1234", "nxsso_test", "wrong")
	if w.Code != http.StatusTooManyRequests {
		t.Fatalf("11th attempt: want 429, got %d", w.Code)
	}
	if w.Header().Get("Retry-After") == "" {
		t.Fatal("429 must carry Retry-After")
	}
	// 被封后连正确密钥也不放行（同 IP）。
	if w := postToken(e, "10.0.0.1:1234", "nxsso_test", testSecret); w.Code != http.StatusTooManyRequests {
		t.Fatalf("blocked IP must stay blocked, got %d", w.Code)
	}
	// 其他 IP 不受影响。
	if w := postToken(e, "10.0.0.2:1234", "nxsso_test", "wrong"); w.Code != http.StatusUnauthorized {
		t.Fatalf("other IP must not be blocked, got %d", w.Code)
	}
}
