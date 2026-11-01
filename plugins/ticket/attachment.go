package ticket

// 工单附件：坐席与访客上传，按内容嗅探类型，落框架 storage（local/S3）。
// 下载按「工单号 + 不可枚举 token」双要素定位；内部备注附件对访客一律 404。
// 单张工单有附件条数/总字节配额（见 SetAttachmentQuota）；删除先删对象后删行，
// 对象删不掉就报错留行，重试可收敛，不做假成功。

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
	"strconv"
	"strings"
	"sync/atomic"
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

// 单张工单的附件配额出厂默认：条数与总字节。取值来源选了「插件内常量 + 进程级覆盖钩子」
// 而不是 config 项：与本插件 exportLimit、activityLimit 的惯例一致，零跨文件协调；
// 代价是运维改上限要改代码重启（不能按部署调）。若框架侧加了配置项，装配层调用
// SetAttachmentQuota 注入即可，本文件不再需要改动（取舍与键名建议见交付报告）。
const (
	attachmentQuotaMaxCountDefault = int64(20)
	attachmentQuotaMaxBytesDefault = int64(100 << 20)
)

// 当前生效的配额（进程级，原子读写；<=0 一律回到出厂默认）。
var (
	attachmentQuotaMaxCount atomic.Int64
	attachmentQuotaMaxBytes atomic.Int64
)

func init() {
	attachmentQuotaMaxCount.Store(attachmentQuotaMaxCountDefault)
	attachmentQuotaMaxBytes.Store(attachmentQuotaMaxBytesDefault)
}

// attachmentQuota 返回当前生效的附件配额（个数与总字节），供接口把上限告诉前端，
// 避免「前端硬编一份、后端另一份」这种漂移。
func attachmentQuota() (maxCount, maxBytes int64) {
	return attachmentQuotaMaxCount.Load(), attachmentQuotaMaxBytes.Load()
}

// SetAttachmentQuota 覆盖附件配额（装配层/配置注入与测试用）；maxCount 或 maxBytes <= 0 时该项取出厂默认。
func SetAttachmentQuota(maxCount, maxBytes int64) {
	if maxCount <= 0 {
		maxCount = attachmentQuotaMaxCountDefault
	}
	if maxBytes <= 0 {
		maxBytes = attachmentQuotaMaxBytesDefault
	}
	attachmentQuotaMaxCount.Store(maxCount)
	attachmentQuotaMaxBytes.Store(maxBytes)
}

// 配额超限与对象删除失败的错误契约：key 走 i18n 字典（中英双份待接线），
// Reason 供前端定位是哪一类超限（与 export 的 TICKET_EXPORT_TOO_MANY 同一手法）。
const (
	AttachmentQuotaCountReason = "TICKET_ATTACHMENT_QUOTA_COUNT"
	AttachmentQuotaBytesReason = "TICKET_ATTACHMENT_QUOTA_BYTES"
)

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

