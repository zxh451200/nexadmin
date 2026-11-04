package codegen

import (
	"fmt"
	"strings"
)

// Field 是生成物里的一个业务字段（模板输入）。所有字符串要么是白名单标识符，要么经 sanitizeComment。
type Field struct {
	Name, Pascal, Camel, Title string
	Comment                    string // 已 sanitizeComment；中文文案优先用它
	Kind                       Kind
	GoType, FormType, TSType   string
	Control                    string
	// Dict 是字典编码（Control = dict 时非空）：ident 白名单内的 [a-z][a-z0-9_]，
	// 可安全嵌进生成的 TSX 属性。
	Dict                  string
	MaxLen                int
	Nullable              bool
	Required, List, Query bool
	Form                  bool
}

// Label 返回中文文案。
func (f Field) LabelZh() string {
	if f.Comment != "" {
		return f.Comment
	}
	return f.Title
}

// IsDict 报告这一列是否用字典下拉渲染。
func (f Field) IsDict() bool { return f.Control == "dict" && f.Dict != "" }

// Spec 是一次生成的全部输入（已校验）。
type Spec struct {
	GoModule   string
	Module     string
	Pascal     string
	Plural     string
	Table      string
	Shape      string
	Tenant     bool
	SoftDelete bool
	// Tree 表示树形表：有 parent_id 自引用，列表用树形控件、表单用父级下拉，不分页。
	Tree       bool
	Fields     []Field // 模型字段：不含 id / tenant_id（以及嵌入基座已有的 created_at / updated_at）
	Migrations []File  // 新建表模式附带的三引擎迁移；已有表模式为空
	Sub        *SubSpec
}

// SubSpec 是主子表模式的子表部分：子行随主表整存整取（每次保存按主表重写全部子行）。
type SubSpec struct {
	Table      string
	Pascal     string // 子表实体名（取子表名末段）
	JSONPlural string // 主表 Form 里子行数组的 JSON 键（末段复数的 camelCase）
	FK         string // 子表里指向主表的列名（snake_case）
	FKPascal   string
	Shape      string
	Tenant     bool
	SoftDelete bool
	Fields     []Field // 可编辑的子行业务列（不含 FK 与系统列）
}

// FormFields 是子行编辑器里可编辑的字段。
func (s *SubSpec) FormFields() []Field {
	var out []Field
	for _, f := range s.Fields {
		if f.Form {
			out = append(out, f)
		}
	}
	return out
}

// HasStamps: 子表嵌入基座（保存时一并写入时间戳）。
func (s *SubSpec) HasStamps() bool { return s.Shape != ShapePlain }

// HasStringForm: 子行编辑器里有短文本字段。
func (s *SubSpec) HasStringForm() bool {
	for _, f := range s.Fields {
		if f.Form && f.Kind == KindString {
			return true
		}
	}
	return false
}

func goType(k Kind) string {
	switch k {
	case KindInt:
		return "int64"
	case KindFloat:
		return "float64"
	case KindBool:
		return "bool"
	case KindID:
		return "idgen.ID"
	case KindDate, KindDateTime:
		return "*time.Time"
	}
	return "string"
}

func formType(k Kind) string {
	if k == KindDate || k == KindDateTime {
		return "*string"
	}
	return goType(k)
}

func tsType(k Kind) string {
	switch k {
	case KindInt, KindFloat:
		return "number"
	case KindBool:
		return "boolean"
	case KindDate, KindDateTime:
		return "string | null"
	}
	return "string"
}

// buildSpec 校验请求配置并组装 Spec。未出现在 cfg 里的列沿用推荐配置。
func buildSpec(a *Analysis, goModule, module string, cfg []ColumnConfig) (*Spec, error) {
	if err := ValidModule(module); err != nil {
		return nil, err
	}
	if goModule == "" {
		m, err := detectGoModule()
		if err != nil {
			return nil, err
		}
		goModule = m
	}
	if err := ValidGoModule(goModule); err != nil {
		return nil, err
	}
	if len(cfg) > 200 {
		return nil, fmt.Errorf("too many column configs")
	}
	byName := map[string]*ColumnInfo{}
	for i := range a.Cols {
		byName[a.Cols[i].Name] = &a.Cols[i]
	}
	eff := map[string]ColumnConfig{}
	for name, c := range byName {
		eff[name] = c.Config
	}
	seen := map[string]bool{}
	for _, cc := range cfg {
		if err := ValidIdent(cc.Name); err != nil {
			return nil, err
		}
		col, ok := byName[cc.Name]
		if !ok {
			return nil, fmt.Errorf("column %q does not exist in table %q", cc.Name, a.Table)
		}
		if seen[cc.Name] {
			return nil, fmt.Errorf("column %q configured twice", cc.Name)
		}
		seen[cc.Name] = true
		if !col.Supported || col.System {
			if cc.List || cc.Query || cc.Form || cc.Required {
				return nil, fmt.Errorf("column %q cannot be configured", cc.Name)
			}
			continue
		}
		adj, err := validateColumnCfg(col, cc)
		if err != nil {
			return nil, err
		}
		eff[cc.Name] = adj
	}

	s := &Spec{GoModule: goModule, Module: module, Pascal: Pascal(module), Plural: Plural(module),
		Table: a.Table, Shape: a.Shape, Tenant: a.Tenant, SoftDelete: a.SoftDelete, Tree: a.Tree}
	for _, col := range a.Cols {
		if !col.Supported || col.System {
			continue
		}
		s.Fields = append(s.Fields, newField(col, eff[col.Name]))
	}
	return s, nil
}

