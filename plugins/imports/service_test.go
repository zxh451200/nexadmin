package imports

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/gin-gonic/gin"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"

	"nexadmin/core/event"
	"nexadmin/core/response"
	"nexadmin/core/tenant"
	"nexadmin/pkg/idgen"
)

// skipCtx 是无租户上下文的测试 ctx(TenantModel 写入时 tenant_id=0)。
func skipCtx() context.Context { return tenant.Skip(context.Background()) }

func TestMain(m *testing.M) {
	if err := idgen.Init(1); err != nil {
		panic(err)
	}
	gin.SetMode(gin.TestMode)
	m.Run()
}

var testNow = time.Date(2025, 6, 1, 0, 0, 0, 0, time.UTC)

func mockDB(t *testing.T) (*gorm.DB, sqlmock.Sqlmock) {
	t.Helper()
	sqlDB, mk, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	db, err := gorm.Open(mysql.New(mysql.Config{Conn: sqlDB, SkipInitializeWithVersion: true}), &gorm.Config{
		DisableAutomaticPing: true,
		NowFunc:              func() time.Time { return testNow },
	})
	if err != nil {
		t.Fatal(err)
	}
	return db, mk
}

type nopLogger struct{}

func (nopLogger) Warn(string, ...any)  {}
func (nopLogger) Error(string, ...any) {}

// testSource 是测试导入源:title/price 两列,title 必填。
func testSource(name string, handler func(ctx context.Context, row int, r map[string]string) error) Source {
	return Source{
		Name: name, Title: "测试导入",
		Columns: []Column{
			{Key: "title", Label: "名称", Required: true, Example: "测试商品"},
			{Key: "price", Label: "价格(元)", Example: "9.90"},
		},
		Handler: handler,
	}
}

const testCSV = "名称,价格(元)\r\n商品A,9.90\r\n,1.00\r\n商品C,1.00\r\n"

// Run:2 行成功 + 1 行必填失败;事件带计数。
func TestRunRowIsolation(t *testing.T) {
	if err := RegisterSource(testSource("t1", func(context.Context, int, map[string]string) error { return nil })); err != nil {
		if !strings.Contains(err.Error(), "already registered") {
			t.Fatal(err)
		}
	}
	db, mk := mockDB(t)
	s := NewService(db, nopLogger{})

	events := make(chan event.Event, 4)
	event.Subscribe(event.SubscriberFunc(func(e event.Event) {
		if e.Name == EventCompleted {
			events <- e
		}
	}))

	mk.ExpectBegin()
	mk.ExpectExec("INSERT INTO `plg_imports_job`").WillReturnResult(sqlmock.NewResult(1, 1))
	mk.ExpectCommit()

	v, err := s.Run(skipCtx(), RunInput{
		Source: "t1", FileName: "test.csv", Content: []byte(testCSV),
	}, "7")
	if err != nil {
		t.Fatal(err)
	}
	if v.OkRows != 2 || v.FailRows != 1 || v.TotalRows != 3 {
		t.Fatalf("ok=%d fail=%d total=%d", v.OkRows, v.FailRows, v.TotalRows)
	}
	if len(v.Errors) != 1 || v.Errors[0].Row != 2 || v.Errors[0].Key != "title" {
		t.Fatalf("unexpected errors %+v", v.Errors)
	}
	select {
	case ev := <-events:
		data, ok := ev.Data.(EventData)
		if !ok || data.OkRows != 2 || data.FailRows != 1 {
			t.Fatalf("unexpected event %+v", ev.Data)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("imports.completed not published")
	}
	if err := mk.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

// 模板 CSV:表头 + 示例行,Excel 可直接打开(BOM)。
func TestTemplateAndParse(t *testing.T) {
	src := testSource("t2", nil)
	tpl := BuildTemplateCSV(context.Background(), src)
	rows, err := ParseUpload(src, tpl, "template.csv")
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0]["title"] != "测试商品" || rows[0]["price"] != "9.90" {
		t.Fatalf("unexpected rows %+v", rows)
	}
}

// 模板 XLSX:生成后按同名上传解析,应与 CSV 模板解出同一批行。
func TestTemplateXLSXRoundTrip(t *testing.T) {
	src := testSource("t2x", nil)
	body, err := BuildTemplateXLSX(context.Background(), src)
	if err != nil {
		t.Fatal(err)
	}
	if len(body) < 2 || string(body[:2]) != "PK" {
		t.Fatalf("not an xlsx (zip) stream: %q", body[:min(8, len(body))])
	}
	rows, err := ParseUpload(src, body, "template.xlsx")
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0]["title"] != "测试商品" || rows[0]["price"] != "9.90" {
		t.Fatalf("unexpected rows %+v", rows)
	}
}

