package saml

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"encoding/xml"
	"errors"
	"fmt"
	"math/big"
	"net/url"
	"strings"
	"time"

	"github.com/crewjam/saml"
	"gorm.io/gorm"

	"nexadmin/core/auth"
	"nexadmin/core/cache"
	"nexadmin/core/secret"
	"nexadmin/pkg/idgen"
)

// Issuer 是 ACS 完成断言校验后签发登录态的最小依赖（*auth.Service 满足）。
type Issuer interface {
	LoginAs(ctx context.Context, req auth.LoginAsRequest) (*auth.LoginOutcome, error)
}

// SessionRevoker 是可选的本地会话吊销能力（*auth.Service 满足）；SLO 时调用。
type SessionRevoker interface {
	RevokeSession(ctx context.Context, cl *auth.Claims) error
}

// sloKey 保存「本地用户在某 SAML 源上的 NameID」，供 SP 发起登出时构造 LogoutRequest。
func sloKey(tid, srcID, uid idgen.ID) string {
	return "saml:slo:" + tid.String() + ":" + srcID.String() + ":" + uid.String()
}

type Service struct {
	db     *gorm.DB
	issuer Issuer
	cache  cache.Cache
}

func NewService(db *gorm.DB, issuer Issuer, c cache.Cache) *Service {
	return &Service{db: db, issuer: issuer, cache: c}
}

type SourceForm struct {
	Name          string `json:"name"`
	Enabled       bool   `json:"enabled"`
	IdPMetadata   string `json:"idpMetadata"`
	AttrUsername  string `json:"attrUsername"`
	AttrEmail     string `json:"attrEmail"`
	AttrNickname  string `json:"attrNickname"`
	AutoCreate    bool   `json:"autoCreate"`
	MatchExisting bool   `json:"matchExisting"`
	DefaultRole   string `json:"defaultRole"`
}

func (s *Service) List(ctx context.Context) ([]Source, error) {
	var out []Source
	err := s.db.WithContext(ctx).Order("created_at ASC").Find(&out).Error
	return out, err
}

func (s *Service) Get(ctx context.Context, id string) (*Source, error) {
	var m Source
	if err := s.db.WithContext(ctx).Take(&m, "id = ?", id).Error; err != nil {
		return nil, err
	}
	return &m, nil
}

// Create 新建源并生成 SP 密钥对（RSA 2048，自签证书，私钥加密落库）。
func (s *Service) Create(ctx context.Context, f SourceForm) (*Source, error) {
	f.Name = strings.TrimSpace(f.Name)
	if f.Name == "" {
		return nil, errors.New("saml: name required")
	}
	if f.IdPMetadata != "" {
		if err := validateMetadata(f.IdPMetadata); err != nil {
			return nil, err
		}
	}
	certPEM, keyEnc, err := newSPKeyPair()
	if err != nil {
		return nil, err
	}
	m := Source{
		Name: f.Name, Enabled: f.Enabled, IdPMetadata: f.IdPMetadata,
		SPCert: certPEM, SPKeyEnc: keyEnc,
		AttrUsername: defAttr(f.AttrUsername, "uid"), AttrEmail: defAttr(f.AttrEmail, "mail"),
		AttrNickname: defAttr(f.AttrNickname, "displayName"),
		AutoCreate:   f.AutoCreate, MatchExisting: f.MatchExisting, DefaultRole: f.DefaultRole,
	}
	if err := s.db.WithContext(ctx).Create(&m).Error; err != nil {
		return nil, err
	}
	return &m, nil
}

func (s *Service) Update(ctx context.Context, id string, f SourceForm) (*Source, error) {
	m, err := s.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	if f.IdPMetadata != "" {
		if err := validateMetadata(f.IdPMetadata); err != nil {
			return nil, err
		}
		m.IdPMetadata = f.IdPMetadata
	}
	if f.Name != "" {
		m.Name = strings.TrimSpace(f.Name)
	}
	m.Enabled = f.Enabled
	m.AttrUsername = defAttr(f.AttrUsername, m.AttrUsername)
	m.AttrEmail = defAttr(f.AttrEmail, m.AttrEmail)
	m.AttrNickname = defAttr(f.AttrNickname, m.AttrNickname)
	m.AutoCreate, m.MatchExisting, m.DefaultRole = f.AutoCreate, f.MatchExisting, f.DefaultRole
	if err := s.db.WithContext(ctx).Save(m).Error; err != nil {
		return nil, err
	}
	return m, nil
}

