package pay

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

// 渠道批量删与单条 DeleteChannel（service.go 的裸 DELETE，没有「渠道在用/已结算不可删」
// 这类护栏，也不做连带清理）完全同口径：只删点名的行。
// 这里钉的是「只动点名的 id」与「实删数取自 RowsAffected」两条：
// 去掉 Where("id IN ?") 就是清渠道表，语句形状匹配不上直接红。
// 租户隔离由 core/database 的 tenantPlugin 在 DELETE 前追加 tenant_id 条件（与单条同一条路径，
// 裸 gorm 的 sqlmock 里注册不上该插件，插件本身由 core/database 的用例保证）。
// sqlmock 只正则匹配、从不执行 SQL，「少了哪几行」只能按 RowsAffected 的桩值钉。

func channelBatchStatus(err error) int {
	var re *response.Error
	if errors.As(err, &re) {
		return re.Status
	}
	return 0
}

func TestDeleteChannelsOnlyRequestedIDs(t *testing.T) {
	db, mock := mockDB(t)
	s := NewService(db, nopLog())
	mock.ExpectBegin()
	mock.ExpectExec("DELETE FROM `plg_pay_channel` WHERE id IN \\(\\?,\\?\\)").
		WithArgs(int64(11), int64(12)).
		WillReturnResult(sqlmock.NewResult(0, 2))
	mock.ExpectCommit()

	n, err := s.DeleteChannels(tenant.With(context.Background(), 1), []idgen.ID{11, 12})
	if err != nil {
		t.Fatalf("DeleteChannels: %v", err)
	}
	if n != 2 {
		t.Errorf("deleted = %d, want 2", n)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Error(err)
	}
}

// 越权 / 已不存在的 id 删不到，也就不会被计入实删数：点名 3 条、命中 1 条时回执必须是 1。
func TestDeleteChannelsCountsOnlyRealDeletes(t *testing.T) {
	db, mock := mockDB(t)
	s := NewService(db, nopLog())
	mock.ExpectBegin()
	mock.ExpectExec("DELETE FROM `plg_pay_channel` WHERE id IN \\(\\?,\\?,\\?\\)").
		WithArgs(int64(11), int64(12), int64(13)).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	n, err := s.DeleteChannels(tenant.With(context.Background(), 1), []idgen.ID{11, 12, 13})
	if err != nil {
		t.Fatalf("DeleteChannels: %v", err)
	}
	if n != 1 {
		t.Errorf("deleted = %d, want 1", n)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Error(err)
	}
}

// 0 命中报 not found，不能静默返回 deleted=0 让界面以为删成功了。
// 语句本身成功（0 行），not found 是在事务外判的，所以这里必须仍是 Commit 而不是 Rollback。
func TestDeleteChannelsNotFoundWhenNothingMatches(t *testing.T) {
	db, mock := mockDB(t)
	s := NewService(db, nopLog())
	mock.ExpectBegin()
	mock.ExpectExec("DELETE FROM `plg_pay_channel` WHERE id IN \\(\\?\\)").
		WithArgs(int64(99)).
		WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectCommit()

	if _, err := s.DeleteChannels(tenant.With(context.Background(), 1), []idgen.ID{99}); channelBatchStatus(err) != http.StatusNotFound {
		t.Fatalf("err = %v, want 404", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Error(err)
	}
}

// ?ids= 缺失或任一项非法一律 400，且在碰到数据库之前就返回（svc 为 nil：走到任何 SQL 都会 panic）。
func TestDeleteChannelsHandlerRejectsMissingOrBadIDs(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, q := range []string{"", "?ids=", "?ids=abc", "?ids=11,abc", "?ids=0", "?ids=-1"} {
		e := gin.New()
		h := &handler{svc: nil}
		e.DELETE("/plugins/pay/channels", h.deleteChannelsBatch)
		w := httptest.NewRecorder()
		e.ServeHTTP(w, httptest.NewRequest(http.MethodDelete, "/plugins/pay/channels"+q, nil))
		if w.Code != http.StatusBadRequest {
			t.Errorf("query %q: code=%d want 400 body=%s", q, w.Code, w.Body.String())
		}
	}
}

// 回执的 key 名字是前端读的字段（deleted），写成别的名字界面就永远提示「删除 0 条」。
func TestDeleteChannelsHandlerReplyKey(t *testing.T) {
	db, mock := mockDB(t)
	s := NewService(db, nopLog())
	gin.SetMode(gin.TestMode)
	e := gin.New()
	e.DELETE("/plugins/pay/channels", func(c *gin.Context) {
		c.Request = c.Request.WithContext(tenant.With(c.Request.Context(), 1))
		(&handler{svc: s}).deleteChannelsBatch(c)
	})
	mock.ExpectBegin()
	mock.ExpectExec("DELETE FROM `plg_pay_channel` WHERE id IN \\(\\?,\\?\\)").
		WithArgs(int64(11), int64(12)).
		WillReturnResult(sqlmock.NewResult(0, 2))
	mock.ExpectCommit()

	w := httptest.NewRecorder()
	e.ServeHTTP(w, httptest.NewRequest(http.MethodDelete, "/plugins/pay/channels?ids=11,12", nil))
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

// 集合路径与 :id 路径必须同时注册（同 media 的 TestVideoRoutesKeepSingleAndBatch）：
// 前端批量走 /channels?ids=，单条走 /channels/:id。夹具沿用 contract_test.go 的构造：
// Register 只装配路由，不触库，但公开端要挂 Public 分组。
func TestChannelRoutesKeepSingleAndBatch(t *testing.T) {
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	api := engine.Group("/api/v1")
	(&module{}).Register(api, &core.Deps{Config: &config.Config{}, Public: api})
	got := map[string]bool{}
	for _, r := range engine.Routes() {
		got[r.Method+" "+r.Path] = true
	}
	for _, want := range []string{
		"DELETE /api/v1/plugins/pay/channels",
		"DELETE /api/v1/plugins/pay/channels/:id",
	} {
		if !got[want] {
			t.Errorf("缺少路由 %s（已注册 %v）", want, got)
		}
	}
}
