package saml

import (
	"context"
	"encoding/base64"
	"encoding/pem"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/crewjam/saml"
	"github.com/russellhaering/goxmldsig"

	"nexadmin/core/cache"
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
