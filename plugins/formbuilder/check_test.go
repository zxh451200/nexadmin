package formbuilder

import (
	"math"
	"strings"
	"testing"
	"time"
)

func ptr[T any](v T) *T { return &v }

func def(fields ...Field) *Definition {
	return &Definition{Name: "t", Fields: fields}
}

func opts(vals ...string) []Option {
	out := make([]Option, len(vals))
	for i, v := range vals {
		out[i] = Option{Label: strings.ToUpper(v), Value: v}
	}
	return out
}

// one 校验单个字段 f 收到值 v 的结果。
func one(f Field, v any) (any, []FieldError) {
	d := def(f)
	clean, errs := ValidateSubmission(d, map[string]any{f.Key: v})
	return clean[f.Key], errs
}

func wantErr(t *testing.T, name string, f Field, v any) {
	t.Helper()
	if _, errs := one(f, v); len(errs) == 0 {
		t.Errorf("%s: 期望校验失败，实际通过 (value=%v)", name, v)
	}
}

func wantOK(t *testing.T, name string, f Field, v any) any {
	t.Helper()
	got, errs := one(f, v)
	if len(errs) > 0 {
		t.Errorf("%s: 期望通过，实际失败 %v", name, errs)
	}
	return got
}

func TestTextField(t *testing.T) {
	f := Field{Key: "a", Type: TypeText, Title: "A", MinLen: ptr(2), MaxLen: ptr(5)}
	wantOK(t, "正常", f, "abc")
	wantOK(t, "按字符数而不是字节数", f, "你好吗呀吖") // 5 个汉字
	wantErr(t, "超长", f, "abcdef")
	wantErr(t, "过短", f, "a")
	wantErr(t, "数字不能当文本", f, 123.0)
	wantErr(t, "数组不能当文本", f, []any{"a"})
	wantErr(t, "对象不能当文本", f, map[string]any{"x": 1})
	wantErr(t, "布尔不能当文本", f, true)
	wantErr(t, "NUL 字符", f, "ab\x00c")
	wantErr(t, "控制字符", f, "ab\x07")
	wantErr(t, "非法 UTF-8", f, "ab\xff\xfe")
	wantErr(t, "单行文本不允许换行", Field{Key: "a", Type: TypeText, Title: "A"}, "a\nb")
	wantOK(t, "多行文本允许换行", Field{Key: "a", Type: TypeTextarea, Title: "A"}, "a\nb")
	// 未设置 maxLen 时有默认上限，防止塞巨型字符串
	wantErr(t, "默认上限", Field{Key: "a", Type: TypeText, Title: "A"}, strings.Repeat("x", DefaultTextRunes+1))
	wantErr(t, "多行默认上限", Field{Key: "a", Type: TypeTextarea, Title: "A"}, strings.Repeat("x", DefaultAreaRunes+1))
	wantOK(t, "默认上限内", Field{Key: "a", Type: TypeText, Title: "A"}, strings.Repeat("x", DefaultTextRunes))
}

func TestPatternField(t *testing.T) {
	f := Field{Key: "p", Type: TypeText, Title: "P", Pattern: `^\d{3}-\d{4}$`}
	wantOK(t, "匹配", f, "123-4567")
	wantErr(t, "不匹配", f, "1234567")
	wantErr(t, "部分匹配不算（锚点由定义者控制）", Field{Key: "p", Type: TypeText, Title: "P", Pattern: `^a$`}, "ba")
}

func TestRegexGuard(t *testing.T) {
	bad := map[string]string{
		"过长":            strings.Repeat("a", MaxPatternLen+1),
		"语法错误":          `(`,
		"前瞻（RE2 不支持）":   `(?=a)b`,
		"反向引用（RE2 不支持）": `(a)\1`,
		"重复次数过大":        `a{1,1000}`,
		"嵌套重复放大":        `((a{100}){100}){100}`,
		"节点过多":          strings.Repeat("a|", MaxPatternNodes) + "a",
	}
	for name, p := range bad {
		if _, err := compilePattern(p); err == nil {
			t.Errorf("%s: 正则 %.40q 应被拒绝", name, p)
		}
	}
	for _, p := range []string{`^[a-z0-9_]+$`, `^\d{1,3}(\.\d{1,3}){3}$`, `(a|b)*c`, `^.{0,50}$`} {
		if _, err := compilePattern(p); err != nil {
			t.Errorf("正则 %q 应被接受: %v", p, err)
		}
	}
}

