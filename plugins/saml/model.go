package saml

import "nexadmin/core/database"

// Source 是一个 SAML 2.0 身份源（企业 IdP）。创建时生成 SP 密钥对，私钥加密存储。
type Source struct {
	database.TenantModel
	Name          string `gorm:"size:100;not null"`
	Enabled       bool   `gorm:"not null;default:false"`
	IdPMetadata   string `gorm:"column:idp_metadata;type:text"`
	SPCert        string `gorm:"column:sp_cert;type:text"`
	SPKeyEnc      string `gorm:"column:sp_key_enc;type:text"`
	AttrUsername  string `gorm:"column:attr_username;size:64;not null;default:''"`
	AttrEmail     string `gorm:"column:attr_email;size:64;not null;default:''"`
	AttrNickname  string `gorm:"column:attr_nickname;size:64;not null;default:''"`
	AutoCreate    bool   `gorm:"column:auto_create;not null;default:false"`
	MatchExisting bool   `gorm:"column:match_existing;not null;default:false"`
	DefaultRole   string `gorm:"column:default_role;size:64;not null;default:''"`
}

func (Source) TableName() string { return "plg_saml_source" }
