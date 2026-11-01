// 演示站写保护：公开演示环境（config 的 demo.enabled）下，对 /api/v1 的非只读请求
// 一律 403（error.demo_mode），只放行 GET/HEAD/OPTIONS、内建白名单与注册的额外接口。
package middleware

import (
	"net/http"
	"strings"
	"sync"

	"github.com/gin-gonic/gin"

	"nexadmin/core/response"
)

// DemoRegistry 收集演示模式放行的写接口。启动装配期间写入（配置项 + Deps.DemoAllow），
// 每个请求读取一次；热挂载插件也可能在运行期注册，所以用读写锁而不是假定只读。
type DemoRegistry struct {
	mu     sync.RWMutex
	exact  map[string]struct{} // "POST /api/v1/auth/login"
	prefix []string            // 尾部 * 的条目去掉 * 后存前缀，如 "POST /api/v1/pay/"
}

// NewDemoRegistry 用给定条目建注册表；条目格式 "METHOD /path"，方法大写、支持尾部 *。
func NewDemoRegistry(entries ...string) *DemoRegistry {
	r := &DemoRegistry{}
	r.Allow(entries...)
	return r
}

// DemoBuiltinAllow 返回演示模式内建放行的公开接口：登录 / 刷新令牌 / 登出 /
// WebSocket 入场票。改密码、改资料等故意不放行——演示账号的密码因此改不掉，省去重置兜底。
func DemoBuiltinAllow(prefix string) []string {
	return []string{
		"POST " + prefix + "/auth/login",
		"POST " + prefix + "/auth/refresh",
		"POST " + prefix + "/auth/logout",
		"POST " + prefix + "/ws/ticket",
	}
}

// Allow 追加放行条目。格式不对（缺方法、缺路径）或为空的条目忽略而不是报错：
// 配置写错不该挡住演示站启动，条目格式在配置注释里说明。nil 接收者（测试里手工
// 构造的 Deps 未装配注册表）是空操作。
func (r *DemoRegistry) Allow(entries ...string) {
	if r == nil || len(entries) == 0 {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, e := range entries {
		e = strings.TrimSpace(e)
		if e == "" {
			continue
		}
		method, path, ok := strings.Cut(e, " ")
		if !ok {
			continue
		}
		m := strings.ToUpper(strings.TrimSpace(method))
		p := demoNormPath(path)
		if m == "" || p == "" {
			continue
		}
		if strings.HasSuffix(p, "*") {
			r.prefix = append(r.prefix, m+" "+strings.TrimSuffix(p, "*"))
			continue
		}
		if r.exact == nil {
			r.exact = make(map[string]struct{})
		}
		r.exact[m+" "+p] = struct{}{}
	}
}

// Allowed 判断一个写请求是否在放行清单里。nil 接收者视为没有放行。
func (r *DemoRegistry) Allowed(method, path string) bool {
	if r == nil {
		return false
	}
	m := strings.ToUpper(method)
	p := demoNormPath(path)
	key := m + " " + p
	r.mu.RLock()
	defer r.mu.RUnlock()
	if _, ok := r.exact[key]; ok {
		return true
	}
	for _, pfx := range r.prefix {
		if strings.HasPrefix(key, pfx) {
			return true
		}
	}
	return false
}

// demoNormPath 去掉首尾空白与重复的尾部斜杠（根路径 "/" 保留），让
// "POST /api/v1/x/" 与 "POST /api/v1/x" 视为同一条目。
func demoNormPath(p string) string {
	p = strings.TrimSpace(p)
	for len(p) > 1 && strings.HasSuffix(p, "/") {
		p = strings.TrimSuffix(p, "/")
	}
	return p
}

// DemoMode 挂在 /api/v1 分组上：只读方法直接放行，写请求查放行清单，
// 不在清单里就回 403 error.demo_mode 并中止。根路径公开页（官网、下载等）
// 不经过它，公开表单不受影响。
func DemoMode(r *DemoRegistry) gin.HandlerFunc {
	return func(c *gin.Context) {
		switch c.Request.Method {
		case http.MethodGet, http.MethodHead, http.MethodOptions:
			c.Next()
			return
		}
		if r.Allowed(c.Request.Method, c.Request.URL.Path) {
			c.Next()
			return
		}
		response.Fail(c, response.Forbidden("error.demo_mode"))
		c.Abort()
	}
}
