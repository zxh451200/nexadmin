package security

import (
	"context"
	"encoding/json"
	"regexp"
	"strings"
	"time"

	"go.uber.org/zap"

	"nexadmin/core/auth"
	"nexadmin/core/cluster"
	"nexadmin/core/response"
	"nexadmin/pkg/idgen"
)

var roleCodeRe = regexp.MustCompile(`^[A-Za-z0-9_.:-]{1,64}$`)

// ---- 策略 ----

// PolicyForm 是策略的读写形态。
type PolicyForm struct {
	Mode      string   `json:"mode"`
	Roles     []string `json:"roles"`
	GraceDays int      `json:"graceDays"`
}

func (s *Service) GetPolicy(ctx context.Context, tid idgen.ID) (*PolicyForm, error) {
	p, err := s.policy(ctx, tid)
	if err != nil {
		return nil, err
	}
	roles := splitRoles(p.Roles)
	if roles == nil {
		roles = []string{}
	}
	return &PolicyForm{Mode: p.Mode, Roles: roles, GraceDays: p.GraceDays}, nil
}

func (s *Service) SavePolicy(ctx context.Context, op User, f PolicyForm) (*PolicyForm, error) {
	switch f.Mode {
	case ModeOptional, ModeAll, ModeRoles:
	default:
		return nil, badReq("security.mode_invalid")
	}
	if f.GraceDays < minGrace || f.GraceDays > maxGrace {
		return nil, badReq("security.grace_days_invalid")
	}
	seen := map[string]bool{}
	var roles []string
	for _, r := range f.Roles {
		r = strings.TrimSpace(r)
		if !roleCodeRe.MatchString(r) {
			return nil, badReq("security.role_code_invalid")
		}
		if !seen[r] {
			seen[r] = true
			roles = append(roles, r)
		}
	}
	if f.Mode == ModeRoles && len(roles) == 0 {
		return nil, badReq("security.roles_required")
	}
	if len(roles) > 50 {
		return nil, badReq("security.roles_too_many")
	}
	if f.Mode != ModeRoles {
		roles = nil
	}
	p := &Policy{Mode: f.Mode, Roles: strings.Join(roles, ","), GraceDays: f.GraceDays, UpdatedBy: clip(op.Name, 100)}
	if err := s.st.SavePolicy(ctx, op.TID, p); err != nil {
		return nil, err
	}
	s.audit(ctx, op, "save_policy", f.Mode, "roles="+p.Roles+" graceDays="+itoa(f.GraceDays))
	return s.GetPolicy(ctx, op.TID)
}

// ---- 用户 2FA 列表 / 重置 ----

// UserItem 是管理员看到的用户 2FA 状态。
type UserItem struct {
	UserID     idgen.ID   `json:"userId"`
	Username   string     `json:"username"`
	Status     string     `json:"status"` // enabled | pending | grace | expired
	EnabledAt  *time.Time `json:"enabledAt"`
	GraceUntil *time.Time `json:"graceUntil"`
}

func (s *Service) ListUsers(ctx context.Context, tid idgen.ID, keyword string, page, size int) ([]UserItem, int64, error) {
	rows, total, err := s.st.ListTOTP(ctx, tid, keyword, page, size)
	if err != nil {
		return nil, 0, err
	}
	pol, err := s.policy(ctx, tid)
	if err != nil {
		return nil, 0, err
	}
	now := s.now()
	out := make([]UserItem, 0, len(rows))
	for i := range rows {
		r := &rows[i]
		it := UserItem{UserID: r.UserID, Username: r.Username, EnabledAt: r.EnabledAt, GraceUntil: r.GraceUntil}
		switch {
		case r.Enabled:
			it.Status = "enabled"
		case now.Before(pol.graceDeadline(r)):
			it.Status = "grace"
			if r.SecretEnc != "" {
				it.Status = "pending"
			}
		default:
			it.Status = "expired"
		}
		out = append(out, it)
	}
	return out, total, nil
}

