package dept

import "nexadmin/core/event"

// deptEvent 构造 dept.* 事件载荷：id、名称、上级、状态和租户。
func deptEvent(d *Dept) event.DeptData {
	return event.DeptData{ID: d.ID, TenantID: d.TenantID, Name: d.Name, ParentID: d.ParentID, Status: d.Status}
}
