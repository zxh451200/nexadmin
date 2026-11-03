// Package post 是岗位模块：简单 CRUD，同租户内岗位编码唯一，可关联部门。
package post

import (
	"gorm.io/gorm"

	"nexadmin/core/database"
	"nexadmin/pkg/idgen"
)

// Post 对应表 sys_post，JSON 字段与前端 SysPost 一致。
type Post struct {
	database.TenantModel
	Name   string    `gorm:"size:100;not null" json:"name"`
	Code   string    `gorm:"size:64;not null" json:"code"`
	DeptID *idgen.ID `json:"deptId,omitempty"`
	Sort   int       `gorm:"not null" json:"sort"`
	Status string    `gorm:"size:1;not null" json:"status"` // "1" 启用 "0" 停用
	Remark string    `gorm:"size:500;not null" json:"remark,omitempty"`
	// DeletedAt 软删除列：删除即进回收站（recbin.TypePost）。
	// 同租户内 code 的唯一索引（uk_sys_post_tenant_code）已降级为普通索引，
	// 唯一性由 codeTaken 保证——否则软删除行会永久占住编码，还原时也永远撞键。
	DeletedAt gorm.DeletedAt `gorm:"index" json:"-"`
}

// TableName 指定表名。
func (Post) TableName() string { return "sys_post" }

// Input 是新增/修改的请求体（前端 SysPostInput）。
type Input struct {
	Name   string    `json:"name" binding:"required,max=100"`
	Code   string    `json:"code" binding:"required,max=64"`
	DeptID *idgen.ID `json:"deptId"`
	Sort   int       `json:"sort"`
	Status string    `json:"status" binding:"omitempty,oneof=0 1"`
	Remark string    `json:"remark" binding:"max=500"`
}

// SortInput 是排序请求体：ids 为全部岗位的新顺序（parentId 恒为 null，仅为与前端对齐）。
type SortInput struct {
	IDs []idgen.ID `json:"ids" binding:"required"`
}

// Option 是 GET /posts/options 的返回项。
type Option struct {
	ID   idgen.ID `json:"id"`
	Name string   `json:"name"`
}
