package formbuilder

import (
	"strings"
	"testing"
)

func leaf(field, op string, v any) *Cond { return &Cond{Field: field, Op: op, Value: v} }

func TestEvalCond(t *testing.T) {
	vals := map[string]any{
		"s": "hello", "n": 5.0, "b": false, "z": "",
		"arr": []any{"x", "y"}, "files": []any{map[string]any{"name": "a"}},
	}
	cases := []struct {
		name string
		c    *Cond
		want bool
	}{
		{"eq 字符串", leaf("s", OpEq, "hello"), true},
		{"eq 不等", leaf("s", OpEq, "nope"), false},
		{"eq 类型错配为 false", leaf("n", OpEq, "5"), false},
		{"eq 数字", leaf("n", OpEq, 5.0), true},
		{"eq 布尔 false 是有效值", leaf("b", OpEq, false), true},
		{"ne", leaf("s", OpNe, "x"), true},
		{"ne 缺失字段为真", leaf("missing", OpNe, "x"), true},
		{"eq 缺失字段为假", leaf("missing", OpEq, "x"), false},
		{"in 标量", leaf("s", OpIn, []any{"a", "hello"}), true},
		{"in 数组任一命中", leaf("arr", OpIn, []any{"q", "y"}), true},
		{"in 无命中", leaf("arr", OpIn, []any{"q"}), false},
		{"contains 数组元素", leaf("arr", OpContains, "x"), true},
		{"contains 子串", leaf("s", OpContains, "ell"), true},
		{"contains 空串不命中", leaf("s", OpContains, ""), false},
		{"contains 数字字段为假", leaf("n", OpContains, 5.0), false},
		{"empty 空串", leaf("z", OpEmpty, nil), true},
		{"empty 缺失", leaf("missing", OpEmpty, nil), true},
		{"empty 空数组", &Cond{Field: "e", Op: OpEmpty}, true},
		{"notEmpty", leaf("s", OpNotEmpty, nil), true},
		{"notEmpty false 值", leaf("b", OpNotEmpty, nil), true},
		{"gt", leaf("n", OpGt, 4.0), true},
		{"gt 边界", leaf("n", OpGt, 5.0), false},
		{"gte 边界", leaf("n", OpGte, 5.0), true},
		{"lt", leaf("n", OpLt, 6.0), true},
		{"lte 边界", leaf("n", OpLte, 5.0), true},
		{"gt 类型错配", leaf("n", OpGt, "4"), false},
		{"gt 字符串（日期）", leaf("s", OpGt, "hello!"), false},
		{"未知运算符", leaf("s", "exec", "x"), false},
		{"all", &Cond{All: []Cond{*leaf("s", OpEq, "hello"), *leaf("n", OpEq, 5.0)}}, true},
		{"all 有一个假", &Cond{All: []Cond{*leaf("s", OpEq, "hello"), *leaf("n", OpEq, 6.0)}}, false},
		{"any", &Cond{Any: []Cond{*leaf("s", OpEq, "no"), *leaf("n", OpEq, 5.0)}}, true},
		{"any 全假", &Cond{Any: []Cond{*leaf("s", OpEq, "no")}}, false},
	}
	for _, c := range cases {
		if got := evalCond(c.c, vals); got != c.want {
			t.Errorf("%s: got %v want %v", c.name, got, c.want)
		}
	}
}

func TestValidateCond(t *testing.T) {
	earlier := map[string]struct{}{"a": {}, "b": {}}
	good := []*Cond{
		leaf("a", OpEq, "x"), leaf("a", OpEmpty, nil), leaf("a", OpIn, []any{"x", 1.0, true}),
		{All: []Cond{*leaf("a", OpEq, "x"), {Any: []Cond{*leaf("b", OpGt, 1.0)}}}},
	}
	for i, c := range good {
		if err := validateCond(c, earlier); err != nil {
			t.Errorf("good[%d] 应通过: %v", i, err)
		}
	}
	deep := leaf("a", OpEq, "x")
	for i := 0; i < MaxCondDepth+1; i++ {
		deep = &Cond{All: []Cond{*deep}}
	}
	wide := &Cond{}
	for i := 0; i < MaxCondNodes+1; i++ {
		wide.All = append(wide.All, *leaf("a", OpEq, "x"))
	}
	bad := map[string]*Cond{
		"引用不存在的字段":  leaf("zzz", OpEq, "x"),
		"空条件":       {},
		"三种形态混用":    {Field: "a", Op: OpEq, Value: "x", All: []Cond{*leaf("a", OpEq, "x")}},
		"未知运算符":     leaf("a", "eval", "x"),
		"eq 的值是数组":  leaf("a", OpEq, []any{"x"}),
		"eq 的值是对象":  leaf("a", OpEq, map[string]any{"x": 1}),
		"in 的值不是数组": leaf("a", OpIn, "x"),
		"in 为空数组":   leaf("a", OpIn, []any{}),
		"in 元素是对象":  leaf("a", OpIn, []any{map[string]any{}}),
		"gt 的值是布尔":  leaf("a", OpGt, true),
		"过深":        deep,
		"过宽":        wide,
	}
	for name, c := range bad {
		if err := validateCond(c, earlier); err == nil {
			t.Errorf("%s: 应被拒绝", name)
		}
	}
}

