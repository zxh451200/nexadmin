package identity

// 测试服务（回环地址上的假 LDAP / CAS）需要放行私网地址；SSRF 用例自己把开关改回去再恢复。
func init() { setAllowPrivateHosts(true) }
