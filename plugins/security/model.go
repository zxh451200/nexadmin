package security

import (
	"time"

	"nexadmin/core/database"
	"nexadmin/pkg/idgen"
)

// 强制策略。
const (
	ModeOptional = "optional" // 可选：用户自愿启用
	ModeAll      = "all"      // 全员强制
	ModeRoles    = "roles"    // 按角色强制
)

// TOTPRow 对应 plg_security_totp：每个用户至多一行。除了绑定信息，它也记录“强制绑定宽限期”的起点，
// 所以未启用（Enabled=false）的行可能只有 GraceUntil（尚未开始绑定）或同时有待确认的密钥。
type TOTPRow struct {
	database.TenantModel
	UserID     idgen.ID   `gorm:"not null" json:"userId"`
	Username   string     `gorm:"size:100;not null" json:"username"`
	SecretEnc  string     `gorm:"type:text" json:"-"` // enc:v1: 密文，永不明文落库
	Enabled    bool       `json:"enabled"`            // 不加 gorm default 标签，否则 false 会被默认值覆盖
	EnabledAt  *time.Time `json:"enabledAt"`
	GraceUntil *time.Time `json:"graceUntil"`
}

func (TOTPRow) TableName() string { return "plg_security_totp" }

// RecoveryRow 对应 plg_security_recovery：只存哈希。
type RecoveryRow struct {
	database.TenantModel
	UserID   idgen.ID   `gorm:"not null" json:"-"`
	CodeHash string     `gorm:"size:64;not null" json:"-"`
	UsedAt   *time.Time `json:"-"`
}

func (RecoveryRow) TableName() string { return "plg_security_recovery" }

// Policy 对应 plg_security_policy：每租户一行。
type Policy struct {
	database.TenantModel
	Mode      string `gorm:"size:16;not null" json:"mode"`
	Roles     string `gorm:"size:1000;not null" json:"-"` // 逗号分隔的角色编码
	GraceDays int    `gorm:"not null" json:"graceDays"`
	UpdatedBy string `gorm:"size:100;not null" json:"updatedBy"`
}

func (Policy) TableName() string { return "plg_security_policy" }

// IPRule 对应 plg_security_ip_rule。
type IPRule struct {
	database.TenantModel
	Action  string `gorm:"size:8;not null" json:"action"`
	CIDR    string `gorm:"column:cidr;size:64;not null" json:"cidr"`
	Scope   string `gorm:"size:8;not null" json:"scope"`
	Remark  string `gorm:"size:200;not null" json:"remark"`
	Enabled bool   `json:"enabled"`
}

func (IPRule) TableName() string { return "plg_security_ip_rule" }

// LogRow 对应 plg_security_log：管理类写操作留痕（重置 2FA、策略与规则变更、用户自己启停 2FA）。
type LogRow struct {
	database.TenantModel
	OperatorID   idgen.ID `gorm:"not null" json:"operatorId"`
	OperatorName string   `gorm:"size:100;not null" json:"operatorName"`
	Action       string   `gorm:"size:32;not null" json:"action"`
	Target       string   `gorm:"size:100;not null" json:"target"`
	Detail       string   `gorm:"size:500;not null" json:"detail"`
	IP           string   `gorm:"size:64;not null" json:"ip"`
}

func (LogRow) TableName() string { return "plg_security_log" }
