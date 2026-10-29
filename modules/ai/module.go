// Package ai 是 AI 网关的 HTTP 层：GET /ai/status、POST /ai/filter、POST /ai/translate、POST /ai/write。
// 模型调用、限流、租户开关与配额、脱敏、校验都在 core/ai；这里只做路由、鉴权、配置读取、
// 租户策略装配与错误映射。
package ai

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"nexadmin/core"
	coreai "nexadmin/core/ai"
	"nexadmin/core/auth"
	"nexadmin/core/config"
	"nexadmin/core/middleware"
	"nexadmin/core/response"
	tn "nexadmin/core/tenant"
	settings "nexadmin/modules/system/config"
)

func init() { core.RegisterBuiltin(module{}) }

type module struct{}

func (module) Name() string { return "ai" }

// Register 注册 /ai/*。/ai/status 登录即可；筛选要求对应页面的列表权限；写作辅助要求公告或官网内容的编辑权限；
// 翻译要求有翻译助手入口的页面之一的编辑权限（公告 / 字典 / 语言包编辑 / 官网内容）。AI 不放大权限，
// 也不能让没有编辑权限的账号白白消耗租户的每日配额。
func (module) Register(r *gin.RouterGroup, d *core.Deps) {
	loc, err := time.LoadLocation(d.Config.App.Timezone)
	if err != nil {
		loc = time.UTC
	}
	gw := NewGateway(d, loc)
	h := &handler{gw: gw}
	g := r.Group("/ai")
	g.GET("/status", h.status)
	g.POST("/filter", h.filter)
	g.POST("/translate", h.translate)
	g.POST("/write", h.write)
}

// NewGateway assembles the same configured gateway used by the builtin AI
// endpoints. Assistant plugins must use this factory with authenticated tenant
// contexts instead of creating an unmetered client.
func NewGateway(d *core.Deps, loc *time.Location) *coreai.Gateway {
	gw := coreai.NewGateway(loader(d.DB, d.Config), d.Log, loc)
	// 每用户限流：用 core/middleware 的缓存计数（redis 多实例共享），规则沿用 ratelimit.ai；
	// 放在网关里而不是路由中间件，这样参数校验没通过的请求不会占用次数。
	rule := d.Config.RateLimit.Rule("ai")
	gw.Limiter = newCacheLimiter(middleware.NewLimiter(d.Cache, d.Log, "ai", rule.Limit, rule.Window))
	gw.Policy = tenantPolicy
	gw.Quota = coreai.CacheCounter(d.Cache)
	return gw
}

// cacheLimiter 把 middleware.Limiter 适配成 core/ai 的 Limiter。
type cacheLimiter struct{ l *middleware.Limiter }

func newCacheLimiter(l *middleware.Limiter) coreai.Limiter { return cacheLimiter{l} }

func (c cacheLimiter) Allow(key string) bool {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	ok, _ := c.l.Hit(ctx, key)
	return ok
}

// tenantPolicy 读取调用方所在租户的 AI 开关与每日配额（复用租户信息的短缓存，不额外查库）。
// 平台租户 / 不受管租户没有租户记录，返回 nil = 不受限。
func tenantPolicy(ctx context.Context, _ coreai.Caller) (*coreai.TenantPolicy, error) {
	info, err := tn.QuotaInfo(ctx)
	if err != nil || info == nil {
		return nil, err
	}
	return &coreai.TenantPolicy{Disabled: info.AIDisabled, DailyQuota: info.AIDailyQuota}, nil
}

