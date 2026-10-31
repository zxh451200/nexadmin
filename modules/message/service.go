package message

import (
	"context"
	"errors"
	"strings"
	"time"

	"gorm.io/gorm"

	"nexadmin/core/response"
	"nexadmin/modules/notice"
	"nexadmin/pkg/idgen"
)

// Service 是消息的业务层。所有方法都带 uid，查询强制限定收件人。
type Service struct {
	db      *gorm.DB
	notices *notice.Service
	loc     *time.Location
}

// NewService 建服务。tz 是站点时区（app.timezone）：用户选的「某一天」按它解释，
// 再换算成 UTC 比较（created_at 落库是 UTC），否则非 UTC 站点会错开几小时。
func NewService(db *gorm.DB, n *notice.Service, tz string) *Service {
	loc, err := time.LoadLocation(tz)
	if err != nil || tz == "" {
		loc = time.UTC
	}
	return &Service{db: db, notices: n, loc: loc}
}

func (s *Service) own(ctx context.Context, uid idgen.ID) *gorm.DB {
	return s.db.WithContext(ctx).Model(&Message{}).Where("user_id = ?", int64(uid))
}

// List 未读优先，其后按时间倒序。
func (s *Service) List(ctx context.Context, uid idgen.ID, q ListQuery) ([]View, int64, error) {
	tx := s.own(ctx, uid)
	if q.Unread {
		tx = tx.Where("read_at IS NULL")
	}
	if q.Category != "" {
		tx = tx.Where("category = ?", q.Category)
	}
	if q.Keyword != "" {
		r := strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`)
		like := "%" + r.Replace(q.Keyword) + "%"
		tx = tx.Where("(title LIKE ? OR content LIKE ?)", like, like)
	}
	if t, err := time.ParseInLocation("2006-01-02", q.From, s.loc); err == nil {
		tx = tx.Where("created_at >= ?", t.UTC())
	}
	if t, err := time.ParseInLocation("2006-01-02", q.To, s.loc); err == nil {
		tx = tx.Where("created_at < ?", t.AddDate(0, 0, 1).UTC())
	}
	tx = tx.Session(&gorm.Session{})
	var total int64
	if err := tx.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var rows []Message
	err := tx.Order("CASE WHEN read_at IS NULL THEN 0 ELSE 1 END").Order("created_at DESC").
		Offset((q.Page - 1) * q.PageSize).Limit(q.PageSize).Find(&rows).Error
	if err != nil {
		return nil, 0, err
	}
	out := make([]View, len(rows))
	for i, m := range rows {
		out[i] = toView(m)
	}
	return out, total, nil
}

// Unread 未读消息数 + 未读公告数。公告统计失败（例如公告模块被停用）按 0 处理，不影响消息。
func (s *Service) Unread(ctx context.Context, uid idgen.ID) (UnreadCount, error) {
	var c UnreadCount
	if err := s.own(ctx, uid).Where("read_at IS NULL").Count(&c.Messages).Error; err != nil {
		return c, err
	}
	if n, err := s.notices.UnreadCount(ctx, uid); err == nil {
		c.Notices = n
	}
	return c, nil
}

// MarkRead 把一条消息标为已读（已读的不变）。
func (s *Service) ValidateMarkRead(ctx context.Context, uid, id idgen.ID) error {
	var m Message
	if err := s.own(ctx, uid).First(&m, "id = ?", int64(id)).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return response.NotFound("error.not_found")
		}
		return err
	}
	return nil
}

func (s *Service) MarkRead(ctx context.Context, uid, id idgen.ID) error {
	var m Message
	if err := s.own(ctx, uid).First(&m, "id = ?", int64(id)).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return response.NotFound("error.not_found")
		}
		return err
	}
	if m.ReadAt != nil {
		return nil
	}
	err := s.own(ctx, uid).Where("id = ? AND read_at IS NULL", int64(id)).Update("read_at", time.Now().UTC()).Error
	if err == nil {
		defaultHub.poke(ctx, uid)
	}
	return err
}

// ReadAll 全部标为已读。
func (s *Service) ReadAll(ctx context.Context, uid idgen.ID) error {
	err := s.own(ctx, uid).Where("read_at IS NULL").Update("read_at", time.Now().UTC()).Error
	if err == nil {
		defaultHub.poke(ctx, uid)
	}
	return err
}

// Delete 删除自己的一条消息。
func (s *Service) Delete(ctx context.Context, uid, id idgen.ID) error {
	res := s.db.WithContext(ctx).Where("user_id = ? AND id = ?", int64(uid), int64(id)).Delete(&Message{})
	if res.Error == nil {
		defaultHub.poke(ctx, uid)
	}
	return res.Error
}

// ClearAll 清空自己的全部消息（含未读）。
func (s *Service) ClearAll(ctx context.Context, uid idgen.ID) error {
	res := s.db.WithContext(ctx).Where("user_id = ?", int64(uid)).Delete(&Message{})
	if res.Error == nil {
		defaultHub.poke(ctx, uid)
	}
	return res.Error
}

// Send 给多个收件人各插入一条消息，并立即通过 SSE 提醒在线的收件人。
// 业务模块发消息时调用（ctx 里要有租户）。
func (s *Service) Send(ctx context.Context, to []idgen.ID, category, title, content, link string) error {
	if len(to) == 0 {
		return nil
	}
	rows := make([]Message, 0, len(to))
	for _, uid := range to {
		rows = append(rows, Message{UserID: uid, Category: category, Title: title, Content: content, Link: link})
	}
	if err := s.db.WithContext(ctx).Create(&rows).Error; err != nil {
		return err
	}
	for _, uid := range to {
		defaultHub.poke(ctx, uid)
	}
	return nil
}
