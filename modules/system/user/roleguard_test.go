package user

import (
	"testing"

	"github.com/DATA-DOG/go-sqlmock"

	"nexadmin/pkg/idgen"
)

// expectResolve 预期 datascope.Resolve 的查询：角色 → 自己的部门 →（deptAndChild）路径与子树。
func expectResolve(mock sqlmock.Sqlmock, scope string, selfDept int64, subtree ...int64) {
	mock.ExpectQuery("FROM sys_user_role AS ur").
		WillReturnRows(sqlmock.NewRows([]string{"id", "data_scope"}).AddRow(7, scope))
	if scope == "all" {
		return
	}
	mock.ExpectQuery("SELECT dept_id FROM `sys_user`").WillReturnRows(sqlmock.NewRows([]string{"dept_id"}).AddRow(selfDept))
	if scope == "deptAndChild" {
		mock.ExpectQuery("SELECT path FROM `sys_dept`").WillReturnRows(sqlmock.NewRows([]string{"path"}).AddRow("/1/50/"))
		rows := sqlmock.NewRows([]string{"id"})
		for _, id := range subtree {
			rows.AddRow(id)
		}
		mock.ExpectQuery("SELECT `id` FROM `sys_dept` WHERE path LIKE").WillReturnRows(rows)
	}
	if scope == "custom" {
		mock.ExpectQuery("FROM `sys_role_dept`").WillReturnRows(sqlmock.NewRows([]string{"dept_id"}).AddRow(50))
		mock.ExpectQuery("SELECT id, path FROM `sys_dept`").WillReturnRows(sqlmock.NewRows([]string{"id", "path"}).AddRow(50, "/1/50/"))
	}
}

// roleSpec 描述 loadGrants 要返回的角色。
type roleSpec struct {
	id        int64
	builtin   bool
	notAssign bool
	dataScope string
	perms     []string
}

// expectRole 预期 roleassign.Load：角色、权限码（custom 范围时还有部门）。
func expectRole(mock sqlmock.Sqlmock, r roleSpec) {
	if r.dataScope == "" {
		r.dataScope = "self"
	}
	mock.ExpectQuery("FROM `sys_role` WHERE tenant_id").
		WillReturnRows(sqlmock.NewRows([]string{"id", "builtin", "assignable", "data_scope"}).AddRow(r.id, r.builtin, !r.notAssign, r.dataScope))
	rows := sqlmock.NewRows([]string{"role_id", "perm"})
	for _, p := range r.perms {
		rows.AddRow(r.id, p)
	}
	mock.ExpectQuery("FROM sys_role_menu AS rm").WillReturnRows(rows)
	if r.dataScope == "custom" {
		mock.ExpectQuery("FROM `sys_role_dept`").WillReturnRows(sqlmock.NewRows([]string{"role_id", "dept_id"}).AddRow(r.id, 50))
	}
}

// expectOperator 预期 roleassign.LoadOperator：权限码，然后数据范围（同 expectResolve）。
func expectOperator(mock sqlmock.Sqlmock, perms []string, scope string, selfDept int64, subtree ...int64) {
	mock.ExpectQuery("SELECT count\\(\\*\\) FROM sys_user_role AS ur JOIN sys_role r ON r.id = ur.role_id AND r.status").
		WillReturnRows(sqlmock.NewRows([]string{"c"}).AddRow(0))
	rows := sqlmock.NewRows([]string{"perm"})
	for _, p := range perms {
		rows.AddRow(p)
	}
	mock.ExpectQuery("SELECT DISTINCT m.perm").WillReturnRows(rows)
	expectResolve(mock, scope, selfDept, subtree...)
}

func expectHeld(mock sqlmock.Sqlmock, roles ...int64) {
	rows := sqlmock.NewRows([]string{"role_id"})
	for _, r := range roles {
		rows.AddRow(r)
	}
	mock.ExpectQuery("SELECT `role_id` FROM `sys_user_role`").WillReturnRows(rows)
}

