package auth

import (
	"context"
	"sync"

	"github.com/golang-jwt/jwt/v5"
)

// SessionTracker 在登录（含二次验证后的签发）与刷新签发令牌后回调，Claims 已带 SessionBinding。
// 插件据此记录轮换后的令牌 ID，供外部 IdP 发起的单点登出吊销本地会话。
type SessionTracker func(ctx context.Context, cl *Claims)

var sessionTrackers struct {
	mu  sync.RWMutex
	fns []SessionTracker
}

// RegisterSessionTracker 注册会话回调；插件在 Start 阶段调用一次。
func RegisterSessionTracker(fn SessionTracker) {
	if fn == nil {
		return
	}
	sessionTrackers.mu.Lock()
	defer sessionTrackers.mu.Unlock()
	sessionTrackers.fns = append(sessionTrackers.fns, fn)
}

// notifySessionTrackers 只对带 SessionBinding 的令牌回调，普通本地登录不触发。
func (s *Service) notifySessionTrackers(ctx context.Context, pair Pair) {
	if pair.AccessToken == "" {
		return
	}
	cl, err := s.tokenClaims(pair.AccessToken)
	if err != nil || cl == nil || cl.SessionBinding == "" {
		return
	}
	sessionTrackers.mu.RLock()
	fns := append([]SessionTracker(nil), sessionTrackers.fns...)
	sessionTrackers.mu.RUnlock()
	for _, fn := range fns {
		fn(ctx, cl)
	}
}

// tokenClaims 只验签不查吊销缓存：刚签发的令牌不可能在吊销表里。
func (s *Service) tokenClaims(token string) (*Claims, error) {
	var cl Claims
	t, err := jwt.ParseWithClaims(token, &cl, func(*jwt.Token) (any, error) { return s.secret, nil },
		jwt.WithValidMethods([]string{"HS256"}))
	if err != nil || !t.Valid {
		return nil, ErrInvalidToken
	}
	return &cl, nil
}