// validateColumnCfg 校验用户提交的单列配置并补齐默认值（控件、字典编码）。
func validateColumnCfg(col *ColumnInfo, cc ColumnConfig) (ColumnConfig, error) {
	if cc.Query && !queryable(col.Kind) {
		return cc, fmt.Errorf("column %q (%s) does not support query", cc.Name, col.Kind)
	}
	if cc.Required && !requirable(col.Kind) {
		return cc, fmt.Errorf("column %q (%s) cannot be required", cc.Name, col.Kind)
	}
	if cc.Required && !cc.Form {
		return cc, fmt.Errorf("column %q is required but not in the form", cc.Name)
	}
	ctl := cc.Control
	if ctl == "" {
		ctl = controlsByKind[col.Kind][0]
	}
	if !contains(controlsByKind[col.Kind], ctl) {
		return cc, fmt.Errorf("column %q: control %q not allowed for %s", cc.Name, ctl, col.Kind)
	}
	cc.Control = ctl
	if ctl == "dict" {
		if col.Kind != KindString {
			return cc, fmt.Errorf("column %q: dict control is only for string columns", cc.Name)
		}
		if err := ValidIdent(cc.Dict); err != nil {
			return cc, fmt.Errorf("column %q: invalid dict code %q", cc.Name, cc.Dict)
		}
	} else {
		cc.Dict = ""
	}
	return cc, nil
}

// newField 从列信息与（已校验的）配置构造模板输入字段。
func newField(col ColumnInfo, c ColumnConfig) Field {
	f := Field{
		Name: col.Name, Pascal: Pascal(col.Name), Camel: Camel(col.Name), Title: Title(col.Name),
		Comment: col.Comment, Kind: col.Kind, GoType: goType(col.Kind), FormType: formType(col.Kind),
		TSType: tsType(col.Kind), Control: c.Control, Dict: c.Dict, Nullable: col.Nullable,
		Required: c.Required, List: c.List, Query: c.Query, Form: c.Form, MaxLen: 255,
	}
	if col.Length > 0 && col.Length <= 4000 {
		f.MaxLen = int(col.Length)
	}
	return f
}

// buildSubSpec 组装主子表模式的子表部分。cfg 只消费 form / required / control；
// FK 列本身由服务端填，不进编辑器。
func buildSubSpec(a *Analysis, fk string, cfg []ColumnConfig, masterPascal string) (*SubSpec, error) {
	if err := ValidIdent(fk); err != nil {
		return nil, fmt.Errorf("invalid foreign key %q", fk)
	}
	var fkCol *ColumnInfo
	for i := range a.Cols {
		if a.Cols[i].Name == fk {
			fkCol = &a.Cols[i]
		}
	}
	if fkCol == nil || !fkCol.Supported || fkCol.Kind != KindID {
		return nil, fmt.Errorf("column %q in table %q must be a BIGINT *_id column pointing at the main table", fk, a.Table)
	}
	if a.Tree {
		return nil, fmt.Errorf("table %q is a tree table; master-detail editing does not apply", a.Table)
	}
	seg := lastSegment(a.Table)
	pascal := Pascal(seg)
	if pascal == masterPascal || reservedEntities[pascal] {
		pascal += "Item"
	}
	s := &SubSpec{Table: a.Table, Pascal: pascal, JSONPlural: Camel(Plural(seg)),
		FK: fk, FKPascal: Pascal(fk), Shape: a.Shape, Tenant: a.Tenant, SoftDelete: a.SoftDelete}
	byName := map[string]ColumnConfig{}
	for _, cc := range cfg {
		if err := ValidIdent(cc.Name); err != nil {
			return nil, err
		}
		byName[cc.Name] = cc
	}
	for _, col := range a.Cols {
		if !col.Supported || col.System || col.Name == fk {
			continue
		}
		c := col.Config
		if cc, ok := byName[col.Name]; ok {
			// 子行编辑器只有表单语义：List/Query 忽略；Required 校验与主表一致。
			c.Form = cc.Form
			c.Required = cc.Required
			adj, err := validateColumnCfg(&col, cc)
			if err != nil {
				return nil, err
			}
			c.Control, c.Dict = adj.Control, adj.Dict
		}
		if !c.Form {
			continue
		}
		f := newField(col, c)
		f.List, f.Query = false, false
		s.Fields = append(s.Fields, f)
	}
	return s, nil
}

func lastSegment(table string) string {
	if i := strings.LastIndex(table, "_"); i >= 0 {
		return table[i+1:]
	}
	return table
}

func contains(list []string, v string) bool {
	for _, x := range list {
		if x == v {
			return true
		}
	}
	return false
}

