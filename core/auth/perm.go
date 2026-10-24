package auth

import (
	"context"
	"strings"
	"sync"

	"github.com/gin-gonic/gin"
	"golang.org/x/crypto/bcrypt"

	"nexadmin/pkg/idgen"
)

const claimsKey = "nexadmin.claims"

type ctxClaimsKey struct{}

// SetClaims 把解析后的令牌载荷放进 gin.Context（由 middleware.ParseToken 调用），
// 同时放进 request context：模块里 db.WithContext(c.Request.Context()) 之后，
// GORM 回调、日志、审计等不依赖 gin 的代码也能用 ClaimsFromContext 取到当前用户。
func SetClaims(c *gin.Context, cl *Claims) {
	c.Set(claimsKey, cl)
	if c.Request != nil {
		c.Request = c.Request.WithContext(context.WithValue(c.Request.Context(), ctxClaimsKey{}, cl))
	}
}

// ClaimsFromContext 从 request context 读取当前用户的令牌载荷，未登录或不是请求内的 context 返回 nil。
func ClaimsFromContext(ctx context.Context) *Claims {
	cl, _ := ctx.Value(ctxClaimsKey{}).(*Claims)
	return cl
}

type ctxUserKey struct{}

type ctxUser struct {
	id   idgen.ID
	name string
}

// ContextWithUser 把“归属用户”放进 context，只用于审计、操作日志等归属记录（如后台任务以提交者的身份留痕）。
// 它刻意用独立的 context key，不会让 ClaimsFromContext 返回非 nil，也不构造 Claims：
// 权限校验、租户与数据范围判断都读不到它，因此不可能被当作已通过校验的登录态。
func ContextWithUser(ctx context.Context, id idgen.ID, name string) context.Context {
	return context.WithValue(ctx, ctxUserKey{}, ctxUser{id: id, name: name})
}

// UserFromContext 读取 ContextWithUser 放入的归属用户；没有（或 id 与名称都为空）时 ok=false。
// 归属读取方应先用 ClaimsFromContext，取不到再回落到它。
func UserFromContext(ctx context.Context) (id idgen.ID, name string, ok bool) {
	u, found := ctx.Value(ctxUserKey{}).(ctxUser)
	if !found || (u.id == 0 && u.name == "") {
		return 0, "", false
	}
	return u.id, u.name, true
}

// ClaimsFrom 读取当前请求的令牌载荷，未登录返回 nil。
func ClaimsFrom(c *gin.Context) *Claims {
	v, _ := c.Get(claimsKey)
	cl, _ := v.(*Claims)
	return cl
}

// HashPassword 用 bcrypt 生成密码哈希。
func HashPassword(pw string) (string, error) {
	b, err := bcrypt.GenerateFromPassword([]byte(pw), bcrypt.DefaultCost)
	return string(b), err
}

// CheckPassword 校验密码。
func CheckPassword(hash, pw string) bool {
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(pw)) == nil
}

var (
	permMu sync.Mutex
	perms  = map[string]struct{}{}
)

// RegisterPerm 登记权限点（core.Perm 在注册路由时调用）。
func RegisterPerm(code string) {
	permMu.Lock()
	perms[code] = struct{}{}
	permMu.Unlock()
}

// RegisteredPerms 返回已登记的权限点，供菜单/角色模块展示。
func RegisteredPerms() []string {
	permMu.Lock()
	defer permMu.Unlock()
	out := make([]string, 0, len(perms))
	for p := range perms {
		out = append(out, p)
	}
	return out
}

// HasPerm 判断 owned 是否满足 need："*" 匹配全部，"user:*" 匹配 "user:" 开头的权限点。
func HasPerm(owned []string, need string) bool {
	for _, p := range owned {
		if p == "*" || p == need {
			return true
		}
		if strings.HasSuffix(p, ":*") && strings.HasPrefix(need, strings.TrimSuffix(p, "*")) {
			return true
		}
	}
	return false
}
