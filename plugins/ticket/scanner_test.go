package ticket

// 附件杀毒闸的用例：这道闸是「可插拔」的，所以要同时证明三件事——
// 没接引擎时上传照常（零开销跳过）、接了引擎时真的把字节交给它、
// 引擎说脏就 400 且不落存储对象（否则拒绝完留下一堆孤儿文件）。

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"

	"nexadmin/core/storage"
)

type fakeScanner struct {
	err  error
	seen int
	got  string
}

func (f *fakeScanner) Name() string { return "fake-av" }

func (f *fakeScanner) Scan(_ context.Context, filename, _ string, data []byte) error {
	f.seen++
	f.got = filename
	if len(data) == 0 {
		return errors.New("扫描器没拿到任何字节")
	}
	return f.err
}

// uploadHarness 起一套 handler + 本地存储 + sqlmock，返回三者与临时目录。
func uploadHarness(t *testing.T) (*handler, string, sqlmock.Sqlmock) {
	t.Helper()
	db, m := mockDB(t)
	storeDir := t.TempDir()
	store, err := storage.NewLocal(storeDir)
	if err != nil {
		t.Fatal(err)
	}
	svc := NewService(db, nopLogger{})
	svc.SetStorage(store)
	return &handler{svc: svc, store: store}, storeDir, m
}

// expectUploadEntry 是上传入口的两条查询（读工单 + 配额预检）。
func expectUploadEntry(m sqlmock.Sqlmock) {
	m.ExpectQuery("SELECT \\* FROM `plg_ticket` WHERE ticket_no = \\?").
		WillReturnRows(ticketRow("TK1001", StatusOpen, 1))
	m.ExpectQuery("SELECT COUNT\\(\\*\\) AS cnt").WithArgs(int64(1001)).
		WillReturnRows(quotaRows(1, 1<<20))
}

// expectUploadFinish 是 SaveAttachment 之后的其余查询与写入。
func expectUploadFinish(m sqlmock.Sqlmock) {
	m.ExpectQuery("SELECT \\* FROM `plg_ticket` WHERE ticket_no = \\?").
		WillReturnRows(ticketRow("TK1001", StatusOpen, 1))
	m.ExpectQuery("SELECT COUNT\\(\\*\\) AS cnt").WithArgs(int64(1001)).
		WillReturnRows(quotaRows(1, 1<<20))
	m.ExpectBegin()
	m.ExpectExec("INSERT INTO `plg_ticket_attachment`").WillReturnResult(sqlmock.NewResult(1, 1))
	m.ExpectCommit()
	m.ExpectBegin()
	m.ExpectExec("INSERT INTO `plg_ticket_activity`").WillReturnResult(sqlmock.NewResult(1, 1))
	m.ExpectCommit()
}

func TestScannerGate(t *testing.T) {
	t.Cleanup(func() { RegisterScanner(nil) })

	t.Run("未接入时上传照常", func(t *testing.T) {
		RegisterScanner(nil)
		if got := ScannerName(); got != "none" {
			t.Fatalf("ScannerName = %q, want none", got)
		}
		h, storeDir, m := uploadHarness(t)
		expectUploadEntry(m)
		expectUploadFinish(m)
		w := httptest.NewRecorder()
		attachmentEngine(h).ServeHTTP(w, uploadPNGRequest(t, "TK1001"))
		if w.Code != http.StatusOK {
			t.Fatalf("status = %d body=%s", w.Code, w.Body.String())
		}
		if n := countStoredFiles(t, storeDir); n != 1 {
			t.Fatalf("存储对象数 = %d, want 1", n)
		}
		if err := m.ExpectationsWereMet(); err != nil {
			t.Fatal(err)
		}
	})

	t.Run("引擎判脏就拒绝且不落存储", func(t *testing.T) {
		RegisterScanner(&fakeScanner{err: errors.New("eicar signature")})
		h, storeDir, m := uploadHarness(t)
		expectUploadEntry(m)
		w := httptest.NewRecorder()
		attachmentEngine(h).ServeHTTP(w, uploadPNGRequest(t, "TK1001"))
		if w.Code != http.StatusBadRequest {
			t.Fatalf("status = %d body=%s", w.Code, w.Body.String())
		}
		if body := w.Body.String(); !strings.Contains(body, "attachment_rejected") {
			t.Fatalf("拒绝原因缺失: %s", body)
		}
		if n := countStoredFiles(t, storeDir); n != 0 {
			t.Fatalf("被拒绝的附件不该留下存储对象，实际 %d 个", n)
		}
		if err := m.ExpectationsWereMet(); err != nil {
			t.Fatal(err)
		}
	})

	t.Run("引擎放行时字节确实交到了扫描器手里", func(t *testing.T) {
		sc := &fakeScanner{}
		RegisterScanner(sc)
		h, storeDir, m := uploadHarness(t)
		expectUploadEntry(m)
		expectUploadFinish(m)
		w := httptest.NewRecorder()
		attachmentEngine(h).ServeHTTP(w, uploadPNGRequest(t, "TK1001"))
		if w.Code != http.StatusOK {
			t.Fatalf("status = %d body=%s", w.Code, w.Body.String())
		}
		if sc.seen != 1 {
			t.Fatalf("扫描器调用次数 = %d, want 1（接线断了也全绿就是假闸）", sc.seen)
		}
		if sc.got != "shot.png" {
			t.Errorf("扫描器收到的文件名 = %q", sc.got)
		}
		if n := countStoredFiles(t, storeDir); n != 1 {
			t.Fatalf("存储对象数 = %d, want 1", n)
		}
	})
}
