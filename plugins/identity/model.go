package identity

import (
	"time"

	"nexadmin/core/database"
	"nexadmin/pkg/idgen"
)

// 身份源类型。
const (
	TypeLDAP = "ldap"
	TypeCAS  = "cas"
)

// Source 对应 plg_identity_source（租户表：每个租户管理自己的身份源）。
// Config 是配置 JSON（LDAP 绑定口令以 enc:v1: 密文存放），永远不直接序列化给前端，见 SourceView。
type Source struct {
	database.TenantModel
	Type    string `gorm:"size:16;not null"`
	Name    string `gorm:"size:100;not null"`
	Enabled bool
	Config  string `gorm:"type:text"`
}

func (Source) TableName() string { return "plg_identity_source" }

// SourceView 是对外的身份源视图：配置里的口令已被去掉。
type SourceView struct {
	ID        idgen.ID  `json:"id"`
	Type      string    `json:"type"`
	Name      string    `json:"name"`
	Enabled   bool      `json:"enabled"`
	Config    any       `json:"config"`
	EntryURL  string    `json:"entryUrl,omitempty"`  // CAS：可复制到登录页公告 / 书签的登录入口
	LogoutURL string    `json:"logoutUrl,omitempty"` // CAS：登出入口（跳转到 CAS 登出地址）
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}
