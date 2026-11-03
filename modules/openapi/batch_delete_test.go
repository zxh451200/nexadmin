package openapi

// 三个批量删除端点的 SQL 形状在这里钉住：apps / webhooks 的作用域是 tenant_id，
// tokens 的作用域是 user_id（用户自助接口，没有权限码）。
// 生产里 core/database 的租户插件还会再兜一层 tenant_id，但那个插件包内私有、装不进本包的
// sqlmock，所以 handler 显式写进 WHERE（同 modules/sso、modules/sendlog 的批量删除）：
// 断言测的就是真实语句——少写一个作用域条件，这里立刻变红。

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/gin-gonic/gin"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"

	"nexadmin/core/auth"
	"nexadmin/core/tenant"
	"nexadmin/pkg/idgen"
)

func batchMockDB(t *testing.T) (*handler, sqlmock.Sqlmock) {
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
	return &handler{db: db}, mock
}

// batchRouter 挂租户（tid=0 表示 ctx 里没有租户）与登录用户，只注册被测的那条批量路由。
func batchRouter(t *testing.T, h *handler, path string, tid, uid idgen.ID) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)
	e := gin.New()
	e.Use(func(c *gin.Context) {
		if tid != 0 {
			c.Request = c.Request.WithContext(tenant.With(c.Request.Context(), tid))
		}
		if uid != 0 {
			auth.SetClaims(c, &auth.Claims{UserID: uid, TenantID: tid})
		}
	})
	switch path {
	case "/openapi/apps":
		e.DELETE(path, h.appRemoveMany)
	case "/openapi/webhooks":
		e.DELETE(path, h.hookRemoveMany)
	case "/profile/tokens":
		e.DELETE(path, h.tokenDeleteMany)
	default:
		t.Fatalf("未知路由 %s", path)
	}
	return e
}

func batchDelete(t *testing.T, h *handler, path, query string, tid, uid idgen.ID) *httptest.ResponseRecorder {
	t.Helper()
	w := httptest.NewRecorder()
	batchRouter(t, h, path, tid, uid).ServeHTTP(w, httptest.NewRequest(http.MethodDelete, path+query, nil))
	return w
}

