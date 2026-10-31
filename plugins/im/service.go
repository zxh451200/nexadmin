package im

import (
	"context"
	"encoding/json"
	"errors"
	"sort"
	"strconv"
	"strings"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"nexadmin/core/cache"
	"nexadmin/core/cluster"
	"nexadmin/core/response"
	"nexadmin/core/storage"
	"nexadmin/core/tenant"
	"nexadmin/core/ws"
	"nexadmin/pkg/idgen"
)

// Service 是 IM 业务层。
type Service struct {
	db    *gorm.DB
	log   logger
	hub   *hub
	store storage.Storage
	bus   *cluster.Bus
	cache cache.Cache
}

// logger 是最小日志面（便于测试注入空实现）。
type logger interface {
	Info(msg string, keysAndValues ...any)
	Warn(msg string, keysAndValues ...any)
	Error(msg string, keysAndValues ...any)
}

// NewService 创建 IM 服务。
func NewService(db *gorm.DB, log logger) *Service { return &Service{db: db, log: log, hub: newHub()} }

// SetStorage 注入附件存储（local / S3 由框架 storage 驱动决定）。
func (s *Service) SetStorage(st storage.Storage) { s.store = st }

// Storage 返回附件存储；未配置时为 nil。
func (s *Service) Storage() storage.Storage { return s.store }

// SetBus 注入跨实例事件总线；nil 安全（单实例 / 缓存不支持发布订阅时只走进程内推送）。
func (s *Service) SetBus(b *cluster.Bus) { s.bus = b }

// SetCache 注入缓存：坐席在线状态（心跳）与跨实例共享的短 TTL 键都用它。
func (s *Service) SetCache(c cache.Cache) { s.cache = c }

// busKind 是 IM 跨实例广播的事件种类。
const busKind = "im.push"

// busEnvelope 是跨实例转发的推送载荷：会话号 + 原始帧（消息或控制帧）。
type busEnvelope struct {
	ConvNo string          `json:"convNo"`
	Frame  json.RawMessage `json:"frame"`
}

// Deliver 把别的实例转发来的帧投递给本实例的订阅者（SSE hub + 本地 WebSocket）。
// 由 cluster.Bus 的订阅回调调用；本实例自己发出的帧不会回到这里（Bus 不回声）。
func (s *Service) Deliver(raw json.RawMessage) {
	var e busEnvelope
	if err := json.Unmarshal(raw, &e); err != nil || e.ConvNo == "" || len(e.Frame) == 0 {
		return
	}
	s.hub.publish(e.ConvNo, e.Frame)
	ws.Publish("im:"+e.ConvNo, e.Frame)
}

// push 先把帧投给本实例，再广播给其它实例（多实例部署时由对端 Deliver 落地）。
func (s *Service) push(convNo string, raw []byte) {
	s.hub.publish(convNo, raw)
	ws.Publish("im:"+convNo, json.RawMessage(raw))
	if s.bus != nil {
		s.bus.Publish(busKind, busEnvelope{ConvNo: convNo, Frame: json.RawMessage(raw)})
	}
}

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
	// 访客来源，仅用于坐席判断上下文（IP / UA / 来源页）。
	IP  string
	UA  string
	Ref string
}

// Open 打开会话：同 (biz_type, biz_no, visitor_token) 直接返回既有会话（幂等），
// 并顺手刷新访客名 / 来源信息（同一访客下次打开挂件时可用）。
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
		s.refreshVisitor(ctx, &existing, in)
		// 已结束的会话再被打开 = 访客重新开口：复用同一行重新开启（biz 三元组唯一），
		// 结算上一轮的评分与归属，并留一条「会话已重新开启」的系统提示。
		if existing.Status != StatusOpen {
			if rerr := s.reopen(ctx, &existing); rerr != nil {
				return nil, rerr
			}
			s.publishControl(existing.ConvNo, map[string]any{"type": "reopened"})
			s.autoAssign(ctx, &existing)
		}
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
		Status:    StatusOpen,
		VisitorIP: truncate(in.IP, 45), VisitorUA: truncate(in.UA, 255), VisitorRef: truncate(in.Ref, 255),
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
	s.sendWelcome(ctx, c)
	s.autoAssign(ctx, c)
	return c, nil
}

// sendWelcome 在新会话里落一条欢迎语（以客服身份发送），失败只记日志、不影响开会话。
func (s *Service) sendWelcome(ctx context.Context, c *Conversation) {
	text := strings.TrimSpace(s.Settings(ctx).WelcomeText)
	if text == "" {
		return
	}
	id, err := idgen.Next()
	if err != nil {
		return
	}
	now := time.Now().UTC()
	m := Message{ConvID: c.ID, Seq: c.MsgSeq + 1, SenderType: SenderAgent, Content: truncate(text, 2000), Kind: KindText, CreatedAt: now}
	m.ID = id
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(&m).Error; err != nil {
			return err
		}
		return tx.Model(&Conversation{}).Where("id = ?", int64(c.ID)).Updates(map[string]any{
			"msg_seq": m.Seq, "last_msg_at": now, "last_preview": preview(text),
		}).Error
	})
	if err != nil {
		if s.log != nil {
			s.log.Warn("im: send welcome message failed", "convNo", c.ConvNo, "err", err.Error())
		}
		return
	}
	c.MsgSeq = m.Seq
	c.LastMsgAt = &now
	c.LastPreview = preview(text)
}

// reopen 把已结束的会话重新开启：清掉上一轮的结束信息、评分与归属，
// 追加一条系统提示并推进序号。并发打开时以行锁保证只开一次。
func (s *Service) reopen(ctx context.Context, c *Conversation) error {
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var cur Conversation
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("id = ?", int64(c.ID)).Take(&cur).Error; err != nil {
			return err
		}
		if cur.Status == StatusOpen {
			*c = cur
			return nil
		}
		now := time.Now().UTC()
		id, err := idgen.Next()
		if err != nil {
			return err
		}
		meta, _ := json.Marshal(map[string]string{"code": "conv_reopened"})
		m := Message{ConvID: cur.ID, Seq: cur.MsgSeq + 1, SenderType: SenderSystem, Kind: KindSystem, Meta: string(meta), CreatedAt: now}
		m.ID = id
		if err := tx.Create(&m).Error; err != nil {
			return err
		}
		if err := tx.Model(&Conversation{}).Where("id = ?", int64(cur.ID)).Updates(map[string]any{
			"status": StatusOpen, "closed_at": nil, "closed_by": "",
			"rating": 0, "rating_comment": "", "rated_at": nil,
			"assignee_id": "", "assignee_name": "",
			"msg_seq": m.Seq, "last_msg_at": now, "last_preview": "",
		}).Error; err != nil {
			return err
		}
		cur.Status, cur.ClosedAt, cur.ClosedBy = StatusOpen, nil, ""
		cur.Rating, cur.RatingComment, cur.RatedAt = 0, "", nil
		cur.AssigneeID, cur.AssigneeName = "", ""
		cur.MsgSeq, cur.LastMsgAt, cur.LastPreview = m.Seq, &now, ""
		*c = cur
		return nil
	})
}

