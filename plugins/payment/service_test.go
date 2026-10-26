package payment

import (
	"context"
	"encoding/json"
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
	"nexadmin/pkg/idgen"
)

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
	sqlDB, m, err := sqlmock.New()
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
	return db, m
}

func nopLog() logger { return nopLogger{} }

type nopLogger struct{}

func (nopLogger) Info(string, ...any)  {}
func (nopLogger) Warn(string, ...any)  {}
func (nopLogger) Error(string, ...any) {}

func channelRow(code string, cfg string) *sqlmock.Rows {
	return sqlmock.NewRows([]string{
		"id", "code", "name", "enabled", "sort", "config_json", "created_at", "updated_at",
	}).AddRow(1, code, "channel "+code, true, 0, cfg, testNow, testNow)
}

func orderRow(no, status string, amount, refunded int64) *sqlmock.Rows {
	return sqlmock.NewRows([]string{
		"id", "payment_no", "biz_type", "biz_no", "subject", "amount_minor", "currency",
		"channel", "channel_txn_id", "status", "fail_reason", "refunded_minor",
		"paid_at", "expire_at", "created_at", "updated_at",
	}).AddRow(
		1001, no, "trade_group", "G-1", "test order", amount, "CNY",
		"mock", "", status, "", refunded,
		nil, nil, testNow, testNow,
	)
}

func TestSettleable(t *testing.T) {
	for _, st := range []string{StatusPaid, StatusRefunding, StatusPartiallyRefunded} {
		if !Settleable(st) {
			t.Fatalf("Settleable(%q) = false", st)
		}
	}
	for _, st := range []string{StatusPending, StatusFailed, StatusClosed, StatusRefunded} {
		if Settleable(st) {
			t.Fatalf("Settleable(%q) = true", st)
		}
	}
}

func TestMockVerifyCallback(t *testing.T) {
	cfg := map[string]string{"secret": "s3cret"}
	body, _ := json.Marshal(mockCallbackBody{PaymentNo: "PAY1", Result: "success", Timestamp: time.Now().Unix()})
	nowTs := time.Now().Unix()
	ts := formatID(nowTs)
	sig := SignMockCallback("s3cret", body, nowTs)

	okRes, err := mockProvider{}.VerifyCallback(CallbackInput{
		Channel: "mock", Body: body,
		Headers: map[string]string{"x-timestamp": ts, "x-signature": sig},
	}, cfg)
	if err != nil || !okRes.Paid || okRes.PaymentNo != "PAY1" {
		t.Fatalf("valid callback rejected: %+v err=%v", okRes, err)
	}

	// tampered body must fail
	bad := append([]byte(nil), body...)
	bad[1] = 'X'
	if _, err := (mockProvider{}).VerifyCallback(CallbackInput{
		Channel: "mock", Body: bad,
		Headers: map[string]string{"x-timestamp": ts, "x-signature": sig},
	}, cfg); err == nil {
		t.Fatal("tampered body accepted")
	}

	// stale timestamp must fail (replay window)
	oldUnix := time.Now().Add(-time.Hour).Unix()
	old := formatID(oldUnix)
	oldBody, _ := json.Marshal(mockCallbackBody{PaymentNo: "PAY1", Result: "success", Timestamp: oldUnix})
	if _, err := (mockProvider{}).VerifyCallback(CallbackInput{
		Channel: "mock", Body: oldBody,
		Headers: map[string]string{"x-timestamp": old, "x-signature": SignMockCallback("s3cret", oldBody, oldUnix)},
	}, cfg); err == nil {
		t.Fatal("stale callback accepted")
	}

	// missing headers must fail
	if _, err := (mockProvider{}).VerifyCallback(CallbackInput{Channel: "mock", Body: body}, cfg); err == nil {
		t.Fatal("unsigned callback accepted")
	}
}

