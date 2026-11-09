package crm

// 合并预览：确认之前先看到「会带走什么、会补上什么」。规则与 MergeCustomers 完全同源
//（mergeFills 一份实现两处调用），预览不是另一个真相，是同一份规则的只读投影。

import (
	"context"
	"fmt"

	"nexadmin/core/datascope"
	"nexadmin/core/response"
	"nexadmin/pkg/idgen"
)

// MergeFieldPreview 是一个会被从客户「补空」进主客户的字段。
type MergeFieldPreview struct {
	Column string `json:"column"` // 列名（前端映射成展示名）
	Value  string `json:"value"`  // 将补入的值（来自从客户）
}

// MergeCustomPreview 是一个会被从客户补进主客户的自定义字段值。
// 单列一组而不是塞进 FillFields：列名是管理员起的 label（前端没有映射表），
// 值也可能是数字/布尔，与主数据列的展示口径不同。
type MergeCustomPreview struct {
	Code  string `json:"code"`
	Label string `json:"label"`
	Value string `json:"value"`
}

// MergePreview 是合并确认页的全部信息：两边的主数据、会被补空的字段、关联记录的迁移量。
type MergePreview struct {
	Target          Customer             `json:"target"`
	Source          Customer             `json:"source"`
	Contacts        int64                `json:"contacts"`
	Opportunities   int64                `json:"opportunities"`
	Activities      int64                `json:"activities"`
	FillFields      []MergeFieldPreview  `json:"fillFields"`
	NewTags         []string             `json:"newTags"`         // 从客户带来的、主客户还没有的标签
	NewCustomFields []MergeCustomPreview `json:"newCustomFields"` // 从客户带来的、主客户没有的自定义字段值
}

// PreviewMerge 组装合并预览。可见性与 MergeCustomers 同口径：两边都要看得见。
func (s *Service) PreviewMerge(ctx context.Context, sc *datascope.Scope, targetID, sourceID idgen.ID) (*MergePreview, error) {
	if targetID == sourceID {
		return nil, response.BadRequest("crm.merge_self")
	}
	target, err := s.visibleCustomer(ctx, sc, targetID)
	if err != nil {
		return nil, err
	}
	source, err := s.visibleCustomer(ctx, sc, sourceID)
	if err != nil {
		return nil, err
	}
	out := &MergePreview{
		Target: *target, Source: *source,
		// 空切片而不是 nil：前端直接读 .length，nil 会序列化成 null 把确认页弄崩。
		FillFields: []MergeFieldPreview{}, NewTags: []string{}, NewCustomFields: []MergeCustomPreview{},
	}
	if err := s.db.WithContext(ctx).Model(&Contact{}).Where("customer_id = ?", int64(sourceID)).Count(&out.Contacts).Error; err != nil {
		return nil, err
	}
	if err := s.db.WithContext(ctx).Model(&Opportunity{}).Where("customer_id = ?", int64(sourceID)).Count(&out.Opportunities).Error; err != nil {
		return nil, err
	}
	if err := s.db.WithContext(ctx).Model(&Activity{}).Where("customer_id = ?", int64(sourceID)).Count(&out.Activities).Error; err != nil {
		return nil, err
	}
	// 补空字段与合并走的同一份 mergeFills：预览说会补的，合并就一定会补
	fills := mergeFills(target, source)
	for col, v := range fills {
		if sv, ok := v.(string); ok {
			out.FillFields = append(out.FillFields, MergeFieldPreview{Column: col, Value: sv})
		}
	}
	// 标签并集预览：与合并写入同一套 normalize
	union, err := normalizeTags(append(splitTags(target.Tags), splitTags(source.Tags)...))
	if err != nil {
		return nil, err
	}
	have := map[string]bool{}
	for _, tag := range splitTags(target.Tags) {
		have[tag] = true
	}
	for _, tag := range union {
		if !have[tag] {
			out.NewTags = append(out.NewTags, tag)
		}
	}
	// 自定义字段值与合并同一份规则（mergeCustomValues）：预览说会补的，合并就一定会补。
	mergedValues, added := mergeCustomValues(target, source)
	if len(added) > 0 {
		defs, err := s.ListFields(ctx)
		if err != nil {
			return nil, err
		}
		labels := make(map[string]string, len(defs))
		for _, d := range defs {
			labels[d.Code] = d.Label
		}
		for _, code := range added {
			label := labels[code]
			if label == "" {
				label = code // 定义已被删除：旧值仍会合并，按 code 显示，不假装没有
			}
			out.NewCustomFields = append(out.NewCustomFields, MergeCustomPreview{
				Code: code, Label: label, Value: formatCustomValue(mergedValues[code]),
			})
		}
	}
	return out, nil
}

// formatCustomValue 把自定义字段值转成预览里的一行文本：字符串原样，数字/布尔按字面量。
func formatCustomValue(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	return fmt.Sprint(v)
}