// refreshVisitor 在有新信息时刷新访客名与来源；没有变化就不写库（挂件每次打开都会走这里）。
func (s *Service) refreshVisitor(ctx context.Context, c *Conversation, in OpenInput) {
	updates := map[string]any{}
	if name := truncate(in.VisitorName, 64); name != "" && name != c.VisitorName {
		c.VisitorName = name
		updates["visitor_name"] = name
	}
	if subject := truncate(in.Subject, 200); subject != "" && subject != c.Subject {
		c.Subject = subject
		updates["subject"] = subject
	}
	if ip := truncate(in.IP, 45); ip != "" && ip != c.VisitorIP {
		c.VisitorIP = ip
		updates["visitor_ip"] = ip
	}
	if ua := truncate(in.UA, 255); ua != "" && ua != c.VisitorUA {
		c.VisitorUA = ua
		updates["visitor_ua"] = ua
	}
	if ref := truncate(in.Ref, 255); ref != "" && ref != c.VisitorRef {
		c.VisitorRef = ref
		updates["visitor_ref"] = ref
	}
	if len(updates) == 0 {
		return
	}
	if err := s.db.WithContext(ctx).Model(&Conversation{}).Where("id = ?", int64(c.ID)).
		Updates(updates).Error; err != nil && s.log != nil {
		s.log.Warn("im: refresh visitor failed", "convNo", c.ConvNo, "err", err.Error())
	}
}

// SendInput 是发送消息的入参；SenderType 只能是 visitor / agent（system 预留给宿主）。
// Attachment 是 /attachments 上传后拿到的 token；带附件时 Content 可以为空。
type SendInput struct {
	SenderType string
	SenderID   string
	SenderName string
	Content    string
	Attachment string
}

// Send 在会话内发一条消息：行锁下取序号、更新会话摘要，事务提交后发事件。
// 关闭的会话拒收（Conflict im.conv_closed）。
func (s *Service) Send(ctx context.Context, convNo string, in SendInput) (*Message, error) {
	convNo = strings.TrimSpace(convNo)
	in.Content = strings.TrimSpace(in.Content)
	in.Attachment = strings.TrimSpace(in.Attachment)
	switch {
	case convNo == "":
		return nil, response.BadRequest("error.bad_request")
	case in.SenderType != SenderVisitor && in.SenderType != SenderAgent && in.SenderType != SenderSystem:
		return nil, response.BadRequest("im.sender_invalid")
	case in.Content == "" && in.Attachment == "":
		return nil, response.BadRequest("im.content_required")
	}
	in.Content = truncate(in.Content, 2000)
	// 访客消息过一遍敏感词（坐席回复不替换，避免误伤内部话术）。
	if in.SenderType == SenderVisitor && in.Content != "" {
		if words := s.Settings(ctx).BannedWords; len(words) > 0 {
			in.Content = maskBanned(in.Content, words)
		}
	}
	in.SenderName = truncate(strings.TrimSpace(in.SenderName), 64)
	kind := KindText
	if in.SenderType == SenderSystem {
		kind = KindSystem
	}

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
		meta := ""
		previewText := preview(in.Content)
		if in.Attachment != "" {
			var att Attachment
			if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
				Where("token = ? AND conv_id = ?", in.Attachment, int64(c.ID)).Take(&att).Error; err != nil {
				if errors.Is(err, gorm.ErrRecordNotFound) {
					return response.BadRequest("im.attachment_invalid")
				}
				return err
			}
			if att.Used {
				return response.Conflict("im.attachment_used")
			}
			if strings.HasPrefix(att.Mime, "image/") {
				kind = KindImage
			} else {
				kind = KindFile
			}
			meta = attachmentMeta(&att)
			if previewText == "" {
				previewText = preview(att.Name)
			}
			if err := tx.Model(&Attachment{}).Where("id = ?", int64(att.ID)).Update("used", true).Error; err != nil {
				return err
			}
		}
		out = Message{
			ConvID: c.ID, Seq: c.MsgSeq + 1, SenderType: in.SenderType,
			SenderID: truncate(in.SenderID, 64), SenderName: in.SenderName,
			Content: in.Content, Kind: kind, Meta: meta,
		}
		out.ID = id
		out.CreatedAt = time.Now().UTC()
		if err := tx.Create(&out).Error; err != nil {
			return err
		}
		if err := tx.Model(&Conversation{}).Where("id = ?", int64(c.ID)).Updates(map[string]any{
			"msg_seq": out.Seq, "last_msg_at": out.CreatedAt, "last_preview": previewText,
		}).Error; err != nil {
			return err
		}
		// 坐席首次回复自动认领，省掉一次额外点击。
		if in.SenderType == SenderAgent && in.SenderID != "" {
			return tx.Model(&Conversation{}).Where("id = ? AND assignee_id = ''", int64(c.ID)).
				Updates(map[string]any{"assignee_id": in.SenderID, "assignee_name": in.SenderName}).Error
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	// 事件在事务外发（载荷只带预览，失败由轮询兜底）；
	// 双通道推送：SSE 流(插件自带 hub) + 框架 WebSocket hub(主题 im:<convNo>)。
	var c Conversation
	if err := s.db.WithContext(ctx).Where("conv_no = ?", convNo).Take(&c).Error; err == nil {
		s.publishCreated(ctx, &c, &out)
		s.publishMessageTo(convNo, &out)
	} else if s.log != nil {
		s.log.Warn("im: reload conversation after send failed", "convNo", convNo, "err", err.Error())
	}
	return &out, nil
}

// publishMessageTo 把一条消息推给会话订阅者（本实例 SSE + WS，并广播到其它实例）。
func (s *Service) publishMessageTo(convNo string, m *Message) {
	raw, err := json.Marshal(m)
	if err != nil {
		return
	}
	s.push(convNo, raw)
}

// MessageQuery 是增量 / 窗口 / 翻页的统一入参：
//   - Since > 0：拉取 seq > Since 的新消息（轮询 / SSE 兜底）；
//   - Before > 0：拉取 seq < Before 的更早消息（向上翻页）；
//   - 都为 0：拉取最近 Limit 条（首屏）。
type MessageQuery struct {
	Since  int64
	Before int64
	Limit  int
}

// Messages 拉取会话消息，返回 (会话, 消息, 是否还有更早/更多, 错误)。
func (s *Service) Messages(ctx context.Context, convNo string, q MessageQuery) (*Conversation, []Message, bool, error) {
	var c Conversation
	if err := s.db.WithContext(ctx).Where("conv_no = ?", strings.TrimSpace(convNo)).Take(&c).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil, false, response.NotFound("im.conv_not_found")
		}
		return nil, nil, false, err
	}
	limit := q.Limit
	if limit <= 0 {
		limit = 50
	}
	if limit > 200 {
		limit = 200
	}
	db := s.db.WithContext(ctx).Where("conv_id = ?", int64(c.ID))
	var rows []Message
	var asc bool
	switch {
	case q.Since > 0:
		db = db.Where("seq > ?", q.Since).Order("seq ASC")
		asc = true
	case q.Before > 0:
		db = db.Where("seq < ?", q.Before).Order("seq DESC")
	default:
		db = db.Order("seq DESC")
	}
	if err := db.Limit(limit).Find(&rows).Error; err != nil {
		return nil, nil, false, err
	}
	hasMore := len(rows) == limit
	if !asc {
		for i, j := 0, len(rows)-1; i < j; i, j = i+1, j-1 {
			rows[i], rows[j] = rows[j], rows[i]
		}
	}
	return &c, rows, hasMore, nil
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

