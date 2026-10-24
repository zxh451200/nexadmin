package mcp

import "time"

// SetBodyReadTimeout 缩短请求体读取时限，返回还原函数。
func SetBodyReadTimeout(d time.Duration) func() {
	prev := bodyReadTimeout
	bodyReadTimeout = d
	return func() { bodyReadTimeout = prev }
}
