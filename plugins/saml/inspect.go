package saml

import (
	"bytes"
	"context"
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
	"encoding/xml"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/crewjam/saml"
)

// certWarnWindow 是证书「即将过期」的提示窗口（仍可登录，只在界面上提示）。
const certWarnWindow = 30 * 24 * time.Hour

// IdPCandidate 是打包元数据（EntitiesDescriptor）里的一个可选 IdP。
type IdPCandidate struct {
	EntityID         string `json:"entityId"`
	SSOURL           string `json:"ssoUrl"`
	SSOBinding       string `json:"ssoBinding"`
	CertNotAfter     string `json:"certNotAfter,omitempty"`
	CertCount        int    `json:"certCount"`
	CertExpired      bool   `json:"certExpired"`
	CertExpiringSoon bool   `json:"certExpiringSoon"`
}

type IdPInfo struct {
	EntityID         string         `json:"entityId"`
	SSOURL           string         `json:"ssoUrl"`
	SSOBinding       string         `json:"ssoBinding"`
	SLOURL           string         `json:"sloUrl"`
	CertNotAfter     string         `json:"certNotAfter,omitempty"`
	CertCount        int            `json:"certCount"`
	CertExpired      bool           `json:"certExpired"`
	CertExpiringSoon bool           `json:"certExpiringSoon"`
	MetadataURL      string         `json:"metadataUrl,omitempty"`
	Candidates       []IdPCandidate `json:"candidates,omitempty"`
	Error            string         `json:"error,omitempty"`
}

type SPInfo struct {
	EntityID         string `json:"entityId"`
	ACSUrl           string `json:"acsUrl"`
	SLOUrl           string `json:"sloUrl"`
	MetadataURL      string `json:"metadataUrl"`
	LoginURL         string `json:"loginUrl"`
	CertNotAfter     string `json:"certNotAfter,omitempty"`
	CertExpired      bool   `json:"certExpired"`
	CertExpiringSoon bool   `json:"certExpiringSoon"`
}

// SourceInfo 是列表/详情返回给前端的形状：源配置 + 解析后的 IdP 摘要 + SP 地址。
type SourceInfo struct {
	Source
	IdP *IdPInfo `json:"idp"`
	SP  SPInfo   `json:"sp"`
}

type Check struct {
	Key     string `json:"key"`
	OK      bool   `json:"ok"`
	Warning bool   `json:"warning,omitempty"`
	Detail  string `json:"detail,omitempty"`
}

type TestResult struct {
	OK     bool    `json:"ok"`
	Checks []Check `json:"checks"`
}

// metadataIDPEntities 解析单实体或打包元数据，返回其中所有带 IDPSSODescriptor 的实体。
// 打包元数据允许嵌套 EntitiesDescriptor，逐层展开。
func metadataIDPEntities(raw string) ([]saml.EntityDescriptor, error) {
	if strings.TrimSpace(raw) == "" {
		return nil, errors.New("尚未导入 IdP 元数据")
	}
	var out []saml.EntityDescriptor
	var bundle saml.EntitiesDescriptor
	if err := xml.Unmarshal([]byte(raw), &bundle); err == nil {
		flattenIDPs(bundle, &out)
	}
	if len(out) == 0 {
		var one saml.EntityDescriptor
		if err := xml.Unmarshal([]byte(raw), &one); err != nil {
			return nil, fmt.Errorf("元数据解析失败: %w", err)
		}
		if len(one.IDPSSODescriptors) > 0 && strings.TrimSpace(one.EntityID) != "" {
			out = append(out, one)
		}
	}
	if len(out) == 0 {
		return nil, errors.New("元数据缺少 IDPSSODescriptor")
	}
	return out, nil
}

func flattenIDPs(in saml.EntitiesDescriptor, out *[]saml.EntityDescriptor) {
	for _, ed := range in.EntityDescriptors {
		if len(ed.IDPSSODescriptors) > 0 && strings.TrimSpace(ed.EntityID) != "" {
			*out = append(*out, ed)
		}
	}
	for _, nested := range in.EntitiesDescriptors {
		flattenIDPs(nested, out)
	}
}

