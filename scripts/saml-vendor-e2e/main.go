// saml-vendor-e2e 模拟“厂商租户”对接：按 Okta / Entra / Keycloak 的常见形态
// 生成 IdP 元数据与签名断言，直接打到真实 SP 的 ACS，验证登录、角色同步与 SLO。
//
// 它不替代真厂商租户，只把“协议形态差异”做成可重复的回归：
//
//	okta     属性 uid/mail/groups（friendly），NameID=emailAddress，只签断言
//	entra    属性用 schemas.* claim URI，NameID=persistent，响应+断言都签
//	keycloak 属性 uid/mail/groups，NameID=persistent，响应+断言都签
//
// 用法（先把一次性实例按 docs/功能与权限清单.md 2.3 起好）：
//
//	go run ./scripts/saml-vendor-e2e -base http://127.0.0.1:18099 \
//	  -admin-user admin -admin-pass Admin123456 -role ops-role
package main

import (
	"bytes"
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"flag"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/beevik/etree"
	"github.com/russellhaering/goxmldsig"
)

type profile struct {
	key             string
	name            string
	attrUsername    string
	attrEmail       string
	attrNickname    string
	attrGroups      string
	nameIDFormat    string
	signResponse    bool
	groupValue      string
	attributeValues map[string]string
}

var profiles = []profile{
	{
		key: "okta", name: "Okta",
		attrUsername: "uid", attrEmail: "mail", attrNickname: "displayName", attrGroups: "groups",
		nameIDFormat: "urn:oasis:names:tc:SAML:2.0:nameid-format:emailAddress",
		signResponse: false, groupValue: "ops",
	},
	{
		key: "entra", name: "Microsoft Entra",
		attrUsername: "http://schemas.xmlsoap.org/ws/2005/05/identity/claims/name",
		attrEmail:    "http://schemas.xmlsoap.org/ws/2005/05/identity/claims/emailaddress",
		attrNickname: "http://schemas.microsoft.com/identity/claims/displayname",
		attrGroups:   "http://schemas.microsoft.com/ws/2008/06/identity/claims/groups",
		nameIDFormat: "urn:oasis:names:tc:SAML:2.0:nameid-format:persistent",
		signResponse: true, groupValue: "ops",
	},
	{
		key: "keycloak", name: "Keycloak",
		attrUsername: "uid", attrEmail: "mail", attrNickname: "displayName", attrGroups: "groups",
		nameIDFormat: "urn:oasis:names:tc:SAML:2.0:nameid-format:persistent",
		signResponse: true, groupValue: "ops",
	},
}

