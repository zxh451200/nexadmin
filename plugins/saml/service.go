package saml

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"fmt"
	"math/big"
	"net/url"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/crewjam/saml"
	"github.com/russellhaering/goxmldsig"
	"go.uber.org/zap"
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

// TokenRevoker 按令牌 ID 吊销会话：IdP 发起的 SLO 只有 NameID，本地只能靠登录时记下的 jti。
type TokenRevoker interface {
	RevokeByJTI(ctx context.Context, accessJTI, refreshJTI string) error
}

// sloKey 保存「本地用户在某 SAML 源上的 NameID」，供 SP 发起登出时构造 LogoutRequest。
func sloKey(tid, srcID, uid idgen.ID) string {
	return "saml:slo:" + tid.String() + ":" + srcID.String() + ":" + uid.String()
}

type Service struct {
	db     *gorm.DB
	issuer Issuer
	cache  cache.Cache
	log    *zap.Logger
}

func NewService(db *gorm.DB, issuer Issuer, c cache.Cache) *Service {
	return &Service{db: db, issuer: issuer, cache: c}
}

// SetLogger 给后台同步与登录时角色对账留审计线索；不设置时只跳过日志，不跳过动作。
func (s *Service) SetLogger(l *zap.Logger) *Service {
	s.log = l
	return s
}

