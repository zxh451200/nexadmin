// Package tag 由 NexAdmin codegen 插件生成：表 dict_tag 的增删改查。
// 表形态 platform：嵌入 PlatformModel（雪花 ID + 时间戳）。
package tag

import (
	"time"

	"nexadmin/core/database"
	"nexadmin/pkg/idgen"
)

// Tag 对应表 dict_tag。
type Tag struct {
	database.PlatformModel
	Label string `gorm:"column:label"`
}

// TableName 指定表名。
func (Tag) TableName() string { return "dict_tag" }

// View 是接口返回的表示（ID 一律序列化为字符串）。
type View struct {
	ID        idgen.ID  `json:"id"`
	Label     string    `json:"label"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

// Form 是新增 / 修改的请求体。
type Form struct {
	Label string `json:"label" binding:"required,max=255"`
}

// ListQuery 是列表查询条件；零值 / nil 表示不筛选。
type ListQuery struct {
	Page     int
	PageSize int
	Label    string
}