var (
	base      string
	adminUser string
	adminPass string
	roleCode  string
	httpc     = &http.Client{Timeout: 20 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	adminTok  string
)

func main() {
	flag.StringVar(&base, "base", "http://127.0.0.1:18099", "SP base URL")
	flag.StringVar(&adminUser, "admin-user", "admin", "admin username")
	flag.StringVar(&adminPass, "admin-pass", "", "admin password")
	flag.StringVar(&roleCode, "role", "ops-role", "role code granted by the group mapping")
	flag.Parse()
	if adminPass == "" {
		fmt.Println("需要 -admin-pass")
		os.Exit(2)
	}
	adminTok = login()
	fmt.Println("PASS admin login")

	failed := 0
	for _, p := range profiles {
		if err := runProfile(p); err != nil {
			failed++
			fmt.Printf("FAIL [%s] %v\n", p.key, err)
			continue
		}
		fmt.Printf("PASS [%s] login + role sync + SLO\n", p.key)
	}
	if failed > 0 {
		fmt.Printf("%d profile(s) failed\n", failed)
		os.Exit(1)
	}
	fmt.Println("ALL VENDOR PROFILES PASS")
}

func login() string {
	st, raw := req(http.MethodPost, base+"/api/v1/auth/login", "", map[string]string{"username": adminUser, "password": adminPass})
	if st != 200 {
		panic(fmt.Sprintf("admin login %d %s", st, raw))
	}
	return asMap(raw)["accessToken"].(string)
}

func runProfile(p profile) error {
	idp, err := newIdP(p)
	if err != nil {
		return err
	}
	// 每个 profile / 每次运行用独立用户名，避免同名账号被自动建号拒绝
	idp.user = fmt.Sprintf("alice-%s-%d", p.key, time.Now().UnixNano()%1_000_000)
	src, err := createSource(p, idp.metadata())
	if err != nil {
		return err
	}
	xmlText, err := idp.response(src.acs, src.entity, "sess-"+p.key, p.groupValue)
	if err != nil {
		return err
	}
	code, body, hdr := postACS(src.acs, xmlText)
	if code != 302 {
		return fmt.Errorf("ACS %d: %.300s", code, body)
	}
	tok, err := exchange(hdr.Get("Location"))
	if err != nil {
		return err
	}
	roles := meRoles(tok)
	if !contains(roles, roleCode) {
		return fmt.Errorf("roles=%v, want %s", roles, roleCode)
	}
	if code := postSLO(src.slo, idp, "sess-"+p.key); code != 302 && code != 200 {
		return fmt.Errorf("SLO status %d", code)
	}
	if st, _ := req(http.MethodGet, base+"/api/v1/auth/me", tok, nil); st == 200 {
		return fmt.Errorf("token still valid after SLO")
	}
	return nil
}

type idp struct {
	prof     profile
	user     string
	certPEM  string
	keyPEM   string
	cert     *x509.Certificate
	signer   *rsa.PrivateKey
	entityID string
}

func newIdP(p profile) (*idp, error) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return nil, err
	}
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(time.Now().UnixNano()),
		Subject:      pkix.Name{CommonName: "vendor-" + p.key},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(30 * 24 * time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		return nil, err
	}
	cert, _ := x509.ParseCertificate(der)
	pk, _ := x509.MarshalPKCS8PrivateKey(key)
	return &idp{
		prof:     p,
		certPEM:  string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})),
		keyPEM:   string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: pk})),
		cert:     cert,
		signer:   key,
		entityID: "https://" + p.key + "-tenant.example.com/metadata",
	}, nil
}

func (i *idp) metadata() string {
	der, _ := pem.Decode([]byte(i.certPEM))
	return `<EntityDescriptor xmlns="urn:oasis:names:tc:SAML:2.0:metadata" entityID="` + i.entityID + `">
  <IDPSSODescriptor protocolSupportEnumeration="urn:oasis:names:tc:SAML:2.0:protocol">
    <KeyDescriptor use="signing"><ds:KeyInfo xmlns:ds="http://www.w3.org/2000/09/xmldsig#"><ds:X509Data><ds:X509Certificate>` +
		base64.StdEncoding.EncodeToString(der.Bytes) +
		`</ds:X509Certificate></ds:X509Data></ds:KeyInfo></KeyDescriptor>
    <SingleSignOnService Binding="urn:oasis:names:tc:SAML:2.0:bindings:HTTP-Redirect" Location="https://` + i.prof.key + `-tenant.example.com/sso"/>
    <SingleLogoutService Binding="urn:oasis:names:tc:SAML:2.0:bindings:HTTP-Redirect" Location="https://` + i.prof.key + `-tenant.example.com/slo"/>
  </IDPSSODescriptor>
</EntityDescriptor>`
}

