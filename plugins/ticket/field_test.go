package ticket

// 自定义字段：定义校验（normalize）、值校验/归一（Validate*）、custom_values 往返（Encode/Decode）
// 与 CRUD 的 SQL 形状。语义判断全部落在纯函数用例里（sqlmock 从不执行 SQL，只会匹配语句形状），
// 每条用例都按「实现里删掉对应守卫就会红」来写。

import (
	"encoding/json"
	"fmt"
	"math"
	"strings"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"gorm.io/gorm"

	"nexadmin/core/database"
	"nexadmin/pkg/idgen"
)

// fieldMockColumns 是 plg_ticket_field 的列顺序（与 Field 结构体字段顺序一致，含 PlatformModel 的时间列）。
var fieldMockColumns = []string{
	"id", "created_at", "updated_at", "code", "label", "type",
	"options", "required", "enabled", "sort", "group_id",
}

func fieldRow(id int64, code, label, typ, options string, required, enabled bool, sort int, groupID int64) *sqlmock.Rows {
	return sqlmock.NewRows(fieldMockColumns).
		AddRow(id, testNow, testNow, code, label, typ, options, required, enabled, sort, groupID)
}

func selectField() Field {
	return Field{Code: "instrument", Label: "乐器", Type: FieldTypeSelect, OptionList: []string{"viola", "cello"}}
}

// ---------------------------------------------------------------------------
// 类型与定义校验
// ---------------------------------------------------------------------------

func TestFieldValidFieldType(t *testing.T) {
	for _, ok := range []string{FieldTypeText, FieldTypeNumber, FieldTypeSelect, FieldTypeDate, FieldTypeBool} {
		if !ValidFieldType(ok) {
			t.Errorf("ValidFieldType(%q) = false", ok)
		}
	}
	// 少一个 case 就会让「存进去的类型永远校验不过」这种问题拖到线上，这里逐个拒。
	for _, bad := range []string{"", "email", "Text", "text ", "numberic", "boolean", "date-time"} {
		if ValidFieldType(bad) {
			t.Errorf("ValidFieldType(%q) = true, want false", bad)
		}
	}
}

func TestFieldNormalizeCode(t *testing.T) {
	cases := []string{"", "  ", "Order_No", "order-no", "订单编号", "_order", "1order", "a" + strings.Repeat("b", 32)}
	for _, code := range cases {
		f := Field{Code: code, Label: "x", Type: FieldTypeText}
		mustKey(t, f.normalize(), 400, "ticket.field_code_invalid")
	}
	// 边界：32 个字符（首字母 + 31）刚好合法，多一个就拒。
	f := Field{Code: "a" + strings.Repeat("b", 31), Label: "x", Type: FieldTypeText}
	if err := f.normalize(); err != nil {
		t.Fatalf("32-char code rejected: %v", err)
	}
	good := Field{Code: "  order_no2  ", Label: "x", Type: FieldTypeText}
	if err := good.normalize(); err != nil {
		t.Fatal(err)
	}
	if good.Code != "order_no2" {
		t.Fatalf("code not trimmed: %q", good.Code)
	}
}

func TestFieldNormalizeLabel(t *testing.T) {
	f := Field{Code: "a", Label: "   ", Type: FieldTypeText}
	mustKey(t, f.normalize(), 400, "ticket.field_label_required")

	long := Field{Code: "a", Label: strings.Repeat("字", maxFieldLabelRunes+1), Type: FieldTypeText}
	mustKey(t, long.normalize(), 400, "ticket.field_label_required")

	// 按 rune 计：64 个中文字符（192 字节）必须过，实现按字节算就会在这里红。
	ok := Field{Code: "a", Label: strings.Repeat("字", maxFieldLabelRunes), Type: FieldTypeText}
	if err := ok.normalize(); err != nil {
		t.Fatalf("64-rune label rejected: %v", err)
	}
}

func TestFieldNormalizeType(t *testing.T) {
	f := Field{Code: "a", Label: "x", Type: "email"}
	mustKey(t, f.normalize(), 400, "ticket.field_type_invalid")

	// 空类型按 text 处理（与列默认值一致），不是报错。
	empty := Field{Code: "a", Label: "x"}
	if err := empty.normalize(); err != nil {
		t.Fatalf("empty type must default to text: %v", err)
	}
	if empty.Type != FieldTypeText {
		t.Fatalf("type = %q, want %q", empty.Type, FieldTypeText)
	}
}

