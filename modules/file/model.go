package file

import (
	"time"

	"nexadmin/core/database"
	"nexadmin/pkg/idgen"
)

// File 对应 sys_file。嵌入 TenantModel：雪花主键 + 自动租户隔离。
// StoragePath 不为空表示文件在本地存储；为空时 URL 是外部地址。GroupID 为 0 表示未分组。
type File struct {
	database.TenantModel
	GroupID     idgen.ID `gorm:"column:group_id"`
	Name        string   `gorm:"column:name;size:255"`
	StoragePath string   `gorm:"column:storage_path;size:500"`
	URL         string   `gorm:"column:url"`
	PosterURL   string   `gorm:"column:poster_url"`
	MimeType    string   `gorm:"column:mime_type;size:150"`
	Category    string   `gorm:"column:category;size:20"`
	Size        int64    `gorm:"column:size"`
	Hash        string   `gorm:"column:hash;size:64"`
	Visibility  string   `gorm:"column:visibility;size:10"`
	Token       string   `gorm:"column:token;size:32"`
	Uploader    string   `gorm:"column:uploader;size:100"`
}

func (File) TableName() string { return "sys_file" }

// Group 对应 sys_file_group。
type Group struct {
	database.TenantModel
	Name string `gorm:"column:name;size:100"`
}

func (Group) TableName() string { return "sys_file_group" }

// ---- 对外 DTO：字段与前端 api.ts 完全一致，ID 一律字符串 ----

// FileItem 对应前端 FileItem。
type FileItem struct {
	ID         string    `json:"id"`
	Name       string    `json:"name"`
	URL        string    `json:"url"`
	PosterURL  string    `json:"posterUrl,omitempty"`
	Type       string    `json:"type"`
	Category   string    `json:"category"`
	Size       int64     `json:"size"`
	Visibility string    `json:"visibility"`
	GroupID    string    `json:"groupId"` // 空字符串 = 未分组
	Uploader   string    `json:"uploader"`
	CreatedAt  time.Time `json:"createdAt"`
}

// GroupItem 对应前端 FileGroup。
type GroupItem struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Count int64  `json:"count"`
}

type groupReq struct {
	Name string `json:"name" binding:"required,max=100"`
}

type registerFile struct {
	Name      string `json:"name" binding:"required,max=255"`
	URL       string `json:"url"`
	PosterURL string `json:"posterUrl"`
	Type      string `json:"type"`
	Size      int64  `json:"size"`
}

type registerReq struct {
	Files      []registerFile `json:"files" binding:"required,min=1,max=100,dive"`
	GroupID    idgen.ID       `json:"groupId"`
	Visibility string         `json:"visibility"`
}

type updateReq struct {
	Name       *string   `json:"name"`
	Visibility *string   `json:"visibility"`
	GroupID    *idgen.ID `json:"groupId"`
}

type moveReq struct {
	IDs     []idgen.ID `json:"ids" binding:"required,min=1"`
	GroupID idgen.ID   `json:"groupId"`
}

type listQuery struct {
	Keyword  string `form:"keyword"`
	Category string `form:"category"`
	GroupID  string `form:"groupId"` // "none" = 未分组
	From     string `form:"from"`
	To       string `form:"to"`
}
