package middleware

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"

	"nexadmin/core/cache"
	"nexadmin/core/config"
)

func memCache(t *testing.T) cache.Cache {
	t.Helper()
	c, err := cache.New(config.Cache{Driver: "memory"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = c.Close() })
	return c
}

func do(e *gin.Engine, method, path, remote string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, nil)
	req.RemoteAddr = remote
	w := httptest.NewRecorder()
	e.ServeHTTP(w, req)
	return w
}

func TestRateLimitByIPAndPath(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c := memCache(t)
	e := gin.New()
	e.GET("/a", RateLimit(c, zap.NewNop(), "t", ByIPAndPath(), 3, time.Minute), func(c *gin.Context) { c.String(200, "ok") })
	e.GET("/b", RateLimit(c, zap.NewNop(), "t", ByIPAndPath(), 3, time.Minute), func(c *gin.Context) { c.String(200, "ok") })

	for i := 0; i < 3; i++ {
		if w := do(e, "GET", "/a", "1.1.1.1:1"); w.Code != 200 {
			t.Fatalf("request %d: want 200, got %d", i+1, w.Code)
		}
	}
	w := do(e, "GET", "/a", "1.1.1.1:1")
	if w.Code != http.StatusTooManyRequests {
		t.Fatalf("want 429, got %d", w.Code)
	}
	if n, err := strconv.Atoi(w.Header().Get("Retry-After")); err != nil || n < 1 || n > 60 {
		t.Fatalf("bad Retry-After %q", w.Header().Get("Retry-After"))
	}
	if w := do(e, "GET", "/b", "1.1.1.1:1"); w.Code != 200 {
		t.Fatalf("other path must count separately, got %d", w.Code)
	}
	if w := do(e, "GET", "/a", "2.2.2.2:1"); w.Code != 200 {
		t.Fatalf("other IP must count separately, got %d", w.Code)
	}
}

func TestRateLimitWindowResets(t *testing.T) {
	gin.SetMode(gin.TestMode)
	e := gin.New()
	e.GET("/", RateLimit(memCache(t), zap.NewNop(), "t", ByIP(), 1, 80*time.Millisecond), func(c *gin.Context) { c.String(200, "ok") })
	if do(e, "GET", "/", "1.1.1.1:1").Code != 200 || do(e, "GET", "/", "1.1.1.1:1").Code != 429 {
		t.Fatal("second request in window must be limited")
	}
	time.Sleep(120 * time.Millisecond)
	if w := do(e, "GET", "/", "1.1.1.1:1"); w.Code != 200 {
		t.Fatalf("window should have reset, got %d", w.Code)
	}
}

func TestRateLimitDisabledAndEmptyKey(t *testing.T) {
	gin.SetMode(gin.TestMode)
	e := gin.New()
	e.GET("/off", RateLimit(memCache(t), nil, "t", ByIP(), 0, time.Minute), func(c *gin.Context) { c.String(200, "ok") })
	e.GET("/nokey", RateLimit(memCache(t), nil, "t", func(*gin.Context) string { return "" }, 1, time.Minute), func(c *gin.Context) { c.String(200, "ok") })
	for i := 0; i < 5; i++ {
		if do(e, "GET", "/off", "1.1.1.1:1").Code != 200 || do(e, "GET", "/nokey", "1.1.1.1:1").Code != 200 {
			t.Fatal("limit<=0 or empty key must pass through")
		}
	}
}

func TestRateLimitRuleHonoursConfig(t *testing.T) {
	gin.SetMode(gin.TestMode)
	cfg := config.RateLimit{Enabled: true, Login: config.Rule{Limit: 1, Window: time.Minute}}
	e := gin.New()
	e.GET("/", RateLimitRule(memCache(t), nil, cfg, "login", ByIP()), func(c *gin.Context) { c.String(200, "ok") })
	if do(e, "GET", "/", "1.1.1.1:1").Code != 200 || do(e, "GET", "/", "1.1.1.1:1").Code != 429 {
		t.Fatal("rule from config not applied")
	}
	cfg.Enabled = false
	e2 := gin.New()
	e2.GET("/", RateLimitRule(memCache(t), nil, cfg, "login", ByIP()), func(c *gin.Context) { c.String(200, "ok") })
	for i := 0; i < 3; i++ {
		if do(e2, "GET", "/", "1.1.1.1:1").Code != 200 {
			t.Fatal("enabled=false must not limit")
		}
	}
}

// brokenCache 的每个操作都失败，模拟缓存故障。
type brokenCache struct{}

var errBroken = errors.New("cache down")

func (brokenCache) Get(context.Context, string) (string, error)              { return "", errBroken }
func (brokenCache) Set(context.Context, string, string, time.Duration) error { return errBroken }
func (brokenCache) Del(context.Context, ...string) error                     { return errBroken }
func (brokenCache) GetDel(context.Context, string) (string, error)           { return "", errBroken }
func (brokenCache) Exists(context.Context, string) (bool, error)             { return false, errBroken }
func (brokenCache) Close() error                                             { return nil }

func TestRateLimitFailsOpenWhenCacheDown(t *testing.T) {
	gin.SetMode(gin.TestMode)
	e := gin.New()
	e.GET("/", RateLimit(brokenCache{}, zap.NewNop(), "t", ByIP(), 1, time.Minute), func(c *gin.Context) { c.String(200, "ok") })
	for i := 0; i < 3; i++ {
		if w := do(e, "GET", "/", "1.1.1.1:1"); w.Code != 200 {
			t.Fatalf("cache failure must fail open, got %d", w.Code)
		}
	}
	lim := NewLimiter(brokenCache{}, zap.NewNop(), "t", 1, time.Minute)
	lim.Fail(context.Background(), "k")
	if lim.Blocked(context.Background(), "k") {
		t.Fatal("Blocked must be false when cache is down")
	}
}

func TestLimiterBlockedAfterFails(t *testing.T) {
	lim := NewLimiter(memCache(t), zap.NewNop(), "f", 3, time.Minute)
	ctx := context.Background()
	for i := 0; i < 2; i++ {
		lim.Fail(ctx, "k")
	}
	if lim.Blocked(ctx, "k") {
		t.Fatal("2 failures must not block at max=3")
	}
	lim.Fail(ctx, "k")
	if !lim.Blocked(ctx, "k") || lim.Blocked(ctx, "other") {
		t.Fatal("3rd failure must block only that key")
	}
}
