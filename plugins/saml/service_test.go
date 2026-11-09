package saml

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/pem"
	"errors"
	"fmt"
	"math/big"
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/beevik/etree"
	"github.com/crewjam/saml"
	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
	"github.com/russellhaering/goxmldsig"

	"nexadmin/core/auth"
	"nexadmin/core/cache"
	"nexadmin/core/config"
	"nexadmin/pkg/idgen"
)

func certB64(t *testing.T) string {
	t.Helper()
	certPEM, _, err := newSPKeyPair()
	if err != nil {
		t.Fatal(err)
	}
	block, _ := pem.Decode([]byte(certPEM))
	if block == nil {
		t.Fatal("bad pem")
	}
	return base64.StdEncoding.EncodeToString(block.Bytes)
}

func metadataXMLFor(entityID, cert string) string {
	return fmt.Sprintf(`<?xml version="1.0"?>
<EntityDescriptor xmlns="urn:oasis:names:tc:SAML:2.0:metadata" entityID="%s">
  <IDPSSODescriptor protocolSupportEnumeration="urn:oasis:names:tc:SAML:2.0:protocol">
    <KeyDescriptor use="signing">
      <ds:KeyInfo xmlns:ds="http://www.w3.org/2000/09/xmldsig#">
        <ds:X509Data><ds:X509Certificate>%s</ds:X509Certificate></ds:X509Data>
      </ds:KeyInfo>
    </KeyDescriptor>
    <SingleSignOnService Binding="urn:oasis:names:tc:SAML:2.0:bindings:HTTP-POST" Location="https://idp.example.com/sso/post"/>
    <SingleSignOnService Binding="urn:oasis:names:tc:SAML:2.0:bindings:HTTP-Redirect" Location="https://idp.example.com/sso/redirect"/>
    <SingleLogoutService Binding="urn:oasis:names:tc:SAML:2.0:bindings:HTTP-Redirect" Location="https://idp.example.com/slo"/>
  </IDPSSODescriptor>
</EntityDescriptor>`, entityID, cert)
}

func metadataXML(cert string) string { return metadataXMLFor("https://idp.example.com", cert) }

// bundleXML 是两个 EntityDescriptor 的打包元数据（模拟 ADFS/联邦导出）。
func bundleXML(cert string) string {
	one := strings.TrimPrefix(metadataXMLFor("https://idp1.example.com", cert), `<?xml version="1.0"?>`)
	two := strings.TrimPrefix(metadataXMLFor("https://idp2.example.com", cert), `<?xml version="1.0"?>`)
	return `<?xml version="1.0"?>
<EntitiesDescriptor xmlns="urn:oasis:names:tc:SAML:2.0:metadata">` + one + two + `</EntitiesDescriptor>`
}

func TestValidateMetadata(t *testing.T) {
	good := metadataXML(certB64(t))
	if err := validateMetadata(good); err != nil {
		t.Fatalf("valid metadata rejected: %v", err)
	}
	for name, raw := range map[string]string{
		"broken xml":  "<EntityDescriptor",
		"no idp desc": `<EntityDescriptor xmlns="urn:oasis:names:tc:SAML:2.0:metadata" entityID="x"/>`,
	} {
		if err := validateMetadata(raw); err == nil {
			t.Errorf("%s must be rejected", name)
		}
	}
}

func TestParseIdPExtractsEndpointsAndCert(t *testing.T) {
	cert := certB64(t)
	info, err := parseIdP(metadataXML(cert), "")
	if err != nil {
		t.Fatal(err)
	}
	if info.EntityID != "https://idp.example.com" {
		t.Errorf("entity id: %s", info.EntityID)
	}
	// Redirect 优先，便于 SP 端 302 跳转。
	if info.SSOURL != "https://idp.example.com/sso/redirect" || info.SSOBinding != "redirect" {
		t.Errorf("sso: %s / %s", info.SSOURL, info.SSOBinding)
	}
	if info.SLOURL != "https://idp.example.com/slo" {
		t.Errorf("slo: %s", info.SLOURL)
	}
	if info.CertCount != 1 || info.CertNotAfter == "" || info.CertExpired {
		t.Errorf("cert: count=%d notAfter=%s expired=%v", info.CertCount, info.CertNotAfter, info.CertExpired)
	}
}

func TestParseIdPRejectsMissingDescriptor(t *testing.T) {
	if _, err := parseIdP(`<EntityDescriptor xmlns="urn:oasis:names:tc:SAML:2.0:metadata"/>`, ""); err == nil {
		t.Fatal("metadata without IDPSSODescriptor must be rejected")
	}
}

func TestMappedRoleCodes(t *testing.T) {
	ms := []GroupRoleMapping{
		{ExternalGroup: "cn=admins,ou=groups", RoleCodes: []string{"operator"}},
		{ExternalGroup: "cn=ops,ou=groups", RoleCodes: []string{"auditor", "operator"}},
	}
	got, err := mappedRoleCodes([]string{"cn=ops,ou=groups"}, ms, true)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(got, ",") != "auditor,operator" {
		t.Errorf("mapped: %v", got)
	}
	// 组值精确匹配：大小写不同不算命中，required 时拒绝。
	if _, err := mappedRoleCodes([]string{"CN=OPS,OU=GROUPS"}, ms, true); err == nil {
		t.Fatal("group match must be exact")
	}
	// 未命中且不强制时返回空角色（沿用默认角色）。
	if got, err := mappedRoleCodes([]string{"x"}, ms, false); err != nil || len(got) != 0 {
		t.Errorf("unrequired miss: %v %v", got, err)
	}
}