// SetVisitorRead 推进访客已读游标（只前进不后退），并推给坐席端显示"已读"。
func (s *Service) SetVisitorRead(ctx context.Context, convNo string, seq int64) error {
	if seq <= 0 {
		return nil
	}
	convNo = strings.TrimSpace(convNo)
	res := s.db.WithContext(ctx).Model(&Conversation{}).
		Where("conv_no = ? AND visitor_read_seq < ?", convNo, seq).
		Update("visitor_read_seq", seq)
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected > 0 {
		s.publishControl(convNo, map[string]any{"type": "read", "seq": seq})
	}
	return nil
}

// ConversationQuery 是坐席会话列表过滤条件。
type ConversationQuery struct {
	Status     string
	BizType    string
	Keyword    string // 会话号 / 访客名 / 摘要模糊
	AssigneeID string // 精确匹配；空表示不过滤
	Unassigned bool   // 只看未分配
	Tag        string // 标签精确包含（逗号分隔存储）
	Page       int
	PageSize   int
}

// ConversationView 是会话列表视图（含未读数）。
type ConversationView struct {
	Conversation
	Unread int64 `json:"unread"`
}

// convFilter 构造列表与导出共用的筛选条件（标签、关键字、坐席、状态）。
func (s *Service) convFilter(ctx context.Context, q ConversationQuery) *gorm.DB {
	db := s.db.WithContext(ctx).Model(&Conversation{})
	if q.Status != "" {
		db = db.Where("status = ?", q.Status)
	}
	if q.BizType != "" {
		db = db.Where("biz_type = ?", q.BizType)
	}
	if q.Unassigned {
		db = db.Where("assignee_id = ''")
	} else if id := strings.TrimSpace(q.AssigneeID); id != "" {
		db = db.Where("assignee_id = ?", id)
	}
	if tag := strings.TrimSpace(q.Tag); tag != "" {
		db = db.Where("CONCAT(',', tags, ',') LIKE ?", "%,"+tag+",%")
	}
	if kw := strings.TrimSpace(q.Keyword); kw != "" {
		like := "%" + kw + "%"
		db = db.Where("conv_no LIKE ? OR visitor_name LIKE ? OR last_preview LIKE ?", like, like, like)
	}
	return db
}

// ExportConversations 按与列表相同的筛选条件取导出数据（不含未读数），上限 limit。
func (s *Service) ExportConversations(ctx context.Context, q ConversationQuery, limit int) ([]Conversation, error) {
	if limit <= 0 {
		limit = 5000
	}
	var rows []Conversation
	err := s.convFilter(ctx, q).Order("COALESCE(last_msg_at, created_at) DESC").Limit(limit).Find(&rows).Error
	return rows, err
}

// ListConversations 坐席会话列表：按最后消息时间倒序，附带每个会话的访客未读数。
func (s *Service) ListConversations(ctx context.Context, q ConversationQuery) ([]ConversationView, int64, error) {
	db := s.convFilter(ctx, q)
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

// Close 关闭会话（坐席或自动关闭）：写入一条系统消息、推进序号、落结束信息，
// 提交后把系统消息与会话结束事件推给双方实时通道。只有 open 可以关。
func (s *Service) Close(ctx context.Context, convNo, byID string) (*Conversation, *Message, error) {
	convNo = strings.TrimSpace(convNo)
	var (
		c   Conversation
		out Message
	)
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
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
		now := time.Now().UTC()
		out = Message{
			ConvID: c.ID, Seq: c.MsgSeq + 1, SenderType: SenderSystem, Kind: KindSystem,
			Meta: `{"code":"` + MetaCodeConvClosed + `"}`, Content: "",
		}
		out.ID = id
		out.CreatedAt = now
		if err := tx.Create(&out).Error; err != nil {
			return err
		}
		closedBy := truncate(strings.TrimSpace(byID), 64)
		if closedBy == "" {
			closedBy = "auto"
		}
		c.Status = StatusClosed
		c.ClosedAt = &now
		c.ClosedBy = closedBy
		c.MsgSeq = out.Seq
		return tx.Model(&Conversation{}).Where("id = ?", int64(c.ID)).Updates(map[string]any{
			"status": StatusClosed, "closed_at": now, "closed_by": closedBy,
			"msg_seq": out.Seq,
		}).Error
	})
	if err != nil {
		return nil, nil, err
	}
	raw, _ := json.Marshal(out)
	s.hub.publish(c.ConvNo, raw)
	ws.Publish("im:"+c.ConvNo, json.RawMessage(raw))
	s.publishControl(c.ConvNo, map[string]any{"type": "closed", "closedBy": c.ClosedBy})
	return &c, &out, nil
}

// Claim 认领会话；已是别人的则冲突（避免两个坐席同时接）。
func (s *Service) Claim(ctx context.Context, convNo, userID, userName string) (*Conversation, error) {
	userID = strings.TrimSpace(userID)
	if userID == "" {
		return nil, response.Unauthorized("error.unauthorized")
	}
	res := s.db.WithContext(ctx).Model(&Conversation{}).
		Where("conv_no = ? AND (assignee_id = '' OR assignee_id = ?)", strings.TrimSpace(convNo), userID).
		Updates(map[string]any{"assignee_id": userID, "assignee_name": truncate(strings.TrimSpace(userName), 64)})
	if res.Error != nil {
		return nil, res.Error
	}
	if res.RowsAffected == 0 {
		var c Conversation
		if err := s.db.WithContext(ctx).Where("conv_no = ?", strings.TrimSpace(convNo)).Take(&c).Error; err != nil {
			return nil, response.NotFound("im.conv_not_found")
		}
		if c.AssigneeID != "" && c.AssigneeID != userID {
			return nil, response.Conflict("im.claimed")
		}
		return &c, nil
	}
	var c Conversation
	if err := s.db.WithContext(ctx).Where("conv_no = ?", strings.TrimSpace(convNo)).Take(&c).Error; err != nil {
		return nil, err
	}
	s.publishControl(c.ConvNo, map[string]any{"type": "assigned", "assigneeId": c.AssigneeID, "assigneeName": c.AssigneeName})
	return &c, nil
}

