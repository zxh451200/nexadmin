package crm

// 客户标签：规范化存储为「,a,b,」，读路径经 Customer.AfterFind 拆成数组；
// 筛选按逗号边界 LIKE，避免 vip 命中 viptag。与工单标签（plugins/ticket/tag.go）同一套约定。

import (
	"context"
	"errors"
	"sort"
	"strings"
	"unicode/utf8"

	"gorm.io/gorm"

	"nexadmin/core/datascope"
	"nexadmin/core/response"
	"nexadmin/pkg/idgen"
)

const (
	maxTags     = 10
	maxTagRunes = 20
)

func normalizeTags(in []string) ([]string, error) {
	seen := make(map[string]bool, len(in))
	out := make([]string, 0, len(in))
	for _, raw := range in {
		tag := strings.TrimSpace(raw)
		if tag == "" {
			continue
		}
		if strings.Contains(tag, ",") || utf8.RuneCountInString(tag) > maxTagRunes {
			return nil, response.BadRequest("crm.tag_invalid")
		}
		if seen[tag] {
			continue
		}
		seen[tag] = true
		out = append(out, tag)
	}
	if len(out) > maxTags {
		return nil, response.BadRequest("crm.tag_invalid")
	}
	return out, nil
}

func canonicalTags(tags []string) string {
	if len(tags) == 0 {
		return ""
	}
	return "," + strings.Join(tags, ",") + ","
}

func splitTags(raw string) []string {
	out := make([]string, 0, 4)
	for _, part := range strings.Split(raw, ",") {
		if part = strings.TrimSpace(part); part != "" {
			out = append(out, part)
		}
	}
	return out
}

// SetCustomerTags 整份替换客户标签（最多 10 个，每个不超过 20 字）。
// 可见性按数据范围校验；变更留一条 system 跟进。
func (s *Service) SetCustomerTags(ctx context.Context, sc *datascope.Scope, id idgen.ID, tags []string) (*Customer, error) {
	normalized, err := normalizeTags(tags)
	if err != nil {
		return nil, err
	}
	c, err := s.visibleCustomer(ctx, sc, id)
	if err != nil {
		return nil, err
	}
	oldTags := strings.Join(splitTags(c.Tags), ",")
	newTags := strings.Join(normalized, ",")
	if oldTags == newTags {
		return c, nil
	}
	if err := s.db.WithContext(ctx).Model(&Customer{}).Where("id = ?", int64(id)).
		Update("tags", canonicalTags(normalized)).Error; err != nil {
		return nil, err
	}
	c.Tags, c.TagList = canonicalTags(normalized), normalized
	s.noteSystem(ctx, id, "更新了客户标签："+newTags)
	s.publishCustomer(ctx, EventCustomerUpdated, c)
	return c, nil
}

// AddCustomerTags 批量给一组客户追加标签（已有则跳过该词，上限按单客户 10 个算）。
// 返回实际写了几条。可见性外的 id 忽略（与批量删除的 blocked 同口径，这里静默跳过）。
func (s *Service) AddCustomerTags(ctx context.Context, sc *datascope.Scope, ids []idgen.ID, tags []string) (int64, error) {
	add, err := normalizeTags(tags)
	if err != nil || len(add) == 0 {
		return 0, err
	}
	ownable, err := s.scopedIDs(ctx, sc, &Customer{}, ids)
	if err != nil {
		return 0, err
	}
	var updated int64
	for _, id := range ownable {
		var c Customer
		if err := s.db.WithContext(ctx).Take(&c, int64(id)).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				continue
			}
			return updated, err
		}
		merged := make([]string, 0, len(splitTags(c.Tags))+len(add))
		merged = append(merged, splitTags(c.Tags)...)
		merged = append(merged, add...)
		next, err := normalizeTags(merged)
		if err != nil {
			// 单个客户标签超上限：跳过它，不拦整批
			continue
		}
		if strings.Join(next, ",") == strings.Join(splitTags(c.Tags), ",") {
			continue
		}
		if err := s.db.WithContext(ctx).Model(&Customer{}).Where("id = ?", int64(id)).
			Update("tags", canonicalTags(next)).Error; err != nil {
			return updated, err
		}
		updated++
		s.noteSystem(ctx, id, "批量追加标签："+strings.Join(add, ","))
	}
	return updated, nil
}

// ListCustomerTags 返回使用中的标签，按使用次数倒序（最多 50 个），供筛选与建议。
// 数据范围同样生效：受限账号看到的标签来自自己范围内的客户。
func (s *Service) ListCustomerTags(ctx context.Context, sc *datascope.Scope) ([]string, error) {
	db := s.db.WithContext(ctx).Model(&Customer{}).Where("tags <> ''")
	if cond, args, limited := sc.UsernameCond("owner"); limited {
		db = db.Where(cond, args...)
	}
	var raws []string
	if err := db.Pluck("tags", &raws).Error; err != nil {
		return nil, err
	}
	counts := make(map[string]int, len(raws))
	for _, raw := range raws {
		for _, tag := range splitTags(raw) {
			counts[tag]++
		}
	}
	names := make([]string, 0, len(counts))
	for name := range counts {
		names = append(names, name)
	}
	sort.Slice(names, func(i, j int) bool {
		if counts[names[i]] != counts[names[j]] {
			return counts[names[i]] > counts[names[j]]
		}
		return names[i] < names[j]
	})
	if len(names) > 50 {
		names = names[:50]
	}
	return names, nil
}