func TestValidateGroupMappings(t *testing.T) {
	dup := []GroupRoleMapping{
		{ExternalGroup: "g1", RoleCodes: []string{"a"}},
		{ExternalGroup: "g1", RoleCodes: []string{"b"}},
	}
	if err := validateGroupMappings(dup, false); err == nil {
		t.Fatal("duplicate group must be rejected")
	}
	if err := validateGroupMappings(nil, true); err == nil {
		t.Fatal("required with no mapping must be rejected")
	}
	if err := validateGroupMappings([]GroupRoleMapping{{ExternalGroup: "g", RoleCodes: []string{"super"}}}, false); err == nil {
		t.Fatal("role code super must be rejected")
	}
	if err := validateGroupMappings([]GroupRoleMapping{{ExternalGroup: "g", RoleCodes: []string{"ok"}}}, true); err != nil {
		t.Fatal(err)
	}
}

func TestSourceFormValidate(t *testing.T) {
	meta := metadataXML(certB64(t))
	base := SourceForm{Name: "corp", IdPMetadata: meta}
	f := base
	if err := f.validate(); err != nil {
		t.Fatal(err)
	}
	if f.AttrUsername != "uid" || f.AttrEmail != "mail" || f.AttrNickname != "displayName" {
		t.Errorf("defaults: %+v", f)
	}
	for name, mutate := range map[string]func(*SourceForm){
		"empty name":         func(f *SourceForm) { f.Name = " " },
		"missing metadata":   func(f *SourceForm) { f.IdPMetadata = "" },
		"bad metadata url":   func(f *SourceForm) { f.IdPMetadataURL = "ftp://idp/meta" },
		"bad nameid format":  func(f *SourceForm) { f.NameIDFormat = "opaque" },
		"attr with space":    func(f *SourceForm) { f.AttrUsername = "user name" },
		"bad default role":   func(f *SourceForm) { f.DefaultRole = "super" },
		"required no group":  func(f *SourceForm) { f.RequireMapped = true },
		"group without role": func(f *SourceForm) { f.GroupMappings = []GroupRoleMapping{{ExternalGroup: "g"}} },
	} {
		f := base
		mutate(&f)
		if err := f.validate(); err == nil {
			t.Errorf("%s must be rejected", name)
		}
	}
	// 完整 URN 形式的属性名（旧 VARCHAR(64) 放不下）要能保存。
	f = base
	f.AttrEmail = "http://schemas.xmlsoap.org/ws/2005/05/identity/claims/emailaddress"
	if err := f.validate(); err != nil {
		t.Errorf("URI attribute name rejected: %v", err)
	}
}

func TestProviderSecurityOptions(t *testing.T) {
	meta := metadataXML(certB64(t))
	certPEM, keyEnc, err := newSPKeyPair()
	if err != nil {
		t.Fatal(err)
	}
	m := &Source{Name: "corp", IdPMetadata: meta, SPCert: certPEM, SPKeyEnc: keyEnc,
		AllowIDPInitiated: true, SignAuthnRequest: true, NameIDFormat: "persistent"}
	m.ID = 123
	sp, err := (&Service{}).provider(m, "https://admin.example.com")
	if err != nil {
		t.Fatal(err)
	}
	if !sp.AllowIDPInitiated {
		t.Error("AllowIDPInitiated not applied")
	}
	if sp.SignatureMethod != dsig.RSASHA256SignatureMethod {
		t.Errorf("signature method: %q", sp.SignatureMethod)
	}
	if sp.AuthnNameIDFormat != saml.PersistentNameIDFormat {
		t.Errorf("name id format: %q", sp.AuthnNameIDFormat)
	}
	if sp.ReplayCache == nil {
		t.Error("replay cache must be configured")
	}
	if sp.EntityID != "https://admin.example.com/api/v1/auth/saml/123/metadata" {
		t.Errorf("entity id: %s", sp.EntityID)
	}
	if sp.AcsURL.String() != "https://admin.example.com/api/v1/auth/saml/123/acs" {
		t.Errorf("acs: %s", sp.AcsURL.String())
	}
}

