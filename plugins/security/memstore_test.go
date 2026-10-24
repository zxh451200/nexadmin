package security

import (
	"context"
	"errors"
	"sort"
	"strings"
	"sync"
	"time"

	"nexadmin/pkg/idgen"
)

// memStore 是 store 的内存实现（沙箱没有真实数据库）。语义对齐 gormStore：按租户隔离、恢复码原子消费。
type memStore struct {
	mu       sync.Mutex
	next     idgen.ID
	totp     map[[2]idgen.ID]*TOTPRow
	recovery map[[2]idgen.ID][]*RecoveryRow
	policy   map[idgen.ID]*Policy
	rules    map[idgen.ID][]IPRule
	logs     map[idgen.ID][]LogRow
	failLog  bool // 让 ResetUser 的日志写入失败，验证“日志写不进去就不重置”
	now      func() time.Time
}

func newMemStore() *memStore {
	return &memStore{totp: map[[2]idgen.ID]*TOTPRow{}, recovery: map[[2]idgen.ID][]*RecoveryRow{},
		policy: map[idgen.ID]*Policy{}, rules: map[idgen.ID][]IPRule{}, logs: map[idgen.ID][]LogRow{}, now: time.Now}
}

func (m *memStore) id() idgen.ID { m.next++; return m.next }

func (m *memStore) GetTOTP(_ context.Context, tid, uid idgen.ID) (*TOTPRow, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if r, ok := m.totp[[2]idgen.ID{tid, uid}]; ok {
		c := *r
		return &c, nil
	}
	return nil, nil
}

func (m *memStore) SavePending(_ context.Context, tid, uid idgen.ID, username, enc string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	k := [2]idgen.ID{tid, uid}
	if r, ok := m.totp[k]; ok {
		if !r.Enabled {
			r.SecretEnc, r.Username = enc, username
		}
		return nil
	}
	r := &TOTPRow{UserID: uid, Username: username, SecretEnc: enc}
	r.ID, r.TenantID = m.id(), tid
	m.totp[k] = r
	return nil
}

func (m *memStore) EnableTOTP(_ context.Context, tid, uid idgen.ID, hashes []string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	r, ok := m.totp[[2]idgen.ID{tid, uid}]
	if !ok || r.Enabled {
		return errors.New("no pending")
	}
	now := m.now()
	r.Enabled, r.EnabledAt, r.GraceUntil = true, &now, nil
	m.replace(tid, uid, hashes)
	return nil
}

func (m *memStore) replace(tid, uid idgen.ID, hashes []string) {
	var rows []*RecoveryRow
	for _, h := range hashes {
		rows = append(rows, &RecoveryRow{UserID: uid, CodeHash: h})
	}
	m.recovery[[2]idgen.ID{tid, uid}] = rows
}

func (m *memStore) DisableTOTP(_ context.Context, tid, uid idgen.ID) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.totp, [2]idgen.ID{tid, uid})
	delete(m.recovery, [2]idgen.ID{tid, uid})
	return nil
}

func (m *memStore) EnsureGrace(_ context.Context, tid, uid idgen.ID, username string, until time.Time) (*TOTPRow, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	k := [2]idgen.ID{tid, uid}
	r, ok := m.totp[k]
	if !ok {
		r = &TOTPRow{UserID: uid, Username: username, GraceUntil: &until}
		r.ID, r.TenantID = m.id(), tid
		m.totp[k] = r
	} else if r.GraceUntil == nil && !r.Enabled {
		r.GraceUntil = &until
	}
	c := *r
	return &c, nil
}

func (m *memStore) ListTOTP(_ context.Context, tid idgen.ID, keyword string, page, size int) ([]TOTPRow, int64, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []TOTPRow
	for k, r := range m.totp {
		if k[0] == tid && strings.Contains(r.Username, keyword) {
			out = append(out, *r)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].UserID < out[j].UserID })
	total := int64(len(out))
	lo := min((page-1)*size, len(out))
	return out[lo:min(lo+size, len(out))], total, nil
}

func (m *memStore) ResetUser(_ context.Context, tid, uid idgen.ID, lg *LogRow) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.failLog {
		return errors.New("log write failed")
	}
	delete(m.totp, [2]idgen.ID{tid, uid})
	delete(m.recovery, [2]idgen.ID{tid, uid})
	lg.TenantID = tid
	m.logs[tid] = append(m.logs[tid], *lg)
	return nil
}

func (m *memStore) ReplaceRecovery(_ context.Context, tid, uid idgen.ID, hashes []string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.replace(tid, uid, hashes)
	return nil
}

func (m *memStore) ConsumeRecovery(_ context.Context, tid, uid idgen.ID, hash string) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, r := range m.recovery[[2]idgen.ID{tid, uid}] {
		if r.CodeHash == hash && r.UsedAt == nil {
			now := m.now()
			r.UsedAt = &now
			return true, nil
		}
	}
	return false, nil
}

func (m *memStore) CountRecovery(_ context.Context, tid, uid idgen.ID) (int64, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var n int64
	for _, r := range m.recovery[[2]idgen.ID{tid, uid}] {
		if r.UsedAt == nil {
			n++
		}
	}
	return n, nil
}

func (m *memStore) GetPolicy(_ context.Context, tid idgen.ID) (*Policy, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if p, ok := m.policy[tid]; ok {
		c := *p
		return &c, nil
	}
	return nil, nil
}

func (m *memStore) SavePolicy(_ context.Context, tid idgen.ID, p *Policy) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	c := *p
	c.TenantID, c.UpdatedAt = tid, m.now() // 与 gorm 的 updated_at 自动更新一致
	m.policy[tid] = &c
	return nil
}

func (m *memStore) ListRules(_ context.Context, tid idgen.ID) ([]IPRule, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]IPRule(nil), m.rules[tid]...), nil
}

func (m *memStore) GetRule(_ context.Context, tid, id idgen.ID) (*IPRule, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, r := range m.rules[tid] {
		if r.ID == id {
			c := r
			return &c, nil
		}
	}
	return nil, nil
}

func (m *memStore) SaveRule(_ context.Context, tid idgen.ID, r *IPRule) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if r.ID == 0 {
		r.ID, r.TenantID = m.id(), tid
		m.rules[tid] = append(m.rules[tid], *r)
		return nil
	}
	for i := range m.rules[tid] {
		if m.rules[tid][i].ID == r.ID {
			m.rules[tid][i] = *r
		}
	}
	return nil
}

func (m *memStore) DeleteRule(_ context.Context, tid, id idgen.ID) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	var keep []IPRule
	for _, r := range m.rules[tid] {
		if r.ID != id {
			keep = append(keep, r)
		}
	}
	m.rules[tid] = keep
	return nil
}

func (m *memStore) AddLog(_ context.Context, tid idgen.ID, l *LogRow) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	l.TenantID = tid
	m.logs[tid] = append(m.logs[tid], *l)
	return nil
}

func (m *memStore) ListLogs(_ context.Context, tid idgen.ID, page, size int) ([]LogRow, int64, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	l := m.logs[tid]
	return l, int64(len(l)), nil
}

func (m *memStore) logActions(tid idgen.ID) []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []string
	for _, l := range m.logs[tid] {
		out = append(out, l.Action)
	}
	return out
}
