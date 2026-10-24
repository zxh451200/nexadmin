package user

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"go.uber.org/zap"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"

	"nexadmin/core/cache"
	"nexadmin/core/config"
	"nexadmin/core/response"
	"nexadmin/core/tenant"
	"nexadmin/pkg/idgen"
)

func newScopeSvc(t *testing.T) (*service, sqlmock.Sqlmock, context.Context) {
	t.Helper()
	if err := idgen.Init(1); err != nil {
		t.Fatal(err)
	}
	sqlDB, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	db, err := gorm.Open(mysql.New(mysql.Config{Conn: sqlDB, SkipInitializeWithVersion: true}), &gorm.Config{DisableAutomaticPing: true, Logger: nil})
	if err != nil {
		t.Fatal(err)
	}
	ch, err := cache.New(config.Cache{Driver: "memory"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ch.Close() })
	_ = zap.NewNop()
	return &service{db: db, cache: ch}, mock, tenant.With(context.Background(), 1)
}

const operatorID = idgen.ID(100)

// expectScope 依次预期 scopeCond 的查询：角色 → 自己的部门 →（deptAndChild 时）部门路径。
func expectScope(mock sqlmock.Sqlmock, scope string) {
	mock.ExpectQuery("FROM sys_user_role AS ur").
		WillReturnRows(sqlmock.NewRows([]string{"id", "data_scope"}).AddRow(7, scope))
	mock.ExpectQuery("SELECT `dept_id` FROM `sys_user`").
		WillReturnRows(sqlmock.NewRows([]string{"dept_id"}).AddRow(50))
	if scope == "deptAndChild" {
		mock.ExpectQuery("FROM `sys_dept`").WillReturnRows(sqlmock.NewRows([]string{"path"}).AddRow("/1/50/"))
	}
}

// expectCount 预期范围内计数查询，返回 n 个命中。
func expectCount(mock sqlmock.Sqlmock, n int) {
	mock.ExpectQuery("SELECT count\\(\\*\\) FROM `sys_user` WHERE .*id IN").
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(n))
}

func isForbidden(err error) (*response.Error, bool) {
	var re *response.Error
	if errors.As(err, &re) && re.Status == http.StatusForbidden {
		return re, true
	}
	return nil, false
}

func TestEnsureInScope(t *testing.T) {
	me := actor{ID: operatorID}
	cases := []struct {
		name   string
		scope  string
		ids    []idgen.ID
		hits   int
		denied bool
		msgHas string
	}{
		{"dept 范围内", "dept", []idgen.ID{1}, 1, false, ""},
		{"dept 范围外", "dept", []idgen.ID{1}, 0, true, "超出数据范围"},
		{"deptAndChild 范围内", "deptAndChild", []idgen.ID{1}, 1, false, ""},
		{"deptAndChild 范围外", "deptAndChild", []idgen.ID{1}, 0, true, ""},
		{"custom 范围内", "custom", []idgen.ID{1}, 1, false, ""},
		{"custom 范围外", "custom", []idgen.ID{1}, 0, true, ""},
		{"self 仅本人：他人被拒", "self", []idgen.ID{1}, 0, true, ""},
		{"批量全部在范围内", "dept", []idgen.ID{1, 2, 3}, 3, false, ""},
		{"批量有越权整批拒绝并给出数量", "dept", []idgen.ID{1, 2, 3}, 1, true, "2 个"},
		{"批量重复 id 去重后计数", "dept", []idgen.ID{1, 1, 2}, 2, false, ""},
		{"不存在的 id 与越权同样 403", "dept", []idgen.ID{999}, 0, true, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s, mock, ctx := newScopeSvc(t)
			expectScope(mock, tc.scope)
			expectCount(mock, tc.hits)
			err := s.ensureInScope(ctx, me, tc.ids...)
			re, forbidden := isForbidden(err)
			if tc.denied != forbidden || (!tc.denied && err != nil) {
				t.Fatalf("denied=%v 期望 %v，err=%v", forbidden, tc.denied, err)
			}
			if tc.msgHas != "" && !strings.Contains(re.Msg, tc.msgHas) {
				t.Errorf("提示 %q 应包含 %q", re.Msg, tc.msgHas)
			}
			if err := mock.ExpectationsWereMet(); err != nil {
				t.Error(err)
			}
		})
	}
}

