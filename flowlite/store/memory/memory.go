// Package memory 是 flowlite 的内存存储实现：适合单元测试、示例与单机演示。
// 四个类型分别实现 store 的四组端口，包含乐观锁与待办物化语义。
package memory

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/nexadmin/flowlite/store"
)

// Bundle 汇总四组内存端口，方便一次性构造。
type Bundle struct {
	Defs    *Defs
	Insts   *Insts
	Tasks   *Tasks
	Actions *Actions
}

// SetIDGenerator 自定义主键生成（默认 prefix-N）。宿主想复用雪花 ID 时可注入。
func (b *Bundle) SetIDGenerator(fn func(prefix string) string) {
	b.Defs.idf, b.Insts.idf, b.Tasks.idf, b.Actions.idf = fn, fn, fn, fn
}

// New 创建一套相互独立的内存存储。
func New() *Bundle {
	clock := func() time.Time { return time.Now().UTC() }
	return &Bundle{Defs: NewDefs(clock), Insts: NewInsts(clock), Tasks: NewTasks(clock), Actions: NewActions(clock)}
}

// SetClock 注入时钟（测试用）。
func (b *Bundle) SetClock(fn func() time.Time) {
	b.Defs.now, b.Insts.now, b.Tasks.now, b.Actions.now = fn, fn, fn, fn
}

// ---- 定义 ----

// Defs 实现 store.DefStore。
type Defs struct {
	mu   sync.Mutex
	rows map[string]*store.Definition
	seq  int64
	now  func() time.Time
	idf  func(string) string
}

// NewDefs 创建定义存储。
func NewDefs(clock func() time.Time) *Defs {
	return &Defs{rows: map[string]*store.Definition{}, now: clock, idf: defaultID}
}

func cloneDef(d *store.Definition) *store.Definition {
	c := *d
	c.Definition = append([]byte(nil), d.Definition...)
	c.XML = append([]byte(nil), d.XML...)
	return &c
}

func (s *Defs) Get(_ context.Context, id string) (*store.Definition, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	d, ok := s.rows[id]
	if !ok {
		return nil, store.ErrNotFound
	}
	return cloneDef(d), nil
}

func (s *Defs) GetByKey(_ context.Context, key string) (*store.Definition, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var latest *store.Definition
	for _, d := range s.rows {
		if d.Key != key {
			continue
		}
		if latest == nil || d.Version > latest.Version || (d.Version == latest.Version && d.ID > latest.ID) {
			latest = d
		}
	}
	if latest == nil {
		return nil, store.ErrNotFound
	}
	return cloneDef(latest), nil
}

func (s *Defs) Save(_ context.Context, d *store.Definition) (*store.Definition, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.now()
	if d.ID == "" {
		s.seq++
		n := *d
		n.ID = s.idf("def")
		n.Version = 1
		n.CreatedAt, n.UpdatedAt = now, now
		s.rows[n.ID] = cloneDef(&n)
		return cloneDef(&n), nil
	}
	cur, ok := s.rows[d.ID]
	if !ok {
		return nil, store.ErrNotFound
	}
	n := *d
	n.Version = cur.Version + 1
	n.CreatedAt = cur.CreatedAt
	n.UpdatedAt = now
	s.rows[n.ID] = cloneDef(&n)
	return cloneDef(&n), nil
}

func (s *Defs) Delete(_ context.Context, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.rows[id]; !ok {
		return store.ErrNotFound
	}
	delete(s.rows, id)
	return nil
}

func (s *Defs) List(_ context.Context, q store.DefQuery) ([]*store.Definition, int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	kw := strings.ToLower(strings.TrimSpace(q.Keyword))
	rows := make([]*store.Definition, 0, len(s.rows))
	for _, d := range s.rows {
		if q.Status != "" && d.Status != q.Status {
			continue
		}
		if kw != "" && !strings.Contains(strings.ToLower(d.Key), kw) && !strings.Contains(strings.ToLower(d.Name), kw) {
			continue
		}
		rows = append(rows, cloneDef(d))
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].ID > rows[j].ID })
	total := int64(len(rows))
	start, end := bounds(len(rows), q.Page, q.PageSize)
	return rows[start:end], total, nil
}

// ---- 实例 ----

