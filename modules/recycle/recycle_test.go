package recycle

import (
	"context"
	"encoding/json"
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
	"nexadmin/core/tenant"
	"nexadmin/modules/internal/recbin"
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
	return &Service{db: db}, mock
}

var entryCols = []string{"id", "tenant_id", "type", "ref_id", "name", "deleted_by", "deleted_at"}

func entryRow(id, tenantID int64, typ string, ref int64) *sqlmock.Rows {
	return sqlmock.NewRows(entryCols).AddRow(id, tenantID, typ, ref, "n", "admin", time.Now())
}

func expectEntry(mock sqlmock.Sqlmock, rows *sqlmock.Rows) {
	mock.ExpectQuery("FROM `sys_recycle` WHERE id = \\?").WillReturnRows(rows)
}

func statusOf(err error) int {
	var re *response.Error
	if errors.As(err, &re) {
		return re.Status
	}
	return 0
}

func ctxT() context.Context { return tenant.With(context.Background(), 1) }

func TestRestoreUsesRecordOwnTenantAndWhitelistedTable(t *testing.T) {
	s, mock := setup(t)
	expectEntry(mock, entryRow(10, 3, recbin.TypeUser, 77))
	mock.ExpectBegin()
	// 表名来自白名单映射，条件带回收记录自己的租户（原生 SQL 不经过租户 scope）
	mock.ExpectExec("UPDATE sys_user SET deleted_at = NULL WHERE id = \\? AND tenant_id = \\?").
		WithArgs(int64(77), int64(3)).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec("DELETE FROM `sys_recycle` WHERE id = \\?").WithArgs(int64(10)).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()
	if err := s.Restore(ctxT(), 10); err != nil {
		t.Fatal(err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Error(err)
	}
}

func TestRestoreConflictRollsBackAndKeepsEntry(t *testing.T) {
	s, mock := setup(t)
	expectEntry(mock, entryRow(10, 1, recbin.TypeUser, 77))
	mock.ExpectBegin()
	mock.ExpectExec("UPDATE sys_user SET deleted_at = NULL").WillReturnError(errors.New("Duplicate entry 'bob'"))
	mock.ExpectRollback()
	err := s.Restore(ctxT(), 10)
	if statusOf(err) != http.StatusConflict {
		t.Fatalf("唯一约束冲突应返回 409，得到 %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Error(err) // 不能出现删除回收记录的语句
	}
}

func TestPurgeOnlyHardDeletesSoftDeletedRowOfSameTenant(t *testing.T) {
	s, mock := setup(t)
	expectEntry(mock, entryRow(11, 4, recbin.TypeUser, 88))
	mock.ExpectBegin()
	mock.ExpectExec("DELETE FROM sys_user WHERE id = \\? AND tenant_id = \\? AND deleted_at IS NOT NULL").
		WithArgs(int64(88), int64(4)).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec("DELETE FROM `sys_recycle` WHERE id = \\?").WithArgs(int64(11)).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()
	if err := s.Purge(ctxT(), 11); err != nil {
		t.Fatal(err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Error(err)
	}
}

func TestPurgeNoticeAlsoCleansTargetsAndReads(t *testing.T) {
	s, mock := setup(t)
	expectEntry(mock, entryRow(12, 1, recbin.TypeNotice, 99))
	mock.ExpectBegin()
	mock.ExpectExec("DELETE FROM sys_notice_target WHERE notice_id = \\?").WithArgs(int64(99)).WillReturnResult(sqlmock.NewResult(0, 2))
	mock.ExpectExec("DELETE FROM sys_notice_read WHERE notice_id = \\?").WithArgs(int64(99)).WillReturnResult(sqlmock.NewResult(0, 5))
	mock.ExpectExec("DELETE FROM sys_notice WHERE id = \\? AND tenant_id = \\? AND deleted_at IS NOT NULL").
		WithArgs(int64(99), int64(1)).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec("DELETE FROM `sys_recycle`").WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()
	if err := s.Purge(ctxT(), 12); err != nil {
		t.Fatal(err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Error(err)
	}
}

// 类型不在 recbin.Tables 白名单内（包括被篡改成任意表名的脏数据）：不执行任何写操作。
func TestUnknownTypeNeverTouchesSQLTables(t *testing.T) {
	for _, typ := range []string{"sys_role", "user; DROP TABLE sys_user", "", "USER", "sys_user"} {
		for name, op := range map[string]func(*Service) error{
			"restore": func(s *Service) error { return s.Restore(ctxT(), 10) },
			"purge":   func(s *Service) error { return s.Purge(ctxT(), 10) },
		} {
			t.Run(name+"/"+typ, func(t *testing.T) {
				s, mock := setup(t)
				expectEntry(mock, entryRow(10, 1, typ, 5)) // 之后不预期任何语句（含 Begin）
				if got := statusOf(op(s)); got != http.StatusBadRequest {
					t.Fatalf("期望 400，得到 %d", got)
				}
				if err := mock.ExpectationsWereMet(); err != nil {
					t.Error(err)
				}
			})
		}
	}
}

func TestEntryNotFound(t *testing.T) {
	s, mock := setup(t)
	expectEntry(mock, sqlmock.NewRows(entryCols))
	if got := statusOf(s.Restore(ctxT(), 404)); got != http.StatusNotFound {
		t.Fatalf("记录不存在应 404，得到 %d", got)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Error(err)
	}
}

func TestListFiltersEscapesKeywordAndPaginates(t *testing.T) {
	s, mock := setup(t)
	mock.ExpectQuery("SELECT count\\(\\*\\) FROM `sys_recycle` WHERE type = \\? AND name LIKE \\?").
		WithArgs("user", `%a\%b\_c\\%`).WillReturnRows(sqlmock.NewRows([]string{"c"}).AddRow(25))
	mock.ExpectQuery("FROM `sys_recycle` WHERE type = \\? AND name LIKE \\? ORDER BY deleted_at DESC LIMIT \\? OFFSET \\?").
		WithArgs("user", `%a\%b\_c\\%`, 10, 10).WillReturnRows(entryRow(1, 1, "user", 5))
	list, total, err := s.List(ctxT(), "user", `a%b_c\`, 2, 10)
	if err != nil {
		t.Fatal(err)
	}
	if total != 25 || len(list) != 1 || list[0].Name != "n" || list[0].Type != "user" {
		t.Fatalf("total=%d list=%+v", total, list)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Error(err)
	}
}

func newRouter(s *Service) *gin.Engine {
	gin.SetMode(gin.TestMode)
	h := &Handler{svc: s}
	r := gin.New()
	r.Use(func(c *gin.Context) { c.Request = c.Request.WithContext(tenant.With(c.Request.Context(), 1)) })
	r.POST("/recycle/:id/restore", h.Restore)
	r.DELETE("/recycle/:id", h.Purge)
	return r
}

func TestHandlerRejectsBadIDsBeforeAnySQL(t *testing.T) {
	s, mock := setup(t) // 不预期任何 SQL
	r := newRouter(s)
	for _, tc := range []struct{ method, path string }{
		{http.MethodPost, "/recycle/abc/restore"},
		{http.MethodPost, "/recycle/0/restore"},
		{http.MethodDelete, "/recycle/abc"},
		{http.MethodDelete, "/recycle/0"},
	} {
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest(tc.method, tc.path, nil))
		if w.Code != http.StatusBadRequest {
			t.Errorf("%s %s 期望 400，得到 %d", tc.method, tc.path, w.Code)
		}
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Error(err)
	}
}

func TestHandlerRestoreOK(t *testing.T) {
	s, mock := setup(t)
	expectEntry(mock, entryRow(10, 1, recbin.TypeUser, 77))
	mock.ExpectBegin()
	mock.ExpectExec("UPDATE sys_user").WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec("DELETE FROM `sys_recycle`").WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()
	w := httptest.NewRecorder()
	newRouter(s).ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/recycle/10/restore", nil))
	var body response.Body
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil || w.Code != http.StatusOK || body.Code != 0 {
		t.Fatalf("code=%d body=%s", w.Code, w.Body.String())
	}
}

func TestRestoreSourceGoneReturns404AndCleansEntry(t *testing.T) {
	s, mock := setup(t)
	expectEntry(mock, entryRow(10, 3, recbin.TypeUser, 77))
	mock.ExpectBegin()
	mock.ExpectExec("UPDATE sys_user SET deleted_at = NULL WHERE id = \\? AND tenant_id = \\? AND deleted_at IS NOT NULL").
		WithArgs(int64(77), int64(3)).WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectExec("DELETE FROM `sys_recycle` WHERE id = \\?").WithArgs(int64(10)).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit() // 回收记录的清理必须提交，而不是随 404 回滚
	err := s.Restore(ctxT(), 10)
	var re *response.Error
	if !errors.As(err, &re) || re.Status != http.StatusNotFound || re.Key != "recycle.source_gone" {
		t.Fatalf("期望 404 recycle.source_gone，得到 %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Error(err)
	}
}

func TestPurgeSourceGoneReturns404AndCleansEntry(t *testing.T) {
	s, mock := setup(t)
	expectEntry(mock, entryRow(11, 4, recbin.TypeUser, 88))
	mock.ExpectBegin()
	mock.ExpectExec("DELETE FROM sys_user WHERE id = \\? AND tenant_id = \\? AND deleted_at IS NOT NULL").
		WithArgs(int64(88), int64(4)).WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectExec("DELETE FROM `sys_recycle` WHERE id = \\?").WithArgs(int64(11)).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()
	err := s.Purge(ctxT(), 11)
	var re *response.Error
	if !errors.As(err, &re) || re.Status != http.StatusNotFound || re.Key != "recycle.source_gone" {
		t.Fatalf("期望 404 recycle.source_gone，得到 %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Error(err)
	}
}
