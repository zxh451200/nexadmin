package workflow

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"gorm.io/gorm"

	"nexadmin/core/response"
	"nexadmin/core/tenant"
	"nexadmin/pkg/idgen"
)

// Service 是工作流业务层。宿主业务模块通过 client.go 的包级函数发起/查询/推进流程。
type Service struct {
	db  *gorm.DB
	log logger
}

// logger 是最小日志面（便于测试注入空实现）。
type logger interface {
	Warn(msg string, keysAndValues ...any)
	Error(msg string, keysAndValues ...any)
}

// NewService 创建工作流服务。
func NewService(db *gorm.DB, log logger) *Service { return &Service{db: db, log: log} }

// ---- 流程定义 ----

// DefView 是定义视图（definition 已解码）。
type DefView struct {
	ID         idgen.ID   `json:"id"`
	Key        string     `json:"key"`
	Name       string     `json:"name"`
	Status     string     `json:"status"`
	Version    int        `json:"version"`
	Definition Definition `json:"definition"`
	CreatedAt  time.Time  `json:"createdAt"`
	UpdatedAt  time.Time  `json:"updatedAt"`
}

func (s *Service) defGet(ctx context.Context, id idgen.ID) (*Def, error) {
	var m Def
	err := s.db.WithContext(ctx).First(&m, "id = ?", int64(id)).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, response.NotFound("workflow.def_not_found")
	}
	return &m, err
}

func toDefView(m *Def) (DefView, error) {
	v := DefView{
		ID: m.ID, Key: m.DefKey, Name: m.Name, Status: m.Status,
		Version: m.Version, CreatedAt: m.CreatedAt, UpdatedAt: m.UpdatedAt,
	}
	if raw := strings.TrimSpace(m.Definition); raw != "" {
		if err := json.Unmarshal([]byte(raw), &v.Definition); err != nil {
			return DefView{}, response.BadRequest("workflow.def_json_broken")
		}
	}
	return v, nil
}

// DefQuery 是定义列表过滤条件。
type DefQuery struct {
	Keyword  string
	Status   string
	Page     int
	PageSize int
}

// DefList 分页列出流程定义。
func (s *Service) DefList(ctx context.Context, q DefQuery) ([]DefView, int64, error) {
	base := s.db.WithContext(ctx).Model(&Def{})
	if kw := strings.TrimSpace(q.Keyword); kw != "" {
		like := "%" + kw + "%"
		base = base.Where("def_key LIKE ? OR name LIKE ?", like, like)
	}
	if q.Status != "" {
		base = base.Where("status = ?", q.Status)
	}
	base = base.Session(&gorm.Session{})
	var total int64
	if err := base.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var rows []Def
	if err := base.Order("id DESC").Offset((q.Page - 1) * q.PageSize).Limit(q.PageSize).Find(&rows).Error; err != nil {
		return nil, 0, err
	}
	out := make([]DefView, 0, len(rows))
	for i := range rows {
		v, err := toDefView(&rows[i])
		if err != nil {
			return nil, 0, err
		}
		out = append(out, v)
	}
	return out, total, nil
}

// DefGet 返回定义详情。
func (s *Service) DefGet(ctx context.Context, id idgen.ID) (*DefView, error) {
	m, err := s.defGet(ctx, id)
	if err != nil {
		return nil, err
	}
	v, err := toDefView(m)
	if err != nil {
		return nil, err
	}
	return &v, nil
}

// DefForm 是定义保存表单。
type DefForm struct {
	Key        string     `json:"key"`
	Name       string     `json:"name"`
	Status     string     `json:"status"`
	Definition Definition `json:"definition"`
}

