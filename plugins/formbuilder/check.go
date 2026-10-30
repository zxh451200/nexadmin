package formbuilder

import (
	"context"
	"errors"
	"math"
	"net/url"
	"strings"
	"time"
	"unicode/utf8"

	"nexadmin/core/i18n"
)

const maxNumber = 1e15

// FieldError 是单个字段的校验失败：MsgKey + Args 由接口层按请求语言翻译成文案。
type FieldError struct {
	Key    string `json:"key"`
	MsgKey string `json:"msgKey"`
	Args   []any  `json:"args,omitempty"`
}

// fieldMsg 是字段级校验失败的原因（i18n key + 占位符参数）。
// Error() 只服务于没有 ctx 的场景（如定义校验里的默认值检查），按默认语言渲染。
type fieldMsg struct {
	key  string
	args []any
}

func (e *fieldMsg) Error() string { return i18n.T(context.Background(), e.key, e.args...) }

// fieldErr 构造一个字段级校验失败原因。
func fieldErr(key string, args ...any) error { return &fieldMsg{key: key, args: args} }

// newFieldError 把 checkValue 的错误转成字段错误；非 fieldMsg（理论上不存在）落到通用原因。
func newFieldError(key string, err error) FieldError {
	var m *fieldMsg
	if errors.As(err, &m) {
		return FieldError{Key: key, MsgKey: m.key, Args: m.args}
	}
	return FieldError{Key: key, MsgKey: "formbuilder.invalid_value"}
}

// ValidateSubmission 按定义 def 重新校验并清洗提交的原始数据 raw，返回可存储的 clean。
//   - 按字段顺序求显隐条件：隐藏字段不校验必填，提交的值直接丢弃；
//   - 定义里没有的键一律丢弃；
//   - 类型错配、越界、超长、正则不匹配都报错（不做隐式转换）。
func ValidateSubmission(def *Definition, raw map[string]any) (map[string]any, []FieldError) {
	clean := make(map[string]any, len(def.Fields))
	var errs []FieldError
	if len(raw) > MaxFields*4 {
		return nil, []FieldError{{MsgKey: "formbuilder.too_many_fields"}}
	}
	for i := range def.Fields {
		f := &def.Fields[i]
		if f.ShowIf != nil && !evalCond(f.ShowIf, clean) {
			continue // 隐藏：不校验、不保存
		}
		if !f.hasValue() {
			continue
		}
		v, present := raw[f.Key]
		if !present || isBlank(f, v) {
			if f.Required {
				errs = append(errs, FieldError{Key: f.Key, MsgKey: "formbuilder.required"})
			}
			continue
		}
		norm, err := checkValue(f, v)
		if err != nil {
			errs = append(errs, newFieldError(f.Key, err))
			continue
		}
		if f.Type == TypeSwitch && f.Required && norm != true {
			errs = append(errs, FieldError{Key: f.Key, MsgKey: "formbuilder.switch_required"})
			continue
		}
		clean[f.Key] = norm
	}
	if len(errs) > 0 {
		return nil, errs
	}
	return clean, nil
}

// isBlank：nil、空白字符串、空数组视为未填；0 与 false 是有效值（switch 的必填语义在调用处处理）。
func isBlank(f *Field, v any) bool {
	switch x := v.(type) {
	case nil:
		return true
	case string:
		return strings.TrimSpace(x) == "" && (f.Type == TypeText || f.Type == TypeTextarea || f.Type == TypeDate || f.Type == TypeRadio || f.Type == TypeSelect)
	case []any:
		return len(x) == 0
	}
	return false
}

// checkValue 校验单个值并返回规范化后的值。
func checkValue(f *Field, v any) (any, error) {
	switch f.Type {
	case TypeText, TypeTextarea:
		return checkString(f, v)
	case TypeNumber:
		return checkNumber(f, v)
	case TypeRadio:
		return checkOne(f, v)
	case TypeSelect:
		if f.Multiple {
			return checkMany(f, v)
		}
		return checkOne(f, v)
	case TypeCheckbox:
		return checkMany(f, v)
	case TypeDate:
		return checkDate(f, v)
	case TypeSwitch:
		b, ok := v.(bool)
		if !ok {
			return nil, fieldErr("formbuilder.need_bool")
		}
		return b, nil
	case TypeRating:
		return checkRating(f, v)
	case TypeFile:
		return checkFiles(f, v)
	}
	return nil, fieldErr("formbuilder.unknown_type")
}

func hasBadControl(s string, allowNewline bool) bool {
	for _, r := range s {
		if r == 0 || r == 0x7f || (r < 0x20 && !(allowNewline && (r == '\n' || r == '\r' || r == '\t'))) {
			return true
		}
	}
	return false
}

func checkString(f *Field, v any) (any, error) {
	s, ok := v.(string)
	if !ok {
		return nil, fieldErr("formbuilder.need_text")
	}
	if !utf8.ValidString(s) || hasBadControl(s, f.Type == TypeTextarea) {
		return nil, fieldErr("formbuilder.invalid_chars")
	}
	limit := DefaultTextRunes
	if f.Type == TypeTextarea {
		limit = DefaultAreaRunes
	}
	if f.MaxLen != nil {
		limit = *f.MaxLen
	}
	n := utf8.RuneCountInString(s)
	if n > limit {
		return nil, fieldErr("formbuilder.text_too_long", limit)
	}
	if f.MinLen != nil && n < *f.MinLen {
		return nil, fieldErr("formbuilder.text_too_short", *f.MinLen)
	}
	if f.Pattern != "" {
		re, err := compilePattern(f.Pattern)
		if err != nil {
			return nil, fieldErr("formbuilder.pattern_unavailable")
		}
		if !re.MatchString(s) {
			return nil, fieldErr("formbuilder.pattern_mismatch")
		}
	}
	return s, nil
}