func TestGuardUserWrite(t *testing.T) {
	me := actor{ID: operatorID}
	other := &User{DeptID: 50}
	other.ID = 1
	self := &User{DeptID: 50}
	self.ID = operatorID
	// 角色分配：先 Resolve（部门校验），再 Load 角色、LoadOperator。
	assign := func(r roleSpec, perms []string, opScope string) func(m sqlmock.Sqlmock) {
		return func(m sqlmock.Sqlmock) {
			expectResolve(m, "dept", 50)
			expectRole(m, r)
			expectOperator(m, perms, opScope, 50)
		}
	}
	cases := []struct {
		name  string
		me    actor
		cur   *User
		in    parsedInput
		setup func(m sqlmock.Sqlmock)
		deny  bool
		why   string // 期望的错误 key
	}{
		{"创建：范围内部门、无角色", me, nil, parsedInput{dept: 50},
			func(m sqlmock.Sqlmock) { expectResolve(m, "dept", 50) }, false, ""},
		{"创建：范围外部门被拒", me, nil, parsedInput{dept: 60},
			func(m sqlmock.Sqlmock) { expectResolve(m, "dept", 50) }, true, "user.dept_out_of_scope"},
		{"创建：受限操作者不填部门被拒", me, nil, parsedInput{},
			func(m sqlmock.Sqlmock) { expectResolve(m, "dept", 50) }, true, "user.dept_out_of_scope"},
		{"创建：deptAndChild 子部门放行", me, nil, parsedInput{dept: 51},
			func(m sqlmock.Sqlmock) { expectResolve(m, "deptAndChild", 50, 50, 51) }, false, ""},
		{"创建：范围=全部时任意部门放行", me, nil, parsedInput{dept: 60},
			func(m sqlmock.Sqlmock) { expectResolve(m, "all", 50) }, false, ""},
		{"创建：三道校验全部满足放行", me, nil, parsedInput{dept: 50, roles: []idgen.ID{5}},
			assign(roleSpec{id: 5, dataScope: "dept", perms: []string{"user:list"}}, []string{"user:list", "user:update"}, "dept"), false, ""},
		{"创建：角色不可分配被拒", me, nil, parsedInput{dept: 50, roles: []idgen.ID{5}},
			assign(roleSpec{id: 5, notAssign: true, perms: []string{"user:list"}}, []string{"user:list"}, "dept"), true, "user.role_not_assignable"},
		{"创建：权限超出自身被拒（提权）", me, nil, parsedInput{dept: 50, roles: []idgen.ID{5}},
			assign(roleSpec{id: 5, perms: []string{"user:list", "role:assign"}}, []string{"user:list"}, "dept"), true, "user.role_perms_exceeded"},
		{"创建：数据范围更宽被拒", me, nil, parsedInput{dept: 50, roles: []idgen.ID{5}},
			assign(roleSpec{id: 5, dataScope: "deptAndChild", perms: []string{"user:list"}}, []string{"user:list"}, "dept"), true, "user.role_scope_exceeded"},
		{"创建：自定义范围不是子集被拒", me, nil, parsedInput{dept: 50, roles: []idgen.ID{5}},
			func(m sqlmock.Sqlmock) {
				expectResolve(m, "dept", 50)
				m.ExpectQuery("FROM `sys_role` WHERE tenant_id").WillReturnRows(sqlmock.NewRows([]string{"id", "builtin", "assignable", "data_scope"}).AddRow(5, false, true, "custom"))
				m.ExpectQuery("FROM sys_role_menu AS rm").WillReturnRows(sqlmock.NewRows([]string{"role_id", "perm"}))
				m.ExpectQuery("FROM `sys_role_dept`").WillReturnRows(sqlmock.NewRows([]string{"role_id", "dept_id"}).AddRow(5, 50).AddRow(5, 99))
				expectOperator(m, nil, "dept", 50)
			}, true, "user.role_scope_exceeded"},
		{"创建：分配内置超管角色被拒", me, nil, parsedInput{dept: 50, roles: []idgen.ID{5}},
			assign(roleSpec{id: 5, builtin: true}, []string{"user:list"}, "dept"), true, "user.role_not_assignable"},
		{"超管豁免部门与角色", actor{ID: 1, Super: true}, nil, parsedInput{dept: 60, roles: []idgen.ID{5}},
			func(m sqlmock.Sqlmock) {}, false, ""},
		{"修改他人：部门与角色不变，无需校验", me, other, parsedInput{dept: 50, roles: []idgen.ID{5}},
			func(m sqlmock.Sqlmock) { expectHeld(m, 5) }, false, ""},
		{"修改他人：把用户移出范围被拒", me, other, parsedInput{dept: 60},
			func(m sqlmock.Sqlmock) { expectHeld(m); expectResolve(m, "dept", 50) }, true, "user.dept_out_of_scope"},
		{"修改他人：移到范围内其他部门放行", me, other, parsedInput{dept: 51},
			func(m sqlmock.Sqlmock) { expectHeld(m); expectResolve(m, "deptAndChild", 50, 50, 51) }, false, ""},
		{"修改他人：新增不可分配的角色被拒，已有角色不受影响", me, other, parsedInput{dept: 50, roles: []idgen.ID{5, 6}},
			func(m sqlmock.Sqlmock) {
				expectHeld(m, 5)
				expectRole(m, roleSpec{id: 6, notAssign: true})
				expectOperator(m, []string{"user:list"}, "dept", 50)
			}, true, "user.role_not_assignable"},
		{"修改自己：资料不变放行", me, self, parsedInput{dept: 50, roles: []idgen.ID{5}},
			func(m sqlmock.Sqlmock) { expectHeld(m, 5) }, false, ""},
		{"修改自己：改部门被拒（扩大范围）", me, self, parsedInput{dept: 1, roles: []idgen.ID{5}},
			func(m sqlmock.Sqlmock) { expectHeld(m, 5) }, true, "user.self_dept_role_immutable"},
		{"修改自己：加角色被拒（自我提权）", me, self, parsedInput{dept: 50, roles: []idgen.ID{5, 6}},
			func(m sqlmock.Sqlmock) { expectHeld(m, 5) }, true, "user.self_dept_role_immutable"},
		{"修改自己：去掉角色也被拒", me, self, parsedInput{dept: 50},
			func(m sqlmock.Sqlmock) { expectHeld(m, 5) }, true, "user.self_dept_role_immutable"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s, mock, ctx := newScopeSvc(t)
			tc.setup(mock)
			err := s.guardUserWrite(ctx, tc.me, tc.cur, tc.in)
			re, forbidden := isForbidden(err)
			if forbidden != tc.deny || (!tc.deny && err != nil) {
				t.Fatalf("err=%v，期望拒绝=%v", err, tc.deny)
			}
			if tc.why != "" && re.Key != tc.why {
				t.Errorf("原因 key 应为 %q，得到 %q", tc.why, re.Key)
			}
			if err := mock.ExpectationsWereMet(); err != nil {
				t.Error(err)
			}
		})
	}
}

