package ticket

import (
	"context"
	"errors"
	"net/url"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"nexadmin/core/notify"
	"nexadmin/core/response"
	"nexadmin/core/storage"
	"nexadmin/core/tenant"
	"nexadmin/pkg/idgen"
)

// Service 是工单业务层。
type Service struct {
	db  *gorm.DB
	log logger
	// loc 是应用时区：统计按天分桶要用它把 UTC 时间换算成用户看到的自然日。
	loc *time.Location
	// notifier 是站内信入口（nil 安全，测试夹具不注入）；lang 取应用默认语言。
	notifier msgSender
	lang     string
	// sla 是进程内缓存的 SLA 策略（插件启动时载入，保存时刷新）。
	sla atomic.Pointer[SLASetting]
	// store 是附件存储（local/S3）；未配置时为 nil，附件接口返回服务不可用。
	store storage.Storage
}

// msgSender 是 core/notify 的最小面（测试注入假实现）。
type msgSender interface {
	Send(ctx context.Context, m notify.Msg) error
}

// notifyCategory 对应字典 message_category 的 business 项：工单提醒属于业务消息。
const notifyCategory = "business"

// WithNotify 注入通知发送器与默认语言；不注入时只做业务、不发站内信。
func (s *Service) WithNotify(n msgSender, lang string) *Service {
	s.notifier, s.lang = n, lang
	return s
}

// WithTimezone 设置应用时区（非法值退回 UTC）。
func (s *Service) WithTimezone(tz string) *Service {
	loc, err := time.LoadLocation(tz)
	if err != nil || loc == nil {
		loc = time.UTC
	}
	s.loc = loc
	return s
}

// logger 是最小日志面（便于测试注入空实现）。
type logger interface {
	Warn(msg string, keysAndValues ...any)
	Error(msg string, keysAndValues ...any)
}

// NewService 创建工单服务；SLA 缓存先落出厂策略，插件启动后再从库里覆盖。
func NewService(db *gorm.DB, log logger) *Service {
	s := &Service{db: db, log: log}
	def := DefaultSLA()
	s.sla.Store(&def)
	return s
}

func (s *Service) location() *time.Location {
	if s.loc != nil {
		return s.loc
	}
	return time.UTC
}