// 表头按 Label 识别(带"(必填)"后缀也认);未知表头列被忽略。
func TestParseHeaderVariants(t *testing.T) {
	src := testSource("t3", nil)
	csv := "\uFEFF名称(必填),价格(元),未知列\r\nA,1.00,x\r\n"
	rows, err := ParseUpload(src, []byte(csv), "upload.csv")
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0]["title"] != "A" || rows[0]["price"] != "1.00" {
		t.Fatalf("unexpected rows %+v", rows)
	}
	if _, ok := rows[0]["未知列"]; ok {
		t.Fatal("unknown column should be ignored")
	}
}

// 缺必填列(表头没有「名称」)→ 整批 400,不静默丢数据。
func TestRunMissingRequiredColumn(t *testing.T) {
	if err := RegisterSource(testSource("t4", func(context.Context, int, map[string]string) error { return nil })); err != nil {
		if !strings.Contains(err.Error(), "already registered") {
			t.Fatal(err)
		}
	}
	db, _ := mockDB(t)
	s := NewService(db, nopLogger{})

	csv := "价格(元)\r\n1.00\r\n"
	_, err := s.Run(skipCtx(), RunInput{Source: "t4", FileName: "x.csv", Content: []byte(csv)}, "7")
	e, ok := err.(*response.Error)
	if !ok || e.Status != http.StatusBadRequest {
		t.Fatalf("want 400, got %v", err)
	}
}

// Handler panic 只算该行失败。
func TestRunHandlerPanicIsolated(t *testing.T) {
	if err := RegisterSource(testSource("t5", func(ctx context.Context, row int, r map[string]string) error {
		if r["title"] == "boom" {
			panic("boom")
		}
		return nil
	})); err != nil {
		if !strings.Contains(err.Error(), "already registered") {
			t.Fatal(err)
		}
	}
	db, mk := mockDB(t)
	s := NewService(db, nopLogger{})

	mk.ExpectBegin()
	mk.ExpectExec("INSERT INTO `plg_imports_job`").WillReturnResult(sqlmock.NewResult(1, 1))
	mk.ExpectCommit()

	csv := "名称,价格(元)\r\nok1,1.00\r\nboom,2.00\r\nok2,3.00\r\n"
	v, err := s.Run(skipCtx(), RunInput{Source: "t5", FileName: "x.csv", Content: []byte(csv)}, "7")
	if err != nil {
		t.Fatal(err)
	}
	if v.OkRows != 2 || v.FailRows != 1 {
		t.Fatalf("ok=%d fail=%d", v.OkRows, v.FailRows)
	}
	if len(v.Errors) != 1 || !strings.Contains(v.Errors[0].Message, "panicked") {
		t.Fatalf("unexpected errors %+v", v.Errors)
	}
}

// Handler 业务错误:错误消息进报告。
func TestRunHandlerErrorReported(t *testing.T) {
	if err := RegisterSource(testSource("t6", func(ctx context.Context, row int, r map[string]string) error {
		return errors.New("币种不支持")
	})); err != nil {
		if !strings.Contains(err.Error(), "already registered") {
			t.Fatal(err)
		}
	}
	db, mk := mockDB(t)
	s := NewService(db, nopLogger{})

	mk.ExpectBegin()
	mk.ExpectExec("INSERT INTO `plg_imports_job`").WillReturnResult(sqlmock.NewResult(1, 1))
	mk.ExpectCommit()

	csv := "名称,价格(元)\r\nA,1.00\r\n"
	v, err := s.Run(skipCtx(), RunInput{Source: "t6", FileName: "x.csv", Content: []byte(csv)}, "7")
	if err != nil {
		t.Fatal(err)
	}
	if v.FailRows != 1 || !strings.Contains(v.Errors[0].Message, "币种不支持") {
		t.Fatalf("unexpected %+v", v)
	}
}

