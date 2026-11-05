package file

import (
	"archive/zip"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"nexadmin/core/auth"
	"nexadmin/core/i18n"
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
		response.Fail(c, response.BadRequest("file.id_invalid"))
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

// GET /files/usage —— 侧栏用量（已用字节 + 套餐额度 MB，0 = 不限）。
func (h *Handler) Usage(c *gin.Context) {
	used, quotaMB, err := h.svc.Usage(c.Request.Context())
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, gin.H{"usedBytes": used, "quotaMB": quotaMB})
}

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

// DownloadZip POST /files/download-zip body: {ids:[...]} —— 把选中的文件打包成一个 zip 返回。
// zip 内按原始文件名落盘，同名文件补 -2 / -3 后缀到真正唯一；
// 没进包的文件（外链、读不出内容）不静默丢，见 writeFilesZip 的交代方式。
func (h *Handler) DownloadZip(c *gin.Context) {
	var req moveReq
	if !response.BindJSON(c, &req) {
		return
	}
	ctx := c.Request.Context()
	files, err := h.svc.FilesForDownload(ctx, req.IDs)
	if err != nil {
		// 还没写出任何字节：这里能正常回 400 + 具体数字，前端把后端文案原样 toast。
		response.Fail(c, err)
		return
	}
	c.Header("Content-Type", "application/zip")
	c.Header("Content-Disposition", dispositionAttachment("files-"+time.Now().Format("20060102-150405")+".zip"))
	zw := zip.NewWriter(c.Writer)
	if err := h.writeFilesZip(ctx, zw, files); err != nil {
		_ = c.Error(err)
		// 故意不 Close：半包不写中央目录，解压端会报「文件损坏」。
		// 已经吐出字节就改不了状态码，但给一个「看起来成功、里面某条 CRC 不对」的包更坏。
		return
	}
	if err := zw.Close(); err != nil {
		_ = c.Error(err)
	}
}

// zipManifestName 是随包交回的「点名了但没进包」清单。名字留 ASCII：
// 非 ASCII 条目名在老 Windows 解压器上会按本地代码页解码成乱码，清单本身读不出来的话
// 就又回到静默丢文件了。
const zipManifestName = "_FAILED.txt"

// zipSkip 是一条没进包的记录（名字 + 已按请求语言翻好的原因）。
type zipSkip struct {
	name   string
	reason string
}

// writeFilesZip 把 files 逐个写进 zw，外链与读不出内容的记入 skipped。
//
// 为什么不整包失败：一个 500 个文件的包里有一个对象丢了，让用户重头再点一遍是惩罚用户；
// 但「什么都不留地跳过」更糟——用户少拿一个文件且毫不知情。响应已经在流式写出，
// 状态码和响应体都改不了，所以把名单随包交回：浏览器保存附件时不展示响应头，
// 下载完成后也不会把响应头留在任何地方，用户能看到的只有解压出来的文件。
// 这与本仓库批量接口用 skipped 字段交代「点名但没处理」的行是同一口径
// （plugins/codegen 的安装结果、plugins/im 的批量删除），清单条目是它的流式版本。
func (h *Handler) writeFilesZip(ctx context.Context, zw *zip.Writer, files []File) error {
	used := map[string]bool{zipManifestName: true} // 预留清单名：真有文件叫 _FAILED.txt 时它让行成 _FAILED-2.txt
	var skipped []zipSkip
	for _, f := range files {
		if f.StoragePath == "" {
			skipped = append(skipped, zipSkip{f.Name, i18n.T(ctx, "file.zip_skip_external")})
			continue // 外链文件没有字节可打包
		}
		// 优先 Opener（本地存储零拷贝句柄）；其他存储走单段 Get（zip 不需要 Range）
		var src io.ReadCloser
		if op, ok := h.svc.store.blobs.(storage.Opener); ok {
			rc, _, oerr := op.Open(ctx, f.StoragePath)
			if oerr != nil {
				skipped = append(skipped, zipSkip{f.Name, i18n.T(ctx, "file.zip_skip_read")})
				continue
			}
			src = rc
		} else {
			obj, oerr := h.svc.store.blobs.Get(ctx, f.StoragePath, nil)
			if oerr != nil {
				skipped = append(skipped, zipSkip{f.Name, i18n.T(ctx, "file.zip_skip_read")})
				continue
			}
			src = obj.Body
		}
		// 名字在取到内容之后才领用：跳过的文件不该占掉一个序号，否则 a.txt 读失败时
		// 下一个 a.txt 会白拿 a-2.txt。
		w, err := zw.Create(zipEntryName(used, f.Name))
		if err != nil {
			_ = src.Close()
			return err
		}
		if _, err := io.Copy(w, src); err != nil {
			_ = src.Close()
			return err
		}
		_ = src.Close()
	}
	if len(skipped) == 0 {
		return nil
	}
	return writeZipManifest(ctx, zw, skipped)
}

