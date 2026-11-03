package imports

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/gin-gonic/gin"

	"nexadmin/core"
	"nexadmin/core/config"
	"nexadmin/core/response"
	"nexadmin/core/tenant"
	"nexadmin/pkg/idgen"
)

// 批量删台账的三条硬约束都在这里钉住：
//   - 只动点名的 id（查询与 DELETE 都带 id IN，去掉任一条件本用例必红）；
//   - 单条删除的「排队中/执行中不可删」护栏原样生效，且整批回滚（去掉状态判断本用例必红）；
//   - deleted 是数据库实删行数，不是请求条数（改成 len(ids) 本用例必红）。
//
// 租户隔离由 core/database 的 tenantPlugin 在查询/DELETE 前追加 tenant_id 条件——
// 裸 gorm 的 sqlmock 里注册不上该插件（与 cron 同一说明），语句形状与 id 集合在这里钉住，
// 插件本身由 core/database 的用例保证。sqlmock 只做正则匹配、从不执行 SQL，
// 所以「数据库里到底少了哪几行」只能靠 RowsAffected 的桩值来钉，不能靠真删。

// 命中 2 条：SELECT 与 DELETE 都只带这 2 个 id，事务正常提交，返回实删 2。
func TestDeleteJobsBatchDeletesOnlyRequestedIDs(t *testing.T) {
	db, mock := mockDB(t)
	s := NewService(db, nil)
	mock.ExpectBegin()
	mock.ExpectQuery("SELECT \\* FROM `plg_imports_job` WHERE id IN \\(\\?,\\?\\)").
		WithArgs(int64(11), int64(12)).
		WillReturnRows(sqlmock.NewRows([]string{"id", "status"}).
			AddRow(int64(11), StatusDone).AddRow(int64(12), StatusFailed))
	mock.ExpectExec("DELETE FROM `plg_imports_job` WHERE id IN \\(\\?,\\?\\)").
		WithArgs(int64(11), int64(12)).
		WillReturnResult(sqlmock.NewResult(0, 2))
	mock.ExpectCommit()

	n, err := s.DeleteJobs(tenant.With(context.Background(), 1), []idgen.ID{11, 12})
	if err != nil {
		t.Fatalf("DeleteJobs: %v", err)
	}
	if n != 2 {
		t.Errorf("deleted = %d, want 2", n)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Error(err)
	}
}

// 批次里有一条还在跑：整批 409 imports.job_running，事务回滚，一条 DELETE 都不发出。
// 去掉 DeleteJobs 里的状态判断，本用例必红（会先删掉已完成的那条再报错）。
func TestDeleteJobsBatchRejectsWholeBatchWhenOneJobRunning(t *testing.T) {
	db, mock := mockDB(t)
	s := NewService(db, nil)
	mock.ExpectBegin()
	mock.ExpectQuery("SELECT \\* FROM `plg_imports_job` WHERE id IN \\(\\?,\\?\\)").
		WithArgs(int64(11), int64(12)).
		WillReturnRows(sqlmock.NewRows([]string{"id", "status"}).
			AddRow(int64(11), StatusDone).AddRow(int64(12), StatusRunning))
	mock.ExpectRollback()

	n, err := s.DeleteJobs(tenant.With(context.Background(), 1), []idgen.ID{11, 12})
	var re *response.Error
	if !errors.As(err, &re) || re.Status != http.StatusConflict || re.Key != "imports.job_running" {
		t.Fatalf("期望 409 imports.job_running，得到 %#v", err)
	}
	if n != 0 {
		t.Errorf("deleted = %d, want 0（整批失败，已校验通过的那条也不能删）", n)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Error(err)
	}
}

// 点名 3 条、库里只有 2 条属于当前租户：实删 2，回执按 RowsAffected 报 2。
func TestDeleteJobsBatchCountsOnlyRealDeletes(t *testing.T) {
	db, mock := mockDB(t)
	s := NewService(db, nil)
	mock.ExpectBegin()
	mock.ExpectQuery("SELECT \\* FROM `plg_imports_job` WHERE id IN \\(\\?,\\?,\\?\\)").
		WithArgs(int64(11), int64(12), int64(13)).
		WillReturnRows(sqlmock.NewRows([]string{"id", "status"}).
			AddRow(int64(11), StatusDone).AddRow(int64(12), StatusCancelled))
	mock.ExpectExec("DELETE FROM `plg_imports_job` WHERE id IN \\(\\?,\\?\\)").
		WithArgs(int64(11), int64(12)).
		WillReturnResult(sqlmock.NewResult(0, 2))
	mock.ExpectCommit()

	n, err := s.DeleteJobs(tenant.With(context.Background(), 1), []idgen.ID{11, 12, 13})
	if err != nil {
		t.Fatalf("DeleteJobs: %v", err)
	}
	if n != 2 {
		t.Errorf("deleted = %d, want 2（越权/不存在的 id 不计入实删数）", n)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Error(err)
	}
}

