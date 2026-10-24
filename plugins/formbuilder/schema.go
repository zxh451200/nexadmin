package formbuilder

import (
	"encoding/json"
	"time"
)

// 字段类型。section 只是分组标题/说明文本，不产生数据。
const (
	TypeText     = "text"
	TypeTextarea = "textarea"
	TypeNumber   = "number"
	TypeRadio    = "radio"
	TypeCheckbox = "checkbox"
	TypeSelect   = "select"
	TypeDate     = "date"
	TypeSwitch   = "switch"
	TypeRating   = "rating"
	TypeFile     = "file"
	TypeSection  = "section"
)

// 资源上限。TEXT 在 MySQL 里最大 65535 字节，所以 JSON 一律控制在 60000 字节以内。
const (
	MaxDefBytes      = 60000 // 单个定义（草稿或版本）JSON 的字节上限
	MaxDataBytes     = 60000 // 单条提交 JSON 的字节上限
	MaxRequestBytes  = 256 << 10
	MaxFields        = 100
	MaxOptions       = 200
	MaxKeyLen        = 32
	MaxTitleRunes    = 200
	MaxTextRunes     = 10000 // 文本类字段 maxLen 的硬上限
	DefaultTextRunes = 500
	DefaultAreaRunes = 5000
	MaxPatternLen    = 200  // 正则源码长度上限
	MaxPatternNodes  = 200  // 正则语法树节点数上限
	MaxRepeat        = 100  // 正则 {n,m} 的上限
	MaxPatternCost   = 2000 // 嵌套重复展开后的粗略规模上限
	MaxRatingStars   = 10
	MaxFiles         = 20
	MaxCondDepth     = 4
	MaxCondNodes     = 20
	MaxCondList      = 50
	MaxAudience      = 200
	MaxSubmissionCap = 1000000 // 提交次数上限的合法最大值
)

// Option 是选项：value 存库，label 展示。
type Option struct {
	Label string `json:"label"`
	Value string `json:"value"`
}

// Cond 是声明式显隐条件，只做数据比较，不执行任何代码。
// 叶子：{"field":"a","op":"eq","value":"x"}；组合：{"all":[...]} 或 {"any":[...]}，三种形态三选一。
type Cond struct {
	All   []Cond `json:"all,omitempty"`
	Any   []Cond `json:"any,omitempty"`
	Field string `json:"field,omitempty"`
	Op    string `json:"op,omitempty"`
	Value any    `json:"value,omitempty"`
}

// Field 是表单里的一个字段。
type Field struct {
	Key         string   `json:"key"`
	Type        string   `json:"type"`
	Title       string   `json:"title"`
	Description string   `json:"description,omitempty"`
	Placeholder string   `json:"placeholder,omitempty"`
	Required    bool     `json:"required,omitempty"`
	Default     any      `json:"default,omitempty"`
	Min         *float64 `json:"min,omitempty"`    // number：最小值；checkbox/select 多选/file：最少个数
	Max         *float64 `json:"max,omitempty"`    // number：最大值；rating：星数；checkbox/多选/file：最多个数
	MinLen      *int     `json:"minLen,omitempty"` // text/textarea
	MaxLen      *int     `json:"maxLen,omitempty"`
	Integer     bool     `json:"integer,omitempty"` // number：仅整数
	Pattern     string   `json:"pattern,omitempty"` // text/textarea：RE2 正则
	MinDate     string   `json:"minDate,omitempty"` // date：yyyy-MM-dd
	MaxDate     string   `json:"maxDate,omitempty"`
	Multiple    bool     `json:"multiple,omitempty"` // select：多选
	Options     []Option `json:"options,omitempty"`
	DictType    string   `json:"dictType,omitempty"` // 绑定字典类型；发布时把字典项快照进 options
	ShowIf      *Cond    `json:"showIf,omitempty"`
}

// Settings 是表单设置。
type Settings struct {
	AllowRepeat    bool       `json:"allowRepeat"`
	Deadline       *time.Time `json:"deadline,omitempty"`
	MaxSubmissions int        `json:"maxSubmissions"` // 0 = 不限
	SuccessMessage string     `json:"successMessage,omitempty"`
}

// Definition 是一份完整的表单定义；每个发布版本冻结一份。
type Definition struct {
	Name        string   `json:"name"`
	Description string   `json:"description,omitempty"`
	Fields      []Field  `json:"fields"`
	Settings    Settings `json:"settings"`
}

// Audience 是可填写范围：角色与用户的并集；两者都空 = 所有拥有 formbuilder:fill 的登录用户。
type Audience struct {
	Roles []string `json:"roles"`
	Users []string `json:"users"`
}

// Empty 报告是否未限制范围。
func (a Audience) Empty() bool { return len(a.Roles) == 0 && len(a.Users) == 0 }

// hasValue 报告字段类型是否产生数据。
func (f Field) hasValue() bool { return f.Type != TypeSection }

// isMulti 报告字段值是否为数组。
func (f Field) isMulti() bool {
	return f.Type == TypeCheckbox || f.Type == TypeFile || (f.Type == TypeSelect && f.Multiple)
}

func (f Field) usesOptions() bool {
	return f.Type == TypeRadio || f.Type == TypeCheckbox || f.Type == TypeSelect
}

// optionSet 返回合法取值集合。
func (f Field) optionSet() map[string]struct{} {
	m := make(map[string]struct{}, len(f.Options))
	for _, o := range f.Options {
		m[o.Value] = struct{}{}
	}
	return m
}

// decodeDefinition 解析并限制大小；未知字段被忽略。
func decodeDefinition(raw []byte) (*Definition, error) {
	if len(raw) > MaxDefBytes {
		return nil, errDefTooLarge
	}
	var d Definition
	if err := json.Unmarshal(raw, &d); err != nil {
		return nil, errDefInvalid
	}
	if d.Fields == nil {
		d.Fields = []Field{}
	}
	return &d, nil
}