// DefSave 新增（id=0）或更新定义；定义必须先通过引擎 Validate。每次保存 version+1。
func (s *Service) DefSave(ctx context.Context, id idgen.ID, f DefForm) (*DefView, error) {
	if errs := Validate(f.Definition); len(errs) > 0 {
		return nil, response.BadRequest("workflow.def_invalid").WithMsg(strings.Join(errs, "；"))
	}
	raw, err := json.Marshal(f.Definition)
	if err != nil {
		return nil, err
	}
	var m Def
	if id != 0 {
		cur, err := s.defGet(ctx, id)
		if err != nil {
			return nil, err
		}
		m = *cur
	}
	m.Name = truncate(strings.TrimSpace(f.Name), 100)
	m.DefKey = strings.TrimSpace(f.Key)
	if m.DefKey == "" {
		m.DefKey = strings.TrimSpace(f.Definition.Key)
	}
	if status := strings.TrimSpace(f.Status); status == DefDraft || status == DefPublished || status == DefDisabled {
		m.Status = status
	}
	if m.Status == "" {
		m.Status = DefDraft
	}
	m.Definition = string(raw)
	db := s.db.WithContext(ctx)
	if id == 0 {
		m.Version = 1
		if err := db.Create(&m).Error; err != nil {
			return nil, err
		}
	} else {
		m.Version++
		if err := db.Save(&m).Error; err != nil {
			return nil, err
		}
	}
	v, err := toDefView(&m)
	if err != nil {
		return nil, err
	}
	return &v, nil
}

// DefDelete 删除定义（已发起的实例保留自己的状态快照，不受影响）。
func (s *Service) DefDelete(ctx context.Context, id idgen.ID) error {
	return s.db.WithContext(ctx).Where("id = ?", int64(id)).Delete(&Def{}).Error
}

// DefTemplate 是内置的流程定义模板（不落库，供前端「新增定义」时套用）。
type DefTemplate struct {
	Key        string     `json:"key"`
	Name       string     `json:"name"`
	Definition Definition `json:"definition"`
}

// Templates 返回内置模板副本；审批人为占位（前端替换成真实用户 ID）。
// 编译期已断言全部通过引擎校验（见 templates.go）。
func Templates() []DefTemplate { return templateDefinitions() }

// ---- 流程实例 ----

// InstView 是实例视图（state 已解码）。
type InstView struct {
	ID          idgen.ID `json:"id"`
	DefID       idgen.ID `json:"defId"`
	DefKey      string   `json:"defKey"`
	Title       string   `json:"title"`
	BizType     string   `json:"bizType"`
	BusinessKey string   `json:"businessKey"`
	Submitter   string   `json:"submitter"`
	Status      string   `json:"status"`
	Current     []string `json:"current"`
	Done        []string `json:"done"`
	Vars        map[string]string
	Votes       map[string]Vote
	History     []Event
	CreatedAt   time.Time `json:"createdAt"`
	UpdatedAt   time.Time `json:"updatedAt"`
}

func decodeState(m *Inst) (*Instance, error) {
	inst := &Instance{}
	if raw := strings.TrimSpace(m.StateJSON); raw != "" {
		if err := json.Unmarshal([]byte(raw), inst); err != nil {
			return nil, err
		}
	}
	return inst, nil
}

func toInstView(m *Inst, inst *Instance) InstView {
	v := InstView{
		ID: m.ID, DefID: m.DefID, DefKey: m.DefKey, Title: m.Title, BizType: m.BizType,
		BusinessKey: m.BusinessKey, Submitter: m.Submitter, Status: m.Status,
		CreatedAt: m.CreatedAt, UpdatedAt: m.UpdatedAt,
		Current: []string{}, Done: []string{}, Vars: map[string]string{},
		Votes: map[string]Vote{}, History: []Event{},
	}
	if inst == nil {
		return v
	}
	if len(inst.Current) > 0 {
		v.Current = inst.Current
	}
	if len(inst.Done) > 0 {
		v.Done = inst.Done
	}
	if inst.Vars != nil {
		v.Vars = inst.Vars
	}
	if inst.Votes != nil {
		v.Votes = inst.Votes
	}
	if inst.History != nil {
		v.History = inst.History
	}
	return v
}

func (s *Service) instGet(ctx context.Context, id idgen.ID) (*Inst, *Instance, error) {
	var m Inst
	err := s.db.WithContext(ctx).First(&m, "id = ?", int64(id)).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil, response.NotFound("workflow.inst_not_found")
	}
	if err != nil {
		return nil, nil, err
	}
	inst, err := decodeState(&m)
	if err != nil {
		return nil, nil, response.BadRequest("workflow.inst_state_broken")
	}
	return &m, inst, nil
}

// InstQuery 是实例列表过滤条件；Todo=1 / Done=1 互斥（我的待办 / 我已办）。
type InstQuery struct {
	Keyword  string
	Status   string
	BizType  string
	Todo     string
	Done     string
	Page     int
	PageSize int
}

