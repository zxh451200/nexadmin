package ticket

import (
	"time"

	"nexadmin/core/database"
	"nexadmin/pkg/idgen"
)

// 工单状态：open → processing → resolved → closed；
// resolved/closed 后报告人再回复自动重开为 processing（异步售后场景的常态）。
const (
	StatusOpen       = "open"
	StatusProcessing = "processing"
	StatusResolved   = "resolved"
	StatusClosed     = "closed"
)

// 优先级。
const (
	PriorityLow    = "low"
	PriorityNormal = "normal"
	PriorityHigh   = "high"
	PriorityUrgent = "urgent"
)

// 回复人类型。
const (
	ReplierReporter = "reporter" // 报告人（访客/会员，宿主侧身份）
	ReplierAgent    = "agent"    // 坐席（管理员/客服）
	ReplierSystem   = "system"   // 系统备注（预留）
)

// ValidPriority 报告优先级是否合法。
func ValidPriority(p string) bool {
	return p == PriorityLow || p == PriorityNormal || p == PriorityHigh || p == PriorityUrgent
}

// Settleable 报告工单是否处于待处理状态（坐席视角的"未完结"）。
func Settleable(status string) bool {
	return status == StatusOpen || status == StatusProcessing
}

// Ticket 是一张工单（平台级表）。biz_type + biz_no 可选关联宿主业务对象（如订单号）；
// 报告人身份由宿主侧给出（visitorToken/会员 ID/后台用户）。
type Ticket struct {
	database.PlatformModel
	TicketNo string `gorm:"size:40;not null;uniqueIndex:uk_plg_ticket_no" json:"ticketNo"`
	BizType  string `gorm:"size:32;not null;default:'';index" json:"bizType"`
	BizNo    string `gorm:"size:64;not null;default:'';index" json:"bizNo"`
	Title    string `gorm:"size:200;not null" json:"title"`
	Status   string `gorm:"size:16;not null;index" json:"status"`
	Priority string `gorm:"size:16;not null;index" json:"priority"`
	// ReporterType：visitor（宿主前台客户）| user（后台用户自建）。
	ReporterType string   `gorm:"size:16;not null" json:"reporterType"`
	ReporterID   string   `gorm:"size:64" json:"reporterId"`
	ReporterName string   `gorm:"size:64" json:"reporterName"`
	AssigneeID   idgen.ID `gorm:"not null;default:0" json:"assigneeId"` // 0 = 未认领
	AssigneeName string   `gorm:"size:64" json:"assigneeName"`
	// ReplySeq 是回复的单调序号（回帖时行锁下 +1），客户端用 since=seq 增量拉取。
	ReplySeq        int64      `gorm:"not null;default:0" json:"-"`
	FirstReplyAt    *time.Time `json:"firstReplyAt,omitempty"` // 坐席首次响应时间（SLA 统计的原料）
	LastReplyAt     *time.Time `json:"lastReplyAt,omitempty"`
	LastReplierType string     `gorm:"size:16" json:"lastReplierType"`
	LastPreview     string     `gorm:"size:120" json:"lastPreview"`
	// DueAt 是 SLA 响应到期时间（按优先级定，见 SLADurations）；Overdue 由清扫器标记。
	// 坐席每次回复重置时钟（等待报告人的钟重新起算）。
	DueAt   *time.Time `json:"dueAt,omitempty"`
	Overdue bool       `gorm:"not null;default:0" json:"overdue"`
}

func (Ticket) TableName() string { return "plg_ticket" }

// SLADurations 是各优先级的响应时限（工程参考值，非标准强制；坐席回复后重置）。
func SLADurations() map[string]time.Duration {
	return map[string]time.Duration{
		PriorityUrgent: 2 * time.Hour,
		PriorityHigh:   8 * time.Hour,
		PriorityNormal: 24 * time.Hour,
		PriorityLow:    72 * time.Hour,
	}
}

// SLAFor 返回优先级对应的响应时限；未知优先级按 normal。
func SLAFor(priority string) time.Duration {
	if d, ok := SLADurations()[priority]; ok {
		return d
	}
	return SLADurations()[PriorityNormal]
}

// TicketReply 是一条回复；content 只支持文本（P0），附件留给 P1。
type TicketReply struct {
	database.PlatformModel
	TicketID    idgen.ID  `gorm:"not null;uniqueIndex:uk_plg_ticket_reply_seq,priority:1" json:"-"`
	Seq         int64     `gorm:"not null;uniqueIndex:uk_plg_ticket_reply_seq,priority:2" json:"seq"`
	ReplierType string    `gorm:"size:16;not null" json:"replierType"`
	ReplierID   string    `gorm:"size:64" json:"replierId"`
	ReplierName string    `gorm:"size:64" json:"replierName"`
	Content     string    `gorm:"size:2000;not null" json:"content"`
	CreatedAt   time.Time `json:"createdAt"`
}

func (TicketReply) TableName() string { return "plg_ticket_reply" }
