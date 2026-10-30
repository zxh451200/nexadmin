package log

import (
	"context"
	"net/http"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"go.uber.org/zap"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"

	"nexadmin/core/datascope"
	"nexadmin/core/response"
	"nexadmin/pkg/idgen"
)

func newSvc(t *testing.T) (*Service, sqlmock.Sqlmock) {
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
	return NewService(db, "UTC", zap.NewNop()), mock
}

func countRows(n int) *sqlmock.Rows { return sqlmock.NewRows([]string{"c"}).AddRow(n) }

func isForbidden(err error) (*response.Error, bool) {
	re, ok := err.(*response.Error)
	return re, ok && re.Status == http.StatusForbidden
}

var (
	limited = &datascope.Scope{UserID: 100, Depts: []int64{50}}
	all     = &datascope.Scope{All: true}
	ctx     = context.Background()
)

const inScopeSQL = "operator IN \\(SELECT username FROM sys_user"

func TestListOperAppliesScope(t *testing.T) {
	t.Run("受限操作者：查询带归属条件", func(t *testing.T) {
		s, mock := newSvc(t)
		mock.ExpectQuery("SELECT count\\(\\*\\) FROM `sys_oper_log` WHERE .*" + inScopeSQL).WillReturnRows(countRows(0))
		mock.ExpectQuery("FROM `sys_oper_log` WHERE .*" + inScopeSQL).WillReturnRows(sqlmock.NewRows([]string{"id"}))
		if _, _, err := s.ListOper(ctx, Query{Page: 1, PageSize: 10, Scope: limited}); err != nil {
			t.Fatal(err)
		}
		if err := mock.ExpectationsWereMet(); err != nil {
			t.Error(err)
		}
	})
	t.Run("超管/全部：不带归属条件", func(t *testing.T) {
		s, mock := newSvc(t)
		mock.ExpectQuery("SELECT count\\(\\*\\) FROM `sys_oper_log`").WillReturnRows(countRows(0))
		mock.ExpectQuery("FROM `sys_oper_log`").WillReturnRows(sqlmock.NewRows([]string{"id"}))
		if _, _, err := s.ListOper(ctx, Query{Page: 1, PageSize: 10, Scope: all}); err != nil {
			t.Fatal(err)
		}
		if err := mock.ExpectationsWereMet(); err != nil {
			t.Error(err)
		}
	})
}

func TestListLoginAppliesScope(t *testing.T) {
	s, mock := newSvc(t)
	mock.ExpectQuery("SELECT count\\(\\*\\) FROM `sys_login_log` WHERE .*username IN \\(SELECT username FROM sys_user").WillReturnRows(countRows(0))
	mock.ExpectQuery("FROM `sys_login_log` WHERE .*username IN \\(SELECT username FROM sys_user").WillReturnRows(sqlmock.NewRows([]string{"id"}))
	if _, _, err := s.ListLogin(ctx, Query{Page: 1, PageSize: 10, Scope: limited}); err != nil {
		t.Fatal(err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Error(err)
	}
}

func TestCheckIDs(t *testing.T) {
	t.Run("oper 全部在范围内放行", func(t *testing.T) {
		s, mock := newSvc(t)
		mock.ExpectQuery("SELECT count\\(\\*\\) FROM `sys_oper_log` WHERE .*" + inScopeSQL).WillReturnRows(countRows(2))
		if err := s.CheckOperIDs(ctx, limited, []idgen.ID{1, 2, 2}); err != nil {
			t.Fatal(err)
		}
	})
	t.Run("oper 含范围外 id 整批拒绝并只报数量", func(t *testing.T) {
		s, mock := newSvc(t)
		mock.ExpectQuery("SELECT count\\(\\*\\) FROM `sys_oper_log`").WillReturnRows(countRows(1))
		re, ok := isForbidden(s.CheckOperIDs(ctx, limited, []idgen.ID{111, 222, 333}))
		if !ok {
			t.Fatal("期望 403")
		}
		if re.Key != "datascope.forbidden_batch" || len(re.Args) != 2 || re.Args[1] != 2 {
			t.Errorf("提示应报数量且不含 id：key=%q args=%v", re.Key, re.Args)
		}
	})
	t.Run("login 范围外拒绝", func(t *testing.T) {
		s, mock := newSvc(t)
		mock.ExpectQuery("SELECT count\\(\\*\\) FROM `sys_login_log`").WillReturnRows(countRows(0))
		if _, ok := isForbidden(s.CheckLoginIDs(ctx, limited, []idgen.ID{1})); !ok {
			t.Fatal("期望 403")
		}
	})
	t.Run("login 范围内放行", func(t *testing.T) {
		s, mock := newSvc(t)
		mock.ExpectQuery("SELECT count\\(\\*\\) FROM `sys_login_log`").WillReturnRows(countRows(1))
		if err := s.CheckLoginIDs(ctx, limited, []idgen.ID{1}); err != nil {
			t.Fatal(err)
		}
	})
	t.Run("超管豁免不查询", func(t *testing.T) {
		s, mock := newSvc(t)
		if err := s.CheckOperIDs(ctx, all, []idgen.ID{1}); err != nil {
			t.Fatal(err)
		}
		if err := s.CheckLoginIDs(ctx, all, []idgen.ID{1}); err != nil {
			t.Fatal(err)
		}
		if err := mock.ExpectationsWereMet(); err != nil {
			t.Error(err)
		}
	})
}

func TestCheckCleanOper(t *testing.T) {
	t.Run("待清理日志全在范围内放行", func(t *testing.T) {
		s, mock := newSvc(t)
		mock.ExpectQuery("SELECT count\\(\\*\\) FROM `sys_oper_log` WHERE .*NOT \\(").WillReturnRows(countRows(0))
		if err := s.CheckCleanOper(ctx, limited, 30); err != nil {
			t.Fatal(err)
		}
	})
	t.Run("含范围外日志整批拒绝并报数量", func(t *testing.T) {
		s, mock := newSvc(t)
		mock.ExpectQuery("SELECT count\\(\\*\\) FROM `sys_oper_log` WHERE .*NOT \\(").WillReturnRows(countRows(7))
		re, ok := isForbidden(s.CheckCleanOper(ctx, limited, 0))
		if !ok || re.Key != "datascope.forbidden_batch" || len(re.Args) != 2 || re.Args[1] != 7 {
			t.Fatalf("期望 403 且报 7：%v key=%q args=%v", ok, re.Key, re.Args)
		}
	})
	t.Run("超管豁免", func(t *testing.T) {
		s, _ := newSvc(t)
		if err := s.CheckCleanOper(ctx, all, 0); err != nil {
			t.Fatal(err)
		}
	})
}
