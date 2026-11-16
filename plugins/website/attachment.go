package website

// 询盘附件：商详「发送询盘」弹窗里买家可以带图纸/规格书/实拍图。
// 上传与留言分两步：先 POST /website/inquiry/attachments 落一行（message_id = 0）拿 token，
// 提交询盘时把 token 列表随表单一起发过来，服务端在同一个事务里绑定到留言；
// 没人认领的行超过 24 小时由下一次上传顺手清掉，存储对象一起删。
// 下载走 GET /website/inquiry/attachments/:token —— token 不可枚举，不需要登录。

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"io"
	"net/http"
	"net/url"
	"path"
	"strings"
	"time"
	"unicode"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
	"gorm.io/gorm"

	"nexadmin/core"
	"nexadmin/core/response"
	"nexadmin/core/storage"
	"nexadmin/pkg/idgen"
)

const (
	// 附件上限的内置默认（系统参数「上传限制」没设时用这些）。
	inquiryImageBytes = 8 << 20
	inquiryDocBytes   = 20 << 20
	inquiryTextBytes  = 2 << 20
	// inquiryUploadCeiling 是传输层硬顶：参数被调成离谱值也搬不走大文件。
	inquiryUploadCeiling = 50 << 20
	// maxInquiryFiles 是一条询盘最多带几个附件（弹窗里也是这个数）。
	maxInquiryFiles = 5
	// staleInquiryFile 是未绑定附件的保留时长：过了就删行删对象。
	staleInquiryFile = 24 * time.Hour
)

// inquiryCeilings 是一次上传请求生效的上限。
type inquiryCeilings struct {
	image   int64
	doc     int64
	text    int64
	maxBody int64
}

func defaultInquiryCeilings() inquiryCeilings {
	return inquiryCeilings{image: inquiryImageBytes, doc: inquiryDocBytes, text: inquiryTextBytes}
}

// inquiryCeilingsFor 解析生效上限：系统参数「上传限制」> 内置默认，并按传输硬顶截顶。
func inquiryCeilingsFor(ctx context.Context) inquiryCeilings {
	c := defaultInquiryCeilings()
	if l, ok := core.UploadLimitsFromSettings(ctx); ok {
		c.image = core.ResolveUploadCeiling(l.MaxImageMB, c.image, inquiryUploadCeiling)
		c.doc = core.ResolveUploadCeiling(l.MaxFileMB, c.doc, inquiryUploadCeiling)
	}
	c.maxBody = max(inquiryTextBytes, max(c.image, c.doc))
	return c
}

// InquiryAttachmentLimits 是下发给前端的上传限制：前端按它渲染 accept 与提示文案。
type InquiryAttachmentLimits struct {
	Accept   string `json:"accept"`
	MaxBytes int64  `json:"maxBytes"`
	MaxText  string `json:"maxText"`
	MaxFiles int    `json:"maxFiles"`
}

// InquiryAttachmentLimitsFor 返回当前生效的限制；类型白名单由服务端持有。
func InquiryAttachmentLimitsFor(ctx context.Context) InquiryAttachmentLimits {
	c := inquiryCeilingsFor(ctx)
	return InquiryAttachmentLimits{
		Accept: inquiryAccept(), MaxBytes: c.maxBody,
		MaxText: core.MBText(c.maxBody), MaxFiles: maxInquiryFiles,
	}
}

// inquiryAccept 与 inquiryRuleFor 的白名单同源：后端不收的类型不出现在选择器里。
func inquiryAccept() string {
	return strings.Join([]string{
		"image/png", "image/jpeg", "image/gif", "image/webp",
		"application/pdf", "text/plain", ".zip", ".docx", ".xlsx", ".pptx",
	}, ",")
}

// inquiryRule 描述一类可上传文件的落盘扩展名与大小上限。
type inquiryRule struct {
	ext string
	max int64
}

// inquiryRuleFor 按内容嗅探出的 MIME（必要时结合原始文件名）决定是否接收。
func inquiryRuleFor(c inquiryCeilings, mime, filename string) (inquiryRule, bool) {
	switch strings.ToLower(mime) {
	case "image/png":
		return inquiryRule{".png", c.image}, true
	case "image/jpeg":
		return inquiryRule{".jpg", c.image}, true
	case "image/gif":
		return inquiryRule{".gif", c.image}, true
	case "image/webp":
		return inquiryRule{".webp", c.image}, true
	case "application/pdf":
		return inquiryRule{".pdf", c.doc}, true
	case "text/plain":
		return inquiryRule{".txt", c.text}, true
	case "application/zip":
		// Office 文档本质是 zip，按原始扩展名保留可读的扩展名。
		switch strings.ToLower(path.Ext(filename)) {
		case ".docx":
			return inquiryRule{".docx", c.doc}, true
		case ".xlsx":
			return inquiryRule{".xlsx", c.doc}, true
		case ".pptx":
			return inquiryRule{".pptx", c.doc}, true
		}
		return inquiryRule{".zip", c.doc}, true
	}
	return inquiryRule{}, false
}

