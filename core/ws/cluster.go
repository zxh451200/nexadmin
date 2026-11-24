package ws

import (
	"encoding/json"

	"nexadmin/core/cluster"
)

const kindRevoke = "ws.revoke"

type revokeEvent struct {
	TenantID string `json:"t,omitempty"`
	UserID   string `json:"u,omitempty"`
}

var revokeBus *cluster.Bus

// Wire 把本地断连动作广播到其它实例，并在订阅重连后关闭全部旧连接。
func Wire(bus *cluster.Bus, hub *Hub) {
	if bus == nil || hub == nil {
		return
	}
	revokeBus = bus
	bus.On(kindRevoke, func(raw json.RawMessage) {
		var event revokeEvent
		if json.Unmarshal(raw, &event) != nil {
			return
		}
		if event.UserID != "" {
			hub.KickUser(event.UserID, event.TenantID)
			return
		}
		hub.KickTenant(event.TenantID)
	}, hub.CloseAll)
}

// RevokeUser 关闭本地连接并尽力通知其它实例；安全版本号仍是令牌失效的权威依据。
func RevokeUser(userID, tenantID string) {
	if userID == "" {
		return
	}
	if hub := Default(); hub != nil {
		hub.KickUser(userID, tenantID)
	}
	if revokeBus != nil {
		revokeBus.Publish(kindRevoke, revokeEvent{TenantID: tenantID, UserID: userID})
	}
}

// RevokeTenant 关闭本地连接并尽力通知其它实例。
func RevokeTenant(tenantID string) {
	if tenantID == "" {
		return
	}
	if hub := Default(); hub != nil {
		hub.KickTenant(tenantID)
	}
	if revokeBus != nil {
		revokeBus.Publish(kindRevoke, revokeEvent{TenantID: tenantID})
	}
}