type SourceForm struct {
	Name           string             `json:"name"`
	Enabled        bool               `json:"enabled"`
	IdPMetadata    string             `json:"idpMetadata"`
	IdPMetadataURL string             `json:"idpMetadataUrl"`
	IDPEntityID    string             `json:"idpEntityId"`
	AttrUsername   string             `json:"attrUsername"`
	AttrEmail      string             `json:"attrEmail"`
	AttrNickname   string             `json:"attrNickname"`
	AttrGroups     string             `json:"attrGroups"`
	AttrPhone      string             `json:"attrPhone"`
	AutoCreate     bool               `json:"autoCreate"`
	MatchExisting  bool               `json:"matchExisting"`
	DefaultRole    string             `json:"defaultRole"`
	GroupMappings  []GroupRoleMapping `json:"groupRoleMappings"`
	RequireMapped  bool               `json:"requireMappedGroup"`
	SyncRoles      bool               `json:"syncRolesOnLogin"`
	AllowIDPInit   bool               `json:"allowIdpInitiated"`
	SignRequests   bool               `json:"signAuthnRequest"`
	NameIDFormat   string             `json:"nameIdFormat"`
	// RequireEncrypted 要求断言必须加密（明文断言拒绝登录）。
	RequireEncrypted bool `json:"requireEncryptedAssertion"`

	RequireSignedCBC        bool   `json:"requireSignedResponseForCBC"`
	RejectUnknownConditions bool   `json:"rejectUnknownConditions"`
	ForceAuthn              bool   `json:"forceAuthn"`
	AuthnContextClassRef    string `json:"authnContextClassRef"`
	RequireAuthnContext     bool   `json:"requireAuthnContext"`
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
	if err := f.validate(); err != nil {
		return nil, err
	}
	certPEM, keyEnc, err := newSPKeyPair()
	if err != nil {
		return nil, err
	}
	if f.Enabled {
		// 直接以启用态保存时也要过一遍「真的能登录」，避免绕过快捷启停的检查。
		if err := (&Source{IdPMetadata: f.IdPMetadata, SPCert: certPEM}).ready(); err != nil {
			return nil, err
		}
	}
	m := Source{
		Name: f.Name, Enabled: f.Enabled, IdPMetadata: f.IdPMetadata, IdPMetadataURL: f.IdPMetadataURL,
		IDPEntityID: f.IDPEntityID, SPCert: certPEM, SPKeyEnc: keyEnc,
		AttrUsername: f.AttrUsername, AttrEmail: f.AttrEmail, AttrNickname: f.AttrNickname, AttrGroups: f.AttrGroups, AttrPhone: f.AttrPhone,
		AutoCreate: f.AutoCreate, MatchExisting: f.MatchExisting, DefaultRole: f.DefaultRole,
		GroupRoleMappings: f.GroupMappings, RequireMappedGroup: f.RequireMapped, SyncRolesOnLogin: f.SyncRoles,
		AllowIDPInitiated: f.AllowIDPInit, SignAuthnRequest: f.SignRequests, NameIDFormat: f.NameIDFormat,
		RequireEncryptedAssertion: f.RequireEncrypted,
		RequireSignedCBC:          f.RequireSignedCBC, RejectUnknownConditions: f.RejectUnknownConditions,
		ForceAuthn: f.ForceAuthn, AuthnContextClassRef: f.AuthnContextClassRef, RequireAuthnContext: f.RequireAuthnContext,
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
	if err := f.validate(); err != nil {
		return nil, err
	}
	m.Name = f.Name
	m.Enabled = f.Enabled
	m.IdPMetadata = f.IdPMetadata
	m.IdPMetadataURL = f.IdPMetadataURL
	m.IDPEntityID = f.IDPEntityID
	m.AttrUsername = f.AttrUsername
	m.AttrEmail = f.AttrEmail
	m.AttrNickname = f.AttrNickname
	m.AttrGroups = f.AttrGroups
	m.AttrPhone = f.AttrPhone
	m.AutoCreate = f.AutoCreate
	m.MatchExisting = f.MatchExisting
	m.DefaultRole = f.DefaultRole
	m.GroupRoleMappings = f.GroupMappings
	m.RequireMappedGroup = f.RequireMapped
	m.SyncRolesOnLogin = f.SyncRoles
	m.AllowIDPInitiated = f.AllowIDPInit
	m.SignAuthnRequest = f.SignRequests
	m.NameIDFormat = f.NameIDFormat
	m.RequireEncryptedAssertion = f.RequireEncrypted
	m.RequireSignedCBC = f.RequireSignedCBC
	m.RejectUnknownConditions = f.RejectUnknownConditions
	m.ForceAuthn = f.ForceAuthn
	m.AuthnContextClassRef = f.AuthnContextClassRef
	m.RequireAuthnContext = f.RequireAuthnContext
	if m.Enabled {
		if err := m.ready(); err != nil {
			return nil, err
		}
	}
	if err := s.db.WithContext(ctx).Save(m).Error; err != nil {
		return nil, err
	}
	return m, nil
}

// spPrevGrace 是切换后旧证书继续出现在元数据、并继续用于解密断言的时长。
const spPrevGrace = 7 * 24 * time.Hour

// StageSPKey 发布下一张 SP 证书（两段式轮换第一步）：元数据同时列出当前与下一张，
// 签发仍用当前证书，给 IdP 留出更新信任的时间；确认后再 Promote。
func (s *Service) StageSPKey(ctx context.Context, id string) (*Source, error) {
	m, err := s.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(m.NextSPCert) != "" {
		return nil, errors.New("saml: 已经发布过新证书，请先切换到新证书")
	}
	certPEM, keyEnc, err := newSPKeyPair()
	if err != nil {
		return nil, err
	}
	updates := map[string]any{"next_sp_cert": certPEM, "next_sp_key_enc": keyEnc}
	// 过期的上一代证书顺手清掉，避免轮换多次后一直带着旧私钥。
	if m.PrevSPExpiresAt != nil && time.Now().After(*m.PrevSPExpiresAt) {
		updates["prev_sp_cert"], updates["prev_sp_key_enc"], updates["prev_sp_expires_at"] = "", "", nil
	}
	if err := s.db.WithContext(ctx).Model(&Source{}).Where("id = ?", id).Updates(updates).Error; err != nil {
		return nil, err
	}
	m.NextSPCert, m.NextSPKeyEnc = certPEM, keyEnc
	return m, nil
}

// PromoteSPKey 切换到已发布的证书（第二步）：新证书变成签名证书，旧证书保留为解密兜底。
func (s *Service) PromoteSPKey(ctx context.Context, id string) (*Source, error) {
	m, err := s.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(m.NextSPCert) == "" || strings.TrimSpace(m.NextSPKeyEnc) == "" {
		return nil, errors.New("saml: 还没有已发布的新证书，请先发布")
	}
	until := time.Now().Add(spPrevGrace)
	updates := map[string]any{
		"sp_cert":            m.NextSPCert,
		"sp_key_enc":         m.NextSPKeyEnc,
		"next_sp_cert":       "",
		"next_sp_key_enc":    "",
		"prev_sp_cert":       m.SPCert,
		"prev_sp_key_enc":    m.SPKeyEnc,
		"prev_sp_expires_at": until,
	}
	if err := s.db.WithContext(ctx).Model(&Source{}).Where("id = ?", id).Updates(updates).Error; err != nil {
		return nil, err
	}
	m.PrevSPCert, m.PrevSPKeyEnc, m.PrevSPExpiresAt = m.SPCert, m.SPKeyEnc, &until
	m.SPCert, m.SPKeyEnc = m.NextSPCert, m.NextSPKeyEnc
	m.NextSPCert, m.NextSPKeyEnc = "", ""
	return m, nil
}

func (s *Service) SetEnabled(ctx context.Context, id string, enabled bool) error {
	m, err := s.Get(ctx, id)
	if err != nil {
		return err
	}
	if enabled {
		if err := m.ready(); err != nil {
			return err
		}
	}
	return s.db.WithContext(ctx).Model(&Source{}).Where("id = ?", id).Update("enabled", enabled).Error
}

func (s *Service) Delete(ctx context.Context, id string) error {
	// 授予记账跟着来源一起删：来源没了，它授过的角色不再属于任何映射，
	// 但用户身上已生效的角色保持现状（删配置不等于收权）。
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("source_id = ?", id).Delete(&roleGrant{}).Error; err != nil {
			return err
		}
		return tx.Where("id = ?", id).Delete(&Source{}).Error
	})
}