// Release 释放认领。
func (s *Service) Release(ctx context.Context, convNo string) (*Conversation, error) {
	if err := s.db.WithContext(ctx).Model(&Conversation{}).Where("conv_no = ?", strings.TrimSpace(convNo)).
		Updates(map[string]any{"assignee_id": "", "assignee_name": ""}).Error; err != nil {
		return nil, err
	}
	var c Conversation
	if err := s.db.WithContext(ctx).Where("conv_no = ?", strings.TrimSpace(convNo)).Take(&c).Error; err != nil {
		return nil, response.NotFound("im.conv_not_found")
	}
	s.publishControl(c.ConvNo, map[string]any{"type": "assigned", "assigneeId": "", "assigneeName": ""})
	return &c, nil
}

// SetTags 覆盖会话标签（逗号分隔，去重、最多 10 个、每个 ≤ 24 字）。
func (s *Service) SetTags(ctx context.Context, convNo, raw string) (*Conversation, error) {
	tags := normalizeTags(raw)
	if err := s.db.WithContext(ctx).Model(&Conversation{}).Where("conv_no = ?", strings.TrimSpace(convNo)).
		Update("tags", tags).Error; err != nil {
		return nil, err
	}
	var c Conversation
	if err := s.db.WithContext(ctx).Where("conv_no = ?", strings.TrimSpace(convNo)).Take(&c).Error; err != nil {
		return nil, response.NotFound("im.conv_not_found")
	}
	return &c, nil
}

// SetNote 覆盖会话的内部备注（只给坐席看，不推给访客）；空串表示清空。
func (s *Service) SetNote(ctx context.Context, convNo, note string) (*Conversation, error) {
	if err := s.db.WithContext(ctx).Model(&Conversation{}).Where("conv_no = ?", strings.TrimSpace(convNo)).
		Update("note", truncate(strings.TrimSpace(note), 500)).Error; err != nil {
		return nil, err
	}
	var c Conversation
	if err := s.db.WithContext(ctx).Where("conv_no = ?", strings.TrimSpace(convNo)).Take(&c).Error; err != nil {
		return nil, response.NotFound("im.conv_not_found")
	}
	return &c, nil
}

// DeleteResult 是删除会话的结果：实际删掉的会话数与消息数，以及被跳过的会话号。
type DeleteResult struct {
	Deleted  int      `json:"deleted"`
	Messages int64    `json:"messages"`
	Skipped  []string `json:"skipped"`
}

// 批量删除时用于区分「跳过」的原因，不直接暴露给用户。
var (
	errConvMissing   = errors.New("im: conversation not found")
	errConvNotClosed = errors.New("im: conversation not closed")
)

// DeleteConversation 物理删除一个已结束的会话及其消息、附件（含存储对象）。
// 进行中的会话拒绝删除：访客可能正在聊，直接抹掉会让对面突然断流。
func (s *Service) DeleteConversation(ctx context.Context, convNo string) (DeleteResult, error) {
	res, err := s.deleteOne(ctx, strings.TrimSpace(convNo))
	switch {
	case errors.Is(err, errConvNotClosed):
		return res, response.Conflict("im.conv_open")
	case errors.Is(err, errConvMissing):
		return res, response.NotFound("im.conv_not_found")
	}
	return res, err
}

// DeleteConversations 批量删除（只删已结束的），不存在的/进行中的记入 Skipped，单次上限 200。
func (s *Service) DeleteConversations(ctx context.Context, nos []string) (DeleteResult, error) {
	out := DeleteResult{Skipped: []string{}}
	for i, no := range nos {
		if i >= 200 {
			break
		}
		res, err := s.deleteOne(ctx, strings.TrimSpace(no))
		switch {
		case err == nil:
			out.Deleted++
			out.Messages += res.Messages
		case errors.Is(err, errConvMissing), errors.Is(err, errConvNotClosed):
			out.Skipped = append(out.Skipped, strings.TrimSpace(no))
		default:
			return out, err
		}
	}
	return out, nil
}

// PurgeConversations 清理结束时间早于 cutoff 的会话（保留期清理），单轮最多 limit 个，
// 返回扫描数、删除数。存储对象删除失败只留孤儿文件，不影响行删除。
func (s *Service) PurgeConversations(ctx context.Context, cutoff time.Time, limit int) (scanned, deleted int, err error) {
	if limit <= 0 {
		limit = 100
	}
	var nos []string
	if err := s.db.WithContext(ctx).Model(&Conversation{}).
		Where("status = ? AND closed_at IS NOT NULL AND closed_at < ?", StatusClosed, cutoff).
		Order("closed_at ASC").Limit(limit).Pluck("conv_no", &nos).Error; err != nil {
		return 0, 0, err
	}
	for _, no := range nos {
		res, derr := s.deleteOne(ctx, no)
		if derr != nil {
			if s.log != nil {
				s.log.Warn("im: purge conversation failed", "convNo", no, "err", derr.Error())
			}
			continue
		}
		deleted += res.Deleted
	}
	return len(nos), deleted, nil
}

// deleteOne 在一个事务里删掉会话、消息与附件行，事务外再删存储对象。
func (s *Service) deleteOne(ctx context.Context, convNo string) (DeleteResult, error) {
	var out DeleteResult
	if convNo == "" {
		return out, errConvMissing
	}
	var keys []string
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var c Conversation
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("conv_no = ?", convNo).Take(&c).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return errConvMissing
			}
			return err
		}
		if c.Status != StatusClosed {
			return errConvNotClosed
		}
		if err := tx.Model(&Attachment{}).Where("conv_id = ?", int64(c.ID)).Pluck("storage_key", &keys).Error; err != nil {
			return err
		}
		res := tx.Where("conv_id = ?", int64(c.ID)).Delete(&Message{})
		if res.Error != nil {
			return res.Error
		}
		out.Messages = res.RowsAffected
		if err := tx.Where("conv_id = ?", int64(c.ID)).Delete(&Attachment{}).Error; err != nil {
			return err
		}
		return tx.Where("id = ?", int64(c.ID)).Delete(&Conversation{}).Error
	})
	if err != nil {
		return out, err
	}
	out.Deleted = 1
	if s.store != nil {
		for _, key := range keys {
			if err := s.store.Delete(ctx, key); err != nil && s.log != nil {
				s.log.Warn("im: delete conversation attachment failed", "convNo", convNo, "key", key, "err", err.Error())
			}
		}
	}
	return out, nil
}

