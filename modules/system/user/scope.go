package user

import (
	"context"
	"strings"

	"nexadmin/core/datascope"
	"nexadmin/core/response"
	"nexadmin/pkg/idgen"
)

// scopeCond 按当前操作者的角色数据范围生成用户列表的附加条件。
// limited=false 表示不限制（超管或任一启用角色的范围是“全部”）。
// 多个角色取并集；无论如何都能看到自己。
// “本部门及以下”依赖部门表的物化路径 sys_dept.path（如 /1/4/），取不到时退化为“本部门”。
func (s *service) scopeCond(ctx context.Context, me actor) (cond string, args []any, limited bool, err error) {
	if me.Super {
		return "", nil, false, nil
	}
	db := s.db.WithContext(ctx)
	var roles []struct {
		ID        idgen.ID
		DataScope string
	}
	err = db.Table("sys_user_role AS ur").Select("r.id AS id, r.data_scope AS data_scope").
		Joins("JOIN sys_role r ON r.id = ur.role_id AND r.status = ?", statusOn).
		Where("ur.user_id = ?", int64(me.ID)).Scan(&roles).Error
	if err != nil {
		return "", nil, false, err
	}

	conds, args := []string{"id = ?"}, []any{int64(me.ID)}
	var self struct{ DeptID idgen.ID }
	if err = db.Model(&User{}).Select("dept_id").Where("id = ?", int64(me.ID)).Scan(&self).Error; err != nil {
		return "", nil, false, err
	}
	var custom []int64
	hasDept, hasChild := false, false
	for _, r := range roles {
		switch r.DataScope {
		case "all":
			return "", nil, false, nil
		case "dept":
			if self.DeptID != 0 && !hasDept {
				hasDept = true
				conds, args = append(conds, "dept_id = ?"), append(args, int64(self.DeptID))
			}
		case "deptAndChild":
			if self.DeptID == 0 || hasChild {
				continue
			}
			hasChild = true
			var path string
			if e := db.Table("sys_dept").Select("path").Where("id = ?", int64(self.DeptID)).Scan(&path).Error; e == nil && path != "" {
				conds = append(conds, "dept_id IN (SELECT id FROM sys_dept WHERE path LIKE ?)")
				args = append(args, escapeLike(path)+"%")
			} else {
				conds, args = append(conds, "dept_id = ?"), append(args, int64(self.DeptID))
			}
		case "custom":
			custom = append(custom, int64(r.ID))
		}
	}
	if len(custom) > 0 {
		conds = append(conds, "dept_id IN (SELECT dept_id FROM sys_role_dept WHERE role_id IN ?)")
		args = append(args, custom)
	}
	return "(" + strings.Join(conds, " OR ") + ")", args, true, nil
}

// ensureInScope 校验目标用户全部落在操作者的数据范围内，写接口（改/删/状态/重置密码/解锁/分配角色）执行前调用。
// 超管与“全部”范围不受影响（scopeCond 返回 limited=false，不产生额外查询）；操作者自己始终在范围内。
// 越权统一返回 403；受限操作者对不存在/已删除/跨租户的 id 也返回同样的 403，
// 因此无法通过 403 与 404 的差异枚举范围外的账号。批量时整批拒绝，只给出数量，不指明具体 id。
func (s *service) ensureInScope(ctx context.Context, me actor, ids ...idgen.ID) error {
	uniq := make([]int64, 0, len(ids))
	seen := make(map[idgen.ID]bool, len(ids))
	for _, id := range ids {
		if id == me.ID || seen[id] {
			continue
		}
		seen[id] = true
		uniq = append(uniq, int64(id))
	}
	if me.Super || len(uniq) == 0 {
		return nil
	}
	cond, args, limited, err := s.scopeCond(ctx, me)
	if err != nil {
		return err
	}
	if !limited {
		return nil
	}
	var n int64
	if err := s.db.WithContext(ctx).Model(&User{}).Where("id IN ?", uniq).Where(cond, args...).Count(&n).Error; err != nil {
		return err
	}
	if denied := len(uniq) - int(n); denied > 0 {
		return outOfScope(ctx, denied)
	}
	return nil
}

// outOfScope 是越权错误；批量时带上不可操作的数量。
func outOfScope(ctx context.Context, n int) *response.Error {
	return datascope.Forbidden(ctx, "datascope.resource.user", n)
}

// getScoped 读取单个用户详情，越权与不存在对受限操作者表现一致（403），见 ensureInScope。
func (s *service) getScoped(ctx context.Context, id idgen.ID, me actor) (*userDTO, error) {
	if err := s.ensureInScope(ctx, me, id); err != nil {
		return nil, err
	}
	return s.get(ctx, id)
}
