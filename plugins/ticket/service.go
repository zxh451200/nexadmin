package ticket

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"nexadmin/core/response"
	"nexadmin/pkg/idgen"
)

// Service 是工单业务层。
type Service struct {
	db  *gorm.DB
	log logger
}

// logger 是最小日志面（便于测试注入空实现）。
type logger interface {
	Warn(msg string, keysAndValues ...any)
	Error(msg string, keysAndValues ...any)
}

// NewService 创建工单服务。
func NewService(db *gorm.DB, log logger) *Service { return &Service{db: db, log: log} }

// CreateInput 是创建工单的入参。Reporter 身份由宿主给出：前台客户是 visitor
// （visitorToken/会员 ID），后台用户自建是 user。Content 是首条描述（作为 seq=1 的回复）。
type CreateInput struct {
	BizType      string
	BizNo        string
	Title        string
	Content      string
	Priority     string
	ReporterType string
	ReporterID   string
	ReporterName string
}

// Create 建单：title + content 必填，priority 缺省 normal；首条描述作为 seq=1 的回复。
func (s *Service) Create(ctx context.Context, in CreateInput) (*Ticket, error) {
	in.BizType, in.BizNo = strings.TrimSpace(in.BizType), strings.TrimSpace(in.BizNo)
	in.Title, in.Content = strings.TrimSpace(in.Title), strings.TrimSpace(in.Content)
	in.ReporterType = strings.TrimSpace(in.ReporterType)
	in.ReporterID, in.ReporterName = strings.TrimSpace(in.ReporterID), strings.TrimSpace(in.ReporterName)
	switch {
	case in.Title == "" || in.Content == "":
		return nil, response.BadRequest("ticket.title_content_required")
	case in.ReporterType != ReplierReporter && in.ReporterType != ReplierAgent:
		return nil, response.BadRequest("ticket.reporter_invalid")
	case !ValidPriority(in.Priority):
		in.Priority = PriorityNormal
	}
	if len(in.BizType) > 32 || len(in.BizNo) > 64 {
		return nil, response.BadRequest("error.bad_request")
	}

	id, err := idgen.Next()
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	t := &Ticket{
		TicketNo: "TK" + strconv.FormatInt(int64(id), 10),
		BizType:  in.BizType, BizNo: in.BizNo,
		Title: truncate(in.Title, 200), Status: StatusOpen, Priority: in.Priority,
		ReporterType: in.ReporterType, ReporterID: truncate(in.ReporterID, 64),
		ReporterName: truncate(in.ReporterName, 64),
		ReplySeq:     1, LastReplyAt: &now, LastReplierType: in.ReporterType,
		LastPreview: preview(in.Content),
		DueAt:       ptr(now.Add(SLAFor(in.Priority))),
	}
	t.ID = id
	first := TicketReply{
		TicketID: t.ID, Seq: 1, ReplierType: in.ReporterType,
		ReplierID: truncate(in.ReporterID, 64), ReplierName: truncate(in.ReporterName, 64),
		Content: truncate(in.Content, 2000), CreatedAt: now,
	}
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(t).Error; err != nil {
			return err
		}
		first.ID, err = idgen.Next()
		if err != nil {
			return err
		}
		return tx.Create(&first).Error
	})
	if err != nil {
		return nil, err
	}
	s.publish(ctx, EventCreated, t, EventData{
		SenderType: in.ReporterType, SenderName: in.ReporterName, Seq: 1, Preview: preview(in.Content),
	})
	return t, nil
}

// ReplyInput 是回复工单的入参。
type ReplyInput struct {
	ReplierType string
	ReplierID   string
	ReplierName string
	Content     string
}

