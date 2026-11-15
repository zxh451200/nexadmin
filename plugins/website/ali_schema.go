package website

// ali_schema.go 是阿里国际站 schema 体系的解析与回填层。
//
// 官方《【新接口】商品发布接入文档》（developer.alibaba.com treeId=456&articleId=119213）4.3 节
// 对接入方提了三条硬性要求：变更检测、**动态映射**（"改变写死参数的方式"）、关注 field 的 type 与 rule。
// 之前的实现把 field id 与结构写死在 Go 字符串里，一旦阿里的规则变化（例如 logisticsProperty 从
// singleCheck 升级成 multiCheck）就会整批发品失败，且缺字段的原因只出现在网关原文里。
// 现在改成：schema.get 拿规则 xml → 解析成 field 表 → 本地数据按 field id 填值 → 只提交规则里存在的
// field，并按 requiredRule 先自检，缺什么直接列给管理员看。

import (
	"encoding/xml"
	"fmt"
	"sort"
	"strconv"
	"strings"
)

// aliField 是 schema 里一个待填字段（type 见官方 2.1/2.2）。
type aliField struct {
	ID       string
	Name     string
	Type     string
	Rules    map[string]string // rule name → value（requiredRule / maxLengthRule / valueTypeRule …）
	Options  []aliOption
	Children []aliField // complex / multiComplex 的子字段
}

type aliOption struct {
	DisplayName string
	Value       string
}

// schema field 类型（官方七种，label 只是说明信息，无需填写）。
const (
	aliTypeInput        = "input"
	aliTypeMultiInput   = "multiInput"
	aliTypeSingleCheck  = "singleCheck"
	aliTypeMultiCheck   = "multiCheck"
	aliTypeComplex      = "complex"
	aliTypeMultiComplex = "multiComplex"
	aliTypeLabel        = "label"
)

// required 报告字段是否必填（缺省按非必填，与官方 2.3.3 一致）。
func (f aliField) required() bool { return strings.EqualFold(f.Rules["requiredRule"], "true") }

// disabled 报告字段是否可忽略（disableRule=true 时官方不做校验，我们也不提交）。
func (f aliField) disabled() bool { return strings.EqualFold(f.Rules["disableRule"], "true") }

// child 按 id 取子字段。
func (f aliField) child(id string) (aliField, bool) {
	for _, c := range f.Children {
		if c.ID == id {
			return c, true
		}
	}
	return aliField{}, false
}

// aliSchema 是顶层字段表（按 schema 原文顺序，提交时保持同序）。
type aliSchema []aliField

// find 按 id 取顶层字段。
func (sc aliSchema) find(id string) (aliField, bool) {
	for _, f := range sc {
		if f.ID == id {
			return f, true
		}
	}
	return aliField{}, false
}

// ---- 解析 ----

// aliNode 是宽松的 xml 节点：field / rule / option 的信息都在属性上，
// 同一份结构体读不同元素的属性即可（name 既是字段展示名也是规则名）。
type aliNode struct {
	XMLName     xml.Name
	ID          string    `xml:"id,attr"`
	Name        string    `xml:"name,attr"`
	Type        string    `xml:"type,attr"`
	Value       string    `xml:"value,attr"`
	DisplayName string    `xml:"displayName,attr"`
	Children    []aliNode `xml:",any"`
}

// parseAliSchema 解析 schema.get 返回的 xml 文本，返回顶层字段表。
// 官方 xml 的包裹层在不同版本里是 itemSchema / fields / schema，这里按「任何带 id+type 的 field 元素」
// 递归收集，因此不依赖具体包裹层名；子字段挂到父字段的 Children 上。
func parseAliSchema(raw string) (aliSchema, error) {
	if strings.TrimSpace(raw) == "" {
		return nil, fmt.Errorf("empty schema xml")
	}
	var root aliNode
	if err := xml.Unmarshal([]byte(raw), &root); err != nil {
		return nil, fmt.Errorf("schema xml: %w", err)
	}
	fields := collectAliFields(&root)
	if len(fields) == 0 {
		return nil, fmt.Errorf("schema xml has no field")
	}
	return fields, nil
}