// InstList 分页列出实例；Todo/Done 基于状态快照在内存过滤（P0 口径，量大后可加待办表）。
func (s *Service) InstList(ctx context.Context, q InstQuery, actor string) ([]InstView, int64, error) {
	base := s.db.WithContext(ctx).Model(&Inst{})
	if kw := strings.TrimSpace(q.Keyword); kw != "" {
		like := "%" + kw + "%"
		base = base.Where("title LIKE ? OR def_key LIKE ? OR business_key LIKE ?", like, like, like)
	}
	if q.Status != "" {
		base = base.Where("status = ?", q.Status)
	}
	if q.BizType != "" {
		base = base.Where("biz_type = ?", q.BizType)
	}
	if q.Todo == "1" {
		base = base.Where("status = ?", StatusRunning)
	}
	base = base.Session(&gorm.Session{})
	var total int64
	if err := base.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var rows []Inst
	if err := base.Order("id DESC").Limit(500).Find(&rows).Error; err != nil {
		return nil, 0, err
	}
	// 定义一次取全（同一租户的定义数量有限）。
	defs := map[idgen.ID]Definition{}
	if len(rows) > 0 {
		var drows []Def
		if err := s.db.WithContext(ctx).Find(&drows).Error; err == nil {
			for i := range drows {
				var d Definition
				if json.Unmarshal([]byte(drows[i].Definition), &d) == nil {
					defs[drows[i].ID] = d
				}
			}
		}
	}
	out := make([]InstView, 0, len(rows))
	for i := range rows {
		inst, err := decodeState(&rows[i])
		if err != nil {
			inst = nil
		}
		def := defs[rows[i].DefID]
		switch {
		case q.Todo == "1":
			if !instIsMineTodo(&def, inst, actor) {
				continue
			}
		case q.Done == "1":
			if !instIsMineDone(inst, actor) {
				continue
			}
		}
		out = append(out, toInstView(&rows[i], inst))
	}
	// 内存过滤后手工分页。
	total = int64(len(out))
	start := (q.Page - 1) * q.PageSize
	if start > len(out) {
		start = len(out)
	}
	end := start + q.PageSize
	if end > len(out) {
		end = len(out)
	}
	return out[start:end], total, nil
}

// instIsMineTodo 报告 actor 是否为该实例当前待审批人。
func instIsMineTodo(def *Definition, inst *Instance, actor string) bool {
	if inst == nil || inst.Status != StatusRunning || actor == "" {
		return false
	}
	idx := nodeMap(*def)
	for _, key := range dedup(inst.Current) {
		n, ok := idx[key]
		if !ok || n.Kind != KindApproval {
			continue
		}
		approvers := effectiveApprovers(n, inst)
		if len(approvers) == 0 || contains(approvers, actor) {
			return true // 未配置审批人的节点：任意登录用户可审
		}
	}
	return false
}

// instIsMineDone 报告 actor 是否在该实例的流转历史里表过态。
func instIsMineDone(inst *Instance, actor string) bool {
	if inst == nil || actor == "" {
		return false
	}
	for _, e := range inst.History {
		if e.Actor == actor && (e.Action == ActionApprove || e.Action == ActionReject) {
			return true
		}
	}
	return false
}

// InstGet 返回实例详情。
func (s *Service) InstGet(ctx context.Context, id idgen.ID) (*InstView, error) {
	m, inst, err := s.instGet(ctx, id)
	if err != nil {
		return nil, err
	}
	v := toInstView(m, inst)
	return &v, nil
}

// StartInput 是发起流程的入参；BizType/BusinessKey 让宿主把流程实例挂到业务对象上。
type StartInput struct {
	DefID       string
	DefKey      string
	Title       string
	BizType     string
	BusinessKey string
	Vars        map[string]string
}