func TestProviderProtocolOptions(t *testing.T) {
	meta := metadataXML(certB64(t))
	certPEM, keyEnc, err := newSPKeyPair()
	if err != nil {
		t.Fatal(err)
	}
	m := &Source{Name: "corp", IdPMetadata: meta, SPCert: certPEM, SPKeyEnc: keyEnc,
		RequireSignedCBC: true, RejectUnknownConditions: true, ForceAuthn: true,
		AuthnContextClassRef: "mfa", RequireAuthnContext: true}
	m.ID = 124
	sp, err := (&Service{}).provider(m, "https://admin.example.com")
	if err != nil {
		t.Fatal(err)
	}
	if !sp.RequireSignedResponseForCBC || !sp.RejectUnknownConditions {
		t.Error("hardening options not applied")
	}
	if sp.ForceAuthn == nil || !*sp.ForceAuthn {
		t.Error("ForceAuthn not applied")
	}
	if !sp.ValidateAuthnContextClassRef {
		t.Error("ValidateAuthnContextClassRef not applied")
	}
	if sp.RequestedAuthnContext == nil || sp.RequestedAuthnContext.Comparison != "exact" {
		t.Fatalf("RequestedAuthnContext: %+v", sp.RequestedAuthnContext)
	}
	if got := sp.RequestedAuthnContext.ClassRefs(); len(got) != 1 || got[0] != "https://refeds.org/profile/mfa" {
		t.Errorf("class refs: %v", got)
	}

	// 默认必须保持关闭，否则升级后原本能登录的源会突然被拒。
	m2 := &Source{Name: "corp", IdPMetadata: meta, SPCert: certPEM, SPKeyEnc: keyEnc}
	m2.ID = 125
	sp2, err := (&Service{}).provider(m2, "https://admin.example.com")
	if err != nil {
		t.Fatal(err)
	}
	if sp2.RequireSignedResponseForCBC || sp2.RejectUnknownConditions || sp2.ForceAuthn != nil ||
		sp2.RequestedAuthnContext != nil || sp2.ValidateAuthnContextClassRef {
		t.Error("protocol options must stay off by default")
	}
}

func TestAuthnContextClassRefs(t *testing.T) {
	cases := []struct {
		in      string
		want    []string
		wantErr bool
	}{
		{"", nil, false},
		{"mfa", []string{"https://refeds.org/profile/mfa"}, false},
		{"passwordProtectedTransport", []string{"urn:oasis:names:tc:SAML:2.0:ac:classes:PasswordProtectedTransport"}, false},
		{"urn:a, https://b", []string{"urn:a", "https://b"}, false},
		{"aes", nil, true},
		{"urn:a,urn:a", nil, true},
		{"ftp://x", nil, true},
	}
	for _, c := range cases {
		got, err := authnContextClassRefs(c.in)
		if c.wantErr {
			if err == nil {
				t.Errorf("%q: want error", c.in)
			}
			continue
		}
		if err != nil {
			t.Errorf("%q: %v", c.in, err)
			continue
		}
		if strings.Join(got, "|") != strings.Join(c.want, "|") {
			t.Errorf("%q: got %v want %v", c.in, got, c.want)
		}
	}
}

func TestAttrValuesMulti(t *testing.T) {
	a := &saml.Assertion{AttributeStatements: []saml.AttributeStatement{{
		Attributes: []saml.Attribute{{
			Name:   "groups",
			Values: []saml.AttributeValue{{Value: " g1 "}, {Value: ""}, {Value: "g2"}},
		}},
	}}}
	got := attrValues(a, "groups")
	if strings.Join(got, ",") != "g1,g2" {
		t.Errorf("values: %v", got)
	}
	if attrValues(a, "missing") != nil {
		t.Error("missing attribute must return nil")
	}
}

func TestMetadataURLValidationAndSSRFGuard(t *testing.T) {
	for _, raw := range []string{"file:///etc/passwd", "ftp://idp/meta", "https://user:pass@idp/meta", ""} {
		if raw == "" {
			continue
		}
		if err := validateMetadataURL(raw); err == nil {
			t.Errorf("%s must be rejected", raw)
		}
	}
	if err := validateMetadataURL("https://idp.example.com/metadata"); err != nil {
		t.Fatal(err)
	}
	setAllowPrivateHosts(false)
	t.Cleanup(func() { setAllowPrivateHosts(false) })
	for _, host := range []string{"127.0.0.1", "localhost", "169.254.169.254", "10.0.0.5", "[::1]"} {
		if err := checkFetchHost(context.Background(), host); err == nil {
			t.Errorf("%s must be blocked", host)
		}
	}
	setAllowPrivateHosts(true)
	if err := checkFetchHost(context.Background(), "127.0.0.1"); err != nil {
		t.Errorf("private host must be allowed when configured: %v", err)
	}
}

func TestSourceReady(t *testing.T) {
	cert := certB64(t)
	certPEM, keyEnc, err := newSPKeyPair()
	if err != nil {
		t.Fatal(err)
	}
	m := &Source{IdPMetadata: metadataXML(cert), SPCert: certPEM, SPKeyEnc: keyEnc}
	if err := m.ready(); err != nil {
		t.Fatalf("ready source rejected: %v", err)
	}
	// 元数据里没有证书时不能启用（断言无法验签）。
	m.IdPMetadata = withoutCerts(metadataXML(cert))
	if err := m.ready(); err == nil {
		t.Fatal("source without idp cert must not be ready")
	}
}

