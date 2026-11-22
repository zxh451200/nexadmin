package middleware

// 全局请求体上限：/api/v1 的 JSON 写接口原先没有统一封顶，单个巨型字符串字段就能把
// 内存吃满（OWASP API4:2023 资源无限消耗）。MaxBody 用 http.MaxBytesReader 封顶，
// 超限后 BindJSON 读体失败、按 413（error.payload_too_large）返回。

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
)

// MaxBody 限制请求体大小。exemptPrefix 按 gin 路由模板（FullPath）做前缀豁免——
// 大体量端点（tus 上传的 PATCH/POST 自带 Upload-Length 校验）不该被 JSON 上限拦住。
// multipart/form-data 一律放行：那是上传通道，大小由各 handler 按 sys_config 的
// 上传限制（图片/视频/普通文件三档）自行校验，这里不越俎代庖。
func MaxBody(limit int64, exemptPrefix ...string) gin.HandlerFunc {
	return func(c *gin.Context) {
		if strings.HasPrefix(c.ContentType(), "multipart/") {
			c.Next()
			return
		}
		p := c.FullPath()
		if p == "" {
			p = c.Request.URL.Path
		}
		for _, pre := range exemptPrefix {
			if pre != "" && strings.HasPrefix(p, pre) {
				c.Next()
				return
			}
		}
		c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, limit)
		c.Next()
	}
}
