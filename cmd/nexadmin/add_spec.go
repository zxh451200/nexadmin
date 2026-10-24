package main

import (
	"bytes"
	"embed"
	"encoding/json"
	"fmt"
	"go/format"
	"path"
	"strings"
	"text/template"
)

//go:embed tpl/*.tmpl
var tplFS embed.FS

// FieldSpec 是一个字段在各层的完整描述（模板的输入）。
type FieldSpec struct {
	Field
	Pascal, Camel, Title                           string
	ModelType, ViewType, FormType, TSType, Control string
	Required                                       bool // 第一个 string 字段必填
	IsSort                                         bool // --sortable 时的排序列：不进表单，不进列表列
	Filter                                         bool // dict / ref / bool 做筛选
	Search                                         bool // string 参与关键字模糊搜索
	RefURL, RefLabel                               string
	Mobile                                         string // 手机卡片中的角色: title / sub / hidden / 空=normal
}

// Spec 是一次 add module 的全部输入。
type Spec struct {
	Module   string // 项目 Go 模块路径
	Name     string
	Pascal   string
	Camel    string
	Plural   string
	Table    string
	Sortable bool
	MigFile  string
	Fields   []FieldSpec
}

func newFieldSpec(f Field) FieldSpec {
	fs := FieldSpec{
		Field: f, Pascal: pascalName(f.Name), Camel: camelName(f.Name), Title: titleWords(f.Name),
		ModelType: goModelType(f.Kind), ViewType: goViewType(f.Kind), FormType: goFormType(f.Kind),
		TSType: tsType(f.Kind), Control: controlFor(f.Kind),
		Filter: f.Kind == kDict || f.Kind == kRef || f.Kind == kBool, Search: f.Kind == kString,
	}
	if f.Kind == kRef {
		fs.RefURL, fs.RefLabel = refURL(f.Arg), refLabelKey(f.Arg)
	}
	return fs
}

// buildSpec 校验并组装 Spec。sortable 时保证有一列 int 类型的 sort。
func buildSpec(module, name string, fields []Field, sortable bool, migFile string) (*Spec, error) {
	if err := validModuleName(name); err != nil {
		return nil, err
	}
	s := &Spec{Module: module, Name: name, Pascal: pascalName(name), Camel: camelName(name),
		Plural: pluralName(name), Table: name, Sortable: sortable, MigFile: migFile}
	hasSort, firstString := false, true
	for _, f := range fields {
		fs := newFieldSpec(f)
		if sortable && f.Name == "sort" {
			if f.Kind != kInt {
				return nil, fmt.Errorf("--sortable 时 sort 字段必须是 int，当前 %s", f.Kind)
			}
			hasSort = true
			fs.IsSort, fs.Filter, fs.Search = true, false, false
		}
		if fs.Search && firstString {
			fs.Required, firstString = true, false
		}
		s.Fields = append(s.Fields, fs)
	}
	if sortable && !hasSort {
		sf := newFieldSpec(Field{Name: "sort", Kind: kInt})
		sf.IsSort, sf.Filter = true, false
		s.Fields = append(s.Fields, sf)
	}
	s.assignMobile()
	return s, nil
}

// assignMobile 决定手机卡片里每列的角色：第一个 string 当主标题，第一个字典（否则第二个 string）当副标题，长文本隐藏。
func (s *Spec) assignMobile() {
	title, sub := -1, -1
	for i, f := range s.Fields {
		if f.IsSort {
			continue
		}
		if title < 0 && (f.Kind == kString) {
			title = i
		}
	}
	for i, f := range s.Fields {
		if f.IsSort || i == title {
			continue
		}
		if title < 0 {
			title = i
			continue
		}
		if sub < 0 && (f.Kind == kDict || f.Kind == kString) {
			sub = i
		}
	}
	for i := range s.Fields {
		switch {
		case i == title:
			s.Fields[i].Mobile = "title"
		case i == sub:
			s.Fields[i].Mobile = "sub"
		case s.Fields[i].Kind == kText:
			s.Fields[i].Mobile = "hidden"
		}
	}
}

func (s *Spec) pick(pred func(FieldSpec) bool) []FieldSpec {
	var out []FieldSpec
	for _, f := range s.Fields {
		if pred(f) {
			out = append(out, f)
		}
	}
	return out
}

