// Package docs 内嵌框架使用文档：本目录 *.md 固定为 zh-CN 中文源文。
// 译文目录按稳定语言 Code 小写命名，优先完整代码，再回退主语言码：
// 例如 pt-BR 使用 pt-br/<slug>.md 或 pt/<slug>.md，en-US 兼容 en/<slug>.md。
// 目录与站点默认语言、URL 前缀无关；新增语言只需加目录，重新构建即更新。
package docs

import "embed"

//go:embed *.md */*.md
var Files embed.FS