func withoutCerts(raw string) string {
	start := strings.Index(raw, "<KeyDescriptor")
	end := strings.Index(raw, "</KeyDescriptor>")
	if start < 0 || end < start {
		return raw
	}
	return raw[:start] + raw[end+len("</KeyDescriptor>"):]
}

func TestReplayCache(t *testing.T) {
	c := newMemCache()
	r := replayCache{c: c, scope: "42"}
	if err := r.Consume("_assertion-1", time.Now().Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	if err := r.Consume("_assertion-1", time.Now().Add(time.Minute)); !errors.Is(err, saml.ErrAssertionReplayed) {
		t.Fatalf("second consume: %v", err)
	}
	if err := r.Consume("_assertion-2", time.Now().Add(time.Minute)); err != nil {
		t.Fatalf("different assertion rejected: %v", err)
	}
	if err := (replayCache{}).Consume("x", time.Now()); err == nil {
		t.Fatal("nil cache must fail closed")
	}
}

type memCache struct {
	mu sync.Mutex
	m  map[string]string
}

func newMemCache() *memCache { return &memCache{m: map[string]string{}} }

func (c *memCache) Get(_ context.Context, key string) (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	v, ok := c.m[key]
	if !ok {
		return "", cache.ErrNotFound
	}
	return v, nil
}

func (c *memCache) Set(_ context.Context, key, value string, _ time.Duration) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.m[key] = value
	return nil
}

func (c *memCache) Del(_ context.Context, keys ...string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, k := range keys {
		delete(c.m, k)
	}
	return nil
}

func (c *memCache) Exists(_ context.Context, key string) (bool, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	_, ok := c.m[key]
	return ok, nil
}

func (c *memCache) Close() error { return nil }

func (c *memCache) Incr(_ context.Context, key string, ttl time.Duration) (int64, time.Duration, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	n := int64(1)
	if v, ok := c.m[key]; ok {
		var cur int64
		_, _ = fmt.Sscan(v, &cur)
		n = cur + 1
	}
	c.m[key] = fmt.Sprint(n)
	return n, ttl, nil
}

func TestParseIdPBundle(t *testing.T) {
	cert := certB64(t)
	info, err := parseIdP(bundleXML(cert), "")
	if err != nil {
		t.Fatal(err)
	}
	if info.EntityID != "" {
		t.Errorf("ambiguous bundle must not auto-select, got %q", info.EntityID)
	}
	if len(info.Candidates) != 2 {
		t.Fatalf("candidates: %d", len(info.Candidates))
	}
	sel, err := parseIdP(bundleXML(cert), "https://idp2.example.com")
	if err != nil {
		t.Fatal(err)
	}
	if sel.EntityID != "https://idp2.example.com" || len(sel.Candidates) != 2 {
		t.Errorf("selected: %q candidates=%d", sel.EntityID, len(sel.Candidates))
	}
	if _, err := parseIdP(bundleXML(cert), "https://nope.example.com"); err == nil {
		t.Fatal("unknown entity id must be rejected")
	}
	if err := validateMetadata(bundleXML(cert)); err != nil {
		t.Fatalf("bundle metadata rejected: %v", err)
	}
}

func TestValidateEntitySelection(t *testing.T) {
	cert := certB64(t)
	f := SourceForm{Name: "corp", IdPMetadata: metadataXML(cert)}
	if err := f.validate(); err != nil {
		t.Fatal(err)
	}
	if f.IDPEntityID != "https://idp.example.com" {
		t.Errorf("single entity must be auto-selected, got %q", f.IDPEntityID)
	}
	// 打包元数据未选择时允许存草稿（启用会被 ready 拒绝）。
	f = SourceForm{Name: "corp", IdPMetadata: bundleXML(cert)}
	if err := f.validate(); err != nil {
		t.Fatalf("draft bundle should be accepted: %v", err)
	}
	if f.IDPEntityID != "" {
		t.Errorf("bundle must not auto-select, got %q", f.IDPEntityID)
	}
	f.IDPEntityID = "https://idp2.example.com"
	if err := f.validate(); err != nil {
		t.Fatal(err)
	}
	f.IDPEntityID = "https://nope.example.com"
	if err := f.validate(); err == nil {
		t.Fatal("unknown entity id must be rejected")
	}
}

func TestReadyBundleNeedsSelection(t *testing.T) {
	cert := certB64(t)
	certPEM, keyEnc, err := newSPKeyPair()
	if err != nil {
		t.Fatal(err)
	}
	m := &Source{IdPMetadata: bundleXML(cert), SPCert: certPEM, SPKeyEnc: keyEnc}
	if err := m.ready(); err == nil {
		t.Fatal("ambiguous bundle must not be ready")
	}
	m.IDPEntityID = "https://idp2.example.com"
	if err := m.ready(); err != nil {
		t.Fatalf("selected bundle rejected: %v", err)
	}
}

func TestRotateKeyPairProducesNewCert(t *testing.T) {
	a, _, err := newSPKeyPair()
	if err != nil {
		t.Fatal(err)
	}
	b, _, err := newSPKeyPair()
	if err != nil {
		t.Fatal(err)
	}
	if a == b {
		t.Fatal("rotated key pair must produce a different certificate")
	}
}

