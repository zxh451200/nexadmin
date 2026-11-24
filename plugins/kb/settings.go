package kb

// 插件设置：检索阈值、topK 与系统提示模板。存 plg_kb_setting 单行 JSON（k='general'），
// 结构与 plugins/im/settings.go 同款：读取用默认值兜底，写入前做范围校验。

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"unicode/utf8"

	"gorm.io/gorm"

	"nexadmin/core/database"
	"nexadmin/core/response"
	"nexadmin/pkg/idgen"
)

const settingKeyGeneral = "general"

// maxSystemPromptRunes 是管理员追加回答风格的长度上限（runes）。
const maxSystemPromptRunes = 2000

// 检索方式：hybrid = 向量 + 关键词（BM25）融合，vector = 只看向量。
const (
	ModeHybrid = "hybrid"
	ModeVector = "vector"
)

// 分块参数范围。默认值分别等于 chunkSize 的历史硬编码 500 与「相邻块重叠 50 字」。
const (
	defaultChunkSize    = 500
	defaultChunkOverlap = 50
	minChunkSize        = 100
	maxChunkSize        = 2000
)

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
	// Mode 是检索方式：hybrid（默认，向量 + 关键词融合）| vector。
	// 纯向量对「型号 / 订单号 / 专有名词」这类问法天然不稳，关键词那一路是给它们兜底的。
	Mode string `json:"mode"`
	// ChunkSize 是单块最大字符数（runes）：越大上下文越完整，越小定位越准。
	ChunkSize int `json:"chunkSize"`
	// ChunkOverlap 是超长段落被硬切时，相邻块重叠的字符数：跨块被切断的句子靠它保住上下文。
	ChunkOverlap int `json:"chunkOverlap"`
}

// DefaultSettings：阈值 0.35（主流 embedding 模型下「沾边但不确切」的分界线）、topK 4、
// 混合检索、单块 500 字、重叠 50 字。
func DefaultSettings() Settings {
	return Settings{
		Threshold: 0.35, TopK: 4,
		Mode: ModeHybrid, ChunkSize: defaultChunkSize, ChunkOverlap: defaultChunkOverlap,
	}
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
	// SystemPrompt 拼进每轮问答的 system prompt：不设上限的配置会被 Run 的输入上限
	// 以 ErrInputTooLong 拒掉，机器人全线静默转人工——写入时截断而不是靠调用方兜底。
	st.SystemPrompt = strings.TrimSpace(st.SystemPrompt)
	if runes := utf8.RuneCountInString(st.SystemPrompt); runes > maxSystemPromptRunes {
		st.SystemPrompt = string([]rune(st.SystemPrompt)[:maxSystemPromptRunes])
	}
	if st.Mode != ModeVector {
		st.Mode = ModeHybrid // 空值 / 脏值都归到默认的混合检索
	}
	if st.ChunkSize < minChunkSize || st.ChunkSize > maxChunkSize {
		st.ChunkSize = defaultChunkSize
	}
	// 0 是合法值（不重叠）：只在越界时收敛到一半，不把显式的 0 改成默认值。
	if st.ChunkOverlap < 0 || st.ChunkOverlap > st.ChunkSize/2 {
		st.ChunkOverlap = min(max(st.ChunkOverlap, 0), st.ChunkSize/2)
	}
}
