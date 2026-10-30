package saml

// 真实协议端到端：进程内起一个真实 SAML IdP（crewjam IdentityProvider），
// 走 SP-initiated 登录 → 签名断言 → ACS → LoginAs。不依赖企业 IdP。

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/xml"
	"html"
	"io"
	"math/big"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/crewjam/saml"
	"github.com/gin-gonic/gin"
	gormmysql "gorm.io/driver/mysql"
	"gorm.io/gorm"

	"nexadmin/core/auth"
	"nexadmin/core/cache"
	"nexadmin/core/config"
)

type fakeIssuer struct {
	got   auth.LoginAsRequest
	calls int
}

func (f *fakeIssuer) LoginAs(_ context.Context, req auth.LoginAsRequest) (*auth.LoginOutcome, error) {
	f.got = req
	f.calls++
	return &auth.LoginOutcome{
		Pair: auth.Pair{AccessToken: "access-token-e2e", RefreshToken: "refresh-token-e2e"},
		User: &auth.User{ID: 1, Username: req.Identity.Username},
	}, nil
}

func idpKeyPair(t *testing.T) (*rsa.PrivateKey, *x509.Certificate) {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(2),
		Subject:      pkix.Name{CommonName: "test-idp"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(24 * time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	cert, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatal(err)
	}
	return key, cert
}

var formValueRe = regexp.MustCompile(`name="(SAMLResponse|RelayState)" value="([^"]*)"`)

func formValues(t *testing.T, body string) map[string]string {
	t.Helper()
	out := map[string]string{}
	for _, m := range formValueRe.FindAllStringSubmatch(body, -1) {
		out[m[1]] = html.UnescapeString(m[2])
	}
	return out
}

func TestEndToEndSPInitiatedLogin(t *testing.T) {
	gin.SetMode(gin.TestMode)

	// 1) 真实 IdP 进程内服务
	idpKey, idpCert := idpKeyPair(t)
	idp := &saml.IdentityProvider{Key: idpKey, Certificate: idpCert}
	// Handler 注册路由时就要有路径；host 在服务启动后再补。
	idp.MetadataURL = url.URL{Path: "/metadata"}
	idp.SSOURL = url.URL{Path: "/sso"}
	idpServer := httptest.NewServer(idp.Handler())
	defer idpServer.Close()
	idp.MetadataURL = mustURL(t, idpServer.URL+"/metadata")
	idp.SSOURL = mustURL(t, idpServer.URL+"/sso")
	idpMetaXML, err := xml.Marshal(idp.Metadata())
	if err != nil {
		t.Fatal(err)
	}

	// 2) SP：源含 IdP 元数据与自动生成的 SP 密钥
	certPEM, keyEnc, err := newSPKeyPair()
	if err != nil {
		t.Fatal(err)
	}
	src := Source{Name: "corp", Enabled: true, IdPMetadata: string(idpMetaXML), SPCert: certPEM, SPKeyEnc: keyEnc,
		AttrUsername: "uid", AttrEmail: "mail", AttrNickname: "displayName", AutoCreate: true}
	src.ID = 123

	// 3) 数据库：ACS 读取源（测试库未启用租户插件，按 id 精确匹配）
	sqlDB, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer sqlDB.Close()
	db, err := gorm.Open(gormmysql.New(gormmysql.Config{Conn: sqlDB, SkipInitializeWithVersion: true}), &gorm.Config{DisableAutomaticPing: true})
	if err != nil {
		t.Fatal(err)
	}
	cols := []string{"id", "tenant_id", "name", "enabled", "idp_metadata", "sp_cert", "sp_key_enc",
		"attr_username", "attr_email", "attr_nickname", "auto_create", "match_existing", "default_role", "created_at", "updated_at"}
	sourceRow := func() *sqlmock.Rows {
		return sqlmock.NewRows(cols).AddRow(123, 1, src.Name, true, src.IdPMetadata, src.SPCert, src.SPKeyEnc,
			src.AttrUsername, src.AttrEmail, src.AttrNickname, true, false, "", time.Now(), time.Now())
	}
	// metadata / login / acs 各读一次源
	for i := 0; i < 3; i++ {
		mock.ExpectQuery("SELECT \\* FROM `plg_saml_source`").WithArgs("123", 1).WillReturnRows(sourceRow())
	}

	issuer := &fakeIssuer{}
	ch, err := cache.New(config.Cache{Driver: "memory"})
	if err != nil {
		t.Fatal(err)
	}
	defer ch.Close()
	svc := NewService(db, issuer, ch)
	router := gin.New()
	router.GET("/api/v1/auth/saml/:id/metadata", svc.metadata)
	router.GET("/api/v1/auth/saml/:id/login", svc.login)
	router.POST("/api/v1/auth/saml/:id/acs", svc.acs)
	spServer := httptest.NewServer(router)
	defer spServer.Close()

	// 4) IdP 需要知道 SP 元数据：按真实流程从 SP metadata 端点获取
	metaResp, err := http.Get(spServer.URL + "/api/v1/auth/saml/123/metadata")
	if err != nil {
		t.Fatal(err)
	}
	metaRaw, _ := io.ReadAll(metaResp.Body)
	_ = metaResp.Body.Close()
	if metaResp.StatusCode != 200 {
		t.Fatalf("sp metadata: %d %s", metaResp.StatusCode, metaRaw)
	}
	var spMeta saml.EntityDescriptor
	if err := xml.Unmarshal(metaRaw, &spMeta); err != nil {
		t.Fatalf("sp metadata parse: %v\n%s", err, metaRaw)
	}
	idp.ServiceProviderProvider = staticSP{meta: &spMeta}
	idp.SessionProvider = staticSession{session: &saml.Session{
		ID: "sid-1", CreateTime: time.Now(), ExpireTime: time.Now().Add(time.Hour), Index: "idx-1",
		NameID: "alice", NameIDFormat: string(saml.EmailAddressNameIDFormat),
		UserName: "alice", UserEmail: "alice@example.com", UserCommonName: "Alice",
		CustomAttributes: []saml.Attribute{
			{Name: "uid", Values: []saml.AttributeValue{{Value: "alice"}}},
			{Name: "mail", Values: []saml.AttributeValue{{Value: "alice@example.com"}}},
			{Name: "displayName", Values: []saml.AttributeValue{{Value: "Alice"}}},
		},
	}}

	// 5) SP-initiated：拿 302 到 IdP，再用浏览器语义请求 IdP，得到自动提交表单
	client := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	loginResp, err := client.Get(spServer.URL + "/api/v1/auth/saml/123/login")
	if err != nil {
		t.Fatal(err)
	}
	loginResp.Body.Close()
	if loginResp.StatusCode != http.StatusFound {
		t.Fatalf("login: %d", loginResp.StatusCode)
	}
	idpURL := loginResp.Header.Get("Location")
	if !strings.HasPrefix(idpURL, idpServer.URL) {
		t.Fatalf("redirect target: %s", idpURL)
	}
	idpResp, err := http.Get(idpURL)
	if err != nil {
		t.Fatal(err)
	}
	page, _ := io.ReadAll(idpResp.Body)
	_ = idpResp.Body.Close()
	if idpResp.StatusCode != 200 {
		t.Fatalf("idp sso: %d %s", idpResp.StatusCode, page)
	}
	form := formValues(t, string(page))
	if form["SAMLResponse"] == "" {
		t.Fatalf("idp page has no SAMLResponse:\n%s", page)
	}

	// 6) 提交到 ACS，应完成 LoginAs
	acsResp, err := http.PostForm(spServer.URL+"/api/v1/auth/saml/123/acs", url.Values{
		"SAMLResponse": {form["SAMLResponse"]}, "RelayState": {form["RelayState"]},
	})
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(acsResp.Body)
	_ = acsResp.Body.Close()
	if acsResp.StatusCode != 200 {
		t.Fatalf("acs: %d %s", acsResp.StatusCode, body)
	}
	if issuer.calls != 1 {
		t.Fatalf("LoginAs calls = %d", issuer.calls)
	}
	if issuer.got.Method != "saml" || issuer.got.Identity.Username != "alice" ||
		issuer.got.Identity.Email != "alice@example.com" || issuer.got.Identity.Nickname != "Alice" ||
		issuer.got.Identity.Subject != "123:alice" || issuer.got.Identity.ProviderKey != "" || !issuer.got.Identity.AutoCreate {
		t.Fatalf("LoginAs request: %+v", issuer.got)
	}
	if !strings.Contains(string(body), "accessToken") {
		t.Fatalf("acs body: %s", body)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func mustURL(t *testing.T, raw string) url.URL {
	t.Helper()
	u, err := url.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	return *u
}

type staticSP struct{ meta *saml.EntityDescriptor }

func (s staticSP) GetServiceProvider(*http.Request, string) (*saml.EntityDescriptor, error) {
	return s.meta, nil
}

type staticSession struct{ session *saml.Session }

func (s staticSession) GetSession(http.ResponseWriter, *http.Request, *saml.IdpAuthnRequest) *saml.Session {
	return s.session
}