func TestGuardTarget(t *testing.T) {
	s, mock, ctx := newScopeSvc(t)
	target := &User{}
	target.ID = 1
	super := &User{IsSuper: true}
	super.ID = 2
	me := actor{ID: operatorID}
	if _, ok := isForbidden(s.guardTarget(ctx, me, super)); !ok {
		t.Error("非超管不能操作超管账号")
	}
	mock.ExpectQuery("SELECT count\\(\\*\\) FROM sys_user_role AS ur").WillReturnRows(sqlmock.NewRows([]string{"c"}).AddRow(1))
	if _, ok := isForbidden(s.guardTarget(ctx, me, target)); !ok {
		t.Error("非超管不能操作持有内置超管角色的账号")
	}
	mock.ExpectQuery("SELECT count\\(\\*\\) FROM sys_user_role AS ur").WillReturnRows(sqlmock.NewRows([]string{"c"}).AddRow(0))
	if err := s.guardTarget(ctx, me, target); err != nil {
		t.Errorf("普通账号应放行：%v", err)
	}
	if err := s.guardTarget(ctx, actor{ID: 9, Super: true}, super); err != nil {
		t.Errorf("超管豁免：%v", err)
	}
	self := &User{}
	self.ID = operatorID
	if err := s.guardTarget(ctx, me, self); err != nil {
		t.Errorf("自己豁免：%v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Error(err)
	}
}

func TestGetScoped(t *testing.T) {
	me := actor{ID: operatorID}
	t.Run("范围外 403", func(t *testing.T) {
		s, mock, ctx := newScopeSvc(t)
		expectScope(mock, "dept")
		expectCount(mock, 0)
		if _, err := s.getScoped(ctx, 1, me); err == nil {
			t.Fatal("期望 403")
		} else if _, ok := isForbidden(err); !ok {
			t.Fatalf("期望 403，得到 %v", err)
		}
		if err := mock.ExpectationsWereMet(); err != nil {
			t.Error(err)
		}
	})
	t.Run("范围内放行到读取", func(t *testing.T) {
		s, mock, ctx := newScopeSvc(t)
		expectScope(mock, "dept")
		expectCount(mock, 1)
		mock.ExpectQuery("FROM `sys_user` WHERE .*id = ").WillReturnRows(sqlmock.NewRows([]string{"id", "tenant_id", "username"}))
		_, err := s.getScoped(ctx, 1, me)
		if _, forbidden := isForbidden(err); forbidden || err == nil {
			t.Fatalf("越过范围校验后应走到读取（这里查不到 -> 404），得到 %v", err)
		}
		if err := mock.ExpectationsWereMet(); err != nil {
			t.Error(err)
		}
	})
	t.Run("超管豁免", func(t *testing.T) {
		s, mock, ctx := newScopeSvc(t)
		mock.ExpectQuery("FROM `sys_user` WHERE .*id = ").WillReturnRows(sqlmock.NewRows([]string{"id", "tenant_id", "username"}))
		_, err := s.getScoped(ctx, 1, actor{ID: 9, Super: true})
		if _, forbidden := isForbidden(err); forbidden {
			t.Fatalf("超管不应被拒：%v", err)
		}
		if err := mock.ExpectationsWereMet(); err != nil {
			t.Error(err)
		}
	})
}

// 创建被拒（部门越界 / 提权）时不得产生写入；导入把这类行记为失败行并说明原因。
func TestCreateAndImportRejected(t *testing.T) {
	me := actor{ID: operatorID}
	t.Run("create 部门越界", func(t *testing.T) {
		s, mock, ctx := newScopeSvc(t)
		mock.ExpectQuery("SELECT count\\(\\*\\) FROM `sys_user`").WillReturnRows(sqlmock.NewRows([]string{"c"}).AddRow(0)) // usernameTaken
		expectResolve(mock, "dept", 50)
		_, err := s.create(ctx, userInput{Username: "newuser", DeptID: "60"}, me)
		if _, ok := isForbidden(err); !ok {
			t.Fatalf("期望 403，得到 %v", err)
		}
		if err := mock.ExpectationsWereMet(); err != nil {
			t.Error(err)
		}
	})
	t.Run("导入：越界部门与未填部门的行失败，原因明确", func(t *testing.T) {
		s, mock, ctx := newScopeSvc(t)
		mock.ExpectQuery("FROM `sys_role`").WillReturnRows(sqlmock.NewRows([]string{"id", "name", "code"}))
		mock.ExpectQuery("FROM `sys_dept`").WillReturnRows(sqlmock.NewRows([]string{"id", "name"}).AddRow(60, "财务部").AddRow(50, "研发部"))
		mock.ExpectQuery("FROM `sys_post`").WillReturnRows(sqlmock.NewRows([]string{"id", "name", "code"}))
		for i := 0; i < 2; i++ { // 两行：财务部（范围外）、未填部门
			mock.ExpectQuery("SELECT count\\(\\*\\) FROM `sys_user`").WillReturnRows(sqlmock.NewRows([]string{"c"}).AddRow(0))
			expectResolve(mock, "dept", 50)
		}
		rows := []importRow{
			{Row: 2, Username: "u_fin", Gender: "男", Dept: "财务部"},
			{Row: 3, Username: "u_nodept", Gender: "男"},
		}
		res, err := s.importUsers(ctx, rows, nil, me)
		if err != nil {
			t.Fatal(err)
		}
		if res.Success != 0 || len(res.Failed) != 2 {
			t.Fatalf("结果 = %+v", res)
		}
		for _, f := range res.Failed {
			if f.Reason == "" || f.Reason == "导入失败" || f.Reason == "写入失败" {
				t.Errorf("第 %d 行失败原因不明确：%q", f.Row, f.Reason)
			}
		}
		if err := mock.ExpectationsWereMet(); err != nil {
			t.Error(err)
		}
	})
}

// 导入：角色不可分配 / 权限超出 / 范围更宽的行失败，原因明确；可分配的角色不在此处拒绝（走到写入前的最后一道校验）。
func TestImportRejectsUnassignableRole(t *testing.T) {
	me := actor{ID: operatorID}
	for _, tc := range []struct {
		name string
		role roleSpec
		why  string
	}{
		{"不可分配", roleSpec{id: 5, notAssign: true}, "user.role_not_assignable"},
		{"权限超出", roleSpec{id: 5, perms: []string{"role:assign"}}, "user.role_perms_exceeded"},
		{"数据范围更宽", roleSpec{id: 5, dataScope: "deptAndChild"}, "user.role_scope_exceeded"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, mock, ctx := newScopeSvc(t)
			mock.ExpectQuery("FROM `sys_role`").WillReturnRows(sqlmock.NewRows([]string{"id", "name", "code"}).AddRow(5, "运营", "operator"))
			mock.ExpectQuery("FROM `sys_dept`").WillReturnRows(sqlmock.NewRows([]string{"id", "name"}).AddRow(50, "研发部"))
			mock.ExpectQuery("FROM `sys_post`").WillReturnRows(sqlmock.NewRows([]string{"id", "name", "code"}))
			mock.ExpectQuery("SELECT count\\(\\*\\) FROM `sys_user`").WillReturnRows(sqlmock.NewRows([]string{"c"}).AddRow(0)) // usernameTaken
			mock.ExpectQuery("SELECT count\\(\\*\\) FROM `sys_role`").WillReturnRows(sqlmock.NewRows([]string{"c"}).AddRow(1)) // checkRoles
			expectResolve(mock, "dept", 50)
			expectRole(mock, tc.role)
			expectOperator(mock, []string{"user:list"}, "dept", 50)
			res, err := s.importUsers(ctx, []importRow{{Row: 2, Username: "u_role", Gender: "男", Dept: "研发部", Roles: "运营"}}, nil, me)
			if err != nil {
				t.Fatal(err)
			}
			if want := errText(tc.why); res.Success != 0 || len(res.Failed) != 1 || res.Failed[0].Reason != want {
				t.Fatalf("结果 = %+v，期望失败原因为 %q", res, want)
			}
			if err := mock.ExpectationsWereMet(); err != nil {
				t.Error(err)
			}
		})
	}
}
