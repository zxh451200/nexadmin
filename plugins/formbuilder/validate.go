package formbuilder

import (
	"encoding/json"
	"fmt"
	"math"
	"regexp"
	"regexp/syntax"
	"strings"
	"time"
	"unicode/utf8"
)

var keyRe = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_]{0,31}$`)
var dictRe = regexp.MustCompile(`^[A-Za-z0-9_.-]{1,64}$`)

// compilePattern 编译用户给的正则，防 ReDoS：
//  1. Go 的 regexp 是 RE2，匹配时间与输入长度成线性，没有回溯爆炸；
//  2. 仍然限制源码长度、语法树节点数、{n,m} 的 m，以及嵌套重复展开后的粗略规模，防止编译期/内存放大；
//  3. 被匹配的输入由字段的 maxLen（<= MaxTextRunes）限制。
//
// Go 标准库没有匹配超时机制，线性复杂度加上述长度限制就是本插件的防护边界。
func compilePattern(p string) (*regexp.Regexp, error) {
	if len(p) > MaxPatternLen {
		return nil, fmt.Errorf("正则过长（上限 %d 字节）", MaxPatternLen)
	}
	tree, err := syntax.Parse(p, syntax.Perl)
	if err != nil {
		return nil, fmt.Errorf("正则语法错误")
	}
	nodes := 0
	cost, err := patternCost(tree, &nodes)
	if err != nil {
		return nil, err
	}
	if cost > MaxPatternCost {
		return nil, fmt.Errorf("正则过于复杂")
	}
	re, err := regexp.Compile(p)
	if err != nil {
		return nil, fmt.Errorf("正则语法错误")
	}
	return re, nil
}

func patternCost(re *syntax.Regexp, nodes *int) (int, error) {
	*nodes++
	if *nodes > MaxPatternNodes {
		return 0, fmt.Errorf("正则过于复杂")
	}
	sum := 0
	for _, s := range re.Sub {
		c, err := patternCost(s, nodes)
		if err != nil {
			return 0, err
		}
		sum += c
	}
	switch re.Op {
	case syntax.OpRepeat:
		n := re.Max
		if n < 0 {
			n = re.Min
		}
		if re.Max > MaxRepeat || re.Min > MaxRepeat {
			return 0, fmt.Errorf("正则重复次数过大（上限 %d）", MaxRepeat)
		}
		if n < 1 {
			n = 1
		}
		return sum * n, nil
	case syntax.OpLiteral:
		return len(re.Rune), nil
	case syntax.OpConcat, syntax.OpAlternate, syntax.OpCapture, syntax.OpStar, syntax.OpPlus, syntax.OpQuest:
		return sum + 1, nil
	}
	return 1, nil
}

// ValidateDefinition 校验设计器提交的定义（草稿保存与发布前都会调用）。
func ValidateDefinition(d *Definition) error {
	name := strings.TrimSpace(d.Name)
	if name == "" || utf8.RuneCountInString(name) > 100 {
		return bad("表单名称必填，且不超过 100 个字符")
	}
	if utf8.RuneCountInString(d.Description) > 500 {
		return bad("表单说明不超过 500 个字符")
	}
	if len(d.Fields) > MaxFields {
		return bad(fmt.Sprintf("字段数量不能超过 %d", MaxFields))
	}
	if err := validateSettings(&d.Settings); err != nil {
		return err
	}
	earlier := map[string]struct{}{}
	seen := map[string]struct{}{}
	for i := range d.Fields {
		f := &d.Fields[i]
		if !keyRe.MatchString(f.Key) {
			return bad(fmt.Sprintf("字段标识 %q 不合法（字母开头，字母数字下划线，最长 %d）", f.Key, MaxKeyLen))
		}
		if _, dup := seen[f.Key]; dup {
			return bad(fmt.Sprintf("字段标识 %q 重复", f.Key))
		}
		seen[f.Key] = struct{}{}
		if err := validateFieldDef(f); err != nil {
			return bad(fmt.Sprintf("字段 %s：%s", f.Key, err.Error()))
		}
		if f.ShowIf != nil {
			if err := validateCond(f.ShowIf, earlier); err != nil {
				return bad(fmt.Sprintf("字段 %s 的显隐条件：%s", f.Key, err.Error()))
			}
		}
		if f.hasValue() {
			earlier[f.Key] = struct{}{}
		}
	}
	raw, err := json.Marshal(d)
	if err != nil || len(raw) > MaxDefBytes {
		return errDefTooLarge
	}
	return nil
}

func validateSettings(s *Settings) error {
	if s.MaxSubmissions < 0 || s.MaxSubmissions > MaxSubmissionCap {
		return bad("提交次数上限不合法")
	}
	if utf8.RuneCountInString(s.SuccessMessage) > 500 {
		return bad("提交后提示不超过 500 个字符")
	}
	return nil
}

func validateFieldDef(f *Field) error {
	switch f.Type {
	case TypeText, TypeTextarea, TypeNumber, TypeRadio, TypeCheckbox, TypeSelect, TypeDate, TypeSwitch, TypeRating, TypeFile, TypeSection:
	default:
		return fmt.Errorf("未知字段类型 %q", f.Type)
	}
	if strings.TrimSpace(f.Title) == "" || utf8.RuneCountInString(f.Title) > MaxTitleRunes {
		return fmt.Errorf("标题必填，且不超过 %d 个字符", MaxTitleRunes)
	}
	if utf8.RuneCountInString(f.Description) > 1000 || utf8.RuneCountInString(f.Placeholder) > 200 {
		return fmt.Errorf("说明或占位文字过长")
	}
	if !f.hasValue() {
		return nil
	}
	if f.usesOptions() {
		if err := validateOptions(f); err != nil {
			return err
		}
	}
	if err := validateRules(f); err != nil {
		return err
	}
	if f.Default != nil && (!f.usesOptions() || len(f.Options) > 0) {
		if _, err := checkValue(f, f.Default); err != nil {
			return fmt.Errorf("默认值不合法：%s", err.Error())
		}
	}
	return nil
}

func validateOptions(f *Field) error {
	if f.DictType != "" && !dictRe.MatchString(f.DictType) {
		return fmt.Errorf("字典类型不合法")
	}
	if f.DictType == "" && len(f.Options) == 0 {
		return fmt.Errorf("需要至少一个选项，或绑定字典类型")
	}
	if len(f.Options) > MaxOptions {
		return fmt.Errorf("选项不能超过 %d 个", MaxOptions)
	}
	seen := map[string]struct{}{}
	for _, o := range f.Options {
		if o.Value == "" || utf8.RuneCountInString(o.Value) > 100 || strings.TrimSpace(o.Label) == "" || utf8.RuneCountInString(o.Label) > 100 {
			return fmt.Errorf("选项的值与名称必填，且不超过 100 个字符")
		}
		if _, dup := seen[o.Value]; dup {
			return fmt.Errorf("选项值 %q 重复", o.Value)
		}
		seen[o.Value] = struct{}{}
	}
	return nil
}

func validateRules(f *Field) error {
	if f.MinLen != nil || f.MaxLen != nil || f.Pattern != "" {
		if f.Type != TypeText && f.Type != TypeTextarea {
			return fmt.Errorf("长度与正则规则只适用于文本字段")
		}
	}
	if f.MinLen != nil && *f.MinLen < 0 || f.MaxLen != nil && (*f.MaxLen < 1 || *f.MaxLen > MaxTextRunes) {
		return fmt.Errorf("长度范围不合法（最大 %d）", MaxTextRunes)
	}
	if f.MinLen != nil && f.MaxLen != nil && *f.MinLen > *f.MaxLen {
		return fmt.Errorf("最小长度不能大于最大长度")
	}
	if f.Pattern != "" {
		if _, err := compilePattern(f.Pattern); err != nil {
			return err
		}
	}
	if f.Min != nil && f.Max != nil && *f.Min > *f.Max {
		return fmt.Errorf("最小值不能大于最大值")
	}
	for _, v := range []*float64{f.Min, f.Max} {
		if v != nil && (math.IsNaN(*v) || math.IsInf(*v, 0) || math.Abs(*v) > maxNumber) {
			return fmt.Errorf("范围值不合法")
		}
	}
	if f.Type == TypeRating && f.Max != nil && (*f.Max < 1 || *f.Max > MaxRatingStars || *f.Max != math.Trunc(*f.Max)) {
		return fmt.Errorf("评分最大值需为 1-%d 的整数", MaxRatingStars)
	}
	if f.Type == TypeFile && f.Max != nil && (*f.Max < 1 || *f.Max > MaxFiles) {
		return fmt.Errorf("文件个数上限需为 1-%d", MaxFiles)
	}
	for _, s := range []string{f.MinDate, f.MaxDate} {
		if s != "" {
			if f.Type != TypeDate {
				return fmt.Errorf("日期范围只适用于日期字段")
			}
			if _, err := time.Parse("2006-01-02", s); err != nil {
				return fmt.Errorf("日期范围格式应为 yyyy-MM-dd")
			}
		}
	}
	if f.MinDate != "" && f.MaxDate != "" && f.MinDate > f.MaxDate {
		return fmt.Errorf("最早日期不能晚于最晚日期")
	}
	if f.Multiple && f.Type != TypeSelect {
		return fmt.Errorf("multiple 只适用于下拉")
	}
	return nil
}
