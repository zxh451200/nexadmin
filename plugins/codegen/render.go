package codegen

import (
	"archive/zip"
	"bytes"
	"embed"
	"encoding/json"
	"fmt"
	"go/format"
	"path"
	"regexp"
	"sort"
	"strings"
	"text/template"
	"time"
	"unicode/utf8"
)

//go:embed tpl/*.tmpl
var tplFS embed.FS

// File 是一个待生成文件。Path 永远由服务端拼接，且相对用户项目根目录。
type File struct {
	Path    string `json:"path"`
	Content string `json:"content"`
	Lang    string `json:"lang"`
}

// Output 是一次生成的全部结果。
type Output struct {
	Files []File `json:"files"`
	Notes []Note `json:"notes"`
}

// Note 是给用户的后续步骤提示；Code 由前端翻译，Value 是需要原样展示的内容。
type Note struct {
	Code  string `json:"code"`
	Value string `json:"value"`
}

const maxTotalBytes = 4 << 20

// 输出路径白名单：只允许 internal/modules/<name>/... 与 web/src/pages/<name>/...，不含 .. 与隐藏文件。
var outPathRe = regexp.MustCompile(`^(internal/modules|web/src/pages)/[a-z][a-z0-9]{0,30}(/[A-Za-z0-9][A-Za-z0-9._-]*)+$`)

func validOutPath(p string) bool {
	return outPathRe.MatchString(p) && !strings.Contains(p, "..") && path.Clean(p) == p
}

// tsStr 生成安全的 TS 单引号字符串字面量。
func tsStr(s string) string {
	var b strings.Builder
	b.WriteByte('\'')
	for _, r := range s {
		switch {
		case r == '\\' || r == '\'':
			b.WriteByte('\\')
			b.WriteRune(r)
		case r < 0x20 || r == 0x7f || r == '\u2028' || r == '\u2029' || r == '<' || r == '>':
			fmt.Fprintf(&b, `\u%04x`, r)
		default:
			b.WriteRune(r)
		}
	}
	b.WriteByte('\'')
	return b.String()
}

func bindTag(f Field) string {
	var parts []string
	if f.Required && f.Kind != KindBool {
		parts = append(parts, "required")
	}
	if f.Kind == KindString {
		parts = append(parts, fmt.Sprintf("max=%d", f.MaxLen))
	}
	if len(parts) == 0 {
		return ""
	}
	return ` binding:"` + strings.Join(parts, ",") + `"`
}

func queryType(f Field) string {
	switch f.Kind {
	case KindInt:
		return "*int64"
	case KindID:
		return "*idgen.ID"
	case KindBool:
		return "*bool"
	case KindDate:
		return "*time.Time"
	}
	return "string"
}

func mockValue(f Field, i int) string {
	switch f.Kind {
	case KindInt:
		return fmt.Sprint(i * 10)
	case KindFloat:
		return fmt.Sprintf("%d.5", i)
	case KindBool:
		return fmt.Sprint(i%2 == 0)
	case KindDate:
		return fmt.Sprintf("'2025-03-%02d'", i)
	case KindDateTime:
		return fmt.Sprintf("'2025-03-%02dT10:00:00.000Z'", i)
	case KindID:
		return fmt.Sprintf("'189000000000%07d'", i)
	}
	return tsStr(fmt.Sprintf("%s %d", f.Title, i))
}

func mockEmpty(f Field) string {
	switch f.Kind {
	case KindInt, KindFloat:
		return "0"
	case KindBool:
		return "false"
	case KindDate, KindDateTime:
		return "null"
	}
	return "''"
}

// MockRows 是前端 mock 的示例行序号；HasTimeList 决定列表页是否需要日期格式化函数。
func (s *Spec) MockRows() []int { return []int{1, 2, 3, 4, 5} }
func (s *Spec) HasTimeList() bool {
	return s.has(func(f Field) bool { return f.List && isTime(f) })
}

func parseTemplates() (*template.Template, error) {
	funcs := template.FuncMap{
		"bind": bindTag, "queryType": queryType, "isTime": isTime,
		"mock": mockValue, "mockEmpty": mockEmpty,
		"mockid": func(i int) string { return fmt.Sprintf("188000000000%07d", i) },
	}
	return template.New("").Delims("[[", "]]").Funcs(funcs).ParseFS(tplFS, "tpl/*.tmpl")
}

