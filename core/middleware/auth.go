package middleware

import (
	"strings"

	"github.com/gin-gonic/gin"

	"nexadmin/core/auth"
	"nexadmin/core/response"
	"nexadmin/core/tenant"
	"nexadmin/pkg/idgen"
)

// ParseToken 尝试解析 Bearer token；成功则写入 claims，失败不拦截（是否必须登录由 RequireLogin / Perm 决定）。
func ParseToken(svc *auth.Service) gin.HandlerFunc {
	return func(c *gin.Context) {
		if h := c.GetHeader("Authorization"); strings.HasPrefix(h, "Bearer ") {
			if cl, err := svc.ParseAccess(c.Request.Context(), strings.TrimPrefix(h, "Bearer ")); err == nil {
				auth.SetClaims(c, cl)
			}
		}
		c.Next()
	}
}

// RequireLogin 未登录返回 401（前端据此走 refresh 流程）。
func RequireLogin() gin.HandlerFunc {
	return func(c *gin.Context) {
		if auth.ClaimsFrom(c) == nil {
			response.Fail(c, response.Unauthorized("error.unauthorized"))
			return
		}
		c.Next()
	}
}

// RequireSuper 只允许超管（用于 /api/v1/admin）。
func RequireSuper() gin.HandlerFunc {
	return func(c *gin.Context) {
		cl := auth.ClaimsFrom(c)
		if cl == nil {
			response.Fail(c, response.Unauthorized("error.unauthorized"))
			return
		}
		if !cl.Super {
			response.Fail(c, response.Forbidden("error.forbidden"))
			return
		}
		c.Next()
	}
}

// Perm 要求登录并拥有权限点 code。登记权限点由 core.Perm 在注册路由时完成。
// 权限每次请求从 Provider.Profile 读取最新值（Provider 自行决定是否缓存）。
func Perm(code string) gin.HandlerFunc {
	return func(c *gin.Context) {
		cl := auth.ClaimsFrom(c)
		if cl == nil {
			response.Fail(c, response.Unauthorized("error.unauthorized"))
			return
		}
		if cl.Super {
			c.Next()
			return
		}
		if cl.Principal == auth.PrincipalApp { // 开放应用没有用户记录，权限已由开放平台中间件按应用角色算好
			if !auth.HasPerm(cl.Perms, code) {
				response.Fail(c, response.Forbidden("error.forbidden"))
				return
			}
			c.Next()
			return
		}
		svc := auth.Default()
		if svc == nil || svc.Provider() == nil {
			response.Fail(c, response.Unavailable("auth.provider_missing"))
			return
		}
		u, err := svc.Profile(c.Request.Context(), cl)
		if err != nil {
			response.Fail(c, err)
			return
		}
		if !auth.HasPerm(u.Perms, code) {
			response.Fail(c, response.Forbidden("error.forbidden"))
			return
		}
		c.Next()
	}
}

// DefaultTenantResolver 是默认解析链：域名绑定 > X-Tenant-ID > JWT claim > 默认租户。
// lookup 可为 nil（跳过域名这一级）。
func DefaultTenantResolver(lookup tenant.DomainLookup) tenant.Resolver {
	return func(c *gin.Context) (idgen.ID, bool) {
		if lookup != nil {
			host := c.Request.Host
			if i := strings.LastIndexByte(host, ':'); i > 0 && !strings.Contains(host[i:], "]") {
				host = host[:i]
			}
			if id, ok := lookup(host); ok {
				return id, true
			}
		}
		if h := c.GetHeader("X-Tenant-ID"); h != "" {
			if id, err := idgen.Parse(h); err == nil && id != 0 {
				return id, true
			}
		}
		if cl := auth.ClaimsFrom(c); cl != nil && cl.TenantID != 0 {
			return cl.TenantID, true
		}
		return tenant.DefaultID, true
	}
}

// Tenant 解析租户并注入 context。已登录的非超管用户，解析结果必须与令牌里的租户一致，
// 否则 403，防止通过 X-Tenant-ID / 域名越权访问其他租户；租户已过期或停用同样 403（reason TENANT_EXPIRED）。
func Tenant(r tenant.Resolver) gin.HandlerFunc {
	return func(c *gin.Context) {
		id, ok := r(c)
		if !ok {
			id = tenant.DefaultID
		}
		if cl := auth.ClaimsFrom(c); cl != nil && !cl.Super && cl.TenantID != 0 && cl.TenantID != id {
			response.Fail(c, response.Forbidden("error.tenant_mismatch"))
			return
		}
		ctx := tenant.With(c.Request.Context(), id)
		c.Request = c.Request.WithContext(ctx)
		// 租户过期/停用：已登录的非超管（含 PAT、应用签名调用）一律 403 TENANT_EXPIRED；登出放行，让前端能清理会话。
		if cl := auth.ClaimsFrom(c); cl != nil && !cl.Super && !strings.HasSuffix(c.Request.URL.Path, "/auth/logout") {
			tid := cl.TenantID
			if tid == 0 {
				tid = id
			}
			if err := tenant.CheckActive(ctx, tid); err != nil {
				response.Fail(c, err)
				return
			}
		}
		c.Next()
	}
}

// SkipTenant 让后续 handler 跳过租户隔离（超管端 /api/v1/admin）。
func SkipTenant() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Request = c.Request.WithContext(tenant.Skip(c.Request.Context()))
		c.Next()
	}
}
