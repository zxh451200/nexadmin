package formbuilder

import (
	"time"

	"nexadmin/core/database"
	"nexadmin/pkg/idgen"
)

// 表单状态。
const (
	StatusDraft     = "draft"     // 从未发布
	StatusPublished = "published" // 已发布，可填写
	StatusOffline   = "offline"   // 已下线（保留版本与数据，暂停填写）
)

// MaxVersions 是单个表单的发布版本数上限，防止无限增长。
const MaxVersions = 200

// Form 对应 plg_formbuilder_form。草稿放在 DraftDef，发布后的定义只在 Version 表里。
type Form struct {
	database.TenantModel
	Name             string     `gorm:"size:100;not null" json:"name"`
	Description      string     `gorm:"size:500;not null" json:"description"`
	Status           string     `gorm:"size:16;not null" json:"status"`
	DraftDef         string     `gorm:"type:text" json:"-"`
	DraftRev         int        `gorm:"not null" json:"draftRev"`
	PublishedRev     int        `gorm:"not null" json:"publishedRev"`
	PublishedVersion int        `gorm:"not null" json:"publishedVersion"`
	PublishedAt      *time.Time `json:"publishedAt"`
	Audience         string     `gorm:"type:text" json:"-"`
	AllowRepeat      bool       `json:"allowRepeat"` // 注意：不要加 gorm default 标签，否则 false 会被默认值覆盖
	DeadlineAt       *time.Time `json:"deadlineAt"`
	MaxSubmissions   int        `gorm:"not null" json:"maxSubmissions"`
	SubmissionCount  int64      `gorm:"not null" json:"submissionCount"`
	CreatedBy        idgen.ID   `gorm:"not null" json:"createdBy"`
}

func (Form) TableName() string { return "plg_formbuilder_form" }

// Version 对应 plg_formbuilder_version：发布时冻结的定义。没有任何更新路径，改版 = 新增一行。
type Version struct {
	database.TenantModel
	FormID     idgen.ID `gorm:"not null" json:"formId"`
	Version    int      `gorm:"not null" json:"version"`
	Definition string   `gorm:"type:text;not null" json:"-"`
	CreatedBy  idgen.ID `gorm:"not null" json:"createdBy"`
}

func (Version) TableName() string { return "plg_formbuilder_version" }

// Submission 对应 plg_formbuilder_submission。
type Submission struct {
	database.TenantModel
	FormID    idgen.ID `gorm:"not null" json:"formId"`
	Version   int      `gorm:"not null" json:"version"`
	UserID    idgen.ID `gorm:"not null" json:"userId"`
	Username  string   `gorm:"size:64;not null" json:"username"`
	Data      string   `gorm:"type:text;not null" json:"-"`
	DedupeKey string   `gorm:"size:128;not null" json:"-"`
	IdemKey   string   `gorm:"size:64;not null" json:"-"`
}

func (Submission) TableName() string { return "plg_formbuilder_submission" }

// PublishMeta 是发布时同步到表单行的摘要（列表与"待填"统计用，权威数据仍是版本里的定义）。
type PublishMeta struct {
	Name           string
	Description    string
	Rev            int // 被发布的草稿修订号
	AllowRepeat    bool
	DeadlineAt     *time.Time
	MaxSubmissions int
}

// FormQuery 是表单列表的查询条件。
type FormQuery struct {
	Keyword  string
	Status   string
	Page     int
	PageSize int
}

// SubQuery 是提交列表/导出的查询条件。Field/Value 是按字段内容筛选（在内存里匹配，见 service）。
type SubQuery struct {
	FormID   idgen.ID
	Version  int
	Username string
	From     *time.Time
	To       *time.Time
	Field    string
	Value    string
	Page     int
	PageSize int
}