func TestFieldNormalizeOptions(t *testing.T) {
	// 非 select 带候选项：配置错误，必须拒（否则管理端会显示一批永远不生效的选项）。
	text := selectField()
	text.Type = FieldTypeText
	mustKey(t, text.normalize(), 400, "ticket.field_option_invalid")

	number := selectField()
	number.Type = FieldTypeNumber
	mustKey(t, number.normalize(), 400, "ticket.field_option_invalid")

	// select 至少一个候选项。
	none := Field{Code: "a", Label: "x", Type: FieldTypeSelect, OptionList: []string{"  ", ""}}
	mustKey(t, none.normalize(), 400, "ticket.field_option_invalid")

	// 上限 50 项，第 51 项拒。
	full := Field{Code: "a", Label: "x", Type: FieldTypeSelect, OptionList: make([]string, 0, maxFieldOptionCount+1)}
	for i := 0; i <= maxFieldOptionCount; i++ {
		full.OptionList = append(full.OptionList, fmt.Sprintf("opt%d", i))
	}
	mustKey(t, full.normalize(), 400, "ticket.field_option_invalid")
	trimmed := Field{Code: "a", Label: "x", Type: FieldTypeSelect, OptionList: full.OptionList[:maxFieldOptionCount]}
	if err := trimmed.normalize(); err != nil {
		t.Fatalf("exactly %d options rejected: %v", maxFieldOptionCount, err)
	}

	// 单项超长 / 含逗号（存储串是逗号分隔，含逗号会把一项裂成两项）。
	long := selectField()
	long.OptionList = []string{strings.Repeat("x", maxFieldOptionRunes+1)}
	mustKey(t, long.normalize(), 400, "ticket.field_option_invalid")

	comma := selectField()
	comma.OptionList = []string{"a,b"}
	mustKey(t, comma.normalize(), 400, "ticket.field_option_invalid")
}

func TestFieldNormalizeEncodesOptions(t *testing.T) {
	f := Field{
		Code: "  instrument ", Label: "  乐器  ", Type: " select ", Sort: -2,
		OptionList: []string{" 大", "viola ", "viola", "cello", ""},
	}
	if err := f.normalize(); err != nil {
		t.Fatal(err)
	}
	if f.Code != "instrument" || f.Label != "乐器" || f.Type != FieldTypeSelect || f.Sort != 0 {
		t.Fatalf("unexpected normalized field: %+v", f)
	}
	// 去空白 + 去重 + 顺序保持 + 编进 Options：任一环节丢了，读回来就和写出去的不一致。
	if want := []string{"大", "viola", "cello"}; strings.Join(f.OptionList, "|") != strings.Join(want, "|") {
		t.Fatalf("OptionList = %q, want %q", f.OptionList, want)
	}
	if f.Options != "大,viola,cello" {
		t.Fatalf("Options = %q, want %q", f.Options, "大,viola,cello")
	}
}

func TestFieldAfterFindLoadsOptions(t *testing.T) {
	f := Field{Options: "viola,cello"}
	if err := f.AfterFind(&gorm.DB{}); err != nil {
		t.Fatal(err)
	}
	if len(f.OptionList) != 2 || f.OptionList[0] != "viola" || f.OptionList[1] != "cello" {
		t.Fatalf("OptionList = %q", f.OptionList)
	}
	empty := Field{}
	if err := empty.AfterFind(&gorm.DB{}); err != nil {
		t.Fatal(err)
	}
	if empty.OptionList == nil || len(empty.OptionList) != 0 {
		t.Fatalf("empty options must decode to non-nil empty slice, got %#v", empty.OptionList)
	}
}

// ---------------------------------------------------------------------------
// 单值校验（ValidateValue）
// ---------------------------------------------------------------------------