// CreateInput 是创建工单的入参。Reporter 身份由宿主给出：前台客户是 visitor
// （visitorToken/会员 ID），后台用户自建是 user。Content 是首条描述（作为 seq=1 的回复）。
type CreateInput struct {
	BizType      string `json:"bizType"`
	BizNo        string `json:"bizNo"`
	Title        string `json:"title"`
	Content      string `json:"content"`
	Priority     string `json:"priority"`
	ReporterType string `json:"reporterType"`
	ReporterID   string `json:"reporterId"`
	ReporterName string `json:"reporterName"`
	// CreatorID/CreatorName 由后台代客建单的 handler 从登录态填充，不接受前端传入。
	CreatorID   idgen.ID `json:"-"`
	CreatorName string   `json:"-"`
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
	sla := s.CurrentSLA()
	target := sla.TargetFor(in.Priority)
	due := businessDue(now, target.FirstMinutes, sla, s.location())
	resolutionDue := businessDue(now, target.ResolutionMinutes, sla, s.location())
	t := &Ticket{
		TicketNo: "TK" + strconv.FormatInt(int64(id), 10),
		BizType:  in.BizType, BizNo: in.BizNo,
		Title: truncate(in.Title, 200), Status: StatusOpen, Priority: in.Priority,
		ReporterType: in.ReporterType, ReporterID: truncate(in.ReporterID, 64),
		ReporterName: truncate(in.ReporterName, 64),
		CreatorID:    in.CreatorID, CreatorName: truncate(in.CreatorName, 64),
		ReplySeq: 1, LastReplyAt: &now, LastReplierType: in.ReporterType,
		LastPreview: preview(in.Content),
		DueAt:       &due, ResolutionDueAt: &resolutionDue,
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
	// 自动化规则先跑；规则没分到组时再走默认自动分派；最后发事件与新单提醒。
	s.applyRules(ctx, t)
	s.autoAssignGroup(ctx, t)
	s.publish(ctx, EventCreated, t, EventData{
		SenderType: in.ReporterType, SenderName: in.ReporterName, Seq: 1, Preview: preview(in.Content),
	})
	s.notifyNewTicket(ctx, t)
	return t, nil
}

// ReplyInput 是回复工单的入参。Internal 为内部备注：只有坐席能写，访客接口不可见。
type ReplyInput struct {
	ReplierType string
	ReplierID   string
	ReplierName string
	Content     string
	Internal    bool
}

// Reply 回帖：行锁下取序号；坐席公开回复把工单推到 pending（等客户），并暂停 SLA 时钟；
// 内部备注只落回复、不改状态也不动 SLA。报告人回复把 pending 恢复为 processing（补回暂停时长），
// resolved/closed 重开为 processing（重新起算时钟）。
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
	case in.Internal && in.ReplierType == ReplierReporter:
		return nil, nil, response.BadRequest("error.forbidden")
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
		now := time.Now().UTC()
		out = TicketReply{
			TicketID: t.ID, Seq: t.ReplySeq + 1, ReplierType: in.ReplierType,
			ReplierID: truncate(in.ReplierID, 64), ReplierName: in.ReplierName,
			Content: in.Content, Internal: in.Internal, CreatedAt: now,
		}
		out.ID = id
		if err := tx.Create(&out).Error; err != nil {
			return err
		}

		updates := map[string]any{
			"reply_seq": out.Seq, "last_reply_at": now,
			"last_replier_type": in.ReplierType,
		}
		// 内部备注不改对外摘要：客户看到的最后一条仍是公开回复。
		if !in.Internal {
			updates["last_preview"] = preview(in.Content)
		}
		sla := s.CurrentSLA()
		loc := s.location()
		if in.ReplierType == ReplierAgent && !in.Internal {
			if t.FirstReplyAt == nil {
				updates["first_reply_at"] = now
			}
			if t.AssigneeID == 0 {
				if uid, err := strconv.ParseInt(in.ReplierID, 10, 64); err == nil && uid != 0 {
					updates["assignee_id"] = uid
					updates["assignee_name"] = in.ReplierName
				}
			}
			// 已回复即等客户回应：进入 pending 并停表（到期时间不动，记暂停起点）。
			target := sla.TargetFor(t.Priority)
			updates["status"] = StatusPending
			updates["resolved_at"] = nil
			updates["closed_at"] = nil
			if t.SlaPausedAt == nil {
				updates["sla_paused_at"] = now
			}
			if t.DueAt == nil || t.Status == StatusResolved || t.Status == StatusClosed {
				// 解决/关闭后由坐席重新回复：视为新一轮处理，两个时钟都重新起算。
				updates["due_at"] = businessDue(now, target.FirstMinutes, sla, loc)
				updates["overdue"] = false
				updates["resolution_due_at"] = businessDue(now, target.ResolutionMinutes, sla, loc)
				updates["resolution_overdue"] = false
				updates["rating"] = nil
				updates["rating_comment"] = ""
				updates["rated_at"] = nil
			} else if t.ResolutionDueAt == nil {
				updates["resolution_due_at"] = businessDue(now, target.ResolutionMinutes, sla, loc)
			}
		} else if in.ReplierType == ReplierReporter {
			switch t.Status {
			case StatusPending:
				updates["status"] = StatusProcessing
				updates["sla_paused_at"] = nil
				if d := resumeDueAt(t.DueAt, t.SlaPausedAt, now); d != nil {
					updates["due_at"] = d
				}
				if d := resumeDueAt(t.ResolutionDueAt, t.SlaPausedAt, now); d != nil {
					updates["resolution_due_at"] = d
				}
			case StatusResolved, StatusClosed:
				target := sla.TargetFor(t.Priority)
				updates["status"] = StatusProcessing
				updates["resolved_at"] = nil
				updates["closed_at"] = nil
				updates["sla_paused_at"] = nil
				updates["overdue"] = false
				updates["resolution_overdue"] = false
				updates["rating"] = nil
				updates["rating_comment"] = ""
				updates["rated_at"] = nil
				updates["due_at"] = businessDue(now, target.FirstMinutes, sla, loc)
				updates["resolution_due_at"] = businessDue(now, target.ResolutionMinutes, sla, loc)
			}
		}
		if err := tx.Model(&Ticket{}).Where("id = ?", int64(t.ID)).Updates(updates).Error; err != nil {
			return err
		}
		// 刷新响应结构体：认领/首次响应/SLA/暂停都要反映在返回值里。
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
	if in.ReplierType == ReplierReporter {
		s.notifyReply(ctx, &t)
	}
	return &out, &t, nil
}