const (
	maxMetadataBytes = 1 << 20 // 1 MiB：常见 IdP 元数据在几 KB 量级
	maxAttrName      = 255
)

// validate 归一化并校验表单；创建与更新都走这里，避免两套规则。
func (f *SourceForm) validate() error {
	f.Name = strings.TrimSpace(f.Name)
	if f.Name == "" {
		return errors.New("saml: 名称不能为空")
	}
	if utf8.RuneCountInString(f.Name) > 100 {
		return errors.New("saml: 名称不能超过 100 个字符")
	}
	f.IdPMetadata = strings.TrimSpace(f.IdPMetadata)
	if f.IdPMetadata == "" {
		return errors.New("saml: IdP 元数据不能为空（可粘贴 XML，或先按 URL 拉取）")
	}
	if len(f.IdPMetadata) > maxMetadataBytes {
		return errors.New("saml: IdP 元数据超过 1 MiB")
	}
	if err := validateMetadata(f.IdPMetadata); err != nil {
		return err
	}
	f.IdPMetadataURL = strings.TrimSpace(f.IdPMetadataURL)
	if err := validateMetadataURL(f.IdPMetadataURL); err != nil {
		return err
	}
	f.AttrUsername = defAttr(f.AttrUsername, "uid")
	f.AttrEmail = defAttr(f.AttrEmail, "mail")
	f.AttrNickname = defAttr(f.AttrNickname, "displayName")
	f.AttrGroups = strings.TrimSpace(f.AttrGroups)
	f.AttrPhone = strings.TrimSpace(f.AttrPhone)
	for _, name := range []string{f.AttrUsername, f.AttrEmail, f.AttrNickname, f.AttrGroups, f.AttrPhone} {
		if !validAttrName(name) {
			return fmt.Errorf("saml: 属性名不合法: %q", name)
		}
	}
	if err := f.normalizeEntity(); err != nil {
		return err
	}
	if _, ok := nameIDFormats[f.NameIDFormat]; !ok {
		return fmt.Errorf("saml: 不支持的 NameID 格式: %q", f.NameIDFormat)
	}
	f.AuthnContextClassRef = strings.TrimSpace(f.AuthnContextClassRef)
	refs, err := authnContextClassRefs(f.AuthnContextClassRef)
	if err != nil {
		return err
	}
	if f.RequireAuthnContext && len(refs) == 0 {
		return errors.New("saml: 要求校验认证上下文时必须先指定认证上下文类")
	}
	f.DefaultRole = strings.TrimSpace(f.DefaultRole)
	if f.DefaultRole != "" && !validRoleCode(f.DefaultRole) {
		return fmt.Errorf("saml: 默认角色编码无效: %q", f.DefaultRole)
	}
	return validateGroupMappings(f.GroupMappings, f.RequireMapped)
}