// selectEntity 按 Entity ID 选实体；want 为空且只有一个 IdP 时自动选中，多个时返回 nil 让调用方提示选择。
func selectEntity(entities []saml.EntityDescriptor, want string) (*saml.EntityDescriptor, error) {
	if want != "" {
		for i := range entities {
			if entities[i].EntityID == want {
				return &entities[i], nil
			}
		}
		return nil, fmt.Errorf("元数据里没有 Entity ID 为 %q 的 IdP", want)
	}
	if len(entities) == 1 {
		return &entities[0], nil
	}
	return nil, nil
}

// describeEntity 提取一个 IdP 实体真正要用的信息。
func describeEntity(ed *saml.EntityDescriptor) IdPInfo {
	info := IdPInfo{EntityID: ed.EntityID}
	if len(ed.IDPSSODescriptors) == 0 {
		return info
	}
	d := ed.IDPSSODescriptors[0]

	var post string
	for _, ep := range d.SingleSignOnServices {
		if !validEndpoint(ep.Location) {
			continue
		}
		switch ep.Binding {
		case saml.HTTPRedirectBinding:
			if info.SSOURL == "" {
				info.SSOURL, info.SSOBinding = ep.Location, "redirect"
			}
		case saml.HTTPPostBinding:
			if post == "" {
				post = ep.Location
			}
		}
	}
	if info.SSOURL == "" && post != "" {
		info.SSOURL, info.SSOBinding = post, "post"
	}

	var sloRedirect, sloPost string
	for _, ep := range d.SingleLogoutServices {
		if !validEndpoint(ep.Location) {
			continue
		}
		switch ep.Binding {
		case saml.HTTPRedirectBinding:
			if sloRedirect == "" {
				sloRedirect = ep.Location
			}
		case saml.HTTPPostBinding:
			if sloPost == "" {
				sloPost = ep.Location
			}
		}
	}
	info.SLOURL = sloRedirect
	if info.SLOURL == "" {
		info.SLOURL = sloPost
	}

	var earliest time.Time
	now := time.Now()
	for _, kd := range d.KeyDescriptors {
		if kd.Use != "" && kd.Use != "signing" {
			continue
		}
		for _, c := range kd.KeyInfo.X509Data.X509Certificates {
			der, err := base64.StdEncoding.DecodeString(strings.Join(strings.Fields(c.Data), ""))
			if err != nil {
				continue
			}
			cert, err := x509.ParseCertificate(der)
			if err != nil {
				continue
			}
			info.CertCount++
			if earliest.IsZero() || cert.NotAfter.Before(earliest) {
				earliest = cert.NotAfter
			}
		}
	}
	if !earliest.IsZero() {
		info.CertNotAfter = earliest.UTC().Format(time.RFC3339)
		info.CertExpired = now.After(earliest)
		info.CertExpiringSoon = !info.CertExpired && earliest.Sub(now) < certWarnWindow
	}
	return info
}

func candidateOf(i IdPInfo) IdPCandidate {
	return IdPCandidate{
		EntityID: i.EntityID, SSOURL: i.SSOURL, SSOBinding: i.SSOBinding,
		CertNotAfter: i.CertNotAfter, CertCount: i.CertCount,
		CertExpired: i.CertExpired, CertExpiringSoon: i.CertExpiringSoon,
	}
}

// parseIdP 解析元数据并按 want 选择实体；want 为空且打包里有多 IdP 时只返回候选列表（EntityID 为空）。
func parseIdP(raw, want string) (*IdPInfo, error) {
	entities, err := metadataIDPEntities(raw)
	if err != nil {
		return &IdPInfo{}, err
	}
	candidates := make([]IdPCandidate, 0, len(entities))
	for i := range entities {
		candidates = append(candidates, candidateOf(describeEntity(&entities[i])))
	}
	selected, err := selectEntity(entities, want)
	if err != nil {
		return &IdPInfo{Candidates: candidates}, err
	}
	if selected == nil {
		return &IdPInfo{Candidates: candidates}, nil
	}
	info := describeEntity(selected)
	info.Candidates = candidates
	return &info, nil
}

// validEndpoint 只接受 http(s) 端点，避免把奇怪的 scheme 写进跳转地址。
func validEndpoint(loc string) bool {
	u, err := url.Parse(strings.TrimSpace(loc))
	return err == nil && (u.Scheme == "http" || u.Scheme == "https") && u.Host != ""
}

