package aigateway

import (
	"nexadmin/core/database"
)

// 模型种类（源自 nexRag ragmodel；engine 是 RAG 应用自己的适配器概念，不进网关）。
const (
	KindLLM       = "llm"
	KindEmbedding = "embedding"
	KindRerank    = "rerank"
	KindASR       = "asr"
	KindTTS       = "tts"
	KindOCR       = "ocr"
)

// KindSet 是合法种类集合。
var KindSet = map[string]bool{
	KindLLM: true, KindEmbedding: true, KindRerank: true,
	KindASR: true, KindTTS: true, KindOCR: true,
}

// Model 对应表 plg_aigateway_model：租户级模型接入配置（密钥集中）。
// APIKeyEnc 用 core/secret 整体加密存储，任何读接口只回打码值。
type Model struct {
	database.TenantModel
	Kind      string `gorm:"size:16;not null;index" json:"kind"`
	Name      string `gorm:"size:200;not null" json:"name"`
	Provider  string `gorm:"size:64;not null;default:''" json:"provider"`
	BaseURL   string `gorm:"size:500;not null;default:''" json:"baseUrl"`
	Model     string `gorm:"size:255;not null;default:''" json:"model"`
	APIKeyEnc string `gorm:"size:1024" json:"-"`
	Dimension int    `gorm:"not null;default:0" json:"dimension"`
	Enabled   bool   `gorm:"not null;default:true" json:"enabled"`
	IsDefault bool   `gorm:"not null;default:false" json:"isDefault"`
}

func (Model) TableName() string { return "plg_aigateway_model" }

// Usage 对应表 plg_aigateway_usage：按租户的 token 用量台账（计费的原始事实）。
// 金额换算（单价 × tokens）留给结算层，台账只记事实。
type Usage struct {
	database.TenantModel
	ModelID          int64  `gorm:"not null;default:0;index" json:"modelId"`
	Kind             string `gorm:"size:16;not null" json:"kind"`
	Provider         string `gorm:"size:64;not null;default:''" json:"provider"`
	Model            string `gorm:"size:255;not null;default:''" json:"model"`
	PromptTokens     int64  `gorm:"not null;default:0" json:"promptTokens"`
	CompletionTokens int64  `gorm:"not null;default:0" json:"completionTokens"`
	TotalTokens      int64  `gorm:"not null;default:0" json:"totalTokens"`
	// BizType/BizNo 是调用方的关联键（如 rag/chat-123），网关不解释语义。
	BizType string `gorm:"size:32;not null;default:''" json:"bizType"`
	BizNo   string `gorm:"size:64;not null;default:''" json:"bizNo"`
}

func (Usage) TableName() string { return "plg_aigateway_usage" }
