package core

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"

	"nexadmin/core/config"
	"nexadmin/core/metrics"
)

func metricsStatus(a *App, remote, auth string) int {
	e := gin.New()
	_ = e.SetTrustedProxies(nil)
	e.Use(metrics.Middleware())
	a.registerMetricsRoute(e)
	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	req.RemoteAddr = remote
	if auth != "" {
		req.Header.Set("Authorization", auth)
	}
	w := httptest.NewRecorder()
	e.ServeHTTP(w, req)
	return w.Code
}

func TestMetricsRouteRegistrationAndProtection(t *testing.T) {
	gin.SetMode(gin.TestMode)
	cfg := &config.Config{}
	cfg.Metrics.Enabled = true
	a := &App{cfg: cfg}
	if c := metricsStatus(a, "127.0.0.1:1", ""); c != http.StatusOK {
		t.Errorf("loopback = %d", c)
	}
	if c := metricsStatus(a, "10.0.0.5:1", ""); c != http.StatusForbidden {
		t.Errorf("remote = %d", c)
	}
	cfg.Metrics.Token = "tok"
	if c := metricsStatus(a, "10.0.0.5:1", "Bearer tok"); c != http.StatusOK {
		t.Errorf("remote with token = %d", c)
	}
	if c := metricsStatus(a, "10.0.0.5:1", "Bearer bad"); c != http.StatusUnauthorized {
		t.Errorf("remote bad token = %d", c)
	}
	cfg.Metrics.Enabled = false
	if c := metricsStatus(a, "127.0.0.1:1", ""); c != http.StatusNotFound {
		t.Errorf("disabled = %d, want 404", c)
	}
}