// 经典 ReDoS 模式在回溯引擎里是指数级；Go 的 RE2 线性匹配，应当瞬间完成。
func TestRegexLinearTime(t *testing.T) {
	re, err := compilePattern(`^(a+)+$`)
	if err != nil {
		t.Fatal(err)
	}
	input := strings.Repeat("a", 5000) + "!"
	start := time.Now()
	if re.MatchString(input) {
		t.Fatal("不应匹配")
	}
	if d := time.Since(start); d > 2*time.Second {
		t.Fatalf("匹配耗时 %v，不是线性", d)
	}
	// 经过字段校验同样如此：输入超过 maxLen 直接被长度检查拦下，根本不进入匹配。
	f := Field{Key: "r", Type: TypeText, Title: "R", Pattern: `^(a+)+$`, MaxLen: ptr(100)}
	wantErr(t, "超长输入先被长度拦截", f, input)
}

func TestNumberField(t *testing.T) {
	f := Field{Key: "n", Type: TypeNumber, Title: "N", Min: ptr(1.0), Max: ptr(10.0)}
	wantOK(t, "范围内", f, 5.0)
	wantOK(t, "边界", f, 10.0)
	wantErr(t, "过小", f, 0.5)
	wantErr(t, "过大", f, 10.01)
	wantErr(t, "字符串不能当数字", f, "5")
	wantErr(t, "布尔不能当数字", f, true)
	wantErr(t, "NaN", f, math.NaN())
	wantErr(t, "Inf", f, math.Inf(1))
	wantErr(t, "极大值", Field{Key: "n", Type: TypeNumber, Title: "N"}, 1e20)
	wantErr(t, "非整数", Field{Key: "n", Type: TypeNumber, Title: "N", Integer: true}, 1.5)
	wantOK(t, "整数", Field{Key: "n", Type: TypeNumber, Title: "N", Integer: true}, 3.0)
}

func TestChoiceFields(t *testing.T) {
	radio := Field{Key: "r", Type: TypeRadio, Title: "R", Options: opts("a", "b")}
	wantOK(t, "单选", radio, "a")
	wantErr(t, "不在选项里", radio, "z")
	wantErr(t, "单选传数组", radio, []any{"a"})
	wantErr(t, "单选传数字", radio, 1.0)

	cb := Field{Key: "c", Type: TypeCheckbox, Title: "C", Options: opts("a", "b", "c"), Min: ptr(1.0), Max: ptr(2.0)}
	wantOK(t, "多选", cb, []any{"a", "b"})
	wantErr(t, "超过最多", cb, []any{"a", "b", "c"})
	wantErr(t, "重复项", cb, []any{"a", "a"})
	wantErr(t, "含非法选项", cb, []any{"a", "z"})
	wantErr(t, "元素类型错", cb, []any{1.0})
	wantErr(t, "多选传字符串", cb, "a")
	wantErr(t, "超过选项总数的数组", Field{Key: "c", Type: TypeCheckbox, Title: "C", Options: opts("a")}, []any{"a", "a", "a", "a"})
	huge := make([]any, 100000)
	for i := range huge {
		huge[i] = "a"
	}
	wantErr(t, "巨型数组", cb, huge)

	sel := Field{Key: "s", Type: TypeSelect, Title: "S", Options: opts("a", "b")}
	wantOK(t, "下拉", sel, "b")
	wantErr(t, "下拉非法", sel, "x")
	msel := sel
	msel.Multiple = true
	wantOK(t, "下拉多选", msel, []any{"a", "b"})
	wantErr(t, "下拉多选传字符串", msel, "a")
}

func TestDateSwitchRating(t *testing.T) {
	d := Field{Key: "d", Type: TypeDate, Title: "D", MinDate: "2024-01-01", MaxDate: "2024-12-31"}
	wantOK(t, "日期", d, "2024-06-15")
	wantErr(t, "早于", d, "2023-12-31")
	wantErr(t, "晚于", d, "2025-01-01")
	wantErr(t, "不存在的日期", d, "2024-02-30")
	wantErr(t, "格式不对", d, "2024-6-5")
	wantErr(t, "带时间", d, "2024-06-15T00:00:00Z")
	wantErr(t, "数字", d, 20240615.0)

	sw := Field{Key: "s", Type: TypeSwitch, Title: "S"}
	if got := wantOK(t, "开关 false 是有效值", sw, false); got != false {
		t.Errorf("false 应被保留，得到 %v", got)
	}
	wantErr(t, "字符串不是布尔", sw, "true")
	wantErr(t, "数字不是布尔", sw, 1.0)
	if _, errs := one(Field{Key: "s", Type: TypeSwitch, Title: "S", Required: true}, false); len(errs) == 0 {
		t.Error("必填开关必须为 true")
	}
	wantOK(t, "必填开关为 true", Field{Key: "s", Type: TypeSwitch, Title: "S", Required: true}, true)

	r := Field{Key: "r", Type: TypeRating, Title: "R"}
	wantOK(t, "评分", r, 5.0)
	wantErr(t, "评分 0", r, 0.0)
	wantErr(t, "评分超上限", r, 6.0)
	wantErr(t, "评分小数", r, 3.5)
	wantOK(t, "自定义上限", Field{Key: "r", Type: TypeRating, Title: "R", Max: ptr(10.0)}, 9.0)
}

