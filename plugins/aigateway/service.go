package aigateway

import (
	"context"
	"errors"
	"strings"
	"time"

	"gorm.io/gorm"

	"nexadmin/core/event"
	"nexadmin/core/response"
	"nexadmin/core/secret"
	"nexadmin/pkg/idgen"
)

// secretMask 与前端打码值一致；apiKey 为空或等于打码值表示保持原密钥。
const secretMask = "********"

// ErrDisabled 表示该类模型未配置或未启用（宿主侧据此降级，如 embedding 缺失退化为关键词检索）。
var ErrDisabled = errors.New("aigateway: model kind not configured or disabled")

// Config 是解析后的调用配置：宿主用它构造自己的模型请求（OpenAI 兼容为主）。
// APIKey 已解密，宿主不得落日志、不得回传给前端。
type Config struct {
	Provider  string
	BaseURL   string
	APIKey    string
	Model     string
	Dimension int
}

// Enabled 报告配置是否可用。
func (c Config) Enabled() bool {
	return strings.TrimSpace(c.BaseURL) != "" && strings.TrimSpace(c.Model) != ""
}

// Service 是 aigateway 的业务层。
type Service struct {
	db  *gorm.DB
	log logger
}

// logger 是 Service 需要的最小日志面。
type logger interface {
	Info(msg string, keysAndValues ...any)
	Warn(msg string, keysAndValues ...any)
	Error(msg string, keysAndValues ...any)
}

// NewService 创建服务。
func NewService(db *gorm.DB, log logger) *Service { return &Service{db: db, log: log} }

// ModelInput 是模型配置入参。
type ModelInput struct {
	Kind      string
	Name      string
	Provider  string
	BaseURL   string
	Model     string
	APIKey    string // 空或打码值 = 保持原密钥
	Dimension int
	Enabled   bool
	IsDefault bool
}

func (in *ModelInput) normalize() error {
	in.Kind, in.Name = strings.TrimSpace(in.Kind), strings.TrimSpace(in.Name)
	in.Provider, in.BaseURL, in.Model = strings.TrimSpace(in.Provider), strings.TrimSpace(in.BaseURL), strings.TrimSpace(in.Model)
	if !KindSet[in.Kind] {
		return response.BadRequest("aigateway.kind_invalid")
	}
	if in.Name == "" {
		return response.BadRequest("aigateway.name_required")
	}
	if in.BaseURL != "" {
		// 保存前做不联网的 SSRF 静态校验（连通性测试时再解析域名兜底）。
		if _, err := validateURLStatic(in.BaseURL); err != nil {
			return response.BadRequest("aigateway.baseurl_blocked").WithMsg(err.Error())
		}
	}
	return nil
}

// ListModels 分页列出模型配置（密钥打码）。
func (s *Service) ListModels(ctx context.Context, keyword, kind string, page, pageSize int) ([]ModelView, int64, error) {
	db := s.db.WithContext(ctx).Model(&Model{})
	if kw := strings.TrimSpace(keyword); kw != "" {
		like := "%" + kw + "%"
		db = db.Where("name LIKE ? OR model LIKE ?", like, like)
	}
	if k := strings.TrimSpace(kind); k != "" {
		db = db.Where("kind = ?", k)
	}
	var total int64
	if err := db.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var rows []Model
	if err := db.Order("id DESC").Offset((page - 1) * pageSize).Limit(pageSize).Find(&rows).Error; err != nil {
		return nil, 0, err
	}
	out := make([]ModelView, len(rows))
	for i := range rows {
		out[i] = modelView(&rows[i])
	}
	return out, total, nil
}

// GetModel 返回单个配置（密钥打码）。
func (s *Service) GetModel(ctx context.Context, id idgen.ID) (*ModelView, error) {
	m, err := s.get(ctx, id)
	if err != nil {
		return nil, err
	}
	v := modelView(m)
	return &v, nil
}

