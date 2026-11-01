package crm

import (
	"nexadmin/core/database"
	"nexadmin/pkg/idgen"
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
	// CustomerID 必须走 idgen.ID：雪花 ID 超出 JS 安全整数，JSON 里只能是字符串。
	CustomerID   idgen.ID `gorm:"not null;default:0;index" json:"customerId"`
	CustomerName string   `gorm:"-" json:"customerName,omitempty"`
	// ContactID 是客户下主对接联系人（可选；0 表示未指定）。
	ContactID   idgen.ID `gorm:"not null;default:0" json:"contactId,omitempty"`
	Title       string   `gorm:"size:200;not null" json:"title"`
	Stage        string   `gorm:"size:16;not null;default:'lead';index" json:"stage"`
	AmountMinor  int64    `gorm:"not null;default:0" json:"amountMinor"`
	Currency     string   `gorm:"size:8;not null;default:'CNY'" json:"currency"`
	ExpectedAt   string   `gorm:"size:10;not null;default:''" json:"expectedAt"`
	Owner        string   `gorm:"size:64;not null;default:''" json:"owner"`
	Remark       string   `gorm:"size:500;not null;default:''" json:"remark"`
}

func (Opportunity) TableName() string { return "plg_crm_opportunity" }

// 跟进记录类型（P0 四种：电话/拜访/备注/待办；todo 用 done 与 next_at 表达）。
const (
	KindCall = "call"
	KindVisit = "visit"
	KindNote  = "note"
	KindTodo  = "todo"
)

// ActivityKindSet 是合法跟进类型集合（i18n 与校验共用）。
var ActivityKindSet = map[string]bool{
	KindCall: true, KindVisit: true, KindNote: true, KindTodo: true,
}

// Contact 是客户下的联系人：一个客户可以有多个联系人，is_primary 标主联系人
//（客户内最多一个，由服务层在事务里保证，不做数据库唯一约束以便平滑切换）。
type Contact struct {
	database.TenantModel
	CustomerID idgen.ID `gorm:"not null;default:0;index" json:"customerId"`
	Name       string   `gorm:"size:100;not null" json:"name"`
	Title      string   `gorm:"size:100;not null;default:''" json:"title"`
	Phone      string   `gorm:"size:32;not null;default:''" json:"phone"`
	Email      string   `gorm:"size:128;not null;default:''" json:"email"`
	IsPrimary  bool     `gorm:"not null;default:false" json:"isPrimary"`
	Remark     string   `gorm:"size:500;not null;default:''" json:"remark"`
}

func (Contact) TableName() string { return "plg_crm_contact" }

// Activity 是跟进记录时间线：挂客户（必填）或商机（可选）；author 记录人 = 当前账号，
// 数据范围按 customer.owner 裁剪（跟进跟着客户走，不按记录人裁）。
type Activity struct {
	database.TenantModel
	CustomerID    idgen.ID `gorm:"not null;default:0;index" json:"customerId"`
	OpportunityID idgen.ID `gorm:"not null;default:0" json:"opportunityId,omitempty"`
	Kind          string   `gorm:"size:16;not null;default:'note'" json:"kind"`
	Content       string   `gorm:"size:2000;not null" json:"content"`
	NextAt        string   `gorm:"size:16;not null;default:''" json:"nextAt"`
	Done          bool     `gorm:"not null;default:false" json:"done"`
	Author        string   `gorm:"size:64;not null;default:''" json:"author"`
}

func (Activity) TableName() string { return "plg_crm_activity" }
