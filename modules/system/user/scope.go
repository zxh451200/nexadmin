package user

import (
	"context"
	"strings"

	"nexadmin/core/auth"
	"nexadmin/core/datascope"
	"nexadmin/core/response"
	"nexadmin/pkg/idgen"
)

func (s *service) resolveActorScope(ctx context.Context, me actor) (*datascope.Scope, error) {
	if me.Principal == auth.PrincipalApp {
		return datascope.ResolveApp(ctx, s.db, me.TenantID, me.RoleIDs)
	}
	return datascope.Resolve(ctx, s.db, me.ID, me.Super)
}

// scopeCond 按当前操作者的角色数据范围生成用户列表的附加条件。
// limited=false 表示不限制（超管或任一启用角色的范围是“全部”）。
// 多个角色取并集；用户自己始终在范围内，应用没有“自己”。
func (s *service) scopeCond(ctx context.Context, me actor) (cond string, args []any, limited bool, err error) {
	sc, err := s.resolveActorScope(ctx, me)
	if err != nil || !sc.Limited() {
		return "", nil, false, err
	}
	conds := make([]string, 0, 2)
	if sc.UserID != 0 {
		conds = append(conds, "id = ?")
		args = append(args, int64(sc.UserID))
	}
	if len(sc.Depts) > 0 {
		conds = append(conds, "dept_id IN ?")
		args = append(args, sc.Depts)
	}
	if len(conds) == 0 {
		return "1 = 0", nil, true, nil
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
		if (me.Principal != auth.PrincipalApp && id == me.ID) || seen[id] {
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
