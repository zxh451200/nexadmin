package kb

// 插件设置：检索阈值、topK 与系统提示模板。存 plg_kb_setting 单行 JSON（k='general'），
// 结构与 plugins/im/settings.go 同款：读取用默认值兜底，写入前做范围校验。

import (
	"context"
	"encoding/json"
	"errors"
	"strings"

	"gorm.io/gorm"

	"nexadmin/core/database"
	"nexadmin/core/response"
	"nexadmin/pkg/idgen"
)

const settingKeyGeneral = "general"

// Setting 是插件设置的键值行。
type Setting struct {
	database.PlatformModel
	Key   string `gorm:"column:k;size:64;not null;uniqueIndex:uk_plg_kb_setting_k" json:"-"`
	Value string `gorm:"column:v;type:text;not null" json:"-"`
}

func (Setting) TableName() string { return "plg_kb_setting" }

// Settings 是检索行为的可调参数。
type Settings struct {
	// Threshold 是余弦相似度阈值：topK 里最高分低于它 = 知识库里没有可回答的内容，
	// AI 一律不编造，走「礼貌说明 + 转人工」兜底。
	Threshold float64 `json:"threshold"`
	// TopK 是单次检索最多带入提示词的知识块数。
	TopK int `json:"topK"`
	// SystemPrompt 是回答风格的可追加要求（不能解除「只依据资料回答」的硬约束——
	// 硬约束写在代码里，模板只追加，防止改配置把模型放开）。
	SystemPrompt string `json:"systemPrompt"`
}

// DefaultSettings：阈值 0.35（主流 embedding 模型下「沾边但不确切」的分界线）、topK 4。
func DefaultSettings() Settings {
	return Settings{Threshold: 0.35, TopK: 4}
}

// Settings 读取设置；读失败或没配过时返回默认值（不阻断检索）。
func (s *Service) Settings(ctx context.Context) Settings {
	out := DefaultSettings()
	var row Setting
	if err := s.db.WithContext(ctx).Where("k = ?", settingKeyGeneral).Take(&row).Error; err != nil {
		if !errors.Is(err, gorm.ErrRecordNotFound) && s.log != nil {
			s.log.Warn("kb: load settings failed", "err", err.Error())
		}
		return out
	}
	if row.Value != "" {
		if err := json.Unmarshal([]byte(row.Value), &out); err != nil && s.log != nil {
			s.log.Warn("kb: decode settings failed", "err", err.Error())
		}
	}
	normalizeSettings(&out)
	return out
}

// SaveSettings 校验并保存设置（单行覆盖）。
func (s *Service) SaveSettings(ctx context.Context, in Settings) (*Settings, error) {
	normalizeSettings(&in)
	if in.Threshold <= 0 || in.Threshold >= 1 || in.TopK < 1 || in.TopK > 10 {
		return nil, response.BadRequest("kb.settings_invalid")
	}
	raw, err := json.Marshal(in)
	if err != nil {
		return nil, err
	}
	var row Setting
	if err := s.db.WithContext(ctx).Where("k = ?", settingKeyGeneral).Take(&row).Error; err != nil {
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, err
		}
		id, ierr := idgen.Next()
		if ierr != nil {
			return nil, ierr
		}
		row = Setting{Key: settingKeyGeneral, Value: string(raw)}
		row.ID = id
		if err := s.db.WithContext(ctx).Create(&row).Error; err != nil {
			return nil, err
		}
		return &in, nil
	}
	if err := s.db.WithContext(ctx).Model(&Setting{}).Where("id = ?", int64(row.ID)).
		Update("v", string(raw)).Error; err != nil {
		return nil, err
	}
	return &in, nil
}

func normalizeSettings(st *Settings) {
	if st.Threshold <= 0 || st.Threshold >= 1 {
		st.Threshold = DefaultSettings().Threshold
	}
	if st.TopK < 1 || st.TopK > 10 {
		st.TopK = DefaultSettings().TopK
	}
	st.SystemPrompt = strings.TrimSpace(st.SystemPrompt)
}
