package aigateway

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/gin-gonic/gin"

	"nexadmin/core/response"
	"nexadmin/pkg/idgen"
)

// 批量删必须只动点名的那几条，且必须带租户条件：
// 去掉 tenant_id 就是「拿别的租户的模型 ID 也能删」，去掉 id IN 就是清掉本租户全部模型
// （模型密钥集中在这里，清了等于把所有宿主应用的 AI 能力一起下线）。
// 生产里 core/database 的 tenantPlugin 还会再追加一次 tenant_id，这里是第二道防线。
func TestDeleteModelsScopesToTenantAndIDs(t *testing.T) {
	db, m := mockDB(t)
	s := NewService(db, nopLogger{})

	m.ExpectBegin()
	m.ExpectExec("DELETE FROM `plg_aigateway_model` WHERE tenant_id = \\? AND id IN \\(\\?,\\?\\)").
		WithArgs(int64(7), int64(11), int64(12)).
		WillReturnResult(sqlmock.NewResult(0, 2))
	m.ExpectCommit()

	n, err := s.DeleteModels(tenantCtx, []idgen.ID{11, 12})
	if err != nil {
		t.Fatalf("DeleteModels: %v", err)
	}
	if n != 2 {
		t.Errorf("deleted = %d, want 2", n)
	}
	if err := m.ExpectationsWereMet(); err != nil {
		t.Error(err)
	}
}

// 越权 / 已不存在的 id 落在条件外，删不到也就不会被计入实删数：命中 1 条返回 1。
func TestDeleteModelsCountsOnlyRealDeletes(t *testing.T) {
	db, m := mockDB(t)
	s := NewService(db, nopLogger{})

	m.ExpectBegin()
	m.ExpectExec("DELETE FROM `plg_aigateway_model` WHERE tenant_id = \\? AND id IN \\(\\?,\\?,\\?\\)").
		WithArgs(int64(7), int64(11), int64(12), int64(13)).
		WillReturnResult(sqlmock.NewResult(0, 1))
	m.ExpectCommit()

	n, err := s.DeleteModels(tenantCtx, []idgen.ID{11, 12, 13})
	if err != nil {
		t.Fatalf("DeleteModels: %v", err)
	}
	if n != 1 {
		t.Errorf("deleted = %d, want 1", n)
	}
	if err := m.ExpectationsWereMet(); err != nil {
		t.Error(err)
	}
}

// 0 命中报 not found，不能静默返回 deleted=0 让界面以为删成功了。
func TestDeleteModelsNotFoundWhenNothingMatches(t *testing.T) {
	db, m := mockDB(t)
	s := NewService(db, nopLogger{})

	m.ExpectBegin()
	m.ExpectExec("DELETE FROM `plg_aigateway_model` WHERE tenant_id = \\? AND id IN \\(\\?\\)").
		WithArgs(int64(7), int64(99)).
		WillReturnResult(sqlmock.NewResult(0, 0))
	m.ExpectCommit() // 语句本身成功（0 行），not found 是事务外判的，不该顺手回滚

	if _, err := s.DeleteModels(tenantCtx, []idgen.ID{99}); statusOf(err) != http.StatusNotFound {
		t.Fatalf("err = %v, want 404", err)
	}
	if err := m.ExpectationsWereMet(); err != nil {
		t.Error(err)
	}
}

// 批量是一条 DELETE 走完：语句报错就整批回滚，不能出现「选中 3 条删掉 1 条」的中间状态。
func TestDeleteModelsRollsBackWholeBatchOnSQLFailure(t *testing.T) {
	db, m := mockDB(t)
	s := NewService(db, nopLogger{})

	m.ExpectBegin()
	m.ExpectExec("DELETE FROM `plg_aigateway_model`").
		WillReturnError(errors.New("boom"))
	m.ExpectRollback() // 期望里没有 Commit：一旦实现改成「提交半成品」这里就红

	if n, err := s.DeleteModels(tenantCtx, []idgen.ID{11, 12}); err == nil || n != 0 {
		t.Fatalf("deleted=%d err=%v, want 0 and an error", n, err)
	}
	if err := m.ExpectationsWereMet(); err != nil {
		t.Error(err)
	}
}

// ?ids= 缺失、整体为空或任一项非法一律 400，且在任何数据库访问之前拒绝：
// 退化成「不带 ids 就删光本租户模型」是最难发现的误删。svc 传 nil 是有意的——
// 一旦校验被跳过，调用服务层就会当场炸给测试看。
func TestDeleteModelsHandlerRejectsMissingOrBadIDs(t *testing.T) {
	for _, q := range []string{"", "?ids=", "?ids=abc", "?ids=11,abc", "?ids=0", "?ids=,"} {
		e := gin.New()
		h := &handler{svc: nil}
		e.DELETE("/plugins/aigateway/models", h.deleteModels)
		w := httptest.NewRecorder()
		e.ServeHTTP(w, httptest.NewRequest(http.MethodDelete, "/plugins/aigateway/models"+q, nil))
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
