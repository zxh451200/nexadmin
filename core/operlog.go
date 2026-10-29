package core

import (
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

// SetOperationLogProvider 由 log 模块在 Register 里调用；重复注册后者覆盖。
func SetOperationLogProvider(p OperationLogProvider) { operLogProvider.Store(&p) }

// OperLogMiddleware 返回写操作日志的中间件。业务模块在 Register 里即可直接挂到路由：
//
//	g.POST("/articles", core.Perm("article:create"), d.OperLogMiddleware("article", "create"), h.create)
//
// 不需要关心 log 模块的注册时序，也不需要判空：log 模块尚未注册（时序）或被停用时，
// 返回一个直通中间件（不记日志）。替代旧的“请求时取 logmod.Default()”写法。
func (d *Deps) OperLogMiddleware(module, action string) gin.HandlerFunc {
	if p := operLogProvider.Load(); p != nil {
		return (*p).Middleware(module, action)
	}
	return func(c *gin.Context) { c.Next() }
}
