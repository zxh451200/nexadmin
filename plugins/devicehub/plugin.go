// Package devicehub 是设备接入与监控 Pro 插件：Modbus TCP/RTU + MQTT + S7 + OPC UA 驱动接入变频器、
// PLC 与上位机网关，秒级采集（死区压缩）、实时监控（WS 推送）、指令下发（独立权限）、
// 阈值告警（站内信）、历史趋势与 xlsx 导出。协议差异收敛在 driver 接口后面。
package devicehub

import (
	"context"
	"sync"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"

	"nexadmin/core"
	"nexadmin/core/event"
	"nexadmin/core/plugin"
)

// 建表由运行时执行 plugins/devicehub/migrations/<引擎>/*.sql（安装时 CLI 拷贝），插件自己不做迁移。
var _ plugin.Starter = (*module)(nil)

func init() {
	event.Register(CatalogInfos()...)
	plugin.Register(plugin.Plugin{
		ID: "devicehub", Name: "设备接入与监控", Version: "0.1.0", Edition: plugin.Pro,
		Module: &module{},
	})
}

// zapLog 把 *zap.Logger 适配成 Service 需要的最小日志面（与 ticket 同款）。
type zapLog struct{ l *zap.Logger }

func (z zapLog) Warn(msg string, kv ...any)  { z.l.Sugar().Warnw(msg, kv...) }
func (z zapLog) Error(msg string, kv ...any) { z.l.Sugar().Errorw(msg, kv...) }

type module struct {
	once    sync.Once
	wsGuard sync.Once
	svc     *Service
	deps    *core.Deps
}

func (m *module) service(d *core.Deps) *Service {
	m.once.Do(func() {
		l := d.Log
		if l == nil {
			l = zap.NewNop()
		}
		m.deps = d
		m.svc = NewService(d.DB, zapLog{l})
		if d.Notify != nil {
			lang := ""
			if d.Config != nil {
				lang = d.Config.DefaultLanguage()
			}
			// 告警收件人：持有 devicehub:list 的用户（平台级数据按租户分组发，与 ticket 新单提醒同款）。
			m.svc.WithNotify(d.Notify, m.queryAlarmRecipients, lang)
		}
	})
	return m.svc
}

func (*module) Name() string { return "devicehub" }

// Register 注册路由与 WS 订阅守卫。
func (m *module) Register(r *gin.RouterGroup, d *core.Deps) {
	svc := m.service(d)
	h := &handler{svc: svc, log: d.Log}
	// WS 订阅守卫：devicehub: 前缀的准入判定（匿名拒绝 / 设备存在 / devicehub:list），见 ws_guard.go。
	m.wsGuard.Do(func() { registerWSTopicGuard(svc, d.Auth) })
	SetDefault(svc)
	h.Register(r, d)
}

// Start 实现 plugin.Starter：起采集 worker、指令 worker 与降采样 sweeper。
// 三者都随 ctx 取消整体退出（与 ticket 的清扫器同一模式）。
func (m *module) Start(ctx context.Context, d *core.Deps) error {
	s := m.service(d)
	go s.collector.Start(ctx)
	go s.commandLoop(ctx)
	go s.collector.downsampleLoop(ctx)
	return nil
}

// queryAlarmRecipients 解析持有 devicehub:list 的用户（平台级查询，与 website 的 queryHolders 同款 SQL）。
func (m *module) queryAlarmRecipients(ctx context.Context) ([]alarmRecipient, error) {
	d := m.deps
	if d == nil || d.DB == nil {
		return nil, nil
	}
	return queryDevicehubHolders(d.DB, ctx)
}
