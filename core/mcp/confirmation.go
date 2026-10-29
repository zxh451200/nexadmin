package mcp

import (
	"bytes"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"

	"nexadmin/core/apidoc"
	"nexadmin/core/auth"
	"nexadmin/core/cache"
	"nexadmin/core/i18n"
	"nexadmin/core/response"
	"nexadmin/core/tenant"
	"nexadmin/pkg/idgen"
)

const confirmationTTL = 5 * time.Minute

// errBadArgs 标记"调用方参数不合法"的错误：这类错误文案是本包写死的，可以如实返回；
// 其余错误可能来自数据库 / 鉴权 / 缓存层，对外一律用固定文案，细节只写日志。
var errBadArgs = errors.New("mcp: invalid arguments")

func badArgs(msg string) error { return fmt.Errorf("%w: %s", errBadArgs, msg) }

// reject 记录真实错误（仅日志），并向客户端返回固定的 i18n 文案。
func (s *Server) reject(c *gin.Context, id json.RawMessage, err error) {
	if s.log != nil {
		s.log.Warn("mcp write rejected", zap.Error(err))
	}
	key := "error.mcp_operation_failed"
	if errors.Is(err, errBadArgs) {
		key = "error.bad_request"
	}
	toolError(c, id, i18n.T(c.Request.Context(), key))
}

// WithConfirmLimit 设置确认密码失败的上限：同一「用户+确认 ID」在 window 内超过 limit 次即作废该确认。
// limit <= 0 表示不限制（限流总开关关闭时）。
func (s *Server) WithConfirmLimit(limit int, window time.Duration) *Server {
	s.confirmLimit, s.confirmWindow = limit, window
	return s
}

// WriteConfirmation contains the public metadata of a pending confirmation.
type WriteConfirmation struct {
	ID        string    `json:"confirmationId"`
	URL       string    `json:"confirmationUrl"`
	ExpiresAt time.Time `json:"expiresAt"`
}

// CredentialBinding uses exactly the identity required by MCP consumption.
func (s *Server) CredentialBinding(c *gin.Context) (string, error) {
	owner, err := currentIdentity(c)
	if err != nil || s == nil || s.auth == nil || c.Request.Context().Value(dispatchKey{}) != nil {
		return "", errors.New("confirmation identity unavailable")
	}
	u, err := s.auth.Profile(c.Request.Context(), auth.ClaimsFrom(c))
	if err != nil || u == nil || (!u.Super && !auth.HasPerm(u.Perms, "mcp:list")) {
		return "", errors.New("confirmation permission denied")
	}
	raw, _ := json.Marshal(owner)
	return hash(raw), nil
}

// ConfirmedWrite reuses the MCP parser, validator, atomic consume and dispatcher.
// Trusted server callers select the tool; only explicitly registered writes qualify.
func (s *Server) ConfirmedWrite(c *gin.Context, toolName string, args json.RawMessage) (json.RawMessage, error) {
	if _, err := s.CredentialBinding(c); err != nil {
		return nil, err
	}
	for _, t := range s.doc.Tools() {
		if t.Name != toolName || !t.Write {
			continue
		}
		w := httptest.NewRecorder()
		inner, _ := gin.CreateTestContext(w)
		inner.Request = c.Request
		for key, value := range c.Keys {
			inner.Set(key, value)
		}
		s.write(inner, request{ID: json.RawMessage("1")}, t, args)
		var envelope struct {
			Result json.RawMessage `json:"result"`
		}
		if json.Unmarshal(w.Body.Bytes(), &envelope) != nil || len(envelope.Result) == 0 {
			return nil, errors.New("confirmation operation unavailable")
		}
		var result struct {
			IsError bool `json:"isError"`
		}
		if json.Unmarshal(envelope.Result, &result) != nil || result.IsError {
			return nil, errors.New("confirmation operation rejected")
		}
		return envelope.Result, nil
	}
	return nil, errors.New("write tool unavailable")
}

