package ticket

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io/fs"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/gin-gonic/gin"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"

	"nexadmin/core/response"
	"nexadmin/core/storage"
)

func TestAttachmentRuleFor(t *testing.T) {
	cases := []struct {
		mime, name string
		ok         bool
		ext        string
	}{
		{"image/png", "x.png", true, ".png"},
		{"image/jpeg", "x.jpg", true, ".jpg"},
		{"application/pdf", "x.pdf", true, ".pdf"},
		{"application/zip", "a.docx", true, ".docx"},
		{"application/zip", "a.zip", true, ".zip"},
		{"application/x-msdownload", "a.exe", false, ""},
		{"text/html", "a.html", false, ""},
	}
	for _, c := range cases {
		rule, ok := attachmentRuleFor(c.mime, c.name)
		if ok != c.ok || (ok && rule.ext != c.ext) {
			t.Errorf("%s/%s: got ok=%v ext=%q", c.mime, c.name, ok, rule.ext)
		}
	}
}

func TestSafeFilename(t *testing.T) {
	cases := map[string]string{
		`..\..\etc/passwd`: "passwd",
		"/tmp/a\x00b.txt":  "ab.txt",
		"":                 "file",
		"..":               "file",
	}
	for in, want := range cases {
		if got := safeFilename(in); got != want {
			t.Errorf("safeFilename(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestRandomToken(t *testing.T) {
	a, b := randomToken(16), randomToken(16)
	if len(a) != 32 {
		t.Fatalf("token length = %d, want 32", len(a))
	}
	if a == b {
		t.Fatal("tokens must differ")
	}
}

// ---------------------------------------------------------------------------
// 配额判定（纯函数）与覆盖钩子
// ---------------------------------------------------------------------------

func TestAttachmentQuotaErr(t *testing.T) {
	t.Cleanup(func() { SetAttachmentQuota(0, 0) })

	// 出厂默认：20 个 / 100MB。
	SetAttachmentQuota(0, 0)
	if err := attachmentQuotaErr(19, 90<<20, 1<<20); err != nil {
		t.Fatalf("under limits must pass: %v", err)
	}
	// 总字节恰好压线放行，超 1 字节拒绝。
	if err := attachmentQuotaErr(1, 100<<20-1, 1); err != nil {
		t.Fatalf("exactly at byte limit must pass: %v", err)
	}
	if err := attachmentQuotaErr(1, 100<<20-1, 2); err == nil {
		t.Fatal("over byte limit must be rejected")
	} else {
		var e *response.Error
		if !errors.As(err, &e) || e.Status != http.StatusBadRequest || e.Reason != AttachmentQuotaBytesReason ||
			e.Key != "ticket.attachment_quota_bytes" {
			t.Fatalf("byte quota error = %+v", e)
		}
	}
	// 条数到顶（20 个）再传就拒，与字节余量无关。
	if err := attachmentQuotaErr(20, 1<<20, 1); err == nil {
		t.Fatal("at count limit must be rejected")
	} else {
		var e *response.Error
		if !errors.As(err, &e) || e.Reason != AttachmentQuotaCountReason || e.Key != "ticket.attachment_quota_count" {
			t.Fatalf("count quota error = %+v", e)
		}
	}
	// 覆盖钩子：收紧后越界即拒；非法值（<=0）回到出厂默认。
	SetAttachmentQuota(3, 5<<20)
	if err := attachmentQuotaErr(3, 0, 1); err == nil || err.(*response.Error).Reason != AttachmentQuotaCountReason {
		t.Fatalf("custom count limit not applied: %v", err)
	}
	if err := attachmentQuotaErr(2, 4<<20, 2<<20); err == nil || err.(*response.Error).Reason != AttachmentQuotaBytesReason {
		t.Fatalf("custom byte limit not applied: %v", err)
	}
	SetAttachmentQuota(-1, 0)
	if err := attachmentQuotaErr(19, 0, 1); err != nil {
		t.Fatalf("invalid quota must fall back to defaults: %v", err)
	}
}

// ---------------------------------------------------------------------------
// 入口层（gin + sqlmock + 本地存储）：配额拒绝、删除守卫、下载响应头
// ---------------------------------------------------------------------------

// pngBody 是能过 http.DetectContentType 嗅探的最小合法 PNG 头。
func pngBody() []byte {
	return []byte("\x89PNG\r\n\x1a\n\x00\x00\x00\rIHDR\x00\x00\x00\x01\x00\x00\x00\x01")
}

func attachmentEngine(h *handler) *gin.Engine {
	r := gin.New()
	r.POST("/tickets/:no/attachments", h.uploadAdmin)
	r.DELETE("/tickets/:no/attachments/:token", h.deleteAdmin)
	r.DELETE("/visitor/tickets/:no/attachments/:token", h.deleteVisitor)
	r.GET("/tickets/:no/attachments/:token", h.downloadAdmin)
	r.GET("/visitor/tickets/:no/attachments/:token", h.downloadVisitor)
	return r
}

// attachmentMockRow 造一行附件记录；id 与 ticketRow 的工单 id（1001）对齐。
func attachmentMockRow(token, uploaderType string, internal bool, mime, key string) *sqlmock.Rows {
	return sqlmock.NewRows([]string{
		"id", "token", "ticket_id", "uploader_type", "uploader_name", "internal",
		"name", "mime", "size", "storage_key", "created_at", "updated_at",
	}).AddRow(int64(2001), token, int64(1001), uploaderType, "visitor", internal,
		"shot.png", mime, int64(len(pngBody())), key, testNow, testNow)
}

func quotaRows(cnt, total int64) *sqlmock.Rows {
	return sqlmock.NewRows([]string{"cnt", "total"}).AddRow(cnt, total)
}

func uploadPNGRequest(t *testing.T, ticketNo string) *http.Request {
	t.Helper()
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	fw, err := mw.CreateFormFile("file", "shot.png")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := fw.Write(pngBody()); err != nil {
		t.Fatal(err)
	}
	if err := mw.Close(); err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, "/tickets/"+ticketNo+"/attachments", &buf)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	return req
}

func countStoredFiles(t *testing.T, root string) int {
	t.Helper()
	n := 0
	err := filepath.WalkDir(root, func(_ string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() {
			n++
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return n
}

// 配额超限必须在落存储之前拒绝：存储目录里不能留下孤儿对象。
func TestUploadQuotaRejectionBeforePut(t *testing.T) {
	t.Cleanup(func() { SetAttachmentQuota(0, 0) })
	cases := []struct {
		name       string
		setQuota   func()
		cnt, total int64
		wantReason string
		wantKey    string
	}{
		{"count", func() { SetAttachmentQuota(0, 0) }, 20, 1 << 20, AttachmentQuotaCountReason, "ticket.attachment_quota_count"},
		{"bytes", func() { SetAttachmentQuota(1000, 1<<20) }, 0, (1 << 20) - 1, AttachmentQuotaBytesReason, "ticket.attachment_quota_bytes"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			c.setQuota()
			db, m := mockDB(t)
			storeDir := t.TempDir()
			store, err := storage.NewLocal(storeDir)
			if err != nil {
				t.Fatal(err)
			}
			svc := NewService(db, nopLogger{})
			svc.SetStorage(store)
			h := &handler{svc: svc, store: store}

			m.ExpectQuery("SELECT \\* FROM `plg_ticket` WHERE ticket_no = \\?").
				WillReturnRows(ticketRow("TK1001", StatusOpen, 1))
			m.ExpectQuery("SELECT COUNT\\(\\*\\) AS cnt").WithArgs(int64(1001)).
				WillReturnRows(quotaRows(c.cnt, c.total))

			w := httptest.NewRecorder()
			attachmentEngine(h).ServeHTTP(w, uploadPNGRequest(t, "TK1001"))
			if w.Code != http.StatusBadRequest {
				t.Fatalf("status = %d body=%s", w.Code, w.Body.String())
			}
			if !strings.Contains(w.Body.String(), c.wantReason) {
				t.Fatalf("reason missing: %s", w.Body.String())
			}
			if !strings.Contains(w.Body.String(), c.wantKey) {
				t.Fatalf("message should carry i18n key %s: %s", c.wantKey, w.Body.String())
			}
			if n := countStoredFiles(t, storeDir); n != 0 {
				t.Fatalf("over-quota upload must not leave an object in storage, got %d", n)
			}
			if err := m.ExpectationsWereMet(); err != nil {
				t.Fatal(err)
			}
		})
	}
}

// 上传成功路径也要把配额两处检查（入口预检 + SaveAttachment 兜底）走全，
// 期望序列里少一次聚合查询用例就会红。
func TestUploadSuccessChecksQuotaTwice(t *testing.T) {
	db, m := mockDB(t)
	storeDir := t.TempDir()
	store, err := storage.NewLocal(storeDir)
	if err != nil {
		t.Fatal(err)
	}
	svc := NewService(db, nopLogger{})
	svc.SetStorage(store)
	h := &handler{svc: svc, store: store}

	m.ExpectQuery("SELECT \\* FROM `plg_ticket` WHERE ticket_no = \\?").
		WillReturnRows(ticketRow("TK1001", StatusOpen, 1))
	m.ExpectQuery("SELECT COUNT\\(\\*\\) AS cnt").WithArgs(int64(1001)).
		WillReturnRows(quotaRows(1, 1<<20))
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

	w := httptest.NewRecorder()
	attachmentEngine(h).ServeHTTP(w, uploadPNGRequest(t, "TK1001"))
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", w.Code, w.Body.String())
	}
	if countStoredFiles(t, storeDir) != 1 {
		t.Fatal("object should have landed in storage")
	}
	if err := m.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

// 下载响应头是内容嗅探防线的一部分：nosniff 与 CSP sandbox 缺一不可，图片内联、其余 attachment。
func TestDownloadSecurityHeaders(t *testing.T) {
	db, m := mockDB(t)
	store, err := storage.NewLocal(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	svc := NewService(db, nopLogger{})
	svc.SetStorage(store)
	h := &handler{svc: svc, store: store}
	key := "202506/pub-token.png"
	if err := store.Put(context.Background(), key, bytes.NewReader(pngBody()), int64(len(pngBody())), storage.PutOptions{ContentType: "image/png"}); err != nil {
		t.Fatal(err)
	}
	m.ExpectQuery("SELECT \\* FROM `plg_ticket` WHERE ticket_no = \\?").
		WillReturnRows(ticketRow("TK1001", StatusOpen, 1))
	m.ExpectQuery("SELECT \\* FROM `plg_ticket_attachment` WHERE token = \\? AND ticket_id = \\?").
		WithArgs("pub-token", int64(1001), int64(1)).
		WillReturnRows(attachmentMockRow("pub-token", ReplierReporter, false, "image/png", key))

	r := gin.New()
	r.GET("/tickets/:no/attachments/:token", h.downloadAdmin)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/tickets/TK1001/attachments/pub-token", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", w.Code, w.Body.String())
	}
	if got := w.Header().Get("X-Content-Type-Options"); got != "nosniff" {
		t.Errorf("X-Content-Type-Options = %q", got)
	}
	if got := w.Header().Get("Content-Security-Policy"); !strings.Contains(got, "sandbox") {
		t.Errorf("CSP = %q, want sandbox", got)
	}
	if got := w.Header().Get("Content-Disposition"); !strings.HasPrefix(got, "inline") {
		t.Errorf("image should render inline, got %q", got)
	}
	if err := m.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

// 客户视角的内部附件：下载与删除都按不存在处理，且不触发任何写 SQL。
func TestVisitorCannotSeeOrDeleteInternal(t *testing.T) {
	db, m := mockDB(t)
	store, err := storage.NewLocal(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	key := "202506/inner-token.txt"
	if err := store.Put(context.Background(), key, strings.NewReader("internal note"), 13, storage.PutOptions{ContentType: "text/plain"}); err != nil {
		t.Fatal(err)
	}
	svc := NewService(db, nopLogger{})
	svc.SetStorage(store)
	h := &handler{svc: svc, store: store}
	engine := attachmentEngine(h)

	for _, method := range []string{http.MethodGet, http.MethodDelete} {
		t.Run(method, func(t *testing.T) {
			m.ExpectQuery("SELECT \\* FROM `plg_ticket` WHERE ticket_no = \\?").
				WillReturnRows(ticketRow("TK1001", StatusOpen, 1))
			m.ExpectQuery("SELECT \\* FROM `plg_ticket_attachment` WHERE token = \\? AND ticket_id = \\?").
				WithArgs("inner-token", int64(1001), int64(1)).
				WillReturnRows(attachmentMockRow("inner-token", ReplierAgent, true, "text/plain", key))
			w := httptest.NewRecorder()
			req := httptest.NewRequest(method, "/visitor/tickets/TK1001/attachments/inner-token", nil)
			engine.ServeHTTP(w, req)
			if w.Code != http.StatusNotFound {
				t.Fatalf("status = %d body=%s", w.Code, w.Body.String())
			}
			// 到这里没有更多登记的期望：若实现越权往下走，DELETE/UPDATE 会撞上
			// sqlmock 的“not expected”错误改变响应，断言随之变红。
			if err := m.ExpectationsWereMet(); err != nil {
				t.Fatal(err)
			}
			if ok, err := store.Exists(context.Background(), key); err != nil || !ok {
				t.Fatalf("internal object must stay untouched: exists=%v err=%v", ok, err)
			}
		})
	}
}

// 客户也不能删坐席上传的公开附件（同 ticket 也不太行）：按不存在处理，行与对象都不动。
func TestVisitorCannotDeleteAgentAttachment(t *testing.T) {
	db, m := mockDB(t)
	store, err := storage.NewLocal(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	key := "202506/agent-token.png"
	if err := store.Put(context.Background(), key, bytes.NewReader(pngBody()), int64(len(pngBody())), storage.PutOptions{ContentType: "image/png"}); err != nil {
		t.Fatal(err)
	}
	svc := NewService(db, nopLogger{})
	svc.SetStorage(store)
	h := &handler{svc: svc, store: store}

	m.ExpectQuery("SELECT \\* FROM `plg_ticket` WHERE ticket_no = \\?").
		WillReturnRows(ticketRow("TK1001", StatusOpen, 1))
	m.ExpectQuery("SELECT \\* FROM `plg_ticket_attachment` WHERE token = \\? AND ticket_id = \\?").
		WithArgs("agent-token", int64(1001), int64(1)).
		WillReturnRows(attachmentMockRow("agent-token", ReplierAgent, false, "image/png", key))

	w := httptest.NewRecorder()
	attachmentEngine(h).ServeHTTP(w, httptest.NewRequest(http.MethodDelete, "/visitor/tickets/TK1001/attachments/agent-token", nil))
	if w.Code != http.StatusNotFound {
		t.Fatalf("status = %d body=%s", w.Code, w.Body.String())
	}
	if err := m.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
	if ok, _ := store.Exists(context.Background(), key); !ok {
		t.Fatal("agent attachment object must stay")
	}
}

// 坐席删除（含内部附件）：先删对象再删行，最后补一行 attachment_removed 动态。
func TestDeleteAgentRemovesObjectRowAndRecordsActivity(t *testing.T) {
	db, m := mockDB(t)
	store, err := storage.NewLocal(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	key := "202506/gone-token.png"
	if err := store.Put(context.Background(), key, bytes.NewReader(pngBody()), int64(len(pngBody())), storage.PutOptions{ContentType: "image/png"}); err != nil {
		t.Fatal(err)
	}
	svc := NewService(db, nopLogger{})
	svc.SetStorage(store)
	h := &handler{svc: svc, store: store}

	m.ExpectQuery("SELECT \\* FROM `plg_ticket` WHERE ticket_no = \\?").
		WillReturnRows(ticketRow("TK1001", StatusOpen, 1))
	m.ExpectQuery("SELECT \\* FROM `plg_ticket_attachment` WHERE token = \\? AND ticket_id = \\?").
		WithArgs("gone-token", int64(1001), int64(1)).
		WillReturnRows(attachmentMockRow("gone-token", ReplierAgent, true, "image/png", key))
	m.ExpectBegin()
	m.ExpectExec("DELETE FROM `plg_ticket_attachment` WHERE id = \\?").WithArgs(int64(2001)).
		WillReturnResult(sqlmock.NewResult(0, 1))
	m.ExpectCommit()
	m.ExpectBegin()
	m.ExpectExec("INSERT INTO `plg_ticket_activity`").WillReturnResult(sqlmock.NewResult(1, 1))
	m.ExpectCommit()

	w := httptest.NewRecorder()
	attachmentEngine(h).ServeHTTP(w, httptest.NewRequest(http.MethodDelete, "/tickets/TK1001/attachments/gone-token", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", w.Code, w.Body.String())
	}
	// 对象必须真的没了——只删行不删对象的改法会在这里红。
	if ok, err := store.Exists(context.Background(), key); err != nil || ok {
		t.Fatalf("storage object must be deleted: exists=%v err=%v", ok, err)
	}
	if err := m.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

// failDeleteStore 模拟“对象删不掉”的存储（本地盘只读、S3 5xx 都属此类）。
type failDeleteStore struct {
	storage.Storage
	err error
}

func (f failDeleteStore) Delete(context.Context, string) error { return f.err }

// 删不掉对象时不许假装成功：报 500（带可定位 key）， DELETE SQL 一步都不许发出，行保持原样可重试。
func TestDeleteFailsWhenObjectCannotBeRemoved(t *testing.T) {
	db, m := mockDB(t)
	inner, err := storage.NewLocal(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	key := "202506/stuck-token.png"
	if err := inner.Put(context.Background(), key, bytes.NewReader(pngBody()), int64(len(pngBody())), storage.PutOptions{ContentType: "image/png"}); err != nil {
		t.Fatal(err)
	}
	svc := NewService(db, nopLogger{})
	svc.SetStorage(failDeleteStore{Storage: inner, err: errors.New("disk on fire")})
	h := &handler{svc: svc, store: inner}

	m.ExpectQuery("SELECT \\* FROM `plg_ticket` WHERE ticket_no = \\?").
		WillReturnRows(ticketRow("TK1001", StatusOpen, 1))
	m.ExpectQuery("SELECT \\* FROM `plg_ticket_attachment` WHERE token = \\? AND ticket_id = \\?").
		WithArgs("stuck-token", int64(1001), int64(1)).
		WillReturnRows(attachmentMockRow("stuck-token", ReplierReporter, false, "image/png", key))
	// 故意不登记 DELETE：若实现吞掉存储错误继续删行，sqlmock 会报 not expected，用例变红。

	w := httptest.NewRecorder()
	attachmentEngine(h).ServeHTTP(w, httptest.NewRequest(http.MethodDelete, "/tickets/TK1001/attachments/stuck-token", nil))
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d body=%s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "ticket.attachment_delete_failed") {
		t.Fatalf("body should carry i18n key ticket.attachment_delete_failed: %s", w.Body.String())
	}
	if err := m.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
	if ok, _ := inner.Exists(context.Background(), key); !ok {
		t.Fatal("object must stay when delete failed (retry needs it)")
	}
}

// 存储未配置（store=nil）时删除必须 503，而不是把行删掉留个指向空洞的记录。
func TestDeleteWithoutStorageIsUnavailable(t *testing.T) {
	db, m := mockDB(t)
	svc := NewService(db, nopLogger{})
	h := &handler{svc: svc}
	m.ExpectQuery("SELECT \\* FROM `plg_ticket` WHERE ticket_no = \\?").
		WillReturnRows(ticketRow("TK1001", StatusOpen, 1))
	m.ExpectQuery("SELECT \\* FROM `plg_ticket_attachment` WHERE token = \\? AND ticket_id = \\?").
		WithArgs("x-token", int64(1001), int64(1)).
		WillReturnRows(attachmentMockRow("x-token", ReplierAgent, false, "image/png", "202506/x.png"))
	w := httptest.NewRecorder()
	attachmentEngine(h).ServeHTTP(w, httptest.NewRequest(http.MethodDelete, "/tickets/TK1001/attachments/x-token", nil))
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d body=%s", w.Code, w.Body.String())
	}
	if err := m.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

// SaveAttachment 的兜底配额：超限在 INSERT 之前拒绝（入口预检与插入之间的并发窗口由这里收）。
func TestSaveAttachmentQuotaBackstop(t *testing.T) {
	t.Cleanup(func() { SetAttachmentQuota(0, 0) })
	SetAttachmentQuota(5, 1<<20)
	db, m := mockDB(t)
	svc := NewService(db, nopLogger{})
	m.ExpectQuery("SELECT \\* FROM `plg_ticket` WHERE ticket_no = \\?").
		WillReturnRows(ticketRow("TK1001", StatusOpen, 1))
	m.ExpectQuery("SELECT COUNT\\(\\*\\) AS cnt").WithArgs(int64(1001)).
		WillReturnRows(quotaRows(0, 1<<20))
	_, err := svc.SaveAttachment(context.Background(), "TK1001", ReplierReporter, "visitor", false, "a.png", "image/png", 2<<20, "202506/a.png")
	var e *response.Error
	if !errors.As(err, &e) || e.Reason != AttachmentQuotaBytesReason {
		t.Fatalf("SaveAttachment must reject over byte quota, got %v", err)
	}
	if err := m.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

// ---------------------------------------------------------------------------
// 真实 MySQL：配额、删除失败语义、可见性守卫在真引擎上的行为。
// 守卫：默认跳过；NEXADMIN_IT_MYSQL_DSN 设置后执行。一次性库 nexadmin_ticketatt_<纳秒>，结束 DROP。
// ---------------------------------------------------------------------------

func TestITMySQLAttachmentQuotaAndDelete(t *testing.T) {
	dsn := os.Getenv("NEXADMIN_IT_MYSQL_DSN")
	if dsn == "" {
		t.Skip("NEXADMIN_IT_MYSQL_DSN not set; skipping real-MySQL attachment test")
	}
	if !strings.Contains(dsn, "multiStatements=true") {
		if strings.Contains(dsn, "?") {
			dsn += "&multiStatements=true"
		} else {
			dsn += "?multiStatements=true"
		}
	}
	if !strings.Contains(dsn, "parseTime=true") {
		dsn += "&parseTime=true&loc=UTC"
	}

	admin, err := sql.Open("mysql", dsn)
	if err != nil {
		t.Fatalf("open admin conn: %v", err)
	}
	if err := admin.Ping(); err != nil {
		t.Fatalf("mysql not reachable: %v", err)
	}
	dbName := fmt.Sprintf("nexadmin_ticketatt_%d", time.Now().UnixNano())
	if _, err := admin.Exec("CREATE DATABASE `" + dbName + "` CHARACTER SET utf8mb4"); err != nil {
		t.Fatalf("create throwaway db: %v", err)
	}
	var gdb *gorm.DB
	// 收尾顺序：关业务连接池 → 删库 → 关管理连接（同 it_mysql_test.go 的教训）。
	t.Cleanup(func() {
		if gdb != nil {
			if sqlDB, err := gdb.DB(); err == nil {
				_ = sqlDB.Close()
			}
		}
		if _, err := admin.Exec("DROP DATABASE IF EXISTS `" + dbName + "`"); err != nil {
			t.Errorf("drop throwaway db %s: %v", dbName, err)
		}
		_ = admin.Close()
	})
	gormDSN := strings.Replace(dsn, ")/", ")/"+dbName+"", 1)
	gdb, err = gorm.Open(mysql.Open(gormDSN), &gorm.Config{})
	if err != nil {
		t.Fatalf("open gorm: %v", err)
	}
	runTicketMigrations(t, gdb)

	ctx := context.Background()
	storeDir := t.TempDir()
	store, err := storage.NewLocal(storeDir)
	if err != nil {
		t.Fatal(err)
	}
	svc := NewService(gdb, nopLogger{})
	svc.SetStorage(store)

	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	rowCount := func(where string, args ...any) int64 {
		t.Helper()
		var n int64
		must(gdb.Raw("SELECT COUNT(*) FROM plg_ticket_attachment WHERE "+where, args...).Scan(&n).Error)
		return n
	}

	// 放对象 + 登记行：走真实上传顺序（先落存储再插行）。token 列由 SaveAttachment 随机生成，
	// 定位一律用返回值里的 att.Token；对象键用 keyOf 拼。
	keyOf := func(tag string) string { return "202506/" + tag + ".bin" }
	putAttachment := func(ticketNo, uploaderType string, internal bool, tag string, size int64) *Attachment {
		t.Helper()
		key := keyOf(tag)
		must(store.Put(ctx, key, bytes.NewReader(make([]byte, size)), size, storage.PutOptions{ContentType: "application/octet-stream"}))
		att, err := svc.SaveAttachment(ctx, ticketNo, uploaderType, "someone", internal, tag+".bin", "application/octet-stream", size, key)
		must(err)
		return att
	}

	tk, err := svc.Create(ctx, CreateInput{Title: "quota and delete", Content: "x", ReporterType: ReplierReporter, ReporterName: "visitor"})
	must(err)
	no := tk.TicketNo

	// 配额计数包含内部附件：4 个（报告人公开 / 坐席内部 / 坐席公开 / 坐席公开留存）。
	attRpt := putAttachment(no, ReplierReporter, false, "att-rpt", 10)
	attInt := putAttachment(no, ReplierAgent, true, "att-int", 10)
	attAg := putAttachment(no, ReplierAgent, false, "att-ag", 10)
	attKeep := putAttachment(no, ReplierAgent, false, "att-keep", 10)
	cnt, total, err := svc.AttachmentUsage(ctx, tk.ID)
	must(err)
	if cnt != 4 || total != 40 {
		t.Fatalf("usage = (%d, %d), want (4, 40)", cnt, total)
	}

	t.Cleanup(func() { SetAttachmentQuota(0, 0) })

	// 条数配额：上限 4 时第 5 个被拒，不许插行。
	SetAttachmentQuota(4, 1<<30)
	if _, err := svc.SaveAttachment(ctx, no, ReplierReporter, "visitor", false, "x.bin", "application/octet-stream", 1, keyOf("att-x")); err == nil {
		t.Fatal("over count quota must be rejected")
	} else {
		var e *response.Error
		if !errors.As(err, &e) || e.Reason != AttachmentQuotaCountReason {
			t.Fatalf("count quota error = %v", err)
		}
	}
	if n := rowCount("ticket_id = ?", int64(tk.ID)); n != 4 {
		t.Fatalf("rejected upload must not insert a row, got %d", n)
	}

	// 字节配额：已用 40B、上限 40B 时再来 1 字节就拒（条数远未到顶）。
	SetAttachmentQuota(1000, 40)
	if _, err := svc.SaveAttachment(ctx, no, ReplierReporter, "visitor", false, "y.bin", "application/octet-stream", 1, keyOf("att-y")); err == nil {
		t.Fatal("over byte quota must be rejected")
	} else {
		var e *response.Error
		if !errors.As(err, &e) || e.Reason != AttachmentQuotaBytesReason {
			t.Fatalf("byte quota error = %v", err)
		}
	}
	SetAttachmentQuota(0, 0)

	// 客户守卫：坐席公开附件与内部附件对客户都是 404，行与对象原封不动。
	if _, _, err := svc.DeleteAttachment(ctx, no, attAg.Token, true); err == nil {
		t.Fatal("visitor must not delete an agent attachment")
	} else {
		var e *response.Error
		if !errors.As(err, &e) || e.Status != http.StatusNotFound {
			t.Fatalf("visitor delete agent attachment error = %v", err)
		}
	}
	if _, _, err := svc.DeleteAttachment(ctx, no, attInt.Token, true); err == nil {
		t.Fatal("visitor must not see or delete an internal attachment")
	}
	if n := rowCount("ticket_id = ?", int64(tk.ID)); n != 4 {
		t.Fatalf("guarded deletes must keep all rows, got %d", n)
	}

	// 客户删自己那篇：成功，行与对象一起消失。
	att, delTk, err := svc.DeleteAttachment(ctx, no, attRpt.Token, true)
	must(err)
	if att.StorageKey != keyOf("att-rpt") || delTk.TicketNo != no {
		t.Fatalf("deleted unexpected attachment: %+v ticket=%+v", att, delTk)
	}
	if n := rowCount("storage_key = ?", keyOf("att-rpt")); n != 0 {
		t.Fatalf("row must be gone, got %d", n)
	}
	if ok, err := store.Exists(ctx, keyOf("att-rpt")); err != nil || ok {
		t.Fatalf("object must be gone: exists=%v err=%v", ok, err)
	}

	// 坐席删内部附件 + 动态落表：走完整入口（gin handler），真库上验证审计行。
	h := &handler{svc: svc, store: svc.Storage()}
	w := httptest.NewRecorder()
	attachmentEngine(h).ServeHTTP(w, httptest.NewRequest(http.MethodDelete, "/tickets/TKNOTHERE/attachments/"+attInt.Token, nil))
	// 工单号错 → 404（入口按「工单号 + token」双要素定位，二者都不可枚举）。
	if w.Code != http.StatusNotFound {
		t.Fatalf("wrong ticket no must 404, got %d body=%s", w.Code, w.Body.String())
	}
	w = httptest.NewRecorder()
	engine := gin.New()
	engine.DELETE("/tickets/:no/attachments/:token", h.deleteAdmin)
	engine.ServeHTTP(w, httptest.NewRequest(http.MethodDelete, "/tickets/"+no+"/attachments/"+attInt.Token, nil))
	if w.Code != http.StatusOK {
		t.Fatalf("agent delete internal attachment: status=%d body=%s", w.Code, w.Body.String())
	}
	if ok, err := store.Exists(ctx, keyOf("att-int")); err != nil || ok {
		t.Fatalf("internal object must be deleted: exists=%v err=%v", ok, err)
	}
	var acts int64
	must(gdb.Raw("SELECT COUNT(*) FROM plg_ticket_activity WHERE ticket_id = ? AND action = ?", int64(tk.ID), ActAttachmentRemoved).Scan(&acts).Error)
	if acts != 1 {
		t.Fatalf("attachment_removed activity rows = %d, want 1", acts)
	}

	// 删不掉对象 → 报错且行必须留下；恢复存储后重试收敛成功。
	svc.SetStorage(failDeleteStore{Storage: store, err: errors.New("storage offline")})
	if _, _, err := svc.DeleteAttachment(ctx, no, attAg.Token, false); err == nil {
		t.Fatal("delete must fail when the object cannot be removed")
	} else {
		var e *response.Error
		if !errors.As(err, &e) || e.Status != http.StatusInternalServerError || e.Key != "ticket.attachment_delete_failed" {
			t.Fatalf("delete failure error = %v", err)
		}
	}
	if n := rowCount("storage_key = ?", keyOf("att-ag")); n != 1 {
		t.Fatalf("row must survive a failed object delete, got %d", n)
	}
	svc.SetStorage(store)
	if _, _, err := svc.DeleteAttachment(ctx, no, attAg.Token, false); err != nil {
		t.Fatalf("retry after transient storage failure must succeed: %v", err)
	}
	if n := rowCount("storage_key = ?", keyOf("att-ag")); n != 0 {
		t.Fatalf("retry must converge, got %d rows", n)
	}

	// 对象已被外部抹掉（运维清了目录）：删除仍成功——「对象不存在不算错」，行不能悬着。
	must(store.Delete(ctx, attKeep.StorageKey))
	if _, _, err := svc.DeleteAttachment(ctx, no, attKeep.Token, false); err != nil {
		t.Fatalf("delete with externally removed object must still succeed: %v", err)
	}
	if n := rowCount("ticket_id = ?", int64(tk.ID)); n != 0 {
		t.Fatalf("all attachment rows should be gone, got %d", n)
	}

	// 入口预检在真库上同样不落孤儿对象：配额压到 1 个（已有 1 个新附件），上传被拒后目录里不多文件。
	putAttachment(no, ReplierReporter, false, "att-last", 10)
	beforeFiles := countStoredFiles(t, storeDir)
	SetAttachmentQuota(1, 1<<30)
	r := gin.New()
	r.POST("/tickets/:no/attachments", h.uploadAdmin)
	w = httptest.NewRecorder()
	r.ServeHTTP(w, uploadPNGRequest(t, no))
	if w.Code != http.StatusBadRequest || !strings.Contains(w.Body.String(), AttachmentQuotaCountReason) {
		t.Fatalf("over-quota upload at entry: status=%d body=%s", w.Code, w.Body.String())
	}
	if countStoredFiles(t, storeDir) != beforeFiles {
		t.Fatal("rejected upload must not put an object into storage")
	}
	SetAttachmentQuota(0, 0)
}
