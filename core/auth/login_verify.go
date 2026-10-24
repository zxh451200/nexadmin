package auth

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"nexadmin/core/cache"
	"nexadmin/core/response"
	"nexadmin/core/tenant"
	"nexadmin/pkg/idgen"
)

func (s *Service) gateByName(name string) (int, LoginGate) {
	for i, g := range s.gateList() {
		if g.Name() == name {
			return i, g
		}
	}
	return -1, nil
}

// CompleteLogin 用二次验证结果换令牌（POST /auth/login/verify 的实现；插件自己挂路由时也可直接调用）。
// 挑战一次性、绑定用户与 IP、有 TTL 与尝试次数上限（多实例下用缓存原子计数）。任何失败对外都是同一类 400，不区分原因。
func (s *Service) CompleteLogin(ctx context.Context, challengeID string, meta LoginMeta, ans GateAnswer) (*LoginOutcome, error) {
	if s == nil || s.cache == nil {
		return nil, errChallenge()
	}
	if err := s.PreLogin(ctx, meta.IP, "", meta.UserAgent); err != nil {
		return nil, err
	}
	if len(challengeID) != 64 {
		return nil, errChallenge()
	}
	raw, err := s.cache.Get(ctx, challengeKey(challengeID))
	if errors.Is(err, cache.ErrNotFound) {
		return nil, errChallenge()
	}
	if err != nil {
		return nil, err
	}
	var rec challengeRec
	if json.Unmarshal([]byte(raw), &rec) != nil {
		s.dropChallenge(ctx, challengeID)
		return nil, errChallenge()
	}
	if rec.IP != meta.IP { // 换 IP：挑战作废
		s.dropChallenge(ctx, challengeID)
		s.record(ctx, LoginEvent{Username: rec.Username, Method: rec.Gate, IP: meta.IP, UserAgent: meta.UserAgent, Reason: FailOther})
		return nil, errChallenge()
	}
	if err := s.checkExternalSession(ctx, rec.ExternalSession); err != nil {
		return nil, err
	}
	ctx = WithExternalSession(ctx, rec.ExternalSession)
	ttl, maxTries := s.limits()
	left := time.Until(time.Unix(rec.Exp, 0))
	if left <= 0 {
		left = time.Second
	} else if left > ttl {
		left = ttl
	}
	n, err := s.bump(ctx, triesKey(challengeID), left) // 先计数再校验：并发猜测也逃不掉上限
	if err != nil {
		return nil, err
	}
	if n > int64(maxTries) {
		s.dropChallenge(ctx, challengeID)
		return nil, errChallenge()
	}
	idx, gate := s.gateByName(rec.Gate)
	if gate == nil {
		s.dropChallenge(ctx, challengeID)
		return nil, errChallenge()
	}
	uid, err1 := idgen.Parse(rec.UID)
	tid, err2 := idgen.Parse(rec.TID)
	if err1 != nil || err2 != nil {
		s.dropChallenge(ctx, challengeID)
		return nil, errChallenge()
	}
	vctx := ctx
	if tid != 0 {
		vctx = tenant.With(ctx, tid)
	}
	ch := GateChallenge{UserID: uid, TenantID: tid, Username: rec.Username, Method: rec.Method, IP: rec.IP}
	if verr := gate.Verify(vctx, ch, ans); verr != nil {
		s.record(vctx, LoginEvent{Username: rec.Username, Method: rec.Gate, IP: meta.IP, UserAgent: meta.UserAgent, Reason: FailCredentials})
		if n >= int64(maxTries) {
			s.dropChallenge(ctx, challengeID)
		}
		var re *response.Error
		if errors.As(verr, &re) && re.Status < 500 {
			return nil, re
		}
		return nil, errVerify()
	}
	// 校验通过：原子消费挑战，并发的第二个提交拿不到
	if _, err := cache.GetDel(s.cache, ctx, challengeKey(challengeID)); err != nil {
		if errors.Is(err, cache.ErrNotFound) {
			return nil, errChallenge()
		}
		return nil, err
	}
	_ = s.cache.Del(ctx, triesKey(challengeID))
	p := s.Provider()
	if p == nil {
		return nil, ErrNoProvider
	}
	u, err := p.Profile(vctx, uid) // 重新读取用户：期间被停用 / 删除则失败
	if err != nil {
		return nil, err
	}
	if u == nil || u.ID != uid {
		return nil, errChallenge()
	}
	if err := s.CheckTenant(vctx, u); err != nil {
		return nil, err
	}
	out, err := s.passGates(vctx, u, LoginMeta{Method: rec.Method, IP: meta.IP, UserAgent: meta.UserAgent}, idx+1)
	if err != nil {
		return nil, err
	}
	if out.Challenge == nil {
		s.record(vctx, LoginEvent{Username: u.Username, Method: rec.Gate, IP: meta.IP, UserAgent: meta.UserAgent})
	}
	return out, nil
}

// LoginAsRequest 是外部登录会话入口的入参（CAS 之类：身份已由插件验证，不需要密码）。
type LoginAsRequest struct {
	Identity  ExternalIdentity
	Method    string // 登录方式，如 "cas"；规则同 ExternalAuthenticator.Name，不能用内置方式名
	IP        string
	UserAgent string
}

// LoginAs 为“已验证的外部身份”签发登录态：前置检查 -> 映射 / 创建本地用户（遵守额度）-> 租户校验 -> 二次验证门 -> 令牌，
// 并写登录日志（login_type = Method）。调用方必须已经真正验证过该身份，core 不再校验。
func (s *Service) LoginAs(ctx context.Context, req LoginAsRequest) (*LoginOutcome, error) {
	if s == nil {
		return nil, ErrNoProvider
	}
	if !methodNameRe.MatchString(req.Method) || reservedMethod(req.Method) {
		return nil, response.BadRequest("error.bad_request").Wrap(errors.New("auth: invalid LoginAs method"))
	}
	name := req.Identity.Username
	if err := s.PreLogin(ctx, req.IP, name, req.UserAgent); err != nil {
		return nil, err
	}
	fail := func(err error) (*LoginOutcome, error) {
		s.record(ctx, LoginEvent{Username: name, Method: req.Method, IP: req.IP, UserAgent: req.UserAgent, Reason: FailOther})
		return nil, err
	}
	u, err := s.ResolveExternal(ctx, req.Method, req.Identity)
	if err != nil {
		return fail(err)
	}
	out, err := s.FinishLogin(ctx, u, LoginMeta{Method: req.Method, IP: req.IP, UserAgent: req.UserAgent})
	if err != nil {
		return fail(err)
	}
	s.record(ctx, LoginEvent{Username: u.Username, Method: req.Method, IP: req.IP, UserAgent: req.UserAgent})
	return out, nil
}