// FormFields 是进入表单与请求体的字段；ColFields 是列表列。
func (s *Spec) FormFields() []FieldSpec { return s.pick(func(f FieldSpec) bool { return !f.IsSort }) }
func (s *Spec) ColFields() []FieldSpec  { return s.FormFields() }
func (s *Spec) FilterFields() []FieldSpec {
	return s.pick(func(f FieldSpec) bool { return f.Filter })
}
func (s *Spec) SearchFields() []FieldSpec {
	return s.pick(func(f FieldSpec) bool { return f.Search })
}

// SearchWhere / SearchArgs 生成关键字模糊搜索的 WHERE 片段与参数表（列名来自生成代码，不来自请求）。
func (s *Spec) SearchWhere() string {
	var parts []string
	for _, f := range s.SearchFields() {
		parts = append(parts, f.Name+" LIKE ?")
	}
	return "(" + strings.Join(parts, " OR ") + ")"
}

func (s *Spec) SearchArgs() string {
	return strings.TrimSuffix(strings.Repeat("like, ", len(s.SearchFields())), ", ")
}

// MockRows 是前端 mock 的示例行序号。
func (s *Spec) MockRows() []int { return []int{1, 2, 3, 4, 5} }

func (s *Spec) has(kinds ...string) bool {
	return len(s.pick(func(f FieldSpec) bool {
		for _, k := range kinds {
			if f.Kind == k {
				return true
			}
		}
		return false
	})) > 0
}

// 各层 import 是否需要（避免生成未使用的 import）。
func (s *Spec) UsesTime() bool    { return s.has(kDate, kDateTime) }
func (s *Spec) UsesIdgen() bool   { return s.has(kRef) }
func (s *Spec) UsesUpload() bool  { return s.has(kImage, kFile) }
func (s *Spec) HasDict() bool     { return s.has(kDict) }
func (s *Spec) HasRef() bool      { return s.has(kRef) }
func (s *Spec) HasDate() bool     { return s.has(kDate) }
func (s *Spec) HasDateTime() bool { return s.has(kDateTime) }
func (s *Spec) HasBool() bool     { return s.has(kBool) }
func (s *Spec) HasText() bool     { return s.has(kText) }
func (s *Spec) HasNumber() bool   { return s.has(kInt, kFloat) }
func (s *Spec) HasTime() bool     { return s.has(kDate, kDateTime) }

// FirstString 返回第一个 string 字段名（用作列表主标题 / 必填校验）；没有返回空。
func (s *Spec) FirstString() *FieldSpec {
	for i, f := range s.Fields {
		if f.Kind == kString {
			return &s.Fields[i]
		}
	}
	return nil
}

// ---- 迁移 SQL（纯函数） ----

func renderSQL(s *Spec, engine string) string {
	ts := "DATETIME(3)"
	if engine == "postgres" {
		ts = "TIMESTAMP(3)"
	}
	var b strings.Builder
	fmt.Fprintf(&b, "-- 业务表 %s（nexadmin add module 生成）。迁移里只建表，不写数据。\n", s.Table)
	fmt.Fprintf(&b, "CREATE TABLE %s (\n  id BIGINT NOT NULL,\n  tenant_id BIGINT NOT NULL,\n", s.Table)
	for _, f := range s.Fields {
		fmt.Fprintf(&b, "  %s %s,\n", f.Name, sqlType(f.Kind, engine))
	}
	fmt.Fprintf(&b, "  created_at %s NOT NULL,\n  updated_at %s NOT NULL,\n  PRIMARY KEY (id)\n);\n", ts, ts)
	cols := "tenant_id"
	if s.Sortable {
		cols += ", sort"
	}
	fmt.Fprintf(&b, "CREATE INDEX idx_%s_tenant ON %s (%s);\n", s.Table, s.Table, cols)
	return b.String()
}

// ---- 前端 locale（纯函数） ----

type kv struct{ k, v string }

