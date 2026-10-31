package workflow

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"gorm.io/gorm"

	"github.com/nexadmin/flowlite/engine"
	"github.com/nexadmin/flowlite/runtime"
	"github.com/nexadmin/flowlite/store"
	"nexadmin/core/response"
	"nexadmin/core/tenant"
	"nexadmin/pkg/idgen"
)

// 引擎类型别名：宿主侧沿用简短的 Definition / Node / KindStart 等名字，
// 具体实现与语义在独立 module github.com/nexadmin/flowlite 中维护与测试。
type (
	Definition = engine.Definition
	Node       = engine.Node
	Edge       = engine.Edge
	Vote       = engine.Vote
	Event      = engine.Event
)

const (
	KindStart     = engine.KindStart
	KindApproval  = engine.KindApproval
	KindExclusive = engine.KindExclusive
	KindEnd       = engine.KindEnd

	ModeAny = engine.ModeAny
	ModeAll = engine.ModeAll

	StatusRunning   = engine.StatusRunning
	StatusApproved  = engine.StatusApproved
	StatusRejected  = engine.StatusRejected
	StatusCancelled = engine.StatusCancelled

	ActionStart       = engine.ActionStart
	ActionApprove     = engine.ActionApprove
	ActionReject      = engine.ActionReject
	ActionCountersign = engine.ActionCountersign
	ActionRollback    = engine.ActionRollback
	ActionEscalate    = engine.ActionEscalate
	ActionWithdraw    = engine.ActionWithdraw
)

// Validate 保留给模板与测试使用。
func Validate(def Definition) []string { return engine.Validate(def) }

// logger 是最小日志面（便于测试注入空实现）。
type logger interface {
	Warn(msg string, keysAndValues ...any)
	Error(msg string, keysAndValues ...any)
}

// Service 是工作流宿主适配层：把 flowlite 的存储端口接到 GORM + 租户，
// 并把运行时错误翻译成统一的 HTTP 语义。
type Service struct {
	rt  *runtime.Service
	log logger
}

// NewService 创建工作流服务。
func NewService(db *gorm.DB, log logger) *Service {
	rt := runtime.New(gormDefs{db: db}, gormInsts{db: db}, gormTasks{db: db}, gormActions{db: db})
	s := newWithRuntime(rt, log)
	// 定时扫描会跨租户读取（Skip）；推进时必须先 Unskip 再放实例租户，
	// 否则 tenant.With 保留了 skip 标记，待办/动作会写成 tenant_id=0。
	rt.ContextFor = func(ctx context.Context, inst *store.Instance) context.Context {
		if id, err := idgen.Parse(inst.TenantID); err == nil && id != 0 {
			return tenant.With(tenant.Unskip(ctx), id)
		}
		return ctx
	}
	return s
}

// newWithRuntime 允许测试注入内存存储，避免为了校验业务逻辑拉起数据库。
func newWithRuntime(rt *runtime.Service, log logger) *Service {
	s := &Service{rt: rt, log: log}
	rt.OnEvent = s.handleEvent
	return s
}

// ---- 流程定义 ----

// DefView 是定义视图。
type DefView struct {
	ID         idgen.ID   `json:"id"`
	Key        string     `json:"key"`
	Name       string     `json:"name"`
	Status     string     `json:"status"`
	Version    int        `json:"version"`
	Definition Definition `json:"definition"`
	XML        string     `json:"xml,omitempty"`
	CreatedAt  time.Time  `json:"createdAt"`
	UpdatedAt  time.Time  `json:"updatedAt"`
}

// DefQuery 是定义列表过滤条件。
type DefQuery struct {
	Keyword  string
	Status   string
	Page     int
	PageSize int
}

// DefForm 是定义保存表单。XML 非空时以 XML 为准（标准建模器保存的就是它）。
type DefForm struct {
	Key        string     `json:"key"`
	Name       string     `json:"name"`
	Status     string     `json:"status"`
	Definition Definition `json:"definition"`
	XML        string     `json:"xml"`
}