// normalizeEntity 校验并补全打包元数据里选用的 Entity ID；单实体自动选中。
func (f *SourceForm) normalizeEntity() error {
	f.IDPEntityID = strings.TrimSpace(f.IDPEntityID)
	if len(f.IDPEntityID) > 500 || !utf8.ValidString(f.IDPEntityID) ||
		strings.ContainsFunc(f.IDPEntityID, func(r rune) bool { return r < 0x20 || r == 0x7f }) {
		return errors.New("saml: Entity ID 不合法")
	}
	entities, err := metadataIDPEntities(f.IdPMetadata)
	if err != nil {
		return err
	}
	if f.IDPEntityID == "" {
		if len(entities) == 1 {
			f.IDPEntityID = entities[0].EntityID
		}
		return nil
	}
	for _, e := range entities {
		if e.EntityID == f.IDPEntityID {
			return nil
		}
	}
	return fmt.Errorf("saml: 元数据里没有 Entity ID 为 %q 的 IdP", f.IDPEntityID)
}

// ready 判断这个源现在能否真正完成一次登录（启用前检查，避免「已启用但一登就错」）。
func (m *Source) ready() error {
	info, err := parseIdP(m.IdPMetadata, m.IDPEntityID)
	if err != nil {
		return err
	}
	if info.EntityID == "" {
		return fmt.Errorf("saml: 元数据包含 %d 个 IdP，请先选择 Entity ID", len(info.Candidates))
	}
	if info.SSOURL == "" {
		return errors.New("saml: IdP 元数据里没有可用的 SingleSignOnService 地址")
	}
	if info.CertCount == 0 {
		return errors.New("saml: IdP 元数据里没有签名证书，无法校验断言")
	}
	if info.CertExpired {
		return errors.New("saml: IdP 签名证书已过期，请先更新元数据")
	}
	if _, err := spCertNotAfter(m.SPCert); err != nil {
		return err
	}
	return nil
}

func defAttr(v, fallback string) string {
	if strings.TrimSpace(v) == "" {
		return fallback
	}
	return strings.TrimSpace(v)
}

func validAttrName(v string) bool {
	if v == "" {
		return true
	}
	if len(v) > maxAttrName || !utf8.ValidString(v) {
		return false
	}
	return !strings.ContainsFunc(v, func(r rune) bool { return r < 0x20 || r == 0x7f || r == ' ' })
}

func validRoleCode(code string) bool {
	return len(code) <= 64 && utf8.ValidString(code) && code == strings.TrimSpace(code) &&
		!strings.EqualFold(code, "super") &&
		!strings.ContainsFunc(code, func(r rune) bool { return r < 0x20 || r == 0x7f })
}

