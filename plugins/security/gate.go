package security

import (
	"context"

	"go.uber.org/zap"

	"nexadmin/core/auth"
	"nexadmin/core/tenant"
)

var _ auth.LoginGate = (*gate)(nil)

// gate 实现 auth.LoginGate：凭据通过后，已启用 2FA 的用户必须完成二次验证才签发令牌。
type gate struct{ s *Service }

func (*gate) Name() string { return gateName }

// Check 的顺序：超管 IP 规则 -> 已启用则要求验证 -> 策略要求但未绑定则看宽限期。
// 返回 error 时 core 会失败即关闭（对外是通用错误，原因进服务端日志）。
func (g *gate) Check(ctx context.Context, in auth.GateInput) (*auth.GateRequirement, error) {
	s, u := g.s, in.User
	if u == nil {
		return nil, errIPDenied
	}
	if u.Super && !s.ipRulesBypassed(ctx) { // scope=super 的规则要知道是不是超管，只能在凭据通过后判断；本机直连 / 应急开关豁免
		rules, err := s.rulesFor(ctx, u.TenantID)
		if err != nil {
			return nil, err
		}
		if !allowLogin(rules, in.IP, true) {
			s.log.Warn("security: super admin login denied by ip rule", zap.String("user", u.Username), zap.String("ip", in.IP))
			return nil, errIPDenied
		}
	}
	row, err := s.st.GetTOTP(ctx, u.TenantID, u.ID)
	if err != nil {
		return nil, err
	}
	if row != nil && row.Enabled {
		return &auth.GateRequirement{Methods: []string{"totp", "recovery"}}, nil
	}
	pol, err := s.policy(ctx, u.TenantID)
	if err != nil {
		return nil, err
	}
	if !pol.requires(u.Roles) {
		return nil, nil
	}
	// 策略要求但还没绑定：宽限期内放行（登录后在插件页绑定）。
	until := s.now().Add(days(pol.GraceDays))
	row, err = s.st.EnsureGrace(ctx, u.TenantID, u.ID, u.Username, until)
	if err != nil {
		return nil, err
	}
	if s.now().Before(pol.graceDeadline(row)) {
		return nil, nil
	}
	if u.Super { // 保底：超管永不因宽限期过期被拦截，否则可能没有人能重置别人
		s.log.Warn("security: super admin has no 2FA and grace expired, allowed", zap.String("user", u.Username))
		return nil, nil
	}
	s.log.Warn("security: login blocked, 2FA binding required and grace expired", zap.String("user", u.Username))
	return nil, errBindRequired
}

// Verify 校验二次验证内容：kind 为空/totp 是 6 位动态码，recovery 是恢复码。
func (g *gate) Verify(ctx context.Context, ch auth.GateChallenge, ans auth.GateAnswer) error {
	return g.s.verifyAny(withAttemptIP(ctx, ch.IP), ch.TenantID, ch.UserID, ans.Kind, ans.Code)
}

// preLogin 是 auth.PreLoginFunc：scope=all 的 IP 黑白名单，对所有登录入口生效。
func (s *Service) preLogin(ctx context.Context, ip, _ string) error {
	if s.ipRulesBypassed(ctx) {
		return nil
	}
	tid := tenant.DefaultID
	if t, ok := tenant.From(ctx); ok {
		tid = t
	}
	rules, err := s.rulesFor(ctx, tid)
	if err != nil {
		return err // 读不到规则就不放行
	}
	if !allowLogin(rules, ip, false) {
		return errIPDenied
	}
	return nil
}
