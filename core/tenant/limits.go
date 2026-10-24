package tenant

import (
	"context"
	"fmt"
	"math"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"nexadmin/core/response"
	"nexadmin/pkg/idgen"
)

// 租户被拒绝时响应里的 reason（前端据此提示）。
const (
	ReasonExpired      = "TENANT_EXPIRED"       // 租户已过期或已停用
	ReasonQuotaUsers   = "TENANT_QUOTA_USERS"   // 用户数超过额度
	ReasonQuotaRoles   = "TENANT_QUOTA_ROLES"   // 角色数超过额度
	ReasonQuotaStorage = "TENANT_QUOTA_STORAGE" // 存储超过额度
	ReasonAIDisabled   = "TENANT_AI_DISABLED"   // 租户被关闭 AI
	ReasonQuotaAI      = "TENANT_QUOTA_AI"      // 租户今日 AI 调用次数用完
)

// 租户状态取值（与租户模块一致；core 不依赖模块，这里只放判断要用的常量）。
const (
	StatusSuspended = "suspended"
	StatusExpired   = "expired"
)

// Info 是租户模块提供给 core 的“强制执行所需信息”。额度 0 = 不限。
type Info struct {
	ID           idgen.ID
	Name         string
	Status       string
	ExpireAt     time.Time // 零值 = 永不过期
	MaxUsers     int
	MaxRoles     int
	MaxStorageMB int64
	// AI 开关与每日调用配额（由 AI 网关执行）。用“禁用”而不是“启用”，零值 = 不限制，旧的 Source 实现不用改。
	AIDisabled   bool
	AIDailyQuota int        // 每日调用次数上限，0 = 不限
	MenuIDs      []idgen.ID // 套餐菜单；nil/空 = 不限制菜单
	GraceDays    int        // 过期后的宽限天数（tenant.grace_days，默认 0）
	WarnDays     int        // 到期前几天开始提醒（tenant.expire_warn_days，默认 15）
}

// Source 由租户模块实现并通过 SetSource 注册。
// TenantInfo 找不到租户记录（如默认租户）返回 (nil, nil)，表示“不受管”，一律放行。
type Source interface {
	TenantInfo(ctx context.Context, id idgen.ID) (*Info, error)
	TenantByDomain(ctx context.Context, host string) (idgen.ID, bool, error)
}

var (
	srcMu sync.RWMutex
	src   Source

	// now / infoTTL 仅为测试可替换。
	now     = time.Now
	infoTTL = 30 * time.Second

	cacheMu     sync.Mutex
	infoCache   = map[idgen.ID]infoEntry{}
	domainCache = map[string]domainEntry{}
)

type infoEntry struct {
	info *Info
	exp  time.Time
}

type domainEntry struct {
	id  idgen.ID
	ok  bool
	exp time.Time
}

// SetSource 注册租户信息来源（nil = 取消）。重复注册会清空缓存。
func SetSource(s Source) {
	srcMu.Lock()
	src = s
	srcMu.Unlock()
	ResetCache()
}

func source() Source {
	srcMu.RLock()
	defer srcMu.RUnlock()
	return src
}

// ResetCache 清空租户信息与域名映射缓存。
func ResetCache() {
	cacheMu.Lock()
	infoCache = map[idgen.ID]infoEntry{}
	domainCache = map[string]domainEntry{}
	cacheMu.Unlock()
}

// Broadcaster 把“租户变更”通知到其它实例（由 core 装配时通过 SetBroadcaster 注入，core/tenant 不依赖缓存）。
// all 为 true 表示全部租户都要失效（ids 忽略）。实现必须不阻塞、不回调本包。
type Broadcaster func(ids []idgen.ID, all bool)

var broadcaster atomic.Pointer[Broadcaster]

// SetBroadcaster 设置跨实例失效通知函数；nil = 取消（单实例或测试）。
func SetBroadcaster(b Broadcaster) {
	if b == nil {
		broadcaster.Store(nil)
		return
	}
	broadcaster.Store(&b)
}

func broadcast(ids []idgen.ID, all bool) {
	if b := broadcaster.Load(); b != nil {
		(*b)(ids, all)
	}
}

// Invalidate 让某些租户的缓存立即失效（租户被修改、续期、停用后调用）；域名映射一并清空。
// 多实例部署时通过 SetBroadcaster 注入的广播通道通知其它实例同样失效；广播丢失时其它实例最多延迟 infoTTL（30 秒）生效。
func Invalidate(ids ...idgen.ID) {
	InvalidateLocal(ids...)
	broadcast(ids, false)
}

// InvalidateAll 清空全部租户缓存并通知其它实例（套餐额度 / 菜单变化影响一批租户时用）。
func InvalidateAll() {
	ResetCache()
	broadcast(nil, true)
}