// loader 读取 AI 配置：设置页保存在 sys_setting(section=ai)；没有保存过时回落到向导写入 config.yaml 的 ai.*。
func loader(db *gorm.DB, cfg *config.Config) func(ctx context.Context) (coreai.Config, error) {
	return func(ctx context.Context) (coreai.Config, error) {
		fb := coreai.Config{BaseURL: cfg.AI.BaseURL, APIKey: cfg.AI.APIKey, Model: cfg.AI.Model, Redact: coreai.DefaultRedact(), AllowPrivate: cfg.AI.AllowPrivateBaseURL}
		var rows []string
		err := db.WithContext(tn.Skip(ctx)).Table("sys_setting").Where("section = ?", "ai").Limit(1).Pluck("content", &rows).Error
		if err != nil {
			return coreai.Config{}, err
		}
		if len(rows) == 0 {
			return coreai.FromSettings(nil, fb), nil
		}
		var m map[string]any
		if json.Unmarshal([]byte(rows[0]), &m) != nil {
			return coreai.FromSettings(nil, fb), nil // 内容损坏：按未保存处理
		}
		settings.DecryptSecretsForRuntime(m)
		return coreai.FromSettings(m, fb), nil
	}
}

type handler struct {
	gw *coreai.Gateway
}

// scopePerm 是各筛选页面对应的列表权限。
var scopePerm = map[string]string{"user": "user:list", "notice": "notice:list", "operLog": "operLog:list"}

// 写作辅助与翻译所需的业务权限（满足任意一个即可）。权限码以各模块 core.Perm 注册的为准。
// 官网内容都是多语言字段（新闻/页面/特性/导航/站点文案），能编辑该内容即能用 AI 起草与翻译，不额外放大权限。
var websiteEditPerms = []string{"website:post", "website:page", "website:feature", "website:nav", "website:setting"}

var (
	writePerms     = append([]string{"notice:create", "notice:update"}, websiteEditPerms...)
	translatePerms = append([]string{"notice:create", "notice:update", "dict:create", "dict:update", "i18n-editor:edit"}, websiteEditPerms...)
)

// allowedAny 判断当前用户是否拥有任意一个权限点；超管直通。
func allowedAny(c *gin.Context, perms ...string) bool {
	for _, p := range perms {
		if allowed(c, p) {
			return true
		}
	}
	return false
}

func (h *handler) status(c *gin.Context) {
	// 前端据 enabled 隐藏入口，所以未启用也返回 200 + enabled=false；其余接口未启用时返回 AI_DISABLED。
	response.OK(c, h.gw.Status(c.Request.Context(), caller(c)))
}

func (h *handler) filter(c *gin.Context) {
	var req struct {
		Scope string `json:"scope"`
		Text  string `json:"text"`
	}
	if !bind(c, &req) {
		return
	}
	if perm, ok := scopePerm[req.Scope]; ok && !allowed(c, perm) {
		response.Fail(c, response.Forbidden("error.forbidden"))
		return
	}
	res, err := h.gw.Filter(c.Request.Context(), caller(c), req.Scope, req.Text)
	if err != nil {
		fail(c, err)
		return
	}
	response.OK(c, res)
}

func (h *handler) translate(c *gin.Context) {
	if !allowedAny(c, translatePerms...) {
		response.Fail(c, response.Forbidden("error.forbidden"))
		return
	}
	var req struct {
		Text   string   `json:"text"`
		Texts  []string `json:"texts"` // 批量；与 text 二选一
		From   string   `json:"from"`
		To     string   `json:"to"`
		Format string   `json:"format"`
	}
	if !bind(c, &req) {
		return
	}
	if req.Format == "" {
		req.Format = "text"
	}
	batch := len(req.Texts) > 0
	texts := req.Texts
	if !batch {
		texts = []string{req.Text}
	}
	out, err := h.gw.Translate(c.Request.Context(), caller(c), coreai.TranslateRequest{Texts: texts, From: req.From, To: req.To, Format: req.Format})
	if err != nil {
		fail(c, err)
		return
	}
	if batch {
		response.OK(c, gin.H{"texts": out})
		return
	}
	response.OK(c, gin.H{"text": out[0]})
}