// 一条都没命中报 404，不能静默返回 deleted=0 让界面以为删成功了。
func TestDeleteJobsBatchNotFoundWhenNothingMatches(t *testing.T) {
	db, mock := mockDB(t)
	s := NewService(db, nil)
	mock.ExpectBegin()
	mock.ExpectQuery("SELECT \\* FROM `plg_imports_job` WHERE id IN \\(\\?\\)").
		WithArgs(int64(99)).
		WillReturnRows(sqlmock.NewRows([]string{"id", "status"}))
	mock.ExpectRollback()

	if _, err := s.DeleteJobs(tenant.With(context.Background(), 1), []idgen.ID{99}); statusOf(err) != http.StatusNotFound {
		t.Fatalf("err = %v, want 404", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Error(err)
	}
}

// ?ids= 缺失或任一项非法一律 400，且在碰到数据库之前就返回（svc 为 nil：走到任何 SQL 都会 panic）。
// 尾随逗号不在这里：response.ParseIDs 按「空段跳过」处理，`ids=11,` 是合法的 1 个 ID。
func TestDeleteJobsBatchHandlerRejectsMissingOrBadIDs(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, q := range []string{"", "?ids=", "?ids=abc", "?ids=11,abc", "?ids=0", "?ids=-1"} {
		e := gin.New()
		h := &handler{svc: nil}
		e.DELETE("/plugins/imports", h.removeBatch)
		w := httptest.NewRecorder()
		e.ServeHTTP(w, httptest.NewRequest(http.MethodDelete, "/plugins/imports"+q, nil))
		if w.Code != http.StatusBadRequest {
			t.Errorf("query %q: code=%d want 400 body=%s", q, w.Code, w.Body.String())
		}
	}
}

// 回执的 key 名字是前端读的字段（deleted），写错成别的名字界面就永远提示「删除 0 条」。
func TestDeleteJobsBatchHandlerReplyKey(t *testing.T) {
	db, mock := mockDB(t)
	gin.SetMode(gin.TestMode)
	e := gin.New()
	e.DELETE("/plugins/imports", func(c *gin.Context) {
		c.Request = c.Request.WithContext(tenant.With(c.Request.Context(), 1))
		(&handler{svc: NewService(db, nil)}).removeBatch(c)
	})
	mock.ExpectBegin()
	mock.ExpectQuery("SELECT \\* FROM `plg_imports_job` WHERE id IN \\(\\?,\\?\\)").
		WithArgs(int64(11), int64(12)).
		WillReturnRows(sqlmock.NewRows([]string{"id", "status"}).
			AddRow(int64(11), StatusDone).AddRow(int64(12), StatusDone))
	mock.ExpectExec("DELETE FROM `plg_imports_job` WHERE id IN \\(\\?,\\?\\)").
		WithArgs(int64(11), int64(12)).
		WillReturnResult(sqlmock.NewResult(0, 2))
	mock.ExpectCommit()

	w := httptest.NewRecorder()
	e.ServeHTTP(w, httptest.NewRequest(http.MethodDelete, "/plugins/imports?ids=11,12", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("code=%d body=%s", w.Code, w.Body.String())
	}
	if body := w.Body.String(); !strings.Contains(body, `"deleted":2`) {
		t.Errorf("body = %q, want \"deleted\":2", body)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Error(err)
	}
}

func statusOf(err error) int {
	var re *response.Error
	if errors.As(err, &re) {
		return re.Status
	}
	return 0
}

// 集合路径与 :id 路径必须同时注册（gin 的路由树里两者共存；漏掉任一条本用例红，
// 真冲突时 Register 会直接 panic，启服即炸，所以这条也是「启得来服」的闸）。
func TestImportRoutesKeepSingleAndBatch(t *testing.T) {
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	api := engine.Group("/api/v1")
	(&handler{svc: nil}).Register(api, &core.Deps{Config: &config.Config{}})
	got := map[string]bool{}
	for _, r := range engine.Routes() {
		got[r.Method+" "+r.Path] = true
	}
	for _, want := range []string{
		"DELETE /api/v1/plugins/imports",
		"DELETE /api/v1/plugins/imports/:id",
	} {
		if !got[want] {
			t.Errorf("缺少路由 %s", want)
		}
	}
}
