package main

import (
	"fmt"
	"regexp"
	"strings"
)

// 字段类型。--fields 里写 name:type 或 name:type(arg)。
const (
	kString   = "string"
	kText     = "text"
	kInt      = "int"
	kFloat    = "float"
	kBool     = "bool"
	kDate     = "date"
	kDateTime = "datetime"
	kDict     = "dict" // dict(字典编码)，enum(字典编码) 是它的别名
	kRef      = "ref"  // ref(目标资源)，如 ref(user) -> 远程搜索 /users
	kImage    = "image"
	kFile     = "file"
)

var (
	identRe = regexp.MustCompile(`^[a-z][a-z0-9_]*$`)
	nameRe  = regexp.MustCompile(`^[a-z][a-z0-9]{0,30}$`)
)

// 系统列与常见 SQL 保留字，不允许当字段名。
var reservedFields = map[string]bool{
	"id": true, "tenant_id": true, "created_at": true, "updated_at": true, "deleted_at": true, "created_by": true,
	"order": true, "group": true, "select": true, "table": true, "key": true, "desc": true, "index": true,
	"from": true, "where": true, "user": true, "limit": true, "offset": true, "default": true, "check": true,
}

// 不允许作模块名：Go 关键字、包名冲突、框架保留。
var reservedModules = map[string]bool{
	"core": true, "pkg": true, "modules": true, "internal": true, "main": true, "plugins": true, "example": true,
	"user": true, "order": true, "group": true, "table": true, "select": true, "default": true, "go": true,
	"func": true, "type": true, "var": true, "package": true, "import": true, "range": true, "map": true,
}

// Field 是 --fields 解析后的一个字段。
type Field struct {
	Name string // snake_case 列名
	Kind string
	Arg  string // dict 的字典编码 / ref 的目标资源
}

// splitTop 按逗号切分，括号内的逗号不切。
func splitTop(s string) []string {
	var out []string
	depth, start := 0, 0
	for i, r := range s {
		switch r {
		case '(':
			depth++
		case ')':
			depth--
		case ',':
			if depth == 0 {
				out = append(out, s[start:i])
				start = i + 1
			}
		}
	}
	return append(out, s[start:])
}

// parseFieldType 解析 "dict(sys_status)" 为 (dict, sys_status)。
func parseFieldType(s string) (kind, arg string, err error) {
	s = strings.TrimSpace(s)
	kind = s
	if i := strings.IndexByte(s, '('); i >= 0 {
		if !strings.HasSuffix(s, ")") {
			return "", "", fmt.Errorf("类型 %q 括号不匹配", s)
		}
		kind, arg = strings.TrimSpace(s[:i]), strings.TrimSpace(s[i+1:len(s)-1])
	}
	kind = strings.ToLower(kind)
	if kind == "enum" {
		kind = kDict
	}
	switch kind {
	case kString, kText, kInt, kFloat, kBool, kDate, kDateTime, kImage, kFile:
		if arg != "" {
			return "", "", fmt.Errorf("类型 %s 不接受参数 (%s)", kind, arg)
		}
	case kDict, kRef:
		if !identRe.MatchString(arg) {
			return "", "", fmt.Errorf("类型 %s 需要参数，如 %s(%s)", kind, kind, map[string]string{kDict: "sys_status", kRef: "user"}[kind])
		}
	default:
		return "", "", fmt.Errorf("不支持的字段类型 %q（可用: string text int float bool date datetime dict(code) ref(target) image file）", kind)
	}
	return kind, arg, nil
}

// parseFields 解析 --fields。空串返回空列表；名称重复、非法、保留字都报错。
func parseFields(s string) ([]Field, error) {
	if strings.TrimSpace(s) == "" {
		return nil, nil
	}
	var out []Field
	seen := map[string]bool{}
	for _, part := range splitTop(s) {
		part = strings.TrimSpace(part)
		if part == "" {
			return nil, fmt.Errorf("--fields 里有空项: %q", s)
		}
		i := strings.IndexByte(part, ':')
		if i < 0 {
			return nil, fmt.Errorf("字段 %q 缺少类型，格式 name:type", part)
		}
		name := strings.TrimSpace(part[:i])
		if !identRe.MatchString(name) || len(name) > 40 {
			return nil, fmt.Errorf("非法的字段名 %q（小写字母开头，只含小写字母数字下划线）", name)
		}
		if reservedFields[name] {
			return nil, fmt.Errorf("字段名 %q 是系统列或 SQL 保留字", name)
		}
		if seen[name] {
			return nil, fmt.Errorf("字段名 %q 重复", name)
		}
		seen[name] = true
		kind, arg, err := parseFieldType(part[i+1:])
		if err != nil {
			return nil, fmt.Errorf("字段 %s: %w", name, err)
		}
		out = append(out, Field{Name: name, Kind: kind, Arg: arg})
	}
	return out, nil
}

