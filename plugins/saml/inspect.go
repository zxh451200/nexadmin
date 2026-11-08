package saml

import (
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

type IdPInfo struct {
	EntityID         string `json:"entityId"`
	SSOURL           string `json:"ssoUrl"`
	SSOBinding       string `json:"ssoBinding"`
	SLOURL           string `json:"sloUrl"`
	CertNotAfter     string `json:"certNotAfter,omitempty"`
	CertCount        int    `json:"certCount"`
	CertExpired      bool   `json:"certExpired"`
	CertExpiringSoon bool   `json:"certExpiringSoon"`
	MetadataURL      string `json:"metadataUrl,omitempty"`
	Error            string `json:"error,omitempty"`
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

// parseIdP 解析元数据并提取 IdP 侧真正要用的信息；元数据坏了也返回尽力解析的部分。
func parseIdP(raw string) (*IdPInfo, error) {
	info := &IdPInfo{}
	if strings.TrimSpace(raw) == "" {
		return info, errors.New("尚未导入 IdP 元数据")
	}
	var meta saml.EntityDescriptor
	if err := xml.Unmarshal([]byte(raw), &meta); err != nil {
		return info, fmt.Errorf("元数据解析失败: %w", err)
	}
	if len(meta.IDPSSODescriptors) == 0 {
		return info, errors.New("元数据缺少 IDPSSODescriptor")
	}
	d := meta.IDPSSODescriptors[0]
	info.EntityID = meta.EntityID

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
	return info, nil
}

// validEndpoint 只接受 http(s) 端点，避免把奇怪的 scheme 写进跳转地址。
func validEndpoint(loc string) bool {
	u, err := url.Parse(strings.TrimSpace(loc))
	return err == nil && (u.Scheme == "http" || u.Scheme == "https") && u.Host != ""
}

// spCertNotAfter 读取 SP 证书的到期时间。
func spCertNotAfter(certPEM string) (time.Time, error) {
	block, _ := pem.Decode([]byte(certPEM))
	if block == nil {
		return time.Time{}, errors.New("saml: SP 证书格式错误")
	}
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return time.Time{}, err
	}
	return cert.NotAfter, nil
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
	idp, err := parseIdP(m.IdPMetadata)
	idp.MetadataURL = m.IdPMetadataURL
	if err != nil {
		idp.Error = err.Error()
	}
	info.IdP = idp
	return info
}

// testSourceChecks 做不依赖 IdP 交互的配置自检：元数据、端点、证书、SP 密钥，以及（配置了 URL 时）远程元数据一致性。
func (s *Service) testSourceChecks(ctx context.Context, m *Source) TestResult {
	res := TestResult{Checks: make([]Check, 0, 7)}
	add := func(key string, ok, warning bool, detail string) {
		res.Checks = append(res.Checks, Check{Key: key, OK: ok, Warning: warning, Detail: detail})
	}

	idp, err := parseIdP(m.IdPMetadata)
	if err != nil {
		add("metadata", false, false, err.Error())
		res.OK = false
		return res
	}
	add("metadata", true, false, fmt.Sprintf("Entity ID: %s", idp.EntityID))

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
		} else if remote, err := parseIdP(string(raw)); err != nil {
			add("metadataUrl", false, false, "远程元数据不可用："+err.Error())
			res.OK = false
		} else if remote.EntityID != idp.EntityID {
			add("metadataUrl", false, false, fmt.Sprintf("远程 Entity ID 与已保存的元数据不一致：%s ≠ %s", remote.EntityID, idp.EntityID))
			res.OK = false
		} else {
			add("metadataUrl", true, false, "远程元数据与已保存内容一致")
		}
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