// localeJSON 生成 {"<module>": {...}}；所有文案经 json.Marshal 转义。
func localeJSON(s *Spec, zh bool) string {
	pick := func(z, e string) string {
		if zh {
			return z
		}
		return e
	}
	str := func(v string) string { r, _ := json.Marshal(v); return string(r) }
	title := Title(s.Module)
	var b bytes.Buffer
	fmt.Fprintf(&b, "{\n  %s: {\n", str(s.Module))
	fmt.Fprintf(&b, "    \"menu\": %s,\n", str(title))
	fmt.Fprintf(&b, "    \"description\": %s,\n", str(pick("管理 "+title, "Manage "+strings.ToLower(title))))
	fmt.Fprintf(&b, "    \"empty\": %s,\n", str(pick("暂无数据", "No data yet")))
	fmt.Fprintf(&b, "    \"all\": %s,\n", str(pick("全部", "All")))
	fmt.Fprintf(&b, "    \"action\": {\n      \"add\": %s,\n      \"edit\": %s\n    },\n",
		str(pick("新增 "+title, "Add "+strings.ToLower(title))), str(pick("编辑 "+title, "Edit "+strings.ToLower(title))))
	b.WriteString("    \"field\": {\n")
	for i, f := range s.Fields {
		label := f.Title
		if zh {
			label = f.LabelZh()
		}
		sep := ","
		if i == len(s.Fields)-1 {
			sep = ""
		}
		fmt.Fprintf(&b, "      %s: %s%s\n", str(f.Camel), str(label), sep)
	}
	b.WriteString("    }")
	var req []Field
	for _, f := range s.Fields {
		if f.Form && f.Required && f.Kind != KindBool && f.Kind != KindInt && f.Kind != KindFloat && f.Kind != KindID {
			req = append(req, f)
		}
	}
	if len(req) > 0 {
		b.WriteString(",\n    \"validation\": {\n")
		for i, f := range req {
			sep := ","
			if i == len(req)-1 {
				sep = ""
			}
			label := f.Title
			if zh {
				label = f.LabelZh()
			}
			fmt.Fprintf(&b, "      %s: %s%s\n", str(f.Camel+"Required"), str(pick(label+"不能为空", label+" is required")), sep)
		}
		b.WriteString("    }")
	}
	b.WriteString("\n  }\n}\n")
	return b.String()
}

// Render 渲染全部文件。Go 文件经 go/format 校验与格式化；任何一步失败整体失败。
func Render(s *Spec) (*Output, error) {
	t, err := parseTemplates()
	if err != nil {
		return nil, err
	}
	pascal := s.Pascal
	type item struct{ rel, tmpl, lang string }
	be := path.Join("internal", "modules", s.Module)
	fe := path.Join("web", "src", "pages", s.Module)
	items := []item{
		{path.Join(be, "model.go"), "model.go.tmpl", "go"},
		{path.Join(be, "service.go"), "service.go.tmpl", "go"},
		{path.Join(be, "handler.go"), "handler.go.tmpl", "go"},
		{path.Join(be, "module.go"), "module.go.tmpl", "go"},
		{path.Join(be, "seed.go"), "seed.go.tmpl", "go"},
		{path.Join(fe, "route.tsx"), "route.tsx.tmpl", "tsx"},
		{path.Join(fe, "api.ts"), "api.ts.tmpl", "ts"},
		{path.Join(fe, s.Module+".mock.ts"), "mock.ts.tmpl", "ts"},
		{path.Join(fe, pascal+"Page.tsx"), "page.tsx.tmpl", "tsx"},
		{path.Join(fe, pascal+"FormDialog.tsx"), "form.tsx.tmpl", "tsx"},
	}
	out := &Output{}
	total := 0
	add := func(p, lang string, data []byte) error {
		if !validOutPath(p) {
			return fmt.Errorf("unsafe output path %q", p)
		}
		if !utf8.Valid(data) {
			return fmt.Errorf("%s: generated content is not valid UTF-8", p)
		}
		if total += len(data); total > maxTotalBytes {
			return fmt.Errorf("generated output too large")
		}
		out.Files = append(out.Files, File{Path: p, Content: string(data), Lang: lang})
		return nil
	}
	for _, it := range items {
		var b bytes.Buffer
		if err := t.ExecuteTemplate(&b, it.tmpl, s); err != nil {
			return nil, fmt.Errorf("render %s: %w", it.tmpl, err)
		}
		data := b.Bytes()
		if it.lang == "go" {
			if data, err = format.Source(data); err != nil {
				return nil, fmt.Errorf("generated %s is not valid Go: %w", it.rel, err)
			}
		}
		if err := add(it.rel, it.lang, data); err != nil {
			return nil, err
		}
	}
	for _, l := range []struct {
		name string
		zh   bool
	}{{"zh-CN", true}, {"en-US", false}} {
		raw := localeJSON(s, l.zh)
		if !json.Valid([]byte(raw)) {
			return nil, fmt.Errorf("generated locale %s is not valid JSON", l.name)
		}
		if err := add(path.Join(fe, "locales", l.name+".json"), "json", []byte(raw)); err != nil {
			return nil, err
		}
	}
	sort.Slice(out.Files, func(i, j int) bool { return out.Files[i].Path < out.Files[j].Path })
	out.Notes = []Note{
		{Code: "import_line", Value: fmt.Sprintf("_ \"%s/internal/modules/%s\"", s.GoModule, s.Module)},
		{Code: "api_prefix", Value: "/api/v1/" + s.Plural},
		{Code: "null_zero"},
	}
	return out, nil
}

// Zip 在内存里打包。zip 内路径即 Output 里已校验过的相对路径。
func (o *Output) Zip() ([]byte, error) {
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	mod := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	for _, f := range o.Files {
		if !validOutPath(f.Path) {
			return nil, fmt.Errorf("unsafe output path %q", f.Path)
		}
		h := &zip.FileHeader{Name: f.Path, Method: zip.Deflate, Modified: mod}
		h.SetMode(0o644)
		w, err := zw.CreateHeader(h)
		if err != nil {
			return nil, err
		}
		if _, err := w.Write([]byte(f.Content)); err != nil {
			return nil, err
		}
	}
	if err := zw.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}
