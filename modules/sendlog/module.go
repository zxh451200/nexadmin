// Package sendlog 是发送日志：把经 core/notify 渠道投递的短信 / 邮件 / 机器人，以及 SendMail、验证码短信等直接发送路径
// 的结果统一记一条日志（表 sys_send_log）。
//
// core 不依赖本模块：core/notify 只定义了小接口 Recorder 和 SetRecorder，本模块在注册时把自己注入进去；
// 模块被停用（disabled_modules）时没有注入，发送路径上的记录点都是空操作。
// 红线：不存验证码、正文和密钥；收件人脱敏存储；错误信息脱敏并截断到 500 字；写入异步，失败只记 zap 日志。
package sendlog

import (
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"nexadmin/core"
	"nexadmin/modules/internal/smscode"
)

func init() { core.RegisterBuiltin(module{}) }

type module struct{}

func (module) Name() string { return "sendlog" }

// SeedOnStartup 实现 setup.StartupSeeder：每次启动补齐本模块的菜单与按钮（已存在的不动）。
func (module) SeedOnStartup(db *gorm.DB) error { return seedMenus(db) }

// Register 创建服务、注入各发送路径的 Recorder，并注册路由。
func (module) Register(r *gin.RouterGroup, d *core.Deps) {
	svc := NewService(d.DB, d.Config.App.Timezone, d.Log)
	if d.OnClose != nil {
		d.OnClose(svc.Close)
	}
	d.Notify.SetRecorder(svc) // nil 安全：手工构造的 Deps 没有 Notify 时什么也不做
	smscode.Shared(d.Config, d.Cache, d.Log).SetRecorder(svc)

	h := &Handler{svc: svc}
	r.GET("/sendlogs", core.Perm(PermList), h.List)
	r.GET("/sendlogs/:id", core.Perm(PermList), h.Get)
	r.DELETE("/sendlogs", core.Perm(PermDelete), h.Delete)
	r.POST("/sendlogs/clean", core.Perm(PermDelete), h.Clean)
}
