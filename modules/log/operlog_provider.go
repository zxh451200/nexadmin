package log

import (
	"github.com/gin-gonic/gin"
)

// operLogProvider 把 Service 适配成 core.OperationLogProvider，
// 供 core.Deps.OperLogMiddleware 在任意注册时序下解析（见 core/operlog.go）。
type operLogProvider struct {
	svc *Service
}

// Middleware 实现 core.OperationLogProvider。
func (p operLogProvider) Middleware(module, action string) gin.HandlerFunc {
	return p.svc.Middleware(module, action)
}
