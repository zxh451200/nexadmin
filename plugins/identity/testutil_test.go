package identity

import (
	"context"
	"encoding/json"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"nexadmin/core/secret"
	"nexadmin/core/tenant"
	"nexadmin/pkg/idgen"
)

var initOnce sync.Once

// initTest 初始化雪花 ID 与加密主密钥（测试专用固定密钥）。
func initTest(t *testing.T) {
	t.Helper()
	initOnce.Do(func() {
		if err := idgen.Init(1); err != nil {
			panic(err)
		}
	})
	c, err := secret.New([]byte(strings.Repeat("k", 32)))
	if err != nil {
		t.Fatal(err)
	}
	secret.SetDefault(c)
	t.Cleanup(func() { secret.SetDefault(nil) })
}

// tctx 是“拥有 identity:match 权限的管理员”发起的租户请求；没有该权限的请求用 plainCtx。
func tctx(tid idgen.ID) context.Context { return withMatchPerm(plainCtx(tid)) }

func plainCtx(tid idgen.ID) context.Context { return tenant.With(context.Background(), tid) }

// memStore 是带租户校验的内存 store：任何方法都只能看到给定租户的行。
type memStore struct {
	mu   sync.Mutex
	rows map[idgen.ID]*Source
}

func newMemStore() *memStore { return &memStore{rows: map[idgen.ID]*Source{}} }

func (m *memStore) sorted(tid idgen.ID, keep func(*Source) bool) []Source {
	var out []Source
	for _, r := range m.rows {
		if r.TenantID == tid && keep(r) {
			out = append(out, *r)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

func (m *memStore) list(_ context.Context, tid idgen.ID) ([]Source, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.sorted(tid, func(*Source) bool { return true }), nil
}

func (m *memStore) listEnabled(_ context.Context, tid idgen.ID, typ string) ([]Source, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.sorted(tid, func(s *Source) bool { return s.Type == typ && s.Enabled }), nil
}

func (m *memStore) get(_ context.Context, tid, id idgen.ID) (*Source, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if r, ok := m.rows[id]; ok && r.TenantID == tid {
		c := *r
		return &c, nil
	}
	return nil, errNotFound
}

func (m *memStore) count(_ context.Context, tid idgen.ID) (int64, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return int64(len(m.sorted(tid, func(*Source) bool { return true }))), nil
}

func (m *memStore) create(_ context.Context, s *Source) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	id, err := idgen.Next()
	if err != nil {
		return err
	}
	s.ID, s.CreatedAt, s.UpdatedAt = id, time.Now(), time.Now()
	c := *s
	m.rows[id] = &c
	return nil
}

func (m *memStore) save(_ context.Context, s *Source) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if r, ok := m.rows[s.ID]; ok && r.TenantID == s.TenantID {
		r.Name, r.Enabled, r.Config = s.Name, s.Enabled, s.Config
	}
	return nil
}

func (m *memStore) remove(_ context.Context, tid, id idgen.ID) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if r, ok := m.rows[id]; ok && r.TenantID == tid {
		delete(m.rows, id)
	}
	return nil
}

// addLDAP 经 Service.Save 的同一条路径（校验 + 口令加密）插入一个 LDAP 源，返回存库行。
func addLDAP(t *testing.T, svc *Service, tid idgen.ID, cfg LDAPConfig, enabled bool) *Source {
	t.Helper()
	v, err := svc.Save(tctx(tid), 0, SourceForm{Type: TypeLDAP, Name: "ldap", Enabled: enabled, Config: mustJSON(t, cfg)})
	if err != nil {
		t.Fatal(err)
	}
	src, err := svc.st.get(context.Background(), tid, v.ID)
	if err != nil {
		t.Fatal(err)
	}
	return src
}

func mustJSON(t *testing.T, v any) json.RawMessage {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// initTestNoKey 模拟加密主密钥尚未初始化。
func initTestNoKey() { secret.SetDefault(nil) }