func hiddenDef() *Definition {
	return def(
		Field{Key: "kind", Type: TypeRadio, Title: "类型", Required: true, Options: opts("a", "b")},
		Field{Key: "detail", Type: TypeText, Title: "详情", Required: true, ShowIf: leaf("kind", OpEq, "a")},
		Field{Key: "more", Type: TypeText, Title: "更多", Required: true, ShowIf: leaf("detail", OpNotEmpty, nil)},
		Field{Key: "note", Type: TypeText, Title: "备注"},
	)
}

func TestHiddenFieldNotRequired(t *testing.T) {
	// kind=b：detail 与 more 都隐藏，必填不触发
	clean, errs := ValidateSubmission(hiddenDef(), map[string]any{"kind": "b"})
	if len(errs) != 0 {
		t.Fatalf("隐藏字段的必填不应触发: %v", errs)
	}
	if len(clean) != 1 || clean["kind"] != "b" {
		t.Fatalf("clean=%v", clean)
	}
}

func TestHiddenFieldValueDropped(t *testing.T) {
	// kind=b 时客户端仍然提交了 detail / more：必须被丢弃，而不是存下来
	clean, errs := ValidateSubmission(hiddenDef(), map[string]any{"kind": "b", "detail": "sneaky", "more": "sneakier", "note": "ok"})
	if len(errs) != 0 {
		t.Fatal(errs)
	}
	if _, has := clean["detail"]; has {
		t.Error("隐藏字段 detail 的值应被丢弃")
	}
	if _, has := clean["more"]; has {
		t.Error("隐藏字段 more 的值应被丢弃")
	}
	if clean["note"] != "ok" {
		t.Errorf("可见字段应保留: %v", clean)
	}
}

func TestHiddenFieldInvalidValueIgnored(t *testing.T) {
	// 隐藏字段里提交了非法类型的值：既不报错也不保存
	clean, errs := ValidateSubmission(hiddenDef(), map[string]any{"kind": "b", "detail": []any{1, 2}})
	if len(errs) != 0 || clean["detail"] != nil {
		t.Fatalf("隐藏字段的非法值应被静默丢弃: %v %v", clean, errs)
	}
}

func TestVisibleFieldRequiredAndChain(t *testing.T) {
	// kind=a：detail 可见且必填
	if _, errs := ValidateSubmission(hiddenDef(), map[string]any{"kind": "a"}); len(errs) != 1 || errs[0].Key != "detail" {
		t.Fatalf("detail 应报必填: %v", errs)
	}
	// 填了 detail 后 more 变成可见且必填
	if _, errs := ValidateSubmission(hiddenDef(), map[string]any{"kind": "a", "detail": "d"}); len(errs) != 1 || errs[0].Key != "more" {
		t.Fatalf("more 应报必填: %v", errs)
	}
	clean, errs := ValidateSubmission(hiddenDef(), map[string]any{"kind": "a", "detail": "d", "more": "m"})
	if len(errs) != 0 || len(clean) != 3 {
		t.Fatalf("clean=%v errs=%v", clean, errs)
	}
}

func TestConditionOnHiddenParentIsEmpty(t *testing.T) {
	// more 依赖 detail；kind=b 时 detail 被隐藏并丢弃，即使客户端提交了 detail，more 也必须是隐藏的
	clean, errs := ValidateSubmission(hiddenDef(), map[string]any{"kind": "b", "detail": "x", "more": "y"})
	if len(errs) != 0 || len(clean) != 1 {
		t.Fatalf("链式隐藏失败: %v %v", clean, errs)
	}
}

