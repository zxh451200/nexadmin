// 开发用 SAML IdP：给本地演示/联调提供真实的企业 IdP 行为（登录页、元数据、签名断言）。
// 仅用于开发环境，不随发行包分发（位于 nexadmin-vendor/，new/release 均排除）。
package main

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/xml"
	"flag"
	"fmt"
	"html/template"
	"log"
	"math/big"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/crewjam/saml"
)

type spCache struct {
	mu sync.Mutex
	m  map[string]*saml.EntityDescriptor
}

func (c *spCache) GetServiceProvider(_ *http.Request, entityID string) (*saml.EntityDescriptor, error) {
	c.mu.Lock()
	if v, ok := c.m[entityID]; ok {
		c.mu.Unlock()
		return v, nil
	}
	c.mu.Unlock()
	resp, err := http.Get(entityID)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("sp metadata: %s", resp.Status)
	}
	var meta saml.EntityDescriptor
	if err := xml.NewDecoder(resp.Body).Decode(&meta); err != nil {
		return nil, err
	}
	c.mu.Lock()
	c.m[entityID] = &meta
	c.mu.Unlock()
	return &meta, nil
}

type sessions struct{ user string }

func (s sessions) GetSession(w http.ResponseWriter, r *http.Request, _ *saml.IdpAuthnRequest) *saml.Session {
	name := s.user
	if c, err := r.Cookie("idp_user"); err == nil && c.Value != "" {
		name = c.Value
	} else {
		http.Redirect(w, r, "/login?return="+url.QueryEscape(r.URL.String()), http.StatusFound)
		return nil
	}
	now := time.Now()
	return &saml.Session{
		ID: "sid-" + name, CreateTime: now, ExpireTime: now.Add(time.Hour), Index: "idx-" + name,
		NameID: name + "@example.com", NameIDFormat: string(saml.EmailAddressNameIDFormat),
		UserName: name, UserEmail: name + "@example.com", UserCommonName: strings.ToUpper(name[:1]) + name[1:],
		CustomAttributes: []saml.Attribute{
			{Name: "uid", Values: []saml.AttributeValue{{Value: name}}},
			{Name: "mail", Values: []saml.AttributeValue{{Value: name + "@example.com"}}},
			{Name: "displayName", Values: []saml.AttributeValue{{Value: strings.ToUpper(name[:1]) + name[1:]}}},
		},
	}
}

var loginTmpl = template.Must(template.New("login").Parse(`<!doctype html><html><head><meta charset="utf-8"><title>Dev IdP 登录</title></head>
<body style="font-family:system-ui;max-width:420px;margin:80px auto">
<h2>开发用 SAML IdP</h2>
<p>这是本地演示 IdP，不是你的企业目录。账号：<code>alice</code>，密码任意。</p>
<form method="post" action="/login">
  <input type="hidden" name="return" value="{{.Return}}">
  <p><label>用户名 <input name="username" value="alice" required></label></p>
  <p><label>密码 <input name="password" type="password" value="demo" required></label></p>
  <button type="submit">登录并继续</button>
</form></body></html>`))

func main() {
	addr := flag.String("addr", "127.0.0.1:9090", "监听地址")
	base := flag.String("base", "http://127.0.0.1:9090", "对外基址（写进元数据）")
	user := flag.String("user", "alice", "默认登录用户名")
	flag.Parse()

	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		log.Fatal(err)
	}
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(time.Now().UnixNano()),
		Subject:      pkix.Name{CommonName: "nexadmin-dev-idp"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().AddDate(5, 0, 0),
		KeyUsage:     x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		log.Fatal(err)
	}
	cert, err := x509.ParseCertificate(der)
	if err != nil {
		log.Fatal(err)
	}

	baseURL, err := url.Parse(*base)
	if err != nil {
		log.Fatal(err)
	}
	metaURL, ssoURL := *baseURL, *baseURL
	metaURL.Path, ssoURL.Path = "/metadata", "/sso"
	idp := &saml.IdentityProvider{
		Key: key, Certificate: cert,
		MetadataURL:             metaURL,
		SSOURL:                  ssoURL,
		ServiceProviderProvider: &spCache{m: map[string]*saml.EntityDescriptor{}},
		SessionProvider:         sessions{user: *user},
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/metadata", idp.ServeMetadata)
	mux.HandleFunc("/sso", idp.ServeSSO)
	mux.HandleFunc("/login", func(w http.ResponseWriter, r *http.Request) {
		ret := r.FormValue("return")
		if r.Method == http.MethodPost {
			name := strings.TrimSpace(r.FormValue("username"))
			if name == "" {
				http.Error(w, "username required", http.StatusBadRequest)
				return
			}
			http.SetCookie(w, &http.Cookie{Name: "idp_user", Value: name, Path: "/", HttpOnly: true, SameSite: http.SameSiteLaxMode})
			if ret == "" {
				ret = "/"
			}
			http.Redirect(w, r, ret, http.StatusFound)
			return
		}
		_ = loginTmpl.Execute(w, struct{ Return string }{Return: ret})
	})
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) { fmt.Fprint(w, "ok") })

	log.Printf("dev SAML IdP on %s (metadata %s/metadata, SSO %s/sso)", *addr, *base, *base)
	log.Fatal(http.ListenAndServe(*addr, mux))
}