// spCertNotAfter 读取 SP 证书的到期时间。
func spCertNotAfter(certPEM string) (time.Time, error) {
	cert, err := parseSPCertPEM(certPEM)
	if err != nil {
		return time.Time{}, err
	}
	return cert.NotAfter, nil
}

// ensureSigningKeyDescriptor 保证元数据里列出某张签名证书（轮换期间会调用多次：
// 当前 / 已发布未切换 / 上一代兜底）。按证书内容去重，而不是「已有任意 signing 就跳过」。
func ensureSigningKeyDescriptor(md *saml.EntityDescriptor, cert *x509.Certificate) {
	if md == nil || cert == nil || len(md.SPSSODescriptors) == 0 {
		return
	}
	d := &md.SPSSODescriptors[0]
	data := base64.StdEncoding.EncodeToString(cert.Raw)
	for _, kd := range d.KeyDescriptors {
		for _, c := range kd.KeyInfo.X509Data.X509Certificates {
			if strings.Join(strings.Fields(c.Data), "") == data {
				return
			}
		}
	}
	d.KeyDescriptors = append(d.KeyDescriptors, saml.KeyDescriptor{
		Use: "signing",
		KeyInfo: saml.KeyInfo{
			X509Data: saml.X509Data{
				X509Certificates: []saml.X509Certificate{{
					Data: data,
				}},
			},
		},
	})
}

// hasEncryptedAssertion 检查原始 SAMLResponse 里是否有 saml:EncryptedAssertion：
// 开了「要求加密断言」的源必须在库解密前先拒绝明文断言（库本身两种都接受）。
func hasEncryptedAssertion(raw []byte) bool {
	dec := xml.NewDecoder(bytes.NewReader(raw))
	for {
		tok, err := dec.Token()
		if err != nil {
			return false
		}
		if se, ok := tok.(xml.StartElement); ok &&
			se.Name.Local == "EncryptedAssertion" &&
			se.Name.Space == "urn:oasis:names:tc:SAML:2.0:assertion" {
			return true
		}
	}
}

func spInfo(m *Source, base string) SPInfo {
	prefix := base + "/api/v1/auth/saml/" + m.ID.String()
	out := SPInfo{
		EntityID:    prefix + "/metadata",
		ACSUrl:      prefix + "/acs",
		SLOUrl:      prefix + "/slo",
		MetadataURL: prefix + "/metadata",
		LoginURL:    prefix + "/login",
	}
	if notAfter, err := spCertNotAfter(m.SPCert); err == nil {
		now := time.Now()
		out.CertNotAfter = notAfter.UTC().Format(time.RFC3339)
		out.CertExpired = now.After(notAfter)
		out.CertExpiringSoon = !out.CertExpired && notAfter.Sub(now) < certWarnWindow
	}
	return out
}

// inspectSource 组装单个源的展示形状；IdP 解析失败不吞掉，放进 idp.error 让界面提示。
func inspectSource(m *Source, base string) SourceInfo {
	info := SourceInfo{Source: *m, SP: spInfo(m, base)}
	idp, err := parseIdP(m.IdPMetadata, m.IDPEntityID)
	idp.MetadataURL = m.IdPMetadataURL
	if err != nil {
		idp.Error = err.Error()
	}
	info.IdP = idp
	return info
}

