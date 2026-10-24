package core

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"nexadmin/core/cache"
	"nexadmin/core/config"
)

const secretErr = "dial tcp 10.1.2.3:6379: AUTH failed password=hunter2 version=7.2.4"

func probeOf(name string, fn func(context.Context) error) *healthProbe {
	return &healthProbe{name: name, fn: fn}
}

func healthGet(e *gin.Engine) (int, map[string]any, string) {
	w := httptest.NewRecorder()
	e.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/healthz", nil))
	var m map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &m)
	return w.Code, m, w.Body.String()
}

func healthEngine(probes ...*healthProbe) *gin.Engine {
	gin.SetMode(gin.TestMode)
	e := gin.New()
	registerHealth(e, probes, nil)
	return e
}

func TestHealthzNoProbesIsAlive(t *testing.T) {
	code, m, _ := healthGet(healthEngine())
	if code != 200 || m["data"].(map[string]any)["status"] != "ok" {
		t.Fatalf("got %d %v", code, m)
	}
}

func TestHealthzAllOK(t *testing.T) {
	ok := func(context.Context) error { return nil }
	code, m, _ := healthGet(healthEngine(probeOf("db", ok), probeOf("cache", ok), probeOf("storage", ok)))
	checks := m["data"].(map[string]any)["checks"].(map[string]any)
	if code != 200 || len(checks) != 3 || checks["db"] != "ok" || checks["cache"] != "ok" || checks["storage"] != "ok" {
		t.Fatalf("got %d %v", code, m)
	}
}

// 任一组件失败都返回 503，只给组件名与 ok/fail，不泄露错误文本里的地址、凭据、版本。
func TestHealthzEachComponentFailureIs503AndNoLeak(t *testing.T) {
	ok := func(context.Context) error { return nil }
	bad := func(context.Context) error { return errors.New(secretErr) }
	for _, failing := range []string{"db", "cache", "storage"} {
		var probes []*healthProbe
		for _, n := range []string{"db", "cache", "storage"} {
			fn := ok
			if n == failing {
				fn = bad
			}
			probes = append(probes, probeOf(n, fn))
		}
		code, m, raw := healthGet(healthEngine(probes...))
		if code != 503 {
			t.Fatalf("%s: code = %d, want 503", failing, code)
		}
		checks := m["data"].(map[string]any)["checks"].(map[string]any)
		for n, v := range checks {
			want := "ok"
			if n == failing {
				want = "fail"
			}
			if v != want {
				t.Fatalf("%s: checks[%s] = %v, want %s", failing, n, v, want)
			}
		}
		for _, leak := range []string{"10.1.2.3", "hunter2", "7.2.4", "AUTH", "dial tcp"} {
			if strings.Contains(raw, leak) {
				t.Fatalf("%s: response leaks %q: %s", failing, leak, raw)
			}
		}
	}
}

func TestHealthzTotalTimeoutAndCache(t *testing.T) {
	var calls atomic.Int32
	release := make(chan struct{})
	defer close(release)
	slow := probeOf("storage", func(ctx context.Context) error {
		calls.Add(1)
		<-release // 故意不尊重 ctx，模拟卡死的依赖
		return nil
	})
	fast := probeOf("db", func(context.Context) error { return nil })
	hc := newHealthChecker([]*healthProbe{fast, slow}, nil)
	hc.timeout = 80 * time.Millisecond
	hc.ttl = time.Minute

	start := time.Now()
	res, healthy := hc.check()
	if time.Since(start) > time.Second || healthy || res["storage"] != "fail" || res["db"] != "ok" {
		t.Fatalf("timeout result: %v healthy=%v took=%v", res, healthy, time.Since(start))
	}
	// 缓存期内不会重新探测
	hc.check()
	hc.check()
	if calls.Load() != 1 {
		t.Fatalf("probe ran %d times within cache ttl, want 1", calls.Load())
	}
	// 缓存过期后，上一次仍卡着的探测不会再叠加新的 goroutine
	hc.ttl = 0
	if res, _ := hc.check(); res["storage"] != "fail" {
		t.Fatalf("stuck probe must stay fail: %v", res)
	}
	if calls.Load() != 1 {
		t.Fatalf("stuck probe was re-launched: %d calls", calls.Load())
	}
}

func TestHealthzProbePanicIsFail(t *testing.T) {
	code, _, _ := healthGet(healthEngine(probeOf("cache", func(context.Context) error { panic("boom") })))
	if code != 503 {
		t.Fatalf("code = %d, want 503", code)
	}
}

func TestRealCacheAndStorageProbes(t *testing.T) {
	ctx := context.Background()
	c, err := cache.New(config.Cache{Driver: "memory"})
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	if err := cacheHealthProbe(c).fn(ctx); err != nil {
		t.Fatalf("cache probe: %v", err)
	}
	cfg := &config.Config{DataDir: t.TempDir()}
	if err := storageHealthProbe(cfg).fn(ctx); err != nil {
		t.Fatalf("storage probe: %v", err)
	}
	// 数据目录是个普通文件：本地存储不可写，探针应失败。
	f := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(f, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := storageHealthProbe(&config.Config{DataDir: f}).fn(ctx); err == nil {
		t.Fatal("storage probe should fail when the data dir is not a directory")
	}
}