func toDefView(d *store.Definition) (DefView, error) {
	v := DefView{
		Key: d.Key, Name: d.Name, Status: d.Status, Version: d.Version,
		XML: string(d.XML), CreatedAt: d.CreatedAt, UpdatedAt: d.UpdatedAt,
	}
	if id, err := idgen.Parse(d.ID); err == nil {
		v.ID = id
	}
	if len(d.Definition) > 0 {
		if err := json.Unmarshal(d.Definition, &v.Definition); err != nil {
			return DefView{}, response.BadRequest("workflow.def_json_broken")
		}
	}
	// 没有建模器原文时（历史数据/API 写入）现场生成一份，前端编辑器可以直接打开。
	if strings.TrimSpace(v.XML) == "" && len(d.Definition) > 0 {
		if xml, err := engine.ExportXML(v.Definition); err == nil {
			v.XML = xml
		}
	}
	return v, nil
}

// DefList 分页列出流程定义。
func (s *Service) DefList(ctx context.Context, q DefQuery) ([]DefView, int64, error) {
	rows, total, err := s.rt.Defs.List(ctx, store.DefQuery{
		Keyword: q.Keyword, Status: q.Status, Page: q.Page, PageSize: q.PageSize,
	})
	if err != nil {
		return nil, 0, err
	}
	out := make([]DefView, 0, len(rows))
	for _, d := range rows {
		v, err := toDefView(d)
		if err != nil {
			return nil, 0, err
		}
		out = append(out, v)
	}
	return out, total, nil
}

// DefGet 返回定义详情。
func (s *Service) DefGet(ctx context.Context, id idgen.ID) (*DefView, error) {
	d, err := s.rt.Defs.Get(ctx, idText(id))
	if err != nil {
		return nil, mapStoreErr(err, "workflow.def_not_found")
	}
	v, err := toDefView(d)
	if err != nil {
		return nil, err
	}
	return &v, nil
}

// DefSave 新增或更新定义。定义必须先通过引擎校验；每次保存 version+1。
func (s *Service) DefSave(ctx context.Context, id idgen.ID, f DefForm) (*DefView, error) {
	def := f.Definition
	xmlText := strings.TrimSpace(f.XML)
	if xmlText != "" {
		parsed, err := engine.ImportXML(xmlText)
		if err != nil {
			s.warn("workflow: xml import failed", "err", err.Error())
			return nil, response.BadRequest("workflow.def_invalid").Wrap(err)
		}
		def = parsed
	}
	key := strings.TrimSpace(f.Key)
	if key == "" {
		key = strings.TrimSpace(def.Key)
	}
	if key == "" {
		return nil, response.BadRequest("workflow.def_invalid")
	}
	// 列表与发起都按记录 key 检索；建模器里的 process id 可能不同，这里以表单 key 为准。
	def.Key = key
	if errs := engine.Validate(def); len(errs) > 0 {
		s.warn("workflow: definition validation failed", "errors", strings.Join(errs, "；"))
		return nil, response.BadRequest("workflow.def_invalid")
	}
	raw, err := json.Marshal(def)
	if err != nil {
		return nil, err
	}
	status := strings.TrimSpace(f.Status)
	if status != DefDraft && status != DefPublished && status != DefDisabled {
		status = DefDraft
	}
	rec := &store.Definition{
		Key: key, Name: truncate(strings.TrimSpace(f.Name), 100), Status: status,
		Definition: raw, XML: []byte(xmlText),
	}
	if id != 0 {
		rec.ID = idText(id)
	}
	saved, err := s.rt.Defs.Save(ctx, rec)
	if err != nil {
		return nil, mapStoreErr(err, "workflow.def_not_found")
	}
	v, err := toDefView(saved)
	if err != nil {
		return nil, err
	}
	return &v, nil
}

