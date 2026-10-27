package crm

import (
	"context"
	"time"

	"nexadmin/core/event"
	"nexadmin/pkg/idgen"
)

// 事件契约（收敛方案第 3 步）：宿主模块订阅 crm.* 事件对齐自己的 member/客户表。
// 载荷只含定位字段，不含凭据；TenantID 用于 openapi webhook 按租户外发。
const (
	EventCustomerCreated = "crm.customer.created"
	EventCustomerUpdated = "crm.customer.updated"
	EventOppStageChanged = "crm.opportunity.stage_changed"
)

// CatalogInfos 是插件声明的事件目录条目（plugin.go init 时调用 event.Register）。
func CatalogInfos() []event.Info {
	return []event.Info{
		{Name: EventCustomerCreated, Description: "客户主数据已创建（payload 含 external 锚点，供 member 对齐客户 ID）"},
		{Name: EventCustomerUpdated, Description: "客户主数据已更新"},
		{Name: EventOppStageChanged, Description: "线索/商机阶段已变更（won/lost 为终态）"},
	}
}

// CustomerEventData 是 crm.customer.* 事件载荷。
type CustomerEventData struct {
	TenantID       idgen.ID  `json:"-"`
	CustomerID     idgen.ID  `json:"customerId"`
	Name           string    `json:"name"`
	Code           string    `json:"code,omitempty"`
	Status         string    `json:"status"`
	ExternalSource string    `json:"externalSource,omitempty"`
	ExternalNo     string    `json:"externalNo,omitempty"`
	OccurredAt     time.Time `json:"occurredAt"`
}

// EventTenant 实现租户路由。
func (d CustomerEventData) EventTenant() idgen.ID { return d.TenantID }

// OppEventData 是 crm.opportunity.stage_changed 事件载荷。
type OppEventData struct {
	TenantID      idgen.ID  `json:"-"`
	OpportunityID idgen.ID  `json:"opportunityId"`
	CustomerID    idgen.ID  `json:"customerId"`
	Title         string    `json:"title"`
	FromStage     string    `json:"fromStage"`
	ToStage       string    `json:"toStage"`
	AmountMinor   int64     `json:"amountMinor,omitempty"`
	Currency      string    `json:"currency,omitempty"`
	OccurredAt    time.Time `json:"occurredAt"`
}

// EventTenant 实现租户路由。
func (d OppEventData) EventTenant() idgen.ID { return d.TenantID }

// publishCustomer 把客户事件发布到总线（进程内订阅满足宿主对齐；
// webhook 外发由框架 openapi 分发器按 TenantID 完成）。
func (s *Service) publishCustomer(ctx context.Context, name string, c *Customer) {
	data := CustomerEventData{
		TenantID: c.TenantID, CustomerID: c.ID, Name: c.Name, Code: c.Code, Status: c.Status,
		OccurredAt: time.Now().UTC(),
	}
	if c.ExternalSource != nil {
		data.ExternalSource = *c.ExternalSource
	}
	if c.ExternalNo != nil {
		data.ExternalNo = *c.ExternalNo
	}
	event.Publish(ctx, name, data)
}

// publishOppStage 把商机阶段变更发布到总线。
func (s *Service) publishOppStage(ctx context.Context, o *Opportunity, from string) {
	event.Publish(ctx, EventOppStageChanged, OppEventData{
		TenantID: o.TenantID, OpportunityID: o.ID, CustomerID: idgen.ID(o.CustomerID),
		Title: o.Title, FromStage: from, ToStage: o.Stage,
		AmountMinor: o.AmountMinor, Currency: o.Currency,
		OccurredAt: time.Now().UTC(),
	})
}
