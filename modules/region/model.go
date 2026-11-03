package region

import (
	"time"

	"nexadmin/core/database"
)

// Snapshot 对应表 sys_region_snapshot：同步下来的省市区整棵树只存一行（id=1）。
// 没有行时用 core/region 里随包发布的内置数据。
type Snapshot struct {
	database.PlatformModel
	SourceURL   string    `gorm:"size:512;not null"`
	DataDate    string    `gorm:"column:data_date;size:16;not null"`
	License     string    `gorm:"size:128;not null"`
	Payload     string    `gorm:"type:text;not null"`
	SHA256      string    `gorm:"column:sha256;size:64;not null"`
	Provinces   int       `gorm:"not null"`
	Prefectures int       `gorm:"not null"`
	Counties    int       `gorm:"not null"`
	SyncedBy    string    `gorm:"column:synced_by;size:64;not null"`
	SyncedAt    time.Time `gorm:"column:synced_at;not null"`
}

// TableName 指定表名。
func (Snapshot) TableName() string { return "sys_region_snapshot" }

// SnapshotVersion 对应表 sys_region_snapshot_version：覆盖 sys_region_snapshot 之前
// 存下的旧快照，一条版本就是当时那一行（整棵树 + 摘要 + 计数 + 来源）。
// 与快照表同样是平台级单数据集（全平台共用一份区划数据），所以没有 tenant_id，也不按行发号。
type SnapshotVersion struct {
	database.PlatformModel
	Version     int    `gorm:"column:version;not null"`
	Payload     string `gorm:"type:text;not null" json:"-"` // 整棵树不外泄，列表/详情各自用下面的视图
	SHA256      string `gorm:"column:sha256;size:64;not null"`
	DataDate    string `gorm:"column:data_date;size:16;not null"`
	SourceURL   string `gorm:"size:512;not null"`
	Provinces   int    `gorm:"not null"`
	Prefectures int    `gorm:"not null"`
	Counties    int    `gorm:"not null"`
	CreatedBy   string `gorm:"column:created_by;size:64;not null"`
}

// TableName 指定表名。
func (SnapshotVersion) TableName() string { return "sys_region_snapshot_version" }
