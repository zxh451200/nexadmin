package crm

// 自定义字段（管理员可配置的客户附加字段）：文本/数字/单选/日期/布尔五种，
// 可设必填、可停用；字段定义是平台级表（全租户共享，与工单字段同口径）。
// 字段定义存 plg_crm_field；值不进独立列，而是整体 JSON 落在 plg_crm_customer.custom_values，
// 所以加字段不需要 DDL，也不需要迁移历史行。CRM 没有队列概念——字段全局挂所有客户，无 GroupID。
// 候选项（options）用逗号分隔串存储——三种引擎的 JSON 类型语义不一致，纯文本最稳；
// 因此候选项本身不许含逗号（normalize 里拒）。

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"gorm.io/gorm"

	"nexadmin/core/database"
	"nexadmin/core/response"
	"nexadmin/pkg/idgen"
)

// 字段类型。
const (
	FieldTypeText   = "text"
	FieldTypeNumber = "number"
	FieldTypeSelect = "select"
	FieldTypeDate   = "date"
	FieldTypeBool   = "bool"
)

// 取值边界：与 DDL 的列宽保持一致，超出即在入口拒绝，不落库再截断。
const (
	maxFieldLabelRunes  = 64
	maxFieldOptionCount = 50
	maxFieldOptionRunes = 64
	maxFieldTextRunes   = 500
	// maxFieldNumberAbs 是数字字段的绝对值上限：再大就逼近 float64 的整数精度极限，
	// 前端 JSON 里会掉精度，存进去就成了「看起来是数但值不对」。
	maxFieldNumberAbs = 1e12
	// fieldDateFormat 是日期字段的唯一合法形状（年-月-日，零填充）。
	fieldDateFormat = "2006-01-02"
	// emptyCustomValuesJSON 是 custom_values 的「没有值」形态，跟 `{}` 而不是空串：
	// 列有 NOT NULL，读回来直接能 unmarshal。
	emptyCustomValuesJSON = "{}"
)

// fieldCodePattern 限定 code 只能是小写字母开头的蛇形串（≤32）。
// code 会同时出现在 SQL 参数、前端表单 name、导出表头里，收紧字符集比在各处分别转义更可靠。
var fieldCodePattern = regexp.MustCompile(`^[a-z][a-z0-9_]{0,31}$`)

// ValidFieldType 报告字段类型是否合法（精确匹配，不做空白折叠）。
func ValidFieldType(t string) bool {
	switch t {
	case FieldTypeText, FieldTypeNumber, FieldTypeSelect, FieldTypeDate, FieldTypeBool:
		return true
	}
	return false
}

// Field 是一条自定义字段定义（平台级表，全租户共享）。
type Field struct {
	database.PlatformModel
	Code     string `gorm:"size:32;not null;uniqueIndex:uk_plg_crm_field_code" json:"code"`
	Label    string `gorm:"size:64;not null" json:"label"`
	Type     string `gorm:"size:16;not null" json:"type"`
	Options  string `gorm:"type:text;not null" json:"-"`
	Required bool   `gorm:"not null" json:"required"`
	Enabled  bool   `gorm:"not null" json:"enabled"`
	Sort     int    `gorm:"not null" json:"sort"`
	// OptionList 是 Options 的对外形态：读时由 AfterFind 拆出，写时由 normalize 编回。
	// 打 gorm:"-" 才不会和 Options 抢列；json key 用 options，前端只认数组。
	OptionList []string `gorm:"-" json:"options"`
}

func (Field) TableName() string { return "plg_crm_field" }

// AfterFind 把逗号分隔的候选项拆回数组：列表/详情/校验读到的都是同一份口径。
func (f *Field) AfterFind(*gorm.DB) error {
	f.OptionList = splitTags(f.Options)
	return nil
}