// DefDelete 删除定义（已发起的实例保留自己的快照，不受影响）。
func (s *Service) DefDelete(ctx context.Context, id idgen.ID) error {
	return mapStoreErr(s.rt.Defs.Delete(ctx, idText(id)), "workflow.def_not_found")
}

// DefXML 返回定义的 BPMN XML：优先返回建模器保存的原文（含布局），
// 没有原文时由引擎按定义生成。
func (s *Service) DefXML(ctx context.Context, id idgen.ID) (string, error) {
	d, err := s.rt.Defs.Get(ctx, idText(id))
	if err != nil {
		return "", mapStoreErr(err, "workflow.def_not_found")
	}
	if len(d.XML) > 0 {
		return string(d.XML), nil
	}
	var def engine.Definition
	if err := json.Unmarshal(d.Definition, &def); err != nil {
		return "", response.BadRequest("workflow.def_json_broken")
	}
	out, err := engine.ExportXML(def)
	if err != nil {
		return "", response.BadRequest("workflow.def_invalid").Wrap(err)
	}
	return out, nil
}

// DefTemplate 是内置的流程定义模板（不落库，供前端「新增定义」时套用）。
type DefTemplate struct {
	Key        string     `json:"key"`
	Name       string     `json:"name"`
	Definition Definition `json:"definition"`
	XML        string     `json:"xml"`
}

// Templates 返回内置模板副本（附 BPMN XML，供标准建模器直接打开）。
func Templates() []DefTemplate {
	out := templateDefinitions()
	for i := range out {
		if xml, err := engine.ExportXML(out[i].Definition); err == nil {
			out[i].XML = xml
		}
	}
	return out
}

// ---- 流程实例 ----

// PendingView 是一条当前待办。
type PendingView struct {
	NodeKey  string `json:"nodeKey"`
	NodeName string `json:"nodeName"`
	Assignee string `json:"assignee"`
}

// InstView 是实例视图（state 已解码）。
type InstView struct {
	ID          idgen.ID           `json:"id"`
	DefID       idgen.ID           `json:"defId"`
	DefKey      string             `json:"defKey"`
	DefVersion  int                `json:"defVersion"`
	Title       string             `json:"title"`
	BizType     string             `json:"bizType"`
	BusinessKey string             `json:"businessKey"`
	Submitter   string             `json:"submitter"`
	Status      string             `json:"status"`
	Revision    int64              `json:"revision"`
	Current     []string           `json:"current"`
	Done        []string           `json:"done"`
	Vars        map[string]string  `json:"vars"`
	Votes       map[string]Vote    `json:"votes"`
	History     []Event            `json:"history"`
	Pending     []PendingView      `json:"pending"`
	Definition  *engine.Definition `json:"definition,omitempty"`
	CreatedAt   time.Time          `json:"createdAt"`
	UpdatedAt   time.Time          `json:"updatedAt"`
}

func toInstView(res *runtime.Result) InstView {
	rec, st := res.Inst, res.State
	v := InstView{
		DefKey: rec.DefKey, DefVersion: rec.DefVersion, Title: rec.Title,
		BizType: rec.BizType, BusinessKey: rec.BusinessKey, Submitter: rec.Submitter,
		Status: rec.Status, Revision: rec.Revision,
		Current: []string{}, Done: []string{}, Vars: map[string]string{},
		Votes: map[string]Vote{}, History: []Event{}, Pending: []PendingView{},
		Definition: res.Def, CreatedAt: rec.CreatedAt, UpdatedAt: rec.UpdatedAt,
	}
	if id, err := idgen.Parse(rec.ID); err == nil {
		v.ID = id
	}
	if id, err := idgen.Parse(rec.DefID); err == nil {
		v.DefID = id
	}
	if st == nil {
		return v
	}
	if len(st.Current) > 0 {
		v.Current = st.Current
	}
	if len(st.Done) > 0 {
		v.Done = st.Done
	}
	if st.Vars != nil {
		v.Vars = st.Vars
	}
	if st.Votes != nil {
		v.Votes = st.Votes
	}
	if st.History != nil {
		v.History = st.History
	}
	if res.Def != nil {
		for _, p := range engine.PendingTasks(*res.Def, st) {
			v.Pending = append(v.Pending, PendingView{NodeKey: p.NodeKey, NodeName: p.NodeName, Assignee: p.Assignee})
		}
	}
	return v
}

