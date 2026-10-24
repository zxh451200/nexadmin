package user

import (
	"context"
	"errors"
	"regexp"
	"strings"

	"nexadmin/core/database"
	"nexadmin/core/response"
	"nexadmin/pkg/idgen"
	"nexadmin/pkg/oauth"
)

// identityRow 对应 sys_user_identity：第三方账号 → 本地用户（租户内唯一）。
type identityRow struct {
	database.TenantModel
	UserID       idgen.ID
	Provider     string
	ExternalID   string
	ExternalName string
}

func (identityRow) TableName() string { return "sys_user_identity" }

// errOAuthFailed 是第三方登录一切失败的统一回复：state 无效、换取身份失败、域名不允许、账号停用、匹配歧义……
// 外部看不出具体原因（防枚举、防探测）；真实原因只写服务端日志。
func errOAuthFailed() error {
	return response.BadRequest("auth.oauth_failed").WithMsg("第三方登录失败，请重试")
}

var errOAuthConflict = errors.New("oauth: identity already linked")

var cnMobile = regexp.MustCompile(`^1[3-9]\d{9}$`)

// normalizePhone 去掉空格、横线与 +86/86 前缀，返回 11 位大陆手机号；不是则返回空串。
func normalizePhone(s string) string {
	s = strings.NewReplacer(" ", "", "-", "").Replace(strings.TrimSpace(s))
	s = strings.TrimPrefix(s, "+")
	if len(s) == 13 && strings.HasPrefix(s, "86") {
		s = s[2:]
	}
	if cnMobile.MatchString(s) {
		return s
	}
	return ""
}

// domainAllowed 实现 allowed_email_domains：未配置不限制；配置后必须有“已验证”的邮箱且域名在名单内。
func domainAllowed(allowed []string, id *oauth.Identity) bool {
	if len(allowed) == 0 {
		return true
	}
	if !id.EmailVerified {
		return false
	}
	at := strings.LastIndex(id.Email, "@")
	if strings.Count(id.Email, "@") != 1 || at <= 0 || at == len(id.Email)-1 {
		return false
	}
	dom := strings.ToLower(id.Email[at+1:])
	for _, d := range allowed {
		if dom == strings.ToLower(strings.TrimPrefix(d, "@")) {
			return true
		}
	}
	return false
}

// matchTarget 按 match_by 取出用于匹配本地账号的列与值。ok=false 表示这次不能自动匹配：
// 邮箱只认已验证的；手机号必须是合法的 11 位号码；工号对应本地 username。
func matchTarget(by string, id *oauth.Identity) (column, value string, ok bool) {
	switch by {
	case "phone":
		if p := normalizePhone(id.Phone); p != "" {
			return "phone", p, true
		}
	case "email":
		if e := strings.ToLower(strings.TrimSpace(id.Email)); id.EmailVerified && e != "" {
			return "email", e, true
		}
	case "employee_no", "employeeno":
		if n := strings.TrimSpace(id.EmployeeNo); n != "" {
			return "username", n, true
		}
	}
	return "", "", false
}

// loginOutcome 是第三方登录一次回调的结果。
type loginOutcome struct {
	User      *User
	NeedBind  bool
	BindToken string
}

// ctxErr 判断是否为 context 取消/超时，这类错误按原样上抛，不当作“登录失败”吞掉。
func ctxErr(err error) bool {
	return errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded)
}
