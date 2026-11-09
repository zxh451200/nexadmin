package codegen

import (
	"fmt"
	"strings"
)

// Kind 是列的逻辑类型（与引擎无关）。
type Kind string

const (
	KindString   Kind = "string"
	KindText     Kind = "text"
	KindInt      Kind = "int"
	KindFloat    Kind = "float"
	KindBool     Kind = "bool"
	KindDate     Kind = "date"
	KindDateTime Kind = "datetime"
	KindID       Kind = "id" // 以 _id 结尾的 BIGINT：雪花 ID，接口里序列化为字符串
	KindNone     Kind = ""   // 不支持的类型
)

// 表形态：决定模型基座与是否带租户条件。
const (
	ShapeTenant   = "tenant"   // id + tenant_id + created_at + updated_at：嵌入 database.TenantModel
	ShapePlatform = "platform" // id + created_at + updated_at：嵌入 database.PlatformModel
	ShapePlain    = "plain"    // 其余：显式 ID（有 tenant_id 则显式 TenantID）
)

// ColumnConfig 是一列的生成配置（请求体与推荐配置共用）。
type ColumnConfig struct {
	Name     string `json:"name" binding:"required,max=64"`
	List     bool   `json:"list"`
	Query    bool   `json:"query"`
	Form     bool   `json:"form"`
	Required bool   `json:"required"`
	Control  string `json:"control" binding:"max=16"`
	// QueryOp 是查询条件运算符：like / eq / ne / gt / ge / lt / le / between。
	// 空 = 按列类型取默认（短文本 like，其余 eq）。between 在服务端拆成 XFrom / XTo 两个参数。
	QueryOp string `json:"queryOp" binding:"max=16"`
	// Sort 是生成物里的字段顺序（1 起）；0 = 未指定，按表列顺序。
	Sort int `json:"sort"`
	// Dict 是字典编码（control = dict 时必填）：列表渲染 DictTag，表单 / 筛选渲染
	// DictSelect，选项在运行时从 /dicts/:code/items 按请求语言取。
	Dict string `json:"dict" binding:"max=48"`
}

// ColumnInfo 是 GET /tables/:name/columns 返回的一列。
type ColumnInfo struct {
	Name       string `json:"name"`
	Type       string `json:"type"`
	Kind       Kind   `json:"kind"`
	Nullable   bool   `json:"nullable"`
	PrimaryKey bool   `json:"primaryKey"`
	Comment    string `json:"comment"`
	System     bool   `json:"system"`
	Supported  bool   `json:"supported"`
	Reason     string `json:"reason,omitempty"`
	Length     int64  `json:"length,omitempty"`
	// Dict 是推荐配置里的字典编码（仅新建表定义了 dict 字段时非空）。
	Dict   string       `json:"dict,omitempty"`
	Config ColumnConfig `json:"config"`

	hasDefault bool
}

// TableSchema 是 /columns 的返回值。
type TableSchema struct {
	Table           string       `json:"table"`
	Shape           string       `json:"shape"`
	Tenant          bool         `json:"tenant"`
	Tree            bool         `json:"tree"`
	SuggestedModule string       `json:"suggestedModule"`
	Columns         []ColumnInfo `json:"columns"`
}

// 每种类型允许的表单控件（第一个是默认值）。dict 是短文本的字典下拉：
// 存储与 string 相同（VARCHAR），渲染交给框架的 DictSelect / DictTag。
var controlsByKind = map[Kind][]string{
	KindString: {"input", "textarea", "dict"}, KindText: {"textarea", "input"},
	KindInt: {"number", "input"}, KindFloat: {"number", "input"}, KindID: {"input"},
	KindBool: {"switch"}, KindDate: {"date"}, KindDateTime: {"datetime"},
}

// queryOpsByKind 是每种列类型允许的查询方式。字符串额外支持 LIKE；数值与日期支持
// 比较和区间；ID / 开关只做等值。
var queryOpsByKind = map[Kind][]string{
	KindString:   {"like", "eq", "ne"},
	KindInt:      {"eq", "ne", "gt", "ge", "lt", "le", "between"},
	KindFloat:    {"eq", "ne", "gt", "ge", "lt", "le", "between"},
	KindID:       {"eq", "ne"},
	KindBool:     {"eq"},
	KindDate:     {"eq", "ne", "gt", "ge", "lt", "le", "between"},
	KindDateTime: {"eq", "ne", "gt", "ge", "lt", "le", "between"},
}

