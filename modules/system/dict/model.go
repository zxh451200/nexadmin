// Package dict 是字典模块：字典类型 + 字典数据（平台级，不分租户），含排序与前端下拉/标签取数接口。
package dict

import (
	"nexadmin/core/database"
	"nexadmin/pkg/idgen"
)

// Type 对应表 sys_dict_type，JSON 字段与前端 DictTypeRow 一致。
type Type struct {
	database.PlatformModel
	Name   string `gorm:"size:100;not null" json:"name"`
	Code   string `gorm:"size:64;not null" json:"code"`
	Sort   int    `gorm:"not null" json:"sort"`
	Status string `gorm:"size:1;not null" json:"status"` // "1" 启用 "0" 停用
	Remark string `gorm:"size:500;not null" json:"remark,omitempty"`
	// Builtin 是派生标记：编码在出厂清单 seedTypes 里即为 true；不入库，读出时计算。
	Builtin bool `gorm:"-" json:"builtin"`
}

// TableName 指定表名。
func (Type) TableName() string { return "sys_dict_type" }

// Data 对应表 sys_dict_data，JSON 字段与前端 DictDataRow 一致。
// 列名 dict_label / dict_value 避开各引擎的关键字。
// 译文走 i18n_json（统一契约 pkg/i18n）；LabelEn 是升级前的老列，只读不写，作为英文兜底。
type Data struct {
	database.PlatformModel
	TypeID   idgen.ID `gorm:"not null" json:"typeId"`
	Label    string   `gorm:"column:dict_label;size:100;not null" json:"label"`
	LabelEn  string   `gorm:"size:100;not null" json:"-"`
	I18nJSON string   `gorm:"type:text" json:"i18nJson"`
	Value    string   `gorm:"column:dict_value;size:100;not null" json:"value"`
	Tag      string   `gorm:"size:20;not null" json:"tag"` // default|success|warning|destructive|info
	Sort     int      `gorm:"not null" json:"sort"`
	Status   string   `gorm:"size:1;not null" json:"status"`
	Remark   string   `gorm:"size:500;not null" json:"remark,omitempty"`
}

// TableName 指定表名。
func (Data) TableName() string { return "sys_dict_data" }

// TypeInput 是字典类型的新增/修改请求体。
type TypeInput struct {
	Name   string `json:"name" binding:"required,max=100"`
	Code   string `json:"code" binding:"required,max=64"`
	Sort   int    `json:"sort"`
	Status string `json:"status" binding:"omitempty,oneof=0 1"`
	Remark string `json:"remark" binding:"max=500"`
}

// DataInput 是字典数据的新增/修改请求体。
type DataInput struct {
	TypeID   idgen.ID `json:"typeId" binding:"required"`
	Label    string   `json:"label" binding:"required,max=100"`
	I18nJSON string   `json:"i18nJson"`
	Value    string   `json:"value" binding:"required,max=100"`
	Tag      string   `json:"tag" binding:"omitempty,oneof=default success warning destructive info"`
	Sort     int      `json:"sort"`
	Status   string   `json:"status" binding:"omitempty,oneof=0 1"`
	Remark   string   `json:"remark" binding:"max=500"`
}

// ListQuery 是两个列表接口共用的查询参数（typeId 仅字典数据用）。
type ListQuery struct {
	Keyword string `form:"keyword"`
	Status  string `form:"status"` // 逗号分隔，可多值
	TypeID  string `form:"typeId"`
}

// IDsInput 是排序请求体：ids 为新顺序。
type IDsInput struct {
	IDs []idgen.ID `json:"ids" binding:"required"`
}

// Item 是 GET /dicts/:code/items 的返回项（前端 DictItem）。
type Item struct {
	Value string `json:"value"`
	Label string `json:"label"`
	Tag   string `json:"tag,omitempty"`
}
