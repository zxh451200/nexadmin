// Package openapi 是开放平台的鉴权层：个人访问令牌（PAT，Authorization: Bearer nxp_...）与
// 应用密钥签名调用（X-App-Key / X-Timestamp / X-Nonce / X-Signature）。
// 管理接口与 Webhook 在 modules/openapi；这里只放中间件和被中间件依赖的模型，避免 core 反向依赖 modules。
package openapi

import (
	"encoding/json"
	"time"

	"nexadmin/core/database"
	"nexadmin/pkg/idgen"
)

// PAT 对应 sys_pat：个人访问令牌。库里只存 sha256 哈希与前 8 位 prefix，明文只在创建时返回一次。
type PAT struct {
	database.TenantModel
	UserID     idgen.ID   `gorm:"column:user_id;not null"`
	Name       string     `gorm:"column:name;size:100;not null"`
	Scope      string     `gorm:"column:scope;size:16;not null"` // read | write
	TokenHash  string     `gorm:"column:token_hash;size:64;not null"`
	Prefix     string     `gorm:"column:prefix;size:16;not null"`
	LastUsedAt *time.Time `gorm:"column:last_used_at"`
	ExpiresAt  *time.Time `gorm:"column:expires_at"`
}

// TableName 实现 gorm.Tabler。
func (PAT) TableName() string { return "sys_pat" }

// App 对应 sys_openapi_app：开放应用（服务账号）。签名校验需要原文密钥，所以 SecretEnc 是 core/secret 加密的密文。
type App struct {
	database.TenantModel
	Name        string     `gorm:"column:name;size:100;not null"`
	AppKey      string     `gorm:"column:app_key;size:64;not null"`
	SecretEnc   string     `gorm:"column:secret_enc;type:text;not null"`
	Description string     `gorm:"column:description;size:500"`
	RoleIDs     string     `gorm:"column:role_ids;type:text"` // JSON 数组
	IPAllow     string     `gorm:"column:ip_allow;type:text"` // JSON 数组；空 = 不限制
	Enabled     bool       `gorm:"column:enabled"`
	LastUsedAt  *time.Time `gorm:"column:last_used_at"`
}

// TableName 实现 gorm.Tabler。
func (App) TableName() string { return "sys_openapi_app" }

// RoleList 返回绑定的角色 ID。
func (a *App) RoleList() []string { return DecodeList(a.RoleIDs) }

// IPList 返回 IP 白名单（IP 或 CIDR）。
func (a *App) IPList() []string { return DecodeList(a.IPAllow) }

// DecodeList 解析 JSON 字符串数组；空或非法时返回空切片（不是 nil）。
func DecodeList(s string) []string {
	var out []string
	if s != "" {
		_ = json.Unmarshal([]byte(s), &out)
	}
	if out == nil {
		out = []string{}
	}
	return out
}

// EncodeList 把字符串数组编码成 JSON。
func EncodeList(v []string) string {
	if v == nil {
		v = []string{}
	}
	b, _ := json.Marshal(v)
	return string(b)
}