// InvalidateLocal 只清本实例的缓存，不广播。收到其它实例的失效通知时调用它，避免回环。
func InvalidateLocal(ids ...idgen.ID) {
	cacheMu.Lock()
	for _, id := range ids {
		delete(infoCache, id)
	}
	domainCache = map[string]domainEntry{}
	cacheMu.Unlock()
}

// Lookup 返回租户的强制执行信息（短缓存）。未注册来源或租户不受管返回 (nil, nil)。
// 来源出错时返回错误且不缓存：调用方应拒绝放行（宁可误拒也不漏拦）。
func Lookup(ctx context.Context, id idgen.ID) (*Info, error) {
	s := source()
	if s == nil || id == 0 {
		return nil, nil
	}
	t := now()
	cacheMu.Lock()
	if e, ok := infoCache[id]; ok && t.Before(e.exp) {
		cacheMu.Unlock()
		return e.info, nil
	}
	cacheMu.Unlock()
	info, err := s.TenantInfo(ctx, id)
	if err != nil {
		return nil, err
	}
	cacheMu.Lock()
	infoCache[id] = infoEntry{info: info, exp: t.Add(infoTTL)}
	cacheMu.Unlock()
	return info, nil
}

// DefaultDomainLookup 是 core 默认装配的 DomainLookup：按租户 domain 字段查（大小写不敏感，带短缓存）。
// 没有注册来源或查不到都返回 false，解析链继续走下一级。
func DefaultDomainLookup(host string) (idgen.ID, bool) {
	s := source()
	host = strings.TrimSuffix(strings.ToLower(strings.TrimSpace(host)), ".")
	if s == nil || host == "" {
		return 0, false
	}
	t := now()
	cacheMu.Lock()
	if e, ok := domainCache[host]; ok && t.Before(e.exp) {
		cacheMu.Unlock()
		return e.id, e.ok
	}
	cacheMu.Unlock()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	id, ok, err := s.TenantByDomain(ctx, host)
	if err != nil {
		return 0, false // 查询失败不缓存，本次回退到下一级解析
	}
	cacheMu.Lock()
	domainCache[host] = domainEntry{id: id, ok: ok, exp: t.Add(infoTTL)}
	cacheMu.Unlock()
	return id, ok
}

// State 是租户在某一时刻的可用状态。
type State int

const (
	StateActive    State = iota // 正常
	StateGrace                  // 已过期但仍在宽限期内（放行）
	StateExpired                // 已过期（或状态为 expired），拒绝
	StateSuspended              // 已停用，拒绝
)

// Allowed 报告该状态是否放行。
func (s State) Allowed() bool { return s == StateActive || s == StateGrace }

const day = 24 * time.Hour

// StateAt 计算 at 时刻的状态。info 为 nil（不受管）视为正常。
func (i *Info) StateAt(at time.Time) State {
	if i == nil {
		return StateActive
	}
	switch i.Status {
	case StatusSuspended:
		return StateSuspended
	case StatusExpired:
		return StateExpired
	}
	if i.ExpireAt.IsZero() || !at.After(i.ExpireAt) {
		return StateActive
	}
	if i.GraceDays > 0 && !at.After(i.ExpireAt.Add(time.Duration(i.GraceDays)*day)) {
		return StateGrace
	}
	return StateExpired
}

// DaysLeft 返回距到期的天数（向上取整；已过期为 0 或负数）。ExpireAt 为零值返回 (0, false)。
func (i *Info) DaysLeft(at time.Time) (int, bool) {
	if i == nil || i.ExpireAt.IsZero() {
		return 0, false
	}
	d := i.ExpireAt.Sub(at)
	n := int(d / day)
	if d > 0 && d%day != 0 {
		n++
	}
	return n, true
}

// ExpireSoon 报告租户是否进入“即将到期”提醒窗口：未被停用、未超出宽限期，且距到期不超过 WarnDays 天。
// WarnDays <= 0 表示关闭提醒。宽限期内也提醒（daysLeft 为 0 或负数）。
func (i *Info) ExpireSoon(at time.Time) (int, bool) {
	if i == nil || i.WarnDays <= 0 || !i.StateAt(at).Allowed() {
		return 0, false
	}
	n, ok := i.DaysLeft(at)
	if !ok || n > i.WarnDays {
		return 0, false
	}
	return n, true
}

func expiredErr(s State) *response.Error {
	key := "tenant.expired"
	if s == StateSuspended {
		key = "tenant.suspended"
	}
	e := response.Forbidden(key)
	e.Reason = ReasonExpired
	return e
}

// CheckActive 检查租户是否可用；过期或停用返回 403 + reason TENANT_EXPIRED。
// 调用方负责对平台超管豁免（不要对超管调用）。租户不受管（如默认租户）放行。
func CheckActive(ctx context.Context, id idgen.ID) error {
	info, err := Lookup(ctx, id)
	if err != nil {
		return err
	}
	if s := info.StateAt(now()); !s.Allowed() {
		return expiredErr(s)
	}
	return nil
}

// ---- 额度检查：租户取自 ctx；跳过隔离的 ctx（平台级操作）不受额度限制 ----