// InstStart 用一份已发布的定义发起实例：流程变量用于排他网关条件。
// 幂等：同 (biz_type, business_key) 已有运行中实例时直接返回它（终态后可再发起新实例）。
func (s *Service) InstStart(ctx context.Context, f StartInput, actor string) (*InstView, error) {
	actor = strings.TrimSpace(actor)
	if actor == "" {
		return nil, response.BadRequest("workflow.actor_required")
	}
	var def Def
	q := s.db.WithContext(ctx)
	if id, err := idgen.Parse(strings.TrimSpace(f.DefID)); err == nil && id != 0 {
		q = q.Where("id = ?", int64(id))
	} else if strings.TrimSpace(f.DefKey) != "" {
		q = q.Where("def_key = ?", strings.TrimSpace(f.DefKey))
	} else {
		return nil, response.BadRequest("workflow.def_required")
	}
	if err := q.Take(&def).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, response.NotFound("workflow.def_not_found")
		}
		return nil, err
	}
	if def.Status != DefPublished {
		return nil, response.Conflict("workflow.def_not_published")
	}
	var definition Definition
	if err := json.Unmarshal([]byte(def.Definition), &definition); err != nil {
		return nil, response.BadRequest("workflow.def_json_broken")
	}

	// 幂等：同业务键的运行中实例直接复用。
	bizType, bizKey := truncate(strings.TrimSpace(f.BizType), 32), truncate(strings.TrimSpace(f.BusinessKey), 64)
	if bizType != "" && bizKey != "" {
		var running Inst
		err := s.db.WithContext(ctx).Where("biz_type = ? AND business_key = ? AND status = ?", bizType, bizKey, StatusRunning).
			Take(&running).Error
		if err == nil {
			inst, derr := decodeState(&running)
			if derr == nil {
				v := toInstView(&running, inst)
				return &v, nil
			}
		} else if !errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, err
		}
	}

	inst, err := Start(definition, f.Vars, actor, time.Now().UTC())
	if err != nil {
		return nil, response.BadRequest("workflow.def_invalid").WithMsg(err.Error())
	}
	raw, err := json.Marshal(inst)
	if err != nil {
		return nil, err
	}
	m := Inst{
		DefID: def.ID, DefKey: def.DefKey,
		Title: truncate(strings.TrimSpace(f.Title), 200), BizType: bizType,
		BusinessKey: bizKey, Submitter: truncate(actor, 64),
		Status: inst.Status, StateJSON: string(raw),
	}
	if err := s.db.WithContext(ctx).Create(&m).Error; err != nil {
		return nil, err
	}
	v := toInstView(&m, inst)
	s.publish(ctx, EventStarted, &m, &definition, inst, actor, "")
	s.todoEvent(ctx, &m, &definition, inst, actor)
	return &v, nil
}

// instAct 是审批动作的公共流程：取实例 → 取定义 → 引擎计算 → 落库 → 事件。
func (s *Service) instAct(ctx context.Context, id idgen.ID, actor string, apply func(def Definition, inst *Instance, now time.Time) error) (*InstView, error) {
	if strings.TrimSpace(actor) == "" {
		return nil, response.BadRequest("workflow.actor_required")
	}
	m, inst, err := s.instGet(ctx, id)
	if err != nil {
		return nil, err
	}
	def, err := s.defGet(ctx, m.DefID)
	if err != nil {
		return nil, err
	}
	var definition Definition
	if err := json.Unmarshal([]byte(def.Definition), &definition); err != nil {
		return nil, response.BadRequest("workflow.def_json_broken")
	}
	before := inst.Status
	if err := apply(definition, inst, time.Now().UTC()); err != nil {
		return nil, response.BadRequest("workflow.action_rejected").WithMsg(err.Error())
	}
	raw, err := json.Marshal(inst)
	if err != nil {
		return nil, err
	}
	m.Status = inst.Status
	m.StateJSON = string(raw)
	if err := s.db.WithContext(ctx).Save(m).Error; err != nil {
		return nil, err
	}
	v := toInstView(m, inst)
	if before != StatusRunning {
		return &v, nil // 已结束的实例重复操作：保存幂等但不重复发事件
	}
	switch inst.Status {
	case StatusRunning:
		s.todoEvent(ctx, m, &definition, inst, actor)
	default:
		s.publish(ctx, EventCompleted, m, &definition, inst, actor, "")
	}
	return &v, nil
}

// InstApprove 通过当前节点（会签需全部同意才推进）。
func (s *Service) InstApprove(ctx context.Context, id idgen.ID, actor, comment string) (*InstView, error) {
	return s.instAct(ctx, id, actor, func(def Definition, inst *Instance, now time.Time) error {
		return Approve(def, inst, actor, comment, now)
	})
}

// InstReject 驳回：整条实例置为 rejected。
func (s *Service) InstReject(ctx context.Context, id idgen.ID, actor, comment string) (*InstView, error) {
	return s.instAct(ctx, id, actor, func(def Definition, inst *Instance, now time.Time) error {
		return Reject(def, inst, actor, comment, now)
	})
}