// zipEntryName 从 used 里领一个没被占用的条目名：a.txt → a-2.txt → a-3.txt……
// 每次领用都回填 used，所以三个同名文件拿到的是三个不同条目——旧写法只在原始名上计数
// （used[f.Name]++），第二个和第三个同名都算成 a-2.txt，而 zip.Writer 不拒绝重名，
// 后写的直接顶掉前一个，用户少拿一个文件且毫不知情。
//
// 名字先过 cleanName：历史行里若有带 / 或 \ 的名字，不能让它变成包内目录或 ../ 跳出
// （解压时的 zip-slip）。
func zipEntryName(used map[string]bool, raw string) string {
	name := cleanName(raw)
	if name == "." || name == ".." {
		name = "unnamed"
	}
	if !used[name] {
		used[name] = true
		return name
	}
	ext := filepath.Ext(name)
	base := strings.TrimSuffix(name, ext)
	for n := 2; ; n++ {
		cand := base + "-" + strconv.Itoa(n) + ext
		if !used[cand] {
			used[cand] = true
			return cand
		}
	}
}

// writeZipManifest 写清单条目本体（名字已在 used 里预留，这里不再领用）。
// 带 BOM：清单几乎只会被 Windows 记事本打开，没 BOM 时中文按本地代码页显示成乱码
// （与前端导出 CSV 的 \uFEFF 同一处理）。
func writeZipManifest(ctx context.Context, zw *zip.Writer, skipped []zipSkip) error {
	w, err := zw.Create(zipManifestName)
	if err != nil {
		return err
	}
	var sb strings.Builder
	sb.WriteString("\uFEFF")
	sb.WriteString(i18n.T(ctx, "file.zip_failed_header", len(skipped)))
	sb.WriteString("\r\n\r\n")
	for _, s := range skipped {
		sb.WriteString(cleanName(s.name) + "\t" + s.reason + "\r\n")
	}
	_, err = io.WriteString(w, sb.String())
	return err
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
			response.Fail(c, response.BadRequest("file.ids_invalid"))
			return
		}
		ids = append(ids, id)
	}
	if len(ids) == 0 || len(ids) > 500 {
		response.Fail(c, response.BadRequest("file.ids_limit"))
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
	if err == nil {
		f = h.negotiateVariant(c, f)
	}
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
	if err == nil {
		f = h.negotiateVariant(c, f)
	}
	h.serve(c, f, err, false)
}

// negotiateVariant 在客户端接受 WebP 且原图有衍生副本时，用副本替换（URL 不变）。
// 副本不存在、原图就是副本、或客户端不支持时原样返回。
func (h *Handler) negotiateVariant(c *gin.Context, f File) File {
	if f.Category != "image" || f.VariantOf != 0 || !acceptsWebP(c.GetHeader("Accept")) {
		return f
	}
	var v File
	err := h.svc.db.WithContext(tenant.Skip(c.Request.Context())).
		First(&v, "variant_of = ? AND mime_type = ? AND storage_path <> ''", f.ID, "image/webp").Error
	if err != nil {
		return f
	}
	v.Name = f.Name
	return v
}

// acceptsWebP 只认显式声明 image/webp 的客户端（浏览器的 Accept 都带）；
// q=0 表示明确拒绝，不能当成接受。
func acceptsWebP(accept string) bool {
	for _, part := range strings.Split(accept, ",") {
		fields := strings.Split(part, ";")
		if !strings.EqualFold(strings.TrimSpace(fields[0]), "image/webp") {
			continue
		}
		for _, p := range fields[1:] {
			q := strings.ReplaceAll(strings.ToLower(strings.TrimSpace(p)), " ", "")
			if q == "q=0" || q == "q=0.0" || q == "q=0.00" || q == "q=0.000" {
				return false
			}
		}
		return true
	}
	return false
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
		if f.Category == "image" && !attachment {
			// 同一个 URL 会按 Accept 返回原图或 WebP 副本，缓存必须按请求头区分。
			hd.Set("Vary", "Accept")
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
