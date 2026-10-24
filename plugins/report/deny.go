package report

import (
	"regexp"
	"strings"
)

// 硬编码拒绝列表：无论插件配置、数据集、请求里写了什么，命中的表/列都不会进入报表查询。
// 配置只能在它之内收窄，不能放宽。命中判断先做归一化（大小写、引号/反引号/方括号、schema 前缀），
// 所以 `SYS_USER`、"public"."sys_user"、[sys_config] 这类变体一样被拒绝。

// identRe 是允许出现在元数据里的标识符：小写字母开头，小写字母/数字/下划线，最长 63（PostgreSQL 上限）。
// 引号、分号、注释符、空格、点号（schema 前缀）、大写都不匹配，因此即使绕过拒绝列表也进不了 SQL。
var identRe = regexp.MustCompile(`^[a-z][a-z0-9_]{0,62}$`)

// validIdent 报告 s 是否是可用于拼进 SQL 的安全标识符（严格匹配，不做归一化）。
func validIdent(s string) bool { return identRe.MatchString(s) }

// idSegments 把各种写法拆成归一化后的小写片段：去空白、去 ` " [ ]，按 "." 切分（schema.table.col）。
// 任何一段为空（如 "a..b"、"`"）都用空串占位，调用方一律按拒绝处理。
func idSegments(s string) []string {
	s = strings.ToLower(strings.TrimSpace(s))
	s = strings.NewReplacer("`", "", `"`, "", "[", "", "]", "", "'", "").Replace(s)
	parts := strings.Split(s, ".")
	for i := range parts {
		parts[i] = strings.TrimSpace(parts[i])
	}
	return parts
}

// sysAllowedTables 是 sys_ 前缀下唯一放行的内置表（登录日志，用于登录趋势统计）。
// 它写死在代码里，配置无法再追加；其余 sys_ 表一律拒绝。
var sysAllowedTables = map[string]bool{"sys_login_log": true}

// deniedTableExact 是明确点名拒绝的表（即使将来 sys_ 前缀规则被误改，这些表也不会放开）。
var deniedTableExact = map[string]bool{
	"sys_user": true, "sys_user_identity": true, "sys_user_role": true, "sys_user_post": true,
	"sys_config": true, "sys_setting": true, "sys_pat": true, "sys_openapi_app": true,
	"sys_webhook": true, "sys_webhook_delivery": true, "sys_sso_app": true, "sys_sso_refresh": true,
	"sys_tenant": true, "sys_tenant_pkg": true, "sys_backup": true, "sys_backup_setting": true,
	"sys_role": true, "sys_menu": true, "sys_dept": true,
}

var deniedTablePrefixes = []string{
	"sys_", "plg_report_", "information_schema", "performance_schema", "pg_", "mysql", "sqlite_", "gorm_",
}

var deniedTableSubstrings = []string{
	"migration", "secret", "token", "credential", "password", "passwd", "license", "apikey", "api_key", "session", "keystore",
}

// deniedTable 报告表是否被硬拒绝。入参可以是任意写法；schema 前缀的每一段都要检查
// （information_schema.columns、mysql.user 的 schema 段本身就被拒绝）。
func deniedTable(raw string) bool {
	for _, seg := range idSegments(raw) {
		if seg == "" || deniedTableName(seg) {
			return true
		}
	}
	return false
}

func deniedTableName(t string) bool {
	if sysAllowedTables[t] {
		return false
	}
	if deniedTableExact[t] {
		return true
	}
	for _, p := range deniedTablePrefixes {
		if strings.HasPrefix(t, p) {
			return true
		}
	}
	for _, s := range deniedTableSubstrings {
		if strings.Contains(t, s) {
			return true
		}
	}
	return false
}

// 列名里出现这些子串即拒绝（密码哈希、令牌、密钥、secret 类）。
var deniedColumnSubstrings = []string{
	"password", "passwd", "pwd", "secret", "token", "credential", "hash", "cipher", "signature",
	"apikey", "api_key", "access_key", "private", "privkey", "encrypt",
}

// 列名按下划线切分后，任一段等于这些词即拒绝（避免 "footprint" 这类误伤，同时拦住 totp_seed、auth_code）。
var deniedColumnParts = map[string]bool{
	"key": true, "salt": true, "otp": true, "totp": true, "mfa": true, "sign": true,
	"auth": true, "cookie": true, "session": true, "seed": true, "pin": true, "jwt": true,
}

// deniedColumn 报告列是否被硬拒绝。入参可以是任意写法（含 table.col、引号），每一段都要检查。
func deniedColumn(raw string) bool {
	for _, seg := range idSegments(raw) {
		if seg == "" || deniedColumnName(seg) {
			return true
		}
	}
	return false
}

func deniedColumnName(c string) bool {
	for _, s := range deniedColumnSubstrings {
		if strings.Contains(c, s) {
			return true
		}
	}
	for _, p := range strings.Split(c, "_") {
		if deniedColumnParts[p] {
			return true
		}
	}
	return false
}
