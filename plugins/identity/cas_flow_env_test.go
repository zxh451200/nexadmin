package identity

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"

	"github.com/gin-gonic/gin"

	"nexadmin/core/auth"
	"nexadmin/core/cache"
	"nexadmin/core/tenant"
	"nexadmin/pkg/idgen"
)

const casSuccessFor = `<cas:serviceResponse xmlns:cas="http://www.yale.edu/tp/cas"><cas:authenticationSuccess><cas:user>%s</cas:user>` +
	`<cas:attributes><cas:displayName>Some One</cas:displayName></cas:attributes></cas:authenticationSuccess></cas:serviceResponse>`

type casEnv struct {
	e     *gin.Engine
	svc   *Service
	prov  *fakeProvider
	auth  *auth.Service
	cas   *httptest.Server
	srcID idgen.ID

	mu       sync.Mutex
	hits     int
	services []string
}

func (c *casEnv) hitCount() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.hits
}

func newCASEnv(t *testing.T) *casEnv {
	t.Helper()
	initTest(t)
	gin.SetMode(gin.TestMode)
	c := &casEnv{}
	c.cas = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/cas/p3/serviceValidate" {
			http.NotFound(w, r)
			return
		}
		ticket := r.URL.Query().Get("ticket")
		c.mu.Lock()
		c.hits++
		c.services = append(c.services, r.URL.Query().Get("service"))
		c.mu.Unlock()
		switch {
		case strings.HasPrefix(ticket, "ST-good-"):
			_, _ = w.Write([]byte(strings.Replace(casSuccessFor, "%s", "carol", 1)))
		case strings.HasPrefix(ticket, "ST-dave-"):
			_, _ = w.Write([]byte(strings.Replace(casSuccessFor, "%s", "dave", 1)))
		case strings.HasPrefix(ticket, "ST-xxe-"):
			_, _ = w.Write([]byte(`<!DOCTYPE r [<!ENTITY x SYSTEM "file:///etc/passwd">]><cas:serviceResponse xmlns:cas="http://www.yale.edu/tp/cas"><cas:authenticationSuccess><cas:user>&x;</cas:user></cas:authenticationSuccess></cas:serviceResponse>`))
		case strings.HasPrefix(ticket, "ST-big-"):
			_, _ = w.Write([]byte(strings.Repeat("a", maxCASBody+10)))
		case strings.HasPrefix(ticket, "ST-redir-"):
			http.Redirect(w, r, "http://127.0.0.1:1/evil", http.StatusFound)
		default:
			_, _ = w.Write([]byte(`<cas:serviceResponse xmlns:cas="http://www.yale.edu/tp/cas"><cas:authenticationFailure code="INVALID_TICKET">no</cas:authenticationFailure></cas:serviceResponse>`))
		}
	}))
	t.Cleanup(c.cas.Close)

	var ch cache.Cache
	c.auth, c.prov, ch = newAuthService(t)
	c.prov.add(1, "carol", false)
	c.svc = NewService(newMemStore(), nil)
	c.svc.auth, c.svc.cache = c.auth, ch
	cfg := CASConfig{ServerURL: c.cas.URL + "/cas", ServiceBaseURL: "https://admin.example.com", Version: "3", MatchExisting: true, AttrNickname: "displayName"}
	v, err := c.svc.Save(tctx(1), 0, SourceForm{Type: TypeCAS, Name: "cas", Enabled: true, Config: mustJSON(t, cfg)})
	if err != nil {
		t.Fatal(err)
	}
	c.srcID = v.ID
	h := &casHandler{svc: c.svc}
	c.e = gin.New()
	c.e.Use(func(g *gin.Context) {
		tid := idgen.ID(1)
		if v := g.GetHeader("X-Test-Tenant"); v != "" {
			tid, _ = idgen.Parse(v)
		}
		g.Request = g.Request.WithContext(tenant.With(g.Request.Context(), tid))
	})
	g := c.e.Group("/api/v1/plugins/identity/cas")
	g.GET("/login", h.login)
	g.GET("/callback", h.callback)
	g.GET("/logout", h.logout)
	return c
}

func (c *casEnv) get(path, tenantID, cookie string, hdr map[string]string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodGet, path, nil)
	if tenantID != "" {
		req.Header.Set("X-Test-Tenant", tenantID)
	}
	if cookie != "" {
		req.AddCookie(&http.Cookie{Name: casCookie, Value: cookie})
	}
	for k, v := range hdr {
		req.Header.Set(k, v)
	}
	w := httptest.NewRecorder()
	c.e.ServeHTTP(w, req)
	return w
}

// start 发起登录，返回 state（同时存在于 Cookie 与 service URL 里）。
func (c *casEnv) start(t *testing.T, query string) (state string, loc *url.URL) {
	t.Helper()
	w := c.get("/api/v1/plugins/identity/cas/login?"+query, "", "", nil)
	if w.Code != http.StatusFound {
		t.Fatalf("login start: %d %s", w.Code, w.Body.String())
	}
	for _, ck := range w.Result().Cookies() {
		if ck.Name == casCookie {
			state = ck.Value
			if !ck.HttpOnly || ck.SameSite != http.SameSiteLaxMode || !ck.Secure || ck.MaxAge <= 0 {
				t.Fatalf("state cookie flags: %+v", ck)
			}
		}
	}
	loc, _ = url.Parse(w.Header().Get("Location"))
	return state, loc
}

func (c *casEnv) callback(state, ticket, tenantID, cookie string) *httptest.ResponseRecorder {
	if strings.HasPrefix(ticket, "ST-") && !strings.Contains(ticket, "-a1b2c3d4e5f6g7h8i9j0k1l2m3n4o5p6q7") {
		ticket += "-a1b2c3d4e5f6g7h8i9j0k1l2m3n4o5p6q7"
	}
	q := url.Values{"source": {c.srcID.String()}, "state": {state}, "ticket": {ticket}}
	return c.get("/api/v1/plugins/identity/cas/callback?"+q.Encode(), tenantID, cookie, nil)
}
