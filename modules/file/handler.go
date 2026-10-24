package file

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"nexadmin/core/auth"
	"nexadmin/core/response"
	"nexadmin/core/storage"
	"nexadmin/core/tenant"
	"nexadmin/pkg/idgen"
)

// Handler 是文件模块的 HTTP 层，只做参数解析和响应。
type Handler struct{ svc *Service }

func parseID(c *gin.Context) (idgen.ID, bool) {
	id, err := idgen.Parse(c.Param("id"))
	if err != nil {
		response.Fail(c, badRequest("ID 不正确"))
		return 0, false
	}
	return id, true
}

// ---- 分组 ----

func (h *Handler) ListGroups(c *gin.Context) {
	list, err := h.svc.ListGroups(c.Request.Context())
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, list)
}

func (h *Handler) CreateGroup(c *gin.Context) {
	var req groupReq
	if !response.BindJSON(c, &req) {
		return
	}
	g, err := h.svc.CreateGroup(c.Request.Context(), req.Name)
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, g)
}

func (h *Handler) RenameGroup(c *gin.Context) {
	id, ok := parseID(c)
	if !ok {
		return
	}
	var req groupReq
	if !response.BindJSON(c, &req) {
		return
	}
	g, err := h.svc.RenameGroup(c.Request.Context(), id, req.Name)
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, g)
}

func (h *Handler) DeleteGroup(c *gin.Context) {
	id, ok := parseID(c)
	if !ok {
		return
	}
	if err := h.svc.DeleteGroup(c.Request.Context(), id); err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, nil)
}

// ---- 文件 ----

func (h *Handler) List(c *gin.Context) {
	var q listQuery
	if !response.BindQuery(c, &q) {
		return
	}
	page, size := response.ParsePage(c)
	list, total, err := h.svc.List(c.Request.Context(), q, page, size)
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.Page(c, list, total, page, size)
}

func (h *Handler) Register(c *gin.Context) {
	var req registerReq
	if !response.BindJSON(c, &req) {
		return
	}
	username := ""
	if cl := auth.ClaimsFrom(c); cl != nil {
		username = cl.Username
	}
	list, err := h.svc.Register(c.Request.Context(), username, req)
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, list)
}

func (h *Handler) Update(c *gin.Context) {
	id, ok := parseID(c)
	if !ok {
		return
	}
	var req updateReq
	if !response.BindJSON(c, &req) {
		return
	}
	it, err := h.svc.Update(c.Request.Context(), id, req)
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, it)
}

func (h *Handler) Move(c *gin.Context) {
	var req moveReq
	if !response.BindJSON(c, &req) {
		return
	}
	if err := h.svc.Move(c.Request.Context(), req.IDs, req.GroupID); err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, nil)
}

func (h *Handler) DeleteOne(c *gin.Context) {
	id, ok := parseID(c)
	if !ok {
		return
	}
	if err := h.svc.Delete(c.Request.Context(), []idgen.ID{id}); err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, nil)
}

// DeleteMany 批量删除：DELETE /files?ids=1,2,3（与前端约定一致）。
func (h *Handler) DeleteMany(c *gin.Context) {
	var ids []idgen.ID
	for _, s := range strings.Split(c.Query("ids"), ",") {
		if s = strings.TrimSpace(s); s == "" {
			continue
		}
		id, err := idgen.Parse(s)
		if err != nil {
			response.Fail(c, badRequest("ids 中有不正确的 ID"))
			return
		}
		ids = append(ids, id)
	}
	if len(ids) == 0 || len(ids) > 500 {
		response.Fail(c, badRequest("ids 不能为空，且一次最多 500 个"))
		return
	}
	if err := h.svc.Delete(c.Request.Context(), ids); err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, nil)
}

// ---- 预览 / 下载（匿名路由：<img>、<video> 带不了 Authorization 头）----

// ServePublic 访问公开文件：路径里带随机串，不需要登录。
func (h *Handler) ServePublic(c *gin.Context) {
	var f File
	err := h.svc.db.WithContext(tenant.Skip(c.Request.Context())).
		First(&f, "token = ? AND visibility = ? AND storage_path <> ''", c.Param("token"), "public").Error
	h.serve(c, f, err, true)
}

// ServeSigned 访问私有文件：必须带未过期的签名（exp + sig）。
func (h *Handler) ServeSigned(c *gin.Context) {
	idStr := c.Param("id")
	if !h.svc.store.verify(idStr, c.Query("exp"), c.Query("sig")) {
		response.Fail(c, response.Forbidden("error.forbidden"))
		return
	}
	id, err := idgen.Parse(idStr)
	if err != nil {
		response.Fail(c, response.NotFound("error.not_found"))
		return
	}
	var f File
	err = h.svc.db.WithContext(tenant.Skip(c.Request.Context())).First(&f, "id = ? AND storage_path <> ''", id).Error
	h.serve(c, f, err, false)
}