func TestFieldValidateValueText(t *testing.T) {
	f := Field{Code: "note", Type: FieldTypeText}
	cases := []struct {
		name string
		in   any
		want any
	}{
		{"nil", nil, nil},
		{"empty is unset", "   ", nil},
		{"plain", " hi ", "hi"},
		{"at limit", strings.Repeat("字", maxFieldTextRunes), strings.Repeat("字", maxFieldTextRunes)},
	}
	for _, c := range cases {
		got, err := f.ValidateValue(c.in)
		if err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		if got != c.want {
			t.Errorf("%s: got %#v want %#v", c.name, got, c.want)
		}
	}
	// 501 rune 拒（按字节实现的话，1 个中文字就红）。
	if _, err := f.ValidateValue(strings.Repeat("字", maxFieldTextRunes+1)); err == nil {
		t.Fatal("oversized text accepted")
	} else {
		mustKey(t, err, 400, "ticket.field_value_invalid")
	}
	for _, bad := range []any{float64(1), true, []string{"a"}} {
		mustKey(t, mustValueErr(t, f, bad), 400, "ticket.field_value_invalid")
	}
}

func TestFieldValidateValueNumber(t *testing.T) {
	f := Field{Code: "amount", Type: FieldTypeNumber}
	// 各种数字形状都归一成 float64（int/JSON Number/数字字符串都来自不同前端与导入源）。
	for _, in := range []any{float64(7), int(7), int64(7), json.Number("7"), "42", " -3.5 "} {
		got, err := f.ValidateValue(in)
		if err != nil {
			t.Fatalf("%#v: %v", in, err)
		}
		if _, ok := got.(float64); !ok {
			t.Fatalf("%#v: normalized to %#v (%T), want float64", in, got, got)
		}
	}
	got, err := f.ValidateValue("42")
	if err != nil || got.(float64) != 42 {
		t.Fatalf(`"42" -> %#v (%v), want 42.0`, got, err)
	}
	if got, err = f.ValidateValue(int64(9)); err != nil || got.(float64) != 9 {
		t.Fatalf("int64(9) -> %#v (%v), want 9.0", got, err)
	}
	if got, err = f.ValidateValue(json.Number("1.25")); err != nil || got.(float64) != 1.25 {
		t.Fatalf("json.Number -> %#v (%v), want 1.25", got, err)
	}
	if got, err = f.ValidateValue("  "); err != nil || got != nil {
		t.Fatalf(`"  " -> %#v (%v), want unset`, got, err)
	}
	// NaN/Inf/超界/非数字串：任何一条被放行，报表聚合时就会炸。
	for _, bad := range []any{
		math.NaN(), math.Inf(1), math.Inf(-1),
		"NaN", "Inf", "1e13", float64(maxFieldNumberAbs) + 1, "1 2", "abc", "42abc", true, []string{"1"},
	} {
		mustKey(t, mustValueErr(t, f, bad), 400, "ticket.field_value_invalid")
	}
	// 边界值本身放行。
	if _, err := f.ValidateValue(float64(maxFieldNumberAbs)); err != nil {
		t.Fatalf("at abs limit rejected: %v", err)
	}
}

func TestFieldValidateValueSelect(t *testing.T) {
	f := selectField()
	if got, err := f.ValidateValue("viola"); err != nil || got != "viola" {
		t.Fatalf(`"viola" -> %#v (%v)`, got, err)
	}
	if got, err := f.ValidateValue(" cello "); err != nil || got != "cello" {
		t.Fatalf(`" cello " -> %#v (%v)`, got, err)
	}
	if got, err := f.ValidateValue(nil); err != nil || got != nil {
		t.Fatalf("nil -> %#v (%v)", got, err)
	}
	// 近似值必须拒：实现若退化成 Contains/前缀匹配，"violin" 会命中 "viola"。
	for _, bad := range []any{"violin", "viol", "cell", "VIOLA", "viola,cello", float64(1)} {
		mustKey(t, mustValueErr(t, f, bad), 400, "ticket.field_value_invalid")
	}
	// 内存态（只有 Options、没跑过 AfterFind）也要能校验，否则刚存完就校验必挂。
	raw := Field{Code: "instrument", Type: FieldTypeSelect, Options: "viola,cello"}
	if _, err := raw.ValidateValue("violin"); err == nil {
		t.Fatal("Options-only field accepted an unknown option")
	}
	if _, err := raw.ValidateValue("cello"); err != nil {
		t.Fatalf("Options-only field rejected a valid option: %v", err)
	}
}

