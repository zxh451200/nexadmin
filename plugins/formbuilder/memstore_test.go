package formbuilder

import (
	"context"
	"sort"
	"sync"
	"time"

	"nexadmin/core/database"
	"nexadmin/core/tenant"
	"nexadmin/pkg/idgen"
)

// memStore 是 Store 的内存实现，语义与 gormStore 对齐：每个方法都按 ctx 里的租户隔离，
// 名额占用/唯一键/乐观发布都在同一把锁里完成（等价于数据库里的单条原子语句）。
// 它只用来测 service 层的逻辑；gormStore 的 SQL 形状由 store_test.go（sqlmock）验证。
type memStore struct {
	mu       sync.Mutex
	seq      int64
	forms    map[[2]int64]*Form
	versions map[[3]int64]*Version
	subs     map[int64]*Submission
	dedupe   map[string]int64
	clock    func() time.Time
}

func newMemStore() *memStore {
	return &memStore{
		forms: map[[2]int64]*Form{}, versions: map[[3]int64]*Version{}, subs: map[int64]*Submission{},
		dedupe: map[string]int64{}, clock: time.Now,
	}
}

func (m *memStore) tid(ctx context.Context) (int64, error) {
	t, ok := tenant.From(ctx)
	if !ok || t == 0 {
		return 0, database.ErrNoTenant
	}
	return int64(t), nil
}

func (m *memStore) next() idgen.ID { m.seq++; return idgen.ID(1000 + m.seq) }

func (m *memStore) CreateForm(ctx context.Context, f *Form) error {
	t, err := m.tid(ctx)
	if err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if f.ID == 0 {
		f.ID = m.next()
	}
	f.TenantID = idgen.ID(t)
	f.CreatedAt, f.UpdatedAt = m.clock(), m.clock()
	cp := *f
	m.forms[[2]int64{t, int64(f.ID)}] = &cp
	return nil
}

func (m *memStore) form(t int64, id idgen.ID) (*Form, error) {
	f, ok := m.forms[[2]int64{t, int64(id)}]
	if !ok {
		return nil, errNotFound
	}
	return f, nil
}

func (m *memStore) GetForm(ctx context.Context, id idgen.ID) (*Form, error) {
	t, err := m.tid(ctx)
	if err != nil {
		return nil, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	f, err := m.form(t, id)
	if err != nil {
		return nil, err
	}
	cp := *f
	return &cp, nil
}

func (m *memStore) ListForms(ctx context.Context, q FormQuery) ([]Form, int64, error) {
	t, err := m.tid(ctx)
	if err != nil {
		return nil, 0, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []Form
	for k, f := range m.forms {
		if k[0] == t && (q.Status == "" || f.Status == q.Status) {
			out = append(out, *f)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID > out[j].ID })
	return out, int64(len(out)), nil
}

func (m *memStore) ListPublished(ctx context.Context, limit int) ([]Form, error) {
	all, _, err := m.ListForms(ctx, FormQuery{Status: StatusPublished})
	if len(all) > limit {
		all = all[:limit]
	}
	return all, err
}

func (m *memStore) SaveDraft(ctx context.Context, id idgen.ID, name, desc, def string) error {
	t, err := m.tid(ctx)
	if err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	f, err := m.form(t, id)
	if err != nil {
		return err
	}
	f.Name, f.Description, f.DraftDef = name, desc, def
	f.DraftRev++
	return nil
}

func (m *memStore) SaveAudience(ctx context.Context, id idgen.ID, audience string) error {
	t, err := m.tid(ctx)
	if err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	f, err := m.form(t, id)
	if err != nil {
		return err
	}
	f.Audience = audience
	return nil
}

func (m *memStore) DeleteForm(ctx context.Context, id idgen.ID) error {
	t, err := m.tid(ctx)
	if err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, err := m.form(t, id); err != nil {
		return err
	}
	delete(m.forms, [2]int64{t, int64(id)})
	for k := range m.versions {
		if k[0] == t && k[1] == int64(id) {
			delete(m.versions, k)
		}
	}
	for sid, s := range m.subs {
		if int64(s.TenantID) == t && s.FormID == id {
			delete(m.subs, sid)
			delete(m.dedupe, dkey(t, id, s.DedupeKey))
		}
	}
	return nil
}

func dkey(t int64, f idgen.ID, key string) string {
	return string(rune(t)) + "|" + f.String() + "|" + key
}

func (m *memStore) Publish(ctx context.Context, formID idgen.ID, v *Version, meta PublishMeta) error {
	t, err := m.tid(ctx)
	if err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	f, err := m.form(t, formID)
	if err != nil || f.PublishedVersion != v.Version-1 {
		return errStoreConflict
	}
	if _, dup := m.versions[[3]int64{t, int64(formID), int64(v.Version)}]; dup {
		return errStoreConflict
	}
	f.Status, f.PublishedVersion, f.PublishedRev = StatusPublished, v.Version, meta.Rev
	now := m.clock()
	f.PublishedAt = &now
	f.Name, f.Description, f.AllowRepeat, f.DeadlineAt, f.MaxSubmissions = meta.Name, meta.Description, meta.AllowRepeat, meta.DeadlineAt, meta.MaxSubmissions
	v.ID, v.TenantID, v.FormID = m.next(), idgen.ID(t), formID
	cp := *v
	m.versions[[3]int64{t, int64(formID), int64(v.Version)}] = &cp
	return nil
}

func (m *memStore) SetStatus(ctx context.Context, id idgen.ID, from, to string) error {
	t, err := m.tid(ctx)
	if err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	f, err := m.form(t, id)
	if err != nil || f.Status != from || f.PublishedVersion == 0 {
		return errStoreConflict
	}
	f.Status = to
	return nil
}

func (m *memStore) GetVersion(ctx context.Context, formID idgen.ID, version int) (*Version, error) {
	t, err := m.tid(ctx)
	if err != nil {
		return nil, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	v, ok := m.versions[[3]int64{t, int64(formID), int64(version)}]
	if !ok {
		return nil, errNotFound
	}
	cp := *v
	return &cp, nil
}

func (m *memStore) ListVersions(ctx context.Context, formID idgen.ID) ([]Version, error) {
	t, err := m.tid(ctx)
	if err != nil {
		return nil, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []Version
	for k, v := range m.versions {
		if k[0] == t && k[1] == int64(formID) {
			out = append(out, *v)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Version < out[j].Version })
	return out, nil
}
