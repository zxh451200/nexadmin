// Package sso 是内置的 OIDC Provider（IdP）模块：让 NexAdmin 成为单点登录中心，
// 下游应用（BI/OA/邮箱）用标准 OIDC 授权码 + PKCE 登录。
// 授权确认走 SPA（Bearer 登录态），令牌/用户信息/JWKS 等协议端点是标准 OAuth2/OIDC。
package sso

import (
	"encoding/json"
	"time"

	"nexadmin/core/database"
	"nexadmin/pkg/idgen"
)

// App 对应 sys_sso_app：一个接入的下游应用（按租户隔离）。
// SecretHash 形如 "s256$<salt>$<hash>"，明文密钥只在创建/重置时返回一次。
type App struct {
	database.TenantModel
	Name         string `gorm:"column:name;size:100;not null"`
	ClientID     string `gorm:"column:client_id;size:64;not null"`
	SecretHash   string `gorm:"column:secret_hash;size:200;not null"`
	RedirectURIs string `gorm:"column:redirect_uris;type:text"` // JSON 数组
	Logo         string `gorm:"column:logo;size:500"`
	HomeURL      string `gorm:"column:home_url;size:500"`
	Description  string `gorm:"column:description;size:500"`
	Enabled      bool   `gorm:"column:enabled"`
	RoleIDs      string `gorm:"column:role_ids;type:text"` // JSON 数组；空 = 所有登录用户可见
	// AllowOrgRead：允许该应用用 client_credentials 令牌（scope org.read）拉取通讯录（/oauth/org/*）。默认关闭。
	AllowOrgRead bool `gorm:"column:allow_org_read;not null;default:false"`
}

func (App) TableName() string { return "sys_sso_app" }

func decodeList(s string) []string {
	var out []string
	if s != "" {
		_ = json.Unmarshal([]byte(s), &out)
	}
	if out == nil {
		out = []string{}
	}
	return out
}

func encodeList(v []string) string {
	if v == nil {
		v = []string{}
	}
	b, _ := json.Marshal(v)
	return string(b)
}

// RedirectList 返回白名单回调地址。
func (a *App) RedirectList() []string { return decodeList(a.RedirectURIs) }

// RoleList 返回可见角色 ID；为空表示所有登录用户可见。
func (a *App) RoleList() []string { return decodeList(a.RoleIDs) }

// Refresh 对应 sys_sso_refresh：只存 refresh_token 的 sha256，旧令牌轮换后置 UsedAt。
type Refresh struct {
	database.TenantModel
	AppID     idgen.ID   `gorm:"column:app_id;not null"`
	ClientID  string     `gorm:"column:client_id;size:64;not null"`
	UserID    idgen.ID   `gorm:"column:user_id;not null"`
	FamilyID  string     `gorm:"column:family_id;size:32;not null"`
	TokenHash string     `gorm:"column:token_hash;size:64;not null"`
	Scope     string     `gorm:"column:scope;size:200;not null"`
	AuthTime  time.Time  `gorm:"column:auth_time;not null"`
	ExpiresAt time.Time  `gorm:"column:expires_at;not null"`
	UsedAt    *time.Time `gorm:"column:used_at"`
}

func (Refresh) TableName() string { return "sys_sso_refresh" }

// appDTO 是管理接口的应用结构（字段名与前端契约一致）。
type appDTO struct {
	ID           idgen.ID  `json:"id"`
	Name         string    `json:"name"`
	ClientID     string    `json:"clientId"`
	RedirectURIs []string  `json:"redirectUris"`
	Logo         string    `json:"logo"`
	HomeURL      string    `json:"homeUrl"`
	Description  string    `json:"description"`
	Enabled      bool      `json:"enabled"`
	RoleIDs      []string  `json:"roleIds"`
	AllowOrgRead bool      `json:"allowOrgRead"`
	CreatedAt    time.Time `json:"createdAt"`
	ClientSecret string    `json:"clientSecret,omitempty"` // 仅创建时返回一次
}

func toDTO(a *App) appDTO {
	return appDTO{ID: a.ID, Name: a.Name, ClientID: a.ClientID, RedirectURIs: a.RedirectList(), Logo: a.Logo,
		HomeURL: a.HomeURL, Description: a.Description, Enabled: a.Enabled, RoleIDs: a.RoleList(), AllowOrgRead: a.AllowOrgRead, CreatedAt: a.CreatedAt}
}

// portalDTO 是应用中心卡片。
type portalDTO struct {
	ID          idgen.ID `json:"id"`
	Name        string   `json:"name"`
	Logo        string   `json:"logo"`
	HomeURL     string   `json:"homeUrl"`
	Description string   `json:"description"`
}