// deletedOf 解出 {"data":{"deleted":n}}，顺带确认 data 是对象而不是 null。
func deletedOf(t *testing.T, w *httptest.ResponseRecorder) int64 {
	t.Helper()
	var body struct {
		Code int `json:"code"`
		Data struct {
			Deleted *int64 `json:"deleted"`
		} `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("body = %s: %v", w.Body.String(), err)
	}
	if body.Data.Deleted == nil {
		t.Fatalf("body = %s, want data.deleted", w.Body.String())
	}
	return *body.Data.Deleted
}

// 开放应用的批量删除必须「本租户 + 本次选中的 id」一起走：
// 少了 tenant_id 就是拿别人的应用 ID 也能删（AppKey/密钥跨租户撞车时更是直接删错对象）。
func TestAppRemoveManyScopesToTenantAndSelectedIDs(t *testing.T) {
	h, mock := batchMockDB(t)
	const tid = idgen.ID(7)

	// GORM 的 Delete 自带事务包裹，期望里必须一起排上
	mock.ExpectBegin()
	mock.ExpectExec("DELETE FROM `sys_openapi_app` WHERE tenant_id = \\? AND id IN \\(\\?,\\?\\)").
		WithArgs(int64(tid), int64(11), int64(12)).
		WillReturnResult(sqlmock.NewResult(0, 2))
	mock.ExpectCommit()

	w := batchDelete(t, h, "/openapi/apps", "?ids=11,12", tid, 0)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d body = %s, want 200", w.Code, w.Body.String())
	}
	if n := deletedOf(t, w); n != 2 {
		t.Errorf("deleted = %d, want 2", n)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Error(err)
	}
}

// Webhook 的批量删除与单条同清理：在同一事务里先清投递记录、再删订阅本身。
// 两条语句都要带 tenant_id：少了 webhook_id 会替全站清投递历史，少了 tenant_id 会清掉别人的。
func TestHookRemoveManyClearsDeliveriesInOneTransaction(t *testing.T) {
	h, mock := batchMockDB(t)
	const tid = idgen.ID(7)

	mock.ExpectBegin()
	mock.ExpectExec("DELETE FROM `sys_webhook_delivery` WHERE tenant_id = \\? AND webhook_id IN \\(\\?,\\?\\)").
		WithArgs(int64(tid), int64(11), int64(12)).
		WillReturnResult(sqlmock.NewResult(0, 5))
	mock.ExpectExec("DELETE FROM `sys_webhook` WHERE tenant_id = \\? AND id IN \\(\\?,\\?\\)").
		WithArgs(int64(tid), int64(11), int64(12)).
		WillReturnResult(sqlmock.NewResult(0, 2))
	mock.ExpectCommit()

	w := batchDelete(t, h, "/openapi/webhooks", "?ids=11,12", tid, 0)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d body = %s, want 200", w.Code, w.Body.String())
	}
	if n := deletedOf(t, w); n != 2 {
		t.Errorf("deleted = %d, want 2（实删订阅数，不是投递记录数）", n)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Error(err)
	}
}

// 令牌的作用域是 user_id：混进别人（或别的租户）的令牌 id 不会被删，也不计入实删条数。
// 这里请求 2 条、库里只命中 1 条，返回必须是 deleted=1。
func TestTokenDeleteManyCountsOnlyOwnRows(t *testing.T) {
	h, mock := batchMockDB(t)
	const uid = idgen.ID(91)

	mock.ExpectBegin()
	mock.ExpectExec("DELETE FROM `sys_pat` WHERE user_id = \\? AND id IN \\(\\?,\\?\\)").
		WithArgs(int64(uid), int64(11), int64(12)).
		WillReturnResult(sqlmock.NewResult(0, 1)) // 12 是别人的，一行都没动
	mock.ExpectCommit()

	w := batchDelete(t, h, "/profile/tokens", "?ids=11,12", idgen.ID(7), uid)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d body = %s, want 200", w.Code, w.Body.String())
	}
	if n := deletedOf(t, w); n != 1 {
		t.Errorf("deleted = %d, want 1（越权 id 不计入实删数）", n)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Error(err)
	}
}

// 一条都没命中（含全是越权 id）：与单条删除同口径报 404，不能假装成功。
func TestTokenDeleteManyNotFoundWhenNothingOwned(t *testing.T) {
	h, mock := batchMockDB(t)
	const uid = idgen.ID(91)

	mock.ExpectBegin()
	mock.ExpectExec("DELETE FROM `sys_pat` WHERE user_id = \\? AND id IN \\(\\?\\)").
		WithArgs(int64(uid), int64(12)).
		WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectCommit()

	w := batchDelete(t, h, "/profile/tokens", "?ids=12", idgen.ID(7), uid)
	if w.Code != http.StatusNotFound {
		t.Fatalf("status = %d body = %s, want 404", w.Code, w.Body.String())
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Error(err)
	}
}

// ids 缺失或非法必须整体 400，并且一条 SQL 都不发出：
// 「不带 ids」在这些路由上的语义就是删掉整张表，绝不能悄悄退回去。
func TestBatchDeletesRefuseBadIDsWithoutTouchingDB(t *testing.T) {
	const tid, uid = idgen.ID(7), idgen.ID(91)
	for _, tc := range []struct {
		path string
		uid  idgen.ID
	}{
		{"/openapi/apps", 0},
		{"/openapi/webhooks", 0},
		{"/profile/tokens", uid},
	} {
		h, mock := batchMockDB(t)
		for _, q := range []string{"", "?ids=", "?ids=abc", "?ids=11,abc", "?ids=0", "?ids=-3"} {
			w := batchDelete(t, h, tc.path, q, tid, tc.uid)
			if w.Code != http.StatusBadRequest {
				t.Errorf("%s%s => %d %s, want 400", tc.path, q, w.Code, w.Body.String())
			}
		}
		if err := mock.ExpectationsWereMet(); err != nil {
			t.Errorf("%s: 一条语句都不该发出去: %v", tc.path, err)
		}
	}
}

// ctx 里认不出租户时，apps/webhooks 的批量删除一条都不动（500 + 零 SQL），而不是退化成跨租户删除。
func TestBatchDeletesRefuseWithoutTenant(t *testing.T) {
	for _, path := range []string{"/openapi/apps", "/openapi/webhooks"} {
		h, mock := batchMockDB(t)
		w := batchDelete(t, h, path, "?ids=11", 0, 0)
		if w.Code != http.StatusInternalServerError {
			t.Errorf("%s => %d %s, want 500", path, w.Code, w.Body.String())
		}
		if err := mock.ExpectationsWereMet(); err != nil {
			t.Errorf("%s: 一条语句都不该发出去: %v", path, err)
		}
	}
}

// 令牌接口不接受应用/令牌主体：没有真实登录会话时 403，且不发 SQL（与单条删除一致）。
func TestTokenDeleteManyRefusesNonUserPrincipal(t *testing.T) {
	h, mock := batchMockDB(t)
	gin.SetMode(gin.TestMode)
	e := gin.New()
	e.Use(func(c *gin.Context) {
		c.Request = c.Request.WithContext(tenant.With(c.Request.Context(), idgen.ID(7)))
		auth.SetClaims(c, &auth.Claims{UserID: 91, TenantID: 7, Principal: auth.PrincipalApp})
	})
	e.DELETE("/profile/tokens", h.tokenDeleteMany)

	w := httptest.NewRecorder()
	e.ServeHTTP(w, httptest.NewRequest(http.MethodDelete, "/profile/tokens?ids=11", nil))
	if w.Code != http.StatusForbidden {
		t.Fatalf("status = %d body = %s, want 403", w.Code, w.Body.String())
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Error(err)
	}
}
