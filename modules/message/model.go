package message

import (
	"time"

	"nexadmin/core/database"
	"nexadmin/pkg/idgen"
)

// Message 对应 sys_message。
type Message struct {
	database.TenantModel
	UserID   idgen.ID `gorm:"not null;index"`
	Category string   `gorm:"size:32;not null"`
	Title    string   `gorm:"size:200;not null"`
	Content  string   `gorm:"type:text"`
	Link     string   `gorm:"size:500"`
	ReadAt   *time.Time
}

func (Message) TableName() string { return "sys_message" }

// View 是接口表示，字段与前端 Message 一致。
type View struct {
	ID        idgen.ID   `json:"id"`
	Category  string     `json:"category"`
	Title     string     `json:"title"`
	Content   string     `json:"content"`
	Link      string     `json:"link,omitempty"`
	ReadAt    *time.Time `json:"readAt"`
	CreatedAt time.Time  `json:"createdAt"`
}

// UnreadCount 是铃铛红点：未读消息数 + 未读公告数。
type UnreadCount struct {
	Messages int64 `json:"messages"`
	Notices  int64 `json:"notices"`
}

// ListQuery 是消息列表的查询条件。
type ListQuery struct {
	Unread   bool
	Category string
	Keyword  string
	// From / To 是 '2006-01-02' 的收件时间区间（含首尾两天）；留空不限。
	From     string
	To       string
	Page     int
	PageSize int
}

func toView(m Message) View {
	return View{ID: m.ID, Category: m.Category, Title: m.Title, Content: m.Content, Link: m.Link, ReadAt: m.ReadAt, CreatedAt: m.CreatedAt}
}
