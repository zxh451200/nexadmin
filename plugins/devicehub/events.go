package devicehub

// devicehub.* 事件目录：宿主系统可进程内订阅或经 openapi webhook 消费。
// 载荷刻意保持最小（设备号/测点/值/时间），详情走 HTTP API 拉。

import (
	"context"

	"nexadmin/core/event"
)

// 事件名。
const (
	EventAlarm   = "devicehub.alarm"   // 阈值告警触发（含冷却）
	EventCommand = "devicehub.command" // 指令回执（done/failed/timeout）
	EventStatus  = "devicehub.status"  // 设备在线状态变化
)

// EventData 是事件载荷。
type EventData struct {
	DeviceNo string  `json:"deviceNo"`
	DeviceID int64   `json:"deviceId"`
	PointID  int64   `json:"pointId,omitempty"`
	Address  string  `json:"address,omitempty"`
	Value    float64 `json:"value,omitempty"`
	Level    string  `json:"level,omitempty"`
	Message  string  `json:"message,omitempty"`
}

// CatalogInfos 注册事件目录（openapi 事件清单与 webhook 可订阅范围）。
func CatalogInfos() []event.Info {
	return []event.Info{
		{Name: EventAlarm, Description: "测点越过阈值时触发（含冷却控制），载荷含设备/测点/当前值/级别。"},
		{Name: EventCommand, Description: "指令下发执行完成（成功/失败/超时），载荷含设备/测点/原始值与结果。"},
		{Name: EventStatus, Description: "设备在线状态变化（online/offline），载荷含设备与状态。"},
	}
}

// publish 是 service 侧的事件出口（与 ticket.publish 同款：nil 安全）。
func (s *Service) publish(ctx context.Context, name string, data EventData) {
	event.Publish(ctx, name, data)
}
