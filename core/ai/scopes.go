package ai

import (
	"encoding/json"
	"strings"
	"time"
)

// Kind 是筛选字段的取值类型。
type Kind string

// 字段类型。
const (
	KindEnum Kind = "enum"
	KindDate Kind = "date" // YYYY-MM-DD
	KindText Kind = "text" // 关键字，走后端原有的参数化模糊查询
)

const maxKeywordRunes = 50

// Option 是枚举取值及其含义（含义只给模型看）。
type Option struct {
	Value   string `json:"value"`
	Meaning string `json:"meaning"`
}

// Field 是页面可筛选字段的元数据，是模型能返回的唯一字段集合（白名单）。
type Field struct {
	Key     string   `json:"key"`
	Type    Kind     `json:"type"`
	Desc    string   `json:"description"`
	Options []Option `json:"values,omitempty"`
}

// Scope 是一个页面的筛选元数据。
type Scope struct {
	Name   string  `json:"scope"`
	Desc   string  `json:"description"`
	Fields []Field `json:"fields"`
}

var (
	kw       = Field{Key: "keyword", Type: KindText, Desc: "free-text keyword the user wants to search for"}
	opStatus = Field{Key: "status", Type: KindEnum, Desc: "operation result", Options: []Option{{"success", "succeeded"}, {"failed", "failed or error"}}}
)

// scopes 与前端 AiFilterBox 的 scope 及各页面筛选字段一一对应。
var scopes = map[string]*Scope{
	"user": {Name: "user", Desc: "user management list", Fields: []Field{
		kw,
		{Key: "status", Type: KindEnum, Desc: "account status", Options: []Option{{"1", "enabled"}, {"0", "disabled"}}},
		{Key: "gender", Type: KindEnum, Desc: "gender", Options: []Option{{"1", "male"}, {"2", "female"}, {"0", "unknown"}}},
		{Key: "createdFrom", Type: KindDate, Desc: "account created on or after this date (YYYY-MM-DD)"},
		{Key: "createdTo", Type: KindDate, Desc: "account created on or before this date (YYYY-MM-DD)"},
	}},
	"notice": {Name: "notice", Desc: "notice / announcement list", Fields: []Field{
		kw,
		{Key: "type", Type: KindEnum, Desc: "notice type", Options: []Option{{"notice", "notification"}, {"announcement", "announcement"}, {"activity", "activity"}}},
		{Key: "level", Type: KindEnum, Desc: "importance level", Options: []Option{{"important", "important"}, {"normal", "normal"}}},
		{Key: "status", Type: KindEnum, Desc: "publish status", Options: []Option{{"draft", "draft"}, {"published", "published"}, {"revoked", "revoked"}}},
	}},
	"operLog": {Name: "operLog", Desc: "operation audit log list", Fields: []Field{
		kw,
		{Key: "type", Type: KindEnum, Desc: "operation type", Options: []Option{{"create", "create"}, {"update", "update"}, {"delete", "delete"}, {"export", "export"}, {"import", "import"}, {"other", "query / other"}}},
		opStatus,
		{Key: "startDate", Type: KindDate, Desc: "operated on or after this date (YYYY-MM-DD)"},
		{Key: "endDate", Type: KindDate, Desc: "operated on or before this date (YYYY-MM-DD)"},
	}},
}

// ScopeByName 返回页面元数据；未知页面 ok=false。
func ScopeByName(name string) (*Scope, bool) {
	s, ok := scopes[name]
	return s, ok
}

func (s *Scope) field(key string) *Field {
	for i := range s.Fields {
		if s.Fields[i].Key == key {
			return &s.Fields[i]
		}
	}
	return nil
}

// sanitize 按白名单重新校验模型给出的条件：未知字段、非法枚举值、非法日期、超长关键字一律丢弃；
// 日期范围倒置时两端都丢弃。模型输出永远不会被拼成 SQL 或自由文本条件。
func (s *Scope) sanitize(raw map[string]any) map[string]string {
	out := map[string]string{}
	for k, v := range raw {
		f := s.field(k)
		if f == nil {
			continue
		}
		val, ok := scalar(v)
		if !ok {
			continue
		}
		switch f.Type {
		case KindEnum:
			for _, o := range f.Options {
				if o.Value == val {
					out[k] = val
				}
			}
		case KindDate:
			if t, err := time.Parse("2006-01-02", val); err == nil {
				out[k] = t.Format("2006-01-02")
			}
		case KindText:
			if t := cleanText(val, maxKeywordRunes); t != "" {
				out[k] = t
			}
		}
	}
	for _, p := range [][2]string{{"createdFrom", "createdTo"}, {"startDate", "endDate"}} {
		if a, b := out[p[0]], out[p[1]]; a != "" && b != "" && a > b {
			delete(out, p[0])
			delete(out, p[1])
		}
	}
	return out
}

// scalar 把 JSON 标量转成字符串（模型可能把 1 写成数字）；对象、数组一律拒绝。
func scalar(v any) (string, bool) {
	switch x := v.(type) {
	case string:
		return strings.TrimSpace(x), true
	case float64:
		b, _ := json.Marshal(x)
		return string(b), true
	}
	return "", false
}

// cleanText 去掉控制字符与尖括号，按字符数截断后 trim；超过 max 的直接截断。
func cleanText(s string, max int) string {
	var b strings.Builder
	n := 0
	for _, r := range s {
		if r < 0x20 || r == 0x7f || r == '<' || r == '>' {
			continue
		}
		if n >= max {
			break
		}
		b.WriteRune(r)
		n++
	}
	return strings.TrimSpace(b.String())
}