func TestPlanRefresh(t *testing.T) {
	cert := certB64(t)
	bundle := bundleXML(cert)
	m := &Source{IdPMetadata: bundle, IDPEntityID: "https://idp2.example.com"}
	id, changed, err := planRefresh(m, bundle)
	if err != nil {
		t.Fatal(err)
	}
	if id != "https://idp2.example.com" || changed {
		t.Errorf("id=%q changed=%v", id, changed)
	}
	// 远端元数据里已没有选中的 IdP：拒绝替换，不静默切到另一个。
	if _, _, err := planRefresh(m, metadataXMLFor("https://idp1.example.com", cert)); err == nil {
		t.Fatal("missing selected entity must be rejected")
	}
	// 打包元数据未选择 Entity ID 时不能自动刷新。
	if _, _, err := planRefresh(&Source{}, bundle); err == nil {
		t.Fatal("ambiguous bundle must be rejected")
	}
	// 缺少签名证书的远端内容不替换。
	if _, _, err := planRefresh(m, metadataXMLFor("https://idp2.example.com", "")); err == nil {
		t.Fatal("metadata without cert must be rejected")
	}
}

func TestFetchMetadataLocal(t *testing.T) {
	body := metadataXML(certB64(t))
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(body))
	}))
	defer srv.Close()
	setAllowPrivateHosts(true)
	t.Cleanup(func() { setAllowPrivateHosts(false) })
	raw, err := fetchMetadata(context.Background(), srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	if string(raw) != body {
		t.Fatalf("body mismatch: %d bytes", len(raw))
	}

	big := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(strings.Repeat("x", maxMetadataBytes+16)))
	}))
	defer big.Close()
	if _, err := fetchMetadata(context.Background(), big.URL); err == nil {
		t.Fatal("oversized metadata must be rejected")
	}
}

func TestMetadataRefreshHours(t *testing.T) {
	t.Setenv("NEXADMIN_SAML_METADATA_REFRESH_HOURS", "0")
	if got := loadMetadataRefreshHours(); got != 0 {
		t.Errorf("explicit 0 must disable, got %d", got)
	}
	t.Setenv("NEXADMIN_SAML_METADATA_REFRESH_HOURS", "6")
	if got := loadMetadataRefreshHours(); got != 6 {
		t.Errorf("env override: got %d", got)
	}
}

// fakeSAMLIssuer 只实现会话相关能力：签发结果由测试直接构造，不启动 DB。
type fakeSAMLIssuer struct {
	claims  *auth.Claims
	revoked []string
}

func (*fakeSAMLIssuer) LoginAs(context.Context, auth.LoginAsRequest) (*auth.LoginOutcome, error) {
	return nil, nil
}

func (f *fakeSAMLIssuer) ParseAccess(context.Context, string) (*auth.Claims, error) {
	return f.claims, nil
}

func (f *fakeSAMLIssuer) RevokeByJTI(_ context.Context, accessJTI, refreshJTI string) error {
	f.revoked = append(f.revoked, accessJTI+"/"+refreshJTI)
	return nil
}

func claimsFor(id string) *auth.Claims {
	return &auth.Claims{
		UserID:   idgen.ID(5),
		TenantID: idgen.ID(1),
		RID:      "ref-" + id,
		RegisteredClaims: jwt.RegisteredClaims{
			ID:        id,
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour)),
		},
	}
}

func TestRecordAndRevokeSAMLSessions(t *testing.T) {
	c, err := cache.New(config.Cache{Driver: "memory"})
	if err != nil {
		t.Fatal(err)
	}
	iss := &fakeSAMLIssuer{claims: claimsFor("acc-1")}
	svc := NewService(nil, iss, c)
	m := &Source{}
	m.ID = idgen.ID(9)
	const nameID = "alice@example.com"

	appendSession(context.Background(), c, m.ID, nameID, claimsFor("acc-1"))
	appendSession(context.Background(), c, m.ID, nameID, claimsFor("acc-2"))
	if len(iss.revoked) != 0 {
		t.Fatalf("recording must not revoke anything, got %v", iss.revoked)
	}

	// IdP 发起的 SLO 只有 NameID：两条本地会话都要被吊销。
	svc.revokeSessionsByNameID(context.Background(), m, nameID)
	if len(iss.revoked) != 2 {
		t.Fatalf("want 2 revoked sessions, got %v", iss.revoked)
	}
	if iss.revoked[0] != "acc-1/ref-acc-1" || iss.revoked[1] != "acc-2/ref-acc-2" {
		t.Fatalf("unexpected revocations: %v", iss.revoked)
	}

	// 会话记录消费后应删除：重复 SLO 不再吊销，也不报错。
	svc.revokeSessionsByNameID(context.Background(), m, nameID)
	// 未知 NameID 按规范当作成功（没有可登出的会话）。
	svc.revokeSessionsByNameID(context.Background(), m, "nobody@example.com")
	if len(iss.revoked) != 2 {
		t.Fatalf("repeat/unknown logout must be a no-op, got %v", iss.revoked)
	}
}

