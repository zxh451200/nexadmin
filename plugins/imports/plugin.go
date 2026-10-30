// Package imports 是 NexAdmin 免费导入中心插件:通用 CSV / XLSX 导入(模板下载、表头映射、
// 必填校验、逐行隔离、错误行报告、导入台账)。业务模块通过 registry.go 的
// RegisterSource 注册「可导入的表」,插件负责其余一切。
// 只依赖 core/ 与 pkg/ (+x/text 转码),表前缀 plg_imports_,权限码前缀 imports:,
// 接口路径 /api/v1/plugins/imports/...。
package imports

import (
	"context"
	"sync"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"

	"nexadmin/core"
	"nexadmin/core/event"
	"nexadmin/core/plugin"
)

// 建表由运行时执行 plugins/imports/migrations/<引擎>/*.sql(安装时 CLI 拷贝),插件自己不做迁移。
var _ plugin.Starter = (*module)(nil)

func init() {
	event.Register(CatalogInfos()...)
	plugin.Register(plugin.Plugin{
		ID: "imports", Name: "导入中心", Version: "0.1.0", Edition: plugin.Free,
		Module: &module{},
	})
}

// zapLog 把 *zap.Logger 适配成 Service 需要的最小日志面。
type zapLog struct{ l *zap.Logger }

func (z zapLog) Warn(msg string, kv ...any)  { z.l.Sugar().Warnw(msg, kv...) }
func (z zapLog) Error(msg string, kv ...any) { z.l.Sugar().Errorw(msg, kv...) }

type module struct {
	once sync.Once
	svc  *Service
}

func (m *module) service(d *core.Deps) *Service {
	m.once.Do(func() {
		l := d.Log
		if l == nil {
			l = zap.NewNop()
		}
		m.svc = NewService(d.DB, zapLog{l})
		SetDefault(m.svc)
	})
	return m.svc
}

func (*module) Name() string { return "imports" }

// Register 注册路由，并收集实现了 SourceProvider 的业务模块。
func (m *module) Register(r *gin.RouterGroup, d *core.Deps) {
	collect(d.Modules, d.DB, func(msg string, err error) {
		if d.Log != nil {
			d.Log.Sugar().Warnw(msg, "err", err.Error())
		}
	})
	h := &handler{svc: m.service(d)}
	h.Register(r, d)
}

// Start 实现 plugin.Starter(P0 无后台任务;异步化 core/job 在 P1)。
func (m *module) Start(ctx context.Context, d *core.Deps) error {
	m.service(d)
	return nil
}
