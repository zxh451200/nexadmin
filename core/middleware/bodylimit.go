package middleware

// 全局请求体上限：/api/v1 的 JSON 写接口原先没有统一封顶，单个巨型字符串字段就能把
// 内存吃满（OWASP API4:2023 资源无限消耗）。MaxBody 按 Content-Length 预判直接 413，
// chunked 请求由 http.MaxBytesReader 读体兜底掐断。

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"nexadmin/core/response"
)

// MaxBody 限制请求体大小。exemptPrefix 按 gin 路由模板（FullPath）做前缀豁免——
// 大体量端点（tus 上传的 PATCH/POST 自带 Upload-Length 校验）不该被 JSON 上限拦住。
// multipart/form-data 一律放行：那是上传通道，大小由各 handler 按 sys_config 的
// 上传限制（图片/视频/普通文件三档）自行校验，这里不越俎代庖。
//
// 超限判据分两层：Content-Length 声明的长度超限就在读体前直接 413（curl/axios/fetch
// 都带这个头，是确定路径——真实连接上跨限读取会把截断伪装成 *json.SyntaxError，
// MaxBytesError 到不了 BindJSON 的 413 分支，实测过）；chunked（无 Content-Length）
// 交给 MaxBytesReader 兜底，读体越限时连接被掐断。
func MaxBody(limit int64, exemptPrefix ...string) gin.HandlerFunc {
	tooLarge := func(c *gin.Context) {
		response.Fail(c, response.New(http.StatusRequestEntityTooLarge, "error.payload_too_large"))
		c.Abort()
	}
	return func(c *gin.Context) {
		if strings.HasPrefix(c.ContentType(), "multipart/") {
			c.Next()
			return
		}
		if c.Request.ContentLength > limit {
			tooLarge(c)
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