// validTransition 校验坐席侧状态迁移；报告人重开走 Reply 的自动重开，不在这里。
func validTransition(from, to string) bool {
	switch to {
	case StatusProcessing:
		return from == StatusOpen || from == StatusPending || from == StatusResolved
	case StatusPending:
		return from == StatusOpen || from == StatusProcessing
	case StatusResolved:
		return from == StatusOpen || from == StatusProcessing || from == StatusPending
	case StatusClosed:
		return from != StatusClosed // 任何未关闭状态都可以直接关闭
	}
	return false
}

// SetStatus 坐席改状态：开始处理、待客户回复（停表）、标记解决、关闭，或从解决态重新打开。
func (s *Service) SetStatus(ctx context.Context, ticketNo, to, operator string) (*Ticket, error) {
	ticketNo, operator = strings.TrimSpace(ticketNo), truncate(strings.TrimSpace(operator), 64)
	if to != StatusProcessing && to != StatusPending && to != StatusResolved && to != StatusClosed {
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
		now := time.Now().UTC()
		sla := s.CurrentSLA()
		loc := s.location()
		target := sla.TargetFor(t.Priority)
		updates := map[string]any{"status": to}
		switch to {
		case StatusPending:
			if t.SlaPausedAt == nil {
				updates["sla_paused_at"] = now
			}
			if t.DueAt == nil {
				updates["due_at"] = businessDue(now, target.FirstMinutes, sla, loc)
			}
			if t.ResolutionDueAt == nil {
				updates["resolution_due_at"] = businessDue(now, target.ResolutionMinutes, sla, loc)
			}
		case StatusProcessing:
			switch t.Status {
			case StatusPending:
				updates["sla_paused_at"] = nil
				if d := resumeDueAt(t.DueAt, t.SlaPausedAt, now); d != nil {
					updates["due_at"] = d
				}
				if d := resumeDueAt(t.ResolutionDueAt, t.SlaPausedAt, now); d != nil {
					updates["resolution_due_at"] = d
				}
			case StatusResolved, StatusClosed:
				updates["resolved_at"] = nil
				updates["closed_at"] = nil
				updates["sla_paused_at"] = nil
				updates["overdue"] = false
				updates["resolution_overdue"] = false
				updates["rating"] = nil
				updates["rating_comment"] = ""
				updates["rated_at"] = nil
				updates["due_at"] = businessDue(now, target.FirstMinutes, sla, loc)
				updates["resolution_due_at"] = businessDue(now, target.ResolutionMinutes, sla, loc)
			}
		case StatusResolved:
			updates["resolved_at"] = now
			updates["sla_paused_at"] = nil
			if resolutionBreached(t.ResolutionDueAt, t.SlaPausedAt, now) {
				updates["resolution_overdue"] = true
			}
		case StatusClosed:
			updates["closed_at"] = now
			updates["sla_paused_at"] = nil
			if resolutionBreached(t.ResolutionDueAt, t.SlaPausedAt, now) {
				updates["resolution_overdue"] = true
			}
		}
		if err := tx.Model(&Ticket{}).Where("id = ?", int64(t.ID)).Updates(updates).Error; err != nil {
			return err
		}
		return tx.Where("id = ?", int64(t.ID)).Take(&t).Error
	})
	if err != nil {
		return nil, err
	}
	switch to {
	case StatusResolved:
		s.publish(ctx, EventResolved, &t, EventData{SenderType: ReplierAgent, SenderName: operator})
	case StatusClosed:
		s.publish(ctx, EventClosed, &t, EventData{SenderType: ReplierAgent, SenderName: operator})
	}
	return &t, nil
}

// RateInput 是报告人的满意度评价入参（1~5 分 + 可选文字）。
type RateInput struct {
	Score   int    `json:"score"`
	Comment string `json:"comment"`
}

