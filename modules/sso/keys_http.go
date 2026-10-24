package sso

import (
	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
	"gorm.io/gorm"

	"nexadmin/core/auth"
	"nexadmin/core/response"
)

// keysList GET /sso/keys：JWKS 里仍有效的签名密钥（kid、创建时间、状态），不含任何密钥材料。
func (h *handler) keysList(c *gin.Context) {
	if h.p.keys == nil {
		response.Fail(c, response.Internal())
		return
	}
	response.OK(c, h.p.keys.list())
}

// keysRotate POST /sso/keys/rotate：生成新 key 并成为签名 key；旧 key 继续留在 JWKS 里 7 天（已签发令牌照常验证），之后自动移除。
func (h *handler) keysRotate(c *gin.Context) {
	if h.p.keys == nil {
		response.Fail(c, response.Internal())
		return
	}
	if err := h.p.keys.Rotate(); err != nil {
		response.Fail(c, response.Internal().Wrap(err))
		return
	}
	who := ""
	if cl := auth.ClaimsFrom(c); cl != nil {
		who = cl.Username
	}
	h.log.Info("sso: signing key rotated", zap.String("by", who), zap.String("kid", h.p.keys.active().kid))
	response.OK(c, h.p.keys.list())
}

// ensureOrgColumn 为升级前已建好的 sys_sso_app 补 allow_org_read 列（幂等；新装的建表脚本里已带该列）。
func ensureOrgColumn(db *gorm.DB) error {
	m := db.Migrator()
	if !m.HasTable(&App{}) || m.HasColumn(&App{}, "AllowOrgRead") {
		return nil
	}
	return m.AddColumn(&App{}, "AllowOrgRead")
}
