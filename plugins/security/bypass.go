package security

import (
	"context"
	"os"
	"strings"

	"go.uber.org/zap"

	"nexadmin/core/auth"
)

// IP 规则应急恢复（防止白名单误配把所有人包括超管锁在外面）。两条互相独立的途径：
//
//  1. 本机直连豁免：TCP 对端（RemoteAddr，不是 c.ClientIP()）是回环地址、且请求没有任何转发头时，
//     不受 IP 黑白名单约束（登录前置检查、超管门、刷新复检都豁免）。在服务器上 curl 127.0.0.1 登录即可进后台修规则。
//     转发头无法让远程请求冒充本机：带 X-Forwarded-For 的请求一律不算“直连”，同机 nginx 转发的请求也不算。
//     环境变量 NEXADMIN_SECURITY_LOOPBACK_EXEMPT=0 可关闭（默认开启）。
//  2. 整体关闭：NEXADMIN_SECURITY_IP_RULES_DISABLED=1 在进程启动时让 IP 规则完全不生效（规则数据保留），
//     启动日志里有 ERROR 级告警，修好规则后去掉变量重启。2FA 不受影响。
const (
	envIPRulesDisabled = "NEXADMIN_SECURITY_IP_RULES_DISABLED"
	envLoopbackExempt  = "NEXADMIN_SECURITY_LOOPBACK_EXEMPT"
)

// envBool 解析布尔环境变量：1/true/yes/on 为真，0/false/no/off 为假，空或其它取 def。
func envBool(getenv func(string) string, name string, def bool) bool {
	switch strings.ToLower(strings.TrimSpace(getenv(name))) {
	case "1", "true", "yes", "on":
		return true
	case "0", "false", "no", "off":
		return false
	}
	return def
}

// loadBypass 读取应急开关（进程启动时读一次）。
func (s *Service) loadBypass(getenv func(string) string) {
	if getenv == nil {
		getenv = os.Getenv
	}
	s.ipRulesDisabled = envBool(getenv, envIPRulesDisabled, false)
	s.loopbackExempt = envBool(getenv, envLoopbackExempt, true)
}

// announceBypass 在启动时把应急开关状态写进日志；整体关闭时是 ERROR 级，保证运维看得见。
func (s *Service) announceBypass() {
	if s.ipRulesDisabled {
		s.log.Error("security: !!! IP RULES ARE DISABLED (" + envIPRulesDisabled + ") - IP allow/deny lists are NOT enforced. " +
			"Fix the rules, then unset the variable and restart !!!")
	}
	if !s.loopbackExempt {
		s.log.Warn("security: loopback exemption is off (" + envLoopbackExempt + "=0): local requests are subject to IP rules too")
	}
}

// ipRulesBypassed 报告本次请求是否不受 IP 规则约束：整体关闭，或本机直连（见上）。
func (s *Service) ipRulesBypassed(ctx context.Context) bool {
	if s.ipRulesDisabled {
		return true
	}
	if !s.loopbackExempt {
		return false
	}
	ri, ok := auth.RequestInfoFrom(ctx)
	return ok && ri.DirectLoopback()
}

// refreshCheck 是 auth.RefreshCheckFunc：刷新令牌时复检 IP 规则（登录后才新增的 deny / 白名单变更在此生效）。
// 读不到规则就拒绝（失败即关闭）；被拒时 core 会吊销该刷新令牌。
func (s *Service) refreshCheck(ctx context.Context, u *auth.User, ri auth.RequestInfo) error {
	if s.ipRulesDisabled || (s.loopbackExempt && ri.DirectLoopback()) {
		return nil
	}
	rules, err := s.rulesFor(ctx, u.TenantID)
	if err != nil {
		return err
	}
	if !allowLogin(rules, ri.IP, u.Super) {
		s.log.Warn("security: token refresh denied by ip rule", zap.String("user", u.Username), zap.String("ip", ri.IP))
		return errIPDenied
	}
	return nil
}
