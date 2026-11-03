package cron

import (
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

func mockDB(t *testing.T) (*Service, sqlmock.Sqlmock) {
	t.Helper()
	sqlDB, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	db, err := gorm.Open(mysql.New(mysql.Config{Conn: sqlDB, SkipInitializeWithVersion: true}),
		&gorm.Config{DisableAutomaticPing: true})
	if err != nil {
		t.Fatal(err)
	}
	return NewService(db, nil), mock
}

// 批量删必须只动点名的那几条：去掉 WHERE id IN 就是清表，正则匹配不上直接红。
// 租户隔离由 core/database 的 tenantPlugin 在 DELETE 前追加 tenant_id 条件（与单条 DeleteJob 同一条路径，
// 裸 gorm 的 sqlmock 里注册不上该插件，语句形状与 id 集合在这里钉住，插件本身由 core/database 的用例保证）。
func TestDeleteJobsOnlyRequestedIDs(t *testing.T) {
	s, mock := mockDB(t)
	mock.ExpectBegin()
	mock.ExpectExec("DELETE FROM `plg_cron_job` WHERE id IN \\(\\?,\\?\\)").
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

// 越权 / 已不存在的 id 删不到，也就不会被计入实删数：这里只命中 1 条，返回 1。
func TestDeleteJobsCountsOnlyRealDeletes(t *testing.T) {
	s, mock := mockDB(t)
	mock.ExpectBegin()
	mock.ExpectExec("DELETE FROM `plg_cron_job` WHERE id IN \\(\\?,\\?,\\?\\)").
		WithArgs(int64(11), int64(12), int64(13)).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	n, err := s.DeleteJobs(tenant.With(context.Background(), 1), []idgen.ID{11, 12, 13})
	if err != nil {
		t.Fatalf("DeleteJobs: %v", err)
	}
	if n != 1 {
		t.Errorf("deleted = %d, want 1", n)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Error(err)
	}
}

// 0 命中报 not found，不能静默返回 deleted=0 让界面以为删成功了。
func TestDeleteJobsNotFoundWhenNothingMatches(t *testing.T) {
	s, mock := mockDB(t)
	mock.ExpectBegin()
	mock.ExpectExec("DELETE FROM `plg_cron_job` WHERE id IN \\(\\?\\)").
		WithArgs(int64(99)).
		WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectCommit() // 语句本身成功（0 行），not found 是我在事务外判的，不能顺手回滚掉

	if _, err := s.DeleteJobs(tenant.With(context.Background(), 1), []idgen.ID{99}); statusOf(err) != http.StatusNotFound {
		t.Fatalf("err = %v, want 404", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Error(err)
	}
}

// ?ids= 缺失或非法一律 400：退化成「不带 ids 就删全部任务」是最难发现的误删。
func TestDeleteJobsHandlerRejectsMissingOrBadIDs(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, q := range []string{"", "?ids=", "?ids=abc", "?ids=11,abc", "?ids=0"} {
		e := gin.New()
		h := &handler{svc: nil} // 校验在任何数据库访问之前完成，svc 不该被碰
		e.DELETE("/plugins/cron/jobs", h.deleteJobs)
		w := httptest.NewRecorder()
		e.ServeHTTP(w, httptest.NewRequest(http.MethodDelete, "/plugins/cron/jobs"+q, nil))
		if w.Code != http.StatusBadRequest {
			t.Errorf("query %q: code=%d want 400 body=%s", q, w.Code, w.Body.String())
		}
	}
}

func statusOf(err error) int {
	var re *response.Error
	if errors.As(err, &re) {
		return re.Status
	}
	return 0
}