// InstQuery 是实例列表过滤条件。Todo/Done 互斥；Mine=started 只看我发起的。
type InstQuery struct {
	Keyword  string
	Status   string
	BizType  string
	Todo     string
	Done     string
	Mine     string
	Page     int
	PageSize int
}

// InstList 分页列出实例。待办/已办走物化任务与动作表，可按索引分页。
func (s *Service) InstList(ctx context.Context, q InstQuery, actor string) ([]InstView, int64, error) {
	switch {
	case q.Todo == "1":
		res, total, err := s.rt.ListMinePending(ctx, actor, q.Page, q.PageSize)
		if err != nil {
			return nil, 0, err
		}
		return viewsOf(res), total, nil
	case q.Done == "1":
		res, total, err := s.rt.ListMineHandled(ctx, actor, q.Page, q.PageSize)
		if err != nil {
			return nil, 0, err
		}
		return viewsOf(res), total, nil
	}
	iq := store.InstQuery{
		Keyword: q.Keyword, Status: q.Status, BizType: q.BizType,
		Page: q.Page, PageSize: q.PageSize,
	}
	if q.Mine == "started" {
		iq.Submitter = actor
	}
	recs, total, err := s.rt.Insts.List(ctx, iq)
	if err != nil {
		return nil, 0, err
	}
	out := make([]InstView, 0, len(recs))
	for _, rec := range recs {
		res, err := s.rt.Hydrate(ctx, rec)
		if err != nil {
			continue
		}
		out = append(out, toInstView(res))
	}
	return out, total, nil
}

func viewsOf(res []*runtime.Result) []InstView {
	out := make([]InstView, 0, len(res))
	for _, r := range res {
		out = append(out, toInstView(r))
	}
	return out
}

// InstGet 返回实例详情。
func (s *Service) InstGet(ctx context.Context, id idgen.ID) (*InstView, error) {
	res, err := s.rt.Get(ctx, idText(id))
	if err != nil {
		return nil, mapStoreErr(err, "workflow.inst_not_found")
	}
	v := toInstView(res)
	return &v, nil
}

// StartInput 是发起流程的入参。
type StartInput struct {
	DefID       string
	DefKey      string
	Title       string
	BizType     string
	BusinessKey string
	Vars        map[string]string
}

// InstStart 用一份已发布的定义发起实例；同 (bizType, businessKey) 幂等。
func (s *Service) InstStart(ctx context.Context, f StartInput, actor string) (*InstView, error) {
	res, err := s.rt.Start(ctx, runtime.StartInput{
		DefID: f.DefID, DefKey: f.DefKey, Title: f.Title,
		BizType: f.BizType, BusinessKey: f.BusinessKey, Vars: f.Vars,
	}, actor)
	if err != nil {
		return nil, mapRuntimeErr(err, "workflow.inst_not_found")
	}
	v := toInstView(res)
	return &v, nil
}

// InstApprove 通过当前节点（会签需全部同意才推进）。
func (s *Service) InstApprove(ctx context.Context, id idgen.ID, actor, comment string) (*InstView, error) {
	return s.act(ctx, id, actor, runtime.ActionApprove, runtime.ActionInput{Comment: comment})
}

// InstReject 驳回：整条实例置为 rejected。
func (s *Service) InstReject(ctx context.Context, id idgen.ID, actor, comment string) (*InstView, error) {
	return s.act(ctx, id, actor, runtime.ActionReject, runtime.ActionInput{Comment: comment})
}