// SaveModel 新建（id=0）或修改；同 kind 只保留一个默认：设默认时清掉同 kind 其余默认位。
func (s *Service) SaveModel(ctx context.Context, id idgen.ID, in ModelInput) (*ModelView, error) {
	if err := in.normalize(); err != nil {
		return nil, err
	}
	var m Model
	var cur *Model
	if id != 0 {
		c, err := s.get(ctx, id)
		if err != nil {
			return nil, err
		}
		m, cur = *c, c
	}
	m.Kind, m.Name, m.Provider, m.BaseURL, m.Model = in.Kind, in.Name, in.Provider, in.BaseURL, in.Model
	m.Dimension, m.Enabled, m.IsDefault = in.Dimension, in.Enabled, in.IsDefault
	switch {
	case in.APIKey == "" || in.APIKey == secretMask:
		if cur != nil {
			m.APIKeyEnc = cur.APIKeyEnc // 保持既有密钥
		}
	default:
		enc, err := secretEncrypt(in.APIKey)
		if err != nil {
			return nil, err
		}
		m.APIKeyEnc = enc
	}
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if id == 0 {
			return tx.Create(&m).Error
		}
		return tx.Model(&Model{}).Where("id = ?", int64(id)).Updates(map[string]any{
			"kind": m.Kind, "name": m.Name, "provider": m.Provider, "base_url": m.BaseURL,
			"model": m.Model, "api_key_enc": m.APIKeyEnc, "dimension": m.Dimension,
			"enabled": m.Enabled, "is_default": m.IsDefault,
		}).Error
	})
	if err != nil {
		return nil, err
	}
	if m.IsDefault {
		// 默认位互斥：同 kind 其余行清掉 is_default。
		s.db.WithContext(ctx).Model(&Model{}).
			Where("kind = ? AND is_default = ? AND id <> ?", m.Kind, true, int64(m.ID)).
			Update("is_default", false)
	}
	v := modelView(&m)
	return &v, nil
}

// DeleteModel 删除模型配置。
func (s *Service) DeleteModel(ctx context.Context, id idgen.ID) error {
	return s.db.WithContext(ctx).Delete(&Model{}, id).Error
}

// Resolve 返回某类里默认启用的模型配置（密钥已解密）；没有则 ErrDisabled。
// 这是"多供应商路由"的接缝：调用方只声明要哪类能力，网关决定用哪家。
func (s *Service) Resolve(ctx context.Context, kind string) (*Config, error) {
	if !KindSet[kind] {
		return nil, response.BadRequest("aigateway.kind_invalid")
	}
	var m Model
	err := s.db.WithContext(ctx).Where("kind = ? AND enabled = ?", kind, true).
		Order("is_default DESC, id DESC").First(&m).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrDisabled
	}
	if err != nil {
		return nil, err
	}
	key, _ := secretDecrypt(m.APIKeyEnc)
	cfg := &Config{
		Provider: m.Provider, BaseURL: m.BaseURL, APIKey: key,
		Model: m.Model, Dimension: m.Dimension,
	}
	if !cfg.Enabled() {
		return nil, ErrDisabled
	}
	return cfg, nil
}

// ---- 用量台账 ----

// UsageInput 是用量入参；totalTokens 缺省按 prompt+completion 计算。
type UsageInput struct {
	ModelID          int64
	Kind             string
	Provider         string
	Model            string
	PromptTokens     int64
	CompletionTokens int64
	TotalTokens      int64
	BizType          string
	BizNo            string
}