// testSourceChecks 做不依赖 IdP 交互的配置自检：元数据、端点、证书、SP 密钥，以及（配置了 URL 时）远程元数据一致性。
func (s *Service) testSourceChecks(ctx context.Context, m *Source) TestResult {
	res := TestResult{Checks: make([]Check, 0, 10)}
	add := func(key string, ok, warning bool, detail string) {
		res.Checks = append(res.Checks, Check{Key: key, OK: ok, Warning: warning, Detail: detail})
	}

	idp, err := parseIdP(m.IdPMetadata, m.IDPEntityID)
	if err != nil {
		add("metadata", false, false, err.Error())
		res.OK = false
		return res
	}
	if idp.EntityID == "" {
		add("metadata", false, false, fmt.Sprintf("元数据包含 %d 个 IdP，请先选择 Entity ID", len(idp.Candidates)))
		add("entity", false, false, "未选择 Entity ID")
		res.OK = false
		return res
	}
	add("metadata", true, false, fmt.Sprintf("Entity ID: %s", idp.EntityID))
	add("entity", true, false, fmt.Sprintf("已选择 %s（元数据共 %d 个 IdP）", idp.EntityID, len(idp.Candidates)))

	if idp.SSOURL == "" {
		add("ssoUrl", false, false, "元数据里没有 HTTP-Redirect / HTTP-POST 的 SingleSignOnService")
		res.OK = false
	} else {
		add("ssoUrl", true, false, fmt.Sprintf("%s（%s）", idp.SSOURL, idp.SSOBinding))
	}

	switch {
	case idp.CertCount == 0:
		add("idpCert", false, false, "元数据里没有可解析的签名证书")
		res.OK = false
	case idp.CertExpired:
		add("idpCert", false, false, "签名证书已过期："+dateOnly(idp.CertNotAfter))
		res.OK = false
	case idp.CertExpiringSoon:
		add("idpCert", true, true, "签名证书即将过期："+dateOnly(idp.CertNotAfter))
	default:
		add("idpCert", true, false, fmt.Sprintf("%d 张签名证书，最早到期 %s", idp.CertCount, dateOnly(idp.CertNotAfter)))
	}

	if plain, err := decryptSecret(m.SPKeyEnc); err != nil {
		add("spKey", false, false, "读取 SP 私钥失败："+err.Error())
		res.OK = false
	} else if err := parsePrivateKey(plain); err != nil {
		add("spKey", false, false, "SP 私钥不可用："+err.Error())
		res.OK = false
	} else {
		add("spKey", true, false, "SP 私钥可解密并解析")
	}

	if notAfter, err := spCertNotAfter(m.SPCert); err != nil {
		add("spCert", false, false, "SP 证书不可用："+err.Error())
		res.OK = false
	} else if time.Now().After(notAfter) {
		add("spCert", false, false, "SP 证书已过期："+notAfter.UTC().Format("2006-01-02"))
		res.OK = false
	} else if time.Until(notAfter) < certWarnWindow {
		add("spCert", true, true, "SP 证书即将过期："+notAfter.UTC().Format("2006-01-02"))
	} else {
		add("spCert", true, false, "到期时间 "+notAfter.UTC().Format("2006-01-02"))
	}

	if m.IdPMetadataURL != "" {
		raw, err := fetchMetadata(ctx, m.IdPMetadataURL)
		if err != nil {
			add("metadataUrl", false, false, err.Error())
			res.OK = false
		} else if remote, err := parseIdP(string(raw), idp.EntityID); err != nil {
			add("metadataUrl", false, false, "远程元数据不可用："+err.Error())
			res.OK = false
		} else if remote.EntityID != idp.EntityID {
			add("metadataUrl", false, false, fmt.Sprintf("远程 Entity ID 与已保存的元数据不一致：%s ≠ %s", remote.EntityID, idp.EntityID))
			res.OK = false
		} else {
			add("metadataUrl", true, false, "远程元数据与已保存内容一致")
		}
	}

	if refs, err := authnContextClassRefs(m.AuthnContextClassRef); err != nil {
		add("authnContext", false, false, err.Error())
		res.OK = false
	} else if len(refs) > 0 {
		detail := strings.Join(refs, ", ")
		if m.RequireAuthnContext {
			add("authnContext", true, false, "断言必须命中其中之一："+detail)
		} else {
			add("authnContext", true, true, "只在 AuthnRequest 里声明，未强制校验断言："+detail)
		}
	} else if m.RequireAuthnContext {
		add("authnContext", false, false, "已开启强制校验但没有指定认证上下文类")
		res.OK = false
	}

	if !m.Enabled {
		add("enabled", true, true, "该身份源当前未启用，测试与登录入口不会出现在登录页")
	}
	return res
}

// dateOnly 把 RFC3339 时间裁成日期；解析失败时原样返回。
func dateOnly(v string) string {
	t, err := time.Parse(time.RFC3339, v)
	if err != nil {
		return v
	}
	return t.UTC().Format("2006-01-02")
}

// parsePrivateKey 校验私钥 PEM（PKCS#8）可解析。
func parsePrivateKey(pemText string) error {
	block, _ := pem.Decode([]byte(pemText))
	if block == nil {
		return errors.New("私钥格式错误")
	}
	_, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	return err
}