func defaultQueryOp(k Kind) string {
	if k == KindString {
		return "like"
	}
	return "eq"
}

func queryable(k Kind) bool {
	return len(queryOpsByKind[k]) > 0
}

func requirable(k Kind) bool {
	return k == KindString || k == KindText || k == KindDate || k == KindDateTime
}

// classify 把数据库类型映射到逻辑类型。
func classify(c RawColumn) Kind {
	t := strings.ToLower(strings.TrimSpace(c.TypeName))
	full := strings.ToLower(strings.ReplaceAll(c.FullType, " ", ""))
	switch {
	case strings.HasPrefix(full, "tinyint(1)"), t == "bool", t == "boolean":
		return KindBool
	case t == "bigint" || t == "int8" || t == "bigserial":
		if strings.HasSuffix(c.Name, "_id") {
			return KindID
		}
		return KindInt
	case t == "tinyint" || t == "smallint" || t == "mediumint" || t == "int" || t == "integer" ||
		t == "int2" || t == "int4" || t == "smallserial" || t == "serial":
		return KindInt
	case t == "float" || t == "float4" || t == "float8" || t == "double" || t == "double precision" ||
		t == "real" || t == "decimal" || t == "numeric":
		return KindFloat
	case t == "varchar" || t == "char" || t == "bpchar" || t == "character varying" || t == "character" ||
		t == "nvarchar" || t == "nchar" || t == "varchar2" || t == "citext" || t == "uuid" || t == "enum":
		return KindString
	case t == "text" || t == "tinytext" || t == "mediumtext" || t == "longtext" || t == "json" || t == "jsonb" ||
		t == "clob" || t == "ntext":
		return KindText
	case t == "date":
		return KindDate
	case strings.HasPrefix(t, "timestamp") || strings.HasPrefix(t, "datetime"):
		return KindDateTime
	}
	return KindNone
}

// Analysis 是对一张表的分析结果，供推荐配置与渲染共用。
type Analysis struct {
	Table      string
	Shape      string
	Tenant     bool
	SoftDelete bool
	// Tree 表示这是树形表：有 parent_id（BIGINT）列，生成物用树形列表 + 父级下拉。
	Tree bool
	Cols []ColumnInfo
}

// analyze 校验表结构并给出默认推荐配置。不满足生成前提的表返回错误。
func analyze(table string, raw []RawColumn) (*Analysis, error) {
	if err := ValidTable(table); err != nil {
		return nil, err
	}
	if len(raw) == 0 {
		return nil, fmt.Errorf("table %q has no columns", table)
	}
	if len(raw) > 200 {
		return nil, fmt.Errorf("table %q has too many columns (%d > 200)", table, len(raw))
	}
	a := &Analysis{Table: table}
	var pks []string
	has := map[string]Kind{}
	for _, r := range raw {
		ci := ColumnInfo{Name: r.Name, Type: sanitizeComment(r.TypeName), Nullable: r.Nullable, PrimaryKey: r.PrimaryKey,
			Comment: sanitizeComment(r.Comment), Length: r.Length, Dict: r.Dict, hasDefault: r.HasDefault}
		switch {
		case ValidIdent(r.Name) != nil:
			ci.Reason = "invalid column name"
		default:
			ci.Kind = classify(r)
			if ci.Kind == KindNone {
				ci.Reason = "unsupported column type"
			} else {
				ci.Supported = true
				has[r.Name] = ci.Kind
			}
		}
		if r.PrimaryKey {
			pks = append(pks, r.Name)
		}
		a.Cols = append(a.Cols, ci)
	}
	if len(pks) != 1 || pks[0] != "id" {
		return nil, fmt.Errorf("table %q needs a single-column primary key named id", table)
	}
	for _, c := range a.Cols {
		if c.Name == "id" {
			if t := strings.ToLower(raw[indexOf(raw, "id")].TypeName); t != "bigint" && t != "int8" && t != "bigserial" {
				return nil, fmt.Errorf("table %q: primary key id must be BIGINT (snowflake id)", table)
			}
		}
	}
	if k, ok := has["tenant_id"]; ok {
		if k != KindID {
			return nil, fmt.Errorf("table %q: tenant_id must be BIGINT", table)
		}
		a.Tenant = true
	}
	stamps := has["created_at"] == KindDateTime && has["updated_at"] == KindDateTime
	switch {
	case a.Tenant && stamps:
		a.Shape = ShapeTenant
	case !a.Tenant && stamps:
		a.Shape = ShapePlatform
	default:
		a.Shape = ShapePlain
	}
	// 树形表：parent_id 指向本表父级；生成树形列表与父级下拉，而不是平铺分页。
	a.Tree = has["parent_id"] == KindID
	pascal, camel := map[string]string{}, map[string]string{}
	for i := range a.Cols {
		c := &a.Cols[i]
		if !c.Supported {
			continue
		}
		soft := c.Name == "deleted_at" && (c.Kind == KindDateTime || c.Kind == KindDate)
		if c.Name == "id" || c.Name == "tenant_id" || (a.Shape != ShapePlain && (c.Name == "created_at" || c.Name == "updated_at")) || soft {
			c.System = true
			if soft {
				a.SoftDelete = true
			}
			continue
		}
		p, cm := Pascal(c.Name), Camel(c.Name)
		if reservedCamel[strings.ToLower(cm)] {
			return nil, fmt.Errorf("column %q maps to a reserved name %q", c.Name, cm)
		}
		if o, dup := pascal[p]; dup {
			return nil, fmt.Errorf("columns %q and %q map to the same Go name %q", o, c.Name, p)
		}
		if o, dup := camel[cm]; dup {
			return nil, fmt.Errorf("columns %q and %q map to the same field name %q", o, c.Name, cm)
		}
		pascal[p], camel[cm] = c.Name, c.Name
	}
	a.recommend()
	return a, nil
}