// WriteApproved is only a preflight; ConfirmedWrite rechecks everything at consumption.
func (s *Server) WriteApproved(c *gin.Context, id, toolName string) error {
	if _, err := s.CredentialBinding(c); err != nil {
		return err
	}
	v, _, err := s.load(c, id)
	owner, identityErr := currentIdentity(c)
	if err != nil || identityErr != nil || v.Owner != owner || v.Action.Tool != toolName ||
		v.Status != "approved" || v.Approver == nil {
		return errors.New("confirmation not approved or unavailable")
	}
	_, err = s.validate(c, v)
	return err
}

// Browser API contract (normal response envelope):
// GET /api/v1/mcp/confirmations/:id -> ConfirmationView
// POST /api/v1/mcp/confirmations/:id/approve -> {confirmationId,status,digest,expiresAt}
// input: ApproveInput. Requires mcp:list, same user/tenant, session JWT AND
// local password reauthentication. PAT/app signatures are never accepted.
// Browser route: /admin/mcp/confirmations/:id (后台 SPA 路径；frontend owns rendering).
type ApproveInput struct {
	Digest   string `json:"digest"`
	Password string `json:"password"`
}

type identity struct {
	User        idgen.ID `json:"user"`
	Tenant      idgen.ID `json:"tenant"`
	ClaimTenant idgen.ID `json:"claimTenant"`
	Principal   string   `json:"principal"`
	Session     string   `json:"session"`
	Credential  string   `json:"credential"`
}

type action struct {
	Tool   string            `json:"tool"`
	Method string            `json:"method"`
	Route  string            `json:"route"`
	Path   map[string]string `json:"path"`
	Query  string            `json:"query"`
	Body   json.RawMessage   `json:"body"`
	Target string            `json:"target"`
}

type confirmation struct {
	ID        string    `json:"confirmationId"`
	Owner     identity  `json:"owner"`
	Action    action    `json:"action"`
	Digest    string    `json:"digest"`
	ExpiresAt time.Time `json:"expiresAt"`
	Status    string    `json:"status"`
	Approver  *identity `json:"approver,omitempty"`
}

type ConfirmationView struct {
	ID             string    `json:"confirmationId"`
	Action         action    `json:"action"`
	Digest         string    `json:"digest"`
	Status         string    `json:"status"`
	ExpiresAt      time.Time `json:"expiresAt"`
	CredentialType string    `json:"credentialType"`
}

