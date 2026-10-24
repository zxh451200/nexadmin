package core

import (
	"context"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"

	"nexadmin/core/response"
)

// /healthz 不鉴权，所以返回体只含组件名与 ok / fail：不带错误文本、地址、凭据或版本信息。
// 失败原因只写服务端日志；详细诊断走需要权限的 GET /monitor/health。
const (
	healthTotalTimeout = 2 * time.Second // 一次探测的总超时
	healthCacheTTL     = 2 * time.Second // 结果缓存：高频探针 / 恶意刷接口时不会反复打依赖
	healthStatusOK     = "ok"
	healthStatusFail   = "fail"
)

// healthProbe 是一项依赖探测。fn 应尊重 ctx 的取消，但即使不尊重，总超时到了也会按 fail 返回。
type healthProbe struct {
	name string
	fn   func(ctx context.Context) error

	running atomic.Bool // 上一次探测还没返回时不再叠加新的 goroutine，直接判 fail
}

// healthChecker 并发探测各依赖并缓存结果。
type healthChecker struct {
	probes  []*healthProbe
	log     *zap.Logger
	timeout time.Duration
	ttl     time.Duration
	now     func() time.Time

	mu     sync.Mutex
	at     time.Time
	cached map[string]string
}

func newHealthChecker(probes []*healthProbe, log *zap.Logger) *healthChecker {
	if log == nil {
		log = zap.NewNop()
	}
	return &healthChecker{probes: probes, log: log, timeout: healthTotalTimeout, ttl: healthCacheTTL, now: time.Now}
}

// check 返回各组件状态与整体是否健康。持锁探测：并发请求等同一次探测的结果，而不是各自再探一次。
func (h *healthChecker) check() (map[string]string, bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.cached == nil || h.now().Sub(h.at) >= h.ttl {
		h.cached = h.probe()
		h.at = h.now()
	}
	out := make(map[string]string, len(h.cached))
	healthy := true
	for k, v := range h.cached {
		out[k] = v
		if v != healthStatusOK {
			healthy = false
		}
	}
	return out, healthy
}

func (h *healthChecker) probe() map[string]string {
	ctx, cancel := context.WithTimeout(context.Background(), h.timeout)
	defer cancel()
	type result struct {
		name string
		err  error
	}
	ch := make(chan result, len(h.probes))
	for _, p := range h.probes {
		p := p
		if !p.running.CompareAndSwap(false, true) {
			ch <- result{p.name, context.DeadlineExceeded} // 上一次探测仍卡着
			continue
		}
		go func() {
			defer p.running.Store(false)
			defer func() {
				if r := recover(); r != nil {
					ch <- result{p.name, context.Canceled}
				}
			}()
			ch <- result{p.name, p.fn(ctx)}
		}()
	}
	out := make(map[string]string, len(h.probes))
	for _, p := range h.probes {
		out[p.name] = healthStatusFail // 默认 fail：总超时到了还没回来的组件按失败算
	}
	for range h.probes {
		select {
		case r := <-ch:
			if r.err == nil {
				out[r.name] = healthStatusOK
			} else {
				h.log.Warn("healthz probe failed", zap.String("component", r.name), zap.Error(r.err))
			}
		case <-ctx.Done():
			h.log.Warn("healthz probe timed out", zap.Duration("timeout", h.timeout))
			return out
		}
	}
	return out
}

// registerHealth 注册 /healthz 与 /readyz（不在 /api/v1 下，供负载均衡和容器探针使用）。
// probes 为空（未初始化模式）时只报告进程存活。任一探测失败返回 503。
func registerHealth(e *gin.Engine, probes []*healthProbe, log *zap.Logger) {
	hc := newHealthChecker(probes, log)
	handler := func(c *gin.Context) {
		if len(hc.probes) == 0 {
			response.OK(c, gin.H{"status": healthStatusOK})
			return
		}
		checks, healthy := hc.check()
		c.Header("Cache-Control", "no-store")
		if healthy {
			response.OK(c, gin.H{"status": healthStatusOK, "checks": checks})
			return
		}
		c.AbortWithStatusJSON(http.StatusServiceUnavailable, response.Body{
			Code:    http.StatusServiceUnavailable,
			Message: "unavailable",
			Data:    gin.H{"status": healthStatusFail, "checks": checks},
		})
	}
	e.GET("/healthz", handler)
	e.GET("/readyz", handler)
}