func (i *idp) assertion(acs, aud, sid, group string) string {
	attr := func(name, value string) string {
		return `<saml:Attribute Name="` + name + `"><saml:AttributeValue>` + value + `</saml:AttributeValue></saml:Attribute>`
	}
	xmlText := `<saml:Assertion xmlns:saml="urn:oasis:names:tc:SAML:2.0:assertion" ID="_a` + sid + `" Version="2.0" IssueInstant="` + now() + `">
    <saml:Issuer>` + i.entityID + `</saml:Issuer>
    <saml:Subject>
      <saml:NameID Format="` + i.prof.nameIDFormat + `">` + i.user + `@example.com</saml:NameID>
      <saml:SubjectConfirmation Method="urn:oasis:names:tc:SAML:2.0:cm:bearer">
        <saml:SubjectConfirmationData NotOnOrAfter="` + later(5) + `" Recipient="` + acs + `"/>
      </saml:SubjectConfirmation>
    </saml:Subject>
    <saml:Conditions NotBefore="` + later(-1) + `" NotOnOrAfter="` + later(5) + `">
      <saml:AudienceRestriction><saml:Audience>` + aud + `</saml:Audience></saml:AudienceRestriction>
    </saml:Conditions>
    <saml:AuthnStatement AuthnInstant="` + now() + `" SessionIndex="` + sid + `">
      <saml:AuthnContext><saml:AuthnContextClassRef>urn:oasis:names:tc:SAML:2.0:ac:classes:PasswordProtectedTransport</saml:AuthnContextClassRef></saml:AuthnContext>
    </saml:AuthnStatement>
    <saml:AttributeStatement>
      ` + attr(i.prof.attrUsername, i.user) + `
      ` + attr(i.prof.attrEmail, i.user+"@example.com") + `
      ` + attr(i.prof.attrNickname, "Alice") + `
      ` + attr(i.prof.attrGroups, group) + `
    </saml:AttributeStatement>
  </saml:Assertion>`
	return i.sign(xmlText)
}

func (i *idp) response(acs, aud, sid, group string) (string, error) {
	a := i.assertion(acs, aud, sid, group)
	xmlText := `<samlp:Response xmlns:samlp="urn:oasis:names:tc:SAML:2.0:protocol" xmlns:saml="urn:oasis:names:tc:SAML:2.0:assertion" ID="_r` + sid + `" Version="2.0" IssueInstant="` + now() + `" Destination="` + acs + `">
  <saml:Issuer>` + i.entityID + `</saml:Issuer>
  <samlp:Status><samlp:StatusCode Value="urn:oasis:names:tc:SAML:2.0:status:Success"/></samlp:Status>
  ` + a + `
</samlp:Response>`
	if i.prof.signResponse {
		xmlText = i.sign(xmlText)
	}
	return xmlText, nil
}

func (i *idp) logout(slo, sid string) string {
	si := ""
	if sid != "" {
		si = `<samlp:SessionIndex>` + sid + `</samlp:SessionIndex>`
	}
	return i.sign(`<samlp:LogoutRequest xmlns:samlp="urn:oasis:names:tc:SAML:2.0:protocol" xmlns:saml="urn:oasis:names:tc:SAML:2.0:assertion" ID="_lr` + sid + `" Version="2.0" IssueInstant="` + now() + `" Destination="` + slo + `">
  <saml:Issuer>` + i.entityID + `</saml:Issuer>
  <saml:NameID Format="` + i.prof.nameIDFormat + `">` + i.user + `@example.com</saml:NameID>
  ` + si + `
</samlp:LogoutRequest>`)
}

func (i *idp) sign(xmlText string) string {
	doc := etree.NewDocument()
	if err := doc.ReadFromString(xmlText); err != nil {
		panic(err)
	}
	sc := dsig.NewDefaultSigningContext(dsig.TLSCertKeyStore(tls.Certificate{
		Certificate: [][]byte{i.cert.Raw}, PrivateKey: i.signer,
	}))
	sc.Canonicalizer = dsig.MakeC14N10ExclusiveCanonicalizerWithPrefixList("")
	signed, err := sc.SignEnveloped(doc.Root())
	if err != nil {
		panic(err)
	}
	out := etree.NewDocument()
	out.SetRoot(signed)
	s, err := out.WriteToString()
	if err != nil {
		panic(err)
	}
	return s
}

