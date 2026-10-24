// Package smscode 是短信验证码服务：发送（经 pkg/sms）、限流、校验。手机号登录与个人中心绑定手机号共用。
//
// 规则（设计文档 §12）：6 位、5 分钟有效、加盐哈希后存 core/cache；同一手机号 60 秒一次、每天有上限，
// 同一 IP 每小时有上限；同一验证码连续输错 5 次作废；校验成功即作废。
package smscode

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"fmt"
	"math/big"
	"strconv"
	"strings"
	"sync"
	"time"

	"go.uber.org/zap"

	"nexadmin/core/cache"
	"nexadmin/core/config"
	"nexadmin/core/notify"
	"nexadmin/core/response"
	"nexadmin/pkg/sms"
)

// 限流与有效期。
const (
	CodeTTL     = 5 * time.Minute
	Cooldown    = 60 * time.Second
	DailyLimit  = 10
	IPHourLimit = 10
	MaxAttempts = 5
)

// Service 发送与校验验证码。零值不可用，请用 New / Shared。
type Service struct {
	cache    cache.Cache
	log      *zap.Logger
	prov     sms.Provider
	template string
	now      func() time.Time
	rec      notify.RecorderHolder // 发送日志；没有注入时是空操作
}

var (
	once   sync.Once
	shared *Service
)

// Shared 返回进程内共享的服务（首次调用时创建）。短信配置有误时返回一个“未启用”的服务并记错误日志，不影响启动。
func Shared(cfg *config.Config, c cache.Cache, log *zap.Logger) *Service {
	once.Do(func() {
		s, err := New(cfg, c, log)
		if err != nil {
			log.Error("sms disabled: invalid configuration", zap.Error(err))
			s = &Service{cache: c, log: log, now: time.Now}
		}
		shared = s
	})
	return shared
}

// New 按配置创建服务。sms.provider 为空时返回未启用的服务（Enabled()==false）。
// 生产环境启用 log 通道会在启动时警告：它只把验证码写进日志。
func New(cfg *config.Config, c cache.Cache, log *zap.Logger) (*Service, error) {
	prov, err := sms.New(sms.Config{
		Provider: cfg.SMS.Provider, Log: log, Proxy: cfg.Auth.Proxy,
		WebhookURL: config.ExpandEnv(cfg.SMS.Webhook.URL), WebhookSecret: config.ExpandEnv(cfg.SMS.Webhook.Secret),
		AliyunKeyID: config.ExpandEnv(cfg.SMS.Aliyun.AccessKeyID), AliyunKeySecret: config.ExpandEnv(cfg.SMS.Aliyun.AccessKeySecret),
		AliyunSignName: cfg.SMS.Aliyun.SignName, AliyunEndpoint: cfg.SMS.Aliyun.Endpoint,
		TencentSecretID: config.ExpandEnv(cfg.SMS.Tencent.SecretID), TencentSecretKey: config.ExpandEnv(cfg.SMS.Tencent.SecretKey),
		TencentSDKAppID: config.ExpandEnv(cfg.SMS.Tencent.SDKAppID), TencentSignName: config.ExpandEnv(cfg.SMS.Tencent.SignName),
		TencentRegion: config.ExpandEnv(cfg.SMS.Tencent.Region), TencentEndpoint: config.ExpandEnv(cfg.SMS.Tencent.Endpoint),
		TencentParamOrder: cfg.SMS.Tencent.ParamOrder,
	})
	if err != nil {
		return nil, err
	}
	if prov != nil && cfg.SMS.Provider == "log" && cfg.IsProduction() {
		log.Warn("sms.provider=log is enabled in production: verification codes are written to the log instead of being sent")
	}
	tpl := cfg.SMS.Template
	if tpl == "" {
		tpl = "login"
	}
	return &Service{cache: c, log: log, prov: prov, template: tpl, now: time.Now}, nil
}