// Rate 由访客给已结束的会话打 1~5 分（一次性）。
func (s *Service) Rate(ctx context.Context, convNo string, score int, comment string) (*Conversation, error) {
	if score < 1 || score > 5 {
		return nil, response.BadRequest("im.rating_invalid")
	}
	comment = truncate(strings.TrimSpace(comment), 500)
	var c Conversation
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("conv_no = ?", strings.TrimSpace(convNo)).Take(&c).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return response.NotFound("im.conv_not_found")
			}
			return err
		}
		if c.Status != StatusClosed {
			return response.Conflict("im.rating_not_closed")
		}
		if c.Rating > 0 {
			return response.Conflict("im.rating_exists")
		}
		now := time.Now().UTC()
		c.Rating, c.RatingComment, c.RatedAt = score, comment, &now
		return tx.Model(&Conversation{}).Where("id = ?", int64(c.ID)).Updates(map[string]any{
			"rating": score, "rating_comment": comment, "rated_at": now,
		}).Error
	})
	if err != nil {
		return nil, err
	}
	s.publishControl(c.ConvNo, map[string]any{"type": "rated", "rating": score})
	return &c, nil
}

// QuickReplyList 返回全部快捷回复（按 sort、id 排序）。
func (s *Service) QuickReplyList(ctx context.Context) ([]QuickReply, error) {
	var rows []QuickReply
	err := s.db.WithContext(ctx).Order("sort ASC, id ASC").Limit(200).Find(&rows).Error
	return rows, err
}

// QuickReplySave 新建或更新一条快捷回复（id 为 0 时新建）。
func (s *Service) QuickReplySave(ctx context.Context, id idgen.ID, title, content string, sort int) (*QuickReply, error) {
	title, content = truncate(strings.TrimSpace(title), 64), truncate(strings.TrimSpace(content), 2000)
	if title == "" || content == "" {
		return nil, response.BadRequest("im.quick_reply_required")
	}
	if id == 0 {
		newID, err := idgen.Next()
		if err != nil {
			return nil, err
		}
		q := &QuickReply{Title: title, Content: content, Sort: sort}
		q.ID = newID
		if err := s.db.WithContext(ctx).Create(q).Error; err != nil {
			return nil, err
		}
		return q, nil
	}
	var q QuickReply
	if err := s.db.WithContext(ctx).Where("id = ?", int64(id)).Take(&q).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, response.NotFound("im.quick_reply_not_found")
		}
		return nil, err
	}
	q.Title, q.Content, q.Sort = title, content, sort
	if err := s.db.WithContext(ctx).Model(&QuickReply{}).Where("id = ?", int64(id)).
		Updates(map[string]any{"title": title, "content": content, "sort": sort}).Error; err != nil {
		return nil, err
	}
	return &q, nil
}

// QuickReplyDelete 删除一条快捷回复。
func (s *Service) QuickReplyDelete(ctx context.Context, id idgen.ID) error {
	return s.db.WithContext(ctx).Where("id = ?", int64(id)).Delete(&QuickReply{}).Error
}

// Typing 推送"正在输入"（不落库）：坐席端与访客端共用。
func (s *Service) Typing(convNo, senderType string) {
	if convNo = strings.TrimSpace(convNo); convNo == "" {
		return
	}
	s.publishControl(convNo, map[string]any{"type": "typing", "senderType": senderType})
}

// AutoClose 关闭空闲会话（最后一条消息早于 cutoff），单轮最多 limit 个，返回关闭数量。
func (s *Service) AutoClose(ctx context.Context, cutoff time.Time, limit int) (int, error) {
	if limit <= 0 {
		limit = 50
	}
	var nos []string
	if err := s.db.WithContext(ctx).Model(&Conversation{}).
		Where("status = ? AND COALESCE(last_msg_at, created_at) < ?", StatusOpen, cutoff).
		Order("id ASC").Limit(limit).Pluck("conv_no", &nos).Error; err != nil {
		return 0, err
	}
	n := 0
	for _, no := range nos {
		if _, _, err := s.Close(ctx, no, "auto"); err != nil {
			if s.log != nil {
				s.log.Warn("im: auto close failed", "convNo", no, "err", err.Error())
			}
			continue
		}
		n++
	}
	return n, nil
}

// publishControl 推送非消息类控制帧（typing / closed / read / assigned / rated）。
func (s *Service) publishControl(convNo string, payload map[string]any) {
	raw, err := json.Marshal(payload)
	if err != nil {
		return
	}
	s.push(convNo, raw)
}

func normalizeTags(raw string) string {
	parts := strings.FieldsFunc(raw, func(r rune) bool {
		return r == ',' || r == '，' || r == ';' || r == '；'
	})
	seen := map[string]bool{}
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = truncate(strings.TrimSpace(p), 24)
		if p == "" || seen[strings.ToLower(p)] {
			continue
		}
		seen[strings.ToLower(p)] = true
		out = append(out, p)
		if len(out) == 10 {
			break
		}
	}
	return strings.Join(out, ",")
}

func truncate(s string, n int) string {
	if len(s) > n {
		// 按字节截断可能切坏 UTF-8，退回到合法边界。
		for n > 0 && (s[n]&0xC0) == 0x80 {
			n--
		}
		return s[:n]
	}
	return s
}

// Conversation 按会话号取会话（附件上传前校验会话状态用）。
func (s *Service) Conversation(ctx context.Context, convNo string) (*Conversation, error) {
	var c Conversation
	if err := s.db.WithContext(ctx).Where("conv_no = ?", strings.TrimSpace(convNo)).Take(&c).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, response.NotFound("im.conv_not_found")
		}
		return nil, err
	}
	return &c, nil
}

// SaveAttachment 登记一个已写入存储的附件（key 是 storage 内的对象键）。
func (s *Service) SaveAttachment(ctx context.Context, convNo, uploader, name, mime string, size int64, key string) (*Attachment, error) {
	c, err := s.Conversation(ctx, convNo)
	if err != nil {
		return nil, err
	}
	id, err := idgen.Next()
	if err != nil {
		return nil, err
	}
	att := &Attachment{
		Token: randomToken(16), ConvID: c.ID, Uploader: uploader,
		Name: truncate(strings.TrimSpace(name), 255), Mime: truncate(mime, 128), Size: size, StorageKey: key,
	}
	att.ID = id
	if err := s.db.WithContext(ctx).Create(att).Error; err != nil {
		return nil, err
	}
	return att, nil
}

// Attachment 按 token 取附件（下载用）。
func (s *Service) Attachment(ctx context.Context, token string) (*Attachment, error) {
	var att Attachment
	if err := s.db.WithContext(ctx).Where("token = ?", strings.TrimSpace(token)).Take(&att).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, response.NotFound("im.attachment_invalid")
		}
		return nil, err
	}
	return &att, nil
}

