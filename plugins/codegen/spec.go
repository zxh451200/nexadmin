package codegen

import (
	"fmt"
)

// Field 是生成物里的一个业务字段（模板输入）。所有字符串要么是白名单标识符，要么经 sanitizeComment。
type Field struct {
	Name, Pascal, Camel, Title string
	Comment                    string // 已 sanitizeComment；中文文案优先用它
	Kind                       Kind
	GoType, FormType, TSType   string
	Control                    string
	MaxLen                     int
	Nullable                   bool
	Required, List, Query      bool
	Form                       bool
}

// Label 返回中文文案。
func (f Field) LabelZh() string {
	if f.Comment != "" {
		return f.Comment
	}
	return f.Title
}

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
	Fields     []Field // 模型字段：不含 id / tenant_id（以及嵌入基座已有的 created_at / updated_at）
	Migrations []File  // 新建表模式附带的三引擎迁移；已有表模式为空
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
		goModule = "nexadmin"
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
		if cc.Query && !queryable(col.Kind) {
			return nil, fmt.Errorf("column %q (%s) does not support query", cc.Name, col.Kind)
		}
		if cc.Required && !requirable(col.Kind) {
			return nil, fmt.Errorf("column %q (%s) cannot be required", cc.Name, col.Kind)
		}
		if cc.Required && !cc.Form {
			return nil, fmt.Errorf("column %q is required but not in the form", cc.Name)
		}
		ctl := cc.Control
		if ctl == "" {
			ctl = controlsByKind[col.Kind][0]
		}
		if !contains(controlsByKind[col.Kind], ctl) {
			return nil, fmt.Errorf("column %q: control %q not allowed for %s", cc.Name, ctl, col.Kind)
		}
		cc.Control = ctl
		eff[cc.Name] = cc
	}

	s := &Spec{GoModule: goModule, Module: module, Pascal: Pascal(module), Plural: Plural(module),
		Table: a.Table, Shape: a.Shape, Tenant: a.Tenant, SoftDelete: a.SoftDelete}
	for _, col := range a.Cols {
		if !col.Supported || col.System {
			continue
		}
		c := eff[col.Name]
		f := Field{
			Name: col.Name, Pascal: Pascal(col.Name), Camel: Camel(col.Name), Title: Title(col.Name),
			Comment: col.Comment, Kind: col.Kind, GoType: goType(col.Kind), FormType: formType(col.Kind),
			TSType: tsType(col.Kind), Control: c.Control, Nullable: col.Nullable,
			Required: c.Required, List: c.List, Query: c.Query, Form: c.Form, MaxLen: 255,
		}
		if col.Length > 0 && col.Length <= 4000 {
			f.MaxLen = int(col.Length)
		}
		s.Fields = append(s.Fields, f)
	}
	return s, nil
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

// UsesStrings: service.go 是否用到 strings。
func (s *Spec) UsesStrings() bool { return s.HasStringForm() || s.HasFormTime() || s.HasStringQuery() }

// 前端/文案开关。
func (s *Spec) HasKind(k string) bool {
	return s.has(func(f Field) bool { return f.Form && string(f.Kind) == k })
}
func (s *Spec) HasControl(c string) bool {
	return s.has(func(f Field) bool { return f.Form && f.Control == c })
}
