// Package docs 内嵌框架使用文档（本目录 *.md 与 en/*.md 译文），供官网 /docs 区块渲染。
// 文档只在此处维护一份：改 docs/*.md，重新构建后官网文档区即更新。
package docs

import "embed"

//go:embed *.md en/*.md
var Files embed.FS
