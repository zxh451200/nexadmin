package ticket

// 工单附件：坐席与访客上传，按内容嗅探类型，落框架 storage（local/S3）。
// 下载按「工单号 + 不可枚举 token」双要素定位；内部备注附件对访客一律 404。

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"io"
	"net/http"
	"net/url"
	"path"
	"strings"
	"time"
	"unicode"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"nexadmin/core/database"
	"nexadmin/core/response"
	"nexadmin/core/storage"
	"nexadmin/pkg/idgen"
)

// maxUploadBytes 是附件请求体的硬上限（20MB），更细的类型/大小限制见 attachmentRuleFor。
const maxUploadBytes = 20 << 20

// Attachment 是工单附件；internal 表示挂在内部备注下，访客不可见。
type Attachment struct {
	database.PlatformModel
	Token        string   `gorm:"size:40;not null;uniqueIndex:uk_plg_ticket_attachment_token" json:"token"`
	TicketID     idgen.ID `gorm:"not null" json:"-"`
	UploaderType string   `gorm:"size:16;not null" json:"uploaderType"`
	UploaderName string   `gorm:"size:64;not null;default:''" json:"uploaderName"`
	Internal     bool     `gorm:"not null;default:0" json:"internal"`
	Name         string   `gorm:"size:255;not null" json:"name"`
	Mime         string   `gorm:"size:128;not null" json:"mime"`
	Size         int64    `gorm:"not null" json:"size"`
	StorageKey   string   `gorm:"size:255;not null" json:"-"`
}

func (Attachment) TableName() string { return "plg_ticket_attachment" }

// SetStorage 注入附件存储；未配置时附件接口返回 503。
func (s *Service) SetStorage(st storage.Storage) { s.store = st }

// Storage 返回附件存储（测试与插件装配用）。
func (s *Service) Storage() storage.Storage { return s.store }

// Ticket 按工单号取工单。
func (s *Service) Ticket(ctx context.Context, ticketNo string) (*Ticket, error) {
	var t Ticket
	if err := s.db.WithContext(ctx).Where("ticket_no = ?", strings.TrimSpace(ticketNo)).Take(&t).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, response.NotFound("ticket.not_found")
		}
		return nil, err
	}
	return &t, nil
}

// SaveAttachment 登记附件行（文件已落存储）。
func (s *Service) SaveAttachment(ctx context.Context, ticketNo, uploaderType, uploaderName string, internal bool, name, mime string, size int64, key string) (*Attachment, error) {
	t, err := s.Ticket(ctx, ticketNo)
	if err != nil {
		return nil, err
	}
	id, err := idgen.Next()
	if err != nil {
		return nil, err
	}
	att := &Attachment{
		Token: randomToken(16), TicketID: t.ID, UploaderType: uploaderType,
		UploaderName: truncate(strings.TrimSpace(uploaderName), 64), Internal: internal,
		Name: truncate(strings.TrimSpace(name), 255), Mime: truncate(mime, 128), Size: size, StorageKey: key,
	}
	att.ID = id
	if err := s.db.WithContext(ctx).Create(att).Error; err != nil {
		return nil, err
	}
	return att, nil
}

// ListAttachments 按上传顺序返回工单附件；访客视角过滤内部附件。
func (s *Service) ListAttachments(ctx context.Context, ticketNo string, includeInternal bool) ([]Attachment, error) {
	t, err := s.Ticket(ctx, ticketNo)
	if err != nil {
		return nil, err
	}
	rows := make([]Attachment, 0, 8)
	q := s.db.WithContext(ctx).Where("ticket_id = ?", int64(t.ID))
	if !includeInternal {
		q = q.Where("internal = ?", false)
	}
	if err := q.Order("id ASC").Find(&rows).Error; err != nil {
		return nil, err
	}
	return rows, nil
}

// AttachmentByToken 按工单号 + token 取附件；内部附件在访客视角按不存在处理。
func (s *Service) AttachmentByToken(ctx context.Context, ticketNo, token string, includeInternal bool) (*Attachment, error) {
	t, err := s.Ticket(ctx, ticketNo)
	if err != nil {
		return nil, err
	}
	var att Attachment
	err = s.db.WithContext(ctx).Where("token = ? AND ticket_id = ?", strings.TrimSpace(token), int64(t.ID)).Take(&att).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, response.NotFound("error.not_found")
		}
		return nil, err
	}
	if att.Internal && !includeInternal {
		return nil, response.NotFound("error.not_found")
	}
	return &att, nil
}

// attachmentRule 描述某类可上传文件的落盘扩展名与大小上限。
type attachmentRule struct {
	ext string
	max int64
}