// ResetUser 清除某用户的 2FA（密钥、恢复码、宽限记录）。用户下次登录时：策略要求则获得一个全新的宽限期，否则无需 2FA。
// 重置与操作日志在同一事务里，日志写不进去就不重置。
func (s *Service) ResetUser(ctx context.Context, op User, uid idgen.ID) error {
	if err := requireSession(op); err != nil {
		return err
	}
	row, err := s.st.GetTOTP(ctx, op.TID, uid)
	if err != nil {
		return err
	}
	if row == nil {
		return response.NotFound("error.not_found")
	}
	// 超管的 2FA 只有超管能重置：否则拿到 security:reset 的普通管理员可以先摘掉超管的第二因子再攻击密码。
	// 查不到目标用户身份时失败即关闭。拒绝也写审计。
	if !op.Super {
		target, err := s.targetUser(ctx, uid)
		if err != nil {
			return err
		}
		if target.Super {
			s.audit(ctx, op, "reset_2fa_denied", row.Username, "userId="+uid.String()+" target is a super admin")
			return response.Forbidden("security.reset_super_forbidden")
		}
	}
	lg := &LogRow{OperatorID: op.ID, OperatorName: clip(op.Name, 100), Action: "reset_2fa", Target: clip(row.Username, 100),
		Detail: "userId=" + uid.String(), IP: clip(op.IP, 64)}
	if err := s.st.ResetUser(ctx, op.TID, uid, lg); err != nil {
		return err
	}
	s.clearFails(ctx, op.TID, uid) // 同时解除该用户的验证失败锁定
	return nil
}

// targetUser 读取被操作用户的身份；没有可用的查询函数或查不到都返回错误（失败即关闭）。
func (s *Service) targetUser(ctx context.Context, uid idgen.ID) (*auth.User, error) {
	if s.lookup == nil {
		return nil, response.Unavailable("auth.provider_missing")
	}
	u, err := s.lookup(ctx, uid)
	if err != nil {
		return nil, err
	}
	if u == nil {
		return nil, response.NotFound("error.not_found")
	}
	return u, nil
}

func (s *Service) ListLogs(ctx context.Context, tid idgen.ID, page, size int) ([]LogRow, int64, error) {
	return s.st.ListLogs(ctx, tid, page, size)
}

// ---- IP 规则 ----

// RuleForm 是规则的写入形态。
type RuleForm struct {
	Action  string `json:"action"`
	CIDR    string `json:"cidr"`
	Scope   string `json:"scope"`
	Remark  string `json:"remark"`
	Enabled bool   `json:"enabled"`
	// ConfirmLockout=true 表示操作者已知晓“这次变更会让自己当前 IP 被拒”并坚持保存（见 guard）。
	ConfirmLockout bool `json:"confirmLockout"`
}

func viewsOf(rows []IPRule, log *zap.Logger) []ruleView {
	out := make([]ruleView, 0, len(rows))
	for _, r := range rows {
		p, err := normalizeCIDR(r.CIDR)
		if err != nil { // 库里的脏数据：忽略该条（deny 丢失比全员被锁更可恢复），但要留痕
			log.Warn("security: skip invalid ip rule", zap.String("id", r.ID.String()), zap.String("cidr", r.CIDR))
			continue
		}
		out = append(out, ruleView{Action: r.Action, Scope: r.Scope, Prefix: p, Enabled: r.Enabled})
	}
	return out
}

// rulesFor 返回租户的规则视图，带 5 秒进程内缓存；刷新失败时优先沿用旧快照。
func (s *Service) rulesFor(ctx context.Context, tid idgen.ID) ([]ruleView, error) {
	s.rmu.Lock()
	snap, ok := s.rcache[tid]
	s.rmu.Unlock()
	if ok && s.now().Sub(snap.at) < ruleCacheTTL {
		return snap.views, nil
	}
	rows, err := s.st.ListRules(ctx, tid)
	if err != nil {
		if ok {
			s.log.Warn("security: reload ip rules failed, using stale snapshot", zap.Error(err))
			return snap.views, nil
		}
		return nil, err
	}
	v := viewsOf(rows, s.log)
	s.rmu.Lock()
	s.rcache[tid] = ruleSnap{views: v, at: s.now()}
	s.rmu.Unlock()
	return v, nil
}

