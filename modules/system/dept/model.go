// Package dept 是部门模块：树形部门（parent_id + 物化路径 path 双写），按租户隔离。
package dept

import (
	"nexadmin/core/database"
	"nexadmin/pkg/idgen"
)

// Dept 对应表 sys_dept，JSON 字段与前端 SysDept 一致。
// Path 形如 "/1/4/"，"本部门及以下" = path LIKE '/1/4/%'，三引擎行为一致，不用递归查询。
type Dept struct {
	database.TenantModel
	Name       string    `gorm:"size:100;not null" json:"name"`
	ParentID   *idgen.ID `json:"parentId"` // 根部门为 null
	Path       string    `gorm:"size:512;not null" json:"-"`
	Sort       int       `gorm:"not null" json:"sort"`
	LeaderID   *idgen.ID `json:"leaderId,omitempty"`
	LeaderName string    `gorm:"size:64;not null" json:"leaderName,omitempty"` // 无用户模块可联表，由写入方回填
	Phone      string    `gorm:"size:32;not null" json:"phone"`
	Email      string    `gorm:"size:100;not null" json:"email"`
	Status     string    `gorm:"size:1;not null" json:"status"` // "1" 启用 "0" 停用
	Children   []*Dept   `gorm:"-" json:"children,omitempty"`
	// Readonly 仅在 scene=manage 的裁剪树里出现：为维持树结构而保留的范围外祖先节点，前端置灰、不可操作。
	Readonly bool `gorm:"-" json:"readonly,omitempty"`
}

// TableName 指定表名。
func (Dept) TableName() string { return "sys_dept" }

// Input 是新增/修改的请求体（前端 SysDeptInput）。
type Input struct {
	Name     string    `json:"name" binding:"required,max=100"`
	ParentID *idgen.ID `json:"parentId"`
	Sort     int       `json:"sort"`
	LeaderID *idgen.ID `json:"leaderId"`
	Phone    string    `json:"phone" binding:"max=32"`
	Email    string    `json:"email" binding:"max=100"`
	Status   string    `json:"status" binding:"omitempty,oneof=0 1"`
}

// SortInput 是同级排序请求体：ids 为该父节点下全部子部门的新顺序。
type SortInput struct {
	ParentID *idgen.ID  `json:"parentId"`
	IDs      []idgen.ID `json:"ids" binding:"required"`
}

// MoveInput 是拖拽移动部门的请求体（可跨层级）。
type MoveInput struct {
	ParentID *idgen.ID `json:"parentId"`
	// AfterID 指定插到哪个同级之后：空串=插到最前，省略=追加到末尾。
	AfterID *string `json:"afterId"`
}