// attachmentRuleFor 按服务端探测到的 MIME（必要时结合原始文件名）决定是否接收。
// MIME 一律由内容嗅探得出，不信任客户端声明的 Content-Type 与扩展名。
func attachmentRuleFor(mime, filename string) (attachmentRule, bool) {
	switch strings.ToLower(mime) {
	case "image/png":
		return attachmentRule{".png", 8 << 20}, true
	case "image/jpeg":
		return attachmentRule{".jpg", 8 << 20}, true
	case "image/gif":
		return attachmentRule{".gif", 8 << 20}, true
	case "image/webp":
		return attachmentRule{".webp", 8 << 20}, true
	case "application/pdf":
		return attachmentRule{".pdf", maxUploadBytes}, true
	case "text/plain":
		return attachmentRule{".txt", 2 << 20}, true
	case "application/zip":
		// Office 文档本质是 zip，按原始扩展名保留可读的扩展名。
		switch strings.ToLower(path.Ext(filename)) {
		case ".docx":
			return attachmentRule{".docx", maxUploadBytes}, true
		case ".xlsx":
			return attachmentRule{".xlsx", maxUploadBytes}, true
		case ".pptx":
			return attachmentRule{".pptx", maxUploadBytes}, true
		}
		return attachmentRule{".zip", maxUploadBytes}, true
	}
	return attachmentRule{}, false
}

// safeFilename 去掉路径与控制字符，只保留展示用文件名。
func safeFilename(name string) string {
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
	return truncate(out, 120)
}

func randomToken(n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return strings.ReplaceAll(time.Now().UTC().Format("20060102150405.000000000"), ".", "")
	}
	return hex.EncodeToString(b)
}

// uploadAttachment 读取 multipart 文件、按内容嗅探类型、落存储并登记附件。
func (h *handler) uploadAttachment(c *gin.Context, visitor bool) {
	if h.store == nil {
		response.Fail(c, response.Unavailable("error.unavailable"))
		return
	}
	t, err := h.svc.Ticket(c.Request.Context(), c.Param("no"))
	if err != nil {
		response.Fail(c, err)
		return
	}
	if visitor && t.Status == StatusClosed {
		response.Fail(c, response.Conflict("ticket.illegal_transition"))
		return
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, maxUploadBytes)
	fh, err := c.FormFile("file")
	if err != nil {
		response.Fail(c, response.BadRequest("error.bad_request"))
		return
	}
	if fh.Size <= 0 || fh.Size > maxUploadBytes {
		response.Fail(c, response.BadRequest("error.bad_request"))
		return
	}
	f, err := fh.Open()
	if err != nil {
		response.Fail(c, err)
		return
	}
	defer func() { _ = f.Close() }()
	head := make([]byte, 512)
	n, _ := io.ReadFull(f, head)
	mime := strings.TrimSpace(strings.Split(http.DetectContentType(head[:n]), ";")[0])
	rule, ok := attachmentRuleFor(mime, fh.Filename)
	if !ok || fh.Size > rule.max {
		response.Fail(c, response.BadRequest("error.bad_request"))
		return
	}
	uploaderType, uploaderName := ReplierReporter, "visitor"
	internal := false
	if !visitor {
		uploaderType = ReplierAgent
		_, uploaderName = claimsOperator(c)
		internal = c.PostForm("internal") == "1"
	}
	token := randomToken(16)
	key := time.Now().UTC().Format("200601") + "/" + token + rule.ext
	body := io.MultiReader(bytes.NewReader(head[:n]), f)
	if err := h.store.Put(c.Request.Context(), key, body, fh.Size, storage.PutOptions{ContentType: mime}); err != nil {
		response.Fail(c, err)
		return
	}
	att, err := h.svc.SaveAttachment(c.Request.Context(), t.TicketNo, uploaderType, uploaderName, internal,
		safeFilename(fh.Filename), mime, fh.Size, key)
	if err != nil {
		_ = h.store.Delete(c.Request.Context(), key)
		response.Fail(c, err)
		return
	}
	response.OK(c, att)
}

// downloadAttachment 内联图片、其余作为附件下载；访客不能取内部附件。
func (h *handler) downloadAttachment(c *gin.Context, visitor bool) {
	if h.store == nil {
		response.Fail(c, response.NotFound("error.not_found"))
		return
	}
	att, err := h.svc.AttachmentByToken(c.Request.Context(), c.Param("no"), c.Param("token"), !visitor)
	if err != nil {
		response.Fail(c, err)
		return
	}
	obj, err := h.store.Get(c.Request.Context(), att.StorageKey, nil)
	if err != nil {
		response.Fail(c, response.NotFound("error.not_found"))
		return
	}
	defer func() { _ = obj.Body.Close() }()
	disp := "attachment"
	if strings.HasPrefix(att.Mime, "image/") {
		disp = "inline"
	}
	c.Header("Content-Type", att.Mime)
	c.Header("X-Content-Type-Options", "nosniff")
	c.Header("Content-Security-Policy", "default-src 'none'; sandbox")
	c.Header("Content-Disposition", disp+"; filename*=UTF-8''"+url.PathEscape(att.Name))
	c.Header("Cache-Control", "private, max-age=86400")
	c.DataFromReader(http.StatusOK, obj.Length, att.Mime, obj.Body, nil)
}

func (h *handler) uploadAdmin(c *gin.Context)     { h.uploadAttachment(c, false) }
func (h *handler) uploadVisitor(c *gin.Context)   { h.uploadAttachment(c, true) }
func (h *handler) downloadAdmin(c *gin.Context)   { h.downloadAttachment(c, false) }
func (h *handler) downloadVisitor(c *gin.Context) { h.downloadAttachment(c, true) }