// safeFileName 去掉路径与控制字符，只保留展示用文件名。
func safeFileName(name string) string {
	name = path.Base(strings.ReplaceAll(name, "\\", "/"))
	var b strings.Builder
	for _, r := range name {
		if unicode.IsControl(r) {
			continue
		}
		b.WriteRune(r)
	}
	out := strings.TrimSpace(b.String())
	if out == "" || out == "." || out == ".." {
		return "file"
	}
	if len([]rune(out)) > 120 {
		out = string([]rune(out)[:120])
	}
	return out
}

// inquiryToken 是 32 位十六进制随机串：不可枚举，公开下载地址只靠它。
func inquiryToken() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return ""
	}
	return hex.EncodeToString(b)
}

// ---- HTTP ----

// POST /website/inquiry/attachments —— 访客上传一个询盘附件，返回 token 与展示信息。
func (h *siteHandler) inquiryUpload(c *gin.Context) {
	fail := func(status int, code string) {
		c.JSON(status, gin.H{"ok": false, "error": code})
	}
	if h.svc.Storage() == nil {
		fail(http.StatusServiceUnavailable, "unavailable")
		return
	}
	if h.uploadLocal != nil && !h.uploadLocal.allow(c.ClientIP()) {
		c.Header("Retry-After", "60")
		fail(http.StatusTooManyRequests, "rate")
		return
	}
	ceil := inquiryCeilingsFor(c.Request.Context())
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, ceil.maxBody+1<<20)
	fh, err := c.FormFile("file")
	if err != nil {
		fail(http.StatusBadRequest, "required")
		return
	}
	if fh.Size <= 0 {
		fail(http.StatusBadRequest, "empty")
		return
	}
	f, err := fh.Open()
	if err != nil {
		fail(http.StatusBadRequest, "required")
		return
	}
	defer func() { _ = f.Close() }()
	head := make([]byte, 512)
	n, _ := io.ReadFull(f, head)
	mime := strings.TrimSpace(strings.Split(http.DetectContentType(head[:n]), ";")[0])
	rule, ok := inquiryRuleFor(ceil, mime, fh.Filename)
	if !ok {
		fail(http.StatusBadRequest, "type")
		return
	}
	if fh.Size > rule.max {
		fail(http.StatusBadRequest, "tooLarge")
		return
	}
	token := inquiryToken()
	if token == "" {
		fail(http.StatusInternalServerError, "fail")
		return
	}
	key := "inquiry/" + time.Now().UTC().Format("200601") + "/" + token + rule.ext
	body := io.MultiReader(bytes.NewReader(head[:n]), f)
	if err := h.svc.Storage().Put(c.Request.Context(), key, body, fh.Size, storage.PutOptions{ContentType: mime}); err != nil {
		if h.log != nil {
			h.log.Warn("website: 询盘附件落存储失败", zap.Error(err))
		}
		fail(http.StatusInternalServerError, "fail")
		return
	}
	row, err := h.svc.SaveMessageFile(c.Request.Context(), token, safeFileName(fh.Filename), mime, fh.Size, key)
	if err != nil {
		_ = h.svc.Storage().Delete(c.Request.Context(), key)
		fail(http.StatusInternalServerError, "fail")
		return
	}
	// 顺手清掉没人认领的旧行：访客传完就关页面的情况很常见。
	h.svc.PurgeStaleMessageFiles(c.Request.Context(), staleInquiryFile)
	c.JSON(http.StatusOK, gin.H{"ok": true, "file": MessageFileView{
		Token: row.Token, Name: row.Name, Mime: row.Mime, Size: row.Size,
	}})
}

// GET /website/inquiry/attachments/:token —— 按不可枚举 token 下载附件。
func (h *siteHandler) inquiryAttachment(c *gin.Context) {
	if h.svc.Storage() == nil {
		c.String(http.StatusNotFound, "not found")
		return
	}
	row, err := h.svc.MessageFileByToken(c.Request.Context(), c.Param("token"))
	if err != nil {
		c.String(http.StatusNotFound, "not found")
		return
	}
	obj, err := h.svc.Storage().Get(c.Request.Context(), row.StorageKey, nil)
	if err != nil {
		c.String(http.StatusNotFound, "not found")
		return
	}
	defer func() { _ = obj.Body.Close() }()
	disp := "attachment"
	if strings.HasPrefix(row.Mime, "image/") {
		disp = "inline"
	}
	c.Header("Content-Type", row.Mime)
	c.Header("X-Content-Type-Options", "nosniff")
	c.Header("Content-Security-Policy", "default-src 'none'; sandbox")
	c.Header("Content-Disposition", disp+"; filename*=UTF-8''"+url.PathEscape(row.Name))
	c.Header("Cache-Control", "private, max-age=86400")
	c.DataFromReader(http.StatusOK, obj.Length, row.Mime, obj.Body, nil)
}

