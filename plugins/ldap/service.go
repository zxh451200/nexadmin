package ldap

import (
	"context"
	"errors"
	"strings"
	"time"

	"gorm.io/gorm"

	"nexadmin/core/response"
	"nexadmin/core/secret"
	"nexadmin/pkg/idgen"
)

// Service 是 LDAP 配置业务层。
type Service struct {
	db  *gorm.DB
	log logger
}

// logger 是最小日志面(便于测试注入空实现)。
type logger interface {
	Warn(msg string, keysAndValues ...any)
	Error(msg string, keysAndValues ...any)
}

// NewService 创建 LDAP 配置服务。
func NewService(db *gorm.DB, log logger) *Service { return &Service{db: db, log: log} }

// LdapConfig 是 LDAP 平台级配置(单行,id=1)。密码经 core/secret 加密落库。
type LdapConfig struct {
	databaseID
	Enabled       bool   `gorm:"column:enabled" json:"enabled"`
	Host          string `gorm:"column:host" json:"host"`
	Port          int    `gorm:"column:port" json:"port"`
	UseTLS        bool   `gorm:"column:use_tls" json:"useTLS"`     // ldaps(636)
	StartTLS      bool   `gorm:"column:start_tls" json:"startTLS"` // ldap(389) + StartTLS
	BindDN        string `gorm:"column:bind_dn" json:"bindDN"`
	BindPassword  string `gorm:"column:bind_password" json:"-"` // 加密存储;接口不回传
	BaseDN        string `gorm:"column:base_dn" json:"baseDN"`
	UserFilter    string `gorm:"column:user_filter" json:"userFilter"` // 含 %s 占位
	AttrUsername  string `gorm:"column:attr_username" json:"attrUsername"`
	AttrEmail     string `gorm:"column:attr_email" json:"attrEmail"`
	AttrNickname  string `gorm:"column:attr_nickname" json:"attrNickname"`
	MatchExisting bool   `gorm:"column:match_existing" json:"matchExisting"` // 允许绑定同名本地账号
	AutoCreate    bool   `gorm:"column:auto_create" json:"autoCreate"`       // 允许自动建号
	DefaultRole   string `gorm:"column:default_role" json:"defaultRole"`
	TimeoutMS     int    `gorm:"column:timeout_ms" json:"timeoutMs"`
	// CACert 是信任的 CA 证书（PEM，可多张）。空 = 使用系统根证书。
	CACert string `gorm:"column:ca_cert;type:text" json:"caCert"`
	// BindPasswordPlain 是运行时解密结果,永不落库、永不序列化。
	BindPasswordPlain string `gorm:"-" json:"-"`
}

// databaseID 是平台级单行配置的最小主键集(不引入 TenantModel)。
type databaseID struct {
	ID        uint64 `gorm:"primaryKey;autoIncrement:false"`
	CreatedAt time.Time
	UpdatedAt time.Time
}

func (LdapConfig) TableName() string { return "plg_ldap_config" }

// ConfigView 是配置视图(密码不回传,只回是否已设置)。
type ConfigView struct {
	LdapConfig
	HasBindPassword bool `json:"hasBindPassword"`
}

// LdapConfigForm 是保存表单;bindPassword 留空表示不修改。
type LdapConfigForm struct {
	Enabled       bool   `json:"enabled"`
	Host          string `json:"host"`
	Port          int    `json:"port"`
	UseTLS        bool   `json:"useTLS"`
	StartTLS      bool   `json:"startTLS"`
	BindDN        string `json:"bindDN"`
	BindPassword  string `json:"bindPassword"`
	BaseDN        string `json:"baseDN"`
	UserFilter    string `json:"userFilter"`
	AttrUsername  string `json:"attrUsername"`
	AttrEmail     string `json:"attrEmail"`
	AttrNickname  string `json:"attrNickname"`
	MatchExisting bool   `json:"matchExisting"`
	AutoCreate    bool   `json:"autoCreate"`
	DefaultRole   string `json:"defaultRole"`
	TimeoutMS     int    `json:"timeoutMs"`
	CACert        string `json:"caCert"`
}

// defaultConfig 返回带默认值的空配置。
func defaultConfig() *LdapConfig {
	return &LdapConfig{
		Port: DefaultPort, UserFilter: DefaultFilter,
		AttrUsername: DefaultAttrUser, AttrEmail: DefaultAttrMail, AttrNickname: DefaultAttrCN,
		TimeoutMS: DefaultTimeout,
	}
}

// Config 读取当前配置(单例行;不存在返回默认值副本)。BindPasswordPlain 已解密。
func (s *Service) Config(ctx context.Context) (*LdapConfig, error) {
	var m LdapConfig
	err := s.db.WithContext(ctx).Take(&m, "id = ?", 1).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return defaultConfig(), nil
		}
		return nil, err
	}
	if m.BindPassword != "" {
		if plain, derr := secretDecrypt(m.BindPassword); derr == nil {
			m.BindPasswordPlain = plain
		}
	}
	return &m, nil
}

