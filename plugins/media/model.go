package media

import (
	"time"

	"nexadmin/core/database"
)

// VideoConfig 是 Cloudflare Stream 的接入配置（插件级，平台共享）。
type VideoConfig struct {
	Enabled   bool
	AccountID string
	Token     string
	Subdomain string
}

// VideoConfigView 是下发给前端的配置：令牌只回「有没有」，值永远不回显。
type VideoConfigView struct {
	Enabled   bool   `json:"enabled"`
	AccountID string `json:"accountId"`
	Subdomain string `json:"subdomain"`
	HasToken  bool   `json:"hasToken"`
}

// Video 是一段托管在外部流媒体上的视频台账。
type Video struct {
	database.TenantModel
	Name       string `gorm:"column:name;size:255"`
	Size       int64  `gorm:"column:size"`
	Provider   string `gorm:"column:provider;size:32"`
	VideoID    string `gorm:"column:video_id;size:64"`
	Playback   string `gorm:"column:playback_url;size:500"`
	Status     string `gorm:"column:status;size:20"`
	UploadedBy string `gorm:"column:uploaded_by;size:100"`
}

func (Video) TableName() string { return "plg_media_video" }

type VideoItem struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Size      int64     `json:"size"`
	Provider  string    `json:"provider"`
	VideoID   string    `json:"videoId"`
	Playback  string    `json:"playbackUrl"`
	Download  string    `json:"downloadUrl"`
	Status    string    `json:"status"`
	Uploader  string    `json:"uploader"`
	CreatedAt time.Time `json:"createdAt"`
}

// setting 是 plg_media_setting 的一行。
type setting struct {
	Key   string `gorm:"column:config_key;primaryKey"`
	Value string `gorm:"column:config_value"`
}

func (setting) TableName() string { return "plg_media_setting" }

type videoUploadReq struct {
	Name string `json:"name" binding:"required,max=255"`
	Size int64  `json:"size" binding:"min=0"`
}

type videoSaveReq struct {
	VideoID string `json:"videoId" binding:"required,max=64"`
	Name    string `json:"name" binding:"required,max=255"`
	Size    int64  `json:"size" binding:"min=0"`
}

type configReq struct {
	Enabled   bool   `json:"enabled"`
	AccountID string `json:"accountId" binding:"max=64"`
	Token     string `json:"token" binding:"max=200"`
	Subdomain string `json:"subdomain" binding:"max=100"`
}
