// Package realname 是免费基础证件实名插件：身份证号格式校验与解析（归属地/生日/性别）、
// 二要素在线核验（外部厂商走 Provider，默认关闭）、核验结论台账留痕。
//
// 只依赖 core/ 与 pkg/，表前缀 plg_realname_，接口路径 /api/v1/plugins/realname/...。
// 台账刻意不存明文证件号与姓名：需要溯源时用号码摘要（sha256）定位，展示用脱敏串。
package realname

import (
	"time"

	"github.com/gin-gonic/gin"

	"nexadmin/core"
	"nexadmin/core/middleware"
	"nexadmin/core/plugin"
)

// 权限码与 plugin.yaml 的 menu.perm、前端 route.tsx 保持一致。
const (
	PermList   = "realname:list"
	PermVerify = "realname:verify"
	PermConfig = "realname:config"
	PermDelete = "realname:delete"
)

// 建表由运行时执行 plugins/realname/migrations/<引擎>/*.sql（安装时 CLI 拷贝），插件自己不做迁移。
var _ = plugin.Starter(nil)

func init() {
	plugin.Register(plugin.Plugin{
		ID: "realname", Name: "证件实名", Version: "0.1.0", Edition: plugin.Free,
		Module: &module{},
	})
}

type module struct{}

func (*module) Name() string { return "realname" }

func (*module) Register(r *gin.RouterGroup, d *core.Deps) {
	h := &handler{svc: NewService(d.DB, d.Cache, d.Log)}

	g := r.Group("/plugins/realname")
	g.POST("/parse", core.Perm(PermList), h.parse)
	g.GET("/verifications", core.Perm(PermList), h.list)
	// 台账是审计留痕：删除本身也要进操作日志。
	g.DELETE("/verifications",
		core.Perm(PermDelete),
		d.OperLogMiddleware("realname", "delete"),
		h.deleteVerifications,
	)
	g.GET("/config", core.Perm(PermConfig), h.getConfig)
	g.PUT("/config", core.Perm(PermConfig), h.putConfig)
	// 在线核验是按次计费的外部调用：限流 + 每日额度 + 行为留痕，缺任一层都会把账单和审计敞着。
	g.POST("/verify",
		core.Perm(PermVerify),
		middleware.RateLimit(d.Cache, d.Log, "realname-verify", middleware.ByUser(), 10, time.Minute),
		d.OperLogMiddleware("realname", "verify"),
		h.verify,
	)
}