func TestEnsureInScopeExempt(t *testing.T) {
	t.Run("超管不查询", func(t *testing.T) {
		s, mock, ctx := newScopeSvc(t)
		if err := s.ensureInScope(ctx, actor{ID: operatorID, Super: true}, 1, 2); err != nil {
			t.Fatal(err)
		}
		if err := mock.ExpectationsWereMet(); err != nil {
			t.Error(err)
		}
	})
	t.Run("自改不查询", func(t *testing.T) {
		s, mock, ctx := newScopeSvc(t)
		if err := s.ensureInScope(ctx, actor{ID: operatorID}, operatorID); err != nil {
			t.Fatal(err)
		}
		if err := mock.ExpectationsWereMet(); err != nil {
			t.Error(err)
		}
	})
	t.Run("范围=全部 只查角色", func(t *testing.T) {
		s, mock, ctx := newScopeSvc(t)
		mock.ExpectQuery("FROM sys_user_role AS ur").
			WillReturnRows(sqlmock.NewRows([]string{"id", "data_scope"}).AddRow(7, "all"))
		mock.ExpectQuery("SELECT `dept_id` FROM `sys_user`").WillReturnRows(sqlmock.NewRows([]string{"dept_id"}).AddRow(50))
		if err := s.ensureInScope(ctx, actor{ID: operatorID}, 1, 2); err != nil {
			t.Fatal(err)
		}
		if err := mock.ExpectationsWereMet(); err != nil {
			t.Error(err)
		}
	})
}

// 越权时各写接口必须在任何写入/读取目标之前返回 403：mock 只预期范围查询，多出的 SQL 会让测试失败。
func TestWritesRejectedOutOfScope(t *testing.T) {
	me := actor{ID: operatorID}
	target := idgen.ID(1)
	ops := map[string]func(s *service, ctx context.Context) error{
		"update": func(s *service, ctx context.Context) error {
			_, err := s.update(ctx, target, userInput{Username: "abc", Status: statusOn}, me)
			return err
		},
		"remove":        func(s *service, ctx context.Context) error { return s.remove(ctx, []idgen.ID{target}, me) },
		"removeMany":    func(s *service, ctx context.Context) error { return s.remove(ctx, []idgen.ID{1, 2, 3}, me) },
		"setStatus":     func(s *service, ctx context.Context) error { return s.setStatus(ctx, target, statusOff, me) },
		"resetPassword": func(s *service, ctx context.Context) error { return s.resetPassword(ctx, target, "Passw0rd!x", me) },
		"unlock":        func(s *service, ctx context.Context) error { return s.unlock(ctx, target, me) },
		"assignRoles": func(s *service, ctx context.Context) error { // 分配角色走 PUT /users/:id 的 roleIds
			_, err := s.update(ctx, target, userInput{Username: "abc", RoleIDs: []string{"5"}}, me)
			return err
		},
	}
	for name, op := range ops {
		for _, scope := range []string{"dept", "deptAndChild", "custom", "self"} {
			t.Run(name+"/"+scope, func(t *testing.T) {
				s, mock, ctx := newScopeSvc(t)
				expectScope(mock, scope)
				expectCount(mock, 0)
				if _, ok := isForbidden(op(s, ctx)); !ok {
					t.Fatal("期望 403")
				}
				if err := mock.ExpectationsWereMet(); err != nil {
					t.Error(err)
				}
			})
		}
	}
}

func TestWritesInScopePass(t *testing.T) {
	me := actor{ID: operatorID}
	t.Run("setStatus", func(t *testing.T) {
		s, mock, ctx := newScopeSvc(t)
		expectScope(mock, "dept")
		expectCount(mock, 1)
		mock.ExpectQuery("FROM `sys_user` WHERE .*id = ").WillReturnRows(userRows(statusOn, 1))
		mock.ExpectQuery("SELECT count\\(\\*\\) FROM sys_user_role AS ur").WillReturnRows(sqlmock.NewRows([]string{"c"}).AddRow(0)) // guardTarget
		mock.ExpectBegin()
		mock.ExpectExec("UPDATE `sys_user`").WillReturnResult(sqlmock.NewResult(0, 1))
		mock.ExpectCommit()
		if err := s.setStatus(ctx, 1, statusOff, me); err != nil {
			t.Fatal(err)
		}
		if err := mock.ExpectationsWereMet(); err != nil {
			t.Error(err)
		}
	})
	t.Run("超管批量删除不受限", func(t *testing.T) {
		s, mock, ctx := newScopeSvc(t)
		mock.ExpectQuery("SELECT count\\(\\*\\) FROM `sys_user`").WillReturnRows(sqlmock.NewRows([]string{"c"}).AddRow(0))
		mock.ExpectBegin()
		mock.ExpectQuery("SELECT \\* FROM `sys_user` WHERE id IN").WillReturnRows(userRows(statusOn, 1, 2))
		mock.ExpectExec("UPDATE `sys_user`").WillReturnResult(sqlmock.NewResult(0, 2))
		mock.ExpectExec("INSERT INTO `sys_recycle`").WillReturnResult(sqlmock.NewResult(0, 1))
		mock.ExpectExec("INSERT INTO `sys_recycle`").WillReturnResult(sqlmock.NewResult(0, 1))
		mock.ExpectCommit()
		if err := s.remove(ctx, []idgen.ID{1, 2}, actor{ID: operatorID, Super: true}); err != nil {
			t.Fatal(err)
		}
		if err := mock.ExpectationsWereMet(); err != nil {
			t.Error(err)
		}
	})
}