// validModuleName 校验模块名：小写字母开头的字母数字，不用 sys 前缀与保留名。
func validModuleName(n string) error {
	switch {
	case !nameRe.MatchString(n):
		return fmt.Errorf("非法的模块名 %q（小写字母开头，只含小写字母和数字，最长 31）", n)
	case strings.HasPrefix(n, "sys"):
		return fmt.Errorf("模块名 %q 不能以 sys 开头（该前缀归框架）", n)
	case reservedModules[n]:
		return fmt.Errorf("模块名 %q 是保留名", n)
	}
	return nil
}

// ---- 命名 ----

var initialisms = map[string]string{"id": "ID", "url": "URL", "uri": "URI", "api": "API", "ip": "IP", "html": "HTML", "json": "JSON", "sql": "SQL", "uuid": "UUID"}

func upperFirst(s string) string {
	if s == "" {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
}

// pascalName: published_at -> PublishedAt，owner_id -> OwnerID。
func pascalName(s string) string {
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

// camelName: published_at -> publishedAt（JSON / TS 字段名）。
func camelName(s string) string {
	parts := strings.Split(s, "_")
	for i := 1; i < len(parts); i++ {
		parts[i] = upperFirst(parts[i])
	}
	return strings.Join(parts, "")
}

// titleWords: published_at -> Published At（默认文案）。
func titleWords(s string) string {
	parts := strings.Split(s, "_")
	for i, p := range parts {
		parts[i] = upperFirst(p)
	}
	return strings.Join(parts, " ")
}

// pluralName: article -> articles，category -> categories，box -> boxes。
func pluralName(s string) string {
	switch {
	case strings.HasSuffix(s, "s"), strings.HasSuffix(s, "x"), strings.HasSuffix(s, "ch"), strings.HasSuffix(s, "sh"):
		return s + "es"
	case strings.HasSuffix(s, "y") && len(s) > 1 && !strings.ContainsRune("aeiou", rune(s[len(s)-2])):
		return s[:len(s)-1] + "ies"
	}
	return s + "s"
}

// ---- 类型映射（纯函数，可单测） ----

// goModelType 是 gorm 模型里的 Go 类型。
func goModelType(k string) string {
	switch k {
	case kInt:
		return "int"
	case kFloat:
		return "float64"
	case kBool:
		return "bool"
	case kDate, kDateTime:
		return "*time.Time"
	case kRef:
		return "idgen.ID"
	}
	return "string" // string text dict image file（图片/文件存 JSON 文本）
}

// goViewType 是接口返回里的 Go 类型。
func goViewType(k string) string {
	if k == kImage || k == kFile {
		return "json.RawMessage"
	}
	return goModelType(k)
}

// goFormType 是请求体里的 Go 类型；日期以字符串接收，服务端按多种格式解析。
func goFormType(k string) string {
	switch k {
	case kDate, kDateTime:
		return "*string"
	}
	return goViewType(k)
}

// tsType 是前端接口类型。
func tsType(k string) string {
	switch k {
	case kInt, kFloat:
		return "number"
	case kBool:
		return "boolean"
	case kImage, kFile:
		return "UploadedFile[]"
	case kDate, kDateTime:
		return "string | null"
	}
	return "string"
}

// controlFor 返回表单控件名（设计文档 §18「表单控件」）。
func controlFor(k string) string {
	switch k {
	case kDict:
		return "DictSelect"
	case kRef:
		return "RemoteSelect"
	case kBool:
		return "Switch"
	case kDate:
		return "DatePicker"
	case kDateTime:
		return "DateTimePicker"
	case kImage, kFile:
		return "Uploader"
	case kText:
		return "Textarea"
	}
	return "Input"
}

// sqlType 返回列定义（含 NULL/默认值）；engine 为 mysql|mariadb|postgres。
func sqlType(k, engine string) string {
	pg := engine == "postgres"
	switch k {
	case kText, kImage, kFile:
		return "TEXT"
	case kInt:
		return "INT NOT NULL DEFAULT 0"
	case kFloat:
		if pg {
			return "DOUBLE PRECISION NOT NULL DEFAULT 0"
		}
		return "DOUBLE NOT NULL DEFAULT 0"
	case kBool:
		if pg {
			return "BOOLEAN NOT NULL DEFAULT FALSE"
		}
		return "TINYINT(1) NOT NULL DEFAULT 0"
	case kDate:
		return "DATE NULL"
	case kDateTime:
		if pg {
			return "TIMESTAMP(3) NULL"
		}
		return "DATETIME(3) NULL"
	case kDict:
		return "VARCHAR(64) NOT NULL DEFAULT ''"
	case kRef:
		return "BIGINT NOT NULL DEFAULT 0"
	}
	return "VARCHAR(255) NOT NULL DEFAULT ''"
}

// refURL / refLabelKey: ref(user) -> /users，显示字段 nickname；其余默认 name。
func refURL(target string) string { return "/" + pluralName(target) }

func refLabelKey(target string) string {
	if target == "user" {
		return "nickname"
	}
	return "name"
}
