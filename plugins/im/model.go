package im

import (
	"time"

	"nexadmin/core/database"
	"nexadmin/pkg/idgen"
)

// 会话状态与消息发送方。
const (
	StatusOpen   = "open"
	StatusClosed = "closed"

	SenderVisitor = "visitor" // 客户侧（游客/会员，用 visitorToken 标识）
	SenderAgent   = "agent"   // 宿主系统坐席（管理员/客服）
	SenderSystem  = "system"  // 系统提示（预留）
)

// Conversation 是一个客服会话（平台级表：会话挂在宿主系统的业务对象上）。
// biz_type + biz_no + visitor_token 三元组幂等：同一访客对同一业务对象只有一个会话；
// biz 为空表示通用咨询会话（biz_type/biz_no 留空串）。
type Conversation struct {
	database.PlatformModel
	ConvNo       string `gorm:"size:40;not null;uniqueIndex:uk_plg_im_conv_no" json:"convNo"`
	BizType      string `gorm:"size:32;not null;uniqueIndex:uk_plg_im_conv_biz,priority:1" json:"bizType"`
	BizNo        string `gorm:"size:64;not null;uniqueIndex:uk_plg_im_conv_biz,priority:2" json:"bizNo"`
	VisitorToken string `gorm:"size:64;not null;uniqueIndex:uk_plg_im_conv_biz,priority:3" json:"visitorToken"`
	VisitorName  string `gorm:"size:64" json:"visitorName"`
	Subject      string `gorm:"size:200" json:"subject"`
	Status       string `gorm:"size:16;not null;index" json:"status"`
	// MsgSeq 是会话内消息的单调序号（发消息时行锁下 +1），客户端用 since=seq 增量拉取。
	MsgSeq int64 `gorm:"not null;default:0" json:"-"`
	// AgentReadSeq 是坐席已读到的序号；未读数 = visitor 消息中 seq > agent_read_seq 的条数。
	AgentReadSeq int64 `gorm:"not null;default:0" json:"-"`
	// VisitorReadSeq 是访客已读到的序号；坐席据此显示自己消息的"已读"。
	VisitorReadSeq int64      `gorm:"not null;default:0" json:"visitorReadSeq"`
	LastMsgAt      *time.Time `json:"lastMsgAt,omitempty"`
	LastPreview    string     `gorm:"size:120" json:"lastPreview"`

	// 结束态：closed_at / closed_by（坐席 ID，auto 表示空闲自动关闭）。
	ClosedAt *time.Time `json:"closedAt,omitempty"`
	ClosedBy string     `gorm:"size:64;not null;default:''" json:"closedBy,omitempty"`
	// 认领：空串表示未分配；坐席认领或首次回复后落人。
	AssigneeID   string `gorm:"size:64;not null;default:''" json:"assigneeId,omitempty"`
	AssigneeName string `gorm:"size:64;not null;default:''" json:"assigneeName,omitempty"`
	// 满意度：1~5；0 表示未评价。
	Rating        int        `gorm:"not null;default:0" json:"rating"`
	RatingComment string     `gorm:"size:500;not null;default:''" json:"ratingComment,omitempty"`
	RatedAt       *time.Time `json:"ratedAt,omitempty"`
	// Tags 是逗号分隔的标签（去重、最多 10 个），列表按 LIKE 过滤。
	Tags string `gorm:"size:255;not null;default:''" json:"tags,omitempty"`
	// Note 是坐席内部备注（仅后台可见，不发访客），用于交接与质检。
	Note string `gorm:"size:500;not null;default:''" json:"note,omitempty"`
	// 访客来源：仅记录最近一次打开会话时的 IP / UA / 来源页，用于坐席判断上下文。
	VisitorIP  string `gorm:"size:45;not null;default:''" json:"visitorIp,omitempty"`
	VisitorUA  string `gorm:"size:255;not null;default:''" json:"visitorUa,omitempty"`
	VisitorRef string `gorm:"size:255;not null;default:''" json:"visitorRef,omitempty"`
}

func (Conversation) TableName() string { return "plg_im_conversation" }

// Message 是一条聊天消息。Content 目前仍是文本；Kind 区分普通消息与系统提示，
// Meta 承载系统提示的机器可读载荷（如 {"code":"conv_closed"}），供多语言前端渲染。
type Message struct {
	database.PlatformModel
	ConvID     idgen.ID `gorm:"not null;uniqueIndex:uk_plg_im_msg_seq,priority:1" json:"-"`
	Seq        int64    `gorm:"not null;uniqueIndex:uk_plg_im_msg_seq,priority:2" json:"seq"`
	SenderType string   `gorm:"size:16;not null" json:"senderType"`
	// SenderID 是发送方标识：visitor 侧由宿主给（会员 ID 或匿名令牌），agent 侧是管理员 ID。
	SenderID   string    `gorm:"size:64" json:"senderId"`
	SenderName string    `gorm:"size:64" json:"senderName"`
	Content    string    `gorm:"size:2000;not null" json:"content"`
	Kind       string    `gorm:"size:16;not null" json:"kind"`
	Meta       string    `gorm:"type:text" json:"meta,omitempty"`
	// RecalledAt 是消息被撤回的时间；撤回后正文清空、kind 改为 system 并用 meta.code 标识。
	RecalledAt *time.Time `json:"recalledAt,omitempty"`
	CreatedAt  time.Time  `json:"createdAt"`
}

// Kind 取值：普通文本（默认）与系统提示。系统提示内容为空，由 meta.code 决定前端文案。
const (
	KindText   = "text"
	KindSystem = "system"
	KindImage  = "image"
	KindFile   = "file"

	// MetaCodeConvClosed 是"会话已结束"系统消息的 meta.code。
	MetaCodeConvClosed = "conv_closed"
)

// QuickReply 是坐席共享的快捷回复话术。
type QuickReply struct {
	database.PlatformModel
	Title   string `gorm:"size:64;not null" json:"title"`
	Content string `gorm:"size:2000;not null" json:"content"`
	Sort    int    `gorm:"not null;default:0" json:"sort"`
}

func (QuickReply) TableName() string { return "plg_im_quick_reply" }

// Attachment 是会话内上传的图片/文件；消息 meta 里引用 token，正文仍在 message 表。
// Uploader 区分 visitor / agent；Used 标记是否已随消息发出（未被引用的附件会被定期清理）。
type Attachment struct {
	database.PlatformModel
	Token      string   `gorm:"size:40;not null;uniqueIndex:uk_plg_im_att_token" json:"token"`
	ConvID     idgen.ID `gorm:"not null;index" json:"-"`
	Uploader   string   `gorm:"size:16;not null" json:"uploader"`
	Name       string   `gorm:"size:255;not null" json:"name"`
	Mime       string   `gorm:"size:128;not null" json:"mime"`
	Size       int64    `gorm:"not null" json:"size"`
	StorageKey string   `gorm:"size:255;not null" json:"-"`
	Used       bool     `gorm:"not null;default:false" json:"-"`
}

func (Attachment) TableName() string { return "plg_im_attachment" }

func (Message) TableName() string { return "plg_im_message" }
