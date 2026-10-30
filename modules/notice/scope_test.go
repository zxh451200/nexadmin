package notice

import (
	"context"
	"net/http"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"

	"nexadmin/core/datascope"
	"nexadmin/core/response"
	"nexadmin/pkg/idgen"
)

func newScopeSvc(t *testing.T) (*Service, sqlmock.Sqlmock) {
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
	return NewService(db), mock
}

var (
	limited = &datascope.Scope{UserID: 100, Depts: []int64{50}}
	all     = &datascope.Scope{All: true}
)

const byCreator = "created_by = \\? OR created_by IN \\(SELECT id FROM sys_user"

func forbiddenErr(err error) (*response.Error, bool) {
	re, ok := err.(*response.Error)
	return re, ok && re.Status == http.StatusForbidden
}

func TestNoticeCheckIDs(t *testing.T) {
	ctx := context.Background()
	cnt := func(n int) *sqlmock.Rows { return sqlmock.NewRows([]string{"c"}).AddRow(n) }
	t.Run("范围内放行（单个/重复 id 去重）", func(t *testing.T) {
		s, mock := newScopeSvc(t)
		mock.ExpectQuery("SELECT count\\(\\*\\) FROM `sys_notice` WHERE .*" + byCreator).WillReturnRows(cnt(2))
		if err := s.CheckIDs(ctx, limited, 1, 2, 2); err != nil {
			t.Fatal(err)
		}
		if err := mock.ExpectationsWereMet(); err != nil {
			t.Error(err)
		}
	})
	t.Run("范围外拒绝", func(t *testing.T) {
		s, mock := newScopeSvc(t)
		mock.ExpectQuery("SELECT count\\(\\*\\) FROM `sys_notice`").WillReturnRows(cnt(0))
		if _, ok := forbiddenErr(s.CheckIDs(ctx, limited, 1)); !ok {
			t.Fatal("期望 403")
		}
	})
	t.Run("批量含越权整批拒绝并只报数量", func(t *testing.T) {
		s, mock := newScopeSvc(t)
		mock.ExpectQuery("SELECT count\\(\\*\\) FROM `sys_notice`").WillReturnRows(cnt(1))
		re, ok := forbiddenErr(s.CheckIDs(ctx, limited, idgen.ID(901), idgen.ID(902), idgen.ID(903)))
		if !ok || re.Key != "datascope.forbidden_batch" || len(re.Args) != 2 || re.Args[1] != 2 {
			t.Fatalf("提示应报数量且不含 id：ok=%v key=%q args=%v", ok, re.Key, re.Args)
		}
	})
	t.Run("超管/全部豁免不查询", func(t *testing.T) {
		s, mock := newScopeSvc(t)
		if err := s.CheckIDs(ctx, all, 1, 2); err != nil {
			t.Fatal(err)
		}
		if err := mock.ExpectationsWereMet(); err != nil {
			t.Error(err)
		}
	})
}

func TestNoticeListAppliesScope(t *testing.T) {
	ctx := context.Background()
	t.Run("受限：按创建者部门裁剪", func(t *testing.T) {
		s, mock := newScopeSvc(t)
		mock.ExpectQuery("SELECT count\\(\\*\\) FROM `sys_notice` WHERE .*" + byCreator).WillReturnRows(sqlmock.NewRows([]string{"c"}).AddRow(0))
		mock.ExpectQuery("FROM `sys_notice` WHERE .*" + byCreator).WillReturnRows(sqlmock.NewRows([]string{"id"}))
		if _, _, err := s.List(ctx, ListQuery{Page: 1, PageSize: 10, Scope: limited}); err != nil {
			t.Fatal(err)
		}
		if err := mock.ExpectationsWereMet(); err != nil {
			t.Error(err)
		}
	})
	t.Run("全部：不加条件", func(t *testing.T) {
		s, mock := newScopeSvc(t)
		mock.ExpectQuery("SELECT count\\(\\*\\) FROM `sys_notice`").WillReturnRows(sqlmock.NewRows([]string{"c"}).AddRow(0))
		mock.ExpectQuery("FROM `sys_notice`").WillReturnRows(sqlmock.NewRows([]string{"id"}))
		if _, _, err := s.List(ctx, ListQuery{Page: 1, PageSize: 10, Scope: all}); err != nil {
			t.Fatal(err)
		}
		if err := mock.ExpectationsWereMet(); err != nil {
			t.Error(err)
		}
	})
}