// QuotaInfo 返回 ctx 所在租户的额度信息；跳过隔离、无租户、租户不受管时返回 (nil, nil)。
// 调用方可据此先判断“有没有额度限制”，没有就省掉用量统计查询。
func QuotaInfo(ctx context.Context) (*Info, error) {
	if Skipped(ctx) {
		return nil, nil
	}
	id, ok := From(ctx)
	if !ok {
		return nil, nil
	}
	return Lookup(ctx, id)
}

func quotaErr(key, reason string, args ...any) *response.Error {
	e := response.Forbidden(key, args...)
	e.Reason = reason
	return e
}

// CheckUserQuota 在新增 adding 个用户前调用，current 是当前租户已有用户数。
func CheckUserQuota(ctx context.Context, current int64, adding int) error {
	info, err := QuotaInfo(ctx)
	if err != nil || info == nil || info.MaxUsers <= 0 {
		return err
	}
	if current+int64(adding) > int64(info.MaxUsers) {
		return quotaErr("tenant.quota_users", ReasonQuotaUsers, info.MaxUsers)
	}
	return nil
}

// CheckRoleQuota 在新增 adding 个角色前调用，current 是当前租户已有角色数。
func CheckRoleQuota(ctx context.Context, current int64, adding int) error {
	info, err := QuotaInfo(ctx)
	if err != nil || info == nil || info.MaxRoles <= 0 {
		return err
	}
	if current+int64(adding) > int64(info.MaxRoles) {
		return quotaErr("tenant.quota_roles", ReasonQuotaRoles, info.MaxRoles)
	}
	return nil
}

// CheckStorageQuota 在新增 addBytes 字节存储前调用，usedBytes 是当前租户已用字节数。
func CheckStorageQuota(ctx context.Context, usedBytes, addBytes int64) error {
	if usedBytes < 0 || addBytes < 0 || usedBytes > math.MaxInt64-addBytes {
		return fmt.Errorf("tenant: invalid storage byte count")
	}
	info, err := QuotaInfo(ctx)
	if err != nil || info == nil || info.MaxStorageMB == 0 {
		return err
	}
	if info.MaxStorageMB < 0 || info.MaxStorageMB > math.MaxInt64/(1024*1024) {
		return fmt.Errorf("tenant: storage quota overflows bytes")
	}
	if usedBytes+addBytes > info.MaxStorageMB*1024*1024 {
		return quotaErr("tenant.quota_storage", ReasonQuotaStorage, info.MaxStorageMB)
	}
	return nil
}

// ---- 套餐菜单 ----

// MenuNode 是套餐菜单计算所需的菜单最小信息。
type MenuNode struct {
	ID       idgen.ID
	ParentID idgen.ID
	Type     string // dir | menu | button
	Perm     string
}

// platformPermPrefixes 是不受套餐菜单限制的平台级权限前缀：工作台、个人中心、关于、消息、导出中心。
var platformPermPrefixes = []string{"dashboard:", "profile:", "about:", "message:", "job:"}

// PlatformPerm 报告权限码是否属于平台级（不受套餐菜单限制）。
func PlatformPerm(perm string) bool {
	for _, p := range platformPermPrefixes {
		if strings.HasPrefix(perm, p) {
			return true
		}
	}
	return false
}

// AllowedPerms 计算套餐允许的权限码集合：套餐勾选的菜单/按钮，加上“已勾选菜单下的按钮”，加上平台级权限。
// nodes 应只包含启用的菜单。
func AllowedPerms(menuIDs []idgen.ID, nodes []MenuNode) map[string]bool {
	chosen := make(map[idgen.ID]bool, len(menuIDs))
	for _, id := range menuIDs {
		chosen[id] = true
	}
	out := map[string]bool{}
	for _, n := range nodes {
		if n.Perm == "" {
			continue
		}
		if PlatformPerm(n.Perm) || chosen[n.ID] || (n.Type == "button" && chosen[n.ParentID]) {
			out[n.Perm] = true
		}
	}
	return out
}

// IntersectPerms 返回 perms 与 allowed 的交集。perms 含 "*" 时展开为 allowed 的全部权限码（排序稳定由调用方保证无关）。
// 形如 "user:*" 的前缀通配只保留 allowed 中匹配的具体权限码。
func IntersectPerms(perms []string, allowed map[string]bool) []string {
	out := []string{}
	seen := map[string]bool{}
	add := func(p string) {
		if !seen[p] {
			seen[p] = true
			out = append(out, p)
		}
	}
	for _, p := range perms {
		switch {
		case p == "*":
			for a := range allowed {
				add(a)
			}
		case strings.HasSuffix(p, ":*"):
			prefix := strings.TrimSuffix(p, "*")
			for a := range allowed {
				if strings.HasPrefix(a, prefix) {
					add(a)
				}
			}
		case allowed[p]:
			add(p)
		}
	}
	sort.Strings(out)
	return out
}