func TestFieldValidateValueDate(t *testing.T) {
	f := Field{Code: "visit", Type: FieldTypeDate}
	if got, err := f.ValidateValue("2026-01-02"); err != nil || got != "2026-01-02" {
		t.Fatalf("valid date -> %#v (%v)", got, err)
	}
	if got, err := f.ValidateValue("   "); err != nil || got != nil {
		t.Fatalf("blank date -> %#v (%v), want unset", got, err)
	}
	// 严格 YYYY-MM-DD：月份越界、日子不存在、带时刻、用斜杠、少一位零填充、非字符串，一律拒。
	for _, bad := range []any{
		"2026-13-01", "2026-02-30", "2026-01-01T00:00:00Z", "2026/01/02",
		"2026-1-2", "2026-01-2", "today", float64(20260102),
	} {
		mustKey(t, mustValueErr(t, f, bad), 400, "ticket.field_value_invalid")
	}
}

func TestFieldValidateValueBool(t *testing.T) {
	f := Field{Code: "vip", Type: FieldTypeBool}
	for in, want := range map[any]bool{true: true, "true": true, "TRUE": true, " false": false, "False": false} {
		got, err := f.ValidateValue(in)
		if err != nil {
			t.Fatalf("%#v: %v", in, err)
		}
		if got.(bool) != want {
			t.Errorf("%#v -> %#v, want %v", in, got, want)
		}
	}
	if got, err := f.ValidateValue(""); err != nil || got != nil {
		t.Fatalf(`"" -> %#v (%v), want unset`, got, err)
	}
	// "1"/"yes"/"on" 这类同义词一律拒：跨系统导入时它们的含义随时变。
	for _, bad := range []any{"yes", "no", "1", "0", "on", float64(1), int64(0)} {
		mustKey(t, mustValueErr(t, f, bad), 400, "ticket.field_value_invalid")
	}
}

func TestFieldValidateValueUnknownType(t *testing.T) {
	f := Field{Code: "a", Type: "email"}
	mustKey(t, mustValueErr(t, f, "x"), 400, "ticket.field_type_invalid")
}

// ---------------------------------------------------------------------------
// 整份值校验/合并/往返
// ---------------------------------------------------------------------------

func fieldDefs() []Field {
	vip := Field{Code: "vip_level", Label: "会员等级", Type: FieldTypeSelect, OptionList: []string{"gold", "silver"}, Required: true, Enabled: true}
	note := Field{Code: "note", Label: "备注", Type: FieldTypeText, Enabled: true}
	amount := Field{Code: "amount", Label: "金额", Type: FieldTypeNumber, Enabled: true}
	gone := Field{Code: "legacy", Label: "旧字段", Type: FieldTypeText, Enabled: false}
	defs := []Field{vip, note, amount, gone}
	for i := range defs {
		defs[i].Options = encodeField(defs[i].OptionList)
	}
	return defs
}

func TestFieldValidateCustomValuesRequired(t *testing.T) {
	defs := fieldDefs()
	// 必填缺失：完全没给 key。
	mustKey(t, mustValuesErr(t, defs, map[string]any{"note": "hi"}), 400, "ticket.field_required")
	// 必填给了空值，同样算没填（前端清空 select 就是这个形状）。
	mustKey(t, mustValuesErr(t, defs, map[string]any{"vip_level": ""}), 400, "ticket.field_required")
	// 空串落在 text 上不算违反必填。
	if _, err := ValidateCustomValues(defs, map[string]any{"vip_level": "gold", "note": "  "}); err != nil {
		t.Fatal(err)
	}
}

func TestFieldValidateCustomValuesRejectsUnknownCode(t *testing.T) {
	defs := fieldDefs()
	// 未定义的 code：拒绝而不是丢弃（丢弃会让前端塞错 key 变成「提交成功但值没了」）。
	mustKey(t, mustValuesErr(t, defs, map[string]any{"vip_level": "gold", "secret": "x"}), 400, "ticket.field_code_invalid")
	// 已停用的 code 同样拒绝，避免停用字段被继续写入。
	mustKey(t, mustValuesErr(t, defs, map[string]any{"legacy": "x"}), 400, "ticket.field_code_invalid")
	// 全通过时返回值里只有启用字段的值。
	out, err := ValidateCustomValues(defs, map[string]any{"vip_level": "gold"})
	if err != nil {
		t.Fatal(err)
	}
	if len(out) != 1 || out["vip_level"] != "gold" {
		t.Fatalf("out = %#v", out)
	}
}