// RecordUsage 记一笔用量并发事件（计费/BI 订阅 aigateway.usage.recorded）。
// 记录失败只告警不阻塞调用方：台账是事实记录，不应反噬业务请求。
func (s *Service) RecordUsage(ctx context.Context, in UsageInput) {
	if !KindSet[in.Kind] {
		return
	}
	if in.TotalTokens <= 0 {
		in.TotalTokens = in.PromptTokens + in.CompletionTokens
	}
	row := Usage{
		ModelID: in.ModelID, Kind: in.Kind, Provider: truncate(in.Provider, 64),
		Model: truncate(in.Model, 255), PromptTokens: in.PromptTokens,
		CompletionTokens: in.CompletionTokens, TotalTokens: in.TotalTokens,
		BizType: truncate(in.BizType, 32), BizNo: truncate(in.BizNo, 64),
	}
	if err := s.db.WithContext(ctx).Create(&row).Error; err != nil {
		s.logWarn("aigateway: record usage failed", "err", err.Error())
		return
	}
	event.Publish(ctx, EventUsageRecorded, UsageEventData{
		TenantID: row.TenantID, UsageID: row.ID, ModelID: idgen.ID(in.ModelID),
		Kind: row.Kind, Provider: row.Provider, Model: row.Model,
		PromptTokens: row.PromptTokens, CompletionTokens: row.CompletionTokens,
		TotalTokens: row.TotalTokens, BizType: row.BizType, BizNo: row.BizNo,
		OccurredAt: time.Now().UTC(),
	})
}

// UsageQuery 过滤用量列表。
type UsageQuery struct {
	Kind    string
	BizType string
	Page    int
	PageSize int
}

// ListUsage 分页列出用量（最近在前）。
func (s *Service) ListUsage(ctx context.Context, q UsageQuery) ([]Usage, int64, error) {
	db := s.db.WithContext(ctx).Model(&Usage{})
	if q.Kind != "" {
		db = db.Where("kind = ?", q.Kind)
	}
	if q.BizType != "" {
		db = db.Where("biz_type = ?", q.BizType)
	}
	var total int64
	if err := db.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var rows []Usage
	if err := db.Order("id DESC").Offset((q.Page - 1) * q.PageSize).Limit(q.PageSize).Find(&rows).Error; err != nil {
		return nil, 0, err
	}
	return rows, total, nil
}

// UsageSummaryRow 是按 kind 聚合的用量小计。
type UsageSummaryRow struct {
	Kind             string `json:"kind"`
	Calls            int64  `json:"calls"`
	PromptTokens     int64  `json:"promptTokens"`
	CompletionTokens int64  `json:"completionTokens"`
	TotalTokens      int64  `json:"totalTokens"`
}

// UsageSummary 按 kind 聚合当前租户的 token 用量（计费口径的原始汇总）。
func (s *Service) UsageSummary(ctx context.Context) ([]UsageSummaryRow, error) {
	var rows []UsageSummaryRow
	err := s.db.WithContext(ctx).Model(&Usage{}).
		Select("kind, COUNT(*) AS calls, COALESCE(SUM(prompt_tokens),0) AS prompt_tokens, "+
			"COALESCE(SUM(completion_tokens),0) AS completion_tokens, COALESCE(SUM(total_tokens),0) AS total_tokens").
		Group("kind").Find(&rows).Error
	return rows, err
}

// ---- internals ----

func (s *Service) get(ctx context.Context, id idgen.ID) (*Model, error) {
	var m Model
	err := s.db.WithContext(ctx).First(&m, "id = ?", int64(id)).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, response.NotFound("aigateway.not_found")
	}
	return &m, err
}

func (s *Service) logWarn(msg string, keysAndValues ...any) {
	if s.log != nil {
		s.log.Warn(msg, keysAndValues...)
	}
}

func truncate(s string, n int) string {
	if len(s) > n {
		return s[:n]
	}
	return s
}

// ---- core/secret 包装：测试环境没有默认密钥时退化为明文/原样 ----

func secretEncrypt(plain string) (string, error) {
	if secret.Default() == nil {
		return plain, nil
	}
	return secret.Encrypt(plain)
}

func secretDecrypt(enc string) (string, error) {
	if secret.Default() == nil || !secret.IsEncrypted(enc) {
		return enc, nil
	}
	return secret.Decrypt(enc)
}