// Rate 记录报告人对已解决/已关闭工单的满意度。每张工单只评价一次；
// 访客身份由工单号充当能力令牌（与查询/回复一致），不额外做登录。
func (s *Service) Rate(ctx context.Context, ticketNo string, in RateInput) (*Ticket, error) {
	ticketNo = strings.TrimSpace(ticketNo)
	in.Comment = truncate(strings.TrimSpace(in.Comment), 500)
	if ticketNo == "" || in.Score < 1 || in.Score > 5 {
		return nil, response.BadRequest("error.bad_request")
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
		if t.Status != StatusResolved && t.Status != StatusClosed {
			return response.Conflict("ticket.illegal_transition")
		}
		if t.Rating != nil {
			return response.Conflict("ticket.illegal_transition")
		}
		now := time.Now().UTC()
		if err := tx.Model(&Ticket{}).Where("id = ?", int64(t.ID)).Updates(map[string]any{
			"rating": in.Score, "rating_comment": in.Comment, "rated_at": now,
		}).Error; err != nil {
			return err
		}
		return tx.Where("id = ?", int64(t.ID)).Take(&t).Error
	})
	if err != nil {
		return nil, err
	}
	s.publish(ctx, EventRated, &t, EventData{SenderType: ReplierReporter, SenderName: t.ReporterName, Rating: in.Score})
	return &t, nil
}

// Assign 认领/改派工单（不改状态），并把结果通知给新负责人（转派给自己时不提醒）。
func (s *Service) Assign(ctx context.Context, ticketNo string, assigneeID idgen.ID, assigneeName string, actorID idgen.ID) (*Ticket, error) {
	t, err := s.assign(ctx, ticketNo, assigneeID, assigneeName)
	if err != nil {
		return nil, err
	}
	if assigneeID != actorID {
		s.notifyAssign(ctx, t)
	}
	return t, nil
}

// assign 只落库不改状态；批量转派复用且由调用方合并成一条通知。
func (s *Service) assign(ctx context.Context, ticketNo string, assigneeID idgen.ID, assigneeName string) (*Ticket, error) {
	if assigneeID == 0 {
		return nil, response.BadRequest("ticket.assignee_invalid")
	}
	assigneeName = truncate(strings.TrimSpace(assigneeName), 64)
	if assigneeName == "" {
		assigneeName = s.lookupUserName(ctx, assigneeID)
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
			"assignee_id": assigneeID, "assignee_name": assigneeName,
		}).Error; err != nil {
			return err
		}
		t.AssigneeID, t.AssigneeName = assigneeID, assigneeName
		return nil
	})
	if err != nil {
		return nil, err
	}
	return &t, nil
}