// PurgeUnusedAttachments 清理超时未随消息发出的附件（先删对象再删行），返回清理数量。
func (s *Service) PurgeUnusedAttachments(ctx context.Context, cutoff time.Time, limit int) (int, error) {
	if limit <= 0 {
		limit = 100
	}
	var rows []Attachment
	if err := s.db.WithContext(ctx).Where("used = ? AND created_at < ?", false, cutoff).
		Order("id ASC").Limit(limit).Find(&rows).Error; err != nil {
		return 0, err
	}
	n := 0
	for i := range rows {
		if s.store != nil {
			if err := s.store.Delete(ctx, rows[i].StorageKey); err != nil && s.log != nil {
				s.log.Warn("im: delete attachment object failed", "key", rows[i].StorageKey, "err", err.Error())
				continue
			}
		}
		if err := s.db.WithContext(ctx).Where("id = ?", int64(rows[i].ID)).Delete(&Attachment{}).Error; err != nil {
			if s.log != nil {
				s.log.Warn("im: delete attachment row failed", "token", rows[i].Token, "err", err.Error())
			}
			continue
		}
		n++
	}
	return n, nil
}

// Agent 是可被转接的坐席（拥有 im:reply 或超管）。
type Agent struct {
	ID       idgen.ID `json:"id"`
	Username string   `json:"username"`
	Nickname string   `json:"nickname"`
	// Online 来自心跳（TTL 90 秒），用于转接列表与自动分配。
	Online bool `json:"online"`
}

// Agents 返回当前租户内能回复会话的坐席（按用户名排序）。
// 权限来自角色挂载的菜单按钮（sys_role_menu → sys_menu.perm）；超管直接放行。
func (s *Service) Agents(ctx context.Context) ([]Agent, error) {
	q := s.db.WithContext(ctx).Table("sys_user AS u").
		Select("DISTINCT u.id AS id, u.username AS username, u.nickname AS nickname").
		Joins("LEFT JOIN sys_user_role ur ON ur.user_id = u.id").
		Joins("LEFT JOIN sys_role r ON r.id = ur.role_id AND r.status = '1'").
		Joins("LEFT JOIN sys_role_menu rm ON rm.role_id = r.id").
		Joins("LEFT JOIN sys_menu m ON m.id = rm.menu_id").
		Where("u.deleted_at IS NULL AND u.status = '1' AND (u.is_super OR m.perm IN ?)", []string{"im:reply", "im:*", "*"})
	if tid, ok := tenant.From(ctx); ok {
		q = q.Where("u.tenant_id = ?", int64(tid))
	}
	var out []Agent
	if err := q.Order("u.username ASC").Limit(200).Scan(&out).Error; err != nil {
		return nil, err
	}
	ids := make([]string, len(out))
	for i := range out {
		ids[i] = strconv.FormatInt(int64(out[i].ID), 10)
	}
	online := s.onlineMap(ctx, ids)
	for i := range out {
		out[i].Online = online[ids[i]]
	}
	return out, nil
}

// presenceTTL 是在线状态的存活时间：前端每 60 秒心跳一次，90 秒没心跳即视为离线。
const presenceTTL = 90 * time.Second

func presenceKey(userID string) string { return "im:presence:" + userID }

// Heartbeat 记录坐席在线。
func (s *Service) Heartbeat(ctx context.Context, userID string) {
	if s.cache == nil || strings.TrimSpace(userID) == "" {
		return
	}
	_ = s.cache.Set(ctx, presenceKey(userID), "1", presenceTTL)
}

// onlineMap 批量查在线状态；没有缓存时视为全部在线（单实例部署也无需心跳）。
func (s *Service) onlineMap(ctx context.Context, ids []string) map[string]bool {
	out := make(map[string]bool, len(ids))
	if s.cache == nil {
		for _, id := range ids {
			out[id] = true
		}
		return out
	}
	for _, id := range ids {
		ok, err := s.cache.Exists(ctx, presenceKey(id))
		out[id] = err == nil && ok
	}
	return out
}

// autoAssign 新会话按设置分给在线且手上进行中会话最少的坐席；没有在线坐席就排队。
func (s *Service) autoAssign(ctx context.Context, c *Conversation) {
	if !s.Settings(ctx).AutoAssign {
		return
	}
	id, name, err := s.pickAgent(ctx)
	if err != nil || id == "" {
		return
	}
	res := s.db.WithContext(ctx).Model(&Conversation{}).Where("id = ? AND assignee_id = ''", int64(c.ID)).
		Updates(map[string]any{"assignee_id": id, "assignee_name": name})
	if res.Error != nil || res.RowsAffected == 0 {
		return
	}
	c.AssigneeID, c.AssigneeName = id, name
	s.publishControl(c.ConvNo, map[string]any{"type": "assigned", "assigneeId": id, "assigneeName": name})
}

// pickAgent 在在线坐席里挑进行中会话最少的一个（并列时按用户名序，取第一个）。
func (s *Service) pickAgent(ctx context.Context) (string, string, error) {
	agents, err := s.Agents(ctx)
	if err != nil {
		return "", "", err
	}
	type loadRow struct {
		AssigneeID string
		N          int64
	}
	var loads []loadRow
	if err := s.db.WithContext(ctx).Model(&Conversation{}).Select("assignee_id, COUNT(*) AS n").
		Where("status = ? AND assignee_id <> ''", StatusOpen).Group("assignee_id").Scan(&loads).Error; err != nil {
		return "", "", err
	}
	load := make(map[string]int64, len(loads))
	for _, l := range loads {
		load[l.AssigneeID] = l.N
	}
	bestID, bestName, bestLoad := "", "", int64(1<<62)
	for _, a := range agents {
		if !a.Online {
			continue
		}
		id := strconv.FormatInt(int64(a.ID), 10)
		if load[id] < bestLoad {
			name := a.Nickname
			if name == "" {
				name = a.Username
			}
			bestID, bestName, bestLoad = id, name, load[id]
		}
	}
	return bestID, bestName, nil
}

// AssignNext 坐席心跳后按先后领走一个排队中的会话（仅自动分配开启时），返回领取的会话号。
func (s *Service) AssignNext(ctx context.Context, agentID, agentName string) (string, error) {
	agentID = strings.TrimSpace(agentID)
	if agentID == "" || !s.Settings(ctx).AutoAssign {
		return "", nil
	}
	var c Conversation
	err := s.db.WithContext(ctx).Where("status = ? AND assignee_id = ''", StatusOpen).
		Order("created_at ASC").Take(&c).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return "", nil
		}
		return "", err
	}
	res := s.db.WithContext(ctx).Model(&Conversation{}).Where("id = ? AND assignee_id = ''", int64(c.ID)).
		Updates(map[string]any{"assignee_id": agentID, "assignee_name": truncate(strings.TrimSpace(agentName), 64)})
	if res.Error != nil || res.RowsAffected == 0 {
		return "", res.Error
	}
	s.publishControl(c.ConvNo, map[string]any{"type": "assigned", "assigneeId": agentID, "assigneeName": truncate(agentName, 64)})
	return c.ConvNo, nil
}

