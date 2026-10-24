package roleassign

import "testing"

var tree = map[int64]string{
	50: "/1/50/", 51: "/1/50/51/", 52: "/1/50/52/", 60: "/1/60/", 70: "/1/70/",
}

var (
	opAll        = OperatorScope{All: true}
	opDeptChild  = OperatorScope{Preset: ScopeDeptAndChild, Depts: []int64{50, 51, 52}}
	opDept       = OperatorScope{Preset: ScopeDept, Depts: []int64{50}}
	opSelf       = OperatorScope{Preset: ScopeSelf}
	opCustomOpen = OperatorScope{Custom: true, Depts: []int64{50, 51}}     // 50 的子部门 52 不在集合内：对下级不封闭
	opCustomShut = OperatorScope{Custom: true, Depts: []int64{50, 51, 52}} // 整棵子树都在集合内：封闭
	opCustomLeaf = OperatorScope{Custom: true, Depts: []int64{51, 60}}     // 都是叶子：封闭
	opUnion      = OperatorScope{Preset: ScopeDept, Custom: true, Depts: []int64{50, 70}}
)

func TestScopeNotWider(t *testing.T) {
	cases := []struct {
		name  string
		role  string
		depts []int64
		op    OperatorScope
		want  bool
	}{
		// 角色=全部
		{"全部/操作者全部", ScopeAll, nil, opAll, true},
		{"全部/操作者本部门及以下", ScopeAll, nil, opDeptChild, false},
		{"全部/操作者本部门", ScopeAll, nil, opDept, false},
		{"全部/操作者仅本人", ScopeAll, nil, opSelf, false},
		{"全部/操作者自定义", ScopeAll, nil, opCustomShut, false},
		// 角色=本部门及以下
		{"本部门及以下/操作者全部", ScopeDeptAndChild, nil, opAll, true},
		{"本部门及以下/操作者本部门及以下", ScopeDeptAndChild, nil, opDeptChild, true},
		{"本部门及以下/操作者本部门", ScopeDeptAndChild, nil, opDept, false},
		{"本部门及以下/操作者仅本人", ScopeDeptAndChild, nil, opSelf, false},
		{"本部门及以下/操作者自定义（对下级不封闭）", ScopeDeptAndChild, nil, opCustomOpen, false},
		{"本部门及以下/操作者自定义（封闭）", ScopeDeptAndChild, nil, opCustomShut, true},
		{"本部门及以下/操作者自定义（全是叶子）", ScopeDeptAndChild, nil, opCustomLeaf, true},
		// 角色=本部门
		{"本部门/操作者全部", ScopeDept, nil, opAll, true},
		{"本部门/操作者本部门及以下", ScopeDept, nil, opDeptChild, true},
		{"本部门/操作者本部门", ScopeDept, nil, opDept, true},
		{"本部门/操作者仅本人", ScopeDept, nil, opSelf, false},
		{"本部门/操作者自定义", ScopeDept, nil, opCustomOpen, true},
		// 角色=仅本人：任何操作者都允许
		{"仅本人/操作者仅本人", ScopeSelf, nil, opSelf, true},
		{"仅本人/操作者自定义", ScopeSelf, nil, opCustomOpen, true},
		// 角色=自定义（部门集合 R）
		{"自定义/操作者全部", ScopeCustom, []int64{60, 99}, opAll, true},
		{"自定义 R⊆操作者本部门及以下可见部门", ScopeCustom, []int64{50, 52}, opDeptChild, true},
		{"自定义 R 超出操作者本部门及以下", ScopeCustom, []int64{50, 60}, opDeptChild, false},
		{"自定义 R={操作者本部门} 对操作者本部门", ScopeCustom, []int64{50}, opDept, true},
		{"自定义 R 含本部门以外对操作者本部门", ScopeCustom, []int64{50, 51}, opDept, false},
		{"自定义 R 为空对操作者仅本人", ScopeCustom, nil, opSelf, true},
		{"自定义 R 非空对操作者仅本人", ScopeCustom, []int64{50}, opSelf, false},
		{"自定义 R⊆S", ScopeCustom, []int64{51}, opCustomOpen, true},
		{"自定义 R 不是 S 的子集", ScopeCustom, []int64{51, 60}, opCustomOpen, false},
		// 操作者多个角色：并集
		{"并集：本部门+自定义，角色本部门", ScopeDept, nil, opUnion, true},
		{"并集：本部门+自定义，角色本部门及以下（不封闭）", ScopeDeptAndChild, nil, opUnion, false},
		{"并集：本部门+自定义，角色自定义 ⊆ 并集", ScopeCustom, []int64{50, 70}, opUnion, true},
		{"并集：本部门+自定义，角色自定义 ⊄ 并集", ScopeCustom, []int64{50, 60}, opUnion, false},
		// 未知范围按更宽处理
		{"未知范围", "weird", nil, opDeptChild, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := ScopeNotWider(tc.role, tc.depts, tc.op, tree); got != tc.want {
				t.Errorf("ScopeNotWider = %v，期望 %v", got, tc.want)
			}
		})
	}
}