// TicketQuery 是坐席工单列表过滤条件。
type TicketQuery struct {
	Status   string
	Priority string
	BizType  string
	Keyword  string // 工单号 / 标题 / 摘要模糊
	Overdue  string // "1" = 只看已超时
	// AssigneeID > 0 只看该负责人；Unassigned 只看未分配（优先于 AssigneeID）。
	AssigneeID idgen.ID
	Unassigned bool
	GroupID    idgen.ID
	Tag        string
	Page       int
	PageSize   int
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
	if q.Unassigned {
		db = db.Where("assignee_id = ?", 0)
	} else if q.AssigneeID != 0 {
		db = db.Where("assignee_id = ?", int64(q.AssigneeID))
	}
	if q.GroupID != 0 {
		db = db.Where("group_id = ?", int64(q.GroupID))
	}
	if tag := strings.TrimSpace(q.Tag); tag != "" {
		db = db.Where("tags LIKE ?", "%,"+tag+",%")
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

const overdueSweepLimit = 500

// MarkOverdue 把「未完结且已过响应时限」的工单标记为超时，返回本次标记笔数。
// pending 已停表，不在待响应集合里；只做标记不改状态；由 plugin.Starter 的清扫器每分钟调用。
// 先取候选再按 id 更新，保证「刚变成超时」的工单能给负责人发一次提醒（标记后不会重复提醒）。
func (s *Service) MarkOverdue(ctx context.Context, now time.Time) (int64, error) {
	var total int64
	respRows, err := s.findOverdueCandidates(ctx, false, now)
	if err != nil {
		return 0, err
	}
	if len(respRows) > 0 {
		res := s.db.WithContext(ctx).Model(&Ticket{}).Where("id IN ?", ticketIDs(respRows)).Update("overdue", true)
		if res.Error != nil {
			return 0, res.Error
		}
		total += res.RowsAffected
		s.notifyOverdue(ctx, respRows, "response")
		s.publishOverdueEvents(ctx, respRows, "response")
	}
	resRows, err := s.findOverdueCandidates(ctx, true, now)
	if err != nil {
		return total, err
	}
	if len(resRows) > 0 {
		res := s.db.WithContext(ctx).Model(&Ticket{}).Where("id IN ?", ticketIDs(resRows)).Update("resolution_overdue", true)
		if res.Error != nil {
			return total, res.Error
		}
		total += res.RowsAffected
		s.notifyOverdue(ctx, resRows, "resolution")
		s.publishOverdueEvents(ctx, resRows, "resolution")
	}
	return total, nil
}

// findOverdueCandidates 先取「未标记且已过期」的候选；pending 已停表，不参与。
func (s *Service) findOverdueCandidates(ctx context.Context, resolution bool, now time.Time) ([]Ticket, error) {
	flag, dueColumn := "overdue", "due_at"
	if resolution {
		flag, dueColumn = "resolution_overdue", "resolution_due_at"
	}
	var rows []Ticket
	err := s.db.WithContext(ctx).
		Select("id", "ticket_no", "title", "assignee_id", "priority").
		Where(flag+" = ? AND status IN ? AND "+dueColumn+" IS NOT NULL AND "+dueColumn+" < ?",
			false, []string{StatusOpen, StatusProcessing}, now).
		Limit(overdueSweepLimit).Find(&rows).Error
	return rows, err
}

func ticketIDs(rows []Ticket) []int64 {
	ids := make([]int64, 0, len(rows))
	for _, t := range rows {
		ids = append(ids, int64(t.ID))
	}
	return ids
}

// Replies 增量拉取回复：seq > since，按序号升序，默认 50 条封顶 200。
// includeInternal=false 时过滤内部备注（访客视角）。
func (s *Service) Replies(ctx context.Context, ticketNo string, sinceSeq, limit int, includeInternal bool) (*Ticket, []TicketReply, error) {
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
	if !includeInternal {
		q = q.Where("is_internal = ?", false)
	}
	if sinceSeq > 0 {
		q = q.Where("seq > ?", sinceSeq)
	}
	if err := q.Order("seq ASC").Limit(limit).Find(&rows).Error; err != nil {
		return nil, nil, err
	}
	return &t, rows, nil
}

// CannedInput 是快捷回复的新增/修改入参。
type CannedInput struct {
	Title   string `json:"title"`
	Content string `json:"content"`
	Sort    int    `json:"sort"`
}

func (in *CannedInput) normalize() error {
	in.Title = strings.TrimSpace(in.Title)
	in.Content = strings.TrimSpace(in.Content)
	if in.Title == "" || in.Content == "" {
		return response.BadRequest("ticket.title_content_required")
	}
	if len(in.Title) > 100 || len(in.Content) > 2000 {
		return response.BadRequest("error.bad_request")
	}
	return nil
}

// ListCanned 按 sort、id 升序返回快捷回复；空结果也返回 []。
func (s *Service) ListCanned(ctx context.Context) ([]CannedReply, error) {
	rows := make([]CannedReply, 0, 8)
	err := s.db.WithContext(ctx).Order("sort ASC, id ASC").Find(&rows).Error
	return rows, err
}

// CreateCanned 新增快捷回复。
func (s *Service) CreateCanned(ctx context.Context, in CannedInput) (*CannedReply, error) {
	if err := in.normalize(); err != nil {
		return nil, err
	}
	id, err := idgen.Next()
	if err != nil {
		return nil, err
	}
	c := &CannedReply{Title: in.Title, Content: in.Content, Sort: in.Sort}
	c.ID = id
	if err := s.db.WithContext(ctx).Create(c).Error; err != nil {
		return nil, err
	}
	return c, nil
}

// UpdateCanned 修改快捷回复。
func (s *Service) UpdateCanned(ctx context.Context, id idgen.ID, in CannedInput) (*CannedReply, error) {
	if err := in.normalize(); err != nil {
		return nil, err
	}
	res := s.db.WithContext(ctx).Model(&CannedReply{}).Where("id = ?", int64(id)).
		Updates(map[string]any{"title": in.Title, "content": in.Content, "sort": in.Sort})
	if res.Error != nil {
		return nil, res.Error
	}
	if res.RowsAffected == 0 {
		return nil, response.NotFound("error.not_found")
	}
	var c CannedReply
	if err := s.db.WithContext(ctx).Where("id = ?", int64(id)).Take(&c).Error; err != nil {
		return nil, err
	}
	return &c, nil
}

// DeleteCanned 删除快捷回复。
func (s *Service) DeleteCanned(ctx context.Context, id idgen.ID) error {
	res := s.db.WithContext(ctx).Where("id = ?", int64(id)).Delete(&CannedReply{})
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return response.NotFound("error.not_found")
	}
	return nil
}

const bulkLimit = 100

// BulkInput 是批量操作入参（一次最多 100 张，超过直接 400）。
type BulkInput struct {
	TicketNos    []string `json:"ticketNos"`
	Action       string   `json:"action"` // assign | status | priority
	Status       string   `json:"status"`
	Priority     string   `json:"priority"`
	AssigneeID   idgen.ID `json:"assigneeId"`
	AssigneeName string   `json:"assigneeName"`
}

// BulkResult 返回成功与跳过（非法迁移、目标不存在）的笔数。
type BulkResult struct {
	Updated int `json:"updated"`
	Skipped int `json:"skipped"`
}

// Bulk 批量认领/改派、改状态、改优先级。逐张走单条事务；非法迁移计入 skipped 而不是整体失败。
func (s *Service) Bulk(ctx context.Context, in BulkInput, actorID idgen.ID, actorName string) (BulkResult, error) {
	var res BulkResult
	if len(in.TicketNos) == 0 || len(in.TicketNos) > bulkLimit {
		return res, response.BadRequest("error.bad_request")
	}
	switch in.Action {
	case "assign":
		if in.AssigneeID == 0 {
			return res, response.BadRequest("ticket.assignee_invalid")
		}
		in.AssigneeName = truncate(strings.TrimSpace(in.AssigneeName), 64)
	case "status":
		if in.Status != StatusProcessing && in.Status != StatusPending &&
			in.Status != StatusResolved && in.Status != StatusClosed {
			return res, response.BadRequest("ticket.status_invalid")
		}
	case "priority":
		if !ValidPriority(in.Priority) {
			return res, response.BadRequest("error.bad_request")
		}
	default:
		return res, response.BadRequest("error.bad_request")
	}
	seen := make(map[string]bool, len(in.TicketNos))
	for _, raw := range in.TicketNos {
		no := strings.TrimSpace(raw)
		if no == "" || seen[no] {
			continue
		}
		seen[no] = true
		var err error
		switch in.Action {
		case "assign":
			_, err = s.assign(ctx, no, in.AssigneeID, in.AssigneeName)
		case "status":
			_, err = s.SetStatus(ctx, no, in.Status, actorName)
		case "priority":
			err = s.setPriority(ctx, no, in.Priority)
		}
		if err == nil {
			res.Updated++
			continue
		}
		var re *response.Error
		if errors.As(err, &re) {
			res.Skipped++
			continue
		}
		return res, err
	}
	if in.Action == "assign" && res.Updated > 0 && in.AssigneeID != actorID {
		s.notifyBulkAssign(ctx, res.Updated, in.AssigneeID)
	}
	return res, nil
}

// setPriority 改优先级；未停表时按新优先级重算响应到期时间。
func (s *Service) setPriority(ctx context.Context, ticketNo, priority string) error {
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var t Ticket
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("ticket_no = ?", strings.TrimSpace(ticketNo)).Take(&t).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return response.NotFound("ticket.not_found")
			}
			return err
		}
		updates := map[string]any{"priority": priority}
		if t.SlaPausedAt == nil {
			updates["due_at"] = t.CreatedAt.Add(SLAFor(priority))
		}
		return tx.Model(&Ticket{}).Where("id = ?", int64(t.ID)).Updates(updates).Error
	})
}

