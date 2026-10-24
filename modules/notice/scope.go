package notice

import (
	"context"

	"gorm.io/gorm"

	"nexadmin/core/datascope"
	"nexadmin/pkg/idgen"
)

// 公告管理端的数据范围：按创建者（created_by）所属部门裁剪。受限操作者只能看到/操作
// 自己创建的和本范围内部门成员创建的公告；系统种子公告（created_by=0）对其不可见。
// 超管、范围“全部”不受影响。新建公告不受限（创建者就是自己）。
// 发布对象（全员/角色/部门/用户）的越权限制见 audience_scope.go（CheckAudience）。

func scopedNotices(tx *gorm.DB, sc *datascope.Scope) *gorm.DB {
	if cond, args, ok := sc.UserIDCond("created_by"); ok {
		return tx.Where(cond, args...)
	}
	return tx
}

// CheckIDs 校验公告 id 全部在范围内；范围外与不存在同样返回 403，批量时整批拒绝并只报数量。
func (s *Service) CheckIDs(ctx context.Context, sc *datascope.Scope, ids ...idgen.ID) error {
	if !sc.Limited() {
		return nil
	}
	seen := make(map[idgen.ID]bool, len(ids))
	uniq := make([]int64, 0, len(ids))
	for _, id := range ids {
		if !seen[id] {
			seen[id] = true
			uniq = append(uniq, int64(id))
		}
	}
	var n int64
	if err := scopedNotices(s.db.WithContext(ctx).Model(&Notice{}).Where("id IN ?", uniq), sc).Count(&n).Error; err != nil {
		return err
	}
	if denied := len(uniq) - int(n); denied > 0 {
		return datascope.Forbidden("公告", denied)
	}
	return nil
}