// normalize 清洗并校验字段定义本身（管理端写入前必过）。
// 注意它同时负责把 OptionList 编进 Options，所以调用方拿到的 f 可直接落库。
func (f *Field) normalize() error {
	f.Code = strings.TrimSpace(f.Code)
	f.Label = strings.TrimSpace(f.Label)
	f.Type = strings.TrimSpace(f.Type)
	if f.Type == "" {
		// 空类型按最宽容的文本处理，与列默认值 'text' 一致；管理端可以少填一个字段。
		f.Type = FieldTypeText
	}
	if !fieldCodePattern.MatchString(f.Code) {
		return response.BadRequest("crm.field_code_invalid")
	}
	n := utf8.RuneCountInString(f.Label)
	if n == 0 || n > maxFieldLabelRunes {
		return response.BadRequest("crm.field_label_required")
	}
	if !ValidFieldType(f.Type) {
		return response.BadRequest("crm.field_type_invalid")
	}
	if f.Sort < 0 {
		f.Sort = 0
	}
	opts := make([]string, 0, len(f.OptionList))
	seen := make(map[string]bool, len(f.OptionList))
	for _, raw := range f.OptionList {
		opt := strings.TrimSpace(raw)
		if opt == "" {
			continue
		}
		// 含逗号会让一项在存储串里裂成两项，读回来时对不上，直接拒而不是替换掉（替换是悄悄改数据）。
		if strings.Contains(opt, ",") || utf8.RuneCountInString(opt) > maxFieldOptionRunes {
			return response.BadRequest("crm.field_option_invalid")
		}
		if seen[opt] {
			continue
		}
		seen[opt] = true
		opts = append(opts, opt)
	}
	if len(opts) > maxFieldOptionCount {
		return response.BadRequest("crm.field_option_invalid")
	}
	if f.Type == FieldTypeSelect && len(opts) == 0 {
		return response.BadRequest("crm.field_option_invalid")
	}
	// 非单选带候选项是配置错误：值校验（text/number/date/bool）根本不看候选项，
	// 留着只会让管理端显示一堆永远不生效的选项。
	if f.Type != FieldTypeSelect && len(opts) > 0 {
		return response.BadRequest("crm.field_option_invalid")
	}
	f.OptionList = opts
	f.Options = strings.Join(opts, ",")
	return nil
}

// ValidateValue 按字段类型校验并归一单个值（纯函数，不碰库）。
// 返回 nil 表示「未填」：nil、空串都算未填，是否放行由 Required 决定。
func (f *Field) ValidateValue(raw any) (any, error) {
	if raw == nil {
		return nil, nil
	}
	switch f.Type {
	case FieldTypeText:
		return validateFieldText(raw)
	case FieldTypeNumber:
		return validateFieldNumber(raw)
	case FieldTypeSelect:
		return f.validateFieldSelect(raw)
	case FieldTypeDate:
		return validateFieldDate(raw)
	case FieldTypeBool:
		return validateFieldBool(raw)
	}
	return nil, response.BadRequest("crm.field_type_invalid")
}

// validateFieldText 文本：只接受字符串，去空白后空串视为未填，长度按 rune 计（中文不按 3 字节算）。
func validateFieldText(raw any) (any, error) {
	s, ok := raw.(string)
	if !ok {
		return nil, response.BadRequest("crm.field_value_invalid")
	}
	s = strings.TrimSpace(s)
	if s == "" {
		return nil, nil
	}
	if utf8.RuneCountInString(s) > maxFieldTextRunes {
		return nil, response.BadRequest("crm.field_value_invalid")
	}
	return s, nil
}

// validateFieldNumber 数字：JSON 反序列化后是 float64，也要兼容前端传来的数字字符串与整数，
// 统一归一成 float64（写回 custom_values 也是这个形状）。
func validateFieldNumber(raw any) (any, error) {
	var n float64
	switch v := raw.(type) {
	case float64:
		n = v
	case float32:
		n = float64(v)
	case int:
		n = float64(v)
	case int32:
		n = float64(v)
	case int64:
		n = float64(v)
	case json.Number:
		parsed, err := v.Float64()
		if err != nil {
			return nil, response.BadRequest("crm.field_value_invalid")
		}
		n = parsed
	case string:
		s := strings.TrimSpace(v)
		if s == "" {
			return nil, nil
		}
		// ParseFloat 认 "NaN"/"Inf"/"0x1p3" 这类形状，归一后一律走下面的有限性检查挡掉；
		// 这里额外挡住空格与多值（"1 2"），避免把明显不是数字的串存进去。
		parsed, err := strconv.ParseFloat(s, 64)
		if err != nil {
			return nil, response.BadRequest("crm.field_value_invalid")
		}
		n = parsed
	default:
		return nil, response.BadRequest("crm.field_value_invalid")
	}
	if math.IsNaN(n) || math.IsInf(n, 0) || math.Abs(n) > maxFieldNumberAbs {
		return nil, response.BadRequest("crm.field_value_invalid")
	}
	return n, nil
}