// validateGroupMappings 与 LDAP/CAS 身份源同一套约束：启用「必须命中组」时至少一条，组值原样精确匹配。
func validateGroupMappings(ms []GroupRoleMapping, required bool) error {
	if len(ms) > 100 || (required && len(ms) == 0) {
		return errors.New("saml: 组映射数量无效（启用「必须命中组」时至少一条）")
	}
	groups, total := map[string]bool{}, 0
	for _, m := range ms {
		g := m.ExternalGroup
		if strings.TrimSpace(g) == "" || len(g) > 2048 || !utf8.ValidString(g) ||
			strings.ContainsFunc(g, func(r rune) bool { return r < 0x20 || r == 0x7f }) || groups[g] {
			return errors.New("saml: 组映射里的组值无效或重复")
		}
		groups[g] = true
		if len(m.RoleCodes) == 0 || len(m.RoleCodes) > 100 {
			return errors.New("saml: 每个组映射需要 1-100 个角色编码")
		}
		seen := map[string]bool{}
		for _, code := range m.RoleCodes {
			if !validRoleCode(code) || seen[code] {
				return fmt.Errorf("saml: 角色编码无效或重复: %q", code)
			}
			seen[code] = true
		}
		total += len(m.RoleCodes)
	}
	if total > 1000 {
		return errors.New("saml: 映射角色总数不能超过 1000")
	}
	return nil
}

// mappedRoleCodes 登录时按组值取角色；required 且零命中即拒绝登录。
func mappedRoleCodes(groups []string, ms []GroupRoleMapping, required bool) ([]string, error) {
	if err := validateGroupMappings(ms, required); err != nil {
		return nil, err
	}
	set := make(map[string]bool, len(groups))
	for _, g := range groups {
		set[g] = true
	}
	var out []string
	seen := map[string]bool{}
	for _, m := range ms {
		if !set[m.ExternalGroup] {
			continue
		}
		for _, code := range m.RoleCodes {
			if !seen[code] {
				seen[code] = true
				out = append(out, code)
			}
		}
	}
	if required && len(out) == 0 {
		return nil, errors.New("账号不属于允许登录的组")
	}
	return out, nil
}

var nameIDFormats = map[string]saml.NameIDFormat{
	"":             "",
	"persistent":   saml.PersistentNameIDFormat,
	"transient":    saml.TransientNameIDFormat,
	"emailAddress": saml.EmailAddressNameIDFormat,
	"unspecified":  saml.UnspecifiedNameIDFormat,
	"entity":       saml.NameIDFormat("urn:oasis:names:tc:SAML:2.0:nameid-format:entity"),
}

// authnContextPresets 是常见认证上下文类。exact 比较下用列表表达「这些都可接受」，
// 避免 IdP 回了更强的上下文（如 Password 之外又支持 PPT）时被误拒。
var authnContextPresets = map[string][]string{
	"passwordProtectedTransport": {"urn:oasis:names:tc:SAML:2.0:ac:classes:PasswordProtectedTransport"},
	"password": {
		"urn:oasis:names:tc:SAML:2.0:ac:classes:Password",
		"urn:oasis:names:tc:SAML:2.0:ac:classes:PasswordProtectedTransport",
	},
	"mfa": {"https://refeds.org/profile/mfa"},
	"sfa": {"https://refeds.org/profile/sfa"},
}

const maxAuthnContextRefs = 10

// authnContextClassRefs 把配置值解析成可接受的类列表：预置键直接展开，其余按逗号分隔的自定义 URI 处理。
func authnContextClassRefs(v string) ([]string, error) {
	v = strings.TrimSpace(v)
	if v == "" {
		return nil, nil
	}
	if refs, ok := authnContextPresets[v]; ok {
		return refs, nil
	}
	if len(v) > 500 || !utf8.ValidString(v) ||
		strings.ContainsFunc(v, func(r rune) bool { return r < 0x20 || r == 0x7f }) {
		return nil, errors.New("saml: 认证上下文类不合法")
	}
	var out []string
	seen := make(map[string]bool)
	for _, part := range strings.Split(v, ",") {
		uri := strings.TrimSpace(part)
		if uri == "" {
			continue
		}
		if !strings.HasPrefix(uri, "urn:") && !strings.HasPrefix(uri, "http://") && !strings.HasPrefix(uri, "https://") {
			return nil, fmt.Errorf("saml: 认证上下文类必须是 urn: 或 http(s):// URI: %q", uri)
		}
		if strings.ContainsAny(uri, " \t\r\n") {
			return nil, fmt.Errorf("saml: 认证上下文类不能含空白: %q", uri)
		}
		if seen[uri] {
			return nil, fmt.Errorf("saml: 认证上下文类重复: %q", uri)
		}
		seen[uri] = true
		out = append(out, uri)
	}
	if len(out) == 0 {
		return nil, errors.New("saml: 认证上下文类不能为空")
	}
	if len(out) > maxAuthnContextRefs {
		return nil, fmt.Errorf("saml: 认证上下文类最多 %d 个", maxAuthnContextRefs)
	}
	return out, nil
}