func hash(raw []byte) string {
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

func currentIdentity(c *gin.Context) (identity, error) {
	cl := auth.ClaimsFrom(c)
	if cl == nil || cl.UserID == 0 || cl.ID == "" || cl.Principal == auth.PrincipalApp ||
		c.GetHeader("X-App-Key") != "" {
		return identity{}, errors.New("user bearer credential required")
	}
	tid, ok := tenant.From(c.Request.Context())
	if !ok || tid == 0 {
		return identity{}, errors.New("tenant required")
	}
	return identity{User: cl.UserID, Tenant: tid,
		ClaimTenant: cl.TenantID, Principal: cl.Principal, Session: cl.ID,
		Credential: hash([]byte(c.GetHeader("Authorization")))}, nil
}

// EnforceDispatchIdentity runs after original auth/tenant middleware and before
// route permissions. Context identities are never injected into subrequests.
func EnforceDispatchIdentity() gin.HandlerFunc {
	return func(c *gin.Context) {
		if expected, ok := c.Request.Context().Value(dispatchKey{}).(*identity); ok {
			actual, err := currentIdentity(c)
			if err != nil || actual != *expected {
				response.Fail(c, response.Forbidden("error.forbidden"))
				return
			}
		}
		c.Next()
	}
}

func toolSchema(t apidoc.Tool) gin.H {
	schema := inputSchema(t.Path)
	if t.Write {
		props := schema["properties"].(gin.H)
		props["confirmationId"] = gin.H{"type": "string", "minLength": 64, "maxLength": 64}
		props["body"] = gin.H{"type": "object", "additionalProperties": false}
		props["query"] = gin.H{"type": "object", "additionalProperties": false}
		delete(schema, "required")
		schema["oneOf"] = []any{
			gin.H{"required": []string{"path"}, "not": gin.H{"required": []string{"confirmationId"}}},
			gin.H{"required": []string{"confirmationId"}, "not": gin.H{"anyOf": []any{
				gin.H{"required": []string{"path"}}, gin.H{"required": []string{"query"}}, gin.H{"required": []string{"body"}},
			}}},
		}
	}
	return schema
}

func confirmationKey(id string) string { return cache.GlobalKey("mcp", "confirmation", id) }

func (s *Server) capabilities() (cache.Locker, cache.CompareSwapper, error) {
	locker, lok := cache.As[cache.Locker](s.cache)
	cas, cok := cache.As[cache.CompareSwapper](s.cache)
	if !lok || !cok || s.auth == nil {
		return nil, nil, errors.New("confirmation requires atomic cache capabilities")
	}
	return locker, cas, nil
}

func (s *Server) load(c *gin.Context, id string) (confirmation, string, error) {
	var v confirmation
	if _, _, err := s.capabilities(); err != nil {
		return v, "", err
	}
	if len(id) != 64 {
		return v, "", errors.New("invalid confirmation")
	}
	if _, err := hex.DecodeString(id); err != nil {
		return v, "", errors.New("invalid confirmation")
	}
	raw, err := s.cache.Get(c.Request.Context(), confirmationKey(id))
	if err != nil {
		return v, "", errors.New("confirmation unavailable")
	}
	if json.Unmarshal([]byte(raw), &v) != nil || v.ID != id || !time.Now().Before(v.ExpiresAt) {
		return v, "", errors.New("confirmation expired or invalid")
	}
	encoded, _ := json.Marshal(v.Action)
	if v.Digest != hash(encoded) {
		return v, "", errors.New("invalid action digest")
	}
	return v, raw, nil
}

func (s *Server) validate(c *gin.Context, v confirmation) (apidoc.Tool, error) {
	cl := auth.ClaimsFrom(c)
	if cl == nil {
		return apidoc.Tool{}, errors.New("login required")
	}
	u, err := s.auth.Profile(c.Request.Context(), cl)
	if err != nil {
		return apidoc.Tool{}, err
	}
	if err := s.auth.CheckTenant(c.Request.Context(), u); err != nil {
		return apidoc.Tool{}, err
	}
	for _, t := range s.doc.Tools() {
		if t.Name != v.Action.Tool || !t.Write || t.Method != v.Action.Method || t.Path != v.Action.Route {
			continue
		}
		for _, perm := range strings.Split(t.Permission, ",") {
			if perm != "" && !u.Super && !auth.HasPerm(u.Perms, perm) {
				return t, errors.New("action permission denied")
			}
		}
		target, err := targetURL(t, v.Action.Path, nil)
		if err != nil || target.String() != v.Action.Target || v.Action.Query != "" || string(v.Action.Body) != "{}" {
			return t, errors.New("action changed")
		}
		if t.Validate == nil {
			return t, errors.New("write validator missing")
		}
		return t, t.Validate(c.Request.Context(), cl.UserID, v.Action.Path)
	}
	return apidoc.Tool{}, errors.New("write tool no longer available")
}

func (s *Server) audit(event string, v confirmation) {
	if s.log != nil {
		s.log.Info("mcp confirmation", zap.String("event", event), zap.String("confirmation_id", v.ID),
			zap.String("tool", v.Action.Tool), zap.String("digest", v.Digest),
			zap.String("user", v.Owner.User.String()), zap.String("tenant", v.Owner.Tenant.String()))
	}
}

func (s *Server) write(c *gin.Context, req request, t apidoc.Tool, raw json.RawMessage) {
	fail := func(err error) { s.reject(c, req.ID, err) }
	locker, _, err := s.capabilities()
	if err != nil {
		fail(err)
		return
	}
	owner, err := currentIdentity(c)
	if err != nil {
		fail(err)
		return
	}
	var args struct {
		Path           map[string]string          `json:"path"`
		Query          map[string]json.RawMessage `json:"query"`
		Body           json.RawMessage            `json:"body"`
		ConfirmationID string                     `json:"confirmationId"`
	}
	if len(raw) == 0 || decode(raw, &args) != nil || bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		fail(badArgs("invalid write arguments"))
		return
	}
	if args.ConfirmationID != "" {
		// Execution accepts only the ID: even equivalent client actions are rejected.
		var fields map[string]json.RawMessage
		_ = json.Unmarshal(raw, &fields)
		if len(fields) != 1 {
			fail(badArgs("execution accepts confirmationId only"))
			return
		}
		v, stored, err := s.load(c, args.ConfirmationID)
		if err != nil {
			fail(err)
			return
		}
		if v.Owner != owner || v.Action.Tool != t.Name || v.Status != "approved" || v.Approver == nil {
			fail(errors.New("confirmation identity, tool or approval mismatch"))
			return
		}
		resolved, err := s.validate(c, v)
		if err != nil {
			fail(err)
			return
		}
		// Compare-and-delete only after identity, action and current permission checks.
		ok, err := locker.DelIfEqual(c.Request.Context(), confirmationKey(v.ID), stored)
		if err != nil || !ok {
			fail(errors.New("confirmation already consumed or unavailable"))
			return
		}
		s.audit("consumed", v)
		target, _ := url.Parse(v.Action.Target)
		s.dispatch(c, req, resolved, target, v.Action.Body, &v.Owner)
		s.audit("execution_returned", v)
		return
	}
	if len(args.Query) != 0 {
		fail(badArgs("this write tool accepts no query"))
		return
	}
	if len(args.Body) != 0 {
		var body map[string]json.RawMessage
		if json.Unmarshal(args.Body, &body) != nil || body == nil || len(body) != 0 {
			fail(badArgs("this write tool accepts only an empty body"))
			return
		}
	}
	target, err := targetURL(t, args.Path, nil)
	if err != nil {
		fail(badArgs(err.Error()))
		return
	}
	id := make([]byte, 32)
	if _, err := rand.Read(id); err != nil {
		fail(errors.New("cannot create confirmation"))
		return
	}
	v := confirmation{ID: hex.EncodeToString(id), Owner: owner, Status: "pending",
		ExpiresAt: time.Now().UTC().Add(confirmationTTL),
		Action: action{Tool: t.Name, Method: t.Method, Route: t.Path, Path: args.Path,
			Query: "", Body: json.RawMessage("{}"), Target: target.String()}}
	encoded, _ := json.Marshal(v.Action)
	v.Digest = hash(encoded)
	if _, err := s.validate(c, v); err != nil {
		fail(err)
		return
	}
	encoded, _ = json.Marshal(v)
	ok, err := locker.SetNX(c.Request.Context(), confirmationKey(v.ID), string(encoded), time.Until(v.ExpiresAt))
	if err != nil || !ok {
		fail(errors.New("cannot save confirmation"))
		return
	}
	s.audit("requested", v)
	data := gin.H{"confirmationId": v.ID, "confirmationUrl": "/admin/mcp/confirmations/" + v.ID,
		"digest": v.Digest, "expiresAt": v.ExpiresAt, "status": v.Status}
	text, _ := json.Marshal(data)
	result(c, req.ID, gin.H{"content": []any{gin.H{"type": "text", "text": string(text)}}, "structuredContent": data, "isError": false})
}

