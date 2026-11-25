// HTTP 层：文档管理（kb:list 查看 / kb:edit 上传·重嵌·设置 / kb:delete 删除）与测试提问。
// 模型调用、限流、配额、审计都在 core/ai 网关；这里只做路由、鉴权、上传解析与错误映射。

package kb

import (
	"errors"
	"io"
	"net/http"
	"strings"
	"unicode/utf8"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"nexadmin/core"
	"nexadmin/core/ai"
	"nexadmin/core/auth"
	"nexadmin/core/response"
	"nexadmin/modules/file"
	"nexadmin/pkg/idgen"
	"strconv"
)

// 支持的文档格式：txt / md 直接读；pdf、docx 走文本抽取（见 extract.go）。
var acceptExt = map[string]string{
	".txt": "text/plain", ".md": "text/markdown", ".markdown": "text/markdown",
	".pdf": "application/pdf", ".docx": "application/vnd.openxmlformats-officedocument.wordprocessingml.document",
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
	g.POST("/documents/from-file", core.Perm("kb:edit"), h.uploadFromLibrary)
	g.GET("/documents/:id/chunks", core.Perm("kb:list"), h.chunks)
	g.PUT("/documents/:id", core.Perm("kb:edit"), h.rename)
	g.DELETE("/documents/:id", core.Perm("kb:delete"), d.OperLogMiddleware("kb", "delete"), h.remove)
	g.POST("/documents/:id/reembed", core.Perm("kb:edit"), h.reembed)
	// 异步入库失败重试：原文已在库里，不用重传文件。
	g.POST("/documents/:id/retry", core.Perm("kb:edit"), h.retryIngest)
	// 重新切分：GET 只预览（改没改对先看切出来的块），POST 才落库。
	g.GET("/documents/:id/rechunk", core.Perm("kb:edit"), h.rechunkPreview)
	g.POST("/documents/:id/rechunk", core.Perm("kb:edit"), h.rechunk)
	g.GET("/stats", core.Perm("kb:list"), h.stats)
	// 未命中问题池：访客问了但知识库答不上来的问题，按热度攒着等管理员补答案。
	g.GET("/misses", core.Perm("kb:list"), h.listMisses)
	g.POST("/misses/ignore", core.Perm("kb:edit"), d.OperLogMiddleware("kb", "ignore"), h.ignoreMisses)
	g.POST("/misses/:id/resolve", core.Perm("kb:edit"), d.OperLogMiddleware("kb", "resolve"), h.resolveMiss)
	g.POST("/misses/:id/reopen", core.Perm("kb:edit"), h.reopenMiss)
	g.DELETE("/misses/:id", core.Perm("kb:delete"), d.OperLogMiddleware("kb", "delete"), h.deleteMiss)
	g.GET("/settings", core.Perm("kb:list"), h.getSettings)
	g.PUT("/settings", core.Perm("kb:edit"), h.putSettings)
	g.GET("/status", core.Perm("kb:list"), h.status)
	g.POST("/ask", core.Perm("kb:list"), h.ask)
}

// GET /plugins/kb/documents —— 文档列表（分页）。
// status=stale 是「需重新向量化」的派生态：失败的，或向量模型与当前配置不一致的。
func (h *handler) list(c *gin.Context) {
	sortField, desc := response.ParseSort(c)
	// 未指定排序字段时保持历史默认（新文档在前）
	if sortField == "" {
		desc = true
	}
	page, pageSize := response.ParsePage(c)
	q := DocumentQuery{Sort: sortField, Desc: desc, Page: page, PageSize: pageSize}
	status := c.Query("status")
	if status == "stale" {
		// 当前模型取不到（AI 未配置）时只有 failed 算 stale：用哨兵值让 SQL 走到模型比较分支。
		q.StaleModel = h.embedModel(c)
		if q.StaleModel == "" {
			q.StaleModel = "-"
		}
	} else {
		q.Status = status
	}
	rows, total, err := h.svc.ListDocuments(c.Request.Context(), q)
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.Page(c, rows, total, page, pageSize)
}