// validateMetadata 接受单实体或 EntitiesDescriptor 打包，至少要有一个 IDPSSODescriptor。
func validateMetadata(raw string) error {
	_, err := metadataIDPEntities(raw)
	return err
}

func validateMetadataURL(raw string) error {
	if raw == "" {
		return nil
	}
	if len(raw) > 500 {
		return errors.New("saml: 元数据地址过长")
	}
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.User != nil {
		return errors.New("saml: 元数据地址必须是 http(s) URL")
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

// parseSPPrivateKey 解密并解析 SP 私钥（PKCS#8 PEM）。
func parseSPPrivateKey(keyEnc string) (*rsa.PrivateKey, error) {
	plain, err := decryptSecret(keyEnc)
	if err != nil {
		return nil, err
	}
	block, _ := pem.Decode([]byte(plain))
	if block == nil {
		return nil, errors.New("SP 私钥格式错误")
	}
	parsed, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		return nil, err
	}
	key, ok := parsed.(*rsa.PrivateKey)
	if !ok {
		return nil, errors.New("SP 私钥类型错误")
	}
	return key, nil
}

func parseSPCertPEM(certPEM string) (*x509.Certificate, error) {
	block, _ := pem.Decode([]byte(certPEM))
	if block == nil {
		return nil, errors.New("saml: SP 证书格式错误")
	}
	return x509.ParseCertificate(block.Bytes)
}

// decryptionKeys 返回当前、已发布未切换、上一代（未过期）三把私钥：
// 轮换期间 IdP 可能按其中任意一张证书加密断言。解析失败的额外密钥跳过，不阻断登录。
func (m *Source) decryptionKeys() []*rsa.PrivateKey {
	var out []*rsa.PrivateKey
	add := func(enc string) {
		if strings.TrimSpace(enc) == "" {
			return
		}
		if k, err := parseSPPrivateKey(enc); err == nil {
			out = append(out, k)
		}
	}
	add(m.NextSPKeyEnc)
	if m.PrevSPExpiresAt == nil || time.Now().Before(*m.PrevSPExpiresAt) {
		add(m.PrevSPKeyEnc)
	}
	return out
}

// publishedSPCerts 是元数据里要列出的签名证书：当前 + 已发布未切换 + 上一代（未过期）。
func publishedSPCerts(m *Source) []*x509.Certificate {
	var out []*x509.Certificate
	add := func(certPEM string) {
		if strings.TrimSpace(certPEM) == "" {
			return
		}
		if c, err := parseSPCertPEM(certPEM); err == nil {
			out = append(out, c)
		}
	}
	add(m.SPCert)
	add(m.NextSPCert)
	if m.PrevSPExpiresAt == nil || time.Now().Before(*m.PrevSPExpiresAt) {
		add(m.PrevSPCert)
	}
	return out
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
	entities, err := metadataIDPEntities(m.IdPMetadata)
	if err != nil {
		return nil, fmt.Errorf("saml: %w", err)
	}
	selected, err := selectEntity(entities, m.IDPEntityID)
	if err != nil {
		return nil, fmt.Errorf("saml: %w", err)
	}
	if selected == nil {
		return nil, errors.New("saml: 元数据包含多个 IdP，请先选择 Entity ID")
	}
	key, err := parseSPPrivateKey(m.SPKeyEnc)
	if err != nil {
		return nil, fmt.Errorf("saml: 读取 SP 私钥失败: %w", err)
	}
	cert, err := parseSPCertPEM(m.SPCert)
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
		// 登出消息（请求与应答）都用 SP 私钥签名，IdP 按元数据里的证书验签；
		// 与「是否签 AuthnRequest」解耦，否则关闭请求签名会让 LogoutResponse 也变成裸的。
		LogoutSignatureMethod: dsig.RSASHA256SignatureMethod,
		Key:                   key,
		Certificate:           cert,
		// 轮换期间 IdP 可能仍按下一张或上一张证书加密断言，三把私钥都参与解密。
		DecryptionKeys: m.decryptionKeys(),
		IDPMetadata:    selected,
		// 每个源单独的防重放命名空间，避免不同 IdP 的断言 ID 相撞。
		ReplayCache:       replayCache{c: s.cache, scope: id},
		AllowIDPInitiated: m.AllowIDPInitiated,
		AuthnNameIDFormat: nameIDFormats[m.NameIDFormat],
	}
	// 只有显式开启时才签 AuthnRequest；默认与升级前一致（不签）。
	if m.SignAuthnRequest {
		sp.SignatureMethod = dsig.RSASHA256SignatureMethod
	}
	sp.RequireSignedResponseForCBC = m.RequireSignedCBC
	sp.RejectUnknownConditions = m.RejectUnknownConditions
	// ForceAuthn 为 nil 时请求里不带该属性；显式 false 会被部分 IdP 当成「不要强制」之外的语义。
	if m.ForceAuthn {
		force := true
		sp.ForceAuthn = &force
	}
	refs, err := authnContextClassRefs(m.AuthnContextClassRef)
	if err != nil {
		return nil, err
	}
	if len(refs) > 0 {
		// crewjam 不带自定义比较器时只支持 exact，所以用「可接受类列表」表达「至少这么强」。
		sp.RequestedAuthnContext = &saml.RequestedAuthnContext{Comparison: "exact", AuthnContextClassRefs: refs}
		sp.ValidateAuthnContextClassRef = m.RequireAuthnContext
	}
	return sp, nil
}

