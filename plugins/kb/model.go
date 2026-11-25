package kb

// 模型与常量。文档与切块都是平台级表（无 tenant_id）：客服知识库是整个部署主体对外
// 应答的统一口径，跟 plg_im_* 同一套归属。

import (
	"database/sql"
	"time"

	"nexadmin/core/database"
	"nexadmin/pkg/idgen"
)

// 文档状态。pending/processing 是异步入库的中间态：上传请求只建档，
// 切块与向量化在后台 worker 里做，列表轮询看状态即可。
const (
	StatusPending    = "pending"
	StatusProcessing = "processing"
	StatusReady      = "ready"
	StatusFailed     = "failed"
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
	// Fingerprint 是原文归一化后的 sha256：同一内容重传覆盖同一篇，不再重复入库稀释 topK。
	Fingerprint string `gorm:"size:64;not null;default:'';index:idx_plg_kb_doc_fp" json:"-"`
	// Stage 是异步入库的进度说明（"切分中" / "向量化 12/40 块"），失败后留空。
	Stage string `gorm:"size:128;not null;default:''" json:"stage,omitempty"`
	// SourceText 是抽取出的原文（不参与检索、不外发）：改分块参数重新切分时以它为输入。
	// 003 之前入库的老文档为 NULL，代码回退到「把切块拼回去」。
	SourceText sql.NullString `gorm:"column:source_text;type:text" json:"-"`
	CreatedAt  time.Time      `json:"createdAt"`
	UpdatedAt  time.Time      `json:"updatedAt"`
}

func (Document) TableName() string { return "plg_kb_document" }

// Chunk 是一个文本块及其向量（JSON 浮点数组文本）。
type Chunk struct {
	database.PlatformModel
	DocumentID idgen.ID `gorm:"not null;uniqueIndex:uk_plg_kb_chunk_doc,priority:1" json:"-"`
	Seq        int      `gorm:"not null;uniqueIndex:uk_plg_kb_chunk_doc,priority:2" json:"-"`
	Content    string   `gorm:"type:text;not null" json:"content"`
	// Enabled=false 的块不参与检索，但保留在库里可随时恢复（改坏了不必整篇重传）。
	Enabled bool `gorm:"not null;default:1" json:"enabled"`
	// Vector 是 JSON 浮点数组文本；列类型按引擎在迁移里定义（MySQL/MariaDB MEDIUMTEXT、PG TEXT）。
	Vector string `gorm:"type:text;not null" json:"-"`
}

func (Chunk) TableName() string { return "plg_kb_chunk" }

// 未命中问题池状态：pending=待补，resolved=已转成 QA 文档，ignored=已忽略。
const (
	MissPending  = "pending"
	MissResolved = "resolved"
	MissIgnored  = "ignored"
)

// Miss 是一条「知识库答不上来」的访客问题。平台级表，与文档同归属。
// NormHash 唯一：同一问题（忽略空白 / 大小写 / 首尾标点）只攒一条，热度看 AskCount。
// DocumentID 指向「转 QA」后生成的文档；再次未命中会重开为 pending 并清掉它。
type Miss struct {
	database.PlatformModel
	Question   string    `gorm:"size:1000;not null" json:"question"`
	NormHash   string    `gorm:"size:64;not null;uniqueIndex:uk_plg_kb_miss_norm" json:"-"`
	AskCount   int       `gorm:"not null;default:0" json:"askCount"`
	Status     string    `gorm:"size:16;not null;index:idx_plg_kb_miss_status" json:"status"`
	DocumentID idgen.ID  `gorm:"not null;default:0" json:"documentId"`
	LastConvNo string    `gorm:"size:64;not null;default:''" json:"-"`
	FirstAt    time.Time `json:"firstAt"`
	LastAt     time.Time `json:"lastAt"`
}

func (Miss) TableName() string { return "plg_kb_miss" }
