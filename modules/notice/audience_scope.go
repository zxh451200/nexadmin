package notice

import (
	"context"

	"nexadmin/core/datascope"
	"nexadmin/core/response"
	"nexadmin/pkg/idgen"
)

// 发布对象的数据范围校验：非超管且数据范围非“全部”的操作者（sc.Limited()）不能越过自己的范围发公告。
//   - 目标为“全员”：拒绝（会触达范围外的所有人）；
//   - 指定部门：必须全部在其数据范围内；
//   - 指定用户：必须全部在其数据范围内（本人或范围内部门的成员）；
//   - 指定角色：不限制（角色本身不带部门归属，且公告只展示给角色成员）；
//   - 平台公告（面向全部租户）：受限操作者一律拒绝，等价于“仅超管/范围全部”。
// 超管与范围=全部豁免。拒绝文案走 i18n key（notice.audience_*），中英文各一条。

func audienceForbidden(key string) error { return response.Forbidden(key) }

// CheckAudience 校验表单 f 的发布对象在操作者 sc 范围内；sc 不受限时不产生任何查询。
func (s *Service) CheckAudience(ctx context.Context, sc *datascope.Scope, f Form) error {
	if !sc.Limited() {
		return nil
	}
	if f.Platform {
		return audienceForbidden("notice.audience_platform_forbidden")
	}
	switch f.Scope {
	case ScopeAll:
		return audienceForbidden("notice.audience_all_forbidden")
	case ScopeDept:
		for _, id := range f.TargetIDs {
			if !sc.HasDept(int64(id)) {
				return audienceForbidden("notice.audience_dept_forbidden")
			}
		}
	case ScopeUser:
		return s.checkUsers(ctx, sc, f.TargetIDs)
	}
	return nil // ScopeRole 不限制
}

// checkUsers 指定用户必须都在范围内；范围外与不存在（含已删除）同样拒绝，防枚举。
func (s *Service) checkUsers(ctx context.Context, sc *datascope.Scope, targets []idgen.ID) error {
	seen := make(map[idgen.ID]bool, len(targets))
	uniq := make([]int64, 0, len(targets))
	for _, id := range targets {
		if !seen[id] {
			seen[id] = true
			uniq = append(uniq, int64(id))
		}
	}
	if len(uniq) == 0 {
		return nil
	}
	q := s.db.WithContext(ctx).Table("sys_user").Where("id IN ? AND deleted_at IS NULL", uniq)
	if cond, args, ok := sc.UserIDCond("id"); ok {
		q = q.Where(cond, args...)
	}
	var n int64
	if err := q.Count(&n).Error; err != nil {
		return err
	}
	if int(n) != len(uniq) {
		return audienceForbidden("notice.audience_user_forbidden")
	}
	return nil
}