// replayCache 把断言 ID 记进共享缓存；多实例下也只有一个请求能消费成功。
type replayCache struct {
	c     cache.Cache
	scope string
}

func (r replayCache) Consume(assertionID string, expiresAt time.Time) error {
	if r.c == nil {
		return errors.New("saml: 缓存不可用，无法做断言防重放")
	}
	ttl := time.Until(expiresAt)
	if ttl <= 0 {
		ttl = time.Minute
	}
	key := "saml:replay:" + r.scope + ":" + assertionID
	ctx := context.Background()
	if c, ok := r.c.(cache.Counter); ok {
		n, _, err := c.Incr(ctx, key, ttl)
		if err != nil {
			return err
		}
		if n > 1 {
			return saml.ErrAssertionReplayed
		}
		return nil
	}
	// 后端不支持原子自增时退化为 Get+Set：有并发窗口，但仍能挡住重复提交。
	if _, err := r.c.Get(ctx, key); err == nil {
		return saml.ErrAssertionReplayed
	}
	return r.c.Set(ctx, key, "1", ttl)
}

// attr 从断言属性里取第一个值；未配置属性名时返回空。
func attr(a *saml.Assertion, name string) string {
	vs := attrValues(a, name)
	if len(vs) == 0 {
		return ""
	}
	return vs[0]
}

// attrValues 取断言里某个属性的全部值（组映射需要多值）。
func attrValues(a *saml.Assertion, name string) []string {
	name = strings.TrimSpace(name)
	if a == nil || name == "" {
		return nil
	}
	for _, st := range a.AttributeStatements {
		for _, at := range st.Attributes {
			if at.Name != name {
				continue
			}
			out := make([]string, 0, len(at.Values))
			for _, v := range at.Values {
				if s := strings.TrimSpace(v.Value); s != "" {
					out = append(out, s)
				}
			}
			return out
		}
	}
	return nil
}
