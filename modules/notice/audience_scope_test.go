package notice

import (
	"context"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"

	"nexadmin/core/datascope"
	"nexadmin/pkg/idgen"
)

func ids(v ...int64) []idgen.ID {
	out := make([]idgen.ID, len(v))
	for i, x := range v {
		out[i] = idgen.ID(x)
	}
	return out
}

func audienceKey(err error) string {
	if re, ok := forbiddenErr(err); ok {
		return re.Key
	}
	return ""
}

func TestCheckAudience(t *testing.T) {
	ctx := context.Background()
	super := &datascope.Scope{All: true, UserID: 1}
	cnt := func(n int) *sqlmock.Rows { return sqlmock.NewRows([]string{"c"}).AddRow(n) }
	userQuery := "SELECT count\\(\\*\\) FROM `sys_user` WHERE .*deleted_at IS NULL.*" + "id = \\? OR id IN \\(SELECT id FROM sys_user"

	t.Run("受限：全员拒绝（带 i18n key）且不查库", func(t *testing.T) {
		s, mock := newScopeSvc(t)
		err := s.CheckAudience(ctx, limited, Form{Scope: ScopeAll})
		if audienceKey(err) != "notice.audience_all_forbidden" {
			t.Fatalf("期望 all_forbidden：%v", err)
		}
		if e := mock.ExpectationsWereMet(); e != nil {
			t.Error(e)
		}
	})
	t.Run("受限：平台公告拒绝", func(t *testing.T) {
		s, _ := newScopeSvc(t)
		err := s.CheckAudience(ctx, limited, Form{Scope: ScopeRole, Platform: true, TargetIDs: ids(1)})
		if audienceKey(err) != "notice.audience_platform_forbidden" {
			t.Fatalf("期望 platform_forbidden：%v", err)
		}
	})
	t.Run("受限：部门全部在范围内放行", func(t *testing.T) {
		s, _ := newScopeSvc(t)
		if err := s.CheckAudience(ctx, limited, Form{Scope: ScopeDept, TargetIDs: ids(50)}); err != nil {
			t.Fatal(err)
		}
	})
	t.Run("受限：部门含范围外拒绝", func(t *testing.T) {
		s, _ := newScopeSvc(t)
		err := s.CheckAudience(ctx, limited, Form{Scope: ScopeDept, TargetIDs: ids(50, 60)})
		if audienceKey(err) != "notice.audience_dept_forbidden" {
			t.Fatalf("期望 dept_forbidden：%v", err)
		}
	})
	t.Run("受限：角色不限制且不查库", func(t *testing.T) {
		s, mock := newScopeSvc(t)
		if err := s.CheckAudience(ctx, limited, Form{Scope: ScopeRole, TargetIDs: ids(7, 8)}); err != nil {
			t.Fatal(err)
		}
		if e := mock.ExpectationsWereMet(); e != nil {
			t.Error(e)
		}
	})
	t.Run("受限：用户全部在范围内放行（重复 id 去重）", func(t *testing.T) {
		s, mock := newScopeSvc(t)
		mock.ExpectQuery(userQuery).WillReturnRows(cnt(2))
		if err := s.CheckAudience(ctx, limited, Form{Scope: ScopeUser, TargetIDs: ids(11, 12, 12)}); err != nil {
			t.Fatal(err)
		}
		if e := mock.ExpectationsWereMet(); e != nil {
			t.Error(e)
		}
	})
	t.Run("受限：用户含范围外/不存在拒绝", func(t *testing.T) {
		s, mock := newScopeSvc(t)
		mock.ExpectQuery(userQuery).WillReturnRows(cnt(1))
		err := s.CheckAudience(ctx, limited, Form{Scope: ScopeUser, TargetIDs: ids(11, 99)})
		if audienceKey(err) != "notice.audience_user_forbidden" {
			t.Fatalf("期望 user_forbidden：%v", err)
		}
	})
	t.Run("受限且无部门（仅本人）：用户条件退化为仅本人", func(t *testing.T) {
		s, mock := newScopeSvc(t)
		mock.ExpectQuery("SELECT count\\(\\*\\) FROM `sys_user` WHERE .*id = \\?").WillReturnRows(cnt(1))
		if err := s.CheckAudience(ctx, &datascope.Scope{UserID: 100}, Form{Scope: ScopeUser, TargetIDs: ids(100)}); err != nil {
			t.Fatal(err)
		}
	})
	t.Run("超管/范围=全部：全员、平台、范围外全部豁免且不查库", func(t *testing.T) {
		for _, sc := range []*datascope.Scope{super, all} {
			s, mock := newScopeSvc(t)
			for _, f := range []Form{
				{Scope: ScopeAll}, {Scope: ScopeAll, Platform: true},
				{Scope: ScopeDept, TargetIDs: ids(60)}, {Scope: ScopeUser, TargetIDs: ids(99)},
			} {
				if err := s.CheckAudience(ctx, sc, f); err != nil {
					t.Fatalf("%+v 应豁免：%v", f, err)
				}
			}
			if e := mock.ExpectationsWereMet(); e != nil {
				t.Error(e)
			}
		}
	})
}
