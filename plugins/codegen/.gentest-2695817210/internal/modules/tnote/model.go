// Package tnote 由 NexAdmin codegen 插件生成：表 tenant_note 的增删改查。
// 表形态 plain：显式 ID 与 TenantID。
package tnote

import (
	"nexadmin/pkg/idgen"
)

// Tnote 对应表 tenant_note。
type Tnote struct {
	ID       idgen.ID `gorm:"column:id;primaryKey;autoIncrement:false"`
	TenantID idgen.ID `gorm:"column:tenant_id;not null"`
	Content  string   `gorm:"column:content"`
	Level    int64    `gorm:"column:level"`
}

// TableName 指定表名。
func (Tnote) TableName() string { return "tenant_note" }

// View 是接口返回的表示（ID 一律序列化为字符串）。
type View struct {
	ID      idgen.ID `json:"id"`
	Content string   `json:"content"`
	Level   int64    `json:"level"`
}

// Form 是新增 / 修改的请求体。
type Form struct {
	Content string `json:"content" binding:"required"`
	Level   int64  `json:"level"`
}

// ListQuery 是列表查询条件；零值 / nil 表示不筛选。
type ListQuery struct {
	Page     int
	PageSize int
}