// Insts 实现 store.InstStore。
type Insts struct {
	mu   sync.Mutex
	rows map[string]*store.Instance
	seq  int64
	now  func() time.Time
	idf  func(string) string
}

// NewInsts 创建实例存储。
func NewInsts(clock func() time.Time) *Insts {
	return &Insts{rows: map[string]*store.Instance{}, now: clock, idf: defaultID}
}

func cloneInst(i *store.Instance) *store.Instance {
	c := *i
	c.State = append([]byte(nil), i.State...)
	c.Snapshot = append([]byte(nil), i.Snapshot...)
	return &c
}

func (s *Insts) Create(_ context.Context, inst *store.Instance) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if inst.ID == "" {
		inst.ID = s.idf("inst")
	}
	if inst.Revision == 0 {
		inst.Revision = 1
	}
	now := s.now()
	if inst.CreatedAt.IsZero() {
		inst.CreatedAt = now
	}
	inst.UpdatedAt = now
	s.rows[inst.ID] = cloneInst(inst)
	return nil
}

func (s *Insts) Get(_ context.Context, id string) (*store.Instance, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	i, ok := s.rows[id]
	if !ok {
		return nil, store.ErrNotFound
	}
	return cloneInst(i), nil
}

func (s *Insts) GetMany(_ context.Context, ids []string) ([]*store.Instance, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]*store.Instance, 0, len(ids))
	for _, id := range ids {
		if i, ok := s.rows[id]; ok {
			out = append(out, cloneInst(i))
		}
	}
	return out, nil
}

func (s *Insts) Update(_ context.Context, inst *store.Instance, expectedRevision int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	cur, ok := s.rows[inst.ID]
	if !ok {
		return store.ErrNotFound
	}
	if cur.Revision != expectedRevision {
		return store.ErrConflict
	}
	n := cloneInst(inst)
	n.CreatedAt = cur.CreatedAt
	n.UpdatedAt = s.now()
	s.rows[n.ID] = n
	inst.UpdatedAt = n.UpdatedAt
	return nil
}

func (s *Insts) List(_ context.Context, q store.InstQuery) ([]*store.Instance, int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	kw := strings.ToLower(strings.TrimSpace(q.Keyword))
	rows := make([]*store.Instance, 0, len(s.rows))
	for _, i := range s.rows {
		if q.Status != "" && i.Status != q.Status {
			continue
		}
		if q.BizType != "" && i.BizType != q.BizType {
			continue
		}
		if q.Submitter != "" && i.Submitter != q.Submitter {
			continue
		}
		if kw != "" &&
			!strings.Contains(strings.ToLower(i.Title), kw) &&
			!strings.Contains(strings.ToLower(i.DefKey), kw) &&
			!strings.Contains(strings.ToLower(i.BusinessKey), kw) {
			continue
		}
		rows = append(rows, cloneInst(i))
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].ID > rows[j].ID })
	total := int64(len(rows))
	start, end := bounds(len(rows), q.Page, q.PageSize)
	return rows[start:end], total, nil
}

func (s *Insts) FindRunningByBiz(_ context.Context, bizType, businessKey string) (*store.Instance, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, i := range s.rows {
		if i.BizType == bizType && i.BusinessKey == businessKey && i.Status == "running" {
			return cloneInst(i), nil
		}
	}
	return nil, store.ErrNotFound
}

func (s *Insts) ListRunning(_ context.Context, limit int) ([]*store.Instance, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	rows := make([]*store.Instance, 0, len(s.rows))
	for _, i := range s.rows {
		if i.Status == "running" {
			rows = append(rows, cloneInst(i))
		}
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].UpdatedAt.Before(rows[j].UpdatedAt) })
	if limit > 0 && len(rows) > limit {
		rows = rows[:limit]
	}
	return rows, nil
}

// ---- 待办 ----

// Tasks 实现 store.TaskStore。
type Tasks struct {
	mu   sync.Mutex
	rows map[string][]*store.Task
	seq  int64
	now  func() time.Time
	idf  func(string) string
}

// NewTasks 创建待办存储。
func NewTasks(clock func() time.Time) *Tasks {
	return &Tasks{rows: map[string][]*store.Task{}, now: clock, idf: defaultID}
}