func (s *Service) notifyAssign(ctx context.Context, t *Ticket) {
	title := "工单已转派给你"
	if strings.HasPrefix(strings.ToLower(s.lang), "en") {
		title = "Ticket assigned to you"
	}
	s.sendNotify(ctx, t.AssigneeID, title, t.TicketNo+" · "+t.Title,
		"/ticket/tickets?ticket="+url.QueryEscape(t.TicketNo), t.TicketNo)
}

func (s *Service) notifyReply(ctx context.Context, t *Ticket) {
	if t.AssigneeID == 0 {
		return
	}
	title := "工单有新回复"
	if strings.HasPrefix(strings.ToLower(s.lang), "en") {
		title = "New reply on ticket"
	}
	s.sendNotify(ctx, t.AssigneeID, title, t.TicketNo+" · "+t.Title,
		"/ticket/tickets?ticket="+url.QueryEscape(t.TicketNo), t.TicketNo)
}

func (s *Service) notifyBulkAssign(ctx context.Context, n int, assigneeID idgen.ID) {
	title := strconv.Itoa(n) + " 张工单已转派给你"
	if strings.HasPrefix(strings.ToLower(s.lang), "en") {
		title = "Tickets assigned to you: " + strconv.Itoa(n)
	}
	s.sendNotify(ctx, assigneeID, title, "", "/ticket/tickets", "")
}

