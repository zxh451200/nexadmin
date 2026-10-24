package dept

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
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
	return NewService(db), mock
}

func isForbidden(err error) (*response.Error, bool) {
	re, ok := err.(*response.Error)
	return re, ok && re.Status == http.StatusForbidden
}

func pid(v int64) *idgen.ID { id := idgen.ID(v); return &id }

// 范围：本人所在部门 50 及其下级 51、52；60 是别的部门。
var limited = &datascope.Scope{UserID: 100, Depts: []int64{50, 51, 52}}
var all = &datascope.Scope{All: true}

func expectDept(mock sqlmock.Sqlmock, id int64, parent *int64, path string) {
	mock.ExpectQuery("SELECT \\* FROM `sys_dept`").
		WillReturnRows(sqlmock.NewRows([]string{"id", "parent_id", "path"}).AddRow(id, parent, path))
}

func expectSubtree(mock sqlmock.Sqlmock, ids ...int64) {
	rows := sqlmock.NewRows([]string{"id"})
	for _, id := range ids {
		rows.AddRow(id)
	}
	mock.ExpectQuery("SELECT `id` FROM `sys_dept` WHERE path LIKE").WillReturnRows(rows)
}

func TestCheckCreate(t *testing.T) {
	s, _ := newSvc(t)
	ctx := context.Background()
	for _, tc := range []struct {
		name string
		sc   *datascope.Scope
		in   Input
		deny bool
	}{
		{"范围内父部门放行", limited, Input{ParentID: pid(51)}, false},
		{"范围外父部门拒绝", limited, Input{ParentID: pid(60)}, true},
		{"受限操作者不能建根部门", limited, Input{}, true},
		{"范围=全部可建根部门", all, Input{}, false},
		{"超管（All）豁免", all, Input{ParentID: pid(60)}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, forbidden := isForbidden(s.CheckCreate(ctx, tc.sc, tc.in))
			if forbidden != tc.deny {
				t.Fatalf("拒绝=%v，期望 %v", forbidden, tc.deny)
			}
		})
	}
}

func TestCheckUpdate(t *testing.T) {
	ctx := context.Background()
	p50 := int64(50)
	t.Run("范围外部门拒绝", func(t *testing.T) {
		s, mock := newSvc(t)
		if _, ok := isForbidden(s.CheckUpdate(ctx, limited, 60, Input{ParentID: pid(1)})); !ok {
			t.Fatal("期望 403")
		}
		if err := mock.ExpectationsWereMet(); err != nil {
			t.Error(err)
		}
	})
	t.Run("范围内、不改上级放行", func(t *testing.T) {
		s, mock := newSvc(t)
		expectDept(mock, 51, &p50, "/1/50/51/")
		if err := s.CheckUpdate(ctx, limited, 51, Input{ParentID: pid(50)}); err != nil {
			t.Fatal(err)
		}
		if err := mock.ExpectationsWereMet(); err != nil {
			t.Error(err)
		}
	})
	t.Run("移动到范围外的上级被拒", func(t *testing.T) {
		s, mock := newSvc(t)
		expectDept(mock, 51, &p50, "/1/50/51/")
		if _, ok := isForbidden(s.CheckUpdate(ctx, limited, 51, Input{ParentID: pid(60)})); !ok {
			t.Fatal("期望 403")
		}
	})
	t.Run("受限操作者不能把部门移到根", func(t *testing.T) {
		s, mock := newSvc(t)
		expectDept(mock, 51, &p50, "/1/50/51/")
		if _, ok := isForbidden(s.CheckUpdate(ctx, limited, 51, Input{})); !ok {
			t.Fatal("期望 403")
		}
	})
	t.Run("移动到范围内上级、整棵子树在范围内放行", func(t *testing.T) {
		s, mock := newSvc(t)
		expectDept(mock, 51, &p50, "/1/50/51/")
		expectDept(mock, 51, &p50, "/1/50/51/")
		expectSubtree(mock, 51, 52)
		if err := s.CheckUpdate(ctx, limited, 51, Input{ParentID: pid(52)}); err != nil {
			t.Fatal(err)
		}
		if err := mock.ExpectationsWereMet(); err != nil {
			t.Error(err)
		}
	})
	t.Run("移动的子树含范围外部门整体拒绝", func(t *testing.T) {
		s, mock := newSvc(t)
		expectDept(mock, 51, &p50, "/1/50/51/")
		expectDept(mock, 51, &p50, "/1/50/51/")
		expectSubtree(mock, 51, 99)
		re, ok := isForbidden(s.CheckUpdate(ctx, limited, 51, Input{ParentID: pid(52)}))
		if !ok {
			t.Fatal("期望 403")
		}
		if strings.Contains(re.Msg, "99") {
			t.Errorf("提示不应泄露具体 id：%q", re.Msg)
		}
	})
	t.Run("超管豁免", func(t *testing.T) {
		s, mock := newSvc(t)
		if err := s.CheckUpdate(ctx, all, 60, Input{ParentID: pid(1)}); err != nil {
			t.Fatal(err)
		}
		if err := mock.ExpectationsWereMet(); err != nil {
			t.Error(err)
		}
	})
}

