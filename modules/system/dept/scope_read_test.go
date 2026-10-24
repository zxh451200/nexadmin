package dept

import (
	"context"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"

	"nexadmin/core/datascope"
)

func expectAll(mock sqlmock.Sqlmock) {
	// 1 总部 -> 50 研发(范围内) -> 51、52；60 是另一条支线 1 -> 60
	mock.ExpectQuery("SELECT \\* FROM `sys_dept`").WillReturnRows(
		sqlmock.NewRows([]string{"id", "parent_id", "path"}).
			AddRow(1, nil, "/1/").
			AddRow(50, 1, "/1/50/").
			AddRow(51, 50, "/1/50/51/").
			AddRow(52, 50, "/1/50/52/").
			AddRow(60, 1, "/1/60/").
			AddRow(61, 60, "/1/60/61/"))
}

func TestTreeScoped(t *testing.T) {
	ctx := context.Background()
	t.Run("受限：保留范围内部门与祖先，祖先只读，范围外分支被裁掉", func(t *testing.T) {
		s, mock := newSvc(t)
		expectAll(mock)
		tree, err := s.TreeScoped(ctx, limited) // limited = 50、51、52
		if err != nil {
			t.Fatal(err)
		}
		if len(tree) != 1 || tree[0].ID != 1 || !tree[0].Readonly {
			t.Fatalf("根应为只读祖先 1：%+v", tree)
		}
		if len(tree[0].Children) != 1 || tree[0].Children[0].ID != 50 || tree[0].Children[0].Readonly {
			t.Fatalf("应只剩可操作的 50（60 分支被裁）：%+v", tree[0].Children)
		}
		if got := len(tree[0].Children[0].Children); got != 2 {
			t.Fatalf("50 下应有 51、52，得到 %d", got)
		}
		for _, c := range tree[0].Children[0].Children {
			if c.Readonly {
				t.Errorf("范围内节点 %d 不应只读", c.ID)
			}
		}
	})
	t.Run("受限且无任何部门（仅本人）：空树而非 null", func(t *testing.T) {
		s, mock := newSvc(t)
		expectAll(mock)
		tree, err := s.TreeScoped(ctx, &datascope.Scope{UserID: 100})
		if err != nil {
			t.Fatal(err)
		}
		if tree == nil || len(tree) != 0 {
			t.Fatalf("期望空切片：%#v", tree)
		}
	})
	t.Run("范围=全部/nil：完整树且无只读标记", func(t *testing.T) {
		for _, sc := range []*datascope.Scope{all, nil} {
			s, mock := newSvc(t)
			expectAll(mock)
			tree, err := s.TreeScoped(ctx, sc)
			if err != nil {
				t.Fatal(err)
			}
			if len(tree) != 1 || len(tree[0].Children) != 2 || tree[0].Readonly {
				t.Fatalf("应为完整树：%+v", tree)
			}
		}
	})
}

func TestGetScoped(t *testing.T) {
	ctx := context.Background()
	t.Run("受限：范围外（含只读祖先）403 且不查库", func(t *testing.T) {
		s, mock := newSvc(t)
		for _, id := range []int64{60, 1} {
			_, err := s.GetScoped(ctx, limited, *pid(id))
			if _, ok := isForbidden(err); !ok {
				t.Fatalf("部门 %d 期望 403", id)
			}
		}
		if err := mock.ExpectationsWereMet(); err != nil {
			t.Error(err)
		}
	})
	t.Run("受限：范围内放行", func(t *testing.T) {
		s, mock := newSvc(t)
		expectDept(mock, 51, nil, "/1/50/51/")
		d, err := s.GetScoped(ctx, limited, 51)
		if err != nil || d.ID != 51 {
			t.Fatalf("d=%v err=%v", d, err)
		}
	})
	t.Run("不受限：不存在仍是 404", func(t *testing.T) {
		s, mock := newSvc(t)
		mock.ExpectQuery("SELECT \\* FROM `sys_dept`").WillReturnRows(sqlmock.NewRows([]string{"id"}))
		_, err := s.GetScoped(ctx, nil, 77)
		if err == nil || isNotFoundErr(err) == false {
			t.Fatalf("期望 404：%v", err)
		}
	})
}
