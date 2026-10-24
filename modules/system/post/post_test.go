package post

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/gin-gonic/gin"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"

	"nexadmin/core/response"
	"nexadmin/core/tenant"
	"nexadmin/pkg/idgen"
)

func setup(t *testing.T) (*Service, sqlmock.Sqlmock) {
	t.Helper()
	if err := idgen.Init(1); err != nil {
		t.Fatal(err)
	}
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

var ctx = tenant.With(context.Background(), 3)

func statusOf(err error) int {
	var re *response.Error
	if errors.As(err, &re) {
		return re.Status
	}
	return 0
}

func count(n int) *sqlmock.Rows { return sqlmock.NewRows([]string{"c"}).AddRow(n) }

func expectCodeCount(mock sqlmock.Sqlmock, code string, exclude int64, n int) {
	mock.ExpectQuery("SELECT count\\(\\*\\) FROM `sys_post` WHERE code = \\? AND id <> \\?").
		WithArgs(code, exclude).WillReturnRows(count(n))
}

func expectGet(mock sqlmock.Sqlmock, id int64, found bool) {
	rows := sqlmock.NewRows([]string{"id", "tenant_id", "name", "code"})
	if found {
		rows.AddRow(id, 3, "n", "c")
	}
	mock.ExpectQuery("FROM `sys_post` WHERE id = \\?").WithArgs(id, 1).WillReturnRows(rows)
}

func TestCreateCodeUnique(t *testing.T) {
	t.Run("编码重复 400 且不写入", func(t *testing.T) {
		s, mock := setup(t)
		expectCodeCount(mock, "dev", 0, 1)
		_, err := s.Create(ctx, Input{Name: "开发", Code: "dev"})
		if statusOf(err) != http.StatusBadRequest {
			t.Fatalf("期望 400，得到 %v", err)
		}
		if err := mock.ExpectationsWereMet(); err != nil {
			t.Error(err)
		}
	})
	t.Run("成功：填入当前租户，状态默认启用，部门 0 视为未关联", func(t *testing.T) {
		s, mock := setup(t)
		expectCodeCount(mock, "dev", 0, 0)
		mock.ExpectBegin()
		mock.ExpectExec("INSERT INTO `sys_post`").WillReturnResult(sqlmock.NewResult(1, 1))
		mock.ExpectCommit()
		zero := idgen.ID(0)
		p, err := s.Create(ctx, Input{Name: "开发", Code: "dev", DeptID: &zero})
		if err != nil {
			t.Fatal(err)
		}
		if p.TenantID != 3 || p.Status != "1" || p.DeptID != nil || p.ID == 0 {
			t.Errorf("tenant=%d status=%q dept=%v id=%d", p.TenantID, p.Status, p.DeptID, p.ID)
		}
		if err := mock.ExpectationsWereMet(); err != nil {
			t.Error(err)
		}
	})
	t.Run("无租户 context 拒绝创建", func(t *testing.T) {
		s, mock := setup(t)
		mock.ExpectQuery("SELECT count").WillReturnRows(count(0))
		mock.ExpectBegin()
		mock.ExpectRollback()
		if _, err := s.Create(context.Background(), Input{Name: "x", Code: "x"}); err == nil {
			t.Fatal("没有租户时不应创建出无主岗位")
		}
	})
}

func TestCreateKeepsDeptAssociation(t *testing.T) {
	s, mock := setup(t)
	expectCodeCount(mock, "pm", 0, 0)
	mock.ExpectBegin()
	mock.ExpectExec("INSERT INTO `sys_post`").WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectCommit()
	dept := idgen.ID(55)
	p, err := s.Create(ctx, Input{Name: "产品", Code: "pm", DeptID: &dept})
	if err != nil {
		t.Fatal(err)
	}
	if p.DeptID == nil || *p.DeptID != 55 {
		t.Errorf("部门关联丢失: %v", p.DeptID)
	}
}

func TestUpdate(t *testing.T) {
	t.Run("不存在 404", func(t *testing.T) {
		s, mock := setup(t)
		expectGet(mock, 8, false)
		if _, err := s.Update(ctx, 8, Input{Name: "x", Code: "x"}); statusOf(err) != http.StatusNotFound {
			t.Fatalf("期望 404，得到 %v", err)
		}
	})
	t.Run("改成他人编码 400（排除自己）", func(t *testing.T) {
		s, mock := setup(t)
		expectGet(mock, 8, true)
		expectCodeCount(mock, "dup", 8, 1)
		if _, err := s.Update(ctx, 8, Input{Name: "x", Code: "dup"}); statusOf(err) != http.StatusBadRequest {
			t.Fatalf("期望 400，得到 %v", err)
		}
		if err := mock.ExpectationsWereMet(); err != nil {
			t.Error(err)
		}
	})
	t.Run("部门传 nil 会清空关联", func(t *testing.T) {
		s, mock := setup(t)
		expectGet(mock, 8, true)
		expectCodeCount(mock, "c", 8, 0)
		mock.ExpectBegin()
		mock.ExpectExec("UPDATE `sys_post` SET").
			WithArgs("c", nil, "n", "", 0, "1", sqlmock.AnyArg(), int64(8)).
			WillReturnResult(sqlmock.NewResult(0, 1))
		mock.ExpectCommit()
		expectGet(mock, 8, true)
		if _, err := s.Update(ctx, 8, Input{Name: "n", Code: "c"}); err != nil {
			t.Fatal(err)
		}
		if err := mock.ExpectationsWereMet(); err != nil {
			t.Error(err)
		}
	})
}

func expectUsers(mock sqlmock.Sqlmock, n int) {
	mock.ExpectQuery("SELECT count\\(\\*\\) FROM sys_user_post AS up JOIN sys_user u ON u.id = up.user_id AND u.deleted_at IS NULL WHERE up.post_id = \\?").
		WithArgs(int64(8)).WillReturnRows(count(n))
}

func TestDeleteRejectedWhenUsersAssigned(t *testing.T) {
	s, mock := setup(t)
	expectGet(mock, 8, true)
	expectUsers(mock, 3) // 之后不预期任何写语句（含 Begin / DELETE）
	err := s.Delete(ctx, 8)
	var re *response.Error
	if !errors.As(err, &re) || re.Status != http.StatusConflict || re.Key != "post.in_use" || len(re.Args) != 1 || re.Args[0] != int64(3) {
		t.Fatalf("期望 409 post.in_use(3)，得到 %#v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Error(err)
	}
}

func TestDelete(t *testing.T) {
	t.Run("不存在 404 且不执行删除", func(t *testing.T) {
		s, mock := setup(t)
		expectGet(mock, 8, false)
		if err := s.Delete(ctx, 8); statusOf(err) != http.StatusNotFound {
			t.Fatalf("期望 404，得到 %v", err)
		}
		if err := mock.ExpectationsWereMet(); err != nil {
			t.Error(err)
		}
	})
	t.Run("存在则删除", func(t *testing.T) {
		s, mock := setup(t)
		expectGet(mock, 8, true)
		expectUsers(mock, 0)
		mock.ExpectBegin()
		mock.ExpectExec("DELETE FROM `sys_post` WHERE id = \\?").WithArgs(int64(8)).WillReturnResult(sqlmock.NewResult(0, 1))
		mock.ExpectCommit()
		if err := s.Delete(ctx, 8); err != nil {
			t.Fatal(err)
		}
		if err := mock.ExpectationsWereMet(); err != nil {
			t.Error(err)
		}
	})
}

func TestSortRequiresExactlyAllPosts(t *testing.T) {
	have := func(mock sqlmock.Sqlmock) {
		mock.ExpectQuery("SELECT `id` FROM `sys_post`").WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(1).AddRow(2).AddRow(3))
	}
	for name, ids := range map[string][]idgen.ID{
		"缺少一个":    {1, 2},
		"多出别的租户的": {1, 2, 3, 4},
		"含重复":     {1, 1, 2},
		"含未知":     {1, 2, 9},
	} {
		t.Run(name, func(t *testing.T) {
			s, mock := setup(t)
			mock.ExpectBegin()
			have(mock)
			mock.ExpectRollback()
			if err := s.Sort(ctx, ids); statusOf(err) != http.StatusConflict {
				t.Fatalf("期望 409，得到 %v", err)
			}
			if err := mock.ExpectationsWereMet(); err != nil {
				t.Error(err)
			}
		})
	}
	t.Run("完整排列按 10、20、30 重写", func(t *testing.T) {
		s, mock := setup(t)
		mock.ExpectBegin()
		have(mock)
		for i, id := range []int64{3, 1, 2} {
			mock.ExpectExec("UPDATE `sys_post` SET `sort`=\\?").WithArgs((i+1)*10, sqlmock.AnyArg(), id).WillReturnResult(sqlmock.NewResult(0, 1))
		}
		mock.ExpectCommit()
		if err := s.Sort(ctx, []idgen.ID{3, 1, 2}); err != nil {
			t.Fatal(err)
		}
		if err := mock.ExpectationsWereMet(); err != nil {
			t.Error(err)
		}
	})
}

func TestListAndOptions(t *testing.T) {
	t.Run("关键字转义，匹配名称或编码", func(t *testing.T) {
		s, mock := setup(t)
		mock.ExpectQuery("FROM `sys_post` WHERE LOWER\\(name\\) LIKE \\? OR LOWER\\(code\\) LIKE \\? ORDER BY sort, id").
			WithArgs(`%a\%\_%`, `%a\%\_%`).WillReturnRows(sqlmock.NewRows([]string{"id"}))
		list, err := s.List(ctx, " A%_ ")
		if err != nil || list == nil {
			t.Fatalf("list=%v err=%v", list, err)
		}
		if err := mock.ExpectationsWereMet(); err != nil {
			t.Error(err)
		}
	})
	t.Run("下拉只取启用岗位", func(t *testing.T) {
		s, mock := setup(t)
		mock.ExpectQuery("FROM `sys_post` WHERE status = \\? ORDER BY sort, id").WithArgs("1").
			WillReturnRows(sqlmock.NewRows([]string{"id", "name"}).AddRow(1, "开发").AddRow(2, "测试"))
		got, err := s.Options(ctx)
		if err != nil || len(got) != 2 || got[0].Name != "开发" {
			t.Fatalf("got=%v err=%v", got, err)
		}
	})
}

func TestHandlerValidation(t *testing.T) {
	gin.SetMode(gin.TestMode)
	s, mock := setup(t) // 校验失败不得触达数据库
	h := &Handler{svc: s}
	r := gin.New()
	r.POST("/posts", h.Create)
	r.PUT("/posts/sort", h.Sort)
	r.DELETE("/posts/:id", h.Delete)
	do := func(method, path, body string) int {
		w := httptest.NewRecorder()
		req := httptest.NewRequest(method, path, bytes.NewBufferString(body))
		req.Header.Set("Content-Type", "application/json")
		r.ServeHTTP(w, req)
		return w.Code
	}
	for name, body := range map[string]string{
		"缺名称":  `{"code":"a"}`,
		"缺编码":  `{"name":"a"}`,
		"状态非法": `{"name":"a","code":"c","status":"x"}`,
		"部门非法": `{"name":"a","code":"c","deptId":"abc"}`,
	} {
		if got := do(http.MethodPost, "/posts", body); got != http.StatusBadRequest {
			t.Errorf("%s: 期望 400，得到 %d", name, got)
		}
	}
	if got := do(http.MethodPut, "/posts/sort", `{}`); got != http.StatusBadRequest {
		t.Errorf("排序缺 ids 期望 400，得到 %d", got)
	}
	if got := do(http.MethodDelete, "/posts/abc", ""); got != http.StatusBadRequest {
		t.Errorf("非法 id 期望 400，得到 %d", got)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Error(err)
	}
}
