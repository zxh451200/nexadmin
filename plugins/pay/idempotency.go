package pay

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"strings"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"nexadmin/core/response"
)

// 请求级幂等（契约 Idempotency-Key）：同键同体重放首次响应并置
// X-Idempotent-Replayed: true；同键异体 422。业务键（bizType+bizNo）幂等仍由
// 唯一索引兜底，两者互补：前者防网关重发，后者防业务重试。
const (
	idemReplayHeader = "X-Idempotent-Replayed"
	idemMaxBody      = 64 << 10 // 幂等快照只存 64KB，足够管理端业务响应
)

// idempotency 是 pay 管理端写接口的幂等中间件。仅在带 Idempotency-Key 头时生效；
// 存储故障降级为仅业务键幂等（记录告警，不阻断资金请求）。
func idempotency(db *gorm.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		key := strings.TrimSpace(c.GetHeader("Idempotency-Key"))
		if key == "" {
			c.Next()
			return
		}
		if len(key) > 64 {
			key = key[:64]
		}
		var body bytes.Buffer
		if c.Request.Body != nil {
			_, _ = io.Copy(&body, c.Request.Body)
			_ = c.Request.Body.Close()
			c.Request.Body = io.NopCloser(bytes.NewReader(body.Bytes()))
		}
		sum := sha256.Sum256(append([]byte(key+"|"+c.FullPath()+"|"), body.Bytes()...))
		hash := hex.EncodeToString(sum[:])

		var rec IdemKey
		err := db.WithContext(c.Request.Context()).Where("idem_key = ?", key).Take(&rec).Error
		switch {
		case err == nil:
			if rec.ReqHash != hash {
				response.Fail(c, idemConflict())
				c.Abort()
				return
			}
			c.Header(idemReplayHeader, "true")
			c.Data(rec.RespStatus, "application/json; charset=utf-8", []byte(rec.RespBody))
			c.Abort()
			return
		case !errors.Is(err, gorm.ErrRecordNotFound):
			// 查询失败：降级放行，依赖业务键幂等。
			c.Next()
			return
		}

		cw := &captureWriter{ResponseWriter: c.Writer}
		c.Writer = cw
		c.Next()

		if cw.status < 200 || cw.status >= 300 || cw.len == 0 || cw.len > idemMaxBody {
			return
		}
		row := IdemKey{
			IdemKey: key, Path: truncate(c.FullPath(), 128), ReqHash: hash,
			RespStatus: cw.status, RespBody: cw.buf.String(),
		}
		if err := db.WithContext(c.Request.Context()).Create(&row).Error; err != nil {
			// 并发同键：第二请求存储失败即可忽略（首个请求已存）。
			_ = err
		}
	}
}

// captureWriter 捕获响应状态与字节，用于幂等重放快照。
type captureWriter struct {
	gin.ResponseWriter
	status int
	len    int
	buf    bytes.Buffer
}

func (w *captureWriter) WriteHeader(code int) {
	if w.status == 0 {
		w.status = code
	}
	w.ResponseWriter.WriteHeader(code)
}

func (w *captureWriter) Write(b []byte) (int, error) {
	if w.status == 0 {
		w.status = 200
	}
	w.len += len(b)
	if w.len <= idemMaxBody {
		w.buf.Write(b)
	}
	return w.ResponseWriter.Write(b)
}

func (w *captureWriter) WriteString(s string) (int, error) {
	return w.Write([]byte(s))
}