// InstCountersign 加签：给当前节点追加审批人。
func (s *Service) InstCountersign(ctx context.Context, id idgen.ID, actor, approver, comment string) (*InstView, error) {
	return s.instAct(ctx, id, actor, func(def Definition, inst *Instance, now time.Time) error {
		return Countersign(def, inst, actor, approver, comment, now)
	})
}

// InstRollback 回退到指定审批节点并清空其下游投票。
func (s *Service) InstRollback(ctx context.Context, id idgen.ID, actor, toNode, comment string) (*InstView, error) {
	return s.instAct(ctx, id, actor, func(def Definition, inst *Instance, now time.Time) error {
		return Rollback(def, inst, actor, toNode, comment, now)
	})
}

// InstEscalate 手动超时升级到当前节点的 EscalateTo。
func (s *Service) InstEscalate(ctx context.Context, id idgen.ID, actor, comment string) (*InstView, error) {
	return s.instAct(ctx, id, actor, func(def Definition, inst *Instance, now time.Time) error {
		return Escalate(def, inst, actor, comment, now)
	})
}

// InstWithdraw 撤回：仅提交人可把运行中的实例置为 cancelled。
func (s *Service) InstWithdraw(ctx context.Context, id idgen.ID, actor, comment string) (*InstView, error) {
	m, _, err := s.instGet(ctx, id)
	if err != nil {
		return nil, err
	}
	if m.Submitter != strings.TrimSpace(actor) {
		return nil, response.Forbidden("workflow.withdraw_not_owner")
	}
	return s.instAct(ctx, id, actor, func(_ Definition, in *Instance, now time.Time) error {
		return Withdraw(in, actor, comment, now)
	})
}

// SweepEscalate 清扫器：对「当前审批节点配置了 TimeoutMinutes 且等待超时」的运行中实例
// 自动升级。返回处理笔数；由 plugin.Starter 每分钟调用。
// 后台任务没有 HTTP 请求上下文：发现阶段用 tenant.Skip 做全局扫描，
// 推进实例时再切回实例所属租户（与 cron 的「全局扫描 + 按租户执行」一致），
// 避免绕过租户 scope 把台账 / 事件写到错误的租户。
func (s *Service) SweepEscalate(ctx context.Context, now time.Time) (int, error) {
	scan := tenant.Skip(ctx)
	var rows []Inst
	if err := s.db.WithContext(scan).Where("status = ?", StatusRunning).
		Order("id DESC").Limit(500).Find(&rows).Error; err != nil {
		return 0, err
	}
	defs := map[idgen.ID]Definition{}
	if len(rows) > 0 {
		var drows []Def
		if err := s.db.WithContext(scan).Find(&drows).Error; err == nil {
			for i := range drows {
				var d Definition
				if json.Unmarshal([]byte(drows[i].Definition), &d) == nil {
					defs[drows[i].ID] = d
				}
			}
		}
	}
	count := 0
	for i := range rows {
		inst, err := decodeState(&rows[i])
		if err != nil {
			continue
		}
		def, ok := defs[rows[i].DefID]
		if !ok {
			continue
		}
		idx := nodeMap(def)
		need := false
		for _, key := range dedup(inst.Current) {
			n, ok := idx[key]
			if !ok || n.Kind != KindApproval || n.TimeoutMinutes <= 0 || strings.TrimSpace(n.EscalateTo) == "" {
				continue
			}
			if now.Sub(rows[i].UpdatedAt) >= time.Duration(n.TimeoutMinutes)*time.Minute {
				need = true
				break
			}
		}
		if !need {
			continue
		}
		// 按实例所属租户执行，后面的 instGet/defGet/Save 和事件都带正确租户。
		instCtx := tenant.With(ctx, rows[i].TenantID)
		if _, err := s.InstEscalate(instCtx, rows[i].ID, "system", "超时自动升级"); err != nil {
			s.logWarn("workflow: auto escalate failed", "instId", rows[i].ID, "err", err.Error())
			continue
		}
		count++
	}
	return count, nil
}

func (s *Service) logWarn(msg string, keysAndValues ...any) {
	if s.log != nil {
		s.log.Warn(msg, keysAndValues...)
	}
}

func truncate(s string, n int) string {
	if len(s) > n {
		return s[:n]
	}
	return s
}