func TestOperatorCheck(t *testing.T) {
	op := &Operator{Perms: []string{"user:list", "user:update", "dept:*"}, Scope: opDept, Tree: tree}
	ok := Role{ID: 1, Assignable: true, DataScope: ScopeDept, Perms: []string{"user:list", "dept:create"}}
	cases := []struct {
		name string
		op   *Operator
		role func(r Role) Role
		want string
	}{
		{"三道全部满足放行", op, func(r Role) Role { return r }, ""},
		{"仅「可分配」不满足", op, func(r Role) Role { r.Assignable = false; return r }, ReasonNotAssignable},
		{"仅「权限」不满足", op, func(r Role) Role { r.Perms = append(r.Perms, "role:assign"); return r }, ReasonPerms},
		{"仅「数据范围」不满足", op, func(r Role) Role { r.DataScope = ScopeDeptAndChild; return r }, ReasonScope},
		{"自定义范围不是子集", op, func(r Role) Role { r.DataScope = ScopeCustom; r.Depts = []int64{50, 60}; return r }, ReasonScope},
		{"自定义范围是子集放行", op, func(r Role) Role { r.DataScope = ScopeCustom; r.Depts = []int64{50}; return r }, ""},
		{"内置超管角色不可分配", &Operator{Perms: []string{"*"}, Scope: opAll}, func(r Role) Role { r.Builtin = true; return r }, ReasonNotAssignable},
		{"权限含 * 的角色不可分配给非超管（即使操作者有 *）", &Operator{Perms: []string{"*"}, Scope: opAll},
			func(r Role) Role { r.Perms = []string{"*"}; return r }, ReasonNotAssignable},
		{"操作者通配权限覆盖", &Operator{Perms: []string{"dept:*", "user:*"}, Scope: opDept, Tree: tree},
			func(r Role) Role { r.Perms = []string{"dept:list", "user:delete"}; return r }, ""},
		{"通配权限点不能被普通权限覆盖", op, func(r Role) Role { r.Perms = []string{"user:*"}; return r }, ReasonPerms},
		{"空权限角色放行", op, func(r Role) Role { r.Perms = nil; return r }, ""},
		{"超管豁免三道", &Operator{Super: true}, func(r Role) Role {
			r.Assignable, r.Builtin, r.DataScope, r.Perms = false, true, ScopeAll, []string{"*"}
			return r
		}, ""},
		{"多角色并集：权限取并集后放行", &Operator{Perms: []string{"user:list", "role:assign"}, Scope: opUnion, Tree: tree},
			func(r Role) Role { r.Perms = []string{"user:list", "role:assign"}; return r }, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.op.Check(tc.role(ok)); got != tc.want {
				t.Errorf("Check = %q，期望 %q", got, tc.want)
			}
		})
	}
}

func TestPermsWithin(t *testing.T) {
	if !PermsWithin([]string{"a:*"}, []string{"a:b", "a:c"}) || PermsWithin([]string{"a:b"}, []string{"a:b", "a:c"}) || !PermsWithin(nil, nil) {
		t.Error("PermsWithin 判断错误")
	}
}
