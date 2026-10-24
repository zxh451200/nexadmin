//go:build !embedweb

// Package web 持有前端构建产物；未带 embedweb tag 时不嵌入（前端走 vite dev server）。
package web

import "io/fs"

// Dist 开发构建恒返回 nil：core 不托管静态资源，行为与改动前一致。
func Dist() fs.FS { return nil }