// collectAliFields 从任意包裹层往下找 field；返回顶层字段（父字段带 Children）。
func collectAliFields(n *aliNode) []aliField {
	var out []aliField
	for i := range n.Children {
		c := &n.Children[i]
		if c.XMLName.Local == "field" && c.ID != "" && c.Type != "" {
			out = append(out, buildAliField(c))
			continue
		}
		out = append(out, collectAliFields(c)...)
	}
	return out
}

func buildAliField(n *aliNode) aliField {
	f := aliField{ID: n.ID, Name: strings.TrimSpace(n.Name), Type: n.Type, Rules: map[string]string{}}
	for i := range n.Children {
		ch := &n.Children[i]
		switch ch.XMLName.Local {
		case "rule":
			if ch.Name != "" {
				f.Rules[ch.Name] = ch.Value
			}
		case "rules":
			for j := range ch.Children {
				if r := &ch.Children[j]; r.Name != "" {
					f.Rules[r.Name] = r.Value
				}
			}
		case "option":
			f.Options = append(f.Options, aliOption{DisplayName: ch.DisplayName, Value: ch.Value})
		case "options":
			for j := range ch.Children {
				o := &ch.Children[j]
				f.Options = append(f.Options, aliOption{DisplayName: o.DisplayName, Value: o.Value})
			}
		case "field":
			f.Children = append(f.Children, buildAliField(ch))
		case "fields", "values", "complex-value", "complex-values":
			for j := range ch.Children {
				if c := &ch.Children[j]; c.XMLName.Local == "field" {
					f.Children = append(f.Children, buildAliField(c))
				}
			}
		}
	}
	return f
}

// ---- 值模型 ----

// aliVal 是一个字段的填写值，形态随字段 type：
//   - input / singleCheck：Text（+ Attrs，如 inputValue / fileId / text）
//   - multiInput / multiCheck：List（每项一个值，可带 Attrs）
//   - complex：Sub（子字段 id → 值）
//   - multiComplex：Rows（每行是子字段 id → 值）
type aliVal struct {
	Text  string
	Attrs map[string]string
	List  []aliVal
	Sub   map[string]aliVal
	Rows  []map[string]aliVal
}

func (v aliVal) empty() bool {
	return v.Text == "" && len(v.List) == 0 && len(v.Sub) == 0 && len(v.Rows) == 0
}

// aliInput 是单值字段的便捷构造。
func aliInput(text string) aliVal { return aliVal{Text: text} }

// aliInputAttrs 是带属性（fileId / inputValue / text）的单值。
func aliInputAttrs(text string, attrs map[string]string) aliVal {
	return aliVal{Text: text, Attrs: attrs}
}

// ---- 渲染与自检 ----

// aliMissing 是一条「必填但没填」的记录：Path 用 field id 串联，Name 是阿里的展示名。
type aliMissing struct {
	Path string
	Name string
}

func (m aliMissing) label() string {
	if m.Name != "" {
		return m.Name + "(" + m.Path + ")"
	}
	return m.Path
}

// Render 按 schema 顺序渲染提交 xml，并返回缺失的必填字段。
// schema 里没有的 field 一律不提交（这正是动态映射的意义：阿里的规则变了，我们不会硬送）；
// disableRule=true 与 label 类型跳过；必填但没值的收进 missing。
func (sc aliSchema) Render(vals map[string]aliVal) (string, []aliMissing) {
	var b strings.Builder
	b.WriteString("<itemSchema>\n")
	var missing []aliMissing
	for _, f := range sc {
		if f.disabled() || f.Type == aliTypeLabel {
			continue
		}
		writeAliField(&b, f, vals[f.ID], "", &missing)
	}
	b.WriteString("</itemSchema>")
	return b.String(), missing
}