// notifyNewTicket 新单提醒：通知当前租户内持有 ticket:list 的坐席，排除创建人；已指定负责人的不群发。
func (s *Service) notifyNewTicket(ctx context.Context, t *Ticket) {
	if s.notifier == nil || t.AssigneeID != 0 {
		return
	}
	tid, ok := tenant.From(ctx)
	if !ok {
		return
	}
	agents, err := s.queryAgents(ctx, tid)
	if err != nil {
		s.logWarn("ticket: resolve agents failed", "err", err.Error())
		return
	}
	ids := make([]idgen.ID, 0, len(agents))
	for _, a := range agents {
		if a.UserID != t.CreatorID {
			ids = append(ids, a.UserID)
		}
	}
	if len(ids) == 0 {
		return
	}
	title := "有新工单待认领"
	if strings.HasPrefix(strings.ToLower(s.lang), "en") {
		title = "New ticket needs an owner"
	}
	s.sendNotifyTo(ctx, ids, title, t.TicketNo+" · "+t.Title,
		"/ticket/tickets?ticket="+url.QueryEscape(t.TicketNo), t.TicketNo)
}

type agentContact struct {
	UserID   idgen.ID `gorm:"column:user_id"`
	TenantID idgen.ID `gorm:"column:tenant_id"`
}

// queryAgents 返回当前租户内持有 ticket:list 的坐席（含超管与内置角色）。
func (s *Service) queryAgents(ctx context.Context, tid idgen.ID) ([]agentContact, error) {
	db := s.db.WithContext(ctx)
	holders := db.Table("sys_user_role AS ur").Select("ur.user_id").
		Joins("JOIN sys_role r ON r.id = ur.role_id AND r.status = ?", "1").
		Joins("LEFT JOIN sys_role_menu rm ON rm.role_id = r.id").
		Joins("LEFT JOIN sys_menu m ON m.id = rm.menu_id AND m.status = ? AND m.perm = ?", "1", "ticket:list").
		Where("r.builtin = ? OR m.id IS NOT NULL", true)
	var rows []agentContact
	err := db.Table("sys_user AS u").
		Select("DISTINCT u.id AS user_id, u.tenant_id AS tenant_id").
		Where("u.deleted_at IS NULL AND u.status = ?", "1").
		Where("u.tenant_id = ?", int64(tid)).
		Where("u.is_super = ? OR u.id IN (?)", true, holders).
		Find(&rows).Error
	return rows, err
}

// publishOverdueEvents 把超时工单按负责人所属租户发布 ticket.overdue，
// 让 openapi webhook 与进程内订阅都能收到（清扫器自身没有租户上下文）。
func (s *Service) publishOverdueEvents(ctx context.Context, rows []Ticket, kind string) {
	if len(rows) == 0 {
		return
	}
	ids := make([]int64, 0, len(rows))
	for _, t := range rows {
		if t.AssigneeID != 0 {
			ids = append(ids, int64(t.AssigneeID))
		}
	}
	if len(ids) == 0 {
		return
	}
	var contacts []struct {
		ID       idgen.ID `gorm:"column:id"`
		TenantID idgen.ID `gorm:"column:tenant_id"`
	}
	if err := s.db.WithContext(tenant.Skip(ctx)).Table("sys_user").
		Select("id, tenant_id").Where("id IN ?", ids).Scan(&contacts).Error; err != nil {
		s.logWarn("ticket: resolve overdue assignee tenants failed", "err", err.Error())
		return
	}
	tenantOf := make(map[idgen.ID]idgen.ID, len(contacts))
	for _, c := range contacts {
		tenantOf[c.ID] = c.TenantID
	}
	for _, t := range rows {
		if t.AssigneeID == 0 {
			continue
		}
		tid := tenantOf[t.AssigneeID]
		if tid == 0 {
			continue
		}
		s.publish(tenant.With(ctx, tid), EventOverdue, &t, EventData{SenderType: ReplierSystem, SLAKind: kind})
	}
}

