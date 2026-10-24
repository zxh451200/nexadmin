package user

import "nexadmin/core/event"

// userEvent 构造 user.* 事件载荷：只含 id、账号、昵称、状态、部门和租户，不含密码哈希、手机号、邮箱。
func userEvent(u *User) event.UserData {
	return event.UserData{ID: u.ID, TenantID: u.TenantID, Username: u.Username, Nickname: u.Nickname, Status: u.Status, DeptID: u.DeptID}
}
