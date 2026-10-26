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
	AgentReadSeq int64      `gorm:"not null;default:0" json:"-"`
	LastMsgAt    *time.Time `json:"lastMsgAt,omitempty"`
	LastPreview  string     `gorm:"size:120" json:"lastPreview"`
}

func (Conversation) TableName() string { return "plg_im_conversation" }

// Message 是一条聊天消息；content 只支持文本（P0），富媒体留给 P1。
type Message struct {
	database.PlatformModel
	ConvID     idgen.ID `gorm:"not null;uniqueIndex:uk_plg_im_msg_seq,priority:1" json:"-"`
	Seq        int64    `gorm:"not null;uniqueIndex:uk_plg_im_msg_seq,priority:2" json:"seq"`
	SenderType string   `gorm:"size:16;not null" json:"senderType"`
	// SenderID 是发送方标识：visitor 侧由宿主给（会员 ID 或匿名令牌），agent 侧是管理员 ID。
	SenderID   string    `gorm:"size:64" json:"senderId"`
	SenderName string    `gorm:"size:64" json:"senderName"`
	Content    string    `gorm:"size:2000;not null" json:"content"`
	CreatedAt  time.Time `json:"createdAt"`
}

func (Message) TableName() string { return "plg_im_message" }