func (h *handler) write(c *gin.Context) {
	if !allowedAny(c, writePerms...) {
		response.Fail(c, response.Forbidden("error.forbidden"))
		return
	}
	var req struct {
		Mode   string `json:"mode"`   // write | polish
		Target string `json:"target"` // title | body
		Text   string `json:"text"`
		Lang   string `json:"lang"`
		Tone   string `json:"tone"`
		Format string `json:"format"` // text | html
	}
	if !bind(c, &req) {
		return
	}
	out, err := h.gw.Write(c.Request.Context(), caller(c), coreai.WriteRequest{
		Mode: req.Mode, Target: req.Target, Text: req.Text, Lang: req.Lang, Tone: req.Tone, Format: req.Format,
	})
	if err != nil {
		fail(c, err)
		return
	}
	response.OK(c, gin.H{"text": out})
}

// bind 限制请求体大小后解析 JSON。
func bind(c *gin.Context, v any) bool {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 256<<10)
	return response.BindJSON(c, v)
}

func caller(c *gin.Context) coreai.Caller {
	cl := auth.ClaimsFrom(c)
	if cl == nil {
		return coreai.Caller{}
	}
	tid := cl.TenantID
	if id, ok := tn.From(c.Request.Context()); ok {
		tid = id // 配额与开关以请求解析出的租户为准（与 tenantPolicy 读取的租户一致）
	}
	return coreai.Caller{
		UserID: strconv.FormatInt(int64(cl.UserID), 10), TenantID: strconv.FormatInt(int64(tid), 10),
		// 平台超管与平台租户（默认租户）不受租户级开关和配额限制。
		Exempt: cl.Super || tid == tn.DefaultID,
	}
}

// allowed 判断当前用户是否拥有权限点；超管直通。
func allowed(c *gin.Context, perm string) bool {
	cl := auth.ClaimsFrom(c)
	if cl == nil {
		return false
	}
	if cl.Super {
		return true
	}
	svc := auth.Default()
	if svc == nil || svc.Provider() == nil {
		return false
	}
	u, err := svc.Profile(c.Request.Context(), cl)
	return err == nil && auth.HasPerm(u.Perms, perm)
}

// fail 把网关错误映射成统一响应；上游细节不外泄（Wrap 的错误只进日志，且已不含响应体）。
func fail(c *gin.Context, err error) {
	var e *response.Error
	switch {
	case errors.Is(err, coreai.ErrDisabled):
		e = response.New(http.StatusServiceUnavailable, "error.ai_disabled")
		e.Reason = "AI_DISABLED"
	case errors.Is(err, coreai.ErrTenantDisabled):
		e = response.Forbidden("error.ai_tenant_disabled")
		e.Reason = tn.ReasonAIDisabled
	case errors.Is(err, coreai.ErrQuotaExceeded):
		var qe *coreai.QuotaError
		limit := 0
		if errors.As(err, &qe) {
			limit = qe.Limit
		}
		e = response.New(http.StatusTooManyRequests, "error.ai_quota_exceeded", limit)
		e.Reason = tn.ReasonQuotaAI
	case errors.Is(err, coreai.ErrRateLimited):
		c.Header("Retry-After", "60")
		e = response.New(http.StatusTooManyRequests, "error.ai_rate_limited")
	case errors.Is(err, coreai.ErrInputTooLong):
		e = response.BadRequest("error.ai_input_too_long")
	case errors.Is(err, coreai.ErrBadInput):
		e = response.BadRequest("error.bad_request")
	case errors.Is(err, coreai.ErrTimeout):
		e = response.New(http.StatusGatewayTimeout, "error.ai_timeout")
	case errors.Is(err, coreai.ErrBadOutput):
		e = response.New(http.StatusBadGateway, "error.ai_bad_output")
	case errors.Is(err, coreai.ErrUpstream):
		e = response.New(http.StatusBadGateway, "error.ai_unavailable")
	default:
		response.Fail(c, err) // 配置读取失败等：500，细节只进日志
		return
	}
	response.Fail(c, e.Wrap(err))
}
