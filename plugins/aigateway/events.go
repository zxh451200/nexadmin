package aigateway

import (
	"time"

	"nexadmin/core/event"
	"nexadmin/pkg/idgen"
)

// 用量事件：宿主/BI 订阅做准实时计费或告警；台账 plg_aigateway_usage 是持久事实。
const EventUsageRecorded = "aigateway.usage.recorded"

// CatalogInfos 是插件声明的事件目录条目（plugin.go init 时调用 event.Register）。
func CatalogInfos() []event.Info {
	return []event.Info{
		{Name: EventUsageRecorded, Description: "模型调用计量已入账（按租户 token 台账）"},
	}
}

// UsageEventData 是 aigateway.usage.recorded 事件载荷：只含计量字段，不含任何密钥。
type UsageEventData struct {
	TenantID         idgen.ID  `json:"-"`
	UsageID          idgen.ID  `json:"usageId"`
	ModelID          idgen.ID  `json:"modelId"`
	Kind             string    `json:"kind"`
	Provider         string    `json:"provider,omitempty"`
	Model            string    `json:"model,omitempty"`
	PromptTokens     int64     `json:"promptTokens"`
	CompletionTokens int64     `json:"completionTokens"`
	TotalTokens      int64     `json:"totalTokens"`
	BizType          string    `json:"bizType,omitempty"`
	BizNo            string    `json:"bizNo,omitempty"`
	OccurredAt       time.Time `json:"occurredAt"`
}

// EventTenant 实现租户路由。
func (d UsageEventData) EventTenant() idgen.ID { return d.TenantID }
