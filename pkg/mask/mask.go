// Package mask 提供字段级数据脱敏：常用函数(手机号/邮箱/身份证/银行卡/姓名) +
// 反射遍历(按 `mask:"phone"` 等标签自动替换)。业务 handler 在返回敏感数据前调用。
//
// 用法一(显式)：response.OK(c, gin.H{"phone": mask.Phone(p)})
// 用法二(标签)：
//
//	type UserView struct {
//	    Phone string `json:"phone" mask:"phone"`
//	    Email string `json:"email" mask:"email"`
//	}
//	response.OK(c, mask.Apply(view))
//
// 标签支持 phone / email / id_card / bank_card / name / custom:<keepPrefix>:<keepSuffix>。
package mask

import (
	"reflect"
	"strconv"
	"strings"
)

// Phone 手机号：保留前 3 后 4，中间 ****（如 138****1234）。
func Phone(s string) string {
	return Custom(s, 3, 4)
}

// Email 邮箱：保留 @ 前 2 字符 + 完整域名（如 ab***@example.com）。
func Email(s string) string {
	at := strings.Index(s, "@")
	if at <= 0 {
		return "***"
	}
	prefix := s[:at]
	if len(prefix) > 2 {
		prefix = prefix[:2]
	}
	return prefix + "***" + s[at:]
}

// IDCard 身份证：保留前 6 后 4（如 110101****1234）。
func IDCard(s string) string {
	return Custom(s, 6, 4)
}

// BankCard 银行卡：只保留后 4（如 **** **** **** 1234）。
func BankCard(s string) string {
	if len(s) <= 4 {
		return strings.Repeat("*", len(s))
	}
	return strings.Repeat("*", len(s)-4) + s[len(s)-4:]
}

// Name 姓名：保留第一个字符，其余 **（中文名）或前 2 字符（英文）。
func Name(s string) string {
	if s == "" {
		return ""
	}
	runes := []rune(s)
	if len(runes) <= 1 {
		return s
	}
	if len(runes) <= 3 {
		return string(runes[0]) + "**"
	}
	return string(runes[0:2]) + strings.Repeat("*", len(runes)-2)
}

// Custom 保留前 keepPrefix 和后 keepSuffix 字符，中间用 * 替换（每个原始字符一个 *）。
// 字符串太短时全部替换为 ***。
func Custom(s string, keepPrefix, keepSuffix int) string {
	if s == "" {
		return ""
	}
	runes := []rune(s)
	if len(runes) <= keepPrefix+keepSuffix {
		return "***"
	}
	return string(runes[:keepPrefix]) + strings.Repeat("*", len(runes)-keepPrefix-keepSuffix) + string(runes[len(runes)-keepSuffix:])
}

// Apply 递归遍历 v（struct / []struct / *struct / map），
// 对 struct 字段标签 `mask:"phone"` 的 string 字段应用对应脱敏函数。
// 返回副本（不改原始数据）；struct 返回指针以便类型断言。
func Apply(v any) any {
	return applyValue(reflect.ValueOf(v))
}

func applyValue(v reflect.Value) any {
	switch v.Kind() {
	case reflect.Ptr:
		if v.IsNil() {
			return v.Interface()
		}
		if v.Elem().Kind() != reflect.Struct {
			return v.Interface()
		}
		masked := applyStruct(v.Elem())
		p := reflect.New(v.Elem().Type())
		p.Elem().Set(reflect.ValueOf(masked))
		return p.Interface()
	case reflect.Struct:
		return applyStruct(v)
	case reflect.Slice, reflect.Array:
		out := reflect.MakeSlice(v.Type(), v.Len(), v.Len())
		for i := 0; i < v.Len(); i++ {
			item := v.Index(i)
			if item.Kind() == reflect.Struct {
				masked := applyStruct(item)
				out.Index(i).Set(reflect.ValueOf(masked))
			} else if item.Kind() == reflect.Ptr && !item.IsNil() {
				masked := applyStruct(item.Elem())
				ptr := reflect.New(item.Elem().Type())
				ptr.Elem().Set(reflect.ValueOf(masked))
				out.Index(i).Set(ptr)
			} else {
				out.Index(i).Set(item)
			}
		}
		return out.Interface()
	case reflect.Map:
		out := reflect.MakeMap(v.Type())
		iter := v.MapRange()
		for iter.Next() {
			inner := applyValue(iter.Value())
			out.SetMapIndex(iter.Key(), reflect.ValueOf(inner))
		}
		return out.Interface()
	default:
		return v.Interface()
	}
}

func applyStruct(v reflect.Value) any {
	t := v.Type()
	out := reflect.New(t).Elem()
	for i := 0; i < t.NumField(); i++ {
		field := t.Field(i)
		tag := field.Tag.Get("mask")
		if tag == "" {
			if field.IsExported() {
				inner := v.Field(i)
				switch inner.Kind() {
				case reflect.Struct:
					out.Field(i).Set(reflect.ValueOf(applyStruct(inner)))
				case reflect.Ptr:
					if !inner.IsNil() && inner.Elem().Kind() == reflect.Struct {
						masked := applyStruct(inner.Elem())
						ptr := reflect.New(inner.Elem().Type())
						ptr.Elem().Set(reflect.ValueOf(masked))
						out.Field(i).Set(ptr)
					} else {
						out.Field(i).Set(inner)
					}
				case reflect.Slice, reflect.Map:
					if !inner.IsNil() {
						out.Field(i).Set(reflect.ValueOf(applyValue(inner)))
					} else {
						out.Field(i).Set(inner)
					}
				default:
					out.Field(i).Set(inner)
				}
			} else {
				out.Field(i).Set(v.Field(i))
			}
			continue
		}
		if !field.IsExported() || v.Field(i).Kind() != reflect.String {
			out.Field(i).Set(v.Field(i))
			continue
		}
		s := v.Field(i).String()
		if s == "" {
			out.Field(i).Set(v.Field(i))
			continue
		}
		out.Field(i).Set(reflect.ValueOf(applyTag(tag, s)))
	}
	return out.Interface()
}

func applyTag(tag, s string) string {
	switch tag {
	case "phone":
		return Phone(s)
	case "email":
		return Email(s)
	case "id_card":
		return IDCard(s)
	case "bank_card":
		return BankCard(s)
	case "name":
		return Name(s)
	}
	if strings.HasPrefix(tag, "custom:") {
		parts := strings.SplitN(strings.TrimPrefix(tag, "custom:"), ":", 2)
		p, _ := strconv.Atoi(parts[0])
		sfx := 0
		if len(parts) > 1 {
			sfx, _ = strconv.Atoi(parts[1])
		}
		return Custom(s, p, sfx)
	}
	return s
}
