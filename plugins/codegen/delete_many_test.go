package codegen

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/gin-gonic/gin"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"

	"nexadmin/core/response"
	"nexadmin/pkg/idgen"
)

func mockCfgDB(t *testing.T) (*Service, sqlmock.Sqlmock) {
	t.Helper()
	sqlDB, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	db, err := gorm.Open(mysql.New(mysql.Config{Conn: sqlDB, SkipInitializeWithVersion: true}),
		&gorm.Config{DisableAutomaticPing: true, NowFunc: func() time.Time { return time.Unix(0, 0).UTC() }})
	if err != nil {
		t.Fatal(err)
	}
	return NewService(newFake(), db), mock
}

// 批量删必须只动点名的那几条：plg_codegen_config 是平台级表（PlatformModel 没有 tenant_id 列），
// id IN 就是它唯一的范围条件——去掉它等于清掉所有人存过的生成配置，正则匹配不上直接红。
func TestDeleteConfigsOnlyRequestedIDs(t *testing.T) {
	s, mock := mockCfgDB(t)
	mock.ExpectBegin()
	mock.ExpectExec("DELETE FROM `plg_codegen_config` WHERE id IN \\(\\?,\\?\\)").
		WithArgs(int64(11), int64(12)).
		WillReturnResult(sqlmock.NewResult(0, 2))
	mock.ExpectCommit()

	n, err := s.DeleteConfigs(context.Background(), []idgen.ID{11, 12})
	if err != nil {
		t.Fatalf("DeleteConfigs: %v", err)
	}
	if n != 2 {
		t.Errorf("deleted = %d, want 2", n)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Error(err)
	}
}

// 已不存在 / 别人存到的 id 删不到，也不计入实删数：命中 1 条就返回 1。
func TestDeleteConfigsCountsOnlyRealDeletes(t *testing.T) {
	s, mock := mockCfgDB(t)
	mock.ExpectBegin()
	mock.ExpectExec("DELETE FROM `plg_codegen_config` WHERE id IN \\(\\?,\\?,\\?\\)").
		WithArgs(int64(11), int64(12), int64(13)).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	n, err := s.DeleteConfigs(context.Background(), []idgen.ID{11, 12, 13})
	if err != nil {
		t.Fatalf("DeleteConfigs: %v", err)
	}
	if n != 1 {
		t.Errorf("deleted = %d, want 1", n)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Error(err)
	}
}

// 0 命中报 not found，不能静默返回 deleted=0 让界面以为删成功了。
func TestDeleteConfigsNotFoundWhenNothingMatches(t *testing.T) {
	s, mock := mockCfgDB(t)
	mock.ExpectBegin()
	mock.ExpectExec("DELETE FROM `plg_codegen_config` WHERE id IN \\(\\?\\)").
		WithArgs(int64(99)).
		WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectCommit() // 语句本身成功（0 行），not found 是事务外判的，不该顺手回滚

	if _, err := s.DeleteConfigs(context.Background(), []idgen.ID{99}); statusOf(err) != http.StatusNotFound {
		t.Fatalf("err = %v, want 404", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Error(err)
	}
}

// 批量是一条 DELETE 走完：语句报错整批回滚，不会留下「选中 3 条删掉 1 条」的中间状态。
func TestDeleteConfigsRollsBackWholeBatchOnSQLFailure(t *testing.T) {
	s, mock := mockCfgDB(t)
	mock.ExpectBegin()
	mock.ExpectExec("DELETE FROM `plg_codegen_config`").WillReturnError(errors.New("boom"))
	mock.ExpectRollback() // 期望里没有 Commit：改成「提交半成品」这里就红

	if n, err := s.DeleteConfigs(context.Background(), []idgen.ID{11, 12}); err == nil || n != 0 {
		t.Fatalf("deleted=%d err=%v, want 0 and an error", n, err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Error(err)
	}
}

// ?ids= 缺失、整体为空或任一项非法一律 400，且在任何数据库访问之前拒绝：
// 退化成「不带 ids 就删光配置」是最难发现的误删。svc 传 nil 是有意的——
// 一旦跳过校验就会走到服务层，当场炸给测试看。
func TestDeleteConfigsHandlerRejectsMissingOrBadIDs(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, q := range []string{"", "?ids=", "?ids=abc", "?ids=11,abc", "?ids=0", "?ids=,"} {
		e := gin.New()
		e.DELETE("/plugins/codegen/configs", (&handler{svc: nil}).deleteConfigs)
		w := httptest.NewRecorder()
		e.ServeHTTP(w, httptest.NewRequest(http.MethodDelete, "/plugins/codegen/configs"+q, nil))
		if w.Code != http.StatusBadRequest {
			t.Errorf("query %q: code=%d want 400 body=%s", q, w.Code, w.Body.String())
		}
	}
}

// 没装配库时报内部错误，不是「删了 0 条」也不是放行。
func TestDeleteConfigsWithoutDB(t *testing.T) {
	s := NewService(newFake(), nil)
	if _, err := s.DeleteConfigs(context.Background(), []idgen.ID{11}); statusOf(err) != http.StatusInternalServerError {
		t.Fatalf("err = %v, want 500", err)
	}
}

func statusOf(err error) int {
	var re *response.Error
	if errors.As(err, &re) {
		return re.Status
	}
	return 0
}
