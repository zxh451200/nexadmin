package saml

import (
	"strings"
	"testing"

	"github.com/crewjam/saml"
)

const minimalMetadata = `<?xml version="1.0"?>
<EntityDescriptor xmlns="urn:oasis:names:tc:SAML:2.0:metadata" entityID="https://idp.example.com">
  <IDPSSODescriptor protocolSupportEnumeration="urn:oasis:names:tc:SAML:2.0:protocol">
    <SingleSignOnService Binding="urn:oasis:names:tc:SAML:2.0:bindings:HTTP-Redirect" Location="https://idp.example.com/sso"/>
  </IDPSSODescriptor>
</EntityDescriptor>`

func TestValidateMetadata(t *testing.T) {
	if err := validateMetadata(minimalMetadata); err != nil {
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

func TestProviderURLsAndKeyPair(t *testing.T) {
	certPEM, keyEnc, err := newSPKeyPair()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(certPEM, "BEGIN CERTIFICATE") || keyEnc == "" {
		t.Fatal("key pair not generated")
	}
	m := &Source{Name: "corp", IdPMetadata: minimalMetadata, SPCert: certPEM, SPKeyEnc: keyEnc}
	m.ID = 123
	sp, err := (&Service{}).provider(m, "https://admin.example.com")
	if err != nil {
		t.Fatal(err)
	}
	if sp.EntityID != "https://admin.example.com/api/v1/auth/saml/123/metadata" {
		t.Errorf("entity id: %s", sp.EntityID)
	}
	if sp.AcsURL.String() != "https://admin.example.com/api/v1/auth/saml/123/acs" {
		t.Errorf("acs: %s", sp.AcsURL.String())
	}
	if sp.Metadata() == nil {
		t.Fatal("metadata must render")
	}
}

func TestAttrMapping(t *testing.T) {
	a := &saml.Assertion{AttributeStatements: []saml.AttributeStatement{{
		Attributes: []saml.Attribute{
			{Name: "uid", Values: []saml.AttributeValue{{Value: "alice"}}},
			{Name: "mail", Values: []saml.AttributeValue{{Value: "alice@example.com"}}},
		},
	}}}
	if got := attr(a, "uid"); got != "alice" {
		t.Errorf("uid: %q", got)
	}
	if got := attr(a, "mail"); got != "alice@example.com" {
		t.Errorf("mail: %q", got)
	}
	if got := attr(a, "missing"); got != "" {
		t.Errorf("missing: %q", got)
	}
	if got := attr(nil, "uid"); got != "" {
		t.Errorf("nil: %q", got)
	}
}