// embedModel 返回当前配置的 embedding 模型名；AI 未装配或未配置时为空。
func (h *handler) embedModel(c *gin.Context) string {
	if h.svc.gw == nil {
		return ""
	}
	cfg, err := h.svc.gw.Load(c.Request.Context())
	if err != nil {
		return ""
	}
	return cfg.Normalize().EmbedModel
}

// GET /plugins/kb/stats —— 全库统计（文档 / 知识块 / 未就绪 / 占用体积）。
func (h *handler) stats(c *gin.Context) {
	st, err := h.svc.Stats(c.Request.Context())
	respond(c, st, err)
}

// POST /plugins/kb/documents —— multipart 上传（file + 可选 title）。
// 这里只做抽取与建档，切块/向量化交给后台 worker：批量上传投递即返回。
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
	rawBytes, err := io.ReadAll(io.LimitReader(f, maxDocBytes+1))
	if err != nil {
		response.Fail(c, err)
		return
	}
	if len(rawBytes) > maxDocBytes {
		response.Fail(c, response.BadRequest("kb.document_too_large"))
		return
	}
	// 文本抽取按格式分流（txt/md 直读，pdf/docx 解析；扫描件 / 老 .doc 在这里明确报错）。
	text, err := extractText(ext, string(rawBytes))
	if err != nil {
		response.Fail(c, err)
		return
	}
	title := strings.TrimSpace(c.PostForm("title"))
	if title == "" {
		title = strings.TrimSuffix(fh.Filename, ext)
	}
	if utf8.RuneCountInString(title) > 200 {
		title = string([]rune(title)[:200])
	}
	doc, err := h.svc.Enqueue(c.Request.Context(), IngestInput{
		Title: title, Source: fh.Filename, Mime: mime, Size: fh.Size, Text: text,
	})
	respond(c, doc, err)
}

// libraryUploadBody 是「从文件库选取」的请求体：fileId 指向 sys_file 已上传文件。
type libraryUploadBody struct {
	FileID string `json:"fileId" binding:"required"`
	Title  string `json:"title"`
}

// uploadFromLibrary POST /plugins/kb/documents/from-file —— 复用文件库已上传的文件建文档：
// 不重复占存储、同一份 PDF 可以同时进多个知识库。读出的内容走与直传完全相同的
// 格式校验、大小上限与文本抽取管线。
func (h *handler) uploadFromLibrary(c *gin.Context) {
	var body libraryUploadBody
	if !response.BindJSON(c, &body) {
		return
	}
	id, err := idgen.Parse(body.FileID)
	if err != nil {
		response.Fail(c, response.BadRequest("error.bad_request"))
		return
	}
	ref, ferr := file.OpenByID(c.Request.Context(), id)
	switch {
	case errors.Is(ferr, file.ErrNoContent):
		response.Fail(c, response.BadRequest("kb.document_required"))
		return
	case errors.Is(ferr, gorm.ErrRecordNotFound):
		response.Fail(c, response.NotFound("error.not_found"))
		return
	case ferr != nil:
		// 文件模块未装配 / 存储读取失败：不落成「格式不支持」这类误导文案
		response.Fail(c, response.Internal())
		return
	}
	defer func() { _ = ref.Body.Close() }()

	ext := strings.ToLower(ref.Name)
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
	if ref.Size <= 0 || ref.Size > maxDocBytes {
		response.Fail(c, response.BadRequest("kb.document_too_large"))
		return
	}
	rawBytes, err := io.ReadAll(io.LimitReader(ref.Body, maxDocBytes+1))
	if err != nil {
		response.Fail(c, err)
		return
	}
	if len(rawBytes) > maxDocBytes {
		response.Fail(c, response.BadRequest("kb.document_too_large"))
		return
	}
	text, err := extractText(ext, string(rawBytes))
	if err != nil {
		response.Fail(c, err)
		return
	}
	title := strings.TrimSpace(body.Title)
	if title == "" {
		title = strings.TrimSuffix(ref.Name, ext)
	}
	if utf8.RuneCountInString(title) > 200 {
		title = string([]rune(title)[:200])
	}
	doc, err := h.svc.Enqueue(c.Request.Context(), IngestInput{
		Title: title, Source: ref.Name, Mime: mime, Size: ref.Size, Text: text,
	})
	respond(c, doc, err)
}