// validateFieldSelect 单选：值必须精确命中某个候选项。
// 不做大小写折叠也不做前缀匹配——「近似就放行」会让按候选项聚合时冒出没人认领的值。
func (f *Field) validateFieldSelect(raw any) (any, error) {
	s, ok := raw.(string)
	if !ok {
		return nil, response.BadRequest("crm.field_value_invalid")
	}
	s = strings.TrimSpace(s)
	if s == "" {
		return nil, nil
	}
	opts := f.OptionList
	if len(opts) == 0 {
		opts = splitTags(f.Options)
	}
	for _, opt := range opts {
		if opt == s {
			return s, nil
		}
	}
	return nil, response.BadRequest("crm.field_value_invalid")
}

// validateFieldDate 日期：严格 YYYY-MM-DD，归一成补零后的同形字符串（"2026-1-2" → "2026-01-02"）。
func validateFieldDate(raw any) (any, error) {
	s, ok := raw.(string)
	if !ok {
		return nil, response.BadRequest("crm.field_value_invalid")
	}
	s = strings.TrimSpace(s)
	if s == "" {
		return nil, nil
	}
	// 不套时区：日期字段没有时刻语义，带 loc 解析会让 "2026-01-01" 变成上一个/下一天。
	day, err := time.Parse(fieldDateFormat, s)
	if err != nil {
		return nil, response.BadRequest("crm.field_value_invalid")
	}
	return day.Format(fieldDateFormat), nil
}

// validateFieldBool 布尔：只认真/假两种形状（JSON 布尔或 "true"/"false"）。
// 不接受 "1"/"yes"/"on"：这些在跨系统导入里含义随时变，宁拒不含糊。
func validateFieldBool(raw any) (any, error) {
	switch v := raw.(type) {
	case bool:
		return v, nil
	case string:
		switch strings.ToLower(strings.TrimSpace(v)) {
		case "":
			return nil, nil
		case "true":
			return true, nil
		case "false":
			return false, nil
		}
	}
	return nil, response.BadRequest("crm.field_value_invalid")
}

// ValidateCustomValues 按启用中的字段定义校验并归一整份自定义值（建客户/改客户入口用）。
// 处置口径：
//   - in 里出现「未定义或已停用」的 code → 拒绝（crm.field_code_invalid），不静默丢弃：
//     静默丢弃会让前端塞错 key 变成「提交成功但值没了」，排查成本极高；
//   - Required 且未填 → crm.field_required；
//   - 单个值形状不对 → crm.field_value_invalid（由 ValidateValue 决定）。
//
// 返回的 map 只含非空值，且永远非 nil（nil 会被序列化成 JSON null，前端读 undefined.length 直接崩页）。
func ValidateCustomValues(defs []Field, in map[string]any) (map[string]any, error) {
	enabled := make(map[string]*Field, len(defs))
	for i := range defs {
		if !defs[i].Enabled {
			continue
		}
		code := strings.TrimSpace(defs[i].Code)
		if code == "" {
			continue
		}
		enabled[code] = &defs[i]
	}
	for code := range in {
		if _, ok := enabled[code]; !ok {
			return nil, response.BadRequest("crm.field_code_invalid")
		}
	}
	out := make(map[string]any, len(enabled))
	for code, def := range enabled {
		value, err := def.ValidateValue(in[code])
		if err != nil {
			return nil, err
		}
		if value == nil {
			if def.Required {
				return nil, response.BadRequest("crm.field_required")
			}
			continue
		}
		out[code] = value
	}
	return out, nil
}

// EncodeCustomValues 把自定义值序列化成 custom_values 列的文本；空 map 落 '{}'。
func EncodeCustomValues(in map[string]any) (string, error) {
	if len(in) == 0 {
		return emptyCustomValuesJSON, nil
	}
	raw, err := json.Marshal(in)
	if err != nil {
		// 只有 NaN/Inf/channel 之类 JSON 写不出的值会到这里：值来自 ValidateCustomValues 时不会发生。
		return emptyCustomValuesJSON, response.BadRequest("crm.field_value_invalid").Wrap(err)
	}
	return string(raw), nil
}

