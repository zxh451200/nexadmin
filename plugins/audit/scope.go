package audit

import (
	"sort"
	"strings"
	"sync"
)

// SelfPrefix 是本插件自己的表前缀，永远不被审计（避免递归）。
const SelfPrefix = "plg_audit_"

// Excluded 报告表是否不允许被审计：插件自己的表、日志类大表、迁移记录表。
func Excluded(table string) bool {
	t := strings.ToLower(table)
	if strings.HasPrefix(t, SelfPrefix) {
		return true
	}
	if strings.HasSuffix(t, "_log") || strings.HasSuffix(t, "_logs") || strings.Contains(t, "_log_") ||
		strings.HasSuffix(t, "_history") || strings.HasSuffix(t, "_histories") {
		return true
	}
	return strings.Contains(t, "migration") || strings.HasPrefix(t, "gorm_")
}

// Available 从库里的全部表名中筛出可选的审计表（去重、排序）。
func Available(all []string) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, len(all))
	for _, t := range all {
		if t == "" || Excluded(t) {
			continue
		}
		if _, dup := seen[t]; dup {
			continue
		}
		seen[t] = struct{}{}
		out = append(out, t)
	}
	sort.Strings(out)
	return out
}

// Scope 是内存里的审计范围：租户 -> 被审计的表。回调只读它，不查库。
type Scope struct {
	mu sync.RWMutex
	m  map[int64]map[string]struct{}
}

func NewScope() *Scope { return &Scope{m: map[int64]map[string]struct{}{}} }

// Audited 报告该租户下的表是否在审计范围内。默认（没配置过）不审计任何表。
func (s *Scope) Audited(tenantID int64, table string) bool {
	if Excluded(table) {
		return false
	}
	s.mu.RLock()
	_, ok := s.m[tenantID][table]
	s.mu.RUnlock()
	return ok
}

// Set 设置一个租户的审计表。
func (s *Scope) Set(tenantID int64, tables []string) {
	set := make(map[string]struct{}, len(tables))
	for _, t := range tables {
		set[t] = struct{}{}
	}
	s.mu.Lock()
	if len(set) == 0 {
		delete(s.m, tenantID)
	} else {
		s.m[tenantID] = set
	}
	s.mu.Unlock()
}

// Replace 整体替换（定时从库刷新，多实例之间靠它收敛）。
func (s *Scope) Replace(all map[int64][]string) {
	m := make(map[int64]map[string]struct{}, len(all))
	for id, tables := range all {
		set := make(map[string]struct{}, len(tables))
		for _, t := range tables {
			set[t] = struct{}{}
		}
		if len(set) > 0 {
			m[id] = set
		}
	}
	s.mu.Lock()
	s.m = m
	s.mu.Unlock()
}

// CleanSelection 校验并规整管理员提交的表清单：去重、排序，只允许 available 里的表。
// 返回的 bad 是不可审计（不存在或被排除）的表名。
func CleanSelection(selected, available []string) (clean, bad []string) {
	ok := make(map[string]struct{}, len(available))
	for _, t := range available {
		ok[t] = struct{}{}
	}
	seen := map[string]struct{}{}
	for _, t := range selected {
		t = strings.TrimSpace(t)
		if t == "" {
			continue
		}
		if _, dup := seen[t]; dup {
			continue
		}
		seen[t] = struct{}{}
		if _, allowed := ok[t]; !allowed {
			bad = append(bad, t)
			continue
		}
		clean = append(clean, t)
	}
	sort.Strings(clean)
	return clean, bad
}
