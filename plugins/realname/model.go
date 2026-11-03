package realname

import (
	"nexadmin/core/database"
	"nexadmin/pkg/idgen"
)

// IdTypeIDCard 是本插件唯一支持在线核验的证件类型。其它证件（护照、港澳台通行证等）
// 的二要素接口形态完全不同，宁可明确拒绝，也不要"看起来支持"。
const IdTypeIDCard = "id_card"

// 号码自己就能分辨出来的另两类证件；台账按识别结果记录，不按调用方声称的类型记录。
const (
	IdTypeHMTPermit = "hmt_permit"    // 港澳台居民居住证
	IdTypeForeigner = "foreigner_prp" // 外国人永久居留身份证（五星卡）
)

// 核验结论。格式结论与在线结论分开，插件未启用在线核验时停在 valid。
const (
	StatusValid    = "valid"    // 号码合规则，未做在线核验
	StatusInvalid  = "invalid"  // 号码不合规则，Reason 给机器码
	StatusMatch    = "match"    // 在线核验：姓名与号码一致
	StatusMismatch = "mismatch" // 在线核验：不一致
	StatusError    = "error"    // 在线核验未完成：配置缺失、额度用尽或上游异常
)

// StatusSet 是合法状态集合（列表筛选与写入共用）。
var StatusSet = map[string]bool{
	StatusValid: true, StatusInvalid: true, StatusMatch: true, StatusMismatch: true, StatusError: true,
}

// 非号码格式的失败原因机器码，与 pkg/idcard 的 Reason 共用一个 Reason 字段。
const (
	ReasonDailyLimit      = "daily_limit"
	ReasonProviderError   = "provider_error"
	ReasonTypeUnsupported = "type_unsupported"
)

// Config 是在线核验的租户级配置（一行一租户）。密钥经 core/secret 加密落库。
type Config struct {
	database.TenantModel
	Enabled      bool   `gorm:"column:enabled;not null;default:false" json:"enabled"`
	Provider     string `gorm:"column:provider;size:32;not null;default:''" json:"provider"`
	Endpoint     string `gorm:"column:endpoint;size:512;not null;default:''" json:"endpoint"`
	AppKey       string `gorm:"column:app_key;size:128;not null;default:''" json:"appKey"`
	AppSecretEnc string `gorm:"column:app_secret_enc;size:1024;not null;default:''" json:"-"`
	TimeoutMs    int    `gorm:"column:timeout_ms;not null;default:8000" json:"timeoutMs"`
	DailyLimit   int    `gorm:"column:daily_limit;not null;default:200" json:"dailyLimit"`
}

func (Config) TableName() string { return "plg_realname_config" }

// Verification 是一次核验的留痕。设计约束：任何字段都不出现明文证件号或完整姓名，
// 需要回溯同一次输入时用 NumberHash（sha256 摘要）比对，展示时用脱敏串。
type Verification struct {
	database.TenantModel
	NameMasked   string   `gorm:"column:name_masked;size:64;not null;default:''" json:"nameMasked"`
	IdType       string   `gorm:"column:id_type;size:16;not null;default:'id_card'" json:"idType"`
	NumberMasked string   `gorm:"column:number_masked;size:64;not null;default:''" json:"numberMasked"`
	NumberHash   string   `gorm:"column:number_hash;size:64;not null;default:'';index:idx_plg_realname_hash" json:"-"`
	Status       string   `gorm:"column:status;size:16;not null;default:'valid';index:idx_plg_realname_status" json:"status"`
	Reason       string   `gorm:"column:reason;size:32;not null;default:''" json:"reason"`
	Provider     string   `gorm:"column:provider;size:32;not null;default:''" json:"provider"`
	ProviderMsg  string   `gorm:"column:provider_msg;size:200;not null;default:''" json:"providerMsg"`
	LatencyMs    int64    `gorm:"column:latency_ms;not null;default:0" json:"latencyMs"`
	OperatorID   idgen.ID `gorm:"column:operator_id;not null;default:0" json:"operatorId"`
	OperatorName string   `gorm:"column:operator_name;size:64;not null;default:''" json:"operatorName"`
	Remark       string   `gorm:"column:remark;size:200;not null;default:''" json:"remark"`
}

func (Verification) TableName() string { return "plg_realname_verification" }