type source struct {
	id, entity, acs, slo string
}

func createSource(p profile, meta string) (source, error) {
	body := map[string]any{
		"name": "vendor-" + p.key, "enabled": true, "idpMetadata": meta,
		"attrUsername": p.attrUsername, "attrEmail": p.attrEmail,
		"attrNickname": p.attrNickname, "attrGroups": p.attrGroups,
		"autoCreate": true,
		"groupRoleMappings": []map[string]any{
			{"externalGroup": p.groupValue, "roleCodes": []string{roleCode}},
		},
		"requireMappedGroup": true, "syncRolesOnLogin": true, "allowIdpInitiated": true,
	}
	st, raw := req(http.MethodPost, base+"/api/v1/plugins/saml/sources", adminTok, body)
	if st != 200 {
		return source{}, fmt.Errorf("create source %d: %s", st, raw)
	}
	id := fmt.Sprint(asMap(raw)["id"])
	return source{
		id:     id,
		entity: base + "/api/v1/auth/saml/" + id + "/metadata",
		acs:    base + "/api/v1/auth/saml/" + id + "/acs",
		slo:    base + "/api/v1/auth/saml/" + id + "/slo",
	}, nil
}

func postACS(acs, xmlText string) (int, string, http.Header) {
	form := url.Values{}
	form.Set("SAMLResponse", base64.StdEncoding.EncodeToString([]byte(xmlText)))
	form.Set("RelayState", "/")
	r, _ := http.NewRequest(http.MethodPost, acs, strings.NewReader(form.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := httpc.Do(r)
	if err != nil {
		return 0, err.Error(), nil
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, string(b), resp.Header
}

func postSLO(slo string, i *idp, sid string) int {
	form := url.Values{}
	form.Set("SAMLRequest", base64.StdEncoding.EncodeToString([]byte(i.logout(slo, sid))))
	resp, err := httpc.PostForm(slo, form)
	if err != nil {
		return 0
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, resp.Body)
	return resp.StatusCode
}

func exchange(loc string) (string, error) {
	u, err := url.Parse(loc)
	if err != nil {
		return "", err
	}
	code := u.Query().Get("samlCode")
	if code == "" {
		return "", fmt.Errorf("no samlCode in %s", loc)
	}
	st, raw := req(http.MethodPost, base+"/api/v1/auth/saml/exchange", "", map[string]string{"code": code})
	if st != 200 {
		return "", fmt.Errorf("exchange %d: %s", st, raw)
	}
	return asMap(raw)["accessToken"].(string), nil
}

func meRoles(tok string) []string {
	st, raw := req(http.MethodGet, base+"/api/v1/auth/me", tok, nil)
	if st != 200 {
		return nil
	}
	var out []string
	if rs, ok := asMap(raw)["roles"].([]any); ok {
		for _, r := range rs {
			out = append(out, fmt.Sprint(r))
		}
	}
	return out
}

func req(method, rawurl, token string, body any) (int, []byte) {
	var rd io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rd = bytes.NewReader(b)
	}
	r, _ := http.NewRequest(method, rawurl, rd)
	if body != nil {
		r.Header.Set("Content-Type", "application/json")
	}
	if token != "" {
		r.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := httpc.Do(r)
	if err != nil {
		return 0, []byte(err.Error())
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, raw
}

func asMap(raw []byte) map[string]any {
	var m map[string]any
	_ = json.Unmarshal(raw, &m)
	if d, ok := m["data"].(map[string]any); ok {
		return d
	}
	return m
}

func now() string { return time.Now().UTC().Format("2006-01-02T15:04:05Z") }
func later(m int) string {
	return time.Now().UTC().Add(time.Duration(m) * time.Minute).Format("2006-01-02T15:04:05Z")
}
func contains(list []string, v string) bool {
	for _, x := range list {
		if x == v {
			return true
		}
	}
	return false
}
