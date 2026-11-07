package crm

import (
	"time"

	"gorm.io/gorm"

	"nexadmin/core/database"
	"nexadmin/pkg/idgen"
)

// 客户主数据状态与来源。
const (
	StatusActive   = "active"
	StatusInactive = "inactive"
)

// 客户类型：个人（自然人，如酒店住客、商城会员）或企业（签约单位）。
const (
	TypePerson  = "person"
	TypeCompany = "company"
)

// TypeSet 是合法客户类型集合（i18n 与校验共用）。
var TypeSet = map[string]bool{TypePerson: true, TypeCompany: true}

// 个人客户的证件类型（对齐酒店实名登记口径）；空串 = 未登记。
const (
	IdTypeIDCard    = "id_card"
	IdTypePassport  = "passport"
	IdTypeOfficer   = "officer_card"
	IdTypeHMTPermit = "hmt_permit"
	IdTypeOther     = "other"
)

// IdTypeSet 是合法证件类型集合（空串另算未登记）。
var IdTypeSet = map[string]bool{
	IdTypeIDCard: true, IdTypePassport: true, IdTypeOfficer: true,
	IdTypeHMTPermit: true, IdTypeOther: true,
}

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
// Name 按 Type 取语义——个人填姓名，企业填公司名称。
// ExternalSource + ExternalNo 是与宿主 member 体系对接的锚点
// （如 hotel+M0001 / mall+10086）：可空，非空时 (tenant_id, source, no) 唯一
// ——唯一索引由迁移 SQL 定义（gorm 标签不覆盖嵌入的 tenant_id），宿主侧以此反查客户 ID。
type Customer struct {
	database.TenantModel
	Name           string  `gorm:"size:200;not null" json:"name"`
	Code           string  `gorm:"size:64;not null;default:''" json:"code"`
	Level          string  `gorm:"size:16;not null;default:''" json:"level"`
	Status         string  `gorm:"size:16;not null;default:'active';index" json:"status"`
	Type           string  `gorm:"size:16;not null;default:'person';index" json:"type"`
	ExternalSource *string `gorm:"size:16" json:"externalSource"`
	ExternalNo     *string `gorm:"size:64" json:"externalNo"`
	Owner          string  `gorm:"size:64;not null;default:''" json:"owner"`
	Phone          string  `gorm:"size:32;not null;default:''" json:"phone"`
	Email          string  `gorm:"size:128;not null;default:''" json:"email"`
	// Country / Nationality 存 ISO 3166-1 alpha-2 代码（CN、US…），空串 = 未登记。
	// 只存代码不存名称：名称由前端 Intl.DisplayNames 按当前语言生成，中英文都不占翻译数据。
	Country     string `gorm:"size:2;not null;default:''" json:"country"`
	Nationality string `gorm:"size:2;not null;default:''" json:"nationality"`
	// 个人客户的身份：证件类型 + 证件号，配合国籍做实名识别（酒店入住、跨境订单）。
	IdType string `gorm:"size:16;not null;default:''" json:"idType"`
	IdNo   string `gorm:"size:64;not null;default:''" json:"idNo"`
	// 企业客户的身份：统一社会信用代码 + 行业 + 注册地址。
	TaxNo    string `gorm:"size:32;not null;default:''" json:"taxNo"`
	Industry string `gorm:"size:64;not null;default:''" json:"industry"`
	Address  string `gorm:"size:200;not null;default:''" json:"address"`
	Remark   string `gorm:"size:500;not null;default:''" json:"remark"`
	// Tags 以逗号包裹存储（,vip,大客户,），对外用 TagList 数组；与工单标签同一套约定。
	Tags    string   `gorm:"size:240;not null;default:''" json:"-"`
	TagList []string `gorm:"-" json:"tags"`
	// CustomValuesRaw 是自定义字段的值（JSON 对象文本，列 custom_values）。
	// 值不进独立列，管理员加字段不需要迁移历史客户。
	CustomValuesRaw string `gorm:"column:custom_values;type:text;not null" json:"-"`
	// CustomValues 是对外形态；读路径由 AfterFind 解码，写路径在 service 里显式编码。
	CustomValues map[string]any `gorm:"-" json:"customValues"`
	// DeletedAt 软删除列：删除即进回收站（recbin.TypeCustomer），联系人/跟进原样保留等还原。
	DeletedAt gorm.DeletedAt `gorm:"index" json:"-"`
}

// AfterFind 把存储串拆回标签数组、把 custom_values 拆回值映射：
// 所有读路径（列表/详情）自动生效；坏 JSON 一律当「没有值」而不是报错。
func (c *Customer) AfterFind(*gorm.DB) error {
	c.TagList = splitTags(c.Tags)
	c.CustomValues = DecodeCustomValues(c.CustomValuesRaw)
	return nil
}

func (Customer) TableName() string { return "plg_crm_customer" }

// Opportunity 是线索/商机台账。金额遵守生态规范：int64 最小货币单位（*_minor），
// 禁止 float。Stage 从 lead 推进到 won/lost；won/lost 是终态。
type Opportunity struct {
	database.TenantModel
	// CustomerID 必须走 idgen.ID：雪花 ID 超出 JS 安全整数，JSON 里只能是字符串。
	CustomerID   idgen.ID `gorm:"not null;default:0;index" json:"customerId"`
	CustomerName string   `gorm:"-" json:"customerName,omitempty"`
	// CustomerType 是列表展示用的客户类型（个人/企业），不落商机表——类型归客户。
	CustomerType string `gorm:"-" json:"customerType,omitempty"`
	// ContactID 是客户下主对接联系人（可选；0 表示未指定）。
	ContactID   idgen.ID `gorm:"not null;default:0" json:"contactId,omitempty"`
	Title       string   `gorm:"size:200;not null" json:"title"`
	Stage       string   `gorm:"size:16;not null;default:'lead';index" json:"stage"`
	AmountMinor int64    `gorm:"not null;default:0" json:"amountMinor"`
	Currency    string   `gorm:"size:8;not null;default:'CNY'" json:"currency"`
	ExpectedAt  string   `gorm:"size:10;not null;default:''" json:"expectedAt"`
	Owner       string   `gorm:"size:64;not null;default:''" json:"owner"`
	Remark      string   `gorm:"size:500;not null;default:''" json:"remark"`
}

func (Opportunity) TableName() string { return "plg_crm_opportunity" }

// 跟进记录类型（电话/拜访/备注/待办由人工写；system 是归属/合并/状态变更等操作留痕，
// 只能由服务层写入，不出现在写跟进的类型选择里）。
const (
	KindCall   = "call"
	KindVisit  = "visit"
	KindNote   = "note"
	KindTodo   = "todo"
	KindSystem = "system"
)

// ActivityKindSet 是合法跟进类型集合（i18n 与校验共用）。
var ActivityKindSet = map[string]bool{
	KindCall: true, KindVisit: true, KindNote: true, KindTodo: true, KindSystem: true,
}

// Contact 是客户下的联系人：一个客户可以有多个联系人，is_primary 标主联系人
// （客户内最多一个，由服务层在事务里保证，不做数据库唯一约束以便平滑切换）。
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
	// RemindedAt 是到期提醒的发送时间（清扫器写）；NULL = 还没提醒过。
	// 跟进记录建后不可编辑（next_at 不变），所以一次即止：发过就不再发。
	RemindedAt *time.Time `gorm:"column:reminded_at" json:"-"`
}

func (Activity) TableName() string { return "plg_crm_activity" }