func TestFileField(t *testing.T) {
	f := Field{Key: "f", Type: TypeFile, Title: "F", Max: ptr(2.0)}
	ok := map[string]any{"name": "a.png", "url": "/files/a.png", "size": 12.0, "type": "image/png", "evil": "dropped"}
	got := wantOK(t, "合法文件", f, []any{ok}).([]any)
	if _, has := got[0].(map[string]any)["evil"]; has {
		t.Error("未知键应被丢弃")
	}
	wantOK(t, "http 地址", f, []any{map[string]any{"name": "a", "url": "https://cdn.example.com/a.png"}})
	for _, u := range []string{"javascript:alert(1)", "data:text/html;base64,AAAA", "//evil.example.com/x", "file:///etc/passwd", "ftp://x/y", "/a b", "/a\\b", ""} {
		wantErr(t, "非法地址 "+u, f, []any{map[string]any{"name": "a", "url": u}})
	}
	wantErr(t, "缺少文件名", f, []any{map[string]any{"url": "/a"}})
	wantErr(t, "文件名含控制字符", f, []any{map[string]any{"name": "a\nb", "url": "/a"}})
	wantErr(t, "个数超限", f, []any{ok, ok, ok})
	wantErr(t, "元素不是对象", f, []any{"/a"})
	wantErr(t, "不是数组", f, "/a")
}

func TestRequiredAndBlank(t *testing.T) {
	f := Field{Key: "a", Type: TypeText, Title: "A", Required: true}
	for _, v := range []any{nil, "", "   ", "\t"} {
		wantErr(t, "必填为空", f, v)
	}
	d := def(f)
	if _, errs := ValidateSubmission(d, map[string]any{}); len(errs) != 1 {
		t.Errorf("缺少必填字段应报错，得到 %v", errs)
	}
	// 数值 0 是有效值，不算空
	wantOK(t, "0 不是空", Field{Key: "n", Type: TypeNumber, Title: "N", Required: true}, 0.0)
	wantErr(t, "必填多选为空数组", Field{Key: "c", Type: TypeCheckbox, Title: "C", Required: true, Options: opts("a")}, []any{})
	// 非必填且为空：不报错、不保存
	clean, errs := ValidateSubmission(def(Field{Key: "a", Type: TypeText, Title: "A"}), map[string]any{"a": ""})
	if len(errs) != 0 || len(clean) != 0 {
		t.Errorf("空的可选字段不应保存: %v %v", clean, errs)
	}
}

func TestUnknownKeysAndFlood(t *testing.T) {
	d := def(Field{Key: "a", Type: TypeText, Title: "A"})
	clean, errs := ValidateSubmission(d, map[string]any{"a": "x", "__proto__": "y", "admin": true, "b": map[string]any{"c": 1}})
	if len(errs) != 0 || len(clean) != 1 || clean["a"] != "x" {
		t.Fatalf("未知键应被丢弃: %v %v", clean, errs)
	}
	flood := map[string]any{}
	for i := 0; i < MaxFields*4+1; i++ {
		flood[strings.Repeat("k", 1+i%30)+string(rune('a'+i%26))+time.Duration(i).String()] = i
	}
	if _, errs := ValidateSubmission(d, flood); len(errs) == 0 {
		t.Error("键数量过多应被拒绝")
	}
}

func TestSectionHasNoValue(t *testing.T) {
	d := def(Field{Key: "s", Type: TypeSection, Title: "分组"}, Field{Key: "a", Type: TypeText, Title: "A"})
	clean, errs := ValidateSubmission(d, map[string]any{"s": "x", "a": "y"})
	if len(errs) != 0 || len(clean) != 1 {
		t.Fatalf("section 不产生数据: %v %v", clean, errs)
	}
}
