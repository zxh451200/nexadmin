package role

import "nexadmin/core/event"

// roleEvent 构造 role.* 事件载荷：id、名称、编码、状态和租户。
func roleEvent(r *Role) event.RoleData {
	return event.RoleData{ID: r.ID, TenantID: r.TenantID, Name: r.Name, Code: r.Code, Status: r.Status}
}