// SetRecorder 注入发送日志记录器（sendlog 模块在注册时调用）；传 nil 移除。
// 每次真实发出（或发送失败）的验证码短信记一条：只含脱敏前的手机号、模板名、结果与耗时，绝不含验证码本身。
func (s *Service) SetRecorder(r notify.Recorder) {
	if s != nil {
		s.rec.Set(r)
	}
}

type sceneKey struct{}

// WithScene 标记这次发送的场景（notify.Scene*），默认是 notify.SceneVerifyCode。
// 个人中心绑定手机号用 notify.SceneBindPhone，与手机号登录区分开。
func WithScene(ctx context.Context, scene string) context.Context {
	return context.WithValue(ctx, sceneKey{}, scene)
}

func sceneOf(ctx context.Context) string {
	if v, ok := ctx.Value(sceneKey{}).(string); ok && v != "" {
		return v
	}
	return notify.SceneVerifyCode
}

// Enabled 是否配置了可用的短信通道。
func (s *Service) Enabled() bool { return s != nil && s.prov != nil }

func tooMany() error {
	return response.New(429, "error.bad_request").WithMsg("发送过于频繁，请稍后再试")
}

// ErrBadCode 的文案不区分“没发过”“过期”“输错”“已作废”。
func badCode() error {
	return response.BadRequest("error.bad_request").WithMsg("验证码错误或已过期")
}

func digest(parts ...string) string {
	h := sha256.Sum256([]byte(strings.Join(parts, ":")))
	return hex.EncodeToString(h[:])
}

// bump 原子计数（cache.Incr，首次创建时设 ttl）；超过 limit 返回 false。被拒绝的请求也会计数，对限流语义无影响。
func (s *Service) bump(ctx context.Context, key string, limit int, ttl time.Duration) (bool, error) {
	n, err := cache.Incr(s.cache, ctx, key, ttl)
	if err != nil {
		return false, err
	}
	return n <= int64(limit), nil
}

func codeKey(phone string) string { return cache.GlobalKey("sms", "code", digest(phone)) }

// triesKey 是某一条验证码的输错计数 key；带上盐，重新发送的新验证码天然从 0 开始计数。
func triesKey(phone, salt string) string {
	return cache.GlobalKey("sms", "tries", digest(phone), salt)
}

// Send 发送验证码。deliver=false 时走完全相同的限流与计数但不真正发送、不生成验证码：
// 用于登录场景下手机号未绑定任何账号 —— 对外表现与成功发送一致，避免枚举账号，也不浪费短信费用。
func (s *Service) Send(ctx context.Context, phone, ip string, deliver bool) error {
	if !s.Enabled() {
		return response.Unavailable("auth.provider_missing").WithMsg("短信服务未启用")
	}
	ph := digest(phone)
	// 冷却原子化：对冷却 key 做 Incr（首次创建时 TTL=Cooldown），结果 ==1 才放行，并发请求只有一个能拿到。
	coolKey := cache.GlobalKey("sms", "cool", ph)
	if n, err := cache.Incr(s.cache, ctx, coolKey, Cooldown); err != nil {
		return err
	} else if n != 1 {
		return tooMany()
	}
	sent := false // 走到“已处理”为止；中途失败（限流拒绝、渠道报错等）都要释放冷却，免得用户被锁 60 秒却没收到短信
	defer func() {
		if !sent {
			_ = s.cache.Del(ctx, coolKey)
		}
	}()
	for _, c := range []struct {
		key   string
		limit int
		ttl   time.Duration
	}{
		{cache.GlobalKey("sms", "ip", digest(ip)), IPHourLimit, time.Hour},
		{cache.GlobalKey("sms", "day", ph, s.now().UTC().Format("20060102")), DailyLimit, 24 * time.Hour},
	} {
		ok, err := s.bump(ctx, c.key, c.limit, c.ttl)
		if err != nil {
			return err
		}
		if !ok {
			return tooMany()
		}
	}
	if !deliver {
		sent = true
		return nil
	}
	n, err := rand.Int(rand.Reader, big.NewInt(1000000))
	if err != nil {
		return err
	}
	code := fmt.Sprintf("%06d", n.Int64())
	salt := make([]byte, 8)
	if _, err := rand.Read(salt); err != nil {
		return err
	}
	sl := hex.EncodeToString(salt)
	val := fmt.Sprintf("%s|%s|0|%d", sl, digest(sl, phone, code), s.now().Add(CodeTTL).Unix())
	if err := s.cache.Set(ctx, codeKey(phone), val, CodeTTL); err != nil {
		return err
	}
	t0 := time.Now()
	err = s.prov.Send(ctx, phone, s.template, map[string]string{"code": code})
	s.record(ctx, phone, code, err, time.Since(t0))
	if err != nil {
		_ = s.cache.Del(ctx, codeKey(phone)) // 没发出去的验证码不留着
		s.log.Error("send sms failed", zap.String("phone", sms.MaskPhone(phone)), zap.Error(err))
		return response.Unavailable("auth.provider_missing").WithMsg("短信发送失败，请稍后再试")
	}
	sent = true
	return nil
}

