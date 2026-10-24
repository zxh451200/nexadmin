package codegen

import (
	"fmt"
	"go/token"
	"regexp"
	"strings"
	"unicode"
)

// 标识符白名单。进入生成代码的表名、列名、模块名、Go 模块路径都必须先过这里；
// 其余自由文本（列注释）只能经 sanitizeComment 压成单行纯文本后使用。
var (
	identRe  = regexp.MustCompile(`^[a-z][a-z0-9_]{0,47}$`)
	moduleRe = regexp.MustCompile(`^[a-z][a-z0-9]{0,30}$`)
	goModRe  = regexp.MustCompile(`^[a-z0-9][a-z0-9._~-]*(/[a-z0-9][a-z0-9._~-]*){0,6}$`)
)

// frameworkTablePrefixes / frameworkTables 是不允许生成代码的框架表。
var (
	frameworkTablePrefixes = []string{"sys_", "plg_"}
	frameworkTables        = map[string]bool{
		"schema_migrations": true, "schema_migration": true, "migrations": true, "gorm_migrations": true,
		"goose_db_version": true, "flyway_schema_history": true, "alembic_version": true,
	}
)

// 模块名不能与框架内置模块 / 路由重名，也不能是 Go 关键字。
var reservedModules = map[string]bool{
	"core": true, "pkg": true, "modules": true, "internal": true, "main": true, "init": true, "plugins": true,
	"example": true, "test": true, "about": true, "ai": true, "apimonitor": true, "cachemonitor": true,
	"clienterror": true, "dashboard": true, "exportcenter": true, "file": true, "log": true, "message": true,
	"notice": true, "online": true, "openapi": true, "plugin": true, "profile": true, "recycle": true,
	"region": true, "servermonitor": true, "sso": true, "system": true, "tenant": true, "backup": true,
	"config": true, "dept": true, "dict": true, "menu": true, "post": true, "role": true, "user": true,
	"auth": true, "setup": true, "setting": true, "settings": true, "upload": true, "metrics": true,
}

// 生成的类型名不能与生成物里固定存在的符号冲突（实体名 = Pascal(模块名)）。
var reservedEntities = map[string]bool{
	"Service": true, "Handler": true, "View": true, "Form": true, "ListQuery": true,
}

// 列名的 camelCase 不能是会污染对象原型的名字。
var reservedCamel = map[string]bool{"constructor": true, "prototype": true, "proto": true, "tostring": true}

// IsFrameworkTable 判断是否框架表（含迁移记录表）。
func IsFrameworkTable(name string) bool {
	n := strings.ToLower(name)
	for _, p := range frameworkTablePrefixes {
		if strings.HasPrefix(n, p) {
			return true
		}
	}
	return frameworkTables[n] || strings.HasSuffix(n, "_migrations")
}

// ValidIdent 校验表名 / 列名。
func ValidIdent(s string) error {
	if !identRe.MatchString(s) {
		return fmt.Errorf("invalid identifier %q (want %s)", s, identRe.String())
	}
	return nil
}

// ValidTable 校验表名：标识符合法且不是框架表。
func ValidTable(s string) error {
	if err := ValidIdent(s); err != nil {
		return err
	}
	if IsFrameworkTable(s) {
		return fmt.Errorf("table %q is a framework table", s)
	}
	return nil
}

// ValidModule 校验模块名（同时是 Go 包名与前端目录名）。
func ValidModule(s string) error {
	switch {
	case !moduleRe.MatchString(s):
		return fmt.Errorf("invalid module name %q (want %s)", s, moduleRe.String())
	case strings.HasPrefix(s, "sys"), strings.HasPrefix(s, "plg"):
		return fmt.Errorf("module name %q uses a framework prefix", s)
	case reservedModules[s], token.IsKeyword(s):
		return fmt.Errorf("module name %q is reserved", s)
	case reservedEntities[Pascal(s)]:
		return fmt.Errorf("module name %q collides with a generated symbol", s)
	}
	return nil
}

// ValidGoModule 校验用户项目的 Go 模块路径（用于 import 路径）。
func ValidGoModule(s string) error {
	if len(s) > 100 || !goModRe.MatchString(s) || strings.Contains(s, "..") {
		return fmt.Errorf("invalid go module path %q", s)
	}
	return nil
}

var initialisms = map[string]string{"id": "ID", "url": "URL", "uri": "URI", "api": "API", "ip": "IP", "html": "HTML", "json": "JSON", "sql": "SQL", "uuid": "UUID"}

func upperFirst(s string) string {
	if s == "" {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
}

// Pascal: published_at -> PublishedAt，owner_id -> OwnerID。入参必须已通过白名单。
func Pascal(s string) string {
	var b strings.Builder
	for _, p := range strings.Split(s, "_") {
		if v, ok := initialisms[p]; ok {
			b.WriteString(v)
		} else {
			b.WriteString(upperFirst(p))
		}
	}
	return b.String()
}

// Camel: published_at -> publishedAt（JSON / TS 字段名）。
func Camel(s string) string {
	parts := strings.Split(s, "_")
	for i := 1; i < len(parts); i++ {
		parts[i] = upperFirst(parts[i])
	}
	return strings.Join(parts, "")
}

// Title: published_at -> Published At（默认英文文案）。
func Title(s string) string {
	parts := strings.Split(s, "_")
	for i, p := range parts {
		parts[i] = upperFirst(p)
	}
	return strings.TrimSpace(strings.Join(parts, " "))
}

// Plural: article -> articles，category -> categories，box -> boxes。
func Plural(s string) string {
	switch {
	case strings.HasSuffix(s, "s"), strings.HasSuffix(s, "x"), strings.HasSuffix(s, "ch"), strings.HasSuffix(s, "sh"):
		return s + "es"
	case strings.HasSuffix(s, "y") && len(s) > 1 && !strings.ContainsRune("aeiou", rune(s[len(s)-2])):
		return s[:len(s)-1] + "ies"
	}
	return s + "s"
}

// sanitizeComment 把任意文本压成单行、可安全放进 // 注释和 JSON 文案的纯文本：
// 控制字符 / 换行 / 行分隔符 / 双向控制符 / 非可打印字符一律变空格，反引号变单引号，限 60 个字符。
func sanitizeComment(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch {
		case r == '`':
			b.WriteRune('\'')
		case r == ' ' || unicode.IsSpace(r) || !unicode.IsPrint(r):
			b.WriteRune(' ')
		default:
			b.WriteRune(r)
		}
	}
	out := strings.Join(strings.Fields(b.String()), " ")
	if r := []rune(out); len(r) > 60 {
		out = string(r[:60])
	}
	return out
}