func TestMockCreatePayURL(t *testing.T) {
	p, err := mockProvider{}.Create(context.Background(), PayRequest{PaymentNo: "PAY9", AmountMinor: 100, Currency: "CNY"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(p.PayURL, "/plugins/payment/mock/cashier") || !strings.Contains(p.PayURL, "PAY9") {
		t.Fatalf("unexpected payURL %q", p.PayURL)
	}
}

// Create is idempotent on (biz_type, biz_no): an existing pending order is
// returned with a freshly derived pay URL, no INSERT happens.
func TestCreateIdempotent(t *testing.T) {
	db, m := mockDB(t)
	s := NewService(db, nopLog())

	m.ExpectQuery("SELECT \\* FROM `plg_payment_order` WHERE biz_type = \\? AND biz_no = \\?").
		WillReturnRows(orderRow("PAY1", StatusPending, 500, 0))
	m.ExpectQuery("SELECT \\* FROM `plg_payment_channel` WHERE code = \\?").
		WillReturnRows(channelRow("mock", `{"secret":"k"}`))

	o, payURL, err := s.Create(context.Background(), CreateInput{
		BizType: "trade_group", BizNo: "G-1", AmountMinor: 500, Currency: "CNY", Subject: "t",
	})
	if err != nil {
		t.Fatal(err)
	}
	if o.PaymentNo != "PAY1" || o.Status != StatusPending {
		t.Fatalf("unexpected order %+v", o)
	}
	if !strings.Contains(payURL, "PAY1") {
		t.Fatalf("payURL %q missing payment no", payURL)
	}
	if err := m.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

// Create rejects non-positive amounts before touching the DB.
func TestCreateInvalidAmount(t *testing.T) {
	db, _ := mockDB(t)
	s := NewService(db, nopLog())
	_, _, err := s.Create(context.Background(), CreateInput{BizType: "t", BizNo: "1", AmountMinor: 0})
	e, ok := err.(*response.Error)
	if !ok || e.Status != http.StatusBadRequest {
		t.Fatalf("want 400, got %v", err)
	}
}

// HandleCallback stores an audit row and returns 400 when the signature is bad.
func TestHandleCallbackInvalidSignature(t *testing.T) {
	db, m := mockDB(t)
	s := NewService(db, nopLog())

	m.ExpectQuery("SELECT \\* FROM `plg_payment_channel` WHERE code = \\?").
		WillReturnRows(channelRow("mock", `{"secret":"k"}`))
	m.ExpectBegin()
	m.ExpectExec("INSERT INTO `plg_payment_callback`").WillReturnResult(sqlmock.NewResult(1, 1))
	m.ExpectCommit()

	_, err := s.HandleCallback(context.Background(), CallbackInput{
		Channel: "mock", Body: []byte(`{"paymentNo":"PAY1","result":"success"}`),
		Headers: map[string]string{"x-timestamp": "123", "x-signature": "nope"},
	})
	e, ok := err.(*response.Error)
	if !ok || e.Status != http.StatusBadRequest {
		t.Fatalf("want 400, got %v", err)
	}
}

// A success callback on a pending order flips it to paid, writes the audit
// row and publishes payment.succeeded.
func TestCallbackMarksPaidAndPublishes(t *testing.T) {
	db, m := mockDB(t)
	s := NewService(db, nopLog())

	events := make(chan event.Event, 4)
	event.Subscribe(event.SubscriberFunc(func(e event.Event) {
		if strings.HasPrefix(e.Name, "payment.") {
			events <- e
		}
	}))

	m.ExpectQuery("SELECT \\* FROM `plg_payment_channel` WHERE code = \\?").
		WillReturnRows(channelRow("mock", `{"secret":"k"}`))
	m.ExpectBegin()
	m.ExpectQuery("SELECT \\* FROM `plg_payment_order` WHERE payment_no = \\?").
		WillReturnRows(orderRow("PAY1", StatusPending, 500, 0))
	m.ExpectExec("UPDATE `plg_payment_order`").WillReturnResult(sqlmock.NewResult(1, 1))
	m.ExpectCommit()
	m.ExpectBegin()
	m.ExpectExec("INSERT INTO `plg_payment_callback`").WillReturnResult(sqlmock.NewResult(1, 1))
	m.ExpectCommit()

	body, _ := json.Marshal(mockCallbackBody{PaymentNo: "PAY1", Result: "success", ChannelTxnID: "mock_x", Timestamp: time.Now().Unix()})
	ts := time.Now().Unix()
	o, err := s.HandleCallback(context.Background(), CallbackInput{
		Channel: "mock", Body: body,
		Headers: map[string]string{"x-timestamp": formatID(ts), "x-signature": SignMockCallback("k", body, ts)},
	})
	if err != nil {
		t.Fatal(err)
	}
	if o.Status != StatusPaid || o.ChannelTxnID != "mock_x" {
		t.Fatalf("unexpected order %+v", o)
	}
	select {
	case ev := <-events:
		if ev.Name != EventSucceeded {
			t.Fatalf("want %s, got %s", EventSucceeded, ev.Name)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("payment.succeeded not published")
	}
	if err := m.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

// A duplicate success callback on a paid order is accepted without error and
// does not publish a second event.
func TestCallbackDuplicateIgnored(t *testing.T) {
	db, m := mockDB(t)
	s := NewService(db, nopLog())

	published := 0
	event.Subscribe(event.SubscriberFunc(func(e event.Event) {
		if e.Name == EventSucceeded {
			published++
		}
	}))

	m.ExpectQuery("SELECT \\* FROM `plg_payment_channel` WHERE code = \\?").
		WillReturnRows(channelRow("mock", `{"secret":"k"}`))
	m.ExpectBegin()
	m.ExpectQuery("SELECT \\* FROM `plg_payment_order` WHERE payment_no = \\?").
		WillReturnRows(orderRow("PAY1", StatusPaid, 500, 0))
	m.ExpectCommit() // nothing changed inside the tx
	m.ExpectBegin()
	m.ExpectExec("INSERT INTO `plg_payment_callback`").WillReturnResult(sqlmock.NewResult(1, 1))
	m.ExpectCommit()

	body, _ := json.Marshal(mockCallbackBody{PaymentNo: "PAY1", Result: "success", Timestamp: time.Now().Unix()})
	ts := time.Now().Unix()
	if _, err := s.HandleCallback(context.Background(), CallbackInput{
		Channel: "mock", Body: body,
		Headers: map[string]string{"x-timestamp": formatID(ts), "x-signature": SignMockCallback("k", body, ts)},
	}); err != nil {
		t.Fatal(err)
	}
	time.Sleep(100 * time.Millisecond)
	if published != 0 {
		t.Fatalf("duplicate callback published %d events", published)
	}
}

// CloseExpired flips only expired pending orders.
func TestCloseExpired(t *testing.T) {
	db, m := mockDB(t)
	s := NewService(db, nopLog())

	m.ExpectQuery("SELECT \\* FROM `plg_payment_order` WHERE").
		WillReturnRows(orderRow("PAY1", StatusPending, 500, 0))
	m.ExpectBegin()
	m.ExpectExec("UPDATE `plg_payment_order`").WillReturnResult(sqlmock.NewResult(1, 1))
	m.ExpectCommit()

	n, err := s.CloseExpired(context.Background(), testNow.Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("closed %d, want 1", n)
	}
}