func (s *Spec) pick(pred func(Field) bool) []Field {
	var out []Field
	for _, f := range s.Fields {
		if pred(f) {
			out = append(out, f)
		}
	}
	return out
}

func (s *Spec) has(pred func(Field) bool) bool { return len(s.pick(pred)) > 0 }

// FormFields / ListFields / QueryFields 是三类视图的字段。
func (s *Spec) FormFields() []Field  { return s.pick(func(f Field) bool { return f.Form }) }
func (s *Spec) ListFields() []Field  { return s.pick(func(f Field) bool { return f.List }) }
func (s *Spec) QueryFields() []Field { return s.pick(func(f Field) bool { return f.Query }) }

func isTime(f Field) bool { return f.Kind == KindDate || f.Kind == KindDateTime }

// HasStamps: 嵌入基座时 View 带 createdAt / updatedAt。
func (s *Spec) HasStamps() bool { return s.Shape != ShapePlain }

// PlainTenant: 显式 ID 且有 tenant_id（创建时手动填租户）。
func (s *Spec) PlainTenant() bool { return s.Shape == ShapePlain && s.Tenant }

// TreeLabel 是树形下拉里每行的展示字段：第一个短文本业务列，退回 id。
func (s *Spec) TreeLabel() string {
	for _, f := range s.Fields {
		if f.Kind == KindString && f.Name != "parent_id" {
			return f.Camel
		}
	}
	return "id"
}

// 各文件的 import 开关（避免生成未使用的 import）。
func (s *Spec) UsesTimeModel() bool { return s.HasStamps() || s.has(isTime) }
func (s *Spec) UsesDatabase() bool  { return s.Shape != ShapePlain }
func (s *Spec) HasFormTime() bool   { return s.has(func(f Field) bool { return f.Form && isTime(f) }) }
func (s *Spec) HasStringForm() bool {
	return s.has(func(f Field) bool { return f.Form && f.Kind == KindString })
}
func (s *Spec) HasQuery() bool { return s.has(func(f Field) bool { return f.Query }) }
func (s *Spec) HasStringQuery() bool {
	return s.has(func(f Field) bool { return f.Query && f.Kind == KindString })
}

// HasInputQuery: 有用 Input 渲染的查询条件（除 bool / date 外）。
func (s *Spec) HasInputQuery() bool {
	return s.has(func(f Field) bool { return f.Query && f.Kind != KindBool && f.Kind != KindDate })
}
func (s *Spec) HasKindQuery(k string) bool {
	return s.has(func(f Field) bool { return f.Query && string(f.Kind) == k })
}

// UsesParseTime: 主表或子表表单里有时间字段（service.go 需要 parseTime 助手）。
func (s *Spec) UsesParseTime() bool {
	return s.HasFormTime() || (s.Sub != nil && s.Sub.HasFormTime())
}

// UsesStrings: service.go 是否用到 strings。
func (s *Spec) UsesStrings() bool {
	return s.HasStringForm() || s.HasFormTime() || s.HasStringQuery() ||
		(s.Sub != nil && (s.Sub.HasStringForm() || s.Sub.HasFormTime()))
}

// 前端/文案开关。
func (s *Spec) HasKind(k string) bool {
	return s.has(func(f Field) bool { return f.Form && string(f.Kind) == k })
}
func (s *Spec) HasControl(c string) bool {
	return s.has(func(f Field) bool { return f.Form && f.Control == c })
}

// HasDictList / HasDictQuery / HasDictForm 决定生成页要不要引入 DictTag / DictSelect。
func (s *Spec) HasDictList() bool {
	return s.has(func(f Field) bool { return f.List && f.IsDict() })
}
func (s *Spec) HasDictQuery() bool {
	return s.has(func(f Field) bool { return f.Query && f.IsDict() })
}
func (s *Spec) HasDictForm() bool {
	return s.has(func(f Field) bool { return f.Form && f.IsDict() })
}

// SubFormTime: 子行编辑器里有日期字段（决定要不要引 DatePicker / DateTimePicker）。
func (s *SubSpec) HasFormTime() bool {
	for _, f := range s.Fields {
		if f.Form && isTime(f) {
			return true
		}
	}
	return false
}

// SubHasControl 报告子行编辑器是否用到某个控件（决定生成文件的 import）。
func (s *SubSpec) HasControl(c string) bool {
	for _, f := range s.Fields {
		if f.Form && f.Control == c {
			return true
		}
	}
	return false
}

// SubHasDate / SubHasDateTime / SubHasSwitch / SubHasTextarea 是模板里 import 开关的糖。
func (s *Spec) SubHasDate() bool     { return s.Sub != nil && s.Sub.HasControl("date") }
func (s *Spec) SubHasDateTime() bool { return s.Sub != nil && s.Sub.HasControl("datetime") }
func (s *Spec) SubHasSwitch() bool   { return s.Sub != nil && s.Sub.HasControl("switch") }
func (s *Spec) SubHasTextarea() bool { return s.Sub != nil && s.Sub.HasControl("textarea") }
