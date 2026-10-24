package notice

import (
	"encoding/json"
	"strings"
	"time"

	"nexadmin/core/datascope"
	"nexadmin/pkg/idgen"
)

// View 是公告的接口表示，字段与前端 Notice 一致。
type View struct {
	ID          idgen.ID        `json:"id"`
	Title       string          `json:"title"`
	Type        string          `json:"type"`
	Level       string          `json:"level"`
	Status      string          `json:"status"`
	Content     string          `json:"content"`
	Scope       string          `json:"scope"`
	TargetIDs   []idgen.ID      `json:"targetIds"`
	Pinned      bool            `json:"pinned"`
	Popup       bool            `json:"popup"`
	Platform    bool            `json:"platform"`
	PublishAt   *time.Time      `json:"publishAt"`
	ExpireAt    *time.Time      `json:"expireAt"`
	PublishedAt *time.Time      `json:"publishedAt"`
	Attachments json.RawMessage `json:"attachments"`
	ReadCount   int64           `json:"readCount"`
	TotalCount  int             `json:"totalCount"`
	CreatedAt   time.Time       `json:"createdAt"`
	UpdatedAt   time.Time       `json:"updatedAt"`
}

// Form 是新增/修改请求体（前端 NoticeForm）。
type Form struct {
	Title       string          `json:"title" binding:"required,max=200"`
	Type        string          `json:"type" binding:"required,max=32"`
	Level       string          `json:"level" binding:"required,max=32"`
	Content     string          `json:"content"`
	Scope       string          `json:"scope" binding:"required,oneof=all role dept user"`
	TargetIDs   []idgen.ID      `json:"targetIds"`
	Pinned      bool            `json:"pinned"`
	Popup       bool            `json:"popup"`
	Platform    bool            `json:"platform"`
	PublishAt   *string         `json:"publishAt"`
	ExpireAt    *string         `json:"expireAt"`
	Attachments json.RawMessage `json:"attachments"`
}

// ListQuery 是管理端列表的查询条件。
type ListQuery struct {
	Keyword  string
	Type     string
	Level    string
	Status   string
	Page     int
	PageSize int
	Scope    *datascope.Scope // 操作者的数据范围；nil = 不裁剪（仅内部调用），HTTP 入口必须设置
}

// PopupItem 是登录弹窗用的精简公告。
type PopupItem struct {
	ID          idgen.ID   `json:"id"`
	Title       string     `json:"title"`
	Content     string     `json:"content"`
	PublishedAt *time.Time `json:"publishedAt"`
	Type        string     `json:"type"`
	Level       string     `json:"level"`
}

// MyNotice 是“我的公告”（消息中心的公告标签页）。
type MyNotice struct {
	ID          idgen.ID        `json:"id"`
	Title       string          `json:"title"`
	Type        string          `json:"type"`
	Level       string          `json:"level"`
	Content     string          `json:"content"`
	Pinned      bool            `json:"pinned"`
	Read        bool            `json:"read"`
	PublishedAt *time.Time      `json:"publishedAt"`
	Attachments json.RawMessage `json:"attachments"`
}

// UnreadUser 是未读名单里的一行。
type UnreadUser struct {
	ID       idgen.ID `json:"id"`
	Username string   `json:"username"`
	Nickname string   `json:"nickname"`
	DeptName string   `json:"deptName,omitempty"`
}

// ReadStats 是已读/未读统计。
type ReadStats struct {
	ReadCount   int64        `json:"readCount"`
	UnreadCount int64        `json:"unreadCount"`
	UnreadUsers []UnreadUser `json:"unreadUsers"`
}

// parseTime 解析前端传来的时间；空串/nil 表示不设置。接受带时区的 ISO8601 和不带时区的本地写法（按 UTC）。
func parseTime(s *string) (*time.Time, bool) {
	if s == nil || strings.TrimSpace(*s) == "" {
		return nil, true
	}
	for _, layout := range []string{time.RFC3339Nano, "2006-01-02T15:04:05", "2006-01-02T15:04", "2006-01-02 15:04:05", "2006-01-02"} {
		if t, err := time.Parse(layout, strings.TrimSpace(*s)); err == nil {
			u := t.UTC()
			return &u, true
		}
	}
	return nil, false
}