// Reply 回帖：行锁下取序号；坐席回复把 open/resolved 推到 processing（resolved 再回复
// 视为重开），并补首次响应时间与默认认领；报告人回复把 resolved/closed 重开为 processing。
func (s *Service) Reply(ctx context.Context, ticketNo string, in ReplyInput) (*TicketReply, *Ticket, error) {
	ticketNo = strings.TrimSpace(ticketNo)
	in.Content = strings.TrimSpace(in.Content)
	switch {
	case ticketNo == "":
		return nil, nil, response.BadRequest("error.bad_request")
	case in.ReplierType != ReplierReporter && in.ReplierType != ReplierAgent && in.ReplierType != ReplierSystem:
		return nil, nil, response.BadRequest("ticket.replier_invalid")
	case in.Content == "":
		return nil, nil, response.BadRequest("ticket.content_required")
	}
	in.Content = truncate(in.Content, 2000)
	in.ReplierName = truncate(strings.TrimSpace(in.ReplierName), 64)

	var out TicketReply
	var t Ticket
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("ticket_no = ?", ticketNo).Take(&t).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return response.NotFound("ticket.not_found")
			}
			return err
		}
		id, err := idgen.Next()
		if err != nil {
			return err
		}
		out = TicketReply{
			TicketID: t.ID, Seq: t.ReplySeq + 1, ReplierType: in.ReplierType,
			ReplierID: truncate(in.ReplierID, 64), ReplierName: in.ReplierName,
			Content: in.Content, CreatedAt: time.Now().UTC(),
		}
		out.ID = id
		if err := tx.Create(&out).Error; err != nil {
			return err
		}

		updates := map[string]any{
			"reply_seq": out.Seq, "last_reply_at": out.CreatedAt,
			"last_replier_type": in.ReplierType, "last_preview": preview(in.Content),
		}
		if in.ReplierType == ReplierAgent {
			if t.FirstReplyAt == nil {
				updates["first_reply_at"] = out.CreatedAt
			}
			if t.AssigneeID == 0 {
				if uid, err := strconv.ParseInt(in.ReplierID, 10, 64); err == nil && uid != 0 {
					updates["assignee_id"] = uid
					updates["assignee_name"] = in.ReplierName
				}
			}
			if t.Status == StatusOpen || t.Status == StatusResolved {
				updates["status"] = StatusProcessing
			}
			// 坐席回复重置 SLA 时钟：等待报告人的钟重新起算，并清掉超时标记。
			updates["due_at"] = out.CreatedAt.Add(SLAFor(t.Priority))
			updates["overdue"] = false
		} else if in.ReplierType == ReplierReporter {
			if t.Status == StatusResolved || t.Status == StatusClosed {
				updates["status"] = StatusProcessing // 报告人再回复自动重开
			}
		}
		if err := tx.Model(&Ticket{}).Where("id = ?", int64(t.ID)).Updates(updates).Error; err != nil {
			return err
		}
		// 刷新响应结构体：认领/首次响应/SLA 重置都要反映在返回值里。
		var fresh Ticket
		if err := tx.Where("id = ?", int64(t.ID)).Take(&fresh).Error; err == nil {
			t = fresh
		} else {
			t.ReplySeq = out.Seq
		}
		return nil
	})
	if err != nil {
		return nil, nil, err
	}
	s.publish(ctx, EventReplied, &t, EventData{
		SenderType: in.ReplierType, SenderName: in.ReplierName, Seq: out.Seq, Preview: preview(in.Content),
	})
	return &out, &t, nil
}

// validTransition 校验坐席侧状态迁移；报告人重开走 Reply 的自动重开，不在这里。
func validTransition(from, to string) bool {
	switch to {
	case StatusProcessing:
		return from == StatusOpen
	case StatusResolved:
		return from == StatusOpen || from == StatusProcessing
	case StatusClosed:
		return from != StatusClosed // 任何未关闭状态都可以直接关闭
	}
	return false
}

// SetStatus 坐席改状态：认领处理（open→processing）、标记解决、关闭。
func (s *Service) SetStatus(ctx context.Context, ticketNo, to, operator string) (*Ticket, error) {
	ticketNo, operator = strings.TrimSpace(ticketNo), truncate(strings.TrimSpace(operator), 64)
	if to != StatusProcessing && to != StatusResolved && to != StatusClosed {
		return nil, response.BadRequest("ticket.status_invalid")
	}
	var t Ticket
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("ticket_no = ?", ticketNo).Take(&t).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return response.NotFound("ticket.not_found")
			}
			return err
		}
		if !validTransition(t.Status, to) {
			return response.Conflict("ticket.illegal_transition")
		}
		if err := tx.Model(&Ticket{}).Where("id = ?", int64(t.ID)).Update("status", to).Error; err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	t.Status = to
	switch to {
	case StatusResolved:
		s.publish(ctx, EventResolved, &t, EventData{SenderType: ReplierAgent, SenderName: operator})
	case StatusClosed:
		s.publish(ctx, EventClosed, &t, EventData{SenderType: ReplierAgent, SenderName: operator})
	}
	return &t, nil
}