func TestValidateDefinition(t *testing.T) {
	good := &Definition{Name: "ok", Fields: []Field{
		{Key: "a", Type: TypeText, Title: "A", Pattern: `^\d+$`, Default: "12"},
		{Key: "b", Type: TypeRadio, Title: "B", Options: opts("x", "y"), Default: "x", ShowIf: leaf("a", OpNotEmpty, nil)},
		{Key: "g", Type: TypeSection, Title: "分组"},
		{Key: "d", Type: TypeSelect, Title: "D", DictType: "sys_gender"},
	}}
	if err := ValidateDefinition(good); err != nil {
		t.Fatalf("合法定义被拒绝: %v", err)
	}
	mk := func(fs ...Field) *Definition { return &Definition{Name: "n", Fields: fs} }
	many := make([]Field, MaxFields+1)
	for i := range many {
		many[i] = Field{Key: "f" + strings.Repeat("x", i%5) + string(rune('a'+i%26)) + string(rune('a'+i/26)), Type: TypeText, Title: "t"}
	}
	bad := map[string]*Definition{
		"空名称":         {Name: " ", Fields: []Field{}},
		"key 以数字开头":   mk(Field{Key: "1a", Type: TypeText, Title: "t"}),
		"key 含非法字符":   mk(Field{Key: "a-b", Type: TypeText, Title: "t"}),
		"key 重复":      mk(Field{Key: "a", Type: TypeText, Title: "t"}, Field{Key: "a", Type: TypeText, Title: "t"}),
		"未知类型":        mk(Field{Key: "a", Type: "script", Title: "t"}),
		"标题为空":        mk(Field{Key: "a", Type: TypeText}),
		"单选无选项":       mk(Field{Key: "a", Type: TypeRadio, Title: "t"}),
		"选项值重复":       mk(Field{Key: "a", Type: TypeRadio, Title: "t", Options: []Option{{"A", "x"}, {"B", "x"}}}),
		"字典类型含非法字符":   mk(Field{Key: "a", Type: TypeSelect, Title: "t", DictType: "a;drop table"}),
		"默认值不在选项里":    mk(Field{Key: "a", Type: TypeRadio, Title: "t", Options: opts("x"), Default: "y"}),
		"默认值类型错":      mk(Field{Key: "a", Type: TypeNumber, Title: "t", Default: "abc"}),
		"正则非法":        mk(Field{Key: "a", Type: TypeText, Title: "t", Pattern: "("}),
		"正则过长":        mk(Field{Key: "a", Type: TypeText, Title: "t", Pattern: strings.Repeat("a", MaxPatternLen+1)}),
		"长度规则用在数字上":   mk(Field{Key: "a", Type: TypeNumber, Title: "t", MaxLen: ptr(3)}),
		"maxLen 超硬上限": mk(Field{Key: "a", Type: TypeText, Title: "t", MaxLen: ptr(MaxTextRunes + 1)}),
		"min>max":     mk(Field{Key: "a", Type: TypeNumber, Title: "t", Min: ptr(5.0), Max: ptr(1.0)}),
		"评分上限过大":      mk(Field{Key: "a", Type: TypeRating, Title: "t", Max: ptr(11.0)}),
		"日期格式":        mk(Field{Key: "a", Type: TypeDate, Title: "t", MinDate: "2024/01/01"}),
		"条件引用后面的字段":   mk(Field{Key: "a", Type: TypeText, Title: "t", ShowIf: leaf("b", OpEq, "x")}, Field{Key: "b", Type: TypeText, Title: "t"}),
		"条件引用自己":      mk(Field{Key: "a", Type: TypeText, Title: "t", ShowIf: leaf("a", OpEq, "x")}),
		"条件引用分组":      mk(Field{Key: "g", Type: TypeSection, Title: "g"}, Field{Key: "a", Type: TypeText, Title: "t", ShowIf: leaf("g", OpNotEmpty, nil)}),
		"字段过多":        {Name: "n", Fields: many},
		"次数上限为负":      {Name: "n", Fields: []Field{}, Settings: Settings{MaxSubmissions: -1}},
	}
	for name, d := range bad {
		if err := ValidateDefinition(d); err == nil {
			t.Errorf("%s: 应被拒绝", name)
		}
	}
}

func TestDefinitionSizeLimit(t *testing.T) {
	d := &Definition{Name: "n", Description: "x", Fields: []Field{}}
	for i := 0; i < 60; i++ {
		d.Fields = append(d.Fields, Field{
			Key: "f" + string(rune('a'+i%26)) + string(rune('a'+i/26)), Type: TypeCheckbox, Title: "t",
			Options: func() []Option {
				o := make([]Option, 150)
				for j := range o {
					o[j] = Option{Label: strings.Repeat("L", 90), Value: strings.Repeat("v", 80) + string(rune('A'+j/26)) + string(rune('a'+j%26))}
				}
				return o
			}(),
		})
	}
	if err := ValidateDefinition(d); err == nil {
		t.Error("超过 MaxDefBytes 的定义应被拒绝")
	}
}
