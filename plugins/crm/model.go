package crm

import (
	"nexadmin/core/database"
)

// 客户主数据状态与来源。
const (
	StatusActive   = "active"
	StatusInactive = "inactive"
)

// 商机阶段（线索 → 商机共用一张表，stage 区分；不做销售管线，阶段只有这六档）。
const (
	StageLead      = "lead"
	StageQualified = "qualified"
	StageProposal  = "proposal"
	StageNegotiate = "negotiate"
	StageWon       = "won"
	StageLost      = "lost"
)

// StageSet 是合法阶段集合（i18n 与校验共用）。
var StageSet = map[string]bool{
	StageLead: true, StageQualified: true, StageProposal: true,
	StageNegotiate: true, StageWon: true, StageLost: true,
}

// ExternalSourceSet 是外部锚点来源白名单：hotel/mall 的 member 与 manual。
var ExternalSourceSet = map[string]bool{
	"hotel": true, "mall": true, "order": true, "manual": true,
}

// Customer 是客户主数据：全生态认同的"同一个客户 ID"。
// ExternalSource + ExternalNo 是与宿主 member 体系对接的锚点
// （如 hotel+M0001 / mall+10086）：可空，非空时 (tenant_id, source, no) 唯一
// ——唯一索引由迁移 SQL 定义（gorm 标签不覆盖嵌入的 tenant_id），宿主侧以此反查客户 ID。
type Customer struct {
	database.TenantModel
	Name           string  `gorm:"size:200;not null" json:"name"`
	Code           string  `gorm:"size:64;not null;default:''" json:"code"`
	Level          string  `gorm:"size:16;not null;default:''" json:"level"`
	Status         string  `gorm:"size:16;not null;default:'active';index" json:"status"`
	ExternalSource *string `gorm:"size:16" json:"externalSource"`
	ExternalNo     *string `gorm:"size:64" json:"externalNo"`
	Owner          string  `gorm:"size:64;not null;default:''" json:"owner"`
	Phone          string  `gorm:"size:32;not null;default:''" json:"phone"`
	Email          string  `gorm:"size:128;not null;default:''" json:"email"`
	Remark         string  `gorm:"size:500;not null;default:''" json:"remark"`
}

func (Customer) TableName() string { return "plg_crm_customer" }

// Opportunity 是线索/商机台账。金额遵守生态规范：int64 最小货币单位（*_minor），
// 禁止 float。Stage 从 lead 推进到 won/lost；won/lost 是终态。
type Opportunity struct {
	database.TenantModel
	CustomerID  int64  `gorm:"not null;default:0;index" json:"customerId"`
	Title       string `gorm:"size:200;not null" json:"title"`
	Stage       string `gorm:"size:16;not null;default:'lead';index" json:"stage"`
	AmountMinor int64  `gorm:"not null;default:0" json:"amountMinor"`
	Currency    string `gorm:"size:8;not null;default:'CNY'" json:"currency"`
	ExpectedAt  string `gorm:"size:10;not null;default:''" json:"expectedAt"`
	Owner       string `gorm:"size:64;not null;default:''" json:"owner"`
	Remark      string `gorm:"size:500;not null;default:''" json:"remark"`
}

func (Opportunity) TableName() string { return "plg_crm_opportunity" }