// Assign 认领/改派工单（不改状态；认领后首次回复会顺手置 processing）。
func (s *Service) Assign(ctx context.Context, ticketNo string, assigneeID idgen.ID, assigneeName string) (*Ticket, error) {
	if assigneeID == 0 {
		return nil, response.BadRequest("ticket.assignee_invalid")
	}
	var t Ticket
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("ticket_no = ?", strings.TrimSpace(ticketNo)).Take(&t).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return response.NotFound("ticket.not_found")
			}
			return err
		}
		if err := tx.Model(&Ticket{}).Where("id = ?", int64(t.ID)).Updates(map[string]any{
			"assignee_id": assigneeID, "assignee_name": truncate(strings.TrimSpace(assigneeName), 64),
		}).Error; err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	t.AssigneeID, t.AssigneeName = assigneeID, truncate(strings.TrimSpace(assigneeName), 64)
	return &t, nil
}

// TicketQuery 是坐席工单列表过滤条件。
type TicketQuery struct {
	Status   string
	Priority string
	BizType  string
	Keyword  string // 工单号 / 标题 / 摘要模糊
	Overdue  string // "1" = 只看已超时
	Page     int
	PageSize int
}

// ListTickets 坐席列表按最后回复时间倒序。
func (s *Service) ListTickets(ctx context.Context, q TicketQuery) ([]Ticket, int64, error) {
	db := s.db.WithContext(ctx).Model(&Ticket{})
	if q.Status != "" {
		db = db.Where("status = ?", q.Status)
	}
	if q.Priority != "" {
		db = db.Where("priority = ?", q.Priority)
	}
	if q.BizType != "" {
		db = db.Where("biz_type = ?", q.BizType)
	}
	if q.Overdue == "1" {
		db = db.Where("overdue = ?", true)
	}
	if kw := strings.TrimSpace(q.Keyword); kw != "" {
		like := "%" + kw + "%"
		db = db.Where("ticket_no LIKE ? OR title LIKE ? OR last_preview LIKE ?", like, like, like)
	}
	var total int64
	if err := db.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var rows []Ticket
	if err := db.Order("COALESCE(last_reply_at, created_at) DESC").
		Offset((q.Page - 1) * q.PageSize).Limit(q.PageSize).Find(&rows).Error; err != nil {
		return nil, 0, err
	}
	return rows, total, nil
}

// MarkOverdue 把「未完结且已过响应时限」的工单标记为超时，返回本次标记笔数。
// 只做标记不改状态；由 plugin.Starter 的清扫器每分钟调用。
func (s *Service) MarkOverdue(ctx context.Context, now time.Time) (int64, error) {
	res := s.db.WithContext(ctx).Model(&Ticket{}).
		Where("overdue = ? AND status IN ? AND due_at IS NOT NULL AND due_at < ?",
			false, []string{StatusOpen, StatusProcessing}, now).
		Update("overdue", true)
	return res.RowsAffected, res.Error
}

// Replies 增量拉取回复：seq > since，按序号升序，默认 50 条封顶 200。
func (s *Service) Replies(ctx context.Context, ticketNo string, sinceSeq, limit int) (*Ticket, []TicketReply, error) {
	var t Ticket
	if err := s.db.WithContext(ctx).Where("ticket_no = ?", strings.TrimSpace(ticketNo)).Take(&t).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil, response.NotFound("ticket.not_found")
		}
		return nil, nil, err
	}
	if limit <= 0 {
		limit = 50
	}
	if limit > 200 {
		limit = 200
	}
	var rows []TicketReply
	q := s.db.WithContext(ctx).Where("ticket_id = ?", int64(t.ID))
	if sinceSeq > 0 {
		q = q.Where("seq > ?", sinceSeq)
	}
	if err := q.Order("seq ASC").Limit(limit).Find(&rows).Error; err != nil {
		return nil, nil, err
	}
	return &t, rows, nil
}

func truncate(s string, n int) string {
	if len(s) > n {
		return s[:n]
	}
	return s
}

func ptr[T any](v T) *T { return &v }
