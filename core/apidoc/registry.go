// Package apidoc 由运行时路由清单生成 OpenAPI 3.0.3 文档，没有注解体系。
//
// 路由 → 权限码的对应关系：core.Perm 在创建中间件时调用 RecordPerm，登记“这个中间件实例 → 权限码”；
// 生成清单时读取 gin 路由树里每条路由的处理链，按实例找回权限码（读 gin 未导出字段，
// 读取失败时只是缺 perm，不影响其余信息，单测会在 gin 升级后第一时间发现）。
package apidoc

import (
	"reflect"
	"runtime"
	"strings"
	"sync"
	"unsafe"

	"github.com/gin-gonic/gin"
)

var (
	permMu   sync.Mutex
	permByFn = map[unsafe.Pointer]string{} // 中间件闭包实例 → 权限码
	descByFn = map[unsafe.Pointer]string{} // 中间件闭包实例 → 路由摘要
	permKeep []gin.HandlerFunc             // 持有引用，避免闭包被回收后地址复用
	descKeep []gin.HandlerFunc
)

// RecordPerm 登记 core.Perm 创建的中间件实例与权限码。
func RecordPerm(code string, h gin.HandlerFunc) {
	permMu.Lock()
	defer permMu.Unlock()
	permByFn[funcval(&h)] = code
	permKeep = append(permKeep, h)
}

// RecordDesc 登记路由描述中间件实例与摘要。
func RecordDesc(summary string, h gin.HandlerFunc) {
	permMu.Lock()
	defer permMu.Unlock()
	descByFn[funcval(&h)] = summary
	descKeep = append(descKeep, h)
}

func permOf(fv unsafe.Pointer) (string, bool) {
	permMu.Lock()
	defer permMu.Unlock()
	c, ok := permByFn[fv]
	return c, ok
}

func descOf(fv unsafe.Pointer) (string, bool) {
	permMu.Lock()
	defer permMu.Unlock()
	s, ok := descByFn[fv]
	return s, ok
}

// funcval 返回函数值指向的闭包对象地址（同一次 Perm 调用得到同一个值）。
func funcval(slot *gin.HandlerFunc) unsafe.Pointer { return *(*unsafe.Pointer)(unsafe.Pointer(slot)) }

// funcName 返回闭包代码对应的函数名，如 nexadmin/core/middleware.RequireLogin.func1；
// 被内联进调用方时形如 pkg.Caller.RequireLogin.func2，所以匹配时只看 "RequireLogin.func" 片段。
func funcName(fv unsafe.Pointer) string {
	if fv == nil {
		return ""
	}
	code := *(*uintptr)(fv)
	if f := runtime.FuncForPC(code); f != nil {
		return f.Name()
	}
	return ""
}

// chainInfo 是一条路由的处理链摘要。
type chainInfo struct {
	perms     []string
	login     bool // 链上有 RequireLogin / RequireSuper / Perm
	superOnly bool
	summary   string
}

func summarize(chain []unsafe.Pointer) chainInfo {
	var ci chainInfo
	for _, fv := range chain {
		if summary, ok := descOf(fv); ok && summary != "" {
			ci.summary = summary
		}
		if code, ok := permOf(fv); ok {
			ci.login = true
			ci.perms = append(ci.perms, code)
			continue
		}
		n := funcName(fv)
		switch {
		case strings.Contains(n, "RequireSuper.func"):
			ci.login, ci.superOnly = true, true
		case strings.Contains(n, "RequireLogin.func"):
			ci.login = true
		}
	}
	return ci
}

// readChains 通过反射读取 gin 路由树，返回 "METHOD path" → 处理链上各中间件的闭包地址。
// gin 内部结构变化时返回 false，调用方降级为只用 Routes()。
func readChains(e *gin.Engine) (out map[string][]unsafe.Pointer, ok bool) {
	defer func() {
		if recover() != nil {
			out, ok = nil, false
		}
	}()
	trees := reflect.ValueOf(e).Elem().FieldByName("trees")
	if !trees.IsValid() || trees.Kind() != reflect.Slice {
		return nil, false
	}
	out = map[string][]unsafe.Pointer{}
	for i := 0; i < trees.Len(); i++ {
		t := trees.Index(i)
		method := t.FieldByName("method").String()
		walk(t.FieldByName("root"), method, out)
	}
	return out, true
}

func walk(n reflect.Value, method string, out map[string][]unsafe.Pointer) {
	if n.Kind() == reflect.Pointer {
		if n.IsNil() {
			return
		}
		n = n.Elem()
	}
	if hs := n.FieldByName("handlers"); hs.Len() > 0 {
		chain := make([]unsafe.Pointer, hs.Len())
		for i := range chain {
			chain[i] = *(*unsafe.Pointer)(unsafe.Pointer(hs.Index(i).UnsafeAddr()))
		}
		out[method+" "+n.FieldByName("fullPath").String()] = chain
	}
	cs := n.FieldByName("children")
	for i := 0; i < cs.Len(); i++ {
		walk(cs.Index(i), method, out)
	}
}
