package identity

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"

	"nexadmin/core/auth"
	"nexadmin/core/cache"
	"nexadmin/core/response"
	"nexadmin/core/tenant"
	"nexadmin/pkg/idgen"
)

const (
	casCookie     = "nex_cas_state"
	casCookiePath = "/api/v1/plugins/identity/cas"
	stateTTL      = 10 * time.Minute
	casMFATTL     = 90 * time.Second
)

// casStateRec 是缓存里的一次性登录状态：把“发起登录的租户 / 身份源 / 登录后去哪”绑在 state 上。
type casStateRec struct {
	Tenant idgen.ID `json:"t"`
	Source idgen.ID `json:"s"`
	Next   string   `json:"n"`
}

func stateKey(state string) string {
	h := sha256.Sum256([]byte(state))
	return cache.GlobalKey("identity", "cas", "state", hex.EncodeToString(h[:]))
}

func randHex(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

// safeNext 只允许站内相对路径（防开放重定向）。
func safeNext(raw string) string {
	if raw == "" || len(raw) > 512 || raw[0] != '/' || strings.HasPrefix(raw, "//") || strings.ContainsAny(raw, "\\\r\n\t") ||
		strings.HasPrefix(raw, "/login") {
		return "/"
	}
	return raw
}

// casSourceFor 取当前租户下启用的 CAS 源；raw 为空时要求恰好只有一个启用的 CAS 源。
func (s *Service) casSourceFor(ctx context.Context, tid idgen.ID, raw string) (*Source, CASConfig, error) {
	var cfg CASConfig
	if raw == "" {
		list, err := s.st.listEnabled(ctx, tid, TypeCAS)
		if err != nil {
			return nil, cfg, err
		}
		if len(list) != 1 {
			return nil, cfg, badReq("identity.source_required")
		}
		cfg, err = list[0].casConfig()
		return &list[0], cfg, err
	}
	id, err := idgen.Parse(raw)
	if err != nil || id == 0 {
		return nil, cfg, badReq("identity.source_invalid")
	}
	src, err := s.st.get(ctx, tid, id)
	if err != nil {
		return nil, cfg, mapNotFound(err)
	}
	if src.Type != TypeCAS || !src.Enabled {
		return nil, cfg, mapNotFound(errNotFound)
	}
	cfg, err = src.casConfig()
	return src, cfg, err
}

type casHandler struct{ svc *Service }

// GET /plugins/identity/cas/login?source=<id>&next=/path —— 重定向到 CAS 登录页。
// service URL 来自身份源配置（serviceBaseUrl），不读取任何请求头。
func (h *casHandler) login(c *gin.Context) {
	ctx := c.Request.Context()
	tid, err := tenantOf(ctx)
	if err != nil {
		respondErr(c, err)
		return
	}
	src, cfg, err := h.svc.casSourceFor(ctx, tid, c.Query("source"))
	if err != nil {
		respondErr(c, err)
		return
	}
	state, err := randHex(32)
	if err != nil || h.svc.cache == nil {
		respondErr(c, errors.New("identity: cannot create login state"))
		return
	}
	raw, _ := json.Marshal(casStateRec{Tenant: tid, Source: src.ID, Next: safeNext(c.Query("next"))})
	if err := h.svc.cache.Set(ctx, stateKey(state), string(raw), stateTTL); err != nil {
		respondErr(c, err)
		return
	}
	http.SetCookie(c.Writer, &http.Cookie{Name: casCookie, Value: state, Path: casCookiePath, MaxAge: int(stateTTL.Seconds()),
		HttpOnly: true, SameSite: http.SameSiteLaxMode, Secure: strings.HasPrefix(cfg.ServiceBaseURL, "https://")})
	c.Header("Cache-Control", "no-store")
	c.Redirect(http.StatusFound, casLoginRedirect(cfg, casServiceURL(cfg, src.ID, state)))
}

// GET /plugins/identity/cas/logout?source=<id> —— 跳转到 CAS 登出地址（最小实现；不撤销本系统令牌，前端登出请照常进行）。
func (h *casHandler) logout(c *gin.Context) {
	ctx := c.Request.Context()
	tid, err := tenantOf(ctx)
	if err != nil {
		respondErr(c, err)
		return
	}
	_, cfg, err := h.svc.casSourceFor(ctx, tid, c.Query("source"))
	if err != nil {
		respondErr(c, err)
		return
	}
	c.Header("Cache-Control", "no-store")
	c.Redirect(http.StatusFound, casLogoutRedirect(cfg))
}

// callback 校验 state（Cookie 绑定 + 缓存一次性）-> 校验 ticket -> LoginAs。
func (h *casHandler) callback(c *gin.Context) {
	ctx := c.Request.Context()
	fail := func(status int, why string, err error) {
		fields := []zap.Field{zap.String("reason", why), zap.String("ip", c.ClientIP())}
		if err != nil {
			fields = append(fields, zap.Error(err))
		}
		h.svc.log.Warn("identity: cas login refused", fields...)
		writeErrorPage(c, status)
	}
	state, ticket := c.Query("state"), c.Query("ticket")
	ck, cerr := c.Cookie(casCookie)
	if cerr != nil || len(state) != 64 || subtle.ConstantTimeCompare([]byte(ck), []byte(state)) != 1 {
		fail(http.StatusForbidden, "state cookie mismatch", nil) // 登录 CSRF：票据不是由本浏览器发起的流程换来的
		return
	}
	http.SetCookie(c.Writer, &http.Cookie{Name: casCookie, Path: casCookiePath, MaxAge: -1, HttpOnly: true})
	if h.svc.cache == nil || h.svc.auth == nil {
		fail(http.StatusServiceUnavailable, "service not ready", nil)
		return
	}
	rawRec, err := cache.GetDel(h.svc.cache, ctx, stateKey(state))
	var rec casStateRec
	if err != nil || json.Unmarshal([]byte(rawRec), &rec) != nil || rec.Source.String() != c.Query("source") {
		fail(http.StatusForbidden, "unknown or reused state", err)
		return
	}
	// 回调所在租户必须与发起登录的租户一致，A 租户发起的流程不能在 B 租户落地。
	if cur, ok := tenant.From(ctx); !ok || cur != rec.Tenant {
		fail(http.StatusForbidden, "tenant mismatch", nil)
		return
	}
	src, cfg, err := h.svc.casSourceFor(ctx, rec.Tenant, rec.Source.String())
	if err != nil {
		fail(http.StatusForbidden, "source unavailable", err)
		return
	}
	res, err := h.svc.casValidateTicket(ctx, cfg, casServiceURL(cfg, src.ID, state), ticket)
	if err != nil {
		fail(http.StatusForbidden, "ticket validation failed", err)
		return
	}
	if !validSLOIndex(ticket) {
		fail(http.StatusForbidden, "ticket unsuitable for backchannel logout", nil)
		return
	}
	session, err := h.svc.auth.BindExternalSession(ctx, rec.Tenant, casSessionScope(src.ID), ticket)
	if err != nil {
		fail(http.StatusServiceUnavailable, "external session unavailable", nil)
		return
	}
	if err := h.svc.markSLOKnown(ctx, rec.Tenant, src.ID, ticket); err != nil {
		fail(http.StatusServiceUnavailable, "cannot register session for logout", err)
		return
	}
	ctx = auth.WithExternalSession(ctx, session)
	ident, err := res.identityFor(cfg, src.ID)
	if err != nil {
		fail(http.StatusForbidden, "identity rejected", err)
		return
	}
	out, err := h.svc.auth.LoginAs(ctx, auth.LoginAsRequest{Identity: *ident, Method: casLoginType, IP: c.ClientIP(), UserAgent: c.Request.UserAgent()})
	if err != nil {
		fail(http.StatusForbidden, "login refused", err)
		return
	}
	if out.Challenge != nil {
		code, err := randHex(32)
		if err != nil {
			fail(http.StatusServiceUnavailable, "cannot create MFA handoff", nil)
			return
		}
		raw, err := json.Marshal(casMFARec{Tenant: rec.Tenant, Source: rec.Source, State: state,
			IP: c.ClientIP(), Challenge: out.Challenge, Next: rec.Next, Exp: time.Now().Add(casMFATTL).Unix()})
		if err != nil {
			fail(http.StatusServiceUnavailable, "cannot encode MFA handoff", nil)
			return
		}
		if err := h.svc.cache.Set(ctx, casMFAKey(code), string(raw), casMFATTL); err != nil {
			fail(http.StatusServiceUnavailable, "cannot cache MFA handoff", nil)
			return
		}
		http.SetCookie(c.Writer, &http.Cookie{Name: casCookie, Value: state, Path: casCookiePath, MaxAge: int(casMFATTL.Seconds()),
			HttpOnly: true, SameSite: http.SameSiteLaxMode, Secure: strings.HasPrefix(cfg.ServiceBaseURL, "https://")})
		writeMFAHandoffPage(c, code)
		return
	}
	writeSessionPage(c, out.Payload(), rec.Next)
}

type casMFARec struct {
	Tenant    idgen.ID        `json:"t"`
	Source    idgen.ID        `json:"s"`
	State     string          `json:"state"`
	IP        string          `json:"ip"`
	Challenge *auth.Challenge `json:"challenge"`
	Next      string          `json:"next"`
	Exp       int64           `json:"exp"`
}

func casMFAKey(code string) string {
	h := sha256.Sum256([]byte(code))
	return cache.GlobalKey("identity", "cas", "mfa", hex.EncodeToString(h[:]))
}

// 只交接 core 已创建的挑战；令牌仍必须由 CompleteLogin 签发。
func (h *casHandler) exchangeMFA(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	c.Header("Pragma", "no-cache")
	c.Header("Referrer-Policy", "no-referrer")
	reject := func() { respondErr(c, response.BadRequest("auth.challenge_invalid")) }
	var req struct {
		Code string `json:"code"`
	}
	state, err := c.Cookie(casCookie)
	atomicCache, atomicOK := h.svc.cache.(cache.AtomicGetDel)
	if err != nil || len(state) != 64 || c.ShouldBindJSON(&req) != nil || len(req.Code) != 64 || !atomicOK {
		reject()
		return
	}
	ctx := c.Request.Context()
	// 先验证绑定再消费，其他浏览器不能用窃取的 code 破坏合法流程。
	raw, err := h.svc.cache.Get(ctx, casMFAKey(req.Code))
	var rec casMFARec
	tid, ok := tenant.From(ctx)
	if err != nil || json.Unmarshal([]byte(raw), &rec) != nil || !ok || tid != rec.Tenant ||
		rec.IP != c.ClientIP() || subtle.ConstantTimeCompare([]byte(state), []byte(rec.State)) != 1 ||
		rec.Challenge == nil || time.Now().Unix() >= rec.Exp {
		reject()
		return
	}
	if _, _, err := h.svc.casSourceFor(ctx, rec.Tenant, rec.Source.String()); err != nil {
		reject()
		return
	}
	// 原子消费保证并发兑换只有一个成功。
	if _, err := atomicCache.GetDel(ctx, casMFAKey(req.Code)); err != nil {
		reject()
		return
	}
	http.SetCookie(c.Writer, &http.Cookie{Name: casCookie, Path: casCookiePath, MaxAge: -1, HttpOnly: true})
	response.OK(c, gin.H{"challenge": rec.Challenge, "next": safeNext(rec.Next)})
}

func newNonce() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return base64.RawStdEncoding.EncodeToString(b)
}
