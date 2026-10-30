package openapi

import (
	"context"
	"errors"
	"io"
	"net/http"
	"path"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
	"go.uber.org/zap"

	"nexadmin/core/auth"
	"nexadmin/core/cache"
	"nexadmin/core/middleware"
	"nexadmin/core/response"
	"nexadmin/core/secret"
)

const (
	// TimestampWindow 是签名时间窗（±5 分钟）。
	TimestampWindow = 5 * time.Minute
	// MaxSignedBody 是需要参与签名的请求体上限。
	MaxSignedBody = 10 << 20
	// touchInterval：同一令牌/应用每分钟最多写一次 last_used_at。
	touchInterval = time.Minute
	apiPrefix     = "/api/v1"
)

// Options 是 Authenticate 的依赖。Limiter 可为 nil（不限流）；Now 默认 time.Now。
type Options struct {
	Store   Store
	Cache   cache.Cache
	Log     *zap.Logger
	Limiter *middleware.Limiter // 令牌/签名校验失败按 IP 计数，达到上限后 429
	Now     func() time.Time
}

type authn struct {
	Options
}

// Authenticate 返回开放平台鉴权中间件，接在 ParseToken 之后、Tenant 之前：
//   - 已有登录会话（JWT）则原样放行；
//   - Authorization: Bearer nxp_... 按个人访问令牌处理，等同令牌所有者；
//   - 带 X-App-Key 的请求按应用签名处理，以应用绑定的角色集合授权；
//   - 其余请求不动。
func Authenticate(o Options) gin.HandlerFunc {
	if o.Now == nil {
		o.Now = time.Now
	}
	if o.Log == nil {
		o.Log = zap.NewNop()
	}
	a := &authn{o}
	return func(c *gin.Context) {
		if auth.ClaimsFrom(c) != nil {
			c.Next()
			return
		}
		h := c.GetHeader("Authorization")
		switch {
		case strings.HasPrefix(h, "Bearer "+TokenPrefix):
			a.pat(c, strings.TrimPrefix(h, "Bearer "))
		case c.GetHeader("X-App-Key") != "":
			a.app(c)
		default:
			c.Next()
		}
	}
}

// unauthorized / forbidden 收 i18n key（openapi.*）与可选参数，按请求语言翻译；调用方不传成品文案。
func unauthorized(c *gin.Context, key string, args ...any) {
	response.Fail(c, response.Unauthorized(key, args...))
}

func forbidden(c *gin.Context, key string, args ...any) {
	response.Fail(c, response.Forbidden(key, args...))
}

func ipKey(c *gin.Context) string { return "ip:" + c.ClientIP() }

// blocked：该 IP 的失败次数已达上限则直接 429。
func (a *authn) blocked(c *gin.Context) bool {
	if a.Limiter != nil && a.Limiter.Blocked(c.Request.Context(), ipKey(c)) {
		middleware.TooManyRequests(c, a.Limiter.Window())
		return true
	}
	return false
}

// fail 记录一次失败并返回 401；key 是 openapi 文案字典的 key。
func (a *authn) fail(c *gin.Context, key string) {
	if a.Limiter != nil {
		a.Limiter.Fail(c.Request.Context(), ipKey(c))
	}
	unauthorized(c, key)
}

// relPath 返回去掉 /api/v1 前缀并规整后的请求路径，用于禁用清单匹配。
func relPath(c *gin.Context) string {
	p := path.Clean("/" + strings.TrimPrefix(c.Request.URL.Path, apiPrefix))
	return p
}

func hasPathPrefix(p, prefix string) bool { return p == prefix || strings.HasPrefix(p, prefix+"/") }

// patForbidden：令牌不能用于登录相关接口，也不能创建/管理令牌自身（防提权）。
func patForbidden(p string) bool {
	return hasPathPrefix(p, "/auth") || hasPathPrefix(p, "/profile/tokens") || hasPathPrefix(p, "/oauth/authorize") ||
		hasPathPrefix(p, "/mcp/confirmations")
}

