package online

import (
	"github.com/gin-gonic/gin"

	"nexadmin/core"
	"nexadmin/modules/internal/session"
)

func init() { core.RegisterBuiltin(module{}) }

// 在线用户模块：无表，基于 core/cache 里的会话索引（见 modules/internal/session）。
type module struct{}

func (module) Name() string { return "online" }

// Register 注册路由：列表需要 online:list，强退需要 online:forceLogout。
func (module) Register(r *gin.RouterGroup, d *core.Deps) {
	store := session.Shared(d.Cache, d.Config.Auth.RefreshTTL)
	h := &Handler{store: store}
	r.GET("/online-users", session.Middleware(store), core.Perm("online:list"), h.List)
	r.DELETE("/online-users/:id", core.Perm("online:forceLogout"), h.Kick)
}