// notifyOverdue 超时提醒：清扫器没有租户上下文，按负责人反查所属租户后逐租户发送。
// 同一负责人的多张超时工单合并成一条，避免一轮清扫刷屏；kind 区分首响/解决时限。
func (s *Service) notifyOverdue(ctx context.Context, rows []Ticket, kind string) {
	if s.notifier == nil {
		return
	}
	counts := overdueByAssignee(rows)
	if len(counts) == 0 {
		return
	}
	ids := make([]int64, 0, len(counts))
	for id := range counts {
		ids = append(ids, int64(id))
	}
	var contacts []struct {
		ID       idgen.ID `gorm:"column:id"`
		TenantID idgen.ID `gorm:"column:tenant_id"`
	}
	if err := s.db.WithContext(tenant.Skip(ctx)).Table("sys_user").
		Select("id, tenant_id").Where("id IN ?", ids).Scan(&contacts).Error; err != nil {
		s.logWarn("ticket: resolve overdue assignee tenants failed", "err", err.Error())
		return
	}
	for _, c := range contacts {
		n := counts[c.ID]
		if n == 0 {
			continue
		}
		title := strconv.Itoa(n) + " 张工单已超响应时限"
		if kind == "resolution" {
			title = strconv.Itoa(n) + " 张工单已超解决时限"
		}
		if strings.HasPrefix(strings.ToLower(s.lang), "en") {
			title = "Response SLA breached: " + strconv.Itoa(n)
			if kind == "resolution" {
				title = "Resolution SLA breached: " + strconv.Itoa(n)
			}
		}
		s.sendNotifyTo(tenant.With(ctx, c.TenantID), []idgen.ID{c.ID}, title, "", "/ticket/tickets?overdue=1", "")
	}
}

// overdueByAssignee 统计每个负责人的超时工单数；未认领的没有收件人，不提醒。
func overdueByAssignee(rows []Ticket) map[idgen.ID]int {
	out := make(map[idgen.ID]int, len(rows))
	for _, t := range rows {
		if t.AssigneeID != 0 {
			out[t.AssigneeID]++
		}
	}
	return out
}

// lookupUserName 在租户用户表里补负责人显示名（前端只传 id）；查不到时留空，由前端回退展示 #id。
func (s *Service) lookupUserName(ctx context.Context, id idgen.ID) string {
	var row struct {
		Nickname string
		Username string
	}
	err := s.db.WithContext(ctx).Table("sys_user").
		Select("nickname, username").
		Where("id = ? AND deleted_at IS NULL", int64(id)).Take(&row).Error
	if err != nil {
		return ""
	}
	if row.Nickname != "" {
		return truncate(row.Nickname, 64)
	}
	return truncate(row.Username, 64)
}

// resolutionBreached 判断解决时限是否已过（暂停期间的时长补回后再比）。
func resolutionBreached(due, paused *time.Time, now time.Time) bool {
	d := resumeDueAt(due, paused, now)
	return d != nil && now.After(*d)
}

// resumeDueAt 把暂停时长补回 SLA 到期时间；无到期时间时返回 nil（由调用方决定是否重算）。
func resumeDueAt(due, paused *time.Time, now time.Time) *time.Time {
	if due == nil {
		return nil
	}
	if paused == nil {
		return due
	}
	shifted := due.Add(now.Sub(*paused))
	return &shifted
}

// sendNotify 是单收件人的站内信出口：未注入 notifier 或没有收件人时静默跳过。
func (s *Service) sendNotify(ctx context.Context, uid idgen.ID, title, content, link, ticketNo string) {
	if uid == 0 {
		return
	}
	s.sendNotifyTo(ctx, []idgen.ID{uid}, title, content, link, ticketNo)
}

// sendNotifyTo 是站内信的唯一出口，支持一次投递给多个收件人（同租户）。
func (s *Service) sendNotifyTo(ctx context.Context, ids []idgen.ID, title, content, link, ticketNo string) {
	if s.notifier == nil || len(ids) == 0 {
		return
	}
	err := s.notifier.Send(ctx, notify.Msg{
		To: ids, Category: notifyCategory, Title: title, Content: content,
		Link: link, Channels: []string{notify.ChannelInApp},
	})
	if err != nil {
		s.logWarn("ticket: notify failed", "ticketNo", ticketNo, "err", err.Error())
	}
}

func truncate(s string, n int) string {
	if len(s) > n {
		return s[:n]
	}
	return s
}

func ptr[T any](v T) *T { return &v }
