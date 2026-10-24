// Package boot 是二开项目向框架入口（cmd/server）注入定制的唯一位置，归用户区，升级不会覆盖。
//
// cmd/server/main.go 属于框架区，二开者不要改它；需要传给 core.New 的 Option
// （自定义租户解析、域名映射、登录提供者等）在这里返回。
package boot

import "nexadmin/core"

// Options 返回追加到 core.New 的 Option。默认没有定制。
//
// 例：
//
//	return []core.Option{core.WithTenantResolver(myResolver)}
func Options() []core.Option { return nil }
