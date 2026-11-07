// Package crm 是免费基础客户主数据插件（nex 生态收敛方案第 3 步的最小骨架）：
// 客户主数据 + 线索商机两张表 + 事件契约，让 hotel/mall 的 member 认同同一个
// 客户 ID。只做主数据与商机台账，不做销售自动化（无管线/评分/营销）。
// 遵守插件规范：只依赖 core/ 与 pkg/，表前缀 plg_crm_，权限码前缀 crm:，
// 接口路径 /api/v1/plugins/crm/...。
package crm

import (
	"context"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"

	"nexadmin/core"
	"nexadmin/core/event"
	"nexadmin/core/plugin"
)

var _ plugin.Starter = (*module)(nil)

func init() {
	event.Register(CatalogInfos()...)
	plugin.Register(plugin.Plugin{
		ID: "crm", Name: "客户主数据", Version: "0.1.0", Edition: plugin.Free,
		Module: &module{},
	})
}

// zapLog 把 *zap.Logger 适配成 Service 需要的最小日志面。
type zapLog struct{ l *zap.Logger }

func (z zapLog) Info(msg string, kv ...any)  { z.l.Sugar().Infow(msg, kv...) }
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

func (*module) Name() string { return "crm" }

// Register 注册管理端路由。
func (m *module) Register(r *gin.RouterGroup, d *core.Deps) {
	h := &handler{svc: m.service(d)}

	g := r.Group("/plugins/crm")
	g.GET("/stats", core.Perm("crm:list"), h.crmStats)
	g.GET("/customers", core.Perm("crm:list"), h.listCustomers)
	// 静态段注册在 /customers/:id 之前（与 pay /orders/stats 同一套匹配约定）。
	g.GET("/customers/export", core.Perm("crm:list"), h.exportCustomers)
	g.GET("/customers/pool", core.Perm("crm:list"), h.listPool)
	g.GET("/customers/tags", core.Perm("crm:list"), h.listCustomerTags)
	g.GET("/customers/fields", core.Perm("crm:list"), h.listFields)
	// 我的待跟进：到期未完成的跟进记录，静态段注册在 /customers/:id 之前。
	g.GET("/customers/todos", core.Perm("crm:list"), h.listTodos)
	// 字段定义是平台共享表（plg_crm_field，全租户共用一份），写入口只认平台级调用方。
	g.POST("/customers/fields", core.Perm("crm:edit"), core.RequirePlatform(), h.createField)
	g.PUT("/customers/fields/:id", core.Perm("crm:edit"), core.RequirePlatform(), h.updateField)
	g.DELETE("/customers/fields/:id", core.Perm("crm:edit"), core.RequirePlatform(), h.deleteField)
	g.GET("/customers/duplicates", core.Perm("crm:list"), h.listDuplicates)
	g.POST("/customers/batch-update", core.Perm("crm:edit"), core.Desc("批量修改客户归属/状态/等级"), h.batchUpdateCustomers)
	g.POST("/customers/batch-tags", core.Perm("crm:edit"), core.Desc("批量给客户追加标签"), h.batchAddTags)
	g.GET("/customers/:id", core.Perm("crm:list"), h.getCustomer)
	// 明文证件号：权限点归 realname 插件，且每次查看都进操作日志。
	g.GET("/customers/:id/idcard", core.Perm(PermRevealID), d.OperLogMiddleware("crm", "reveal-id"), h.revealID)
	g.POST("/customers", core.Perm("crm:edit"), h.createCustomer)
	g.PUT("/customers/:id", core.Perm("crm:edit"), h.updateCustomer)
	g.PUT("/customers/:id/tags", core.Perm("crm:edit"), h.setCustomerTags)
	g.DELETE("/customers/:id", core.Perm("crm:edit"), h.deleteCustomer)
	g.DELETE("/customers", core.Perm("crm:edit"), core.Desc("批量删除客户（连带联系人与跟进）"), h.deleteCustomers)
	g.POST("/customers/bind-external", core.Perm("crm:edit"), h.bindExternal)
	// 静态段 merge 注册在 /customers/:id 之前。
	g.POST("/customers/merge", core.Perm("crm:edit"), core.Desc("合并重复客户（关联记录整体迁移）"), h.mergeCustomers)
	g.POST("/customers/merge-preview", core.Perm("crm:edit"), h.previewMerge)
	g.GET("/customers/resolve-external", core.Perm("crm:list"), h.resolveExternal)
	// 公海：领取/释放是独立权限点——能编辑客户不等于能动归属。
	g.POST("/customers/:id/claim", core.Perm("crm:pool"), h.claimCustomer)
	g.POST("/customers/:id/release", core.Perm("crm:pool"), h.releaseCustomer)
	g.GET("/opportunities", core.Perm("crm:list"), h.listOpportunities)
	g.GET("/opportunities/:id", core.Perm("crm:list"), h.getOpportunity)
	g.POST("/opportunities", core.Perm("crm:opp"), h.createOpportunity)
	g.PUT("/opportunities/:id", core.Perm("crm:opp"), h.updateOpportunity)
	g.DELETE("/opportunities/:id", core.Perm("crm:opp"), h.deleteOpportunity)
	g.DELETE("/opportunities", core.Perm("crm:opp"), core.Desc("批量删除线索/商机"), h.deleteOpportunities)
	// 联系人与跟进记录：读写都继承客户的可见性（数据范围按 customer.owner 裁剪）。
	g.GET("/customers/:id/contacts", core.Perm("crm:list"), h.listContacts)
	g.POST("/customers/:id/contacts", core.Perm("crm:edit"), h.createContact)
	g.PUT("/contacts/:id", core.Perm("crm:edit"), h.updateContact)
	g.DELETE("/contacts/:id", core.Perm("crm:edit"), h.deleteContact)
	g.GET("/customers/:id/activities", core.Perm("crm:list"), h.listActivities)
	g.POST("/customers/:id/activities", core.Perm("crm:edit"), h.createActivity)
	g.PUT("/activities/:id/done", core.Perm("crm:edit"), h.markActivityDone)
	g.DELETE("/activities/:id", core.Perm("crm:edit"), h.deleteActivity)
}

// Start 实现 plugin.Starter：注入通知与时区，并启动到期待办的提醒清扫。
func (m *module) Start(ctx context.Context, d *core.Deps) error {
	s := m.service(d)
	// 站内信：待跟进到期提醒归属人；未注入时业务照常。
	if d.Notify != nil {
		lang := ""
		if d.Config != nil {
			lang = d.Config.DefaultLanguage()
		}
		s.WithNotify(d.Notify, lang)
	}
	if d.Config != nil {
		s.WithTimezone(d.Config.App.Timezone)
	}
	// 清扫：启动先扫一遍（重启后接上欠账），之后每 30 分钟一次。next_at 是日期粒度，
	// 一天多次扫只为「当天新建的到期待办」也能及时提醒，不需要更密。
	go func() {
		sweep := func() {
			n, err := s.SweepReminders(ctx, time.Now())
			switch {
			case err != nil:
				s.log.Warn("crm: todo reminder sweep failed", "err", err.Error())
			case n > 0 && d.Log != nil:
				d.Log.Sugar().Infow("crm: todo reminders sent", "count", n)
			}
		}
		sweep()
		t := time.NewTicker(30 * time.Minute)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				sweep()
			}
		}
	}()
	return nil
}