func TestIsLogoutRequest(t *testing.T) {
	get := httptest.NewRequest(http.MethodGet, "https://sp.example.com/slo?SAMLRequest=abc", nil)
	if !isLogoutRequest(get) {
		t.Fatal("GET SAMLRequest must be recognized as LogoutRequest")
	}
	post := httptest.NewRequest(http.MethodPost, "https://sp.example.com/slo", strings.NewReader("SAMLRequest=abc"))
	post.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if !isLogoutRequest(post) {
		t.Fatal("POST SAMLRequest must be recognized as LogoutRequest")
	}
	resp := httptest.NewRequest(http.MethodPost, "https://sp.example.com/slo", strings.NewReader("SAMLResponse=abc"))
	resp.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if isLogoutRequest(resp) {
		t.Fatal("LogoutResponse must not be treated as LogoutRequest")
	}
}

// idpSigner 是测试用的 IdP 密钥对：用它签发 metadata 与 LogoutRequest。
type idpSigner struct {
	key   *rsa.PrivateKey
	cert  *x509.Certificate
	certB string
}

func newIDPSigner(t *testing.T) *idpSigner {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(7),
		Subject:      pkix.Name{CommonName: "test-idp"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	cert, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatal(err)
	}
	return &idpSigner{key: key, cert: cert, certB: base64.StdEncoding.EncodeToString(der)}
}

func (s *idpSigner) signedLogoutRequest(t *testing.T, entityID, destination, nameID string) string {
	t.Helper()
	raw := fmt.Sprintf(`<samlp:LogoutRequest xmlns:samlp="urn:oasis:names:tc:SAML:2.0:protocol" xmlns:saml="urn:oasis:names:tc:SAML:2.0:assertion" ID="lr-1" Version="2.0" IssueInstant="%s" Destination="%s"><saml:Issuer>%s</saml:Issuer><saml:NameID>%s</saml:NameID></samlp:LogoutRequest>`,
		time.Now().UTC().Format(time.RFC3339), destination, entityID, nameID)
	doc := etree.NewDocument()
	if err := doc.ReadFromString(raw); err != nil {
		t.Fatal(err)
	}
	ctx, err := dsig.NewSigningContext(s.key, [][]byte{s.cert.Raw})
	if err != nil {
		t.Fatal(err)
	}
	signed, err := ctx.SignEnveloped(doc.Root())
	if err != nil {
		t.Fatal(err)
	}
	out := etree.NewDocument()
	out.SetRoot(signed)
	buf, err := out.WriteToBytes()
	if err != nil {
		t.Fatal(err)
	}
	return base64.StdEncoding.EncodeToString(buf)
}

func TestIDPInitiatedLogoutRejectsUnsignedAndRevokesSigned(t *testing.T) {
	gin.SetMode(gin.TestMode)
	const entityID = "https://idp.example.com"
	signer := newIDPSigner(t)
	certPEM, keyEnc, err := newSPKeyPair()
	if err != nil {
		t.Fatal(err)
	}
	m := &Source{Name: "idp", Enabled: true, IDPEntityID: entityID, IdPMetadata: metadataXMLFor(entityID, signer.certB), SPCert: certPEM, SPKeyEnc: keyEnc}
	m.ID = idgen.ID(42)

	c, err := cache.New(config.Cache{Driver: "memory"})
	if err != nil {
		t.Fatal(err)
	}
	iss := &fakeSAMLIssuer{claims: claimsFor("acc-1")}
	svc := NewService(nil, iss, c)
	const base = "http://sp.example.com"
	sp, err := svc.provider(m, base)
	if err != nil {
		t.Fatal(err)
	}
	destination := base + "/api/v1/auth/saml/42/slo"
	const nameID = "alice@example.com"
	appendSession(context.Background(), c, m.ID, nameID, claimsFor("acc-1"))

	post := func(payload string) (*httptest.ResponseRecorder, *gin.Context) {
		form := url.Values{"SAMLRequest": {payload}}
		r := httptest.NewRequest(http.MethodPost, destination, strings.NewReader(form.Encode()))
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		w := httptest.NewRecorder()
		gc, _ := gin.CreateTestContext(w)
		gc.Request = r
		return w, gc
	}

	// 未签名的 LogoutRequest 必须被拒，且不吊销任何会话。
	w, gc := post(base64.StdEncoding.EncodeToString([]byte(strings.ReplaceAll(
		fmt.Sprintf(`<samlp:LogoutRequest xmlns:samlp="urn:oasis:names:tc:SAML:2.0:protocol" xmlns:saml="urn:oasis:names:tc:SAML:2.0:assertion" ID="lr-x" Version="2.0" IssueInstant="%s" Destination="%s"><saml:Issuer>%s</saml:Issuer><saml:NameID>%s</saml:NameID></samlp:LogoutRequest>`,
			time.Now().UTC().Format(time.RFC3339), destination, entityID, nameID), "\n", ""))))
	svc.idpInitiatedLogout(gc, m, sp)
	if w.Code == http.StatusFound || len(iss.revoked) != 0 {
		t.Fatalf("unsigned LogoutRequest must be rejected (code=%d revoked=%v)", w.Code, iss.revoked)
	}

	// 签名正确的 LogoutRequest：吊销会话并回重定向到 IdP 的 SLO 端点（带 SAMLResponse）。
	signedPayload := signer.signedLogoutRequest(t, entityID, destination, nameID)
	w, gc = post(signedPayload)
	svc.idpInitiatedLogout(gc, m, sp)
	// gin 的测试上下文不会把仅写头的 302 刷到 Recorder，这里读 gin 自己记录的 status。
	if got := gc.Writer.Status(); got != http.StatusFound {
		t.Fatalf("want 302 LogoutResponse, got %d body=%s", got, w.Body.String())
	}
	loc := w.Header().Get("Location")
	if !strings.Contains(loc, "SAMLResponse=") {
		t.Fatalf("LogoutResponse missing: %s", loc)
	}
	if len(iss.revoked) != 1 || iss.revoked[0] != "acc-1/ref-acc-1" {
		t.Fatalf("session not revoked: %v", iss.revoked)
	}
}