func (s *Tasks) SyncPending(_ context.Context, instID, defKey string, pending []store.PendingTask) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.now()
	old := map[string]*store.Task{}
	for _, t := range s.rows[instID] {
		if t.Status == store.TaskPending {
			old[t.NodeKey+"\x00"+t.Assignee] = t
		}
	}
	next := make([]*store.Task, 0, len(pending))
	for _, p := range pending {
		sig := p.NodeKey + "\x00" + p.Assignee
		if cur, ok := old[sig]; ok {
			cur.NodeName, cur.UpdatedAt = p.NodeName, now
			next = append(next, cur)
			delete(old, sig)
			continue
		}
		next = append(next, &store.Task{
			ID: s.idf("task"), InstID: instID, DefKey: defKey,
			NodeKey: p.NodeKey, NodeName: p.NodeName, Assignee: p.Assignee,
			Status: store.TaskPending, CreatedAt: now, UpdatedAt: now,
		})
	}
	kept := make([]*store.Task, 0, len(s.rows[instID]))
	for _, t := range s.rows[instID] {
		if t.Status != store.TaskPending {
			kept = append(kept, t)
		}
	}
	for _, t := range old {
		t.Status = store.TaskClosed
		t.UpdatedAt = now
		kept = append(kept, t)
	}
	s.rows[instID] = append(kept, next...)
	return nil
}

func (s *Tasks) ListPending(_ context.Context, assignee string, q store.TaskQuery) ([]*store.Task, int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var rows []*store.Task
	for _, list := range s.rows {
		for _, t := range list {
			if t.Status != store.TaskPending {
				continue
			}
			if t.Assignee != "" && t.Assignee != assignee {
				continue
			}
			rows = append(rows, t)
		}
	}
	sort.Slice(rows, func(i, j int) bool {
		if rows[i].CreatedAt.Equal(rows[j].CreatedAt) {
			return rows[i].ID > rows[j].ID
		}
		return rows[i].CreatedAt.After(rows[j].CreatedAt)
	})
	total := int64(len(rows))
	start, end := bounds(len(rows), q.Page, q.PageSize)
	return rows[start:end], total, nil
}

func (s *Tasks) ListByInstance(_ context.Context, instID string) ([]*store.Task, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]*store.Task(nil), s.rows[instID]...), nil
}

// ---- 动作 ----

// Actions 实现 store.ActionStore。
type Actions struct {
	mu   sync.Mutex
	rows []*store.Action
	seq  int64
	now  func() time.Time
	idf  func(string) string
}

// NewActions 创建动作存储。
func NewActions(clock func() time.Time) *Actions { return &Actions{now: clock, idf: defaultID} }

func (s *Actions) Record(_ context.Context, a *store.Action) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	c := *a
	if c.ID == "" {
		c.ID = s.idf("act")
	}
	if c.At.IsZero() {
		c.At = s.now()
	}
	s.rows = append(s.rows, &c)
	return nil
}

func (s *Actions) List(_ context.Context, q store.ActionQuery) ([]*store.Action, int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	allow := map[string]bool{}
	for _, a := range q.Actions {
		allow[a] = true
	}
	var rows []*store.Action
	for i := len(s.rows) - 1; i >= 0; i-- {
		a := s.rows[i]
		if q.Actor != "" && a.Actor != q.Actor {
			continue
		}
		if len(allow) > 0 && !allow[a.Action] {
			continue
		}
		rows = append(rows, a)
	}
	total := int64(len(rows))
	start, end := bounds(len(rows), q.Page, q.PageSize)
	return rows[start:end], total, nil
}

func (s *Actions) ListByInstance(_ context.Context, instID string) ([]*store.Action, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []*store.Action
	for _, a := range s.rows {
		if a.InstID == instID {
			out = append(out, a)
		}
	}
	return out, nil
}

func defaultID(prefix string) string {
	return fmt.Sprintf("%s-%d", prefix, time.Now().UnixNano())
}

// ---- 分页工具 ----

func bounds(n, page, size int) (int, int) {
	if page < 1 {
		page = 1
	}
	if size < 1 {
		size = 20
	}
	start := (page - 1) * size
	if start > n {
		start = n
	}
	end := start + size
	if end > n {
		end = n
	}
	return start, end
}