func indexOf(raw []RawColumn, name string) int {
	for i, r := range raw {
		if r.Name == name {
			return i
		}
	}
	return 0
}

// recommend 生成默认推荐配置：列表最多 8 列、查询条件最多 4 个，表单含全部业务列。
func (a *Analysis) recommend() {
	lists, queries, strQueries := 0, 0, 0
	for i := range a.Cols {
		c := &a.Cols[i]
		c.Config = ColumnConfig{Name: c.Name}
		if !c.Supported || c.System {
			continue
		}
		c.Config.Control = controlsByKind[c.Kind][0]
		// 新建表定义了 dict 字段时，推荐配置直接用字典下拉；列信息带出编码，向导可改。
		if c.Dict != "" {
			c.Config.Control = "dict"
			c.Config.Dict = c.Dict
		}
		c.Config.Form = true
		c.Config.Required = requirable(c.Kind) && !c.Nullable && !c.hasDefault
		// 树形表的 parent_id 不进列表/查询：层级结构由树形控件表达。
		if c.Kind != KindText && !(a.Tree && c.Name == "parent_id") && lists < 8 {
			c.Config.List, lists = true, lists+1
		}
		if queries >= 4 || !queryable(c.Kind) {
			continue
		}
		switch {
		case c.Kind == KindString && strQueries < 2:
			c.Config.Query, strQueries, queries = true, strQueries+1, queries+1
		case c.Kind == KindBool, isStateName(c.Name) && (c.Kind == KindInt || c.Kind == KindString):
			c.Config.Query, queries = true, queries+1
		}
		if c.Config.Query {
			c.Config.QueryOp = defaultQueryOp(c.Kind)
		}
	}
}

func isStateName(n string) bool {
	for _, s := range []string{"status", "state", "type", "kind"} {
		if n == s || strings.HasSuffix(n, "_"+s) {
			return true
		}
	}
	return false
}

func (a *Analysis) schema() TableSchema {
	return TableSchema{Table: a.Table, Shape: a.Shape, Tenant: a.Tenant, Tree: a.Tree, SuggestedModule: suggestModule(a.Table), Columns: a.Cols}
}

// suggestModule 由表名推荐模块名：去掉 biz_ 前缀与下划线；不合法时退回全名，仍不合法则返回空串让用户自己填。
func suggestModule(table string) string {
	if m, ok := moduleCandidate(strings.TrimPrefix(table, bizPrefix)); ok {
		return m
	}
	if m, ok := moduleCandidate(table); ok {
		return m
	}
	return ""
}

func moduleCandidate(name string) (string, bool) {
	m := strings.ReplaceAll(name, "_", "")
	if len(m) > 31 {
		m = m[:31]
	}
	if ValidModule(m) != nil {
		return "", false
	}
	return m, true
}
