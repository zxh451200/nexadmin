package middleware

// RequestID 中间件：为每个请求确定一个 id，用于把同一次请求在访问日志、错误上报、
// 异步任务里的记录串起来。

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
)

// RequestIDKey 是 gin context 里的 key：handler 与后续中间件用 c.GetString(RequestIDKey)
// 取当前请求的 id。
const RequestIDKey = "request_id"

// maxRequestIDLen 是采纳入站 X-Request-ID 的长度上限。
const maxRequestIDLen = 64

// requestIDCtxKey 是 request context 里的 key（私有类型，防跨包冲突）。
type requestIDCtxKey struct{}

// RequestID 为每个请求确定 request id：入站头 X-Request-ID 合法（非空、≤64 字节、可见 ASCII 无空白）
// 就沿用——网关 / 上游已生成的 id 得以贯穿整条链路；否则生成 16 字节随机 hex。
// id 回写响应头 X-Request-ID，并写入 gin context（同请求内取用）与 request context
// （job / 异步链路拿不到 gin context，经 RequestIDFrom 取用）。
func RequestID() gin.HandlerFunc {
	return func(c *gin.Context) {
		id := c.GetHeader("X-Request-ID")
		if !validRequestID(id) {
			id = newRequestID()
		}
		c.Set(RequestIDKey, id)
		c.Header("X-Request-ID", id)
		c.Request = c.Request.WithContext(context.WithValue(c.Request.Context(), requestIDCtxKey{}, id))
		c.Next()
	}
}

// RequestIDFrom 从 request context 取 request id：异步任务 / job 不在 gin 链路里，从这里取。
// 不在请求链路（没有中间件写入）时返回空串。
func RequestIDFrom(ctx context.Context) string {
	id, _ := ctx.Value(requestIDCtxKey{}).(string)
	return id
}

// validRequestID 判断入站 id 是否可采纳：只放可见 ASCII（0x21-0x7E）防头注入与日志污染，限长防刷屏。
func validRequestID(s string) bool {
	if s == "" || len(s) > maxRequestIDLen {
		return false
	}
	for i := 0; i < len(s); i++ {
		if b := s[i]; b < 0x21 || b > 0x7e {
			return false
		}
	}
	return true
}

// newRequestID 生成 16 字节随机 hex；crypto/rand 失败（实际不会发生）退化为纳秒时间戳，
// 保证响应头里始终有 id。
func newRequestID() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return strconv.FormatUint(uint64(time.Now().UnixNano()), 36)
	}
	return hex.EncodeToString(b)
}
