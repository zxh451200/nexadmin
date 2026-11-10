package core

import (
	"sync"
	"sync/atomic"

	"github.com/gin-gonic/gin"
)

// OperationLogProvider 操作日志中间件的提供方，由 log 模块在 Register 里注入。
type OperationLogProvider interface {
	// Middleware 返回写操作日志的中间件（module 是模块名，action 如 "create"）。
	Middleware(module, action string) gin.HandlerFunc
}

// operLogProvider 保存 log 模块注册的提供方。
// 用 atomic 是因为业务模块（internal/，按包路径字母序）的 Register 可能早于 log 模块执行——
// 注册期间取不到属正常现象，业务路由应无条件挂载，由本包装在请求期完成解析。
var operLogProvider atomic.Pointer[OperationLogProvider]

// operLogHandlers 缓存请求期解析出的中间件：key 为 module\x00action。
// 不缓存的话每个请求都要经过 provider.Middleware() 造一遍 handler。
var operLogHandlers sync.Map

// SetOperationLogProvider 由 log 模块在 Register 里调用；重复注册后者覆盖。
func SetOperationLogProvider(p OperationLogProvider) { operLogProvider.Store(&p) }

// OperLogMiddleware 返回写操作日志的中间件。业务模块在 Register 里即可直接挂到路由：
//
//	g.POST("/articles", core.Perm("article:create"), d.OperLogMiddleware("article", "create"), h.create)
//
// 不需要关心 log 模块的注册时序，也不需要判空。provider 必须在**请求期**解析：
// 注册期一次性解析的话，注册序早于 log 模块的模块拿到的全是直通中间件，操作日志
// 系统性缺失。log 模块尚未注册（时序）或被停用时，直通放行（不记日志）。
func (d *Deps) OperLogMiddleware(module, action string) gin.HandlerFunc {
	key := module + "\x00" + action
	if h, ok := operLogHandlers.Load(key); ok {
		return h.(gin.HandlerFunc)
	}
	return func(c *gin.Context) {
		if p := operLogProvider.Load(); p != nil {
			h, _ := operLogHandlers.LoadOrStore(key, (*p).Middleware(module, action))
			h.(gin.HandlerFunc)(c)
			return
		}
		c.Next()
	}
}