func TestFieldValidateCustomValuesNormalizes(t *testing.T) {
	defs := fieldDefs()
	out, err := ValidateCustomValues(defs, map[string]any{"vip_level": "silver", "note": " 客户来电 ", "amount": "42"})
	if err != nil {
		t.Fatal(err)
	}
	if out["note"] != "客户来电" {
		t.Fatalf(`note = %#v, want "客户来电"`, out["note"])
	}
	// 数字字符串归一成 float64：下游 EncodeCustomValues 才会写成 JSON 数字。
	if n, ok := out["amount"].(float64); !ok || n != 42 {
		t.Fatalf("amount = %#v (%T), want float64(42)", out["amount"], out["amount"])
	}
}

func TestFieldValidateCustomValuesReturnsNonNilMap(t *testing.T) {
	defs := []Field{{Code: "note", Label: "备注", Type: FieldTypeText, Enabled: true}}
	out, err := ValidateCustomValues(defs, nil)
	if err != nil {
		t.Fatal(err)
	}
	// nil 会被序列化成 JSON null，前端读 null 的字段直接崩页：必须是空 map。
	if out == nil || len(out) != 0 {
		t.Fatalf("out = %#v, want non-nil empty map", out)
	}
	raw, err := json.Marshal(out)
	if err != nil || string(raw) != "{}" {
		t.Fatalf("marshal = %s (%v), want {}", raw, err)
	}
}