// InstCountersign 加签。
func (s *Service) InstCountersign(ctx context.Context, id idgen.ID, actor, approver, comment string) (*InstView, error) {
	return s.act(ctx, id, actor, runtime.ActionCountersign, runtime.ActionInput{Approver: approver, Comment: comment})
}

// InstRollback 回退到指定审批节点。
func (s *Service) InstRollback(ctx context.Context, id idgen.ID, actor, toNode, comment string) (*InstView, error) {
	return s.act(ctx, id, actor, runtime.ActionRollback, runtime.ActionInput{ToNode: toNode, Comment: comment})
}

// InstEscalate 手动超时升级。
func (s *Service) InstEscalate(ctx context.Context, id idgen.ID, actor, comment string) (*InstView, error) {
	return s.act(ctx, id, actor, runtime.ActionEscalate, runtime.ActionInput{Comment: comment})
}

// InstWithdraw 撤回：仅提交人可撤回运行中的实例。
func (s *Service) InstWithdraw(ctx context.Context, id idgen.ID, actor, comment string) (*InstView, error) {
	return s.act(ctx, id, actor, runtime.ActionWithdraw, runtime.ActionInput{Comment: comment})
}

func (s *Service) act(ctx context.Context, id idgen.ID, actor string, act runtime.Action, in runtime.ActionInput) (*InstView, error) {
	res, err := s.rt.Act(ctx, idText(id), actor, act, in)
	if err != nil {
		return nil, mapRuntimeErr(err, "workflow.inst_not_found")
	}
	v := toInstView(res)
	return &v, nil
}

// SweepEscalate 清扫器：超时自动升级。由 plugin.Starter 每分钟调用。
func (s *Service) SweepEscalate(ctx context.Context, now time.Time) (int, error) {
	return s.rt.Sweep(tenant.Skip(ctx), now)
}

// ---- 错误映射 ----

func mapStoreErr(err error, notFoundKey string) error {
	if errors.Is(err, store.ErrNotFound) {
		return response.NotFound(notFoundKey)
	}
	return err
}

func mapRuntimeErr(err error, notFoundKey string) error {
	var re *response.Error
	if errors.As(err, &re) {
		return err
	}
	switch {
	case errors.Is(err, store.ErrNotFound):
		return response.NotFound(notFoundKey)
	case errors.Is(err, store.ErrConflict):
		return response.Conflict("workflow.concurrent_update")
	case errors.Is(err, runtime.ErrActorRequired):
		return response.BadRequest("workflow.actor_required")
	case errors.Is(err, runtime.ErrDefRequired):
		return response.BadRequest("workflow.def_required")
	case errors.Is(err, runtime.ErrNotPublished):
		return response.Conflict("workflow.def_not_published")
	case errors.Is(err, runtime.ErrNotOwner):
		return response.Forbidden("workflow.withdraw_not_owner")
	case errors.Is(err, engine.ErrNotApprover):
		return response.Forbidden("workflow.not_approver")
	case errors.Is(err, engine.ErrNotRunning):
		return response.Conflict("workflow.inst_not_running")
	case errors.Is(err, engine.ErrNoActiveTask), errors.Is(err, engine.ErrMultipleActiveTasks):
		return response.Conflict("workflow.no_active_task")
	case errors.Is(err, engine.ErrNoEscalateTarget):
		return response.BadRequest("workflow.escalate_target_missing")
	case errors.Is(err, engine.ErrInvalidDefinition):
		return response.BadRequest("workflow.def_invalid")
	default:
		return err
	}
}

func (s *Service) warn(msg string, kv ...any) {
	if s.log != nil {
		s.log.Warn(msg, kv...)
	}
}

func truncate(s string, n int) string {
	if len(s) > n {
		return s[:n]
	}
	return s
}