// 未知来源 400;空文件 400。
func TestRunValidation(t *testing.T) {
	db, _ := mockDB(t)
	s := NewService(db, nopLogger{})

	if _, err := s.Run(skipCtx(), RunInput{Source: "nope", FileName: "x", Content: []byte("a,b\r\n1,2")}, "7"); err == nil {
		t.Fatal("unknown source accepted")
	}
	if _, err := s.Run(skipCtx(), RunInput{Source: "t1", FileName: "x", Content: nil}, "7"); err == nil {
		t.Fatal("empty file accepted")
	}
}

// 超过 SyncMaxRows 的大文件走异步:落 pending 台账,后台协程处理。
func TestRunAsyncLargeFile(t *testing.T) {
	if err := RegisterSource(Source{
		Name: "t_async", Title: "异步测试",
		Columns: []Column{{Key: "title", Label: "名称", Required: true}},
		Handler: func(ctx context.Context, row int, r map[string]string) error { return nil },
	}); err != nil && !strings.Contains(err.Error(), "already registered") {
		t.Fatal(err)
	}
	db, mk := mockDB(t)
	s := NewService(db, nopLogger{})

	// 构造超过 SyncMaxRows 行的 CSV
	var sb strings.Builder
	sb.WriteString("名称\r\n")
	for i := 0; i < SyncMaxRows+10; i++ {
		sb.WriteString("row" + strings.Repeat("x", 1) + "\r\n")
	}

	mk.ExpectBegin()
	mk.ExpectExec("INSERT INTO `plg_imports_job`").WillReturnResult(sqlmock.NewResult(1, 1))
	mk.ExpectCommit()
	// 后台协程里的 UPDATE(status→running) 和最终 UPDATE(done) — 因为是 goroutine,不精确匹配,
	// 只验证 Run 返回的 status 是 pending。
	mk.ExpectExec("UPDATE `plg_imports_job`").WillReturnResult(sqlmock.NewResult(1, 1)).WillDelayFor(time.Millisecond)
	mk.ExpectExec("UPDATE `plg_imports_job`").WillReturnResult(sqlmock.NewResult(1, 1))

	v, err := s.Run(skipCtx(), RunInput{Source: "t_async", FileName: "big.csv", Content: []byte(sb.String())}, "7")
	if err != nil {
		t.Fatal(err)
	}
	if v.Status != StatusPending {
		t.Fatalf("status = %s, want pending", v.Status)
	}
	if v.TotalRows != SyncMaxRows+10 {
		t.Fatalf("total = %d, want %d", v.TotalRows, SyncMaxRows+10)
	}
	time.Sleep(200 * time.Millisecond) // 给后台协程一点时间
}

// 删除台账：done 可以删，pending/running 拒绝（后台协程还在写这一行）。
func TestDeleteJob(t *testing.T) {
	db, mk := mockDB(t)
	s := NewService(db, nopLogger{})

	mk.ExpectQuery("SELECT .+plg_imports_job").
		WillReturnRows(sqlmock.NewRows([]string{"id", "status"}).AddRow(int64(7), StatusDone))
	mk.ExpectBegin()
	mk.ExpectExec("DELETE FROM `plg_imports_job`").WillReturnResult(sqlmock.NewResult(0, 1))
	mk.ExpectCommit()
	if err := s.DeleteJob(skipCtx(), idgen.ID(7)); err != nil {
		t.Fatalf("已完成的任务应可删除: %v", err)
	}

	mk.ExpectQuery("SELECT .+plg_imports_job").
		WillReturnRows(sqlmock.NewRows([]string{"id", "status"}).AddRow(int64(8), StatusRunning))
	err := s.DeleteJob(skipCtx(), idgen.ID(8))
	var respErr *response.Error
	if !errors.As(err, &respErr) || respErr.Status != http.StatusConflict {
		t.Fatalf("执行中的任务应被拒绝(409)，got %v", err)
	}
	if err := mk.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}
