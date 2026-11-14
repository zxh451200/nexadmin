// HTTP 层：文档管理（kb:list 查看 / kb:edit 上传·重嵌·设置 / kb:delete 删除）与测试提问。
// 模型调用、限流、配额、审计都在 core/ai 网关；这里只做路由、鉴权、上传解析与错误映射。

package kb

import (
	"io"
	"net/http"
	"strings"
	"unicode/utf8"

	"github.com/gin-gonic/gin"

	"nexadmin/core"
	"nexadmin/core/ai"
	"nexadmin/core/auth"
	"nexadmin/core/response"
	"strconv"
)

// 纯文本白名单：一期只收 txt / markdown（解析链路最稳）；PDF/DOCX 二期。
var acceptExt = map[string]string{
	".txt": "text/plain", ".md": "text/markdown", ".markdown": "text/markdown",
}

type handler struct {
	svc *Service
}

func respond(c *gin.Context, data any, err error) {
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, data)
}

// Register 注册管理路由。文档与设置都是平台级表，整组只对平台调用方开放（与 im 同口径）。
func (h *handler) Register(r *gin.RouterGroup, d *core.Deps) {
	g := r.Group("/plugins/kb")
	g.Use(core.RequirePlatformFeature())
	g.GET("/documents", core.Perm("kb:list"), h.list)
	g.POST("/documents", core.Perm("kb:edit"), h.upload)
	g.DELETE("/documents/:id", core.Perm("kb:delete"), d.OperLogMiddleware("kb", "delete"), h.remove)
	g.POST("/documents/:id/reembed", core.Perm("kb:edit"), h.reembed)
	g.GET("/settings", core.Perm("kb:list"), h.getSettings)
	g.PUT("/settings", core.Perm("kb:edit"), h.putSettings)
	g.GET("/status", core.Perm("kb:list"), h.status)
	g.POST("/ask", core.Perm("kb:list"), h.ask)
}

// GET /plugins/kb/documents —— 文档列表。
func (h *handler) list(c *gin.Context) {
	rows, err := h.svc.ListDocuments(c.Request.Context(), DocumentQuery{Status: c.Query("status")})
	respond(c, rows, err)
}

// POST /plugins/kb/documents —— multipart 上传（file + 可选 title），同步切分并向量化。
func (h *handler) upload(c *gin.Context) {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, maxDocBytes+(64<<10))
	fh, err := c.FormFile("file")
	if err != nil {
		response.Fail(c, response.BadRequest("kb.document_required"))
		return
	}
	if fh.Size <= 0 || fh.Size > maxDocBytes {
		response.Fail(c, response.BadRequest("kb.document_too_large"))
		return
	}
	ext := strings.ToLower(fh.Filename)
	if i := strings.LastIndexByte(ext, '.'); i >= 0 {
		ext = ext[i:]
	} else {
		ext = ""
	}
	mime, ok := acceptExt[ext]
	if !ok {
		response.Fail(c, response.BadRequest("kb.document_type"))
		return
	}
	f, err := fh.Open()
	if err != nil {
		response.Fail(c, err)
		return
	}
	defer func() { _ = f.Close() }()
	raw, err := io.ReadAll(io.LimitReader(f, maxDocBytes+1))
	if err != nil {
		response.Fail(c, err)
		return
	}
	if len(raw) > maxDocBytes {
		response.Fail(c, response.BadRequest("kb.document_too_large"))
		return
	}
	text := strings.TrimPrefix(string(raw), "\uFEFF") // 去 BOM
	// 二进制防线：UTF-8 校验不过、含 NUL 或控制字符占比异常，都按「不是文本」拒收。
	if !utf8.ValidString(text) || strings.ContainsRune(text, '\x00') {
		response.Fail(c, response.BadRequest("kb.document_type"))
		return
	}
	title := strings.TrimSpace(c.PostForm("title"))
	if title == "" {
		title = strings.TrimSuffix(fh.Filename, ext)
	}
	if utf8.RuneCountInString(title) > 200 {
		title = string([]rune(title)[:200])
	}
	doc, err := h.svc.Ingest(c.Request.Context(), IngestInput{
		Title: title, Source: fh.Filename, Mime: mime, Size: fh.Size, Text: text,
	})
	respond(c, doc, err)
}

// DELETE /plugins/kb/documents/:id —— 删除文档与切块。
func (h *handler) remove(c *gin.Context) {
	respond(c, gin.H{"ok": true}, h.svc.DeleteDocument(c.Request.Context(), c.Param("id")))
}

// POST /plugins/kb/documents/:id/reembed —— 用当前 embedding 模型重跑该文档。
func (h *handler) reembed(c *gin.Context) {
	doc, err := h.svc.Reembed(c.Request.Context(), c.Param("id"))
	respond(c, doc, err)
}

// GET /plugins/kb/settings
func (h *handler) getSettings(c *gin.Context) {
	respond(c, h.svc.Settings(c.Request.Context()), nil)
}

// PUT /plugins/kb/settings
func (h *handler) putSettings(c *gin.Context) {
	var in Settings
	if !response.BindJSON(c, &in) {
		return
	}
	st, err := h.svc.SaveSettings(c.Request.Context(), in)
	respond(c, st, err)
}

// GET /plugins/kb/status —— 前端据此提示「先去系统设置配置 AI / Embedding 模型」。
func (h *handler) status(c *gin.Context) {
	out := gin.H{"embedReady": false, "chatReady": false}
	if h.svc.gw == nil {
		response.OK(c, out)
		return
	}
	cfg, err := h.svc.gw.Load(c.Request.Context())
	if err != nil {
		response.Fail(c, err)
		return
	}
	cfg = cfg.Normalize()
	out["embedReady"] = cfg.EmbedReady()
	out["chatReady"] = cfg.Ready()
	out["model"] = cfg.Model
	out["embedModel"] = cfg.EmbedModel
	response.OK(c, out)
}

// POST /plugins/kb/ask {"query":"..."} —— 测试提问：不走 IM，直接看检索命中与模型回答。
func (h *handler) ask(c *gin.Context) {
	var in struct {
		Query string `json:"query"`
	}
	if !response.BindJSON(c, &in) {
		return
	}
	cl := auth.ClaimsFrom(c)
	who := ai.Caller{}
	if cl != nil {
		who = ai.Caller{UserID: strconv.FormatInt(int64(cl.UserID), 10), TenantID: strconv.FormatInt(int64(cl.TenantID), 10), Exempt: cl.Super}
	}
	res, err := h.svc.Answer(c.Request.Context(), who, in.Query)
	respond(c, res, err)
}