// DecodeCustomValues 读 custom_values 列。脏数据（空串、"null"、截断的 JSON、数组）一律回落空 map：
// 附加字段读不出来不该让客户详情打不开，更不该 panic。永不返回 nil。
func DecodeCustomValues(raw string) map[string]any {
	out := make(map[string]any, 4)
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return out
	}
	if err := json.Unmarshal([]byte(raw), &out); err != nil || out == nil {
		return make(map[string]any, 4)
	}
	return out
}

// ListFields 按 sort、id 升序返回全部字段定义（含停用的，管理页要能重新启用）。
// OptionList 由 GORM 在每行 Find 后自动调 AfterFind 装载，不要再手搓一遍拆分。
func (s *Service) ListFields(ctx context.Context) ([]Field, error) {
	rows := make([]Field, 0, 8)
	err := s.db.WithContext(ctx).Order("sort ASC, id ASC").Find(&rows).Error
	return rows, err
}

// EnabledFields 返回启用中的字段定义：建客户/改客户校验与表单渲染都用这份，口径必须一致。
func (s *Service) EnabledFields(ctx context.Context) ([]Field, error) {
	rows := make([]Field, 0, 8)
	err := s.db.WithContext(ctx).Where("enabled = ?", true).Order("sort ASC, id ASC").Find(&rows).Error
	return rows, err
}

// SaveField 新增或修改字段定义：有 id 走更新，无 id 生成雪花 id 建行。
// code 全局唯一（表上有唯一键），这里先查一次「同 code 非自身」，
// 让重复 code 报可读的 409，而不是把驱动的唯一键冲突原样抛成 500。
func (s *Service) SaveField(ctx context.Context, in Field) (Field, error) {
	f := in
	f.OptionList = append([]string(nil), in.OptionList...)
	if err := f.normalize(); err != nil {
		return Field{}, err
	}
	var dup int64
	if err := s.db.WithContext(ctx).Model(&Field{}).
		Where("code = ? AND id <> ?", f.Code, int64(f.ID)).Count(&dup).Error; err != nil {
		return Field{}, err
	}
	if dup > 0 {
		return Field{}, response.Conflict("crm.field_code_conflict")
	}
	if f.ID == 0 {
		id, err := idgen.Next()
		if err != nil {
			return Field{}, err
		}
		f.ID = id
		if err := s.db.WithContext(ctx).Create(&f).Error; err != nil {
			return Field{}, err
		}
		return f, nil
	}
	// 先确认存在：MySQL 下值没变时 RowsAffected 为 0，只按它判「不存在」会误报。
	var existing Field
	if err := s.db.WithContext(ctx).Where("id = ?", int64(f.ID)).Take(&existing).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return Field{}, response.NotFound("error.not_found")
		}
		return Field{}, err
	}
	updates := map[string]any{
		"code": f.Code, "label": f.Label, "type": f.Type, "options": f.Options,
		"required": f.Required, "enabled": f.Enabled, "sort": f.Sort,
	}
	if err := s.db.WithContext(ctx).Model(&Field{}).Where("id = ?", int64(f.ID)).Updates(updates).Error; err != nil {
		return Field{}, err
	}
	// created_at 属于建行那一刻，更新不许改写；updated_at 由 GORM 填，返回值取同一个时钟，
	// 免得前端拿到的行比库里旧一拍。
	f.CreatedAt = existing.CreatedAt
	f.UpdatedAt = s.db.Config.NowFunc()
	return f, nil
}

// DeleteField 删除字段定义。
// 存量客户里的该 code 不清洗：批量 UPDATE 全表代价大、不可回滚，历史值还可能是审计材料；
// 读路径（ValidateCustomValues/前端渲染）只认启用中的定义，所以旧值留在 custom_values 里只是不再显示。
// 之后若重建同 code 的定义，旧值会重新被认出来——这是有意保留，不是遗漏。
func (s *Service) DeleteField(ctx context.Context, id int64) error {
	res := s.db.WithContext(ctx).Where("id = ?", id).Delete(&Field{})
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return response.NotFound("error.not_found")
	}
	return nil
}

// customValuesFor 校验并编码一份自定义字段值，返回可直接落库的 JSON 文本（建/改客户共用）。
func (s *Service) customValuesFor(ctx context.Context, in map[string]any) (string, error) {
	if len(in) == 0 {
		return emptyCustomValuesJSON, nil
	}
	defs, err := s.EnabledFields(ctx)
	if err != nil {
		return "", err
	}
	values, err := ValidateCustomValues(defs, in)
	if err != nil {
		return "", err
	}
	return EncodeCustomValues(values)
}