func (s *Server) browser(c *gin.Context) (confirmation, string, bool) {
	c.Header("Cache-Control", "no-store")
	c.Header("Referrer-Policy", "no-referrer")
	me, err := currentIdentity(c)
	if err != nil || me.Principal != "" || c.Request.Context().Value(dispatchKey{}) != nil {
		response.Fail(c, response.Forbidden("error.forbidden"))
		return confirmation{}, "", false
	}
	v, raw, err := s.load(c, c.Param("id"))
	if err != nil || me.User != v.Owner.User || me.Tenant != v.Owner.Tenant || me.ClaimTenant != v.Owner.ClaimTenant {
		response.Fail(c, response.Forbidden("error.forbidden"))
		return confirmation{}, "", false
	}
	if _, err := s.validate(c, v); err != nil {
		response.Fail(c, response.Forbidden("error.forbidden"))
		return confirmation{}, "", false
	}
	return v, raw, true
}

func (s *Server) Confirmation(c *gin.Context) {
	v, _, ok := s.browser(c)
	if !ok {
		return
	}
	response.OK(c, ConfirmationView{ID: v.ID, Action: v.Action, Digest: v.Digest, Status: v.Status,
		ExpiresAt: v.ExpiresAt, CredentialType: "password"})
}

func (s *Server) Approve(c *gin.Context) {
	v, raw, ok := s.browser(c)
	if !ok {
		return
	}
	var in ApproveInput
	body, err := io.ReadAll(io.LimitReader(c.Request.Body, 4097))
	if err != nil || len(body) > 4096 || decode(body, &in) != nil || in.Digest != v.Digest || v.Status != "pending" {
		response.Fail(c, response.BadRequest("error.bad_request"))
		return
	}
	verifier, ok := s.auth.Provider().(auth.PasswordVerifier)
	if !ok {
		response.Fail(c, response.Unavailable("auth.provider_missing"))
		return
	}
	if !s.countConfirmAttempt(c, v) {
		return
	}
	if err := verifier.VerifyPassword(c.Request.Context(), v.Owner.User, in.Password); err != nil {
		s.audit("credential_rejected", v)
		response.Fail(c, err)
		return
	}
	// Recheck after credential verification; approval never extends the deadline.
	if _, err := s.validate(c, v); err != nil || !time.Now().Before(v.ExpiresAt) {
		response.Fail(c, response.Forbidden("error.forbidden"))
		return
	}
	me, err := currentIdentity(c)
	if err != nil {
		response.Fail(c, response.Forbidden("error.forbidden"))
		return
	}
	v.Status, v.Approver = "approved", &me
	next, _ := json.Marshal(v)
	_, cas, err := s.capabilities()
	if err != nil {
		response.Fail(c, response.Unavailable("error.unavailable"))
		return
	}
	ok, err = cas.CompareAndSwap(c.Request.Context(), confirmationKey(v.ID), raw, string(next))
	if err != nil || !ok {
		response.Fail(c, response.Conflict("error.bad_request"))
		return
	}
	s.audit("approved", v)
	response.OK(c, gin.H{"confirmationId": v.ID, "status": v.Status, "digest": v.Digest, "expiresAt": v.ExpiresAt})
}

