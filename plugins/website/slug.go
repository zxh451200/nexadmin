package website

import (
	"context"
	"fmt"
	"strings"

	"nexadmin/pkg/idgen"
	"nexadmin/pkg/slug"
)

// resolveSlug 是全站 slug 的统一入口（2026-11-12 对齐行业标准：NFKD 去变音符、小写、
// 非字母数字压单连字符、限长 64、空结果回退随机串），三种输入形态：
//
//   显式 slug   → ValidateSlug 校验，更新时排除自身行让号；
//   留空 + 新建 → 按标题走 pkg/slug 行业标准生成（生成结果撞预留段时直接换随机串）；
//   留空 + 更新 → 返回空串，调用方保留行上原 slug——已分享的 URL 不因改个标题断掉。
//
// 返回值已保证与表内其他行不冲突（唯一索引在各表，让号在这里统一做：base-2、base-3…）。
func (s *Service) resolveSlug(ctx context.Context, model any, explicit, title string, self idgen.ID) (string, error) {
	explicit = strings.TrimSpace(explicit)
	if explicit == "" && self != 0 {
		return "", nil
	}
	if explicit == "" {
		explicit = slug.From(title, slug.MaxLen)
		if err := ValidateSlug(explicit); err != nil {
			// 生成结果撞了预留段等边界：不把校验错误抛给用户，直接换随机串
			explicit = slug.Random()
		}
	} else if err := ValidateSlug(explicit); err != nil {
		return "", err
	}
	for n := 2; ; n++ {
		var cnt int64
		q := s.db.WithContext(ctx).Model(model).Where("slug = ?", explicit)
		if self != 0 {
			q = q.Where("id <> ?", int64(self))
		}
		if err := q.Count(&cnt).Error; err != nil {
			return "", err
		}
		if cnt == 0 {
			return explicit, nil
		}
		// 行业标准让号：base-2、base-3…；超长先截短再让号
		explicit = slug.Clamp(fmt.Sprintf("%s-%d", explicit, n), slug.MaxLen, "")
		if explicit == "" {
			explicit = slug.Random()
		}
	}
}
