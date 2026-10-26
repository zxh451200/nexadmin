package im

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"nexadmin/core/response"
	"nexadmin/core/ws"
	"nexadmin/pkg/idgen"
)

// Service 是 IM 业务层。
type Service struct {
	db  *gorm.DB
	log logger
	hub *hub
}

// logger 是最小日志面（便于测试注入空实现）。
type logger interface {
	Warn(msg string, keysAndValues ...any)
	Error(msg string, keysAndValues ...any)
}

// NewService 创建 IM 服务。
func NewService(db *gorm.DB, log logger) *Service { return &Service{db: db, log: log, hub: newHub()} }

// Subscribe 订阅一个会话的实时消息（SSE 用）；返回的取消函数在连接结束时必须调用。
func (s *Service) Subscribe(convNo string) (<-chan []byte, func()) {
	return s.hub.subscribe(convNo)
}

// OpenInput 是打开（或幂等取回）会话的入参。VisitorToken 由宿主前端给出
// （匿名令牌或会员 ID），是访客侧唯一身份；biz 三元组决定会话挂哪个业务对象。
type OpenInput struct {
	BizType      string
	BizNo        string
	VisitorToken string
	VisitorName  string
	Subject      string
}

// Open 打开会话：同 (biz_type, biz_no, visitor_token) 直接返回既有会话（幂等）。
func (s *Service) Open(ctx context.Context, in OpenInput) (*Conversation, error) {
	in.BizType, in.BizNo = strings.TrimSpace(in.BizType), strings.TrimSpace(in.BizNo)
	in.VisitorToken = strings.TrimSpace(in.VisitorToken)
	in.VisitorName, in.Subject = strings.TrimSpace(in.VisitorName), strings.TrimSpace(in.Subject)
	if in.VisitorToken == "" {
		return nil, response.BadRequest("im.visitor_required")
	}
	if len(in.BizType) > 32 || len(in.BizNo) > 64 {
		return nil, response.BadRequest("error.bad_request")
	}

	var existing Conversation
	err := s.db.WithContext(ctx).Where(
		"biz_type = ? AND biz_no = ? AND visitor_token = ?", in.BizType, in.BizNo, in.VisitorToken,
	).Take(&existing).Error
	if err == nil {
		return &existing, nil
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, err
	}
	id, err := idgen.Next()
	if err != nil {
		return nil, err
	}
	c := &Conversation{
		ConvNo:  "IM" + strconv.FormatInt(int64(id), 10),
		BizType: in.BizType, BizNo: in.BizNo, VisitorToken: in.VisitorToken,
		VisitorName: truncate(in.VisitorName, 64), Subject: truncate(in.Subject, 200),
		Status: StatusOpen,
	}
	c.ID = id
	if err := s.db.WithContext(ctx).Create(c).Error; err != nil {
		// 并发幂等：撞唯一索引后回读先到的那笔。
		var dup Conversation
		if take := s.db.WithContext(ctx).Where(
			"biz_type = ? AND biz_no = ? AND visitor_token = ?", in.BizType, in.BizNo, in.VisitorToken,
		).Take(&dup).Error; take == nil {
			return &dup, nil
		}
		return nil, err
	}
	return c, nil
}

// SendInput 是发送消息的入参；SenderType 只能是 visitor / agent（system 预留给宿主）。
type SendInput struct {
	SenderType string
	SenderID   string
	SenderName string
	Content    string
}

// Send 在会话内发一条消息：行锁下取序号、更新会话摘要，事务提交后发事件。
// 关闭的会话拒收（Conflict im.conv_closed）。
func (s *Service) Send(ctx context.Context, convNo string, in SendInput) (*Message, error) {
	convNo = strings.TrimSpace(convNo)
	in.Content = strings.TrimSpace(in.Content)
	switch {
	case convNo == "":
		return nil, response.BadRequest("error.bad_request")
	case in.SenderType != SenderVisitor && in.SenderType != SenderAgent && in.SenderType != SenderSystem:
		return nil, response.BadRequest("im.sender_invalid")
	case in.Content == "":
		return nil, response.BadRequest("im.content_required")
	}
	in.Content = truncate(in.Content, 2000)
	in.SenderName = truncate(strings.TrimSpace(in.SenderName), 64)

	var out Message
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var c Conversation
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("conv_no = ?", convNo).Take(&c).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return response.NotFound("im.conv_not_found")
			}
			return err
		}
		if c.Status != StatusOpen {
			return response.Conflict("im.conv_closed")
		}
		id, err := idgen.Next()
		if err != nil {
			return err
		}
		out = Message{
			ConvID: c.ID, Seq: c.MsgSeq + 1, SenderType: in.SenderType,
			SenderID: truncate(in.SenderID, 64), SenderName: in.SenderName, Content: in.Content,
		}
		out.ID = id
		out.CreatedAt = time.Now().UTC()
		if err := tx.Create(&out).Error; err != nil {
			return err
		}
		return tx.Model(&Conversation{}).Where("id = ?", int64(c.ID)).Updates(map[string]any{
			"msg_seq": out.Seq, "last_msg_at": out.CreatedAt, "last_preview": preview(in.Content),
		}).Error
	})
	if err != nil {
		return nil, err
	}
	// 事件在事务外发（载荷只带预览，失败由轮询兜底）；
	// 双通道推送：SSE 流(插件自带 hub) + 框架 WebSocket hub(主题 im:<convNo>)。
	var c Conversation
	if err := s.db.WithContext(ctx).Where("conv_no = ?", convNo).Take(&c).Error; err == nil {
		s.publishCreated(ctx, &c, &out)
		if raw, err := json.Marshal(out); err == nil {
			s.hub.publish(convNo, raw)
			ws.Publish("im:"+convNo, json.RawMessage(raw))
		}
	} else if s.log != nil {
		s.log.Warn("im: reload conversation after send failed", "convNo", convNo, "err", err.Error())
	}
	return &out, nil
}

