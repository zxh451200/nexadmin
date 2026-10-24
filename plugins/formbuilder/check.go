package formbuilder

import (
	"fmt"
	"math"
	"net/url"
	"strings"
	"time"
	"unicode/utf8"
)

const maxNumber = 1e15

// FieldError 是单个字段的校验失败。
type FieldError struct {
	Key string `json:"key"`
	Msg string `json:"msg"`
}

// ValidateSubmission 按定义 def 重新校验并清洗提交的原始数据 raw，返回可存储的 clean。
//   - 按字段顺序求显隐条件：隐藏字段不校验必填，提交的值直接丢弃；
//   - 定义里没有的键一律丢弃；
//   - 类型错配、越界、超长、正则不匹配都报错（不做隐式转换）。
func ValidateSubmission(def *Definition, raw map[string]any) (map[string]any, []FieldError) {
	clean := make(map[string]any, len(def.Fields))
	var errs []FieldError
	if len(raw) > MaxFields*4 {
		return nil, []FieldError{{Msg: "提交的字段过多"}}
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
				errs = append(errs, FieldError{f.Key, "必填"})
			}
			continue
		}
		norm, err := checkValue(f, v)
		if err != nil {
			errs = append(errs, FieldError{f.Key, err.Error()})
			continue
		}
		if f.Type == TypeSwitch && f.Required && norm != true {
			errs = append(errs, FieldError{f.Key, "必须开启"})
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
			return nil, fmt.Errorf("需要布尔值")
		}
		return b, nil
	case TypeRating:
		return checkRating(f, v)
	case TypeFile:
		return checkFiles(f, v)
	}
	return nil, fmt.Errorf("未知字段类型")
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
		return nil, fmt.Errorf("需要文本")
	}
	if !utf8.ValidString(s) || hasBadControl(s, f.Type == TypeTextarea) {
		return nil, fmt.Errorf("包含非法字符")
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
		return nil, fmt.Errorf("长度不能超过 %d", limit)
	}
	if f.MinLen != nil && n < *f.MinLen {
		return nil, fmt.Errorf("长度不能少于 %d", *f.MinLen)
	}
	if f.Pattern != "" {
		re, err := compilePattern(f.Pattern)
		if err != nil {
			return nil, fmt.Errorf("字段的正则规则不可用")
		}
		if !re.MatchString(s) {
			return nil, fmt.Errorf("格式不正确")
		}
	}
	return s, nil
}

func checkNumber(f *Field, v any) (any, error) {
	n, ok := v.(float64)
	if !ok || math.IsNaN(n) || math.IsInf(n, 0) {
		return nil, fmt.Errorf("需要数字")
	}
	if math.Abs(n) > maxNumber {
		return nil, fmt.Errorf("数值超出范围")
	}
	if f.Integer && n != math.Trunc(n) {
		return nil, fmt.Errorf("需要整数")
	}
	if f.Min != nil && n < *f.Min {
		return nil, fmt.Errorf("不能小于 %v", *f.Min)
	}
	if f.Max != nil && n > *f.Max {
		return nil, fmt.Errorf("不能大于 %v", *f.Max)
	}
	return n, nil
}

func checkOne(f *Field, v any) (any, error) {
	s, ok := v.(string)
	if !ok {
		return nil, fmt.Errorf("需要选项值")
	}
	if _, ok := f.optionSet()[s]; !ok {
		return nil, fmt.Errorf("不是有效选项")
	}
	return s, nil
}

func checkMany(f *Field, v any) (any, error) {
	arr, ok := v.([]any)
	if !ok {
		return nil, fmt.Errorf("需要选项数组")
	}
	set := f.optionSet()
	if len(arr) > len(set) {
		return nil, fmt.Errorf("选择的数量超出选项数")
	}
	seen := map[string]struct{}{}
	out := make([]any, 0, len(arr))
	for _, e := range arr {
		s, ok := e.(string)
		if !ok {
			return nil, fmt.Errorf("需要选项值")
		}
		if _, ok := set[s]; !ok {
			return nil, fmt.Errorf("不是有效选项")
		}
		if _, dup := seen[s]; dup {
			return nil, fmt.Errorf("选项重复")
		}
		seen[s] = struct{}{}
		out = append(out, s)
	}
	if f.Min != nil && float64(len(out)) < *f.Min {
		return nil, fmt.Errorf("至少选择 %v 项", *f.Min)
	}
	if f.Max != nil && float64(len(out)) > *f.Max {
		return nil, fmt.Errorf("最多选择 %v 项", *f.Max)
	}
	return out, nil
}

func checkDate(f *Field, v any) (any, error) {
	s, ok := v.(string)
	if !ok || len(s) != 10 {
		return nil, fmt.Errorf("日期格式应为 yyyy-MM-dd")
	}
	if _, err := time.Parse("2006-01-02", s); err != nil {
		return nil, fmt.Errorf("日期格式应为 yyyy-MM-dd")
	}
	if f.MinDate != "" && s < f.MinDate {
		return nil, fmt.Errorf("不能早于 %s", f.MinDate)
	}
	if f.MaxDate != "" && s > f.MaxDate {
		return nil, fmt.Errorf("不能晚于 %s", f.MaxDate)
	}
	return s, nil
}

func checkRating(f *Field, v any) (any, error) {
	n, ok := v.(float64)
	if !ok || n != math.Trunc(n) {
		return nil, fmt.Errorf("需要整数评分")
	}
	max := 5.0
	if f.Max != nil {
		max = *f.Max
	}
	if n < 1 || n > max {
		return nil, fmt.Errorf("评分范围 1-%v", max)
	}
	return n, nil
}

func checkFiles(f *Field, v any) (any, error) {
	arr, ok := v.([]any)
	if !ok {
		return nil, fmt.Errorf("需要文件数组")
	}
	limit := 5.0
	if f.Max != nil {
		limit = *f.Max
	}
	if float64(len(arr)) > limit {
		return nil, fmt.Errorf("最多 %v 个文件", limit)
	}
	if f.Min != nil && float64(len(arr)) < *f.Min {
		return nil, fmt.Errorf("至少 %v 个文件", *f.Min)
	}
	out := make([]any, 0, len(arr))
	for _, e := range arr {
		m, ok := e.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("文件格式不正确")
		}
		name, _ := m["name"].(string)
		u, _ := m["url"].(string)
		if name == "" || utf8.RuneCountInString(name) > 200 || !utf8.ValidString(name) || hasBadControl(name, false) {
			return nil, fmt.Errorf("文件名不合法")
		}
		if !validFileURL(u) {
			return nil, fmt.Errorf("文件地址不合法")
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