// Transfer 把会话转给指定坐席（覆盖原归属），并通知双方刷新。
func (s *Service) Transfer(ctx context.Context, convNo, userID, userName string) (*Conversation, error) {
	userID = strings.TrimSpace(userID)
	convNo = strings.TrimSpace(convNo)
	if userID == "" {
		return nil, response.BadRequest("im.agent_required")
	}
	res := s.db.WithContext(ctx).Model(&Conversation{}).Where("conv_no = ?", convNo).
		Updates(map[string]any{"assignee_id": userID, "assignee_name": truncate(strings.TrimSpace(userName), 64)})
	if res.Error != nil {
		return nil, res.Error
	}
	if res.RowsAffected == 0 {
		return nil, response.NotFound("im.conv_not_found")
	}
	var c Conversation
	if err := s.db.WithContext(ctx).Where("conv_no = ?", convNo).Take(&c).Error; err != nil {
		return nil, err
	}
	s.publishControl(c.ConvNo, map[string]any{"type": "assigned", "assigneeId": c.AssigneeID, "assigneeName": c.AssigneeName})
	return &c, nil
}

// ---- 统计 ----

// statsLimit 是一次统计最多纳入的会话数：超过就截断并标记 truncated，避免长窗口拖垮接口。
const statsLimit = 5000

type StatsDaily struct {
	Date    string `json:"date"`
	Created int    `json:"created"`
	Closed  int    `json:"closed"`
}

type StatsAgent struct {
	ID        string  `json:"id"`
	Name      string  `json:"name"`
	Online    bool    `json:"online"`
	Assigned  int     `json:"assigned"`
	Closed    int     `json:"closed"`
	AvgRating float64 `json:"avgRating"`
}

// Stats 是统计看板的数据：窗口内的会话数、结束数、首响、满意度、按天趋势与坐席工作量。
type Stats struct {
	Days                int          `json:"days"`
	Total               int          `json:"total"`
	Closed              int          `json:"closed"`
	Open                int          `json:"open"`
	AvgRating           float64      `json:"avgRating"`
	RatingCount         int          `json:"ratingCount"`
	AvgFirstResponseSec int64        `json:"avgFirstResponseSec"`
	FirstResponseCount  int          `json:"firstResponseCount"`
	Truncated           bool         `json:"truncated"`
	Daily               []StatsDaily `json:"daily"`
	Agents              []StatsAgent `json:"agents"`
}

// Stats 聚合窗口内的会话；首响按「访客第一条消息 → 坐席第一条回复」计算。
func (s *Service) Stats(ctx context.Context, days int) (*Stats, error) {
	if days < 1 {
		days = 7
	}
	if days > 90 {
		days = 90
	}
	now := time.Now()
	start := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location()).AddDate(0, 0, -(days - 1))
	out := &Stats{Days: days, Agents: []StatsAgent{}}
	daily := make([]StatsDaily, days)
	for i := 0; i < days; i++ {
		daily[i] = StatsDaily{Date: start.AddDate(0, 0, i).Format("2006-01-02")}
	}
	dayIdx := func(t time.Time) int {
		d := int(t.In(start.Location()).Sub(start) / (24 * time.Hour))
		if d < 0 || d >= days {
			return -1
		}
		return d
	}

	var convs []Conversation
	if err := s.db.WithContext(ctx).Where("created_at >= ?", start).Order("id ASC").Limit(statsLimit + 1).Find(&convs).Error; err != nil {
		return nil, err
	}
	if len(convs) > statsLimit {
		convs = convs[:statsLimit]
		out.Truncated = true
	}

	ratingSum, ratingCount := 0, 0
	agentRating := map[string]int{}
	agentRatingN := map[string]int{}
	perAgent := map[string]*StatsAgent{}
	for _, c := range convs {
		out.Total++
		if c.Status == StatusClosed {
			out.Closed++
		} else {
			out.Open++
		}
		if i := dayIdx(c.CreatedAt); i >= 0 {
			daily[i].Created++
		}
		if c.ClosedAt != nil {
			if i := dayIdx(*c.ClosedAt); i >= 0 {
				daily[i].Closed++
			}
		}
		if c.Rating > 0 {
			ratingSum += c.Rating
			ratingCount++
		}
		if c.AssigneeID != "" {
			st := perAgent[c.AssigneeID]
			if st == nil {
				st = &StatsAgent{ID: c.AssigneeID, Name: c.AssigneeName}
				perAgent[c.AssigneeID] = st
			}
			st.Assigned++
			if c.Status == StatusClosed {
				st.Closed++
			}
			if c.Rating > 0 {
				agentRating[c.AssigneeID] += c.Rating
				agentRatingN[c.AssigneeID]++
			}
		}
	}
	if ratingCount > 0 {
		out.AvgRating = float64(ratingSum) / float64(ratingCount)
	}
	out.RatingCount = ratingCount
	out.Daily = daily

	// 首响：按会话取访客首条与坐席首条的最早时间，再算差（跳过坐席先开口的会话）。
	type firstRow struct {
		ConvID idgen.ID
		At     time.Time
	}
	firsts := func(sender string) (map[idgen.ID]time.Time, error) {
		var rows []firstRow
		err := s.db.WithContext(ctx).Raw(
			`SELECT m.conv_id AS conv_id, MIN(m.created_at) AS at FROM plg_im_message m
			 JOIN plg_im_conversation c ON c.id = m.conv_id
			 WHERE c.created_at >= ? AND m.sender_type = ? GROUP BY m.conv_id`, start, sender).Scan(&rows).Error
		if err != nil {
			return nil, err
		}
		out := make(map[idgen.ID]time.Time, len(rows))
		for _, r := range rows {
			out[r.ConvID] = r.At
		}
		return out, nil
	}
	visitorFirst, err := firsts(SenderVisitor)
	if err != nil {
		return nil, err
	}
	agentFirst, err := firsts(SenderAgent)
	if err != nil {
		return nil, err
	}
	var sumSec, n int64
	for id, vt := range visitorFirst {
		at, ok := agentFirst[id]
		if !ok || at.Before(vt) {
			continue
		}
		sumSec += int64(at.Sub(vt).Seconds())
		n++
	}
	if n > 0 {
		out.AvgFirstResponseSec = sumSec / n
	}
	out.FirstResponseCount = int(n)

	agents, err := s.Agents(ctx)
	if err != nil {
		// 坐席名单只用于补全名字与在线状态，取不到不影响统计主体。
		if s.log != nil {
			s.log.Warn("im: load agents for stats failed", "err", err.Error())
		}
		agents = nil
	}
	for _, a := range agents {
		id := strconv.FormatInt(int64(a.ID), 10)
		st := perAgent[id]
		if st == nil {
			st = &StatsAgent{ID: id}
			perAgent[id] = st
		}
		if st.Name == "" {
			st.Name = a.Nickname
		}
		if st.Name == "" {
			st.Name = a.Username
		}
		st.Online = a.Online
	}
	for id, st := range perAgent {
		if agentRatingN[id] > 0 {
			st.AvgRating = float64(agentRating[id]) / float64(agentRatingN[id])
		}
		out.Agents = append(out.Agents, *st)
	}
	sort.Slice(out.Agents, func(i, j int) bool {
		if out.Agents[i].Assigned != out.Agents[j].Assigned {
			return out.Agents[i].Assigned > out.Agents[j].Assigned
		}
		return out.Agents[i].Name < out.Agents[j].Name
	})
	return out, nil
}

