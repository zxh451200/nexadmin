package session

import (
	"github.com/gin-gonic/gin"

	"nexadmin/core/auth"
)

// Middleware 在已登录请求上登记会话（尽力而为，失败不影响请求）。
// 挂在每个用户都会频繁访问的接口上（消息铃铛、个人中心、在线用户页）即可。
func Middleware(s *Store) gin.HandlerFunc {
	return func(c *gin.Context) {
		if cl := auth.ClaimsFrom(c); cl != nil {
			ctx := c.Request.Context()
			_ = s.Touch(ctx, cl, c.ClientIP(), c.Request.UserAgent(), func() string {
				if svc := auth.Default(); svc != nil {
					if u, err := svc.Profile(ctx, cl); err == nil && u.Nickname != "" {
						return u.Nickname
					}
				}
				return cl.Username
			})
		}
		c.Next()
	}
}

// Current 返回当前请求所属的会话 ID。
func Current(c *gin.Context) string {
	cl := auth.ClaimsFrom(c)
	if cl == nil {
		return ""
	}
	return SID(cl.UserID, c.ClientIP(), c.Request.UserAgent())
}
