package formbuilder

import (
	"context"
	"encoding/json"
	"sort"
	"strings"
	"testing"

	"nexadmin/pkg/idgen"
)

func (m *memStore) InsertSubmission(ctx context.Context, s *Submission, maxCount int) error {
	t, err := m.tid(ctx)
	if err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	f, err := m.form(t, s.FormID)
	if err != nil || f.Status != StatusPublished {
		return errStoreNotPublished
	}
	if maxCount > 0 && f.SubmissionCount >= int64(maxCount) {
		return errStoreFull
	}
	k := dkey(t, s.FormID, s.DedupeKey)
	if _, dup := m.dedupe[k]; dup {
		return errStoreDuplicate // 事务回滚：名额未占用
	}
	if s.ID == 0 {
		s.ID = m.next()
	}
	s.TenantID = idgen.ID(t)
	s.CreatedAt = m.clock()
	f.SubmissionCount++
	cp := *s
	m.subs[int64(s.ID)] = &cp
	m.dedupe[k] = int64(s.ID)
	return nil
}

func (m *memStore) FindByDedupe(ctx context.Context, formID idgen.ID, key string) (*Submission, error) {
	t, err := m.tid(ctx)
	if err != nil {
		return nil, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	id, ok := m.dedupe[dkey(t, formID, key)]
	if !ok {
		return nil, errNotFound
	}
	cp := *m.subs[id]
	return &cp, nil
}

func (m *memStore) GetSubmission(ctx context.Context, formID, id idgen.ID) (*Submission, error) {
	t, err := m.tid(ctx)
	if err != nil {
		return nil, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	s, ok := m.subs[int64(id)]
	if !ok || int64(s.TenantID) != t || s.FormID != formID {
		return nil, errNotFound
	}
	cp := *s
	return &cp, nil
}

func (m *memStore) DeleteSubmission(ctx context.Context, formID, id idgen.ID) error {
	t, err := m.tid(ctx)
	if err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	s, ok := m.subs[int64(id)]
	if !ok || int64(s.TenantID) != t || s.FormID != formID {
		return errNotFound
	}
	delete(m.subs, int64(id))
	delete(m.dedupe, dkey(t, formID, s.DedupeKey))
	if f, err := m.form(t, formID); err == nil && f.SubmissionCount > 0 {
		f.SubmissionCount--
	}
	return nil
}

// matching 返回符合 SQL 条件（租户、表单、版本、用户名、时间）的提交，按 id 倒序。调用方持锁。
func (m *memStore) matching(t int64, q SubQuery) []Submission {
	var out []Submission
	for _, s := range m.subs {
		if int64(s.TenantID) != t || s.FormID != q.FormID {
			continue
		}
		if q.Version > 0 && s.Version != q.Version {
			continue
		}
		if q.Username != "" && !strings.Contains(s.Username, q.Username) {
			continue
		}
		if q.From != nil && s.CreatedAt.Before(*q.From) || q.To != nil && s.CreatedAt.After(*q.To) {
			continue
		}
		out = append(out, *s)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID > out[j].ID })
	return out
}

func (m *memStore) CountSubmissions(ctx context.Context, q SubQuery) (int64, error) {
	t, err := m.tid(ctx)
	if err != nil {
		return 0, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	return int64(len(m.matching(t, q))), nil
}

func (m *memStore) PageSubmissions(ctx context.Context, q SubQuery) ([]Submission, error) {
	t, err := m.tid(ctx)
	if err != nil {
		return nil, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	all := m.matching(t, q)
	start := (q.Page - 1) * q.PageSize
	if start >= len(all) {
		return nil, nil
	}
	return all[start:min(start+q.PageSize, len(all))], nil
}

func (m *memStore) ScanSubmissions(ctx context.Context, q SubQuery, before idgen.ID, limit int) ([]Submission, error) {
	t, err := m.tid(ctx)
	if err != nil {
		return nil, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []Submission
	for _, s := range m.matching(t, q) {
		if before > 0 && s.ID >= before {
			continue
		}
		out = append(out, s)
		if len(out) == limit {
			break
		}
	}
	return out, nil
}

func (m *memStore) SubmittedForms(ctx context.Context, userID idgen.ID, formIDs []idgen.ID) (map[idgen.ID]bool, error) {
	t, err := m.tid(ctx)
	if err != nil {
		return nil, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	want := map[idgen.ID]bool{}
	for _, id := range formIDs {
		want[id] = true
	}
	out := map[idgen.ID]bool{}
	for _, s := range m.subs {
		if int64(s.TenantID) == t && s.UserID == userID && want[s.FormID] {
			out[s.FormID] = true
		}
	}
	return out, nil
}

// fakeDir 是 Directory 的测试实现。
type fakeDir struct {
	roles   map[idgen.ID][]idgen.ID // 用户 -> 角色
	dicts   map[string][]Option
	failFor map[idgen.ID]bool // 这些用户读取角色会报错
}

func (d *fakeDir) UserRoleIDs(_ context.Context, uid idgen.ID) ([]idgen.ID, error) {
	if d.failFor[uid] {
		return nil, context.DeadlineExceeded
	}
	return d.roles[uid], nil
}
func (d *fakeDir) Roles(context.Context) ([]RoleOption, error)              { return nil, nil }
func (d *fakeDir) Users(context.Context, string, int) ([]UserOption, error) { return nil, nil }
func (d *fakeDir) DictItems(_ context.Context, code string) ([]Option, error) {
	return d.dicts[code], nil
}

func mustJSON(t *testing.T, v any) []byte {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return b
}