// SaveAttachment 登记附件行（文件已落存储）。配额在这里兜底再查一次：
// 上传入口在落存储前先查（少写一个孤儿对象），但并发下预查与插入之间仍有窗口，
// 以插入前的这次检查为准——超了就把错误抛回入口，由入口回滚删掉刚落的对象。
func (s *Service) SaveAttachment(ctx context.Context, ticketNo, uploaderType, uploaderName string, internal bool, name, mime string, size int64, key string) (*Attachment, error) {
	t, err := s.Ticket(ctx, ticketNo)
	if err != nil {
		return nil, err
	}
	if err := s.enforceAttachmentQuota(ctx, t.ID, size); err != nil {
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

// AttachmentUsage 返回工单已登记的附件条数与总字节（内部附件同样计数：配额防的是存储被拖垮，不区分可见性）。
func (s *Service) AttachmentUsage(ctx context.Context, ticketID idgen.ID) (int64, int64, error) {
	var res struct {
		Cnt   int64 `gorm:"column:cnt"`
		Total int64 `gorm:"column:total"`
	}
	err := s.db.WithContext(ctx).Model(&Attachment{}).Where("ticket_id = ?", int64(ticketID)).
		Select("COUNT(*) AS cnt, COALESCE(SUM(size), 0) AS total").Scan(&res).Error
	if err != nil {
		return 0, 0, err
	}
	return res.Cnt, res.Total, nil
}

// attachmentQuotaErr 是纯函数判定：再登记 size 字节是否越过配额。
// 提示里带上当前值与上限（谁超限、超到哪），调用方不用猜。
func attachmentQuotaErr(count, total, size int64) error {
	if max := attachmentQuotaMaxCount.Load(); count >= max {
		e := response.BadRequest("ticket.attachment_quota_count",
			strconv.FormatInt(count, 10), strconv.FormatInt(max, 10))
		e.Reason = AttachmentQuotaCountReason
		return e
	}
	if max := attachmentQuotaMaxBytes.Load(); total+size > max {
		// MB 向上取整：0.5MB 的超限提示成 1MB，避免显示 0MB 让人对不上号。
		need := (total + size + (1 << 20) - 1) >> 20
		e := response.BadRequest("ticket.attachment_quota_bytes",
			strconv.FormatInt(need, 10), strconv.FormatInt(max>>20, 10))
		e.Reason = AttachmentQuotaBytesReason
		return e
	}
	return nil
}

// enforceAttachmentQuota 查当前用量并做配额判定；查询失败原样上抛（宁可拒绝上传也不放行未校验的附件）。
func (s *Service) enforceAttachmentQuota(ctx context.Context, ticketID idgen.ID, size int64) error {
	count, total, err := s.AttachmentUsage(ctx, ticketID)
	if err != nil {
		return err
	}
	return attachmentQuotaErr(count, total, size)
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
	return s.attachmentByTicket(ctx, t, token, includeInternal)
}

// attachmentByTicket 在已定位的工单内按 token 取附件；越权一律按不存在处理，不外泄存在性。
func (s *Service) attachmentByTicket(ctx context.Context, t *Ticket, token string, includeInternal bool) (*Attachment, error) {
	var att Attachment
	err := s.db.WithContext(ctx).Where("token = ? AND ticket_id = ?", strings.TrimSpace(token), int64(t.ID)).Take(&att).Error
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

// DeleteAttachment 删除「工单号 + token」定位的附件，返回被删行与所属工单（入口记动态要用）。
// 视角规则：坐席（visitor=false）可删任意附件，含内部备注附件；客户只能删本工单里报告人侧
// 上传的非内部附件——坐席附件与内部附件对客户一律按不存在处理，不泄露存在性。
// 失败语义：先删存储对象、后删库行。对象删不掉直接报错并保留行（storage.Delete 对
// 「对象不存在」不算错），所以失败后重试能收敛；绝不出现「行没了、对象还在」的假成功。
// 对象已删但行删除失败时同样上抛（500，细节进日志）：行还在，重试即补删。
func (s *Service) DeleteAttachment(ctx context.Context, ticketNo, token string, visitor bool) (*Attachment, *Ticket, error) {
	t, err := s.Ticket(ctx, ticketNo)
	if err != nil {
		return nil, nil, err
	}
	att, err := s.attachmentByTicket(ctx, t, token, !visitor)
	if err != nil {
		return nil, nil, err
	}
	if visitor && att.UploaderType != ReplierReporter {
		return nil, nil, response.NotFound("error.not_found")
	}
	if s.store == nil {
		return nil, nil, response.Unavailable("error.unavailable")
	}
	if err := s.store.Delete(ctx, att.StorageKey); err != nil {
		return nil, nil, response.New(http.StatusInternalServerError, "ticket.attachment_delete_failed").Wrap(err)
	}
	if err := s.db.WithContext(ctx).Where("id = ?", int64(att.ID)).Delete(&Attachment{}).Error; err != nil {
		return nil, nil, err
	}
	return att, t, nil
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
	// 配额预检放在落存储之前：超限直接拒绝，不产生孤儿对象（SaveAttachment 里还会兜底复查一次）。
	if err := h.svc.enforceAttachmentQuota(c.Request.Context(), t.ID, fh.Size); err != nil {
		response.Fail(c, err)
		return
	}
	// 杀毒是内容嗅探之后的第二道闸：没注册扫描器时连文件都不再读一次（见 scanner.go）。
	if currentScanner() != nil {
		sf, err := fh.Open()
		if err != nil {
			response.Fail(c, err)
			return
		}
		data, rerr := io.ReadAll(io.LimitReader(sf, maxUploadBytes))
		_ = sf.Close()
		if rerr != nil || int64(len(data)) != fh.Size {
			response.Fail(c, response.BadRequest("error.bad_request"))
			return
		}
		if serr := scanAttachment(c.Request.Context(), fh.Filename, mime, data); serr != nil {
			h.svc.logWarn("ticket attachment rejected by scanner",
				"scanner", ScannerName(), "file", safeFilename(fh.Filename), "err", serr.Error())
			response.Fail(c, response.BadRequest("ticket.attachment_rejected"))
			return
		}
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
	if err := h.svc.RecordAttachmentAdded(c.Request.Context(), t,
		ActivityActor{Type: uploaderType, Name: uploaderName}, att.Name); err != nil && h.svc.log != nil {
		h.svc.log.Warn("ticket: record attachment activity failed", "ticketNo", t.TicketNo, "err", err.Error())
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

// removeAttachment 删除附件的公共入口：视角差异（坐席/客户）全部由 DeleteAttachment 兜。
// 存储可用性也在服务层判（h.store 与 svc.store 装配时同源，判两处反而多一个说谎的机会）。
func (h *handler) removeAttachment(c *gin.Context, visitor bool) {
	att, t, err := h.svc.DeleteAttachment(c.Request.Context(), c.Param("no"), c.Param("token"), visitor)
	if err != nil {
		response.Fail(c, err)
		return
	}
	actor := ActivityActor{Type: ReplierReporter, Name: "visitor"}
	if !visitor {
		actor.Type = ReplierAgent
		_, actor.Name = claimsOperator(c)
	}
	// 动态删除失败不回滚删除：与上传一致（审计缺口留日志，不让业务变更被旁路表拖死）。
	if err := h.svc.recordActivity(c.Request.Context(), nil, t, actor,
		ActAttachmentRemoved, "", "", att.Name); err != nil && h.svc.log != nil {
		h.svc.log.Warn("ticket: record attachment removal activity failed", "ticketNo", t.TicketNo, "err", err.Error())
	}
	response.OK(c, gin.H{"ok": true})
}

func (h *handler) uploadAdmin(c *gin.Context)     { h.uploadAttachment(c, false) }
func (h *handler) uploadVisitor(c *gin.Context)   { h.uploadAttachment(c, true) }
func (h *handler) downloadAdmin(c *gin.Context)   { h.downloadAttachment(c, false) }
func (h *handler) downloadVisitor(c *gin.Context) { h.downloadAttachment(c, true) }
func (h *handler) deleteAdmin(c *gin.Context)     { h.removeAttachment(c, false) }
func (h *handler) deleteVisitor(c *gin.Context)   { h.removeAttachment(c, true) }