// GET /plugins/kb/documents/:id/chunks —— 知识块预览：看库里实际参与检索的文本，
// 排查「AI 为什么这么答 / 为什么没命中」时用。
func (h *handler) chunks(c *gin.Context) {
	doc, list, err := h.svc.ListChunks(c.Request.Context(), c.Param("id"))
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, gin.H{"document": doc, "chunks": list})
}

// PUT /plugins/kb/documents/:id —— 重命名：只改展示标题，切块与向量不动（无需重嵌）。
func (h *handler) rename(c *gin.Context) {
	var in struct {
		Title string `json:"title"`
	}
	if !response.BindJSON(c, &in) {
		return
	}
	doc, err := h.svc.RenameDocument(c.Request.Context(), c.Param("id"), in.Title)
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

// POST /plugins/kb/documents/:id/retry —— 失败文档重新排队（原文已在库里）。
func (h *handler) retryIngest(c *gin.Context) {
	doc, err := h.svc.RetryIngest(c.Request.Context(), c.Param("id"))
	respond(c, doc, err)
}

// GET /plugins/kb/documents/:id/rechunk —— 按当前分块参数预览重切结果（不落库）。
func (h *handler) rechunkPreview(c *gin.Context) {
	doc, blocks, err := h.svc.RechunkPreview(c.Request.Context(), c.Param("id"))
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, gin.H{"document": doc, "chunks": blocks, "count": len(blocks)})
}

// POST /plugins/kb/documents/:id/rechunk —— 按当前分块参数重切并重新向量化。
func (h *handler) rechunk(c *gin.Context) {
	doc, err := h.svc.Rechunk(c.Request.Context(), c.Param("id"))
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
	// 管理端测试提问打开召回调试：handoff 时带回最接近的候选与分数，调阈值不用盲调
	res, err := h.svc.Answer(c.Request.Context(), who, in.Query, true)
	respond(c, res, err)
}

// GET /plugins/kb/misses —— 未命中问题池。默认按提问次数降序，被问得最多的先补。
func (h *handler) listMisses(c *gin.Context) {
	sortField, desc := response.ParseSort(c)
	if sortField == "" {
		sortField, desc = "ask_count", true
	}
	page, pageSize := response.ParsePage(c)
	if pageSize > 100 {
		pageSize = 100
	}
	rows, total, err := h.svc.ListMisses(c.Request.Context(), MissQuery{
		Status: c.Query("status"), Sort: sortField, Desc: desc, Page: page, PageSize: pageSize,
	})
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.Page(c, rows, total, page, pageSize)
}

// POST /plugins/kb/misses/:id/resolve {"answer":"..."} —— 补答案并转成 QA 文档。
func (h *handler) resolveMiss(c *gin.Context) {
	var in struct {
		Answer string `json:"answer"`
	}
	if !response.BindJSON(c, &in) {
		return
	}
	doc, err := h.svc.ResolveMiss(c.Request.Context(), c.Param("id"), in.Answer)
	respond(c, doc, err)
}

// POST /plugins/kb/misses/ignore {"ids":[...]} —— 批量忽略（只处理待补的）。
func (h *handler) ignoreMisses(c *gin.Context) {
	var in struct {
		IDs []string `json:"ids"`
	}
	if !response.BindJSON(c, &in) {
		return
	}
	n, err := h.svc.IgnoreMisses(c.Request.Context(), in.IDs)
	respond(c, gin.H{"count": n}, err)
}

// POST /plugins/kb/misses/:id/reopen —— 把已忽略 / 已解决的项放回待补队列。
func (h *handler) reopenMiss(c *gin.Context) {
	miss, err := h.svc.ReopenMiss(c.Request.Context(), c.Param("id"))
	respond(c, miss, err)
}

// DELETE /plugins/kb/misses/:id —— 删除问题项（已转出的 QA 文档不跟着删）。
func (h *handler) deleteMiss(c *gin.Context) {
	respond(c, gin.H{"ok": true}, h.svc.DeleteMiss(c.Request.Context(), c.Param("id")))
}
