package notice

import (
	"encoding/json"
	"strings"
	"time"

	"gorm.io/gorm"

	"nexadmin/core/database"
	"nexadmin/pkg/htmlsafe"
	"nexadmin/pkg/idgen"
)

// 公告状态、发布范围。
const (
	StatusDraft     = "draft"
	StatusPublished = "published"
	StatusRevoked   = "revoked"

	ScopeAll  = "all"
	ScopeRole = "role"
	ScopeDept = "dept"
	ScopeUser = "user"
)

// Notice 对应 sys_notice（软删除，gorm 自动过滤 deleted_at）。
type Notice struct {
	database.TenantModel
	Title       string `gorm:"size:200;not null"`
	Type        string `gorm:"size:32;not null"`
	Level       string `gorm:"size:32;not null"`
	Status      string `gorm:"size:16;not null"`
	Content     string `gorm:"type:text"`
	Scope       string `gorm:"size:16;not null"`
	Pinned      bool
	Popup       bool
	Platform    bool // 平台公告：全部租户可见
	PublishAt   *time.Time
	ExpireAt    *time.Time
	PublishedAt *time.Time
	Attachments string `gorm:"type:text"` // JSON 数组
	TotalCount  int    // 发布时的应读人数快照
	CreatedBy   idgen.ID
	DeletedAt   gorm.DeletedAt `gorm:"index"`
}

func (Notice) TableName() string { return "sys_notice" }

// Target 对应 sys_notice_target：发布对象（角色/部门/用户 ID）。
type Target struct {
	database.TenantModel
	NoticeID idgen.ID `gorm:"not null;index"`
	TargetID idgen.ID `gorm:"not null"`
}

func (Target) TableName() string { return "sys_notice_target" }

// Read 对应 sys_notice_read：已读只记一行，不给每个人复制公告。
type Read struct {
	database.TenantModel
	NoticeID idgen.ID `gorm:"not null"`
	UserID   idgen.ID `gorm:"not null"`
	ReadAt   time.Time
}

func (Read) TableName() string { return "sys_notice_read" }

// Clear 对应 sys_notice_clear：用户“清空我的公告”的时间点，之前发布的公告对该用户隐身。
type Clear struct {
	database.TenantModel
	UserID    idgen.ID `gorm:"not null"`
	ClearedAt time.Time
}

func (Clear) TableName() string { return "sys_notice_clear" }

// attachmentsJSON 保证附件始终是合法 JSON 数组。
func attachmentsJSON(s string) json.RawMessage {
	if strings.TrimSpace(s) == "" || !json.Valid([]byte(s)) {
		return json.RawMessage("[]")
	}
	return json.RawMessage(s)
}

// sanitize 在落库前对公告富文本做服务端白名单净化（pkg/htmlsafe）；读取时不再处理。
func sanitize(html string) string { return htmlsafe.Sanitize(html) }
