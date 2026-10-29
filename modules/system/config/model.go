// Package config 是系统参数模块：平台级 KV 参数（sys_config）、上传限制，以及设置页 /settings/* 分区配置（sys_setting）。
package config

import (
	"nexadmin/core/database"
)

// 参数值类型。
const (
	TypeString  = "string"
	TypeNumber  = "number"
	TypeBoolean = "boolean"
	TypeJSON    = "json"
)

// Config 对应表 sys_config，JSON 字段与前端 ConfigRow 一致。
// 列名 config_key / config_value / config_type 避开各引擎的关键字。
type Config struct {
	database.PlatformModel
	Name     string `gorm:"size:100;not null" json:"name"`
	Key      string `gorm:"column:config_key;size:128;not null" json:"key"`
	Value    string `gorm:"column:config_value;type:text;not null" json:"value"`
	Type     string `gorm:"column:config_type;size:10;not null" json:"type"`
	IsPublic bool   `gorm:"not null" json:"isPublic"`
	Remark   string `gorm:"size:500;not null" json:"remark,omitempty"`
}

// TableName 指定表名。
func (Config) TableName() string { return "sys_config" }

// Setting 对应表 sys_setting：设置页每个分区（site/security/cache/…）一行 JSON。
type Setting struct {
	database.PlatformModel
	Section string `gorm:"size:32;not null"`
	Content string `gorm:"type:text;not null"`
}

// TableName 指定表名。
func (Setting) TableName() string { return "sys_setting" }

// Input 是参数新增/修改的请求体（前端 ConfigInput）。
type Input struct {
	Name     string `json:"name" binding:"required,max=100"`
	Key      string `json:"key" binding:"required,max=128"`
	Value    string `json:"value"`
	Type     string `json:"type" binding:"required,oneof=string number boolean json"`
	IsPublic bool   `json:"isPublic"`
	Remark   string `json:"remark" binding:"max=500"`
}

// ListQuery 是 GET /configs 的查询参数。
type ListQuery struct {
	Keyword  string `form:"keyword"`
	Type     string `form:"type"`     // 逗号分隔，可多值
	IsPublic string `form:"isPublic"` // true|false
}

// 上传限制对应的参数键（0 表示不限制）。
const (
	KeyUploadImage = "upload.max_size_image_mb"
	KeyUploadVideo = "upload.max_size_video_mb"
	KeyUploadFile  = "upload.max_size_file_mb"
	KeyUploadTypes = "upload.allowed_types"
	// KeyUploadWebp 开启后，图片上传会额外生成一份 WebP 副本（原图保留，按 Accept 下发）。
	KeyUploadWebp = "upload.webp_variant"
)

// UploadSettings 是上传限制（前端 UploadSettings）。
type UploadSettings struct {
	MaxImageMb   float64  `json:"maxImageMb"`
	MaxVideoMb   float64  `json:"maxVideoMb"`
	MaxFileMb    float64  `json:"maxFileMb"`
	AllowedTypes []string `json:"allowedTypes"`
	ImageWebp    bool     `json:"imageWebp"`
}

// TypeOption 是 GET /configs/upload-types 的返回项。
type TypeOption struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}