// GET /website/inquiry/attachment-limits —— 弹窗打开时取一次生效限制（大小随系统参数变）。
func (h *siteHandler) inquiryAttachmentLimits(c *gin.Context) {
	c.JSON(http.StatusOK, InquiryAttachmentLimitsFor(c.Request.Context()))
}

// ---- 服务层 ----

// SaveMessageFile 登记一个已落存储的附件（message_id = 0，等询盘提交时绑定）。
func (s *Service) SaveMessageFile(ctx context.Context, token, name, mime string, size int64, key string) (*MessageFile, error) {
	row := &MessageFile{Token: token, Name: name, Mime: mime, Size: size, StorageKey: key}
	if err := s.db.WithContext(ctx).Create(row).Error; err != nil {
		return nil, err
	}
	return row, nil
}

// MessageFileByToken 按 token 取附件；未绑定留言的也能取（访客刚传完就要预览）。
func (s *Service) MessageFileByToken(ctx context.Context, token string) (*MessageFile, error) {
	var row MessageFile
	if token == "" {
		return nil, response.NotFound("website.file_not_found")
	}
	if err := s.db.WithContext(ctx).First(&row, "token = ?", token).Error; err != nil {
		return nil, err
	}
	return &row, nil
}

// PurgeStaleMessageFiles 删除超过 age 还没被询盘认领的附件行与存储对象（尽力而为）。
func (s *Service) PurgeStaleMessageFiles(ctx context.Context, age time.Duration) {
	var rows []MessageFile
	deadline := time.Now().Add(-age)
	if err := s.db.WithContext(ctx).
		Where("message_id = 0 AND created_at < ?", deadline).
		Limit(50).Find(&rows).Error; err != nil || len(rows) == 0 {
		return
	}
	ids := make([]int64, 0, len(rows))
	for _, r := range rows {
		ids = append(ids, int64(r.ID))
	}
	if err := s.db.WithContext(ctx).Delete(&MessageFile{}, "id IN ?", ids).Error; err != nil {
		return
	}
	if st := s.Storage(); st != nil {
		for _, r := range rows {
			_ = st.Delete(ctx, r.StorageKey)
		}
	}
}

// bindMessageFilesTx 把 tokens 里的附件绑到 messageID；已被别的留言占用的 token 不再改（先到先得）。
func bindMessageFilesTx(tx *gorm.DB, messageID idgen.ID, tokens []string) error {
	if len(tokens) == 0 {
		return nil
	}
	seen := make(map[string]bool, len(tokens))
	uniq := make([]string, 0, len(tokens))
	for _, t := range tokens {
		t = strings.TrimSpace(t)
		if t == "" || len(t) > 40 || seen[t] || len(uniq) >= maxInquiryFiles {
			continue
		}
		seen[t] = true
		uniq = append(uniq, t)
	}
	if len(uniq) == 0 {
		return nil
	}
	return tx.Model(&MessageFile{}).
		Where("token IN ? AND message_id = 0", uniq).
		Update("message_id", messageID).Error
}

// MessageFilesOf 批量装配附件（列表页一次查完，避免每条留言一次查询）。
func (s *Service) MessageFilesOf(ctx context.Context, ids []idgen.ID) (map[int64][]MessageFileView, error) {
	out := make(map[int64][]MessageFileView, len(ids))
	if len(ids) == 0 {
		return out, nil
	}
	var rows []MessageFile
	if err := s.db.WithContext(ctx).
		Where("message_id IN ?", int64IDs(ids)).
		Order("id ASC").Find(&rows).Error; err != nil {
		return nil, err
	}
	for _, r := range rows {
		k := int64(r.MessageID)
		out[k] = append(out[k], MessageFileView{Token: r.Token, Name: r.Name, Mime: r.Mime, Size: r.Size})
	}
	return out, nil
}

// deleteMessageFiles 删除一批留言的附件行与存储对象（留言被删时调用）。
func (s *Service) deleteMessageFiles(ctx context.Context, ids []idgen.ID) {
	if len(ids) == 0 {
		return
	}
	var rows []MessageFile
	if err := s.db.WithContext(ctx).Where("message_id IN ?", int64IDs(ids)).Find(&rows).Error; err != nil {
		return
	}
	if err := s.db.WithContext(ctx).Delete(&MessageFile{}, "message_id IN ?", int64IDs(ids)).Error; err != nil {
		return
	}
	if st := s.Storage(); st != nil {
		for _, r := range rows {
			_ = st.Delete(ctx, r.StorageKey)
		}
	}
}
