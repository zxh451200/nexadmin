package kb

// 模型与常量。文档与切块都是平台级表（无 tenant_id）：客服知识库是整个部署主体对外
// 应答的统一口径，跟 plg_im_* 同一套归属。

import (
	"time"

	"nexadmin/core/database"
	"nexadmin/pkg/idgen"
)

// 文档状态。
const (
	StatusReady  = "ready"
	StatusFailed = "failed"
)

// Document 是一份已向量化（或失败）的知识文档。
type Document struct {
	database.PlatformModel
	Title string `gorm:"size:200;not null" json:"title"`
	// SourceName 是上传时的原始文件名（展示用，不参与检索）。
	SourceName string `gorm:"size:255;not null;default:''" json:"sourceName"`
	Mime       string `gorm:"size:128;not null;default:''" json:"mime"`
	Size       int64  `gorm:"not null;default:0" json:"size"`
	// Status：ready=可检索；failed=向量化失败（error 里带原因，可删除后重传）。
	Status string `gorm:"size:16;not null;index" json:"status"`
	// ChunkCount 是切块数；Model/Dimension 记录向量化时的模型与维度：
	// 检索只用「model 与当前配置的 embedding 模型一致」的文档，换模型后旧文档要重新向量化。
	ChunkCount int    `gorm:"not null;default:0" json:"chunkCount"`
	Model      string `gorm:"size:128;not null;default:''" json:"model"`
	Dimension  int    `gorm:"not null;default:0" json:"dimension"`
	Error      string `gorm:"size:500;not null;default:''" json:"error,omitempty"`
	CreatedAt  time.Time `json:"createdAt"`
	UpdatedAt  time.Time `json:"updatedAt"`
}

func (Document) TableName() string { return "plg_kb_document" }

// Chunk 是一个文本块及其向量（JSON 浮点数组文本）。
type Chunk struct {
	database.PlatformModel
	DocumentID idgen.ID `gorm:"not null;uniqueIndex:uk_plg_kb_chunk_doc,priority:1" json:"-"`
	Seq        int      `gorm:"not null;uniqueIndex:uk_plg_kb_chunk_doc,priority:2" json:"-"`
	Content    string   `gorm:"type:text;not null" json:"content"`
	// Vector 是 JSON 浮点数组文本；列类型按引擎在迁移里定义（MySQL/MariaDB MEDIUMTEXT、PG TEXT）。
	Vector string `gorm:"type:text;not null" json:"-"`
}

func (Chunk) TableName() string { return "plg_kb_chunk" }