// appForbidden：服务账号没有用户身份，不能碰登录/个人中心/授权确认，也不能管理开放应用本身（防自我提权）。
func appForbidden(p string) bool {
	return hasPathPrefix(p, "/auth") || hasPathPrefix(p, "/profile") || hasPathPrefix(p, "/oauth/authorize") ||
		hasPathPrefix(p, "/openapi/apps") || hasPathPrefix(p, "/mcp/confirmations") ||
		hasPathPrefix(p, "/sso") || hasPathPrefix(p, "/settings") || hasPathPrefix(p, "/configs") ||
		hasPathPrefix(p, "/plugins") || hasPathPrefix(p, "/recycle") || hasPathPrefix(p, "/online-users") ||
		hasPathPrefix(p, "/audit")
}

// TokenCallable 报告个人访问令牌能否调用该接口；rel 是去掉 /api/v1 前缀的路径（接口文档与中间件共用同一套规则）。
func TokenCallable(rel string) bool { return !patForbidden(path.Clean("/" + rel)) }

// AppCallable 报告开放应用签名能否调用该接口；rel 同 TokenCallable。
func AppCallable(rel string) bool { return !appForbidden(path.Clean("/" + rel)) }

// touch 在每个 key 每分钟只放行一次（true = 这次该写库），cache 不可用时不写（last_used_at 只是提示信息）。
func (a *authn) touch(ctx context.Context, kind, id string) bool {
	if a.Cache == nil {
		return false
	}
	key := cache.GlobalKey("openapi", "touch", kind, id)
	if ctr, ok := a.Cache.(cache.Counter); ok {
		n, _, err := ctr.Incr(ctx, key, touchInterval)
		return err == nil && n == 1
	}
	if ok, err := a.Cache.Exists(ctx, key); err != nil || ok {
		return false
	}
	return a.Cache.Set(ctx, key, "1", touchInterval) == nil
}

// ---- 个人访问令牌 ----

func (a *authn) pat(c *gin.Context, token string) {
	if a.blocked(c) {
		return
	}
	ctx := c.Request.Context()
	if !IsPATFormat(token) {
		a.fail(c, "openapi.invalid_token")
		return
	}
	rec, err := a.Store.PATByHash(ctx, HashToken(token))
	if err != nil {
		response.Fail(c, err)
		return
	}
	now := a.Now()
	if rec == nil || (rec.ExpiresAt != nil && !now.Before(*rec.ExpiresAt)) {
		a.fail(c, "openapi.token_expired")
		return
	}
	owner, err := a.Store.OwnerOf(ctx, rec.UserID)
	if err != nil {
		response.Fail(c, err)
		return
	}
	if owner == nil || owner.TenantID != rec.TenantID { // 所有者被停用/删除 -> 令牌失效
		a.fail(c, "openapi.token_expired")
		return
	}
	if rec.Scope != "write" {
		switch c.Request.Method {
		case http.MethodGet, http.MethodHead, http.MethodOptions:
		default:
			forbidden(c, "openapi.token_scope_readonly")
			return
		}
	}
	if patForbidden(relPath(c)) {
		forbidden(c, "openapi.pat_endpoint_forbidden")
		return
	}
	auth.SetClaims(c, &auth.Claims{
		UserID: owner.ID, TenantID: owner.TenantID, Username: owner.Username, Super: owner.IsSuper,
		Kind: "access", Principal: auth.PrincipalPAT,
		RegisteredClaims: jwt.RegisteredClaims{ID: "pat:" + rec.ID.String(), IssuedAt: jwt.NewNumericDate(now), ExpiresAt: jwt.NewNumericDate(now.Add(time.Minute))},
	})
	if a.touch(ctx, "pat", rec.ID.String()) {
		if err := a.Store.TouchPAT(ctx, rec.ID, now.UTC()); err != nil {
			a.Log.Warn("openapi: update pat last_used_at failed", zap.Error(err))
		}
	}
	c.Next()
}

// ---- 应用签名 ----

