// Package runtime 把引擎、存储端口与事件粘在一起：负责定义快照、商幂等发起、
// 乐观锁重放、待办物化、审计动作与超时扫描。宿主只需要实现 store 端口并注入回调。
package runtime

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/nexadmin/flowlite/engine"
	"github.com/nexadmin/flowlite/store"
)

// 事件名。
const (
	EventStarted   = "started"
	EventTodo      = "todo"
	EventCompleted = "completed"
)

// SystemActor 是定时任务/系统调用使用的操作人标识。
const SystemActor = "system"

// 错误。
var (
	ErrActorRequired = errors.New("flowlite/runtime: actor is required")
	ErrNotPublished  = errors.New("flowlite/runtime: definition is not published")
	ErrNotOwner      = errors.New("flowlite/runtime: actor is not the submitter")
	ErrDefRequired   = errors.New("flowlite/runtime: defId or defKey is required")
)

// Action 是运行时支持的动作。
type Action string

const (
	ActionApprove     Action = "approve"
	ActionReject      Action = "reject"
	ActionCountersign Action = "countersign"
	ActionRollback    Action = "rollback"
	ActionEscalate    Action = "escalate"
	ActionWithdraw    Action = "withdraw"
)

// Event 是宿主可订阅的运行时事件。
type Event struct {
	Name      string
	Inst      *store.Instance
	State     *engine.Instance
	Def       *engine.Definition
	Approvers []string
	Actor     string
	Comment   string
	At        time.Time
}

// Result 是一次读写后的完整视图。
type Result struct {
	Inst  *store.Instance
	State *engine.Instance
	Def   *engine.Definition
}

// Service 是运行时编排器。
type Service struct {
	Defs       store.DefStore
	Insts      store.InstStore
	Tasks      store.TaskStore
	Actions    store.ActionStore
	Now        func() time.Time
	OnEvent    func(context.Context, Event)
	MaxRetries int
	// ContextFor 让宿主为单个实例重建上下文（例如把租户重新放进 context）。
	// 定时扫描 Sweep 先用调用方的 ctx 读取，再按此函数切换后推进。
	ContextFor func(ctx context.Context, inst *store.Instance) context.Context
}

// New 组装一个运行时。Tasks/Actions 可为 nil（此时不做待办物化与审计）。
func New(defs store.DefStore, insts store.InstStore, tasks store.TaskStore, actions store.ActionStore) *Service {
	return &Service{
		Defs: defs, Insts: insts, Tasks: tasks, Actions: actions,
		Now:        func() time.Time { return time.Now().UTC() },
		MaxRetries: 3,
	}
}

// StartInput 是发起入参。BizType/BusinessKey 把实例挂到业务对象上，并作为发起幂等键。
type StartInput struct {
	DefID       string
	DefKey      string
	Title       string
	BizType     string
	BusinessKey string
	Vars        map[string]string
}

// ActionInput 是动作入参。
type ActionInput struct {
	Comment  string
	Approver string // countersign 专用
	ToNode   string // rollback 专用
}

func (s *Service) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now().UTC()
}

func (s *Service) emit(ctx context.Context, ev Event) {
	if s.OnEvent != nil {
		s.OnEvent(ctx, ev)
	}
}

func (s *Service) hydrate(ctx context.Context, rec *store.Instance) (*Result, error) {
	state := &engine.Instance{}
	if len(rec.State) > 0 {
		if err := json.Unmarshal(rec.State, state); err != nil {
			return nil, fmt.Errorf("flowlite/runtime: 实例状态损坏: %w", err)
		}
	}
	def, err := s.definition(ctx, rec)
	if err != nil {
		return nil, err
	}
	return &Result{Inst: rec, State: state, Def: def}, nil
}

// definition 优先用实例快照；快照缺失（历史数据）时退回当前定义。
func (s *Service) definition(ctx context.Context, rec *store.Instance) (*engine.Definition, error) {
	var def engine.Definition
	if len(rec.Snapshot) > 0 {
		if err := json.Unmarshal(rec.Snapshot, &def); err != nil {
			return nil, fmt.Errorf("flowlite/runtime: 定义快照损坏: %w", err)
		}
		return &def, nil
	}
	var d *store.Definition
	var err error
	if rec.DefID != "" {
		d, err = s.Defs.Get(ctx, rec.DefID)
	} else {
		d, err = s.Defs.GetByKey(ctx, rec.DefKey)
	}
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(d.Definition, &def); err != nil {
		return nil, fmt.Errorf("flowlite/runtime: 定义 JSON 损坏: %w", err)
	}
	return &def, nil
}