// localeJSON 按固定顺序生成 {"<name>": {...}} 文件内容；i18n 命名空间 = 模块名。
func localeJSON(s *Spec, lang string) string {
	zh := lang == "zh-CN"
	pick := func(z, e string) string {
		if zh {
			return z
		}
		return e
	}
	title := titleWords(s.Name)
	var b bytes.Buffer
	str := func(v string) string { r, _ := json.Marshal(v); return string(r) }
	fmt.Fprintf(&b, "{\n  %s: {\n", str(s.Name))
	top := []kv{
		{"menu", title},
		{"description", pick("管理"+title, "Manage "+strings.ToLower(title))},
		{"searchPlaceholder", pick("搜索关键字", "Search keyword")},
		{"empty", pick("暂无数据", "No data yet")},
	}
	for _, e := range top {
		fmt.Fprintf(&b, "    %s: %s,\n", str(e.k), str(e.v))
	}
	fmt.Fprintf(&b, "    \"action\": {\n      \"add\": %s,\n      \"edit\": %s\n    },\n",
		str(pick("新增"+title, "Add "+strings.ToLower(title))), str(pick("编辑"+title, "Edit "+strings.ToLower(title))))
	b.WriteString("    \"field\": {\n")
	ff := s.FormFields()
	for i, f := range ff {
		sep := ","
		if i == len(ff)-1 {
			sep = ""
		}
		fmt.Fprintf(&b, "      %s: %s%s\n", str(f.Camel), str(f.Title), sep)
	}
	b.WriteString("    }")
	if fsr := s.FirstString(); fsr != nil && fsr.Required {
		fmt.Fprintf(&b, ",\n    \"validation\": {\n      %s: %s\n    }", str(fsr.Camel+"Required"),
			str(pick(fsr.Title+"不能为空", fsr.Title+" is required")))
	}
	b.WriteString("\n  }\n}\n")
	return b.String()
}

// ---- 模板渲染 ----

func mockValue(f FieldSpec, i int) string {
	switch f.Kind {
	case kString, kText:
		return fmt.Sprintf("'%s %d'", f.Title, i)
	case kInt:
		return fmt.Sprint(i * 10)
	case kFloat:
		return fmt.Sprintf("%d.5", i)
	case kBool:
		return fmt.Sprint(i%2 == 0)
	case kDate:
		return fmt.Sprintf("'2025-03-%02d'", i)
	case kDateTime:
		return fmt.Sprintf("'2025-03-%02dT10:00:00.000Z'", i)
	case kDict:
		if f.Arg == "sys_status" {
			return "'1'"
		}
		return "''"
	case kImage, kFile:
		return "[]"
	}
	return "''"
}

func parseTemplates() (*template.Template, error) {
	funcs := template.FuncMap{"mock": mockValue, "mockid": func(i int) string { return fmt.Sprintf("188000000000000%04d", i) }}
	return template.New("").Delims("[[", "]]").Funcs(funcs).ParseFS(tplFS, "tpl/*.tmpl")
}

// renderModule 渲染一个模块的全部文件，返回 相对路径(正斜杠) -> 内容。Go 文件已 gofmt。
func renderModule(s *Spec) (map[string][]byte, error) {
	t, err := parseTemplates()
	if err != nil {
		return nil, err
	}
	out := map[string][]byte{}
	render := func(rel, tmpl string) error {
		var b bytes.Buffer
		if err := t.ExecuteTemplate(&b, tmpl, s); err != nil {
			return fmt.Errorf("渲染 %s: %w", tmpl, err)
		}
		data := b.Bytes()
		if strings.HasSuffix(rel, ".go") {
			if data, err = format.Source(data); err != nil {
				return fmt.Errorf("生成的 %s 不是合法 Go 代码: %w", rel, err)
			}
		}
		out[rel] = data
		return nil
	}
	be := path.Join("internal", "modules", s.Name)
	for _, n := range []string{"model", "dto", "service", "handler", "module", "seed", "util"} {
		if err := render(path.Join(be, n+".go"), n+".go.tmpl"); err != nil {
			return nil, err
		}
	}
	fe := path.Join("web", "src", "pages", s.Name)
	for rel, tmpl := range map[string]string{
		"route.tsx": "route.tsx.tmpl", "api.ts": "api.ts.tmpl", s.Name + ".mock.ts": "mock.ts.tmpl",
		s.Pascal + "Page.tsx": "page.tsx.tmpl", s.Pascal + "FormDialog.tsx": "form.tsx.tmpl",
	} {
		if err := render(path.Join(fe, rel), tmpl); err != nil {
			return nil, err
		}
	}
	for _, lang := range []string{"zh-CN", "en-US"} {
		out[path.Join(fe, "locales", lang+".json")] = []byte(localeJSON(s, lang))
	}
	for _, eng := range []string{"mysql", "mariadb", "postgres"} {
		out[path.Join("migrations", "business", eng, s.MigFile)] = []byte(renderSQL(s, eng))
	}
	return out, nil
}