// writeAliField 写一个字段：值为空时，必填就记进 missing，非必填直接不写。
func writeAliField(b *strings.Builder, f aliField, v aliVal, path string, missing *[]aliMissing) {
	full := f.ID
	if path != "" {
		full = path + "." + f.ID
	}
	if v.empty() {
		if f.required() {
			*missing = append(*missing, aliMissing{Path: full, Name: f.Name})
		}
		return
	}
	b.WriteString(fieldOpen(f))
	switch f.Type {
	case aliTypeMultiInput, aliTypeMultiCheck:
		b.WriteString("<values>")
		for _, item := range v.List {
			b.WriteString(valueTag(item.Text, item.Attrs))
		}
		b.WriteString("</values>")
	case aliTypeComplex:
		b.WriteString("<complex-value>")
		for _, c := range f.Children {
			if c.disabled() || c.Type == aliTypeLabel {
				continue
			}
			writeAliField(b, c, v.Sub[c.ID], full, missing)
		}
		b.WriteString("</complex-value>")
	case aliTypeMultiComplex:
		for _, row := range v.Rows {
			b.WriteString("<complex-values>")
			for _, c := range f.Children {
				if c.disabled() || c.Type == aliTypeLabel {
					continue
				}
				writeAliField(b, c, row[c.ID], full, missing)
			}
			b.WriteString("</complex-values>")
		}
	default: // input / singleCheck
		b.WriteString(valueTag(v.Text, v.Attrs))
	}
	b.WriteString("</field>\n")
}

func fieldOpen(f aliField) string {
	var b strings.Builder
	b.WriteString("<field id=\"")
	b.WriteString(xmlAttrEscape(f.ID))
	b.WriteString("\"")
	if f.Name != "" {
		b.WriteString(" name=\"")
		b.WriteString(xmlAttrEscape(f.Name))
		b.WriteString("\"")
	}
	b.WriteString(" type=\"")
	b.WriteString(xmlAttrEscape(f.Type))
	b.WriteString("\">")
	return b.String()
}

func valueTag(text string, attrs map[string]string) string {
	var b strings.Builder
	b.WriteString("<value")
	keys := make([]string, 0, len(attrs))
	for k := range attrs {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		b.WriteString(" ")
		b.WriteString(xmlAttrEscape(k))
		b.WriteString("=\"")
		b.WriteString(xmlAttrEscape(attrs[k]))
		b.WriteString("\"")
	}
	b.WriteString(">")
	b.WriteString(xmlAttrEscape(text))
	b.WriteString("</value>")
	return b.String()
}

// ---- 枚举与长度工具（填值时按 schema 规则就地解析）----

// resolveOption 按展示名在字段的 options 里找值：先精确（忽略大小写/首尾点/下划线），
// 再比 "Piece/Pieces" 这类形式里的任一段，最后退回可自定义项（value=-1）。
// 返回 (值, 是否自定义值, 是否命中)；自定义值要把原文写进 value 的 inputValue 属性。
func (f aliField) resolveOption(text string) (string, bool, bool) {
	want := normalizeDisplay(text)
	if want == "" {
		return "", false, false
	}
	custom := ""
	for _, o := range f.Options {
		if normalizeDisplay(o.DisplayName) == want {
			return o.Value, o.Value == "-1", true
		}
		for _, part := range strings.Split(o.DisplayName, "/") {
			if normalizeDisplay(part) == want {
				return o.Value, o.Value == "-1", true
			}
		}
		if o.Value == "-1" {
			custom = o.Value
		}
	}
	if custom != "" {
		return custom, true, true
	}
	return "", false, false
}

func normalizeDisplay(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	s = strings.Trim(s, ".")
	s = strings.ReplaceAll(s, "_", " ")
	return strings.Join(strings.Fields(s), " ")
}

// clampBytes 按 maxLengthRule（unit=byte）截断；规则缺失时原样返回。
func (f aliField) clampBytes(s string) string {
	if v := strings.TrimSpace(f.Rules["maxLengthRule"]); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			return trimLen(s, n)
		}
	}
	return s
}

// fillSequential 把一组文本按顺序填进 complex 的子字段（官方用 productKeywords_0/1/2、
// scImages_0..5、ladderPrice_0..3 这类「同构子字段」表达可重复项）。
// 返回子字段 id → 值；超出子字段个数的部分丢弃（由调用方决定是否提示）。
func fillSequential(f aliField, texts []string, attrs func(i int) map[string]string) map[string]aliVal {
	out := map[string]aliVal{}
	for i, t := range texts {
		if i >= len(f.Children) || t == "" {
			continue
		}
		c := f.Children[i]
		var a map[string]string
		if attrs != nil {
			a = attrs(i)
		}
		out[c.ID] = aliInputAttrs(t, a)
	}
	return out
}