// ---- 撤回 / 合并 ----

// recallWindow 是坐席可撤回自己消息的时间窗口。
const recallWindow = 2 * time.Minute

// Recall 撤回坐席自己在窗口内发的一条消息：正文清空、改为 system + meta 标识、推控制帧。
func (s *Service) Recall(ctx context.Context, convNo string, seq int64, userID string) (*Message, error) {
	convNo = strings.TrimSpace(convNo)
	if convNo == "" || seq <= 0 {
		return nil, response.BadRequest("error.bad_request")
	}
	var (
		out  Message
		conv Conversation
	)
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("conv_no = ?", convNo).Take(&conv).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return response.NotFound("im.conv_not_found")
			}
			return err
		}
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("conv_id = ? AND seq = ?", int64(conv.ID), seq).Take(&out).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return response.NotFound("im.message_not_found")
			}
			return err
		}
		if out.SenderType != SenderAgent || out.SenderID != userID {
			return response.Conflict("im.message_not_recallable")
		}
		if out.RecalledAt != nil || time.Since(out.CreatedAt) > recallWindow {
			return response.Conflict("im.message_not_recallable")
		}
		now := time.Now().UTC()
		meta, _ := json.Marshal(map[string]string{"code": "message_recalled"})
		if err := tx.Model(&Message{}).Where("id = ?", int64(out.ID)).Updates(map[string]any{
			"content": "", "kind": KindSystem, "meta": string(meta), "recalled_at": now,
		}).Error; err != nil {
			return err
		}
		// 撤回的是最后一条：列表摘要不能再显示原文。
		if seq == conv.MsgSeq {
			return tx.Model(&Conversation{}).Where("id = ?", int64(conv.ID)).Update("last_preview", "").Error
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	out.Content, out.Kind = "", KindSystem
	out.RecalledAt = &conv.CreatedAt
	out.Meta = `{"code":"message_recalled"}`
	s.publishControl(conv.ConvNo, map[string]any{"type": "recalled", "seq": seq})
	return &out, nil
}

// MergeResult 是合并结果：目标会话号与被搬过去的消息数。
type MergeResult struct {
	Target string `json:"target"`
	Moved  int64  `json:"moved"`
}

// Merge 把源会话的消息与附件并入目标会话：源会话结束并留一条指向目标的系统提示。
// 目标必须是进行中的会话；两个会话按会话号定序加锁，避免反向并发合并时死锁。
func (s *Service) Merge(ctx context.Context, fromNo, toNo, agentID string) (*MergeResult, error) {
	fromNo, toNo = strings.TrimSpace(fromNo), strings.TrimSpace(toNo)
	if fromNo == "" || toNo == "" || fromNo == toNo {
		return nil, response.BadRequest("error.bad_request")
	}
	var moved int64
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		first, second := fromNo, toNo
		if first > second {
			first, second = second, first
		}
		var a, b Conversation
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("conv_no = ?", first).Take(&a).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return response.NotFound("im.conv_not_found")
			}
			return err
		}
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("conv_no = ?", second).Take(&b).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return response.NotFound("im.conv_not_found")
			}
			return err
		}
		src, target := a, b
		if first == fromNo {
			src, target = a, b
		} else {
			src, target = b, a
		}
		if target.Status != StatusOpen {
			return response.Conflict("im.conv_closed")
		}
		offset := target.MsgSeq
		res := tx.Exec("UPDATE plg_im_message SET conv_id = ?, seq = seq + ? WHERE conv_id = ?",
			int64(target.ID), offset, int64(src.ID))
		if res.Error != nil {
			return res.Error
		}
		moved = res.RowsAffected
		if err := tx.Exec("UPDATE plg_im_attachment SET conv_id = ? WHERE conv_id = ?",
			int64(target.ID), int64(src.ID)).Error; err != nil {
			return err
		}
		now := time.Now().UTC()
		mergedMeta, _ := json.Marshal(map[string]string{"code": "conv_merged", "from": src.ConvNo})
		sysID, err := idgen.Next()
		if err != nil {
			return err
		}
		newSeq := offset + src.MsgSeq + 1
		sys := Message{ConvID: target.ID, Seq: newSeq, SenderType: SenderSystem, Kind: KindSystem, Meta: string(mergedMeta), CreatedAt: now}
		sys.ID = sysID
		if err := tx.Create(&sys).Error; err != nil {
			return err
		}
		lastAt := target.LastMsgAt
		previewText := target.LastPreview
		if src.LastMsgAt != nil && (lastAt == nil || src.LastMsgAt.After(*lastAt)) {
			lastAt, previewText = src.LastMsgAt, src.LastPreview
		}
		if err := tx.Model(&Conversation{}).Where("id = ?", int64(target.ID)).Updates(map[string]any{
			"msg_seq": newSeq, "last_msg_at": lastAt, "last_preview": previewText,
		}).Error; err != nil {
			return err
		}
		// 源会话：结束 + 系统提示（告诉访客去哪儿了）
		intoMeta, _ := json.Marshal(map[string]string{"code": "conv_merged_into", "to": target.ConvNo})
		intoID, err := idgen.Next()
		if err != nil {
			return err
		}
		srcSeq := src.MsgSeq + 1
		note := Message{ConvID: src.ID, Seq: srcSeq, SenderType: SenderSystem, Kind: KindSystem, Meta: string(intoMeta), CreatedAt: now}
		note.ID = intoID
		if err := tx.Create(&note).Error; err != nil {
			return err
		}
		if err := tx.Model(&Conversation{}).Where("id = ?", int64(src.ID)).Updates(map[string]any{
			"status": StatusClosed, "closed_at": now, "closed_by": "merge",
			"msg_seq": srcSeq, "last_msg_at": now, "last_preview": "",
		}).Error; err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	s.publishControl(toNo, map[string]any{"type": "merged", "from": fromNo})
	s.publishControl(fromNo, map[string]any{"type": "closed", "closedBy": "merge"})
	return &MergeResult{Target: toNo, Moved: moved}, nil
}
