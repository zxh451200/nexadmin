package saml

import (
	"encoding/json"
	"strings"
	"time"

	"gorm.io/gorm"

	"nexadmin/core/database"
)

// GroupRoleMapping 仅在自动建号时授予角色：命中的组值替代默认角色，未命中回退默认角色。
// 组值按 IdP 断言里的原值精确匹配（区分大小写与空格），不解析 DN、不展开嵌套组。
type GroupRoleMapping struct {
	ExternalGroup string   `json:"externalGroup"`
	RoleCodes     []string `json:"roleCodes"`
}

// Source 是一个 SAML 2.0 身份源（企业 IdP）。创建时生成 SP 密钥对，私钥加密存储。
type Source struct {
	database.TenantModel
	Name           string `gorm:"size:100;not null"`
	Enabled        bool   `gorm:"not null;default:false"`
	IdPMetadata    string `gorm:"column:idp_metadata;type:text"`
	IdPMetadataURL string `gorm:"column:idp_metadata_url;size:500;not null;default:''"`
	// IDPEntityID 在多 IdP 打包元数据里指定用哪一个；单实体导入时自动填上。
	IDPEntityID         string     `gorm:"column:idp_entity_id;size:500;not null;default:''"`
	MetadataRefreshedAt *time.Time `gorm:"column:metadata_refreshed_at"`
	SPCert              string     `gorm:"column:sp_cert;type:text"`
	SPKeyEnc            string     `gorm:"column:sp_key_enc;type:text"`
	// 两段式轮换：next 已发布但尚未启用（元数据同时列出，签发仍用 current）；
	// 切换后旧证书落到 prev，保留一段时间用于解密 IdP 仍按旧证书加密的断言。
	NextSPCert      string     `gorm:"column:next_sp_cert;type:text"`
	NextSPKeyEnc    string     `gorm:"column:next_sp_key_enc;type:text"`
	PrevSPCert      string     `gorm:"column:prev_sp_cert;type:text"`
	PrevSPKeyEnc    string     `gorm:"column:prev_sp_key_enc;type:text"`
	PrevSPExpiresAt *time.Time `gorm:"column:prev_sp_expires_at"`
	// RequireEncryptedAssertion 要求 IdP 必须用 SP 证书加密断言，明文断言直接拒绝。
	RequireEncryptedAssertion bool `gorm:"column:require_encrypted_assertion;not null;default:false"`

	AttrUsername string `gorm:"column:attr_username;size:255;not null;default:''"`
	AttrEmail    string `gorm:"column:attr_email;size:255;not null;default:''"`
	AttrNickname string `gorm:"column:attr_nickname;size:255;not null;default:''"`
	AttrGroups   string `gorm:"column:attr_groups;size:255;not null;default:''"`
	AttrPhone    string `gorm:"column:attr_phone;size:255;not null;default:''"`

	AutoCreate    bool   `gorm:"column:auto_create;not null;default:false"`
	MatchExisting bool   `gorm:"column:match_existing;not null;default:false"`
	DefaultRole   string `gorm:"column:default_role;size:64;not null;default:''"`

	// GroupRoleMappingsJSON 是落库形态；API 只暴露解析后的 groupRoleMappings。
	GroupRoleMappingsJSON string             `gorm:"column:group_role_mappings;type:text" json:"-"`
	GroupRoleMappings     []GroupRoleMapping `gorm:"-" json:"groupRoleMappings"`
	RequireMappedGroup    bool               `gorm:"column:require_mapped_group;not null;default:false"`
	// SyncRolesOnLogin 每次登录按组映射重算角色：只收回本来源授过的角色，
	// 手工授权与其它来源授予的角色都不动；本次断言没命中任何组时整体不动。
	SyncRolesOnLogin bool `gorm:"column:sync_roles_on_login;not null;default:false"`

	AllowIDPInitiated bool   `gorm:"column:allow_idp_initiated;not null;default:false"`
	SignAuthnRequest  bool   `gorm:"column:sign_authn_request;not null;default:false"`
	NameIDFormat      string `gorm:"column:name_id_format;size:128;not null;default:''"`

	// 协议级加固默认关闭：开启前需确认 IdP 行为，否则会把原本能登录的源拒掉。
	RequireSignedCBC        bool   `gorm:"column:require_signed_cbc;not null;default:false"`
	RejectUnknownConditions bool   `gorm:"column:reject_unknown_conditions;not null;default:false"`
	ForceAuthn              bool   `gorm:"column:force_authn;not null;default:false"`
	AuthnContextClassRef    string `gorm:"column:authn_context_class_ref;size:500;not null;default:''"`
	RequireAuthnContext     bool   `gorm:"column:require_authn_context;not null;default:false"`
}

func (Source) TableName() string { return "plg_saml_source" }

// BeforeSave 把组映射序列化成 JSON 列。
func (m *Source) BeforeSave(*gorm.DB) error {
	if len(m.GroupRoleMappings) == 0 {
		m.GroupRoleMappingsJSON = ""
		return nil
	}
	b, err := json.Marshal(m.GroupRoleMappings)
	if err != nil {
		return err
	}
	m.GroupRoleMappingsJSON = string(b)
	return nil
}

// AfterFind 还原组映射；库里内容损坏时直接报错，不静默当成「没有映射」（否则组准入会失效）。
func (m *Source) AfterFind(*gorm.DB) error {
	m.GroupRoleMappings = nil
	if strings.TrimSpace(m.GroupRoleMappingsJSON) == "" {
		return nil
	}
	return json.Unmarshal([]byte(m.GroupRoleMappingsJSON), &m.GroupRoleMappings)
}