func (a *authn) app(c *gin.Context) {
	if a.blocked(c) {
		return
	}
	ctx := c.Request.Context()
	key := strings.TrimSpace(c.GetHeader("X-App-Key"))
	tsRaw := strings.TrimSpace(c.GetHeader("X-Timestamp"))
	nonce := strings.TrimSpace(c.GetHeader("X-Nonce"))
	sig := strings.TrimSpace(c.GetHeader("X-Signature"))
	if key == "" || tsRaw == "" || nonce == "" || sig == "" || len(nonce) > 128 || len(key) > 64 {
		a.fail(c, "openapi.signature_headers_invalid")
		return
	}
	ts, err := strconv.ParseInt(tsRaw, 10, 64)
	if err != nil {
		a.fail(c, "openapi.timestamp_invalid")
		return
	}
	now := a.Now()
	if d := now.Sub(time.Unix(ts, 0)); d > TimestampWindow || d < -TimestampWindow {
		a.fail(c, "openapi.timestamp_out_of_range")
		return
	}
	app, err := a.Store.AppByKey(ctx, key)
	if err != nil {
		response.Fail(c, err)
		return
	}
	if app == nil || !app.Enabled {
		a.fail(c, "openapi.signature_invalid") // 不区分应用不存在/已停用/签名错误，避免枚举
		return
	}
	plain, err := secret.Decrypt(app.SecretEnc)
	if err != nil {
		response.Fail(c, err)
		return
	}
	body, tooBig, err := readBody(c)
	if err != nil {
		response.Fail(c, err)
		return
	}
	if tooBig {
		response.Fail(c, response.New(http.StatusRequestEntityTooLarge, "error.payload_too_large"))
		return
	}
	if !VerifySignature(plain, c.Request.Method, c.Request.URL.EscapedPath(), c.Request.URL.RawQuery, body, tsRaw, nonce, sig) {
		a.fail(c, "openapi.signature_invalid")
		return
	}
	if !IPAllowed(app.IPList(), c.ClientIP()) {
		a.fail(c, "openapi.ip_not_allowed")
		return
	}
	replay, err := a.replayed(ctx, app.ID.String(), nonce)
	if err != nil {
		response.Fail(c, response.Unavailable("openapi.replay_unavailable").Wrap(err))
		return
	}
	if replay {
		a.fail(c, "openapi.nonce_reused")
		return
	}
	if appForbidden(relPath(c)) {
		forbidden(c, "openapi.app_endpoint_forbidden")
		return
	}
	perms, err := a.Store.AppPerms(ctx, app)
	if err != nil {
		response.Fail(c, err)
		return
	}
	auth.SetClaims(c, &auth.Claims{
		UserID: app.ID, TenantID: app.TenantID, Username: "app:" + app.AppKey, Super: false,
		Kind: "access", Principal: auth.PrincipalApp, Perms: perms,
		RegisteredClaims: jwt.RegisteredClaims{ID: "app:" + app.ID.String(), IssuedAt: jwt.NewNumericDate(now), ExpiresAt: jwt.NewNumericDate(now.Add(time.Minute))},
	})
	if a.touch(ctx, "app", app.ID.String()) {
		if err := a.Store.TouchApp(ctx, app.ID, now.UTC()); err != nil {
			a.Log.Warn("openapi: update app last_used_at failed", zap.Error(err))
		}
	}
	c.Next()
}

// replayed 记录 nonce；已出现过返回 true。nonce 保留 2×时间窗+1 分钟，覆盖整个可接受区间。
// 缓存不可用时返回错误（调用方拒绝请求）：重放保护宁可失败也不能静默失效。
func (a *authn) replayed(ctx context.Context, appID, nonce string) (bool, error) {
	if a.Cache == nil {
		return false, errors.New("openapi: no cache for nonce store")
	}
	key := cache.GlobalKey("openapi", "nonce", appID, HashToken(nonce)[:32])
	ttl := 2*TimestampWindow + time.Minute
	if ctr, ok := a.Cache.(cache.Counter); ok {
		n, _, err := ctr.Incr(ctx, key, ttl)
		return n > 1, err
	}
	exists, err := a.Cache.Exists(ctx, key)
	if err != nil || exists {
		return exists, err
	}
	return false, a.Cache.Set(ctx, key, "1", ttl)
}

// readBody 读出（并还原）请求体用于签名；超过 MaxSignedBody 返回 tooBig。
func readBody(c *gin.Context) (body []byte, tooBig bool, err error) {
	if c.Request.Body == nil || c.Request.Body == http.NoBody {
		return nil, false, nil
	}
	b, err := io.ReadAll(io.LimitReader(c.Request.Body, MaxSignedBody+1))
	_ = c.Request.Body.Close()
	if err != nil {
		return nil, false, response.BadRequest("error.bad_request").Wrap(err)
	}
	if len(b) > MaxSignedBody {
		return nil, true, nil
	}
	c.Request.Body = io.NopCloser(bodyReader(b))
	return b, false, nil
}
