package dashboard

import (
	"context"
	"database/sql/driver"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"

	"nexadmin/core/auth"
	"nexadmin/core/tenant"
	"nexadmin/pkg/idgen"
)

func init() { gin.SetMode(gin.TestMode) }

func newLayoutSvc(t *testing.T) (*service, sqlmock.Sqlmock) {
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
	return &service{db: db, log: zap.NewNop()}, mock
}

func tctx(id idgen.ID) context.Context { return tenant.With(context.Background(), id) }

type jsonContains string

func (j jsonContains) Match(v driver.Value) bool {
	s, _ := v.(string)
	return s == string(j)
}

func TestGetLayoutEmptyWhenNoRow(t *testing.T) {
	s, mock := newLayoutSvc(t)
	mock.ExpectQuery("FROM `sys_dashboard_layout` WHERE user_id = \\?").WithArgs(int64(7), 1).
		WillReturnRows(sqlmock.NewRows([]string{"id", "tenant_id", "user_id", "layout", "updated_at"}))
	l, err := s.GetLayout(tctx(1), 7)
	if err != nil {
		t.Fatal(err)
	}
	if l.Order == nil || l.Hidden == nil || len(l.Order) != 0 || len(l.Hidden) != 0 {
		t.Fatalf("want empty non-nil slices, got %#v", l)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestGetLayoutReadsOwnRow(t *testing.T) {
	s, mock := newLayoutSvc(t)
	mock.ExpectQuery("FROM `sys_dashboard_layout` WHERE user_id = \\?").WithArgs(int64(7), 1).
		WillReturnRows(sqlmock.NewRows([]string{"id", "tenant_id", "user_id", "layout"}).
			AddRow(1, 1, 7, `{"order":["a","b"],"hidden":["c"]}`))
	l, err := s.GetLayout(tctx(1), 7)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(l.Order, ",") != "a,b" || strings.Join(l.Hidden, ",") != "c" {
		t.Fatalf("unexpected layout %#v", l)
	}
}

func TestGetLayoutCorruptRowFallsBackToEmpty(t *testing.T) {
	s, mock := newLayoutSvc(t)
	mock.ExpectQuery("FROM `sys_dashboard_layout`").
		WillReturnRows(sqlmock.NewRows([]string{"id", "tenant_id", "user_id", "layout"}).AddRow(1, 1, 7, `not json`))
	l, err := s.GetLayout(tctx(1), 7)
	if err != nil || len(l.Order) != 0 || len(l.Hidden) != 0 {
		t.Fatalf("got %#v, %v", l, err)
	}
}

func TestPutLayoutWritesTenantAndUser(t *testing.T) {
	s, mock := newLayoutSvc(t)
	mock.ExpectBegin()
	mock.ExpectExec("INSERT INTO `sys_dashboard_layout`.*ON DUPLICATE KEY UPDATE").
		WithArgs(sqlmock.AnyArg(), int64(3), int64(7), jsonContains(`{"order":["a","b"],"hidden":["c"]}`), sqlmock.AnyArg(), jsonContains(`{"order":["a","b"],"hidden":["c"]}`), sqlmock.AnyArg()).
		WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectCommit()
	if err := s.PutLayout(tctx(3), 7, Layout{Order: []string{"a", "b"}, Hidden: []string{"c"}}); err != nil {
		t.Fatal(err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

// 重复保存同一份布局：每次都是同一条 upsert（靠 (tenant_id,user_id) 唯一键覆盖），不会报错也不会多写。
func TestPutLayoutIdempotent(t *testing.T) {
	s, mock := newLayoutSvc(t)
	for i := 0; i < 2; i++ {
		mock.ExpectBegin()
		mock.ExpectExec("ON DUPLICATE KEY UPDATE `layout`=").WillReturnResult(sqlmock.NewResult(1, 1))
		mock.ExpectCommit()
		if err := s.PutLayout(tctx(3), 7, Layout{Order: []string{"a"}}); err != nil {
			t.Fatalf("save #%d: %v", i+1, err)
		}
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestPutLayoutRejectsInvalid(t *testing.T) {
	many := make([]string, 101)
	for i := range many {
		many[i] = "c" + strings.Repeat("x", i%5) + string(rune('a'+i/26)) + string(rune('a'+i%26))
	}
	cases := map[string]Layout{
		"order too many":      {Order: many},
		"hidden too many":     {Hidden: many},
		"id too long":         {Order: []string{strings.Repeat("a", 65)}},
		"uppercase":           {Order: []string{"Dash"}},
		"leading dash":        {Hidden: []string{"-a"}},
		"leading dot":         {Order: []string{".a"}},
		"space":               {Order: []string{"a b"}},
		"slash":               {Order: []string{"a/b"}},
		"empty id":            {Order: []string{""}},
		"duplicate in order":  {Order: []string{"a", "b", "a"}},
		"duplicate in hidden": {Hidden: []string{"a", "a"}},
	}
	for name, l := range cases {
		t.Run(name, func(t *testing.T) {
			s, mock := newLayoutSvc(t)
			if err := s.PutLayout(tctx(1), 7, l); err == nil {
				t.Fatal("want error")
			}
			if err := mock.ExpectationsWereMet(); err != nil { // 校验失败不碰数据库
				t.Fatal(err)
			}
		})
	}
	// 边界：恰好 100 项、64 字符、合法字符集都通过
	ok := Layout{Order: many[:100], Hidden: []string{"a" + strings.Repeat("b", 63), "m-1_x.y"}}
	if err := ok.Validate(); err != nil {
		t.Fatalf("boundary should pass: %v", err)
	}
}

func TestDeleteLayoutOnlyOwnRow(t *testing.T) {
	s, mock := newLayoutSvc(t)
	mock.ExpectBegin()
	mock.ExpectExec("DELETE FROM `sys_dashboard_layout` WHERE user_id = \\?").WithArgs(int64(7)).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()
	if err := s.DeleteLayout(tctx(1), 7); err != nil {
		t.Fatal(err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func layoutReq(s *service, method, body string, cl *auth.Claims) *httptest.ResponseRecorder {
	r := gin.New()
	r.Use(func(c *gin.Context) {
		if cl != nil {
			auth.SetClaims(c, cl)
		}
		c.Request = c.Request.WithContext(tenant.With(c.Request.Context(), 1))
	})
	r.GET("/dashboard/layout", s.getLayoutHandler)
	r.PUT("/dashboard/layout", s.putLayoutHandler)
	r.DELETE("/dashboard/layout", s.deleteLayoutHandler)
	w := httptest.NewRecorder()
	req := httptest.NewRequest(method, "/dashboard/layout", strings.NewReader(body))
	r.ServeHTTP(w, req)
	return w
}

func TestLayoutHandlersForbidWithoutUserIdentity(t *testing.T) {
	for _, cl := range []*auth.Claims{nil, {UserID: 0}, {UserID: 7, Principal: auth.PrincipalApp}} {
		for _, m := range []string{http.MethodGet, http.MethodPut, http.MethodDelete} {
			s, mock := newLayoutSvc(t)
			w := layoutReq(s, m, `{"order":[],"hidden":[]}`, cl)
			if w.Code != http.StatusForbidden {
				t.Fatalf("%s claims=%+v: status %d, want 403", m, cl, w.Code)
			}
			if err := mock.ExpectationsWereMet(); err != nil {
				t.Fatal(err)
			}
		}
	}
}

func TestPutLayoutHandlerBodyLimitAndValidation(t *testing.T) {
	cl := &auth.Claims{UserID: 7}
	s, _ := newLayoutSvc(t)
	big := `{"order":["` + strings.Repeat("a", maxLayoutBody) + `"],"hidden":[]}`
	if w := layoutReq(s, http.MethodPut, big, cl); w.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("oversize body: status %d, want 413", w.Code)
	}
	if w := layoutReq(s, http.MethodPut, `{"order":["A"]}`, cl); w.Code != http.StatusBadRequest {
		t.Fatalf("invalid id: status %d, want 400", w.Code)
	}
	if w := layoutReq(s, http.MethodPut, `not json`, cl); w.Code != http.StatusBadRequest {
		t.Fatalf("bad json: status %d, want 400", w.Code)
	}
}

func TestPutLayoutHandlerSavesForCurrentUser(t *testing.T) {
	s, mock := newLayoutSvc(t)
	mock.ExpectBegin()
	mock.ExpectExec("INSERT INTO `sys_dashboard_layout`").
		WithArgs(sqlmock.AnyArg(), int64(1), int64(7), sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg()).
		WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectCommit()
	w := layoutReq(s, http.MethodPut, `{"order":["a"],"hidden":["b"]}`, &auth.Claims{UserID: 7})
	if w.Code != http.StatusOK {
		t.Fatalf("status %d: %s", w.Code, w.Body.String())
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}