// Get 返回实例详情。
func (s *Service) Get(ctx context.Context, id string) (*Result, error) {
	rec, err := s.Insts.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	return s.hydrate(ctx, rec)
}

// Hydrate 把一条已读出的实例记录补全为 Result（宿主批量列表时避免二次查询）。
func (s *Service) Hydrate(ctx context.Context, rec *store.Instance) (*Result, error) {
	return s.hydrate(ctx, rec)
}

// Start 用一份已发布的定义发起实例。同 (bizType, businessKey) 已有运行中实例时直接返回它。
// 实例保存发起时的定义快照：之后修改流程不影响在途实例。
func (s *Service) Start(ctx context.Context, in StartInput, actor string) (*Result, error) {
	actor = strings.TrimSpace(actor)
	if actor == "" {
		return nil, ErrActorRequired
	}
	defRec, err := s.pickDefinition(ctx, in)
	if err != nil {
		return nil, err
	}
	if defRec.Status != store.DefPublished {
		return nil, fmt.Errorf("%w: %s", ErrNotPublished, defRec.Status)
	}
	var def engine.Definition
	if err := json.Unmarshal(defRec.Definition, &def); err != nil {
		return nil, fmt.Errorf("flowlite/runtime: 定义 JSON 损坏: %w", err)
	}

	bizType, bizKey := strings.TrimSpace(in.BizType), strings.TrimSpace(in.BusinessKey)
	if bizType != "" && bizKey != "" {
		if running, err := s.Insts.FindRunningByBiz(ctx, bizType, bizKey); err == nil {
			return s.hydrate(ctx, running)
		} else if !errors.Is(err, store.ErrNotFound) {
			return nil, err
		}
	}

	now := s.now()
	state, err := engine.Start(def, in.Vars, actor, now)
	if err != nil {
		return nil, err
	}
	stateJSON, err := json.Marshal(state)
	if err != nil {
		return nil, err
	}
	rec := &store.Instance{
		DefID: defRec.ID, DefKey: def.Key, DefVersion: defRec.Version,
		Title: strings.TrimSpace(in.Title), BizType: bizType, BusinessKey: bizKey,
		Submitter: actor, Status: state.Status, Revision: state.Revision,
		State: stateJSON, Snapshot: defRec.Definition,
	}
	if err := s.Insts.Create(ctx, rec); err != nil {
		return nil, err
	}
	s.syncTasks(ctx, rec, &def, state)
	s.recordAction(ctx, rec, state)

	res := &Result{Inst: rec, State: state, Def: &def}
	s.emit(ctx, Event{Name: EventStarted, Inst: rec, State: state, Def: &def, Actor: actor, At: now})
	if state.Status == engine.StatusRunning {
		s.emitEvent(ctx, EventTodo, res, actor, "")
	} else {
		s.emitEvent(ctx, EventCompleted, res, actor, "")
	}
	return res, nil
}

func (s *Service) pickDefinition(ctx context.Context, in StartInput) (*store.Definition, error) {
	if id := strings.TrimSpace(in.DefID); id != "" {
		return s.Defs.Get(ctx, id)
	}
	if key := strings.TrimSpace(in.DefKey); key != "" {
		return s.Defs.GetByKey(ctx, key)
	}
	return nil, ErrDefRequired
}

// Act 推进实例。乐观锁冲突时重读并重放，最多 MaxRetries 次。
func (s *Service) Act(ctx context.Context, id, actor string, act Action, in ActionInput) (*Result, error) {
	actor = strings.TrimSpace(actor)
	if actor == "" {
		return nil, ErrActorRequired
	}
	retries := s.MaxRetries
	if retries <= 0 {
		retries = 1
	}
	var lastErr error
	for attempt := 0; attempt < retries; attempt++ {
		res, err := s.actOnce(ctx, id, actor, act, in)
		if err == nil {
			return res, nil
		}
		lastErr = err
		if !errors.Is(err, store.ErrConflict) {
			return nil, err
		}
	}
	return nil, lastErr
}

