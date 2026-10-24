package audit

import (
	"database/sql/driver"
	"fmt"
	"reflect"
	"sort"
	"strings"
	"time"
	"unicode/utf8"
)

// Masked 是敏感字段在记录里的替身值。
const Masked = "***"

const (
	maxValueRunes = 1000 // 单个字段值写入记录的上限（按字符），超出截断
	maxFieldsLen  = 500  // plg_audit_log.fields 摘要列的上限（字节）
)

// Change 是单个字段的前后值。新增时 Old 为 nil，删除时 New 为 nil。
type Change struct {
	Old any `json:"old"`
	New any `json:"new"`
}

// 字段名（忽略大小写与下划线/连字符）包含这些片段即视为敏感。
var sensitiveParts = []string{"password", "passwd", "pwd", "secret", "token", "credential", "apikey", "privatekey", "accesskey"}

// 噪音列：每次更新都会变，单独变化没有审计价值。
var ignoredColumns = map[string]struct{}{"updated_at": {}}

// IsSensitive 报告字段名是否敏感（password / secret / token 等）。
func IsSensitive(field string) bool {
	n := strings.NewReplacer("_", "", "-", "").Replace(strings.ToLower(field))
	for _, p := range sensitiveParts {
		if strings.Contains(n, p) {
			return true
		}
	}
	return false
}

// normalizeValue 把驱动返回值/结构体字段值统一成可比较、可 JSON 序列化的标量。
func normalizeValue(v any) any {
	if v == nil {
		return nil
	}
	rv := reflect.ValueOf(v)
	for rv.Kind() == reflect.Pointer || rv.Kind() == reflect.Interface {
		if rv.IsNil() {
			return nil
		}
		rv = rv.Elem()
	}
	v = rv.Interface()
	if vr, ok := v.(driver.Valuer); ok {
		dv, err := vr.Value()
		if err != nil || dv == nil {
			return nil
		}
		v = dv
	}
	switch x := v.(type) {
	case []byte:
		return string(x)
	case time.Time:
		return x.UTC().Format(time.RFC3339Nano)
	case bool, string, int, int8, int16, int32, int64, uint, uint8, uint16, uint32, uint64, float32, float64:
		return x
	}
	return fmt.Sprint(v)
}

func normalizeRow(row map[string]any) map[string]any {
	out := make(map[string]any, len(row))
	for k, v := range row {
		out[k] = normalizeValue(v)
	}
	return out
}

func sameValue(a, b any) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return fmt.Sprint(a) == fmt.Sprint(b)
}

func clip(v any) any {
	s, ok := v.(string)
	if !ok || utf8.RuneCountInString(s) <= maxValueRunes {
		return v
	}
	r := []rune(s)
	return string(r[:maxValueRunes]) + "…"
}

func prepare(field string, v any) any {
	if v == nil {
		return nil
	}
	if IsSensitive(field) {
		return Masked
	}
	return clip(v)
}

// Diff 比较前后两行（列名 -> 值），只返回有变化的字段；敏感字段值一律脱敏。
// old 为 nil 表示新增，new 为 nil 表示删除。两个入参都应已经 normalizeRow。
func Diff(old, new map[string]any) map[string]Change {
	out := map[string]Change{}
	seen := make(map[string]struct{}, len(old)+len(new))
	check := func(k string) {
		if _, dup := seen[k]; dup {
			return
		}
		seen[k] = struct{}{}
		if _, skip := ignoredColumns[k]; skip {
			return
		}
		o, n := old[k], new[k]
		if sameValue(o, n) {
			return
		}
		out[k] = Change{Old: prepare(k, o), New: prepare(k, n)}
	}
	for k := range old {
		check(k)
	}
	for k := range new {
		check(k)
	}
	return out
}

// FieldNames 返回变化字段名（排序后逗号连接，超长截断），用于列表摘要列。
func FieldNames(ch map[string]Change) string {
	names := make([]string, 0, len(ch))
	for k := range ch {
		names = append(names, k)
	}
	sort.Strings(names)
	s := strings.Join(names, ",")
	if len(s) > maxFieldsLen {
		s = strings.ToValidUTF8(s[:maxFieldsLen-3], "") + "..."
	}
	return s
}