func (s *Service) Delete(ctx context.Context, id string) error {
	return s.db.WithContext(ctx).Where("id = ?", id).Delete(&Source{}).Error
}

func defAttr(v, fallback string) string {
	if strings.TrimSpace(v) == "" {
		return fallback
	}
	return strings.TrimSpace(v)
}

// validateMetadata 只接受可解析且含 IdP SSO 描述的元数据。
func validateMetadata(raw string) error {
	var meta saml.EntityDescriptor
	if err := xml.Unmarshal([]byte(raw), &meta); err != nil {
		return fmt.Errorf("saml: IdP 元数据解析失败: %w", err)
	}
	if len(meta.IDPSSODescriptors) == 0 {
		return errors.New("saml: 元数据缺少 IDPSSODescriptor")
	}
	return nil
}

// newSPKeyPair 生成 SP 证书与加密私钥（私钥只以密文落库）。
func newSPKeyPair() (certPEM, keyEnc string, err error) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return "", "", err
	}
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(time.Now().UnixNano()),
		Subject:      pkix.Name{CommonName: "nexadmin-saml-sp"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().AddDate(10, 0, 0),
		KeyUsage:     x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		return "", "", err
	}
	pkcs8, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		return "", "", err
	}
	keyEnc, err = encryptSecret(string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: pkcs8})))
	if err != nil {
		return "", "", err
	}
	return string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})), keyEnc, nil
}

// 未初始化主密钥时退化为明文（与其它插件的测试/本地装配约定一致）。
func encryptSecret(v string) (string, error) {
	if secret.Default() == nil {
		return v, nil
	}
	return secret.Encrypt(v)
}

func decryptSecret(v string) (string, error) {
	if secret.Default() == nil || !secret.IsEncrypted(v) {
		return v, nil
	}
	return secret.Decrypt(v)
}

// provider 按源与站点基址构造 SP。base 形如 https://host（不含末尾斜杠）。
func (s *Service) provider(m *Source, base string) (*saml.ServiceProvider, error) {
	if strings.TrimSpace(m.IdPMetadata) == "" {
		return nil, errors.New("saml: 尚未导入 IdP 元数据")
	}
	var meta saml.EntityDescriptor
	if err := xml.Unmarshal([]byte(m.IdPMetadata), &meta); err != nil {
		return nil, fmt.Errorf("saml: IdP 元数据解析失败: %w", err)
	}
	plain, err := decryptSecret(m.SPKeyEnc)
	if err != nil {
		return nil, fmt.Errorf("saml: 读取 SP 私钥失败: %w", err)
	}
	block, _ := pem.Decode([]byte(plain))
	if block == nil {
		return nil, errors.New("saml: SP 私钥格式错误")
	}
	parsed, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		return nil, err
	}
	key, ok := parsed.(*rsa.PrivateKey)
	if !ok {
		return nil, errors.New("saml: SP 私钥类型错误")
	}
	certBlock, _ := pem.Decode([]byte(m.SPCert))
	if certBlock == nil {
		return nil, errors.New("saml: SP 证书格式错误")
	}
	cert, err := x509.ParseCertificate(certBlock.Bytes)
	if err != nil {
		return nil, err
	}
	id := m.ID.String()
	u := func(p string) url.URL { v, _ := url.Parse(base + p); return *v }
	sp := &saml.ServiceProvider{
		EntityID:    base + "/api/v1/auth/saml/" + id + "/metadata",
		AcsURL:      u("/api/v1/auth/saml/" + id + "/acs"),
		MetadataURL: u("/api/v1/auth/saml/" + id + "/metadata"),
		// SLO：登出需要 IdP 侧能回 LogoutResponse 到本端点。
		SloURL:         u("/api/v1/auth/saml/" + id + "/slo"),
		LogoutBindings: []string{saml.HTTPRedirectBinding, saml.HTTPPostBinding},
		Key:            key,
		Certificate:    cert,
		IDPMetadata:    &meta,
	}
	return sp, nil
}

// attr 从断言属性里取第一个值；未配置属性名时返回空。
func attr(a *saml.Assertion, name string) string {
	if a == nil || strings.TrimSpace(name) == "" {
		return ""
	}
	for _, st := range a.AttributeStatements {
		for _, at := range st.Attributes {
			if at.Name == name && len(at.Values) > 0 {
				return strings.TrimSpace(at.Values[0].Value)
			}
		}
	}
	return ""
}