// dropRuleCache 只清本进程的规则缓存。
func (s *Service) dropRuleCache(tid idgen.ID) {
	s.rmu.Lock()
	delete(s.rcache, tid)
	s.rmu.Unlock()
}

type rulesEvent struct {
	Tenant idgen.ID `json:"tenant"`
}

// invalidateRules 规则变更后清本进程缓存，并广播给其它实例清缓存；Bus 不可用（单实例 / 无共享缓存）时
// 其它实例最多靠 ruleCacheTTL（5 秒）自然过期。
func (s *Service) invalidateRules(tid idgen.ID) {
	s.dropRuleCache(tid)
	s.bus.Publish(cluster.KindSecurityRules, rulesEvent{Tenant: tid})
}

// attachBus 接上跨实例总线：收到别的实例的规则变更广播就清本地缓存。b 为 nil 或非分布式时只剩 5 秒 TTL。
func (s *Service) attachBus(b *cluster.Bus) {
	if b == nil {
		return
	}
	s.bus = b
	b.On(cluster.KindSecurityRules, func(raw json.RawMessage) {
		var ev rulesEvent
		if json.Unmarshal(raw, &ev) == nil {
			s.dropRuleCache(ev.Tenant)
		}
	}, s.dropAllRuleCaches)
}

// dropAllRuleCaches 在事件订阅断线重连后调用：断线期间的失效通知可能已丢。
func (s *Service) dropAllRuleCaches() {
	s.rmu.Lock()
	s.rcache = map[idgen.ID]ruleSnap{}
	s.rmu.Unlock()
}

// ListRules 返回规则与当前操作者看到的客户端 IP。
func (s *Service) ListRules(ctx context.Context, tid idgen.ID) ([]IPRule, error) {
	rows, err := s.st.ListRules(ctx, tid)
	if rows == nil {
		rows = []IPRule{}
	}
	return rows, err
}

func (s *Service) validateRule(f RuleForm) (string, error) {
	if f.Action != ActionAllow && f.Action != ActionDeny {
		return "", badReq("security.action_invalid")
	}
	if f.Scope != ScopeAll && f.Scope != ScopeSuper {
		return "", badReq("security.scope_invalid")
	}
	if len([]rune(f.Remark)) > 200 {
		return "", badReq("security.remark_too_long")
	}
	p, err := normalizeCIDR(f.CIDR)
	if err != nil {
		return "", badReq("security.cidr_invalid")
	}
	return p.String(), nil
}

// guard 是防自锁：把 candidate 当作保存后的完整规则集，如果操作者此刻能登录、保存后却不能，就拒绝。
// 操作者的 IP 取 c.ClientIP()（受 server.trusted_proxies 控制），超管额外受 scope=super 的规则约束。
// 会锁住自己时默认 409 SECURITY_SELF_LOCKOUT；带 confirmLockout=true 二次确认才保存（审计里记 confirm_lockout）。
// 只校验当前操作者：「其它超管最近登录 IP 会不会被拒」需要登录 IP 记录，插件拿不到，所以没做。
func (s *Service) guard(ctx context.Context, op User, current, candidate []IPRule) error {
	if allowLogin(viewsOf(current, s.log), op.IP, op.Super) && !allowLogin(viewsOf(candidate, s.log), op.IP, op.Super) {
		if op.ConfirmLockout { // 已二次确认：放行，但留痕。恢复途径见 bypass.go（本机直连豁免 / 环境变量整体关闭）
			s.log.Warn("security: ip rule change saved with confirmLockout, operator's own ip will be denied",
				zap.String("operator", op.Name), zap.String("ip", op.IP))
			return nil
		}
		e := response.Conflict("security.self_lockout", op.IP)
		e.Reason = "SECURITY_SELF_LOCKOUT"
		return e
	}
	return nil
}