// ConfigView 读取配置视图。
func (s *Service) ConfigView(ctx context.Context) (*ConfigView, error) {
	m, err := s.Config(ctx)
	if err != nil {
		return nil, err
	}
	v := &ConfigView{LdapConfig: *m, HasBindPassword: m.BindPassword != ""}
	v.BindPassword, v.BindPasswordPlain = "", ""
	return v, nil
}

// ConfigSave 保存配置:bindPassword 留空 = 不修改;单行 upsert(id 固定 1)。
func (s *Service) ConfigSave(ctx context.Context, f LdapConfigForm) (*ConfigView, error) {
	f.Host = strings.TrimSpace(f.Host)
	f.BaseDN = strings.TrimSpace(f.BaseDN)
	f.BindDN = strings.TrimSpace(f.BindDN)
	f.UserFilter = strings.TrimSpace(f.UserFilter)
	f.CACert = strings.TrimSpace(f.CACert)
	if f.CACert != "" && !validCACert(f.CACert) {
		return nil, response.BadRequest("ldap.ca_invalid")
	}
	if f.Enabled && (f.Host == "" || f.BaseDN == "") {
		return nil, response.BadRequest("ldap.host_basedn_required")
	}
	if f.Enabled && !strings.Contains(f.UserFilter, "%s") {
		return nil, response.BadRequest("ldap.filter_placeholder_required")
	}
	if f.Port <= 0 {
		f.Port = DefaultPort
	}
	if f.UserFilter == "" {
		f.UserFilter = DefaultFilter
	}
	if f.AttrUsername == "" {
		f.AttrUsername = DefaultAttrUser
	}
	if f.AttrEmail == "" {
		f.AttrEmail = DefaultAttrMail
	}
	if f.AttrNickname == "" {
		f.AttrNickname = DefaultAttrCN
	}
	if f.TimeoutMS <= 0 {
		f.TimeoutMS = DefaultTimeout
	}

	m := LdapConfig{
		Enabled: f.Enabled, Host: f.Host, Port: f.Port,
		UseTLS: f.UseTLS, StartTLS: f.StartTLS,
		BindDN: f.BindDN, BaseDN: f.BaseDN, UserFilter: f.UserFilter,
		AttrUsername: f.AttrUsername, AttrEmail: f.AttrEmail, AttrNickname: f.AttrNickname,
		MatchExisting: f.MatchExisting, AutoCreate: f.AutoCreate, DefaultRole: f.DefaultRole,
		TimeoutMS: f.TimeoutMS, CACert: f.CACert,
	}
	m.ID = 1
	if f.BindPassword != "" {
		enc, eerr := secretEncrypt(f.BindPassword)
		if eerr != nil {
			return nil, eerr
		}
		m.BindPassword = enc
	} else {
		// 保留原密码(编辑其它字段时不丢)。
		var cur LdapConfig
		if err := s.db.WithContext(ctx).Take(&cur, "id = ?", 1).Error; err == nil {
			m.BindPassword = cur.BindPassword
		}
	}
	if err := s.db.WithContext(ctx).Save(&m).Error; err != nil {
		return nil, err
	}
	return s.ConfigView(ctx)
}

// TestConnection 验证连通性:拨号 + 服务账号绑定(可选)+ 按过滤器空跑一次搜索。
func (s *Service) TestConnection(ctx context.Context) error {
	cfg, err := s.Config(ctx)
	if err != nil {
		return err
	}
	if cfg.Host == "" || cfg.BaseDN == "" {
		return response.BadRequest("ldap.host_basedn_required")
	}
	conn, derr := dial(cfg)
	if derr != nil {
		return response.BadRequest("ldap.test_failed").Wrap(derr)
	}
	defer func() { _ = conn.Close() }()
	if cfg.BindDN != "" {
		if berr := conn.Bind(cfg.BindDN, cfg.BindPasswordPlain); berr != nil {
			return response.BadRequest("ldap.test_failed").Wrap(berr)
		}
	}
	if _, serr := conn.Search(ldapSearch(cfg, "e2e-probe")); serr != nil {
		return response.BadRequest("ldap.test_failed").Wrap(serr)
	}
	return nil
}

// secretEncrypt/secretDecrypt 包装 core/secret:测试环境没有默认密钥时退化为明文/原样。
func secretEncrypt(plain string) (string, error) {
	if secret.Default() == nil {
		return plain, nil
	}
	return secret.Encrypt(plain)
}

func secretDecrypt(enc string) (string, error) {
	if secret.Default() == nil || !secret.IsEncrypted(enc) {
		return enc, nil
	}
	return secret.Decrypt(enc)
}

var _ = idgen.Next
