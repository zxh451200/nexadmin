// Package middleware 是框架的 Gin 中间件：recovery / logger / cors / i18n / auth / tenant。
package middleware

import (
	"net/http"
	"runtime/debug"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"

	"nexadmin/core/i18n"
	"nexadmin/core/response"
	"nexadmin/core/tenant"
)

// Recovery 捕获 panic，记录堆栈，返回统一格式的 500。
func Recovery(log *zap.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		defer func() {
			if r := recover(); r != nil {
				log.Error("panic recovered", zap.Any("panic", r), zap.String("path", c.Request.URL.Path), zap.ByteString("stack", debug.Stack()))
				response.Fail(c, response.Internal())
			}
		}()
		c.Next()
	}
}

// Logger 记录每个请求；handler 通过 c.Error 附带的错误一并记录（不返回给调用方）。
func Logger(log *zap.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()
		c.Next()
		fields := []zap.Field{
			zap.String("method", c.Request.Method),
			zap.String("path", c.Request.URL.Path),
			zap.Int("status", c.Writer.Status()),
			zap.Duration("latency", time.Since(start)),
			zap.String("ip", c.ClientIP()),
		}
		if len(c.Errors) > 0 {
			fields = append(fields, zap.String("errors", c.Errors.String()))
		}
		switch s := c.Writer.Status(); {
		case s >= 500:
			log.Error("request", fields...)
		case s >= 400:
			log.Warn("request", fields...)
		default:
			log.Info("request", fields...)
		}
	}
}

// corsAllowHeaders / corsExposeHeaders 含 tus 断点续传协议所需的头。
const (
	corsAllowMethods = "GET, POST, PUT, PATCH, DELETE, OPTIONS"
	corsAllowHeaders = "Authorization, Content-Type, Accept-Language, X-Tenant-ID, X-Setup-Token, " +
		"Upload-Offset, Upload-Length, Upload-Metadata, Tus-Resumable, Upload-Defer-Length, Upload-Concat"
	corsExposeHeaders = "Location, Upload-Offset, Upload-Length, Tus-Resumable, Retry-After"
)

// CORS 默认同源：生产环境只对 allowed 白名单里的 Origin（精确匹配，忽略通配符 "*"）输出 CORS 头，
// 开发环境（dev=true）额外放行任意 Origin（前端 vite 开发服务器）。
// 认证走 Authorization 头而不是 Cookie，所以不输出 Allow-Credentials，也就不存在“通配符 + credentials”。
func CORS(dev bool, allowed ...string) gin.HandlerFunc {
	allow := make(map[string]bool, len(allowed))
	for _, o := range allowed {
		if o = strings.TrimRight(strings.TrimSpace(o), "/"); o != "" && o != "*" {
			allow[o] = true
		}
	}
	return func(c *gin.Context) {
		origin := c.GetHeader("Origin")
		if origin == "" || !(dev || allow[origin]) {
			c.Next()
			return
		}
		h := c.Writer.Header()
		h.Set("Access-Control-Allow-Origin", origin)
		h.Add("Vary", "Origin")
		h.Set("Access-Control-Expose-Headers", corsExposeHeaders)
		if c.Request.Method == http.MethodOptions {
			h.Add("Vary", "Access-Control-Request-Method")
			h.Add("Vary", "Access-Control-Request-Headers")
			h.Set("Access-Control-Allow-Methods", corsAllowMethods)
			h.Set("Access-Control-Allow-Headers", corsAllowHeaders)
			h.Set("Access-Control-Max-Age", "600")
			c.AbortWithStatus(http.StatusNoContent)
			return
		}
		c.Next()
	}
}

// I18n 按 Accept-Language 协商语言并写入 request context。
func I18n(b *i18n.Bundle) gin.HandlerFunc {
	return func(c *gin.Context) {
		lang := b.Negotiate(c.GetHeader("Accept-Language"))
		c.Request = c.Request.WithContext(i18n.WithLang(c.Request.Context(), lang))
		c.Header("Content-Language", lang)
		c.Next()
	}
}

// RuntimeTenant copies the resolved tenant into i18n's dependency-free context.
func RuntimeTenant() gin.HandlerFunc {
	return func(c *gin.Context) {
		ctx := c.Request.Context()
		if tid, ok := tenant.From(ctx); ok {
			ctx = i18n.WithRuntimeTenant(ctx, strconv.FormatInt(int64(tid), 10))
			c.Request = c.Request.WithContext(ctx)
		}
		c.Next()
	}
}