func TestCheckDelete(t *testing.T) {
	ctx := context.Background()
	p50 := int64(50)
	t.Run("整棵子树在范围内放行", func(t *testing.T) {
		s, mock := newSvc(t)
		expectDept(mock, 51, &p50, "/1/50/51/")
		expectSubtree(mock, 51, 52)
		if err := s.CheckDelete(ctx, limited, 51); err != nil {
			t.Fatal(err)
		}
		if err := mock.ExpectationsWereMet(); err != nil {
			t.Error(err)
		}
	})
	t.Run("子树含范围外部门整体拒绝并只报数量", func(t *testing.T) {
		s, mock := newSvc(t)
		expectDept(mock, 50, nil, "/1/50/")
		expectSubtree(mock, 50, 51, 98, 99)
		re, ok := isForbidden(s.CheckDelete(ctx, limited, 50))
		if !ok {
			t.Fatal("期望 403")
		}
		if !strings.Contains(re.Msg, "2 个") {
			t.Errorf("应报告数量 2：%q", re.Msg)
		}
	})
	t.Run("范围外部门拒绝", func(t *testing.T) {
		s, mock := newSvc(t)
		expectDept(mock, 60, nil, "/60/")
		expectSubtree(mock, 60)
		if _, ok := isForbidden(s.CheckDelete(ctx, limited, 60)); !ok {
			t.Fatal("期望 403")
		}
	})
	t.Run("不存在的部门对受限操作者同样 403", func(t *testing.T) {
		s, mock := newSvc(t)
		mock.ExpectQuery("SELECT \\* FROM `sys_dept`").WillReturnRows(sqlmock.NewRows([]string{"id", "parent_id", "path"}))
		if _, ok := isForbidden(s.CheckDelete(ctx, limited, 51)); !ok {
			t.Fatal("期望 403")
		}
	})
	t.Run("超管豁免", func(t *testing.T) {
		s, _ := newSvc(t)
		if err := s.CheckDelete(ctx, all, 60); err != nil {
			t.Fatal(err)
		}
	})
}

func TestCheckSort(t *testing.T) {
	s, _ := newSvc(t)
	ctx := context.Background()
	for _, tc := range []struct {
		name string
		sc   *datascope.Scope
		in   SortInput
		deny bool
	}{
		{"全部在范围内放行", limited, SortInput{ParentID: pid(50), IDs: []idgen.ID{51, 52}}, false},
		{"含范围外 id 拒绝", limited, SortInput{ParentID: pid(50), IDs: []idgen.ID{51, 60}}, true},
		{"父节点在范围外拒绝", limited, SortInput{ParentID: pid(60), IDs: []idgen.ID{51}}, true},
		{"根级排序对受限操作者拒绝", limited, SortInput{IDs: []idgen.ID{50}}, true},
		{"超管豁免", all, SortInput{IDs: []idgen.ID{60, 61}}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, forbidden := isForbidden(s.CheckSort(ctx, tc.sc, tc.in)); forbidden != tc.deny {
				t.Fatalf("拒绝=%v，期望 %v", forbidden, tc.deny)
			}
		})
	}
}
