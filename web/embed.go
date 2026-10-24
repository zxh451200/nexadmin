//go:build embedweb

// Package web 持有前端构建产物。带 -tags embedweb 构建时把 web/dist 嵌入二进制（scripts/build.sh 会带上）；
// 不带 tag（开发、CI、派生项目的 go build ./...）时见 embed_dev.go，不要求 web/dist 存在。
package web

import (
	"embed"
	"io/fs"
)

//go:embed all:dist
var dist embed.FS

// Dist 返回以 index.html 为根的前端产物文件系统；未嵌入或没有 index.html 时返回 nil。
func Dist() fs.FS {
	sub, err := fs.Sub(dist, "dist")
	if err != nil {
		return nil
	}
	if _, err := fs.Stat(sub, "index.html"); err != nil {
		return nil
	}
	return sub
}
