package log

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/gin-gonic/gin"

	"nexadmin/core/auth"
)

const (
	maxBody   = 64 << 10 // 最多读取的请求体
	maxStored = 8 << 10  // 入库的参数/响应最大长度
)

// bodyTap 记录响应体前 maxStored 字节。
type bodyTap struct {
	gin.ResponseWriter
	buf bytes.Buffer
}

func (w *bodyTap) Write(b []byte) (int, error) {
	if room := maxStored - w.buf.Len(); room > 0 {
		w.buf.Write(b[:min(room, len(b))])
	}
	return w.ResponseWriter.Write(b)
}

// Middleware 返回一个记录操作日志的 Gin 中间件，挂在需要审计的写操作路由上：
//
//	r.POST("/users", core.Perm("user:create"), log.Default().Middleware("用户管理", "create"), h.Create)
//
// 请求结束后入队，由队列的冲刷协程异步写入；密码、令牌类字段会被打码；HTTP 状态 >= 400 记为失败，并附上内部错误。
// 入队是非阻塞的：队列满时丢弃并计数告警，不占 DB 连接、不拖慢接口，也不随并发无限起协程。
func (s *Service) Middleware(module, typ string) gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()
		var raw []byte
		if c.Request.Body != nil && c.Request.Method != "GET" {
			raw, _ = io.ReadAll(io.LimitReader(c.Request.Body, maxBody))
			c.Request.Body = io.NopCloser(io.MultiReader(bytes.NewReader(raw), c.Request.Body))
		}
		tap := &bodyTap{ResponseWriter: c.Writer}
		c.Writer = tap
		c.Next()

		cl := auth.ClaimsFrom(c)
		if cl == nil {
			return
		}
		l := &OperLog{Module: module, Type: typ, Operator: cl.Username, IP: c.ClientIP(), Status: StatusSuccess,
			CostMs: int(time.Since(start).Milliseconds()), Method: c.Request.Method, URL: c.Request.URL.RequestURI(),
			Params: paramsOf(c, raw), Response: clip(tap.buf.String())}
		l.TenantID = cl.TenantID
		if c.Writer.Status() >= 400 {
			l.Status, l.ErrorStack = StatusFailed, clip(c.Errors.String())
		}
		// 入队前就脱离取消：排队期间请求结束/客户端断连不该让已发生的写操作丢掉审计。
		ctx := context.WithoutCancel(c.Request.Context())
		s.enqueue(ctx, l)
	}
}

// paramsOf 取请求参数：有 JSON 体用 JSON 体（打码敏感字段），否则用查询参数。
func paramsOf(c *gin.Context, body []byte) string {
	if len(bytes.TrimSpace(body)) > 0 {
		var m map[string]any
		if json.Unmarshal(body, &m) == nil {
			for k := range m {
				if sensitive(k) {
					m[k] = "******"
				}
			}
			b, _ := json.Marshal(m)
			return clip(string(b))
		}
		return clip(string(body))
	}
	q := map[string]any{}
	for k, v := range c.Request.URL.Query() {
		if sensitive(k) {
			q[k] = "******"
		} else if len(v) == 1 {
			q[k] = v[0]
		} else {
			q[k] = v
		}
	}
	if len(q) == 0 {
		return ""
	}
	b, _ := json.Marshal(q)
	return clip(string(b))
}

func sensitive(key string) bool {
	k := strings.ToLower(key)
	if k == "code" {
		return true
	}
	for _, w := range []string{"password", "token", "secret", "captcha"} {
		if strings.Contains(k, w) {
			return true
		}
	}
	return false
}

// clip 截断到 maxStored 字节，并保证是合法 UTF-8。
func clip(s string) string {
	if len(s) <= maxStored {
		return s
	}
	s = s[:maxStored]
	for !utf8.ValidString(s) && len(s) > 0 {
		s = s[:len(s)-1]
	}
	return s
}
