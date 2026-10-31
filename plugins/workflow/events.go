package workflow

import (
	"context"
	"time"

	"github.com/nexadmin/flowlite/runtime"
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

// EventData 是 workflow.* 事件载荷。
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

// handleEvent 把运行时事件桥接到框架事件总线。
func (s *Service) handleEvent(ctx context.Context, ev runtime.Event) {
	var name string
	switch ev.Name {
	case runtime.EventStarted:
		name = EventStarted
	case runtime.EventTodo:
		name = EventTodo
	case runtime.EventCompleted:
		name = EventCompleted
	default:
		// task_sync_failed / escalate_failed 等内部告警只记日志，不对外发布。
		s.warn("workflow: "+ev.Name, "instId", instIDOf(ev))
		return
	}
	instID, _ := idgen.Parse(ev.Inst.ID)
	data := EventData{
		InstID: instID, DefKey: ev.Inst.DefKey, Title: ev.Inst.Title,
		BizType: ev.Inst.BizType, BusinessKey: ev.Inst.BusinessKey, Status: ev.Inst.Status,
		Actor: ev.Actor, Comment: ev.Comment, OccurredAt: ev.At,
	}
	if ev.State != nil {
		data.Current = ev.State.Current
	}
	data.Approvers = ev.Approvers
	event.Publish(ctx, name, data)
}

func instIDOf(ev runtime.Event) string {
	if ev.Inst == nil {
		return ""
	}
	return ev.Inst.ID
}