func (s *Service) actOnce(ctx context.Context, id, actor string, act Action, in ActionInput) (*Result, error) {
	rec, err := s.Insts.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	res, err := s.hydrate(ctx, rec)
	if err != nil {
		return nil, err
	}
	def, state := res.Def, res.State
	expected := rec.Revision
	prevHistory := len(state.History)

	if err := s.authorize(act, rec, def, state, actor); err != nil {
		return nil, err
	}
	now := s.now()
	switch act {
	case ActionApprove:
		err = engine.Approve(*def, state, actor, in.Comment, now)
	case ActionReject:
		err = engine.Reject(*def, state, actor, in.Comment, now)
	case ActionCountersign:
		err = engine.Countersign(*def, state, actor, in.Approver, in.Comment, now)
	case ActionRollback:
		err = engine.Rollback(*def, state, actor, in.ToNode, in.Comment, now)
	case ActionEscalate:
		err = engine.Escalate(*def, state, actor, in.Comment, now)
	case ActionWithdraw:
		err = engine.Withdraw(state, actor, in.Comment, now)
	default:
		err = fmt.Errorf("flowlite/runtime: 未知动作 %q", act)
	}
	if err != nil {
		return nil, err
	}

	stateJSON, err := json.Marshal(state)
	if err != nil {
		return nil, err
	}
	rec.State = stateJSON
	rec.Status = state.Status
	rec.Revision = state.Revision
	// 历史实例没有快照时，第一次流转就把当前定义固化下来，避免之后改流程影响它。
	if len(rec.Snapshot) == 0 {
		if raw, err := json.Marshal(def); err == nil {
			rec.Snapshot = raw
		}
	}
	if err := s.Insts.Update(ctx, rec, expected); err != nil {
		return nil, err
	}
	s.syncTasks(ctx, rec, def, state)
	if len(state.History) > prevHistory {
		s.recordAction(ctx, rec, state)
	}
	out := &Result{Inst: rec, State: state, Def: def}
	if state.Status == engine.StatusRunning {
		s.emitEvent(ctx, EventTodo, out, actor, in.Comment)
	} else {
		s.emitEvent(ctx, EventCompleted, out, actor, in.Comment)
	}
	return out, nil
}

func (s *Service) authorize(act Action, rec *store.Instance, def *engine.Definition, state *engine.Instance, actor string) error {
	if actor == SystemActor {
		return nil
	}
	switch act {
	case ActionWithdraw:
		if rec.Submitter != actor {
			return ErrNotOwner
		}
	case ActionCountersign, ActionEscalate:
		if !engine.IsActiveApprover(*def, state, actor) {
			return engine.ErrNotApprover
		}
	case ActionRollback:
		if rec.Submitter != actor && !engine.IsActiveApprover(*def, state, actor) {
			return fmt.Errorf("%w: 只有提交人或当前审批人可以回退", engine.ErrNotApprover)
		}
	}
	return nil
}

func (s *Service) syncTasks(ctx context.Context, rec *store.Instance, def *engine.Definition, state *engine.Instance) {
	if s.Tasks == nil {
		return
	}
	pend := engine.PendingTasks(*def, state)
	out := make([]store.PendingTask, 0, len(pend))
	for _, p := range pend {
		out = append(out, store.PendingTask{NodeKey: p.NodeKey, NodeName: p.NodeName, Assignee: p.Assignee})
	}
	if err := s.Tasks.SyncPending(ctx, rec.ID, rec.DefKey, out); err != nil {
		// 待办物化失败不能让已经落库的流转回滚；下一次流转会重新对齐。
		s.emit(ctx, Event{Name: "task_sync_failed", Inst: rec, State: state, Def: def})
	}
}

func (s *Service) recordAction(ctx context.Context, rec *store.Instance, state *engine.Instance) {
	if s.Actions == nil || len(state.History) == 0 {
		return
	}
	ev := state.History[len(state.History)-1]
	action := &store.Action{
		InstID: rec.ID, DefKey: rec.DefKey, NodeKey: ev.Node,
		Actor: ev.Actor, Action: ev.Action, Comment: ev.Comment, At: ev.At,
	}
	_ = s.Actions.Record(ctx, action)
}