func TestFieldValidateCustomValuesNoDefs(t *testing.T) {
	out, err := ValidateCustomValues(nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if out == nil || len(out) != 0 {
		t.Fatalf("out = %#v, want non-nil empty map", out)
	}
	// 一个字段都没定义时，任何 key 都是乱塞。
	if _, err := ValidateCustomValues(nil, map[string]any{"x": "1"}); err == nil {
		t.Fatal("value accepted with no field defined")
	}
}

func TestFieldMergeCustomValues(t *testing.T) {
	defs := fieldDefs()
	old := map[string]any{"vip_level": "gold", "note": "旧备注"}

	out, err := MergeCustomValues(old, map[string]any{"note": nil}, defs)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := out["note"]; ok {
		t.Fatalf("nil patch must delete the key, got %#v", out)
	}
	if out["vip_level"] != "gold" {
		t.Fatalf("old values must survive, got %#v", out)
	}

	// 覆盖 + 新增值。
	out, err = MergeCustomValues(old, map[string]any{"note": "新备注", "amount": "7"}, defs)
	if err != nil {
		t.Fatal(err)
	}
	if out["note"] != "新备注" || out["amount"].(float64) != 7 {
		t.Fatalf("merged = %#v", out)
	}

	// 删掉必填项必须报错：漏掉这步重校验，工单会退化成「必填字段为空」的历史脏数据。
	mustKey(t, mustMergeErr(t, old, map[string]any{"vip_level": nil}, defs), 400, "ticket.field_required")
	// patch 里的非法值同样在合并这一步被挡下。
	mustKey(t, mustMergeErr(t, old, map[string]any{"vip_level": "bronze"}, defs), 400, "ticket.field_value_invalid")
	mustKey(t, mustMergeErr(t, old, map[string]any{"secret": "x"}, defs), 400, "ticket.field_code_invalid")

	// old 里已停用/已删定义的 code：丢弃，不挡住改单。
	out, err = MergeCustomValues(map[string]any{"legacy": "脏值", "vip_level": "gold"}, map[string]any{"note": "x"}, defs)
	if err != nil {
		t.Fatalf("orphan value in old blocks the update: %v", err)
	}
	if _, ok := out["legacy"]; ok {
		t.Fatalf("orphan code must be dropped, got %#v", out)
	}
	// 前端清空一个已停用字段（patch 里是 nil）：删除语义要先于未知 code 校验，否则「删不掉」。
	if _, err := MergeCustomValues(map[string]any{"vip_level": "gold"}, map[string]any{"legacy": nil}, defs); err != nil {
		t.Fatalf("deleting an orphan code must succeed: %v", err)
	}

	// 合并是纯函数：不许改动入参 map（调用方拿着 old 还要做 diff）。
	if len(old) != 2 || old["note"] != "旧备注" {
		t.Fatalf("old mutated by MergeCustomValues: %#v", old)
	}
	if _, err := MergeCustomValues(old, map[string]any{"note": nil}, defs); err != nil {
		t.Fatal(err)
	}
	if len(old) != 2 {
		t.Fatalf("old mutated: %#v", old)
	}
}

func TestFieldEncodeCustomValues(t *testing.T) {
	got, err := EncodeCustomValues(nil)
	if err != nil || got != "{}" {
		t.Fatalf("nil map -> %q (%v), want {}", got, err)
	}
	if got, err = EncodeCustomValues(map[string]any{}); err != nil || got != "{}" {
		t.Fatalf("empty map -> %q (%v), want {}", got, err)
	}
	got, err = EncodeCustomValues(map[string]any{"note": "hi", "amount": float64(7)})
	if err != nil {
		t.Fatal(err)
	}
	var back map[string]any
	if err := json.Unmarshal([]byte(got), &back); err != nil {
		t.Fatalf("encoded value is not valid JSON: %q", got)
	}
	if back["note"] != "hi" || back["amount"].(float64) != 7 {
		t.Fatalf("round trip = %#v", back)
	}
	// NaN 之类的值 JSON 写不出来：必须报错，不许悄悄落一个 {} 把用户的值吞掉。
	if _, err := EncodeCustomValues(map[string]any{"amount": math.NaN()}); err == nil {
		t.Fatal("NaN encoded without error")
	}
}

func TestFieldDecodeCustomValuesDirtyInput(t *testing.T) {
	// 三种脏输入 + 数组/对象错位都必须回落空 map 且不 panic（工单详情不能因为一列脏值打不开）。
	for _, raw := range []string{"", "   ", "null", "not json", "[1,2]", `{"a":`, `"str"`, "17", "true"} {
		got := func() (out map[string]any) {
			defer func() {
				if r := recover(); r != nil {
					t.Fatalf("DecodeCustomValues(%q) panicked: %v", raw, r)
				}
			}()
			return DecodeCustomValues(raw)
		}()
		if got == nil {
			t.Fatalf("DecodeCustomValues(%q) = nil, want empty map", raw)
		}
		if len(got) != 0 {
			t.Fatalf("DecodeCustomValues(%q) = %#v, want empty map", raw, got)
		}
	}
	got := DecodeCustomValues(`{"note":"hi","amount":7}`)
	if got["note"] != "hi" || got["amount"].(float64) != 7 {
		t.Fatalf("decoded = %#v", got)
	}
	// Encode/Decode 往返：写进去读回来必须同一份。
	encoded, err := EncodeCustomValues(map[string]any{"vip_level": "gold"})
	if err != nil {
		t.Fatal(err)
	}
	if back := DecodeCustomValues(encoded); back["vip_level"] != "gold" {
		t.Fatalf("round trip = %#v", back)
	}
}

// ---------------------------------------------------------------------------
// DB 侧（sqlmock：只验语句形状与参数）
// ---------------------------------------------------------------------------

const (
	sqlFieldCount = "SELECT count\\(\\*\\) FROM `plg_ticket_field` WHERE code = \\? AND id <> \\?"
	sqlFieldTake  = "SELECT \\* FROM `plg_ticket_field` WHERE id = \\?"
	sqlFieldAll   = "SELECT \\* FROM `plg_ticket_field` ORDER BY sort ASC, id ASC"
	sqlFieldIns   = "INSERT INTO `plg_ticket_field`"
	sqlFieldUpd   = "UPDATE `plg_ticket_field` SET"
	sqlFieldDel   = "DELETE FROM `plg_ticket_field` WHERE id = \\?"
)

func TestFieldSaveFieldCreatesRow(t *testing.T) {
	db, m := mockDB(t)
	s := NewService(db, nopLogger{})

	m.ExpectQuery(sqlFieldCount).WithArgs("instrument", int64(0)).WillReturnRows(countRow(0))
	m.ExpectBegin()
	// 列顺序 = 结构体字段顺序（含 PlatformModel 的 id/created_at/updated_at）；
	// id 是雪花值，测试里不可预期，用 AnyArg，其余全部钉死。
	m.ExpectExec(sqlFieldIns).
		WithArgs(sqlmock.AnyArg(), testNow, testNow, "instrument", "乐器", FieldTypeSelect, "viola,cello",
			true, true, int64(3), int64(7)).
		WillReturnResult(sqlmock.NewResult(1, 1))
	m.ExpectCommit()

	got, err := s.SaveField(testCtx(), Field{
		Code: "instrument", Label: "乐器", Type: FieldTypeSelect, OptionList: []string{"viola", "cello"},
		Required: true, Enabled: true, Sort: 3, GroupID: 7,
	})
	if err != nil {
		t.Fatal(err)
	}
	if int64(got.ID) == 0 {
		t.Fatal("created field has no id")
	}
	if got.Options != "viola,cello" || len(got.OptionList) != 2 {
		t.Fatalf("created field = %+v", got)
	}
	if err := m.ExpectationsWereMet(); err != nil {
		t.Error(err)
	}
}

func TestFieldSaveFieldUpdatesRow(t *testing.T) {
	db, m := mockDB(t)
	s := NewService(db, nopLogger{})

	const fieldID = int64(5001)
	m.ExpectQuery(sqlFieldCount).WithArgs("note", int64(fieldID)).WillReturnRows(countRow(0))
	m.ExpectQuery(sqlFieldTake).WithArgs(fieldID, 1).
		WillReturnRows(fieldRow(fieldID, "legacy_code", "旧名", FieldTypeText, "", false, true, 1, 0))
	m.ExpectBegin()
	// map 更新的列按 key 字母序（GORM 排序，保证 SQL 可复现），updated_at 由框架补在最后，再跟 WHERE id。
	m.ExpectExec(sqlFieldUpd).
		WithArgs("note", true, int64(9), "备注", "a,b", true, int64(2), FieldTypeSelect, testNow, fieldID).
		WillReturnResult(sqlmock.NewResult(0, 1))
	m.ExpectCommit()

	got, err := s.SaveField(testCtx(), Field{
		PlatformModel: platformID(fieldID),
		Code:          "note", Label: "备注", Type: FieldTypeSelect, OptionList: []string{"a", "b"},
		Required: true, Enabled: true, Sort: 2, GroupID: 9,
	})
	if err != nil {
		t.Fatal(err)
	}
	// created_at 必须沿用建行时的值（取回的那行），不能被入参覆盖成零值。
	if !got.CreatedAt.Equal(testNow) {
		t.Fatalf("created_at = %v, want %v", got.CreatedAt, testNow)
	}
	if got.ID != idgen.ID(fieldID) || got.Options != "a,b" {
		t.Fatalf("updated field = %+v", got)
	}
	if err := m.ExpectationsWereMet(); err != nil {
		t.Error(err)
	}
}

func TestFieldSaveFieldRejectsDuplicateCode(t *testing.T) {
	db, m := mockDB(t)
	s := NewService(db, nopLogger{})

	m.ExpectQuery(sqlFieldCount).WithArgs("note", int64(0)).WillReturnRows(countRow(1))
	_, err := s.SaveField(testCtx(), Field{Code: "note", Label: "备注", Type: FieldTypeText, Enabled: true})
	mustKey(t, err, 409, "ticket.field_code_conflict")
	// 冲突后一条写 SQL 都不许发出（否则会撞唯一键变成 500）。
	if err := m.ExpectationsWereMet(); err != nil {
		t.Error(err)
	}
}

func TestFieldSaveFieldRejectsInvalidDefinition(t *testing.T) {
	db, m := mockDB(t)
	s := NewService(db, nopLogger{})

	// 定义不合法时在发 SQL 之前就要拦住。
	_, err := s.SaveField(testCtx(), Field{Code: "Note", Label: "备注", Type: FieldTypeText})
	mustKey(t, err, 400, "ticket.field_code_invalid")
	if err := m.ExpectationsWereMet(); err != nil {
		t.Error(err)
	}
}

func TestFieldSaveFieldUpdateMissingRow(t *testing.T) {
	db, m := mockDB(t)
	s := NewService(db, nopLogger{})

	m.ExpectQuery(sqlFieldCount).WithArgs("note", int64(404)).WillReturnRows(countRow(0))
	m.ExpectQuery(sqlFieldTake).WithArgs(int64(404), 1).WillReturnRows(emptyRow(fieldMockColumns...))
	_, err := s.SaveField(testCtx(), Field{
		PlatformModel: platformID(404), Code: "note", Label: "备注", Type: FieldTypeText, Enabled: true,
	})
	mustKey(t, err, 404, "error.not_found")
	// 找不到就不能发 UPDATE。
	if err := m.ExpectationsWereMet(); err != nil {
		t.Error(err)
	}
}

func TestFieldListFieldsLoadsOptions(t *testing.T) {
	db, m := mockDB(t)
	s := NewService(db, nopLogger{})

	m.ExpectQuery(sqlFieldAll).WillReturnRows(
		sqlmock.NewRows(fieldMockColumns).
			AddRow(int64(1), testNow, testNow, "instrument", "乐器", FieldTypeSelect, "viola,cello", false, true, 0, int64(0)).
			AddRow(int64(2), testNow, testNow, "note", "备注", FieldTypeText, "", false, false, 1, int64(7)),
	)
	rows, err := s.ListFields(testCtx())
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 {
		t.Fatalf("rows = %d", len(rows))
	}
	// GORM 必须对每行调 AfterFind：没有它，管理端拿到的是空候选项。
	if len(rows[0].OptionList) != 2 || rows[0].OptionList[1] != "cello" {
		t.Fatalf("OptionList = %#v", rows[0].OptionList)
	}
	if rows[1].Enabled {
		t.Fatal("enabled read back wrong")
	}
	if rows[1].GroupID != 7 {
		t.Fatalf("group_id = %d", rows[1].GroupID)
	}
	if err := m.ExpectationsWereMet(); err != nil {
		t.Error(err)
	}
}

func TestFieldEnabledFieldsFilters(t *testing.T) {
	db, m := mockDB(t)
	s := NewService(db, nopLogger{})

	m.ExpectQuery("SELECT \\* FROM `plg_ticket_field` WHERE enabled = \\?").WithArgs(true).
		WillReturnRows(fieldRow(1, "note", "备注", FieldTypeText, "", false, true, 0, 0))
	rows, err := s.EnabledFields(testCtx())
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0].Code != "note" {
		t.Fatalf("rows = %#v", rows)
	}
	if err := m.ExpectationsWereMet(); err != nil {
		t.Error(err)
	}
}