func checkNumber(f *Field, v any) (any, error) {
	n, ok := v.(float64)
	if !ok || math.IsNaN(n) || math.IsInf(n, 0) {
		return nil, fieldErr("formbuilder.need_number")
	}
	if math.Abs(n) > maxNumber {
		return nil, fieldErr("formbuilder.number_out_of_range")
	}
	if f.Integer && n != math.Trunc(n) {
		return nil, fieldErr("formbuilder.need_integer")
	}
	if f.Min != nil && n < *f.Min {
		return nil, fieldErr("formbuilder.below_min", *f.Min)
	}
	if f.Max != nil && n > *f.Max {
		return nil, fieldErr("formbuilder.above_max", *f.Max)
	}
	return n, nil
}

func checkOne(f *Field, v any) (any, error) {
	s, ok := v.(string)
	if !ok {
		return nil, fieldErr("formbuilder.need_choice")
	}
	if _, ok := f.optionSet()[s]; !ok {
		return nil, fieldErr("formbuilder.invalid_choice")
	}
	return s, nil
}

func checkMany(f *Field, v any) (any, error) {
	arr, ok := v.([]any)
	if !ok {
		return nil, fieldErr("formbuilder.need_choice_list")
	}
	set := f.optionSet()
	if len(arr) > len(set) {
		return nil, fieldErr("formbuilder.too_many_choices")
	}
	seen := map[string]struct{}{}
	out := make([]any, 0, len(arr))
	for _, e := range arr {
		s, ok := e.(string)
		if !ok {
			return nil, fieldErr("formbuilder.need_choice")
		}
		if _, ok := set[s]; !ok {
			return nil, fieldErr("formbuilder.invalid_choice")
		}
		if _, dup := seen[s]; dup {
			return nil, fieldErr("formbuilder.duplicate_choice")
		}
		seen[s] = struct{}{}
		out = append(out, s)
	}
	if f.Min != nil && float64(len(out)) < *f.Min {
		return nil, fieldErr("formbuilder.select_min", *f.Min)
	}
	if f.Max != nil && float64(len(out)) > *f.Max {
		return nil, fieldErr("formbuilder.select_max", *f.Max)
	}
	return out, nil
}

func checkDate(f *Field, v any) (any, error) {
	s, ok := v.(string)
	if !ok || len(s) != 10 {
		return nil, fieldErr("formbuilder.date_invalid")
	}
	if _, err := time.Parse("2006-01-02", s); err != nil {
		return nil, fieldErr("formbuilder.date_invalid")
	}
	if f.MinDate != "" && s < f.MinDate {
		return nil, fieldErr("formbuilder.date_before_min", f.MinDate)
	}
	if f.MaxDate != "" && s > f.MaxDate {
		return nil, fieldErr("formbuilder.date_after_max", f.MaxDate)
	}
	return s, nil
}

func checkRating(f *Field, v any) (any, error) {
	n, ok := v.(float64)
	if !ok || n != math.Trunc(n) {
		return nil, fieldErr("formbuilder.rating_invalid")
	}
	max := 5.0
	if f.Max != nil {
		max = *f.Max
	}
	if n < 1 || n > max {
		return nil, fieldErr("formbuilder.rating_range", max)
	}
	return n, nil
}

func checkFiles(f *Field, v any) (any, error) {
	arr, ok := v.([]any)
	if !ok {
		return nil, fieldErr("formbuilder.need_file_list")
	}
	limit := 5.0
	if f.Max != nil {
		limit = *f.Max
	}
	if float64(len(arr)) > limit {
		return nil, fieldErr("formbuilder.files_too_many", limit)
	}
	if f.Min != nil && float64(len(arr)) < *f.Min {
		return nil, fieldErr("formbuilder.files_too_few", *f.Min)
	}
	out := make([]any, 0, len(arr))
	for _, e := range arr {
		m, ok := e.(map[string]any)
		if !ok {
			return nil, fieldErr("formbuilder.file_item_invalid")
		}
		name, _ := m["name"].(string)
		u, _ := m["url"].(string)
		if name == "" || utf8.RuneCountInString(name) > 200 || !utf8.ValidString(name) || hasBadControl(name, false) {
			return nil, fieldErr("formbuilder.file_name_invalid")
		}
		if !validFileURL(u) {
			return nil, fieldErr("formbuilder.file_url_invalid")
		}
		item := map[string]any{"name": name, "url": u}
		if size, ok := m["size"].(float64); ok && size >= 0 && size < maxNumber {
			item["size"] = size
		}
		if typ, ok := m["type"].(string); ok && len(typ) <= 100 && !hasBadControl(typ, false) {
			item["type"] = typ
		}
		out = append(out, item)
	}
	return out, nil
}

// validFileURL 只接受站内绝对路径或 http(s) 地址，拒绝 javascript:、data: 等可被当作脚本渲染的协议。
func validFileURL(s string) bool {
	if s == "" || len(s) > 500 || hasBadControl(s, false) || strings.ContainsAny(s, "\\ \t\r\n") {
		return false
	}
	if strings.HasPrefix(s, "/") {
		return !strings.HasPrefix(s, "//")
	}
	u, err := url.Parse(s)
	return err == nil && (u.Scheme == "http" || u.Scheme == "https") && u.Host != ""
}
