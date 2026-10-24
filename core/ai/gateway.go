package ai

import (
	"context"
	"errors"
	"time"

	"go.uber.org/zap"
)

// 输入上限（按字符数）。
const (
	MaxFilterRunes    = 200
	MaxTranslateRunes = 8000
	MaxBatchItems     = 50
	DefaultRatePerMin = 20
)

// Caller 是发起调用的用户，只用于限流与审计。
type Caller struct {
	UserID   string
	TenantID string
	// Exempt 为 true 时不受租户级开关与配额限制（平台超管 / 平台租户）。
	Exempt bool
}

func (c Caller) key() string { return c.TenantID + ":" + c.UserID }

// Gateway 是 AI 网关：读配置、限流、调模型、记审计。
type Gateway struct {
	// Load 每次调用读取当前配置（设置页保存后立即生效）。
	Load    func(ctx context.Context) (Config, error)
	Client  *Client
	Limiter Limiter
	// Policy / Quota 实现租户级开关与每日配额；任一为 nil 时对应能力关闭。
	Policy PolicyFunc
	Quota  Counter
	Log    *zap.Logger
	Now    func() time.Time // 提示里的“今天”，须已转换到站点时区
}

// NewGateway 创建网关：默认每用户每分钟 20 次，Log 为 nil 时不输出。
func NewGateway(load func(ctx context.Context) (Config, error), log *zap.Logger, loc *time.Location) *Gateway {
	if log == nil {
		log = zap.NewNop()
	}
	if loc == nil {
		loc = time.UTC
	}
	return &Gateway{
		Load: load, Client: &Client{}, Log: log,
		Limiter: NewWindowLimiter(DefaultRatePerMin, time.Minute),
		Now:     func() time.Time { return time.Now().In(loc) },
	}
}

// StatusInfo 是 GET /ai/status 的数据（前端 AiStatus）。不含 Key 与地址。
type StatusInfo struct {
	Enabled bool   `json:"enabled"`
	Model   string `json:"model"`
}

// Status 返回是否可用；读配置失败按未启用处理（只进日志）。租户被关闭 AI 时同样返回未启用，前端据此隐藏入口。
func (g *Gateway) Status(ctx context.Context, who Caller) StatusInfo {
	cfg, err := g.Load(ctx)
	if err != nil {
		g.Log.Warn("ai status: load settings failed", zap.Error(err))
		return StatusInfo{}
	}
	if !cfg.Ready() || !g.tenantAllowed(ctx, who) {
		return StatusInfo{}
	}
	return StatusInfo{Enabled: true, Model: cfg.Normalize().Model}
}

// begin 检查配置、租户开关 / 配额与限流，通过后返回配置。租户配额放在每用户限流之后，
// 这样被限流挡掉的请求不会消耗租户的每日次数。
func (g *Gateway) begin(ctx context.Context, who Caller) (Config, error) {
	cfg, err := g.Load(ctx)
	if err != nil {
		return Config{}, err // 非 AI 错误，交给调用方当 500
	}
	if !cfg.Ready() {
		return Config{}, ErrDisabled
	}
	pol := g.policyFor(ctx, who)
	if pol != nil && pol.Disabled {
		return Config{}, ErrTenantDisabled // 关闭的租户不占用限流窗口
	}
	if g.Limiter != nil && !g.Limiter.Allow(who.key()) {
		return Config{}, ErrRateLimited
	}
	if err := g.consumeQuota(ctx, who, pol); err != nil {
		return Config{}, err
	}
	return cfg.Normalize(), nil
}

// chat 调模型并返回耗时；审计由调用方在校验完输出后统一记（audit）。
func (g *Gateway) chat(ctx context.Context, cfg Config, system, user string, maxTokens int) (*Result, time.Duration, error) {
	start := time.Now()
	res, err := g.Client.Chat(ctx, cfg, system, user, maxTokens)
	return res, time.Since(start), err
}

// audit 只记元信息：用户、接口、模型、耗时、token、结果类别。不记提示词、回复、Key。
func (g *Gateway) audit(who Caller, endpoint, model string, dur time.Duration, res *Result, err error) {
	f := []zap.Field{
		zap.String("endpoint", endpoint), zap.String("user_id", who.UserID), zap.String("tenant_id", who.TenantID),
		zap.String("model", model), zap.Int64("duration_ms", dur.Milliseconds()), zap.String("outcome", outcome(err)),
	}
	if res != nil {
		f = append(f, zap.Int("prompt_tokens", res.PromptTokens), zap.Int("completion_tokens", res.CompletionTokens), zap.Int("total_tokens", res.TotalTokens))
	}
	if err != nil {
		g.Log.Warn("ai call", f...)
		return
	}
	g.Log.Info("ai call", f...)
}

func outcome(err error) string {
	switch {
	case err == nil:
		return "ok"
	case errors.Is(err, ErrTimeout):
		return "timeout"
	case errors.Is(err, ErrBadOutput):
		return "bad_output"
	default:
		return "upstream_error"
	}
}