func (s *Service) emitEvent(ctx context.Context, name string, res *Result, actor, comment string) {
	approvers := make([]string, 0, 4)
	seen := map[string]bool{}
	for _, t := range engine.PendingTasks(*res.Def, res.State) {
		if t.Assignee == "" || seen[t.Assignee] {
			continue
		}
		seen[t.Assignee] = true
		approvers = append(approvers, t.Assignee)
	}
	s.emit(ctx, Event{
		Name: name, Inst: res.Inst, State: res.State, Def: res.Def,
		Approvers: approvers, Actor: actor, Comment: comment, At: s.now(),
	})
}

// ListMinePending 返回 assignee 的待办（含公开任务），按待办顺序返回实例视图。
func (s *Service) ListMinePending(ctx context.Context, assignee string, page, pageSize int) ([]*Result, int64, error) {
	if s.Tasks == nil {
		return nil, 0, nil
	}
	tasks, total, err := s.Tasks.ListPending(ctx, assignee, store.TaskQuery{Page: page, PageSize: pageSize})
	if err != nil {
		return nil, 0, err
	}
	ids := make([]string, 0, len(tasks))
	seen := map[string]bool{}
	for _, t := range tasks {
		if !seen[t.InstID] {
			seen[t.InstID] = true
			ids = append(ids, t.InstID)
		}
	}
	recs, err := s.Insts.GetMany(ctx, ids)
	if err != nil {
		return nil, 0, err
	}
	byID := make(map[string]*store.Instance, len(recs))
	for _, r := range recs {
		byID[r.ID] = r
	}
	out := make([]*Result, 0, len(ids))
	for _, id := range ids {
		if rec, ok := byID[id]; ok {
			if res, err := s.hydrate(ctx, rec); err == nil {
				out = append(out, res)
			}
		}
	}
	return out, total, nil
}

// ListMineHandled 返回 assignee 处理过（同意/驳回）的实例。
func (s *Service) ListMineHandled(ctx context.Context, assignee string, page, pageSize int) ([]*Result, int64, error) {
	if s.Actions == nil {
		return nil, 0, nil
	}
	acts, total, err := s.Actions.List(ctx, store.ActionQuery{
		Actor: assignee, Actions: []string{engine.ActionApprove, engine.ActionReject},
		Page: page, PageSize: pageSize,
	})
	if err != nil {
		return nil, 0, err
	}
	ids := make([]string, 0, len(acts))
	seen := map[string]bool{}
	for _, a := range acts {
		if !seen[a.InstID] {
			seen[a.InstID] = true
			ids = append(ids, a.InstID)
		}
	}
	recs, err := s.Insts.GetMany(ctx, ids)
	if err != nil {
		return nil, 0, err
	}
	byID := make(map[string]*store.Instance, len(recs))
	for _, r := range recs {
		byID[r.ID] = r
	}
	out := make([]*Result, 0, len(ids))
	for _, id := range ids {
		if rec, ok := byID[id]; ok {
			if res, err := s.hydrate(ctx, rec); err == nil {
				out = append(out, res)
			}
		}
	}
	return out, total, nil
}

// Sweep 扫描运行中实例，把「节点配置了超时且已到期」的实例升级到 EscalateTo。
// 供宿主定时任务调用；返回实际升级的实例数。
func (s *Service) Sweep(ctx context.Context, now time.Time) (int, error) {
	recs, err := s.Insts.ListRunning(ctx, 500)
	if err != nil {
		return 0, err
	}
	count := 0
	for _, rec := range recs {
		res, err := s.hydrate(ctx, rec)
		if err != nil {
			continue
		}
		if len(engine.OverdueApprovals(*res.Def, res.State, rec.UpdatedAt, now)) == 0 {
			continue
		}
		actCtx := ctx
		if s.ContextFor != nil {
			actCtx = s.ContextFor(ctx, rec)
		}
		if _, err := s.Act(actCtx, rec.ID, SystemActor, ActionEscalate, ActionInput{Comment: "超时自动升级"}); err != nil {
			s.emit(ctx, Event{Name: "escalate_failed", Inst: rec, State: res.State, Def: res.Def, At: now})
			continue
		}
		count++
	}
	return count, nil
}
