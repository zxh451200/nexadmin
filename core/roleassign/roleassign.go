// Package roleassign 判断“某个操作者能否把某个角色分配给别人”，用户模块（分配/导入）与角色模块（options）共用。
//
// 对非超管，要同时满足三道校验：
//  1. 角色可分配：role.assignable 为 true，且不是内置超管角色，且权限里不含 "*"；
//  2. 权限不超出：角色的权限集合 ⊆ 操作者当前权限集合（多个角色取并集）；
//  3. 数据范围不更宽：见 ScopeNotWider。
//
// 超管豁免全部三道。
package roleassign

import (
	"context"
	"strings"

	"gorm.io/gorm"

	"nexadmin/core/auth"
	"nexadmin/core/datascope"
	"nexadmin/pkg/idgen"
)

// 数据范围取值，与 sys_role.data_scope 一致。
const (
	ScopeAll          = "all"
	ScopeCustom       = "custom"
	ScopeDept         = "dept"
	ScopeDeptAndChild = "deptAndChild"
	ScopeSelf         = "self"
)

// 拒绝原因（Check 返回空串表示放行）。
const (
	ReasonNotAssignable = "该角色不允许被下级管理员分配"
	ReasonPerms         = "所选角色的权限超出了你自己的权限范围，不能分配"
	ReasonScope         = "所选角色的数据范围比你的数据范围更宽，不能分配"
)

// Role 是待分配的角色及判断所需的信息。
type Role struct {
	ID         idgen.ID
	Builtin    bool
	Assignable bool
	DataScope  string
	Depts      []int64 // 自定义范围的部门
	Perms      []string
}

// OperatorScope 是操作者数据范围的合并结果（多个角色取并集）。
type OperatorScope struct {
	All    bool
	Preset string  // 预设范围里最宽的一档：""/self/dept/deptAndChild
	Custom bool    // 是否持有自定义范围角色
	Depts  []int64 // 合并后的具体部门集合
}

// Operator 是操作者的权限与数据范围。Tree 为 部门 id -> 物化路径，仅在需要时（持有自定义范围）加载。
type Operator struct {
	Super bool
	Perms []string
	Scope OperatorScope
	Tree  map[int64]string
}

func set(ids []int64) map[int64]bool {
	m := make(map[int64]bool, len(ids))
	for _, id := range ids {
		m[id] = true
	}
	return m
}

func subset(a []int64, b map[int64]bool) bool {
	for _, v := range a {
		if !b[v] {
			return false
		}
	}
	return true
}

func presetRank(p string) int {
	switch p {
	case ScopeDeptAndChild:
		return 3
	case ScopeDept:
		return 2
	case ScopeSelf:
		return 1
	}
	return 0
}

// closedUnderChildren 报告 depts 是否对“下级”封闭：每个部门的整棵子树都在 depts 内（按物化路径判断）。
func closedUnderChildren(depts []int64, tree map[int64]string) bool {
	in := set(depts)
	for _, d := range depts {
		prefix, ok := tree[d]
		if !ok || prefix == "" {
			continue
		}
		for id, path := range tree {
			if !in[id] && strings.HasPrefix(path, prefix) {
				return false
			}
		}
	}
	return true
}

// ScopeNotWider 判断 角色的数据范围 是否“不比操作者宽”（true = 可分配）。宽窄顺序：全部 > 本部门及以下 > 本部门 > 仅本人；
// 自定义范围按部门集合比较。操作者多个角色已合并为 OperatorScope（取并集 = 最宽）。
// 被分配者一定在操作者的范围内，所以“本部门”类角色的有效部门落在操作者可见部门里，“本部门及以下”则是其整棵子树。
//
// 结论（行 = 角色范围，列 = 操作者范围）：
//
//	角色\操作者    全部   本部门及以下   本部门        仅本人   自定义(集合 S)
//	全部           允许   拒绝           拒绝          拒绝     拒绝
//	本部门及以下   允许   允许           拒绝          拒绝     S 对下级封闭（每个部门的整棵子树都在 S 内）才允许
//	本部门         允许   允许           允许          拒绝     允许（有效部门 ⊆ S）
//	仅本人         允许   允许           允许          允许     允许
//	自定义(集合 R) 允许   R ⊆ 操作者可见部门 / 本部门及以下时同左 / 本部门时 R ⊆ {本部门} / 仅本人时仅 R 为空才允许 / 自定义时 R ⊆ S
//
// 操作者同时有多个角色时按并集：任一角色为“全部”即全部；预设范围取最宽的一档；自定义部门与预设展开的部门合并成一个集合比较。
// “本部门/本部门及以下”的操作者没有所属部门时视为仅本人。
func ScopeNotWider(roleScope string, roleDepts []int64, op OperatorScope, tree map[int64]string) bool {
	if roleScope == ScopeSelf || op.All {
		return true
	}
	switch roleScope {
	case ScopeAll:
		return false
	case ScopeDept:
		return presetRank(op.Preset) >= presetRank(ScopeDept) || (op.Custom && len(op.Depts) > 0)
	case ScopeDeptAndChild:
		if presetRank(op.Preset) >= presetRank(ScopeDeptAndChild) {
			return true
		}
		return op.Custom && len(op.Depts) > 0 && closedUnderChildren(op.Depts, tree)
	case ScopeCustom:
		return subset(roleDepts, set(op.Depts))
	}
	return false // 未知范围一律按更宽处理
}

// PermsWithin 报告 need 里的每个权限是否都被 own 覆盖（auth.HasPerm 语义，支持 "*" 与 "xx:*"）。
func PermsWithin(own, need []string) bool {
	for _, p := range need {
		if !auth.HasPerm(own, p) {
			return false
		}
	}
	return true
}

