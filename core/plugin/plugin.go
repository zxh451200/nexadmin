// Package plugin 是源码插件的运行时：注册表、清单、授权校验、迁移、菜单写入与（热）挂载。
//
// 依赖方向：plugin -> core（单向）。core 不认识 plugin；由 cmd/server/main.go 调用 plugin.Mount(app) 装配。
//
// 插件作者在 backend/plugin.go 的 init() 里：
//
//	plugin.Register(plugin.Plugin{ID: "cron", Name: "定时任务", Version: "1.0.0", Edition: plugin.Pro, Module: &module{}})
//
// Module 如果同时实现了 Starter，会在路由挂载后被调用一次；进程退出时 ctx 被取消。
package plugin

import (
	"context"
	"sort"
	"sync"

	"nexadmin/core"
)

// Edition 是插件版本：free 不校验授权，pro 需要有效的授权文件。
type Edition string

const (
	Free Edition = "free"
	Pro  Edition = "pro"
)

// Plugin 是插件的注册信息。
type Plugin struct {
	ID      string
	Name    string
	Version string
	Edition Edition
	Module  core.Module
}

// Starter 是可选接口：Module 实现了它，框架在路由挂载后调用 Start。
// Start 必须尽快返回（长期运行的任务请自行起 goroutine，并监听 ctx）；进程退出时 ctx 被取消。
type Starter interface {
	Start(ctx context.Context, d *core.Deps) error
}

var (
	regMu    sync.Mutex
	registry []Plugin
)

// Register 登记一个插件，由插件包的 init() 调用。
func Register(p Plugin) {
	regMu.Lock()
	defer regMu.Unlock()
	registry = append(registry, p)
}

// registered 返回已登记插件的副本，按 ID 排序（顺序稳定）。
func registered() []Plugin {
	regMu.Lock()
	defer regMu.Unlock()
	out := append([]Plugin(nil), registry...)
	sort.SliceStable(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}
