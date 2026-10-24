package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strconv"
	"sync"
	"time"

	"nexadmin/core/cache"
	"nexadmin/core/response"
)

// 二次验证挑战的默认限制。
const (
	DefaultChallengeTTL      = 5 * time.Minute
	DefaultChallengeAttempts = 5 // 每个挑战最多允许几次验证尝试（含失败与成功）
)

// LoginMeta 描述一次登录的来源，随挑战一起绑定。
type LoginMeta struct {
	Method    string // 凭据校验方式，写入登录日志与门的入参
	IP        string
	UserAgent string
}

// Challenge 是要求二次验证时返回给前端的短期挑战（不含令牌）。
type Challenge struct {
	ID        string   `json:"id"`
	Methods   []string `json:"methods"`
	ExpiresIn int      `json:"expiresIn"` // 秒
}

// LoginOutcome 是登录签发阶段的结果：要么 Pair+User（已签发令牌），要么 Challenge（需二次验证，没有令牌）。
type LoginOutcome struct {
	Pair      Pair
	User      *User
	Challenge *Challenge
}

// Payload 返回对外的响应体：已签发时与原 /auth/login 完全一致，需二次验证时为 {challenge}。
func (o *LoginOutcome) Payload() any {
	if o.Challenge != nil {
		return struct {
			Challenge *Challenge `json:"challenge"`
		}{o.Challenge}
	}
	return loginResult{Pair: o.Pair, User: normalize(o.User)}
}

// SetChallengeLimits 调整挑战的有效期与最大尝试次数（<=0 保持默认）。
func (s *Service) SetChallengeLimits(ttl time.Duration, attempts int) {
	if s == nil {
		return
	}
	s.x.mu.Lock()
	s.chTTL, s.chTries = ttl, attempts
	s.x.mu.Unlock()
}

func (s *Service) limits() (time.Duration, int) {
	s.x.mu.RLock()
	defer s.x.mu.RUnlock()
	ttl, n := s.chTTL, s.chTries
	if ttl <= 0 {
		ttl = DefaultChallengeTTL
	}
	if n <= 0 {
		n = DefaultChallengeAttempts
	}
	return ttl, n
}

func errChallenge() *response.Error { return response.BadRequest("auth.challenge_invalid") }
func errVerify() *response.Error    { return response.BadRequest("auth.verify_failed") }

type challengeRec struct {
	ExternalSession ExternalSession `json:"ext,omitempty"`
	UID             string          `json:"u"`
	TID             string          `json:"t"`
	Username        string          `json:"n"`
	Method          string          `json:"m"`
	Gate            string          `json:"g"`
	IP              string          `json:"i"`
	Exp             int64           `json:"e"`
}

// 缓存里只放挑战 ID 的哈希，明文只在响应里出现一次。
func challengeKey(id string) string {
	h := sha256.Sum256([]byte(id))
	return cache.GlobalKey("auth", "challenge", hex.EncodeToString(h[:]))
}

func triesKey(id string) string {
	h := sha256.Sum256([]byte(id))
	return cache.GlobalKey("auth", "challenge-tries", hex.EncodeToString(h[:]))
}

// FinishLogin 是所有“凭据已通过 -> 签发令牌”路径的唯一出口：租户校验 -> 二次验证门 -> 令牌签发。
// 没有注册 LoginGate 时等价于原来的 CheckTenant + Decorate + IssuePair。刷新令牌（Refresh）不经过这里，也不会触发门。
func (s *Service) FinishLogin(ctx context.Context, u *User, meta LoginMeta) (*LoginOutcome, error) {
	if err := s.CheckTenant(ctx, u); err != nil {
		return nil, err
	}
	return s.passGates(ctx, u, meta, 0)
}

func (s *Service) passGates(ctx context.Context, u *User, meta LoginMeta, from int) (*LoginOutcome, error) {
	if err := s.checkExternalSession(ctx, externalSessionFrom(ctx)); err != nil {
		return nil, err
	}
	gates := s.gateList()
	for i := from; i < len(gates); i++ {
		req, err := gates[i].Check(ctx, GateInput{User: u, Method: meta.Method, IP: meta.IP, UserAgent: meta.UserAgent})
		if err != nil { // 失败即关闭：门出错不能放行
			return nil, response.Internal().Wrap(err)
		}
		if req == nil {
			continue
		}
		ch, err := s.newChallenge(ctx, u, meta, gates[i].Name(), req)
		if err != nil {
			return nil, err
		}
		return &LoginOutcome{Challenge: ch}, nil
	}
	s.Decorate(ctx, u)
	pair, err := s.issuePair(ctx, u, "")
	if err != nil {
		return nil, err
	}
	return &LoginOutcome{Pair: pair, User: u}, nil
}

func (s *Service) newChallenge(ctx context.Context, u *User, meta LoginMeta, gate string, req *GateRequirement) (*Challenge, error) {
	if s.cache == nil {
		return nil, response.Internal().Wrap(errors.New("auth: cache required for second-factor challenge"))
	}
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return nil, err
	}
	id := hex.EncodeToString(b)
	ttl, _ := s.limits()
	rec := challengeRec{UID: u.ID.String(), TID: u.TenantID.String(), Username: u.Username, Method: meta.Method, Gate: gate, IP: meta.IP, Exp: time.Now().Add(ttl).Unix()}
	rec.ExternalSession = externalSessionFrom(ctx)
	raw, _ := json.Marshal(rec)
	if err := s.cache.Set(ctx, challengeKey(id), string(raw), ttl); err != nil {
		return nil, err
	}
	methods := append([]string{}, req.Methods...)
	return &Challenge{ID: id, Methods: methods, ExpiresIn: int(ttl.Seconds())}, nil
}

var fallbackMu sync.Mutex

// bump 原子计数（cache.Counter）；底层不支持时回落为进程内加锁的读改写（多实例下不精确，所以内置缓存都实现了 Counter）。
func (s *Service) bump(ctx context.Context, key string, ttl time.Duration) (int64, error) {
	if c, ok := cache.As[cache.Counter](s.cache); ok {
		n, _, err := c.Incr(ctx, key, ttl)
		return n, err
	}
	fallbackMu.Lock()
	defer fallbackMu.Unlock()
	var n int64
	if v, err := s.cache.Get(ctx, key); err == nil {
		n, _ = strconv.ParseInt(v, 10, 64)
	} else if !errors.Is(err, cache.ErrNotFound) {
		return 0, err
	}
	n++
	return n, s.cache.Set(ctx, key, strconv.FormatInt(n, 10), ttl)
}

func (s *Service) dropChallenge(ctx context.Context, id string) {
	_ = s.cache.Del(ctx, challengeKey(id), triesKey(id))
}