// Check 返回空串表示 op 可以分配 r，否则是拒绝原因。三道校验各自独立，按 可分配 → 权限 → 数据范围 的顺序报告第一个不满足的。
func (op *Operator) Check(r Role) string {
	if op.Super {
		return ""
	}
	if !r.Assignable || r.Builtin {
		return ReasonNotAssignable
	}
	for _, p := range r.Perms {
		if p == "*" {
			return ReasonNotAssignable // 权限含 * 的角色等同超管角色
		}
	}
	if !PermsWithin(op.Perms, r.Perms) {
		return ReasonPerms
	}
	if !ScopeNotWider(r.DataScope, r.Depts, op.Scope, op.Tree) {
		return ReasonScope
	}
	return ""
}

// LoadOperator 实时读取操作者的权限与数据范围（不信任令牌里的快照）。
func LoadOperator(ctx context.Context, db *gorm.DB, userID idgen.ID, super bool) (*Operator, error) {
	op := &Operator{Super: super}
	if super {
		return op, nil
	}
	perms, err := ownPerms(ctx, db, userID)
	if err != nil {
		return nil, err
	}
	op.Perms = perms
	sc, err := datascope.Resolve(ctx, db, userID, false)
	if err != nil {
		return nil, err
	}
	op.Scope = OperatorScope{All: sc.All, Preset: sc.Preset, Custom: sc.Custom, Depts: sc.Depts}
	if !sc.All && sc.Custom {
		var rows []struct {
			ID   int64
			Path string
		}
		if err := db.WithContext(ctx).Table("sys_dept").Select("id, path").Scan(&rows).Error; err != nil {
			return nil, err
		}
		op.Tree = make(map[int64]string, len(rows))
		for _, r := range rows {
			op.Tree[r.ID] = r.Path
		}
	}
	return op, nil
}

// ownPerms 取操作者当前权限码：持有启用的内置超管角色 -> ["*"]，否则是启用角色上的菜单/按钮权限码。
func ownPerms(ctx context.Context, db *gorm.DB, userID idgen.ID) ([]string, error) {
	db = db.WithContext(ctx)
	var builtin int64
	if err := db.Table("sys_user_role AS ur").
		Joins("JOIN sys_role r ON r.id = ur.role_id AND r.status = ? AND r.builtin = ?", "1", true).
		Where("ur.user_id = ?", int64(userID)).Count(&builtin).Error; err != nil {
		return nil, err
	}
	if builtin > 0 {
		return []string{"*"}, nil
	}
	var perms []string
	err := db.Table("sys_user_role AS ur").Select("DISTINCT m.perm").
		Joins("JOIN sys_role r ON r.id = ur.role_id AND r.status = ?", "1").
		Joins("JOIN sys_role_menu rm ON rm.role_id = r.id").
		Joins("JOIN sys_menu m ON m.id = rm.menu_id AND m.status = ?", "1").
		Where("ur.user_id = ? AND m.perm <> ''", int64(userID)).Scan(&perms).Error
	return perms, err
}

// Load 读取角色及其权限码（含停用菜单上的，宁严勿松）、自定义范围部门。ids 为空表示当前租户全部启用角色。
func Load(ctx context.Context, db *gorm.DB, tenantID idgen.ID, ids []idgen.ID) ([]Role, error) {
	db = db.WithContext(ctx)
	q := db.Table("sys_role").Select("id, builtin, assignable, data_scope").Where("tenant_id = ?", int64(tenantID))
	if len(ids) > 0 {
		q = q.Where("id IN ?", ids)
	} else {
		q = q.Where("status = ?", "1").Order("sort ASC, id ASC")
	}
	var rs []struct {
		ID         idgen.ID
		Builtin    bool
		Assignable bool
		DataScope  string
	}
	if err := q.Scan(&rs).Error; err != nil {
		return nil, err
	}
	if len(rs) == 0 {
		return nil, nil
	}
	all := make([]int64, len(rs))
	var custom []int64
	for i, r := range rs {
		all[i] = int64(r.ID)
		if r.DataScope == ScopeCustom {
			custom = append(custom, int64(r.ID))
		}
	}
	var perms []struct {
		RoleID idgen.ID
		Perm   string
	}
	if err := db.Table("sys_role_menu AS rm").Select("rm.role_id AS role_id, m.perm AS perm").
		Joins("JOIN sys_menu m ON m.id = rm.menu_id").
		Where("rm.role_id IN ? AND m.perm <> ''", all).Scan(&perms).Error; err != nil {
		return nil, err
	}
	byRole := map[idgen.ID][]string{}
	for _, p := range perms {
		byRole[p.RoleID] = append(byRole[p.RoleID], p.Perm)
	}
	depts := map[idgen.ID][]int64{}
	if len(custom) > 0 {
		var ds []struct {
			RoleID idgen.ID
			DeptID int64
		}
		if err := db.Table("sys_role_dept").Select("role_id, dept_id").Where("role_id IN ?", custom).Scan(&ds).Error; err != nil {
			return nil, err
		}
		for _, d := range ds {
			depts[d.RoleID] = append(depts[d.RoleID], d.DeptID)
		}
	}
	out := make([]Role, len(rs))
	for i, r := range rs {
		out[i] = Role{ID: r.ID, Builtin: r.Builtin, Assignable: r.Assignable, DataScope: r.DataScope, Depts: depts[r.ID], Perms: byRole[r.ID]}
	}
	return out, nil
}
