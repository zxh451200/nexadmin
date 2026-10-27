package aigateway

import (
	"time"

	"nexadmin/pkg/idgen"
)

// ModelView 是模型配置视图：密钥只回打码值。
type ModelView struct {
	ID        idgen.ID  `json:"id"`
	Kind      string    `json:"kind"`
	Name      string    `json:"name"`
	Provider  string    `json:"provider"`
	BaseURL   string    `json:"baseUrl"`
	Model     string    `json:"model"`
	APIKey    string    `json:"apiKey"` // 已设置时返回 ********，否则空
	Dimension int       `json:"dimension"`
	Enabled   bool      `json:"enabled"`
	IsDefault bool      `json:"isDefault"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

func modelView(m *Model) ModelView {
	key := ""
	if m.APIKeyEnc != "" {
		key = secretMask
	}
	return ModelView{
		ID: m.ID, Kind: m.Kind, Name: m.Name, Provider: m.Provider, BaseURL: m.BaseURL,
		Model: m.Model, APIKey: key, Dimension: m.Dimension, Enabled: m.Enabled,
		IsDefault: m.IsDefault, CreatedAt: m.CreatedAt, UpdatedAt: m.UpdatedAt,
	}
}

// PingResult 是连通性测试结果。
type PingResult struct {
	OK       bool   `json:"ok"`
	Status   int    `json:"status"`
	LatencyMs int64 `json:"latencyMs"`
	Detail   string `json:"detail,omitempty"`
}
