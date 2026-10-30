package saml

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/gin-gonic/gin"
	gormmysql "gorm.io/driver/mysql"
	"gorm.io/gorm"

	"nexadmin/core/auth"
	"nexadmin/core/cache"
	"nexadmin/core/config"
	"nexadmin/pkg/idgen"
)

const sloMetadata = `<?xml version="1.0"?>
<EntityDescriptor xmlns="urn:oasis:names:tc:SAML:2.0:metadata" entityID="https://idp.example.com">
  <IDPSSODescriptor protocolSupportEnumeration="urn:oasis:names:tc:SAML:2.0:protocol">
    <SingleSignOnService Binding="urn:oasis:names:tc:SAML:2.0:bindings:HTTP-Redirect" Location="https://idp.example.com/sso"/>
    <SingleLogoutService Binding="urn:oasis:names:tc:SAML:2.0:bindings:HTTP-Redirect" Location="https://idp.example.com/slo"/>
  </IDPSSODescriptor>
</EntityDescriptor>`

type revokingIssuer struct {
	fakeIssuer
	revoked []*auth.Claims
}

func (r *revokingIssuer) RevokeSession(_ context.Context, cl *auth.Claims) error {
	r.revoked = append(r.revoked, cl)
	return nil
}

func sloService(t *testing.T) (*Service, *revokingIssuer, cache.Cache) {
	t.Helper()
	return sloServiceMeta(t, sloMetadata)
}

func sloServiceMeta(t *testing.T, idpMetadata string) (*Service, *revokingIssuer, cache.Cache) {
	t.Helper()
	certPEM, keyEnc, err := newSPKeyPair()
	if err != nil {
		t.Fatal(err)
	}
	src := Source{Name: "corp", Enabled: true, IdPMetadata: idpMetadata, SPCert: certPEM, SPKeyEnc: keyEnc,
		AttrUsername: "uid", AttrEmail: "mail", AttrNickname: "displayName"}
	src.ID = 123

	sqlDB, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	db, err := gorm.Open(gormmysql.New(gormmysql.Config{Conn: sqlDB, SkipInitializeWithVersion: true}), &gorm.Config{DisableAutomaticPing: true})
	if err != nil {
		t.Fatal(err)
	}
	cols := []string{"id", "tenant_id", "name", "enabled", "idp_metadata", "sp_cert", "sp_key_enc",
		"attr_username", "attr_email", "attr_nickname", "auto_create", "match_existing", "default_role", "created_at", "updated_at"}
	row := func() *sqlmock.Rows {
		return sqlmock.NewRows(cols).AddRow(123, 1, src.Name, true, src.IdPMetadata, src.SPCert, src.SPKeyEnc,
			src.AttrUsername, src.AttrEmail, src.AttrNickname, true, false, "", nil, nil)
	}
	mock.ExpectQuery("SELECT \\* FROM `plg_saml_source`").WithArgs("123", 1).WillReturnRows(row())
	mock.ExpectQuery("SELECT \\* FROM `plg_saml_source`").WithArgs("123", 1).WillReturnRows(row())

	ch, err := cache.New(config.Cache{Driver: "memory"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ch.Close() })
	issuer := &revokingIssuer{}
	return NewService(db, issuer, ch), issuer, ch
}

func TestLogoutRejectsAnonymous(t *testing.T) {
	gin.SetMode(gin.TestMode)
	svc, _, _ := sloService(t)
	r := gin.New()
	r.GET("/api/v1/plugins/saml/sources/:id/logout", svc.logout)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest("GET", "/api/v1/plugins/saml/sources/123/logout", nil))
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("anonymous logout: %d %s", w.Code, w.Body.String())
	}
}

func TestLogoutRedirectsToIdPAndRevokes(t *testing.T) {
	gin.SetMode(gin.TestMode)
	svc, issuer, ch := sloService(t)
	cl := &auth.Claims{UserID: 7, TenantID: 1, Username: "alice"}
	if err := ch.Set(context.Background(), sloKey(1, 123, 7), "alice@example.com", 0); err != nil {
		t.Fatal(err)
	}
	r := gin.New()
	r.GET("/api/v1/plugins/saml/sources/:id/logout", func(c *gin.Context) {
		auth.SetClaims(c, cl)
		svc.logout(c)
	})
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest("GET", "/api/v1/plugins/saml/sources/123/logout", nil))
	if w.Code != http.StatusFound {
		t.Fatalf("logout: %d %s", w.Code, w.Body.String())
	}
	loc := w.Header().Get("Location")
	if !strings.HasPrefix(loc, "https://idp.example.com/slo?") || !strings.Contains(loc, "SAMLRequest=") {
		t.Fatalf("redirect: %s", loc)
	}
	if len(issuer.revoked) != 1 || issuer.revoked[0].UserID != 7 {
		t.Fatalf("local session not revoked: %+v", issuer.revoked)
	}
	if _, err := ch.Get(context.Background(), sloKey(1, 123, 7)); err == nil {
		t.Fatal("nameID binding should be cleared after logout")
	}
}

func TestLogoutWithoutSAMLSessionIsRejected(t *testing.T) {
	gin.SetMode(gin.TestMode)
	svc, _, _ := sloService(t)
	r := gin.New()
	r.GET("/api/v1/plugins/saml/sources/:id/logout", func(c *gin.Context) {
		auth.SetClaims(c, &auth.Claims{UserID: idgen.ID(9), TenantID: 1})
		svc.logout(c)
	})
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest("GET", "/api/v1/plugins/saml/sources/123/logout", nil))
	if w.Code != http.StatusBadRequest {
		t.Fatalf("logout without SAML binding: %d %s", w.Code, w.Body.String())
	}
}

func TestSLOResponseValidationRejectsGarbage(t *testing.T) {
	gin.SetMode(gin.TestMode)
	svc, _, _ := sloService(t)
	r := gin.New()
	r.GET("/api/v1/auth/saml/:id/slo", svc.slo)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest("GET", "/api/v1/auth/saml/123/slo?SAMLResponse=not-a-response", nil))
	if w.Code != http.StatusBadRequest {
		t.Fatalf("garbage SLO response: %d %s", w.Code, w.Body.String())
	}
}
