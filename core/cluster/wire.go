package cluster

import (
	"encoding/json"

	"nexadmin/core/tenant"
	"nexadmin/pkg/idgen"
)

type tenantInvalidate struct {
	IDs []idgen.ID `json:"ids,omitempty"`
	All bool       `json:"all,omitempty"`
}

// WireTenant 把租户信息缓存的失效接到总线上：本实例 tenant.Invalidate / InvalidateAll 会广播，
// 收到其它实例的广播就清本地缓存（InvalidateLocal，不再广播，不会回环）。
// 订阅重连后清空全部缓存，补上断线期间可能丢的通知。30 秒 TTL 仍是兜底。返回取消接线的函数。
func WireTenant(b *Bus) (unwire func()) {
	if !b.Distributed() {
		return func() {}
	}
	tenant.SetBroadcaster(func(ids []idgen.ID, all bool) {
		b.Publish(KindTenantInvalidate, tenantInvalidate{IDs: ids, All: all})
	})
	b.On(KindTenantInvalidate, func(raw json.RawMessage) {
		var ev tenantInvalidate
		if json.Unmarshal(raw, &ev) != nil {
			return
		}
		if ev.All {
			tenant.ResetCache()
			return
		}
		tenant.InvalidateLocal(ev.IDs...)
	}, tenant.ResetCache)
	return func() { tenant.SetBroadcaster(nil) }
}
