package saml

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestExchangeIsOneTime(t *testing.T) {
	gin.SetMode(gin.TestMode)
	svc, _, ch := sloService(t)
	raw := `{"payload":{"accessToken":"tok-e2e","refreshToken":"ref-e2e","user":{"username":"bob"}},"sourceId":"123"}`
	if err := ch.Set(context.Background(), "saml:code:abc123", raw, 0); err != nil {
		t.Fatal(err)
	}
	r := gin.New()
	r.POST("/api/v1/auth/saml/exchange", svc.exchange)
	do := func() *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest("POST", "/api/v1/auth/saml/exchange", strings.NewReader(`{"code":"abc123"}`)))
		return w
	}
	w := do()
	if w.Code != http.StatusOK {
		t.Fatalf("exchange: %d %s", w.Code, w.Body.String())
	}
	var out struct {
		Data map[string]any `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if out.Data["accessToken"] != "tok-e2e" || out.Data["samlSourceId"] != "123" {
		t.Fatalf("payload: %+v", out.Data)
	}
	if w2 := do(); w2.Code != http.StatusBadRequest {
		t.Fatalf("code must be single-use, second exchange: %d", w2.Code)
	}
}

func TestSafeRedirectWhitelist(t *testing.T) {
	for in, want := range map[string]string{
		"/login":            "/login",
		"/admin/login?x=1":  "/admin/login?x=1",
		"//evil.example":    "",
		"https://evil.test": "",
		"":                  "",
		"login":             "",
	} {
		if got := safeRedirect(in); got != want {
			t.Errorf("safeRedirect(%q) = %q, want %q", in, got, want)
		}
	}
}