func TestFieldDeleteFieldRemovesDefinition(t *testing.T) {
	db, m := mockDB(t)
	s := NewService(db, nopLogger{})

	m.ExpectBegin()
	m.ExpectExec(sqlFieldDel).WithArgs(int64(2001)).WillReturnResult(sqlmock.NewResult(0, 1))
	m.ExpectCommit()
	if err := s.DeleteField(testCtx(), 2001); err != nil {
		t.Fatal(err)
	}
	if err := m.ExpectationsWereMet(); err != nil {
		t.Error(err)
	}
}

func TestFieldDeleteFieldMissingRow(t *testing.T) {
	db, m := mockDB(t)
	s := NewService(db, nopLogger{})

	m.ExpectBegin()
	m.ExpectExec(sqlFieldDel).WithArgs(int64(404)).WillReturnResult(sqlmock.NewResult(0, 0))
	m.ExpectCommit()
	// 删不动就是不存在：这条断言挡住「删了个空气却返回 200」。
	mustKey(t, s.DeleteField(testCtx(), 404), 404, "error.not_found")
	if err := m.ExpectationsWereMet(); err != nil {
		t.Error(err)
	}
}

// ---------------------------------------------------------------------------
// 小工具
// ---------------------------------------------------------------------------

func platformID(id int64) database.PlatformModel {
	var m database.PlatformModel
	m.ID = idgen.ID(id)
	return m
}

func mustValueErr(t *testing.T, f Field, raw any) error {
	t.Helper()
	_, err := f.ValidateValue(raw)
	if err == nil {
		t.Fatalf("ValidateValue(%#v) accepted, want rejection", raw)
	}
	return err
}

func mustValuesErr(t *testing.T, defs []Field, in map[string]any) error {
	t.Helper()
	_, err := ValidateCustomValues(defs, in)
	if err == nil {
		t.Fatalf("ValidateCustomValues(%#v) accepted, want rejection", in)
	}
	return err
}

func mustMergeErr(t *testing.T, old, patch map[string]any, defs []Field) error {
	t.Helper()
	_, err := MergeCustomValues(old, patch, defs)
	if err == nil {
		t.Fatalf("MergeCustomValues(%#v, %#v) accepted, want rejection", old, patch)
	}
	return err
}