// Messages 增量拉取会话消息：seq > since，按序号升序，默认 50 条封顶 200。
func (s *Service) Messages(ctx context.Context, convNo string, sinceSeq int64, limit int) (*Conversation, []Message, error) {
	var c Conversation
	if err := s.db.WithContext(ctx).Where("conv_no = ?", strings.TrimSpace(convNo)).Take(&c).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil, response.NotFound("im.conv_not_found")
		}
		return nil, nil, err
	}
	if limit <= 0 {
		limit = 50
	}
	if limit > 200 {
		limit = 200
	}
	var rows []Message
	q := s.db.WithContext(ctx).Where("conv_id = ?", int64(c.ID))
	if sinceSeq > 0 {
		q = q.Where("seq > ?", sinceSeq)
	}
	if err := q.Order("seq ASC").Limit(limit).Find(&rows).Error; err != nil {
		return nil, nil, err
	}
	return &c, rows, nil
}

// MarkAgentRead 把坐席已读游标推进到 seq（只前进不后退），返回是否更新。
func (s *Service) MarkAgentRead(ctx context.Context, convNo string, seq int64) error {
	if seq <= 0 {
		return nil
	}
	return s.db.WithContext(ctx).Model(&Conversation{}).
		Where("conv_no = ? AND agent_read_seq < ?", strings.TrimSpace(convNo), seq).
		Update("agent_read_seq", seq).Error
}

// ConversationQuery 是坐席会话列表过滤条件。
type ConversationQuery struct {
	Status   string
	BizType  string
	Keyword  string // 会话号 / 访客名 / 摘要模糊
	Page     int
	PageSize int
}

// ConversationView 是会话列表视图（含未读数）。
type ConversationView struct {
	Conversation
	Unread int64 `json:"unread"`
}

// ListConversations 坐席会话列表：按最后消息时间倒序，附带每个会话的访客未读数。
func (s *Service) ListConversations(ctx context.Context, q ConversationQuery) ([]ConversationView, int64, error) {
	db := s.db.WithContext(ctx).Model(&Conversation{})
	if q.Status != "" {
		db = db.Where("status = ?", q.Status)
	}
	if q.BizType != "" {
		db = db.Where("biz_type = ?", q.BizType)
	}
	if kw := strings.TrimSpace(q.Keyword); kw != "" {
		like := "%" + kw + "%"
		db = db.Where("conv_no LIKE ? OR visitor_name LIKE ? OR last_preview LIKE ?", like, like, like)
	}
	var total int64
	if err := db.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var rows []Conversation
	if err := db.Order("COALESCE(last_msg_at, created_at) DESC").
		Offset((q.Page - 1) * q.PageSize).Limit(q.PageSize).Find(&rows).Error; err != nil {
		return nil, 0, err
	}
	// 一次分组查询补未读数：visitor 消息 seq 超过坐席已读游标即未读。
	unread := map[idgen.ID]int64{}
	if len(rows) > 0 {
		ids := make([]int64, len(rows))
		for i := range rows {
			ids[i] = int64(rows[i].ID)
		}
		type cnt struct {
			ConvID idgen.ID
			N      int64
		}
		var cs []cnt
		if err := s.db.WithContext(ctx).Model(&Message{}).
			Select("conv_id, COUNT(*) AS n").
			Where("conv_id IN ? AND sender_type = ? AND seq > agent_read_seq", ids, SenderVisitor).
			Joins("JOIN plg_im_conversation c ON c.id = plg_im_message.conv_id").
			Group("conv_id").Scan(&cs).Error; err == nil {
			for _, x := range cs {
				unread[x.ConvID] = x.N
			}
		} else if s.log != nil {
			s.log.Warn("im: count unread failed", "err", err.Error())
		}
	}
	out := make([]ConversationView, len(rows))
	for i := range rows {
		out[i] = ConversationView{Conversation: rows[i], Unread: unread[rows[i].ID]}
	}
	return out, total, nil
}

// Close 关闭会话（坐席操作）：只有 open 可以关；关闭后拒收新消息。
func (s *Service) Close(ctx context.Context, convNo string) (*Conversation, error) {
	var c Conversation
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("conv_no = ?", strings.TrimSpace(convNo)).Take(&c).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return response.NotFound("im.conv_not_found")
			}
			return err
		}
		if c.Status != StatusOpen {
			return response.Conflict("im.conv_closed")
		}
		return tx.Model(&Conversation{}).Where("id = ?", int64(c.ID)).
			Update("status", StatusClosed).Error
	})
	if err != nil {
		return nil, err
	}
	c.Status = StatusClosed
	return &c, nil
}

func truncate(s string, n int) string {
	if len(s) > n {
		return s[:n]
	}
	return s
}
