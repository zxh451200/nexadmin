package user

import (
	"context"
	"sort"

	"gorm.io/gorm"

	"nexadmin/core/datascope"
	"nexadmin/core/response"
	"nexadmin/core/roleassign"
	"nexadmin/pkg/idgen"
)

// 防提权：谁能给用户分配哪些角色、能把用户放进哪个部门。
//
// 规则（超管一律豁免）：
//   - 角色必须同时通过三道校验（见 roleassign 包）：可分配（role.assignable，且不是内置超管角色、权限不含 *）、
//     权限集合是操作者当前权限的子集、数据范围不比操作者宽；
//   - 部门：受限操作者只能把用户放进自己数据范围内的部门（不能不填部门，否则用户会脱离范围）；
//   - 操作者改自己的资料时，部门与角色必须保持不变，不能自我提权或扩大范围；
//   - 更新用户时只校验“新增”的角色，保留用户已有的角色不受影响。

func forbiddenRole(msg string) *response.Error {
	return response.Forbidden("error.forbidden").WithMsg(msg)
}

// guardRoles 校验操作者能否分配这些角色（三道校验，任一不满足即 403 并说明原因）。
func (s *service) guardRoles(ctx context.Context, me actor, roles []idgen.ID) error {
	if me.Super || len(roles) == 0 {
		return nil
	}
	loaded, err := roleassign.Load(ctx, s.db, tenantOf(ctx), roles)
	if err != nil {
		return err
	}
	if len(loaded) != len(roles) { // 不在本租户的角色按不可分配处理
		return forbiddenRole(roleassign.ReasonNotAssignable)
	}
	op, err := roleassign.LoadOperator(ctx, s.db, me.ID, me.Super)
	if err != nil {
		return err
	}
	for _, r := range loaded {
		if reason := op.Check(r); reason != "" {
			return forbiddenRole(reason)
		}
	}
	return nil
}

func (s *service) roleIDsOf(ctx context.Context, uid idgen.ID) ([]idgen.ID, error) {
	var raw []int64
	if err := s.db.WithContext(ctx).Model(&userRole{}).Where("user_id = ?", int64(uid)).Pluck("role_id", &raw).Error; err != nil {
		return nil, err
	}
	out := make([]idgen.ID, len(raw))
	for i, v := range raw {
		out[i] = idgen.ID(v)
	}
	return out, nil
}

func sameIDs(a, b []idgen.ID) bool {
	if len(a) != len(b) {
		return false
	}
	x, y := append([]idgen.ID(nil), a...), append([]idgen.ID(nil), b...)
	sort.Slice(x, func(i, j int) bool { return x[i] < x[j] })
	sort.Slice(y, func(i, j int) bool { return y[i] < y[j] })
	for i := range x {
		if x[i] != y[i] {
			return false
		}
	}
	return true
}

// guardUserWrite 在创建（cur=nil）与修改用户时校验部门与角色。超管豁免。
func (s *service) guardUserWrite(ctx context.Context, me actor, cur *User, p parsedInput) error {
	if me.Super {
		return nil
	}
	var have []idgen.ID
	if cur != nil {
		var err error
		if have, err = s.roleIDsOf(ctx, cur.ID); err != nil {
			return err
		}
		if cur.ID == me.ID { // 改自己的资料：部门与角色必须原样
			if p.dept != cur.DeptID || !sameIDs(p.roles, have) {
				return forbiddenRole("不能修改自己的部门或角色")
			}
			return nil
		}
	}
	if cur == nil || p.dept != cur.DeptID {
		sc, err := datascope.Resolve(ctx, s.db, me.ID, me.Super)
		if err != nil {
			return err
		}
		if sc.Limited() && (p.dept == 0 || !sc.HasDept(int64(p.dept))) {
			return forbiddenRole("所选部门超出你的数据范围")
		}
	}
	held := make(map[idgen.ID]bool, len(have))
	for _, r := range have {
		held[r] = true
	}
	var added []idgen.ID
	for _, r := range p.roles {
		if !held[r] {
			added = append(added, r)
		}
	}
	return s.guardRoles(ctx, me, added)
}

// isProtectedUser 报告 u 是否为超管或持有内置（builtin）角色的账号。这类账号不能被外部供号通道
// （SCIM）改动，也不能被外部身份（LDAP / CAS 的 matchExisting）绑定，否则可经改资料 / 重置密码接管。
func isProtectedUser(ctx context.Context, db *gorm.DB, u *User) (bool, error) {
	if u.IsSuper {
		return true, nil
	}
	var n int64
	err := db.WithContext(ctx).Table("sys_user_role AS ur").
		Joins("JOIN sys_role r ON r.id = ur.role_id AND r.builtin = ?", true).
		Where("ur.user_id = ?", int64(u.ID)).Count(&n).Error
	return n > 0, err
}

// guardTarget 禁止非超管操作超管账号或持有内置超管角色的账号（改资料/状态/重置密码/解锁），否则可经重置密码接管。
// Provision 通道（SCIM）即使带 Super 也不豁免；它改自己的账号没有意义，同样不豁免。
func (s *service) guardTarget(ctx context.Context, me actor, u *User) error {
	if me.Super && !me.Provision {
		return nil
	}
	if !me.Provision && u.ID == me.ID {
		return nil
	}
	deny := forbiddenRole("无权操作超级管理员账号")
	protected, err := isProtectedUser(ctx, s.db, u)
	if err != nil {
		return err
	}
	if protected {
		return deny
	}
	return nil
}