// countConfirmAttempt 在校验密码之前按「用户+确认 ID」原子计数（core/cache 的 Incr）。
// 超过 ratelimit.mcp_confirm 上限就作废该确认并要求重新发起；计数器不可用时拒绝（宁可失败也不放开暴力破解）。
// 计数的是校验尝试：一次确认至多成功一次，所以尝试数的上限同时也是失败数的上限。
func (s *Server) countConfirmAttempt(c *gin.Context, v confirmation) bool {
	if s.confirmLimit <= 0 {
		return true
	}
	counter, ok := cache.As[cache.Counter](s.cache)
	if !ok {
		response.Fail(c, response.Unavailable("error.unavailable"))
		return false
	}
	key := cache.GlobalKey("mcp", "confirm-attempts", v.Owner.User.String(), v.ID)
	ttl := s.confirmWindow
	if ttl <= 0 || ttl > confirmationTTL {
		ttl = confirmationTTL
	}
	n, _, err := counter.Incr(c.Request.Context(), key, ttl)
	if err != nil {
		response.Fail(c, response.Unavailable("error.unavailable"))
		return false
	}
	if n > int64(s.confirmLimit) {
		_ = s.cache.Del(c.Request.Context(), confirmationKey(v.ID))
		s.audit("revoked_too_many_attempts", v)
		response.Fail(c, response.New(http.StatusTooManyRequests, "error.mcp_confirmation_revoked"))
		return false
	}
	return true
}
