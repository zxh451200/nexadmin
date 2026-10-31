package ticket

// 工单标签：规范化存储为「,a,b,」，列表/详情通过 AfterFind 拆成数组；
// 筛选按逗号边界 LIKE，避免 vip 命中 viptag。

import (
	"context"
	"errors"
	"sort"
	"strings"
	"unicode/utf8"

	"gorm.io/gorm"

	"nexadmin/core/response"
)

const (
	maxTags     = 10
	maxTagRunes = 20
)

// TagsInput 是整份替换标签的入参。
type TagsInput struct {
	Tags []string `json:"tags"`
}

func normalizeTags(in []string) ([]string, error) {
	seen := make(map[string]bool, len(in))
	out := make([]string, 0, len(in))
	for _, raw := range in {
		tag := strings.TrimSpace(raw)
		if tag == "" {
			continue
		}
		if strings.Contains(tag, ",") || utf8.RuneCountInString(tag) > maxTagRunes {
			return nil, response.BadRequest("error.bad_request")
		}
		if seen[tag] {
			continue
		}
		seen[tag] = true
		out = append(out, tag)
	}
	if len(out) > maxTags {
		return nil, response.BadRequest("error.bad_request")
	}
	return out, nil
}

func canonicalTags(tags []string) string {
	if len(tags) == 0 {
		return ""
	}
	return "," + strings.Join(tags, ",") + ","
}

// SetTags 整份替换工单标签（最多 10 个，每个不超过 20 字）。
func (s *Service) SetTags(ctx context.Context, ticketNo string, tags []string) (*Ticket, error) {
	normalized, err := normalizeTags(tags)
	if err != nil {
		return nil, err
	}
	var t Ticket
	if err := s.db.WithContext(ctx).Where("ticket_no = ?", strings.TrimSpace(ticketNo)).Take(&t).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, response.NotFound("ticket.not_found")
		}
		return nil, err
	}
	oldTags := strings.Join(splitTags(t.Tags), ",")
	if err := s.db.WithContext(ctx).Model(&Ticket{}).Where("id = ?", int64(t.ID)).
		Update("tags", canonicalTags(normalized)).Error; err != nil {
		return nil, err
	}
	newTags := strings.Join(normalized, ",")
	if oldTags != newTags {
		if err := s.recordActivity(ctx, nil, &t, actorFrom(ctx), ActTagsChanged, oldTags, newTags, ""); err != nil {
			return nil, err
		}
	}
	t.Tags, t.TagList = canonicalTags(normalized), normalized
	return &t, nil
}

// ListTags 返回使用中的标签，按使用次数倒序（最多 50 个），供筛选与建议。
func (s *Service) ListTags(ctx context.Context) ([]string, error) {
	raws := make([]string, 0, 64)
	if err := s.db.WithContext(ctx).Model(&Ticket{}).Where("tags <> ''").Pluck("tags", &raws).Error; err != nil {
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