func TestParseSAMLBinding(t *testing.T) {
	id, nameID, ok := parseSAMLBinding(samlBinding(idgen.ID(9), "urn:alice:example.com"))
	if !ok || id != 9 || nameID != "urn:alice:example.com" {
		t.Fatalf("binding round-trip failed: %d %q %v", id, nameID, ok)
	}
	for _, bad := range []string{"", "saml:", "saml:9", "saml:x:alice", "ldap:9:alice"} {
		if _, _, ok := parseSAMLBinding(bad); ok {
			t.Fatalf("%q must be rejected", bad)
		}
	}
}

func TestAppendSessionDedupesRotatedToken(t *testing.T) {
	c, err := cache.New(config.Cache{Driver: "memory"})
	if err != nil {
		t.Fatal(err)
	}
	cl := claimsFor("acc-1")
	appendSession(context.Background(), c, idgen.ID(9), "alice@example.com", cl)
	appendSession(context.Background(), c, idgen.ID(9), "alice@example.com", cl)
	raw, err := c.Get(context.Background(), sessionKey(idgen.ID(9), "alice@example.com"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(raw, `"j":"acc-1"`) != 1 {
		t.Fatalf("same jti must be stored once: %s", raw)
	}
}

func TestRoleGrantDiff(t *testing.T) {
	cases := []struct {
		name    string
		desired []string
		prev    []string
		add     []string
		remove  []string
	}{
		{"first login adds all", []string{"a", "b"}, nil, []string{"a", "b"}, nil},
		{"group switch swaps roles", []string{"b", "c"}, []string{"a", "b"}, []string{"c"}, []string{"a"}},
		{"empty assertion leaves roles alone", nil, []string{"a"}, nil, nil},
		{"no change", []string{"a"}, []string{"a"}, nil, nil},
	}
	for _, c := range cases {
		add, remove := roleGrantDiff(c.desired, c.prev)
		if !slices.Equal(add, c.add) || !slices.Equal(remove, c.remove) {
			t.Errorf("%s: add=%v remove=%v, want add=%v remove=%v", c.name, add, remove, c.add, c.remove)
		}
	}
}

func TestEnsureSigningKeyDescriptor(t *testing.T) {
	cert := &x509.Certificate{Raw: []byte("der-bytes")}
	md := &saml.EntityDescriptor{SPSSODescriptors: []saml.SPSSODescriptor{{}}}
	ensureSigningKeyDescriptor(md, cert)
	kds := md.SPSSODescriptors[0].KeyDescriptors
	if len(kds) != 1 || kds[0].Use != "signing" {
		t.Fatalf("want one signing KeyDescriptor, got %+v", kds)
	}
	if len(kds[0].KeyInfo.X509Data.X509Certificates) != 1 ||
		kds[0].KeyInfo.X509Data.X509Certificates[0].Data != base64.StdEncoding.EncodeToString(cert.Raw) {
		t.Fatalf("signing KeyDescriptor must carry the SP certificate: %+v", kds[0])
	}
	ensureSigningKeyDescriptor(md, cert)
	if len(md.SPSSODescriptors[0].KeyDescriptors) != 1 {
		t.Fatal("must be idempotent")
	}
	present := &saml.EntityDescriptor{SPSSODescriptors: []saml.SPSSODescriptor{{}}}
	present.SPSSODescriptors[0].KeyDescriptors = []saml.KeyDescriptor{{
		Use: "signing",
		KeyInfo: saml.KeyInfo{X509Data: saml.X509Data{
			X509Certificates: []saml.X509Certificate{{Data: base64.StdEncoding.EncodeToString(cert.Raw)}},
		}},
	}}
	ensureSigningKeyDescriptor(present, cert)
	if len(present.SPSSODescriptors[0].KeyDescriptors) != 1 {
		t.Fatal("same certificate must not be duplicated")
	}
	// 轮换期间元数据允许同时列出多张 signing 证书（当前 / 待切换 / 上一代）。
	another := &x509.Certificate{Raw: []byte("der-bytes-next")}
	ensureSigningKeyDescriptor(present, another)
	if len(present.SPSSODescriptors[0].KeyDescriptors) != 2 {
		t.Fatal("a different rollover certificate must be added")
	}
	ensureSigningKeyDescriptor(nil, cert)
	ensureSigningKeyDescriptor(&saml.EntityDescriptor{}, cert)
}

func TestWantsHTML(t *testing.T) {
	cases := map[string]bool{
		"text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8": true,
		"application/xhtml+xml": true,
		"application/json":      false,
		"*/*":                   false,
		"":                      false,
	}
	for accept, want := range cases {
		r := httptest.NewRequest(http.MethodPost, "/acs", nil)
		if accept != "" {
			r.Header.Set("Accept", accept)
		}
		if got := wantsHTML(r); got != want {
			t.Errorf("Accept %q: got %v want %v", accept, got, want)
		}
	}
}

func TestRenderACSErrorPageEscapes(t *testing.T) {
	page := renderACSErrorPage(acsErrorView{
		Lang:        "zh-CN",
		Title:       `<script>alert(1)</script>`,
		ReasonLabel: "原因",
		Reason:      `bad <b>reason</b>`,
		Hint:        "hint",
		BackLabel:   "返回登录",
		BackURL:     `/admin/login?x="y"`,
	})
	if strings.Contains(page, "<script>alert(1)</script>") || strings.Contains(page, "<b>reason</b>") {
		t.Fatalf("dynamic values must be escaped: %s", page)
	}
	for _, want := range []string{"&lt;script&gt;alert(1)&lt;/script&gt;", "bad &lt;b&gt;reason&lt;/b&gt;", "返回登录", `href="/admin/login?x=&#34;y&#34;"`} {
		if !strings.Contains(page, want) {
			t.Fatalf("page must contain %q: %s", want, page)
		}
	}
}

func TestHasEncryptedAssertion(t *testing.T) {
	plain := []byte(`<samlp:Response xmlns:samlp="urn:oasis:names:tc:SAML:2.0:protocol" xmlns:saml="urn:oasis:names:tc:SAML:2.0:assertion"><saml:Assertion/></samlp:Response>`)
	if hasEncryptedAssertion(plain) {
		t.Fatal("plain assertion must not count as encrypted")
	}
	enc := []byte(`<samlp:Response xmlns:samlp="urn:oasis:names:tc:SAML:2.0:protocol" xmlns:saml="urn:oasis:names:tc:SAML:2.0:assertion"><saml:EncryptedAssertion/></samlp:Response>`)
	if !hasEncryptedAssertion(enc) {
		t.Fatal("EncryptedAssertion in assertion namespace must be detected")
	}
	other := []byte(`<x:EncryptedAssertion xmlns:x="urn:example"/>`)
	if hasEncryptedAssertion(other) {
		t.Fatal("foreign namespace must not count")
	}
}

func TestRolloverPublishesAndDecryptsAllKeys(t *testing.T) {
	curCert, curKey, err := newSPKeyPair()
	if err != nil {
		t.Fatal(err)
	}
	nextCert, nextKey, err := newSPKeyPair()
	if err != nil {
		t.Fatal(err)
	}
	prevCert, prevKey, err := newSPKeyPair()
	if err != nil {
		t.Fatal(err)
	}
	until := time.Now().Add(spPrevGrace)
	m := &Source{
		Name: "idp", Enabled: true, IDPEntityID: "https://idp.example.com",
		IdPMetadata: metadataXML(certB64(t)),
		SPCert:      curCert, SPKeyEnc: curKey,
		NextSPCert: nextCert, NextSPKeyEnc: nextKey,
		PrevSPCert: prevCert, PrevSPKeyEnc: prevKey, PrevSPExpiresAt: &until,
	}
	m.ID = idgen.ID(42)
	svc := NewService(nil, nil, nil)
	sp, err := svc.provider(m, "https://sp.example.com")
	if err != nil {
		t.Fatal(err)
	}
	if len(sp.DecryptionKeys) != 2 {
		t.Fatalf("want next+prev decryption keys, got %d", len(sp.DecryptionKeys))
	}
	certs := publishedSPCerts(m)
	if len(certs) != 3 {
		t.Fatalf("want 3 published certs during rollover, got %d", len(certs))
	}
	md := sp.Metadata()
	for _, c := range certs {
		ensureSigningKeyDescriptor(md, c)
	}
	if got := len(md.SPSSODescriptors[0].KeyDescriptors); got < 3 {
		t.Fatalf("metadata should list all rollover certs, got %d descriptors", got)
	}

	// 上一代过期后不再出现在元数据，也不再参与解密。
	past := time.Now().Add(-time.Hour)
	m.PrevSPExpiresAt = &past
	if got := len(publishedSPCerts(m)); got != 2 {
		t.Fatalf("expired prev must not be published, got %d", got)
	}
	sp, err = svc.provider(m, "https://sp.example.com")
	if err != nil {
		t.Fatal(err)
	}
	if len(sp.DecryptionKeys) != 1 {
		t.Fatalf("expired prev must not decrypt, got %d keys", len(sp.DecryptionKeys))
	}
}
