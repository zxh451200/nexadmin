// Package note 由 NexAdmin codegen 插件生成：表 plain_note 的增删改查。
// 表形态 plain：显式 ID。
package note

import (
	"nexadmin/pkg/idgen"
)

// Note 对应表 plain_note。
type Note struct {
	ID   idgen.ID `gorm:"column:id;primaryKey;autoIncrement:false"`
	Name string   `gorm:"column:name"`
}

// TableName 指定表名。
func (Note) TableName() string { return "plain_note" }

// View 是接口返回的表示（ID 一律序列化为字符串）。
type View struct {
	ID   idgen.ID `json:"id"`
	Name string   `json:"name"`
}

// Form 是新增 / 修改的请求体。
type Form struct {
	Name string `json:"name" binding:"required,max=50"`
}

// ListQuery 是列表查询条件；零值 / nil 表示不筛选。
type ListQuery struct {
	Page     int
	PageSize int
	Name     string
}
