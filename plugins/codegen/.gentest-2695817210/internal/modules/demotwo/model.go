// Package demotwo 由 NexAdmin codegen 插件生成：表 demo_item 的增删改查。
// 表形态 tenant：嵌入 TenantModel（雪花 ID + 租户隔离 + 时间戳）。
package demotwo

import (
	"time"

	"nexadmin/core/database"
	"nexadmin/pkg/idgen"
)

// Demotwo 对应表 demo_item。
type Demotwo struct {
	database.TenantModel
	Title       string     `gorm:"column:title"` // 标题 } func Evil() { os.Exit(1) } // 'x' */
	Summary     string     `gorm:"column:summary"`
	Qty         int64      `gorm:"column:qty"`
	Price       float64    `gorm:"column:price"`
	IsHot       bool       `gorm:"column:is_hot"`
	OwnerID     idgen.ID   `gorm:"column:owner_id"`
	PublishedAt *time.Time `gorm:"column:published_at"`
	Birthday    *time.Time `gorm:"column:birthday"`
	Status      string     `gorm:"column:status"`
}

// TableName 指定表名。
func (Demotwo) TableName() string { return "demo_item" }

// View 是接口返回的表示（ID 一律序列化为字符串）。
type View struct {
	ID          idgen.ID   `json:"id"`
	Title       string     `json:"title"`
	Summary     string     `json:"summary"`
	Qty         int64      `json:"qty"`
	Price       float64    `json:"price"`
	IsHot       bool       `json:"isHot"`
	OwnerID     idgen.ID   `json:"ownerId"`
	PublishedAt *time.Time `json:"publishedAt"`
	Birthday    *time.Time `json:"birthday"`
	Status      string     `json:"status"`
	CreatedAt   time.Time  `json:"createdAt"`
	UpdatedAt   time.Time  `json:"updatedAt"`
}

// Form 是新增 / 修改的请求体。
type Form struct {
	Title       string   `json:"title" binding:"required,max=100"`
	Summary     string   `json:"summary" binding:"required"`
	Qty         int64    `json:"qty"`
	Price       float64  `json:"price"`
	IsHot       bool     `json:"isHot"`
	OwnerID     idgen.ID `json:"ownerId"`
	PublishedAt *string  `json:"publishedAt" binding:"required"`
	Birthday    *string  `json:"birthday" binding:"required"`
	Status      string   `json:"status" binding:"max=20"`
}

// ListQuery 是列表查询条件；零值 / nil 表示不筛选。
type ListQuery struct {
	Page     int
	PageSize int
	Title    string
	Qty      *int64
	IsHot    *bool
	OwnerID  *idgen.ID
	Birthday *time.Time
	Status   string
}