// SaveRule 创建（id==0）或更新规则。
func (s *Service) SaveRule(ctx context.Context, op User, id idgen.ID, f RuleForm) (*IPRule, error) {
	cidr, err := s.validateRule(f)
	if err != nil {
		return nil, err
	}
	cur, err := s.st.ListRules(ctx, op.TID)
	if err != nil {
		return nil, err
	}
	rule := IPRule{Action: f.Action, CIDR: cidr, Scope: f.Scope, Remark: strings.TrimSpace(f.Remark), Enabled: f.Enabled}
	cand := make([]IPRule, 0, len(cur)+1)
	found := false
	for _, r := range cur {
		if r.ID == id && id != 0 {
			rule.ID, rule.TenantID, rule.CreatedAt = r.ID, r.TenantID, r.CreatedAt
			cand = append(cand, rule)
			found = true
			continue
		}
		if r.Action == rule.Action && r.CIDR == rule.CIDR && r.Scope == rule.Scope {
			return nil, response.Conflict("security.rule_exists")
		}
		cand = append(cand, r)
	}
	if id != 0 && !found {
		return nil, response.NotFound("error.not_found")
	}
	if id == 0 {
		if len(cur) >= maxRules {
			return nil, badReq("security.rules_too_many")
		}
		cand = append(cand, rule)
	}
	if err := s.guard(ctx, op, cur, cand); err != nil {
		return nil, err
	}
	if err := s.st.SaveRule(ctx, op.TID, &rule); err != nil {
		return nil, err
	}
	s.invalidateRules(op.TID)
	act := "create_ip_rule"
	if id != 0 {
		act = "update_ip_rule"
	}
	s.audit(ctx, op, act, rule.CIDR, rule.Action+"/"+rule.Scope+" enabled="+boolStr(rule.Enabled)+confirmNote(op))
	return &rule, nil
}

// SetRuleEnabled 启停规则（同样做防自锁校验）。
func (s *Service) SetRuleEnabled(ctx context.Context, op User, id idgen.ID, enabled bool) (*IPRule, error) {
	r, err := s.st.GetRule(ctx, op.TID, id)
	if err != nil {
		return nil, err
	}
	if r == nil {
		return nil, response.NotFound("error.not_found")
	}
	return s.SaveRule(ctx, op, id, RuleForm{Action: r.Action, CIDR: r.CIDR, Scope: r.Scope, Remark: r.Remark, Enabled: enabled})
}

// DeleteRule 删除规则（删除 allow 规则可能让自己被排除在白名单外，同样校验）。
func (s *Service) DeleteRule(ctx context.Context, op User, id idgen.ID) error {
	cur, err := s.st.ListRules(ctx, op.TID)
	if err != nil {
		return err
	}
	var target *IPRule
	cand := make([]IPRule, 0, len(cur))
	for i := range cur {
		if cur[i].ID == id {
			target = &cur[i]
			continue
		}
		cand = append(cand, cur[i])
	}
	if target == nil {
		return response.NotFound("error.not_found")
	}
	if err := s.guard(ctx, op, cur, cand); err != nil {
		return err
	}
	if err := s.st.DeleteRule(ctx, op.TID, id); err != nil {
		return err
	}
	s.invalidateRules(op.TID)
	s.audit(ctx, op, "delete_ip_rule", target.CIDR, target.Action+"/"+target.Scope+confirmNote(op))
	return nil
}

func confirmNote(op User) string {
	if op.ConfirmLockout {
		return " confirm_lockout"
	}
	return ""
}

func boolStr(b bool) string {
	if b {
		return "true"
	}
	return "false"
}

func itoa(n int) string { return idgen.ID(n).String() }
