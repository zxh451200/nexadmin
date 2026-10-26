package workflow

import (
	"context"
	"time"

	"nexadmin/core/event"
	"nexadmin/pkg/idgen"
)

// 事件目录条目与载荷。租户级实例：事件带 TenantID，openapi 的 webhook 分发器可外发。
const (
	EventStarted   = "workflow.started"
	EventTodo      = "workflow.todo"      // 实例到达审批节点：payload.approvers 是待审批人并集
	EventCompleted = "workflow.completed" // approved / rejected / cancelled
)

// CatalogInfos 是插件声明的事件目录条目（plugin.go init 时调用 event.Register）。
func CatalogInfos() []event.Info {
	return []event.Info{
		{Name: EventStarted, Description: "流程实例已发起"},
		{Name: EventTodo, Description: "流程等待审批（含待审批人）"},
		{Name: EventCompleted, Description: "流程实例已结束（approved/rejected/cancelled）"},
	}
}

// EventData 是 workflow.* 事件载荷。InstID/DefKey/BizType/BusinessKey 用于宿主定位业务单据；
// Approvers 是当前所有待审批审批人的并集（宿主可据此推送通知）。
type EventData struct {
	InstID      idgen.ID  `json:"instId"`
	DefKey      string    `json:"defKey"`
	Title       string    `json:"title,omitempty"`
	BizType     string    `json:"bizType,omitempty"`
	BusinessKey string    `json:"businessKey,omitempty"`
	Status      string    `json:"status"`
	Current     []string  `json:"current,omitempty"`
	Approvers   []string  `json:"approvers,omitempty"`
	Actor       string    `json:"actor,omitempty"`
	Comment     string    `json:"comment,omitempty"`
	OccurredAt  time.Time `json:"occurredAt"`
}

// publish 统一出口：def 可为 nil（此时不算待审批人）。
func (s *Service) publish(ctx context.Context, name string, m *Inst, def *Definition, inst *Instance, actor, comment string) {
	data := EventData{
		InstID: m.ID, DefKey: m.DefKey, Title: m.Title,
		BizType: m.BizType, BusinessKey: m.BusinessKey, Status: m.Status,
		Actor: actor, Comment: comment, OccurredAt: time.Now().UTC(),
	}
	if inst != nil {
		data.Current = dedup(inst.Current)
		if def != nil {
			idx := nodeMap(*def)
			seen := map[string]bool{}
			for _, key := range dedup(inst.Current) {
				n, ok := idx[key]
				if !ok || n.Kind != KindApproval {
					continue
				}
				for _, a := range effectiveApprovers(n, inst) {
					if !seen[a] {
						seen[a] = true
						data.Approvers = append(data.Approvers, a)
					}
				}
			}
		}
	}
	event.Publish(ctx, name, data)
}

// todoEvent 在实例到达审批节点时发 workflow.todo（仍运行中才发）。
func (s *Service) todoEvent(ctx context.Context, m *Inst, def *Definition, inst *Instance, actor string) {
	if inst != nil && inst.Status == StatusRunning {
		s.publish(ctx, EventTodo, m, def, inst, actor, "")
	}
}
