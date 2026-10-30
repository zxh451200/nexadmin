package saml

import (
	"bytes"
	"compress/flate"
	"crypto/rsa"
	"crypto/x509"
	"encoding/base64"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/beevik/etree"
	"github.com/crewjam/saml"
	"github.com/gin-gonic/gin"
	dsig "github.com/russellhaering/goxmldsig"
)

// SP 侧 SLO 回执校验：真实签名的 LogoutResponse 通过，篡改后拒绝。
func TestSLOAcceptsSignedLogoutResponse(t *testing.T) {
	gin.SetMode(gin.TestMode)
	key, cert := idpKeyPair(t)
	svc, _, _ := sloServiceMeta(t, idpMetadataWithCert("https://idp.example.com", cert))

	router := gin.New()
	router.GET("/api/v1/auth/saml/:id/slo", svc.slo)
	router.POST("/api/v1/auth/saml/:id/slo", svc.slo)
	srv := httptest.NewServer(router)
	defer srv.Close()
	dest := srv.URL + "/api/v1/auth/saml/123/slo"

	signed := signLogoutResponse(t, key, cert, dest)
	packed, err := deflateBytes(signed)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := http.Get(dest + "?SAMLResponse=" + url.QueryEscape(base64.StdEncoding.EncodeToString(packed)))
	if err != nil {
		t.Fatal(err)
	}
	body := make([]byte, 2048)
	n, _ := resp.Body.Read(body)
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("signed LogoutResponse rejected: %d %s", resp.StatusCode, body[:n])
	}

	tampered := bytes.Replace(signed, []byte(saml.StatusSuccess), []byte("urn:oasis:names:tc:SAML:2.0:status:Requester"), 1)
	packedBad, err := deflateBytes(tampered)
	if err != nil {
		t.Fatal(err)
	}
	bad, err := http.Get(dest + "?SAMLResponse=" + url.QueryEscape(base64.StdEncoding.EncodeToString(packedBad)))
	if err != nil {
		t.Fatal(err)
	}
	bad.Body.Close()
	if bad.StatusCode == http.StatusOK {
		t.Fatal("tampered LogoutResponse must be rejected")
	}
}

func idpMetadataWithCert(entityID string, cert *x509.Certificate) string {
	return fmt.Sprintf(`<?xml version="1.0"?>
<EntityDescriptor xmlns="urn:oasis:names:tc:SAML:2.0:metadata" entityID="%s">
  <IDPSSODescriptor protocolSupportEnumeration="urn:oasis:names:tc:SAML:2.0:protocol">
    <KeyDescriptor use="signing">
      <KeyInfo xmlns="http://www.w3.org/2000/09/xmldsig#">
        <X509Data><X509Certificate>%s</X509Certificate></X509Data>
      </KeyInfo>
    </KeyDescriptor>
    <SingleLogoutService Binding="urn:oasis:names:tc:SAML:2.0:bindings:HTTP-Redirect" Location="%s/slo"/>
    <SingleSignOnService Binding="urn:oasis:names:tc:SAML:2.0:bindings:HTTP-Redirect" Location="%s/sso"/>
  </IDPSSODescriptor>
</EntityDescriptor>`, entityID, base64.StdEncoding.EncodeToString(cert.Raw), entityID, entityID)
}

func signLogoutResponse(t *testing.T, key *rsa.PrivateKey, cert *x509.Certificate, dest string) []byte {
	t.Helper()
	resp := saml.LogoutResponse{
		ID: "lr-1", InResponseTo: "req-1", Version: "2.0", IssueInstant: time.Now().UTC(),
		Destination: dest, Issuer: &saml.Issuer{Value: "https://idp.example.com"},
		Status: saml.Status{StatusCode: saml.StatusCode{Value: saml.StatusSuccess}},
	}
	ctx, err := dsig.NewSigningContext(key, [][]byte{cert.Raw})
	if err != nil {
		t.Fatal(err)
	}
	signed, err := ctx.SignEnveloped(resp.Element())
	if err != nil {
		t.Fatal(err)
	}
	doc := etree.NewDocument()
	doc.SetRoot(signed)
	var buf bytes.Buffer
	if _, err := doc.WriteTo(&buf); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func deflateBytes(raw []byte) ([]byte, error) {
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
