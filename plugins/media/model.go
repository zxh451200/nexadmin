package media

import (
	"time"

	"nexadmin/core/database"
)

// 支持的视频托管厂商。新增一家 = 在 provider.go 加一个实现 + 这里加常量。
const (
	ProviderCloudflare = "cloudflare"
	ProviderBunny      = "bunny"
	// 阿里云 VOD / 腾讯云 VOD：暂只支持登记已有视频（直传需厂商 SDK + 转码状态机）。
	ProviderAliyun  = "aliyun"
	ProviderTencent = "tencent"
)

// VideoConfig 是视频托管配置（插件级，平台共享）。字段按 Provider 解释：
//
//	cloudflare: KeyA=AccountID  KeyB=API Token   KeyC=播放子域
//	bunny:      KeyA=LibraryID  KeyB=API Key     KeyC=Pull Zone 子域
type VideoConfig struct {
	Enabled  bool
	Provider string
	KeyA     string
	KeyB     string
	KeyC     string
}

// VideoConfigView 下发给前端：密钥只回「有没有」，值永远不回显。
type VideoConfigView struct {
	Enabled  bool   `json:"enabled"`
	Provider string `json:"provider"`
	KeyA     string `json:"keyA"`
	KeyC     string `json:"keyC"`
	HasKeyB  bool   `json:"hasKeyB"`
	// CanUpload 为 false 时前端隐藏上传，改用「登记已有视频」。
	CanUpload bool `json:"canUpload"`
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
	Enabled  bool   `json:"enabled"`
	Provider string `json:"provider" binding:"max=32"`
	KeyA     string `json:"keyA" binding:"max=200"`
	KeyB     string `json:"keyB" binding:"max=300"`
	KeyC     string `json:"keyC" binding:"max=100"`
}
