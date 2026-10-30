// 开发用 SAML IdP：给本地演示/联调提供真实的企业 IdP 行为（登录页、元数据、签名断言）。
// 仅用于开发环境，不随发行包分发（位于 nexadmin-vendor/，new/release 均排除）。
package main

import (
	"bytes"
	"compress/flate"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/hex"
	"encoding/xml"
	"flag"
	"fmt"
	"html/template"
	"io"
	"log"
	"math/big"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/beevik/etree"
	"github.com/crewjam/saml"
	dsig "github.com/russellhaering/goxmldsig"
)

var (
	idpKey   *rsa.PrivateKey
	idpCerts [][]byte
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
	sloURL := *baseURL
	sloURL.Path = "/slo"
	idpKey, idpCerts = key, [][]byte{der}
	sps := &spCache{m: map[string]*saml.EntityDescriptor{}}
	idp := &saml.IdentityProvider{
		Key: key, Certificate: cert,
		MetadataURL:             metaURL,
		SSOURL:                  ssoURL,
		LogoutURL:               sloURL,
		ServiceProviderProvider: sps,
		SessionProvider:         sessions{user: *user},
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/metadata", idp.ServeMetadata)
	mux.HandleFunc("/sso", idp.ServeSSO)
	mux.HandleFunc("/slo", handleSLO(sps, metaURL.String()))
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

// handleSLO 处理 SP 的 LogoutRequest，返回签名的 LogoutResponse（HTTP-Redirect）。
func handleSLO(sps *spCache, idpEntityID string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		raw, err := base64.StdEncoding.DecodeString(r.FormValue("SAMLRequest"))
		if err != nil {
			http.Error(w, "bad SAMLRequest", http.StatusBadRequest)
			return
		}
		if r.Method == http.MethodGet {
			if b, ierr := inflate(raw); ierr == nil {
				raw = b
			}
		}
		var req saml.LogoutRequest
		if err := xml.Unmarshal(raw, &req); err != nil || req.Issuer == nil {
			http.Error(w, "bad LogoutRequest", http.StatusBadRequest)
			return
		}
		meta, err := sps.GetServiceProvider(r, req.Issuer.Value)
		if err != nil || len(meta.SPSSODescriptors) == 0 || len(meta.SPSSODescriptors[0].SingleLogoutServices) == 0 {
			http.Error(w, "unknown SP", http.StatusBadRequest)
			return
		}
		dest := meta.SPSSODescriptors[0].SingleLogoutServices[0].Location
		resp := saml.LogoutResponse{
			ID: newID(), InResponseTo: req.ID, Version: "2.0", IssueInstant: time.Now().UTC(),
			Destination: dest, Issuer: &saml.Issuer{Value: idpEntityID},
			Status: saml.Status{StatusCode: saml.StatusCode{Value: saml.StatusSuccess}},
		}
		ctx, err := dsig.NewSigningContext(idpKey, idpCerts)
		if err != nil {
			http.Error(w, "sign", http.StatusInternalServerError)
			return
		}
		signed, err := ctx.SignEnveloped(resp.Element())
		if err != nil {
			http.Error(w, "sign", http.StatusInternalServerError)
			return
		}
		var buf bytes.Buffer
		doc := etree.NewDocument()
		doc.SetRoot(signed)
		if _, err := doc.WriteTo(&buf); err != nil {
			http.Error(w, "xml", http.StatusInternalServerError)
			return
		}
		packed, _ := deflate(buf.Bytes())
		target := dest + "?SAMLResponse=" + url.QueryEscape(base64.StdEncoding.EncodeToString(packed))
		if rs := r.FormValue("RelayState"); rs != "" {
			target += "&RelayState=" + url.QueryEscape(rs)
		}
		http.Redirect(w, r, target, http.StatusFound)
	}
}

func newID() string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	return hex.EncodeToString(b[:])
}

func deflate(raw []byte) ([]byte, error) {
	var buf bytes.Buffer
	w, err := flate.NewWriter(&buf, flate.DefaultCompression)
	if err != nil {
		return nil, err
	}
	if _, err := w.Write(raw); err != nil {
		return nil, err
	}
	if err := w.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func inflate(raw []byte) ([]byte, error) {
	r := flate.NewReader(bytes.NewReader(raw))
	defer r.Close()
	return io.ReadAll(r)
}