func (h *Handler) serve(c *gin.Context, f File, findErr error, public bool) {
	if findErr != nil {
		if findErr == gorm.ErrRecordNotFound {
			response.Fail(c, response.NotFound("error.not_found"))
		} else {
			response.Fail(c, findErr)
		}
		return
	}
	st := h.svc.store
	ctx := c.Request.Context()
	inline := isInline(f.Category, f.MimeType)
	attachment := c.Query("download") == "1" || !inline

	// 私有文件 + 配置了 presign_redirect：签名链接校验通过后 302 到存储的预签名 URL（会暴露 bucket 地址，默认关闭）
	if !public && st.presignRedirect {
		opt := storage.PresignOptions{ContentType: f.MimeType}
		if attachment {
			opt.ContentDisposition = dispositionAttachment(f.Name)
		}
		if u, err := st.blobs.PresignGet(ctx, f.StoragePath, 5*time.Minute, opt); err == nil {
			c.Header("Cache-Control", "no-store")
			c.Redirect(http.StatusFound, u)
			return
		} else if !errors.Is(err, storage.ErrNotSupported) {
			response.Fail(c, err)
			return
		}
	}

	hd := c.Writer.Header()
	setServeHeaders := func() {
		hd.Set("Content-Type", f.MimeType)
		hd.Set("X-Content-Type-Options", "nosniff")
		// 同域下提供用户上传的内容，用 CSP sandbox 禁掉脚本（例如 SVG 里夹带 script）
		hd.Set("Content-Security-Policy", "sandbox; default-src 'none'; style-src 'unsafe-inline'; img-src data:; media-src 'self'")
		if public {
			hd.Set("Cache-Control", "public, max-age=3600")
		} else {
			hd.Set("Cache-Control", "private, max-age=300")
		}
		if attachment {
			hd.Set("Content-Disposition", dispositionAttachment(f.Name))
		}
	}

	// 本地存储：保持原行为，http.ServeContent 自带 Range / If-Modified-Since，视频可拖动进度
	if op, ok := st.blobs.(storage.Opener); ok {
		file, info, err := op.Open(ctx, f.StoragePath)
		if err != nil {
			response.Fail(c, response.NotFound("error.not_found").Wrap(err))
			return
		}
		defer file.Close()
		setServeHeaders()
		http.ServeContent(c.Writer, c.Request, f.Name, info.LastModified, file)
		return
	}

	// 其他存储（S3）：后端代理，不暴露 bucket；单段 Range 透传给存储
	var rng *storage.Range
	if rh := c.GetHeader("Range"); rh != "" {
		rng = parseByteRange(rh) // 不认识或多段 Range：按 RFC 7233 忽略，返回整个文件
	}
	obj, err := st.blobs.Get(ctx, f.StoragePath, rng)
	switch {
	case errors.Is(err, storage.ErrNotFound):
		response.Fail(c, response.NotFound("error.not_found"))
		return
	case errors.Is(err, storage.ErrRangeNotSatisfiable):
		hd.Set("Content-Range", "bytes */"+strconv.FormatInt(f.Size, 10))
		c.Status(http.StatusRequestedRangeNotSatisfiable)
		c.Writer.WriteHeaderNow()
		return
	case err != nil:
		response.Fail(c, err)
		return
	}
	defer obj.Body.Close()
	setServeHeaders()
	hd.Set("Accept-Ranges", "bytes")
	if obj.Info.ETag != "" {
		hd.Set("ETag", `"`+obj.Info.ETag+`"`)
	}
	if !obj.Info.LastModified.IsZero() {
		hd.Set("Last-Modified", obj.Info.LastModified.UTC().Format(http.TimeFormat))
	}
	hd.Set("Content-Length", strconv.FormatInt(obj.Length, 10))
	status := http.StatusOK
	if obj.Partial {
		status = http.StatusPartialContent
		hd.Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", obj.Start, obj.End, obj.Info.Size))
	}
	c.Status(status)
	_, _ = io.Copy(c.Writer, obj.Body)
}

// parseByteRange 解析单段 Range 头："bytes=a-b" / "bytes=a-" / "bytes=-n"。
func parseByteRange(h string) *storage.Range {
	spec, ok := strings.CutPrefix(strings.TrimSpace(h), "bytes=")
	if !ok || strings.Contains(spec, ",") {
		return nil
	}
	left, right, ok := strings.Cut(spec, "-")
	if !ok {
		return nil
	}
	if left == "" { // 后缀范围：最后 n 字节
		n, err := strconv.ParseInt(right, 10, 64)
		if err != nil || n <= 0 {
			return nil
		}
		return &storage.Range{Start: -n, End: -1}
	}
	start, err := strconv.ParseInt(left, 10, 64)
	if err != nil || start < 0 {
		return nil
	}
	if right == "" {
		return &storage.Range{Start: start, End: -1}
	}
	end, err := strconv.ParseInt(right, 10, 64)
	if err != nil || end < start {
		return nil
	}
	return &storage.Range{Start: start, End: end}
}
