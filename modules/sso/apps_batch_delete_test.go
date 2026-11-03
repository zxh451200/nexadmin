package sso

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/gin-gonic/gin"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"

	"nexadmin/core/tenant"
	"nexadmin/pkg/idgen"
)

// 批量删除的 SQL 形状在这里钉住：生产里租户条件还由 core/database 的租户插件兜一层，
// 但那个插件包内私有、装不进本包的 sqlmock，所以 handler 显式写 tenant_id（同 modules/sendlog）。
// 好处是断言测的就是真实语句，坏处是没有——少写一个条件，这里立刻变红。

func mockHandler(t *testing.T) (*handler, sqlmock.Sqlmock) {
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
	return &handler{db: db}, mock
}

func appsRouter(h *handler, tid idgen.ID) *gin.Engine {
	gin.SetMode(gin.TestMode)
	e := gin.New()
	if tid != 0 {
		e.Use(func(c *gin.Context) { c.Request = c.Request.WithContext(tenant.With(c.Request.Context(), tid)) })
	}
	e.DELETE("/sso/apps", h.removeMany)
	return e
}

// 两条 DELETE 都必须「租户 + 本次选中的 id」一起走，且在同一事务里：
// 少了 app_id 就是替全站清 refresh_token，少了 tenant_id 就是拿别人的应用 ID 也能删。
func TestRemoveManyScopesToTenantAndSelectedIDs(t *testing.T) {
	h, mock := mockHandler(t)
	const tid = idgen.ID(7)

	mock.ExpectBegin()
	mock.ExpectExec("DELETE FROM `sys_sso_refresh` WHERE tenant_id = \\? AND app_id IN \\(\\?,\\?\\)").
		WithArgs(int64(tid), int64(11), int64(12)).WillReturnResult(sqlmock.NewResult(0, 2))
	mock.ExpectExec("DELETE FROM `sys_sso_app` WHERE tenant_id = \\? AND id IN \\(\\?,\\?\\)").
		WithArgs(int64(tid), int64(11), int64(12)).WillReturnResult(sqlmock.NewResult(0, 2))
	mock.ExpectCommit()

	w := httptest.NewRecorder()
	appsRouter(h, tid).ServeHTTP(w, httptest.NewRequest(http.MethodDelete, "/sso/apps?ids=11,12", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d body = %s, want 200", w.Code, w.Body.String())
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Error(err)
	}
}

// ids 缺失或非法必须整体拒绝，并且一条 SQL 都不发出：
// 「不带 ids」在这个路由上的语义就是删全站，绝不能悄悄退回去。
func TestRemoveManyRefusesBadIDsWithoutTouchingDB(t *testing.T) {
	h, mock := mockHandler(t)
	for _, qs := range []string{"", "ids=", "ids=abc", "ids=11,abc", "ids=0", "ids=-3"} {
		w := httptest.NewRecorder()
		appsRouter(h, idgen.ID(7)).ServeHTTP(w, httptest.NewRequest(http.MethodDelete, "/sso/apps?"+qs, nil))
		if w.Code != http.StatusBadRequest {
			t.Errorf("ids=%q => %d %s, want 400", qs, w.Code, w.Body.String())
		}
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Error(err) // 任何一条语句都不该排到数据库
	}
}

// ctx 里认不出租户时删不了任何东西（500 + 一条 SQL 都不发），而不是退化成 tenant_id=0 的跨租户删除。
// 期望故意排得「宽松到能命中任何 DELETE」：语句真发出去了就会全部满足，
// 而这里要的是「一条都没发」——否则 sqlmock 对越界语句本身就报错，500 是巧合，测不出闸门。
func TestRemoveManyRefusesWithoutTenantInContext(t *testing.T) {
	h, mock := mockHandler(t)
	mock.ExpectBegin()
	mock.ExpectExec("DELETE FROM ").WillReturnResult(sqlmock.NewResult(0, 9))
	mock.ExpectExec("DELETE FROM ").WillReturnResult(sqlmock.NewResult(0, 9))
	mock.ExpectCommit()

	w := httptest.NewRecorder()
	appsRouter(h, 0).ServeHTTP(w, httptest.NewRequest(http.MethodDelete, "/sso/apps?ids=11", nil))
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d body = %s, want 500", w.Code, w.Body.String())
	}
	if err := mock.ExpectationsWereMet(); err == nil {
		t.Error("没有租户却把 DELETE 发到了数据库：应当一条语句都不发")
	}
}
