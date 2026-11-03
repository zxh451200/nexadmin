package backup

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/gin-gonic/gin"

	"nexadmin/core/ctl"
	"nexadmin/pkg/idgen"
)

// putBackupFile 在测试临时目录里造一份备份文件 + .sha256 旁路，返回两个路径。
// 只在 s.dir（t.TempDir，用例结束自动清理）里写，不碰真实备份目录。
func putBackupFile(t *testing.T, s *Service, name string) []string {
	t.Helper()
	p, err := s.Path(&Record{FileName: name})
	if err != nil {
		t.Fatalf("Path(%q): %v", name, err)
	}
	side := ctl.SidecarPath(p)
	for _, f := range []string{p, side} {
		if err := os.WriteFile(f, []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = os.Remove(f) })
	}
	return []string{p, side}
}

func mustGone(t *testing.T, paths []string) {
	t.Helper()
	for _, f := range paths {
		if _, err := os.Stat(f); !os.IsNotExist(err) {
			t.Errorf("%s still exists, want removed", f)
		}
	}
}

func mustExist(t *testing.T, paths []string) {
	t.Helper()
	for _, f := range paths {
		if _, err := os.Stat(f); err != nil {
			t.Errorf("%s missing: %v", f, err)
		}
	}
}

func backupRows() *sqlmock.Rows {
	return sqlmock.NewRows([]string{"id", "file_name", "status", "created_at"})
}

// 批量删必须和单条一样连带删磁盘文件（含 .sha256 旁路）：只删行就是磁盘上留一堆没人认领的备份。
// 把 DeleteMany 里的 removeFiles 调用注释掉，本用例必红。
func TestDeleteManyRemovesFilesAndRows(t *testing.T) {
	s, mock := mockService(t)
	a := putBackupFile(t, s, "a.sql.gz")
	b := putBackupFile(t, s, "b.sql.gz")

	mock.ExpectQuery("SELECT \\* FROM `sys_backup` WHERE id IN \\(\\?,\\?\\)").
		WithArgs(int64(11), int64(12)).
		WillReturnRows(backupRows().
			AddRow(int64(11), "a.sql.gz", StatusSuccess, btestNow).
			AddRow(int64(12), "b.sql.gz", StatusSuccess, btestNow))
	// 只允许删点名的这两条：去掉 WHERE id IN 就变成清表，正则匹配不上直接红
	mock.ExpectBegin()
	mock.ExpectExec("DELETE FROM `sys_backup` WHERE id IN \\(\\?,\\?\\)").
		WithArgs(int64(11), int64(12)).
		WillReturnResult(sqlmock.NewResult(0, 2))
	mock.ExpectCommit()

	n, err := s.DeleteMany(context.Background(), []idgen.ID{11, 12})
	if err != nil {
		t.Fatalf("DeleteMany: %v", err)
	}
	if n != 2 {
		t.Errorf("deleted = %d, want 2", n)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Error(err)
	}
	mustGone(t, append(a, b...))
}

// 进行中的备份不能删（同单条 Delete 的判据）。批量不许「删掉能删的那几条」：命中一条 running 整批拒绝，
// 已校验通过的那条也不能提前删文件。
func TestDeleteManyRejectsWholeBatchWhenOneIsRunning(t *testing.T) {
	s, mock := mockService(t)
	a := putBackupFile(t, s, "a.sql.gz")
	run := putBackupFile(t, s, "r.sql.gz")

	mock.ExpectQuery("SELECT \\* FROM `sys_backup` WHERE id IN \\(\\?,\\?\\)").
		WillReturnRows(backupRows().
			AddRow(int64(11), "a.sql.gz", StatusSuccess, btestNow).
			AddRow(int64(12), "r.sql.gz", StatusRunning, btestNow)) // 刚创建，未过 staleAfter

	// 不排任何 DELETE 期望：真发出了就是「删了能删的」，mock 会报错、断言也会挂
	n, err := s.DeleteMany(context.Background(), []idgen.ID{11, 12})
	if !errors.Is(err, ErrBusy) {
		t.Fatalf("err = %v, want ErrBusy", err)
	}
	if n != 0 {
		t.Errorf("deleted = %d, want 0", n)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Error(err)
	}
	mustExist(t, append(a, run...))
}

// 一条都没命中（都删过了 / id 不属于本表）要报不存在，不能静默返回 deleted=0 让界面以为删成功了。
func TestDeleteManyNotFoundWhenNothingMatches(t *testing.T) {
	s, mock := mockService(t)
	mock.ExpectQuery("SELECT \\* FROM `sys_backup` WHERE id IN \\(\\?\\)").
		WithArgs(int64(99)).WillReturnRows(backupRows())

	n, err := s.DeleteMany(context.Background(), []idgen.ID{99})
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
	if n != 0 {
		t.Errorf("deleted = %d, want 0", n)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Error(err)
	}
}

// ?ids= 缺失或非法一律 400：退化成「不带 ids 就全删」是最难发现的误删。
func TestDeleteManyHandlerRejectsMissingOrBadIDs(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, q := range []string{"", "?ids=", "?ids=abc", "?ids=11,abc", "?ids=0"} {
		e := gin.New()
		h := NewHandler(nil) // 校验在任何数据库访问之前完成，svc 不该被碰
		e.DELETE("/backups", h.DeleteMany)
		w := httptest.NewRecorder()
		e.ServeHTTP(w, httptest.NewRequest(http.MethodDelete, "/backups"+q, nil))
		if w.Code != http.StatusBadRequest {
			t.Errorf("query %q: code=%d want 400 body=%s", q, w.Code, w.Body.String())
		}
	}
}
