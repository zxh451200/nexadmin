package role

import (
	"context"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"

	"nexadmin/core/roleassign"
)

func newSvc(t *testing.T) (*service, sqlmock.Sqlmock) {
	t.Helper()
	sqlDB, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	db, err := gorm.Open(mysql.New(mysql.Config{Conn: sqlDB, SkipInitializeWithVersion: true}), &gorm.Config{DisableAutomaticPing: true})
	if err != nil {
		t.Fatal(err)
	}
	return &service{db: db}, mock
}

func names(opts []option) string {
	var out []string
	for _, o := range opts {
		out = append(out, o.Name)
	}
	return strings.Join(out, ",")
}

// 角色：1 运营（本部门，user:list）、2 不可分配、3 权限超出、4 范围更宽（本部门及以下）、5 内置超管。
func expectRoleList(mock sqlmock.Sqlmock) {
	mock.ExpectQuery("FROM `sys_role` WHERE status").WillReturnRows(sqlmock.NewRows([]string{"id", "name", "tenant_id"}).
		AddRow(1, "运营", 1).AddRow(2, "不可分配", 1).AddRow(3, "权限超出", 1).AddRow(4, "范围更宽", 1).AddRow(5, "超级管理员", 1))
}

func expectRoleDetails(mock sqlmock.Sqlmock) {
	mock.ExpectQuery("FROM `sys_role` WHERE tenant_id").WillReturnRows(sqlmock.NewRows([]string{"id", "builtin", "assignable", "data_scope"}).
		AddRow(1, false, true, "dept").AddRow(2, false, false, "dept").AddRow(3, false, true, "dept").
		AddRow(4, false, true, "deptAndChild").AddRow(5, true, false, "all"))
	mock.ExpectQuery("FROM sys_role_menu AS rm").WillReturnRows(sqlmock.NewRows([]string{"role_id", "perm"}).
		AddRow(1, "user:list").AddRow(2, "user:list").AddRow(3, "role:assign").AddRow(4, "user:list"))
}

func TestOptionsFiltering(t *testing.T) {
	ctx := context.Background()
	op := &roleassign.Operator{
		Perms: []string{"user:list"},
		Scope: roleassign.OperatorScope{Preset: roleassign.ScopeDept, Depts: []int64{50}},
	}
	t.Run("非超管只返回能分配的角色", func(t *testing.T) {
		s, mock := newSvc(t)
		expectRoleList(mock)
		expectRoleDetails(mock)
		got, err := s.options(ctx, op)
		if err != nil {
			t.Fatal(err)
		}
		if names(got) != "运营" {
			t.Errorf("options = %q，期望仅「运营」（不可分配/权限超出/范围更宽/内置超管均被过滤）", names(got))
		}
		if err := mock.ExpectationsWereMet(); err != nil {
			t.Error(err)
		}
	})
	t.Run("范围更宽的操作者能看到更多", func(t *testing.T) {
		s, mock := newSvc(t)
		expectRoleList(mock)
		expectRoleDetails(mock)
		wide := &roleassign.Operator{
			Perms: []string{"user:list", "role:assign"},
			Scope: roleassign.OperatorScope{Preset: roleassign.ScopeDeptAndChild, Depts: []int64{50, 51}},
		}
		got, err := s.options(ctx, wide)
		if err != nil {
			t.Fatal(err)
		}
		if names(got) != "运营,权限超出,范围更宽" {
			t.Errorf("options = %q", names(got))
		}
	})
	t.Run("超管返回全部，不做三道校验", func(t *testing.T) {
		s, mock := newSvc(t)
		expectRoleList(mock)
		got, err := s.options(ctx, &roleassign.Operator{Super: true})
		if err != nil {
			t.Fatal(err)
		}
		if len(got) != 5 {
			t.Errorf("超管应看到 5 个角色，得到 %q", names(got))
		}
		if err := mock.ExpectationsWereMet(); err != nil {
			t.Error(err)
		}
	})
	t.Run("nil 操作者（开放平台应用）返回全部", func(t *testing.T) {
		s, mock := newSvc(t)
		expectRoleList(mock)
		got, err := s.options(ctx, nil)
		if err != nil || len(got) != 5 {
			t.Fatalf("got=%q err=%v", names(got), err)
		}
	})
}

func TestRoleDTOAssignable(t *testing.T) {
	r := &Role{Assignable: true}
	if !toDTO(r, nil).Assignable {
		t.Error("DTO 应带出 assignable")
	}
	if toDTO(&Role{}, nil).Assignable {
		t.Error("assignable=false 应原样输出")
	}
	in := roleInput{}
	if in.Assignable != nil {
		t.Error("缺省应为 nil（新建按 true，修改不变）")
	}
}

func TestFactoryRolesAssignable(t *testing.T) {
	seen := map[string]bool{}
	for _, r := range factoryRoles {
		seen[r.code] = r.assignable
		if r.builtin && r.assignable {
			t.Errorf("内置角色 %s 必须不可分配", r.code)
		}
	}
	if seen["admin"] || seen["secadmin"] || !seen["operator"] {
		t.Errorf("种子 assignable 不符合预期：%v", seen)
	}
}

// 迁移文件：三份引擎都存在，只含 DDL（加列），不含 DML。
func TestAssignableMigrationFiles(t *testing.T) {
	dml := regexp.MustCompile(`(?i)\b(INSERT|UPDATE|DELETE|REPLACE|TRUNCATE)\b`)
	for _, engine := range []string{"mysql", "mariadb", "postgres"} {
		p := filepath.Join("..", "..", "..", "migrations", "framework", engine, "system_role_002_assignable.sql")
		b, err := os.ReadFile(p)
		if err != nil {
			t.Fatalf("%s 缺少迁移文件：%v", engine, err)
		}
		var sql []string
		for _, line := range strings.Split(string(b), "\n") {
			if l := strings.TrimSpace(line); l != "" && !strings.HasPrefix(l, "--") {
				sql = append(sql, l)
			}
		}
		body := strings.Join(sql, "\n")
		if !regexp.MustCompile(`(?i)^ALTER TABLE sys_role ADD COLUMN assignable \w+(\(1\))? NOT NULL DEFAULT (1|TRUE);$`).MatchString(body) {
			t.Errorf("%s 迁移内容不符合预期：%q", engine, body)
		}
		if dml.MatchString(body) {
			t.Errorf("%s 迁移含 DML：%q", engine, body)
		}
	}
}