// record 记一条发送日志。Subject 只放模板名，Err 只放服务商返回的错误文本（由 Recorder 脱敏截断）；验证码从不出现在这里。
func (s *Service) record(ctx context.Context, phone, code string, err error, spent time.Duration) {
	if !s.rec.Active() {
		return
	}
	r := notify.Record{Channel: notify.RecordSMS, Scene: sceneOf(ctx), Recipient: phone, Subject: s.template,
		Status: notify.StatusSuccess, Duration: spent}
	if err != nil {
		// 服务商的错误文本可能回显请求内容：先把验证码和完整手机号抹掉，再交给 Recorder 做通用脱敏。
		msg := strings.ReplaceAll(err.Error(), code, "[REDACTED]")
		r.Status, r.Err = notify.StatusFail, strings.ReplaceAll(msg, phone, sms.MaskPhone(phone))
	}
	s.rec.Emit(ctx, s.log, r)
}

// Verify 校验验证码，成功即作废；累计输错 MaxAttempts 次后作废。
// 每次校验尝试先原子 Incr 该验证码的尝试计数，再比对：并发猜测最多只有前 MaxAttempts 次会被比对，
// 第 MaxAttempts 次输错、或超过上限的尝试都会用 Del 作废验证码；成功用 GetDel 一次性消费。
func (s *Service) Verify(ctx context.Context, phone, code string) error {
	key := codeKey(phone)
	raw, err := s.cache.Get(ctx, key)
	if errors.Is(err, cache.ErrNotFound) {
		return badCode()
	}
	if err != nil {
		return err
	}
	parts := strings.Split(raw, "|")
	if len(parts) != 4 {
		return badCode()
	}
	exp, _ := strconv.ParseInt(parts[3], 10, 64)
	remain := time.Unix(exp, 0).Sub(s.now())
	tk := triesKey(phone, parts[0])
	if remain <= 0 {
		_ = s.cache.Del(ctx, key) // 计数 key 不删：留给 TTL，避免已通过读取的并发尝试把计数重置为 0
		return badCode()
	}
	n, err := cache.Incr(s.cache, ctx, tk, remain)
	if err != nil {
		return err
	}
	if n > MaxAttempts {
		_ = s.cache.Del(ctx, key) // 计数 key 不删：留给 TTL，避免已通过读取的并发尝试把计数重置为 0
		return badCode()
	}
	if subtle.ConstantTimeCompare([]byte(parts[1]), []byte(digest(parts[0], phone, code))) == 1 {
		// 原子消费：多实例下两个并发的正确校验只有一个成功，另一个按“已作废”处理。
		if _, err := cache.GetDel(s.cache, ctx, key); err != nil {
			if errors.Is(err, cache.ErrNotFound) {
				return badCode()
			}
			return err
		}
		return nil
	}
	if n >= MaxAttempts { // 连续输错到上限：验证码作废，必须重新获取
		_ = s.cache.Del(ctx, key) // 计数 key 不删：留给 TTL，避免已通过读取的并发尝试把计数重置为 0
	}
	return badCode()
}
