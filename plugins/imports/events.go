package imports

import (
	"context"
	"time"

	"nexadmin/core/event"
	"nexadmin/pkg/idgen"
)

// 事件目录条目与载荷。租户级:事件带 TenantID,openapi webhook 可外发。
const EventCompleted = "imports.completed"

// CatalogInfos 是插件声明的事件目录条目(plugin.go init 时调用 event.Register)。
func CatalogInfos() []event.Info {
	return []event.Info{
		{Name: EventCompleted, Description: "一次导入执行完成(含行级成功/失败计数)"},
	}
}

// EventData 是 imports.completed 事件载荷。
type EventData struct {
	JobID      idgen.ID  `json:"jobId"`
	Source     string    `json:"source"`
	FileName   string    `json:"fileName,omitempty"`
	TotalRows  int       `json:"totalRows"`
	OkRows     int       `json:"okRows"`
	FailRows   int       `json:"failRows"`
	Actor      string    `json:"actor,omitempty"`
	OccurredAt time.Time `json:"occurredAt"`
}

func (s *Service) publishCompleted(ctx context.Context, m *Job, actor string) {
	event.Publish(ctx, EventCompleted, EventData{
		JobID: m.ID, Source: m.Source, FileName: m.FileName,
		TotalRows: m.TotalRows, OkRows: m.OkRows, FailRows: m.FailRows,
		Actor: actor, OccurredAt: time.Now().UTC(),
	})
}
