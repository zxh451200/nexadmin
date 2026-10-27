package pay

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
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

// tenantCtx 让租户表模型走通（无租户上下文访问租户表会被框架拒绝）。
var tenantCtx = tenant.With(context.Background(), 7)

func channelRow(code, provider, cfg string) *sqlmock.Rows {
	return sqlmock.NewRows([]string{
		"id", "tenant_id", "code", "name", "provider", "enabled", "sort", "config_json",
		"created_at", "updated_at",
	}).AddRow(1, 7, code, "channel "+code, provider, true, 0, cfg, testNow, testNow)
}

func orderRow(no, status string, amount, refunded int64) *sqlmock.Rows {
	return sqlmock.NewRows([]string{
		"id", "tenant_id", "payment_no", "biz_type", "biz_no", "subject_id", "subject",
		"amount_minor", "refunded_minor", "fee_minor", "currency", "fx_rate_micro",
		"channel_code", "channel_txn_id", "status", "fail_reason",
		"paid_at", "expire_at", "created_at", "updated_at",
	}).AddRow(
		1001, 7, no, "trade_group", "G-1", "", "test order",
		amount, refunded, 0, "CNY", 1000000,
		"mock", nil, status, "",
		nil, nil, testNow, testNow,
	)
}

func TestMockVerifyCallback(t *testing.T) {
	cfg := map[string]string{"secret": "s3cret"}
	body, _ := json.Marshal(mockCallbackBody{PaymentNo: "PAY1", Result: "success", Timestamp: time.Now().Unix()})
	nowTs := time.Now().Unix()
	sig := SignMockCallback("s3cret", body, nowTs)

	okRes, err := mockProvider{}.VerifyCallback(CallbackInput{
		Channel: "mock", Body: body,
		Headers: map[string]string{"x-timestamp": formatID(nowTs), "x-signature": sig},
	}, cfg)
	if err != nil || !okRes.Paid || okRes.PaymentNo != "PAY1" {
		t.Fatalf("valid callback rejected: %+v err=%v", okRes, err)
	}

	// 篡改报文必须失败。
	bad := append([]byte(nil), body...)
	bad[1] = 'X'
	if _, err := (mockProvider{}).VerifyCallback(CallbackInput{
		Channel: "mock", Body: bad,
		Headers: map[string]string{"x-timestamp": formatID(nowTs), "x-signature": sig},
	}, cfg); err == nil {
		t.Fatal("tampered body accepted")
	}

	// 超出 5 分钟时间窗必须失败（重放防护，契约取严值）。
	oldUnix := time.Now().Add(-time.Hour).Unix()
	oldBody, _ := json.Marshal(mockCallbackBody{PaymentNo: "PAY1", Result: "success", Timestamp: oldUnix})
	if _, err := (mockProvider{}).VerifyCallback(CallbackInput{
		Channel: "mock", Body: oldBody,
		Headers: map[string]string{"x-timestamp": formatID(oldUnix), "x-signature": SignMockCallback("s3cret", oldBody, oldUnix)},
	}, cfg); err == nil {
		t.Fatal("stale callback accepted")
	}

	// 缺签名头必须失败。
	if _, err := (mockProvider{}).VerifyCallback(CallbackInput{Channel: "mock", Body: body}, cfg); err == nil {
		t.Fatal("unsigned callback accepted")
	}
}

// Create 幂等：同 (biz_type, biz_no) 命中已有单时不再 INSERT，直接返回并重取拉起参数。
func TestCreateIdempotent(t *testing.T) {
	db, m := mockDB(t)
	s := NewService(db, nopLog())

	m.ExpectQuery("SELECT \\* FROM `plg_pay_order` WHERE biz_type = \\? AND biz_no = \\?").
		WillReturnRows(orderRow("PAY1", StatusPending, 500, 0))
	m.ExpectQuery("SELECT \\* FROM `plg_pay_channel` WHERE code = \\?").
		WillReturnRows(channelRow("mock", "mock", `{"secret":"k"}`))

	o, params, err := s.Create(tenantCtx, CreateInput{
		BizType: "trade_group", BizNo: "G-1", AmountMinor: 500, Currency: "CNY", Subject: "t",
	})
	if err != nil {
		t.Fatal(err)
	}
	if o.PaymentNo != "PAY1" || o.Status != StatusPending {
		t.Fatalf("unexpected order %+v", o)
	}
	if !strings.Contains(params.PayURL, "PAY1") {
		t.Fatalf("payURL %q missing payment no", params.PayURL)
	}
	if err := m.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

// Create 拒绝非正金额，且动库之前就拒绝。
func TestCreateInvalidAmount(t *testing.T) {
	db, _ := mockDB(t)
	s := NewService(db, nopLog())
	_, _, err := s.Create(tenantCtx, CreateInput{BizType: "t", BizNo: "1", AmountMinor: 0})
	e, ok := err.(*response.Error)
	if !ok || e.Status != http.StatusBadRequest || e.Key != errInvalidAmount {
		t.Fatalf("want 400 %s, got %v", errInvalidAmount, err)
	}
}

// 外币单缺汇率快照必须拒绝（PAY_FX_RATE_MISSING）。
func TestCreateForeignCurrencyRequiresFx(t *testing.T) {
	db, _ := mockDB(t)
	s := NewService(db, nopLog())
	_, _, err := s.Create(tenantCtx, CreateInput{
		BizType: "t", BizNo: "1", AmountMinor: 100, Currency: "CNY",
		OrigCurrency: "USD", AmountOrigMinor: 15,
	})
	e, ok := err.(*response.Error)
	if !ok || e.Key != errFxRateMissing {
		t.Fatalf("want %s, got %v", errFxRateMissing, err)
	}
}

// HandleCallback 验签失败：审计落 sign_invalid，返回 400，不发事件。
func TestHandleCallbackInvalidSignature(t *testing.T) {
	db, m := mockDB(t)
	s := NewService(db, nopLog())

	m.ExpectQuery("SELECT \\* FROM `plg_pay_channel` WHERE code = \\?").
		WillReturnRows(channelRow("mock", "mock", `{"secret":"k"}`))
	m.ExpectBegin()
	m.ExpectExec("INSERT INTO `plg_pay_callback`").WillReturnResult(sqlmock.NewResult(1, 1))
	m.ExpectCommit()

	_, err := s.HandleCallback(tenant.Skip(context.Background()), CallbackInput{
		Channel: "mock", Body: []byte(`{"paymentNo":"PAY1","result":"success"}`),
		Headers: map[string]string{"x-timestamp": "123", "x-signature": "nope"},
	})
	e, ok := err.(*response.Error)
	if !ok || e.Status != http.StatusBadRequest || e.Key != errSignInvalid {
		t.Fatalf("want 400 %s, got %v", errSignInvalid, err)
	}
	if err := m.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

// pending 单收到成功回调：翻转为 succeeded，审计 accepted，出站箱同事务落一行，
// 事件进总线。手续费按「主体精确 > 租户默认」规则计算。
func TestCallbackMarksPaidAndPublishes(t *testing.T) {
	db, m := mockDB(t)
	s := NewService(db, nopLog())

	events := make(chan event.Event, 4)
	event.Subscribe(event.SubscriberFunc(func(e event.Event) {
		if strings.HasPrefix(e.Name, "pay.") {
			events <- e
		}
	}))

	m.MatchExpectationsInOrder(true)
	m.ExpectQuery("SELECT \\* FROM `plg_pay_channel` WHERE code = \\?").
		WillReturnRows(channelRow("mock", "mock", `{"secret":"k"}`))
	// transition 事务：行锁取单 → 取默认抽佣规则（无规则）→ UPDATE → 出站箱 INSERT。
	m.ExpectBegin()
	m.ExpectQuery("SELECT \\* FROM `plg_pay_order` WHERE payment_no = \\?").
		WillReturnRows(orderRow("PAY1", StatusPending, 500, 0))
	m.ExpectQuery("SELECT \\* FROM `plg_pay_commission_rule` WHERE").
		WillReturnRows(sqlmock.NewRows([]string{"id", "tenant_id", "subject_id", "biz_type", "rate_bp", "fixed_minor", "currency", "status"}))
	m.ExpectQuery("SELECT \\* FROM `plg_pay_commission_rule` WHERE").
		WillReturnRows(sqlmock.NewRows([]string{"id", "tenant_id", "subject_id", "biz_type", "rate_bp", "fixed_minor", "currency", "status"}))
	m.ExpectExec("UPDATE `plg_pay_order`").WillReturnResult(sqlmock.NewResult(1, 1))
	m.ExpectExec("INSERT INTO `plg_pay_outbox`").WillReturnResult(sqlmock.NewResult(1, 1))
	m.ExpectCommit()
	// 事件入队成功后把出站箱标记为已投（gorm 为单条写语句自动包事务）。
	m.ExpectBegin()
	m.ExpectExec("UPDATE `plg_pay_outbox`").WillReturnResult(sqlmock.NewResult(1, 1))
	m.ExpectCommit()
	m.ExpectBegin()
	m.ExpectExec("INSERT INTO `plg_pay_callback`").WillReturnResult(sqlmock.NewResult(1, 1))
	m.ExpectCommit()

	body, _ := json.Marshal(mockCallbackBody{PaymentNo: "PAY1", Result: "success", ChannelTxnID: "mock_x", Timestamp: time.Now().Unix()})
	ts := time.Now().Unix()
	o, err := s.HandleCallback(tenant.With(context.Background(), 7), CallbackInput{
		Channel: "mock", Body: body,
		Headers: map[string]string{"x-timestamp": formatID(ts), "x-signature": SignMockCallback("k", body, ts)},
	})
	if err != nil {
		t.Fatal(err)
	}
	if o.Status != StatusSucceeded || txnIDOf(o) != "mock_x" {
		t.Fatalf("unexpected order %+v", o)
	}
	select {
	case ev := <-events:
		if ev.Name != EventSucceeded {
			t.Fatalf("want %s, got %s", EventSucceeded, ev.Name)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("pay.succeeded not published")
	}
	if err := m.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

// 已支付单收到重复成功回调：原样放行，不重复发事件。
func TestCallbackDuplicateIgnored(t *testing.T) {
	db, m := mockDB(t)
	s := NewService(db, nopLog())

	published := 0
	event.Subscribe(event.SubscriberFunc(func(e event.Event) {
		if e.Name == EventSucceeded {
			published++
		}
	}))

	m.ExpectQuery("SELECT \\* FROM `plg_pay_channel` WHERE code = \\?").
		WillReturnRows(channelRow("mock", "mock", `{"secret":"k"}`))
	m.ExpectBegin()
	m.ExpectQuery("SELECT \\* FROM `plg_pay_order` WHERE payment_no = \\?").
		WillReturnRows(orderRow("PAY1", StatusSucceeded, 500, 0))
	m.ExpectCommit() // 状态未变，事务内无写入
	m.ExpectBegin()
	m.ExpectExec("INSERT INTO `plg_pay_callback`").WillReturnResult(sqlmock.NewResult(1, 1))
	m.ExpectCommit()

	body, _ := json.Marshal(mockCallbackBody{PaymentNo: "PAY1", Result: "success", Timestamp: time.Now().Unix()})
	ts := time.Now().Unix()
	if _, err := s.HandleCallback(tenant.With(context.Background(), 7), CallbackInput{
		Channel: "mock", Body: body,
		Headers: map[string]string{"x-timestamp": formatID(ts), "x-signature": SignMockCallback("k", body, ts)},
	}); err != nil {
		t.Fatal(err)
	}
	time.Sleep(100 * time.Millisecond)
	if published != 0 {
		t.Fatalf("duplicate callback published %d events", published)
	}
	if err := m.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

// 退款超额必须 409（Σrefund ≤ amount 不变式在行锁事务内校验）。
func TestCreateRefundExceeds(t *testing.T) {
	db, m := mockDB(t)
	s := NewService(db, nopLog())

	m.ExpectQuery("SELECT \\* FROM `plg_pay_refund` WHERE biz_type = \\? AND biz_no = \\?").
		WillReturnRows(sqlmock.NewRows([]string{"id", "tenant_id"}))
	m.ExpectBegin()
	m.ExpectQuery("SELECT \\* FROM `plg_pay_order` WHERE payment_no = \\?").
		WillReturnRows(orderRow("PAY1", StatusSucceeded, 500, 400))
	m.ExpectRollback()

	_, err := s.CreateRefund(tenantCtx, RefundInput{
		PaymentNo: "PAY1", BizType: "trade_refund", BizNo: "R-1", AmountMinor: 200,
	})
	e, ok := err.(*response.Error)
	if !ok || e.Status != http.StatusConflict || e.Key != errRefundExceeds {
		t.Fatalf("want 409 %s, got %v", errRefundExceeds, err)
	}
	if err := m.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

// mock 渠道同步退款成功：退款单 succeeded、支付单累计 refunded_minor、事件 pay.refund.succeeded。
func TestCreateRefundSucceeds(t *testing.T) {
	db, m := mockDB(t)
	s := NewService(db, nopLog())

	events := make(chan event.Event, 4)
	event.Subscribe(event.SubscriberFunc(func(e event.Event) {
		if e.Name == EventRefundOK {
			events <- e
		}
	}))

	m.ExpectQuery("SELECT \\* FROM `plg_pay_refund` WHERE biz_type = \\? AND biz_no = \\?").
		WillReturnRows(sqlmock.NewRows([]string{"id", "tenant_id"}))
	// 事务：锁单 → 锁退款幂等键（未命中）→ 抽佣规则（无）→ 渠道 → INSERT 退款 → UPDATE 订单 → 出站箱。
	m.ExpectBegin()
	m.ExpectQuery("SELECT \\* FROM `plg_pay_order` WHERE payment_no = \\?").
		WillReturnRows(orderRow("PAY1", StatusSucceeded, 500, 0))
	m.ExpectQuery("SELECT \\* FROM `plg_pay_refund` WHERE biz_type = \\? AND biz_no = \\?").
		WillReturnRows(sqlmock.NewRows([]string{"id", "tenant_id"}))
	m.ExpectQuery("SELECT \\* FROM `plg_pay_commission_rule` WHERE").
		WillReturnRows(sqlmock.NewRows([]string{"id"}))
	m.ExpectQuery("SELECT \\* FROM `plg_pay_commission_rule` WHERE").
		WillReturnRows(sqlmock.NewRows([]string{"id"}))
	m.ExpectQuery("SELECT \\* FROM `plg_pay_channel` WHERE code = \\?").
		WillReturnRows(channelRow("mock", "mock", `{"secret":"k"}`))
	m.ExpectExec("INSERT INTO `plg_pay_refund`").WillReturnResult(sqlmock.NewResult(1, 1))
	m.ExpectExec("UPDATE `plg_pay_order`").WillReturnResult(sqlmock.NewResult(1, 1))
	m.ExpectExec("INSERT INTO `plg_pay_outbox`").WillReturnResult(sqlmock.NewResult(1, 1))
	m.ExpectCommit()
	m.ExpectBegin()
	m.ExpectExec("UPDATE `plg_pay_outbox`").WillReturnResult(sqlmock.NewResult(1, 1))
	m.ExpectCommit()

	r, err := s.CreateRefund(tenantCtx, RefundInput{
		PaymentNo: "PAY1", BizType: "trade_refund", BizNo: "R-1", AmountMinor: 200, Reason: "partial",
	})
	if err != nil {
		t.Fatal(err)
	}
	if r.Status != RefundSucceeded || r.AmountMinor != 200 {
		t.Fatalf("unexpected refund %+v", r)
	}
	select {
	case <-events:
	case <-time.After(2 * time.Second):
		t.Fatal("pay.refund.succeeded not published")
	}
	if err := m.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

// CloseExpired 只翻转过期 pending 单，且每张都落事件出站箱（跨租户清扫）。
func TestCloseExpired(t *testing.T) {
	db, m := mockDB(t)
	s := NewService(db, nopLog())

	m.ExpectQuery("SELECT \\* FROM `plg_pay_order` WHERE").
		WillReturnRows(orderRow("PAY1", StatusPending, 500, 0))
	m.ExpectBegin()
	m.ExpectExec("UPDATE `plg_pay_order`").WillReturnResult(sqlmock.NewResult(1, 1))
	m.ExpectExec("INSERT INTO `plg_pay_outbox`").WillReturnResult(sqlmock.NewResult(1, 1))
	m.ExpectCommit()
	m.ExpectBegin()
	m.ExpectExec("UPDATE `plg_pay_outbox`").WillReturnResult(sqlmock.NewResult(1, 1))
	m.ExpectCommit()

	n, err := s.CloseExpired(tenant.Skip(context.Background()), testNow.Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("closed %d, want 1", n)
	}
	if err := m.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

// UpsertChannel：code 被其他记录占用时 409；provider 未注册时 400。
func TestUpsertChannelValidation(t *testing.T) {
	db, m := mockDB(t)
	s := NewService(db, nopLog())

	// provider 未知直接拒绝，不动库。
	if _, err := s.UpsertChannel(tenantCtx, SaveChannelInput{Code: "wx01", Name: "微信", Provider: "nope"}); err == nil {
		t.Fatal("unknown provider should fail")
	}

	m.ExpectBegin()
	m.ExpectQuery("SELECT \\* FROM `plg_pay_channel` WHERE code = \\?").
		WillReturnRows(channelRow("wx01", "mock", "")) // 同租户已有该 code 的新建请求
	m.ExpectRollback()
	if _, err := s.UpsertChannel(tenantCtx, SaveChannelInput{Code: "wx01", Name: "微信", Provider: "mock"}); err == nil {
		t.Fatal("duplicate code should conflict")
	}
	if err := m.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

// ---- 请求级幂等中间件 ----

func idemRouter(db *gorm.DB) *gin.Engine {
	r := gin.New()
	r.POST("/plugins/pay/orders", idempotency(db), func(c *gin.Context) {
		var f struct {
			BizNo string `json:"bizNo"`
		}
		_ = c.ShouldBindJSON(&f)
		c.JSON(http.StatusOK, gin.H{"code": 0, "data": gin.H{"bizNo": f.BizNo}})
	})
	return r
}

func idemRow(key, hash, body string) *sqlmock.Rows {
	return sqlmock.NewRows([]string{
		"id", "tenant_id", "idem_key", "path", "req_hash", "resp_status", "resp_body",
		"created_at", "updated_at",
	}).AddRow(1, 7, key, "/plugins/pay/orders", hash, 200, body, testNow, testNow)
}

// 同键同体：重放首次响应并置 X-Idempotent-Replayed: true。
func TestIdempotencyReplay(t *testing.T) {
	db, m := mockDB(t)
	r := idemRouter(db)

	body := `{"bizNo":"G-1"}`
	sum := sha256Hex("K1|/plugins/pay/orders|"+body)
	stored := `{"code":0,"data":{"bizNo":"G-1"}}`
	m.ExpectQuery("SELECT \\* FROM `plg_pay_idem` WHERE idem_key = \\?").
		WillReturnRows(idemRow("K1", sum, stored))

	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/plugins/pay/orders", strings.NewReader(body))
	req.Header.Set("Idempotency-Key", "K1")
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK || w.Body.String() != stored {
		t.Fatalf("replay: code=%d body=%s", w.Code, w.Body.String())
	}
	if w.Header().Get(idemReplayHeader) != "true" {
		t.Fatal("missing X-Idempotent-Replayed header")
	}
	if err := m.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

// 同键异体：422 PAY_IDEMPOTENCY_CONFLICT。
func TestIdempotencyConflict(t *testing.T) {
	db, m := mockDB(t)
	r := idemRouter(db)

	m.ExpectQuery("SELECT \\* FROM `plg_pay_idem` WHERE idem_key = \\?").
		WillReturnRows(idemRow("K1", "deadbeef", `{"code":0}`))

	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/plugins/pay/orders", strings.NewReader(`{"bizNo":"G-2"}`))
	req.Header.Set("Idempotency-Key", "K1")
	r.ServeHTTP(w, req)
	if w.Code != 422 {
		t.Fatalf("conflict: want 422, got %d", w.Code)
	}
	if err := m.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

// 首次请求（无记录）：放行并在 2xx 后落幂等快照。
func TestIdempotencyFirstRequestStores(t *testing.T) {
	db, m := mockDB(t)
	r := idemRouter(db)

	m.ExpectQuery("SELECT \\* FROM `plg_pay_idem` WHERE idem_key = \\?").
		WillReturnRows(sqlmock.NewRows([]string{"id", "tenant_id"}))
	m.ExpectBegin()
	m.ExpectExec("INSERT INTO `plg_pay_idem`").WillReturnResult(sqlmock.NewResult(1, 1))
	m.ExpectCommit()

	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/plugins/pay/orders", strings.NewReader(`{"bizNo":"G-1"}`))
	req.Header.Set("Idempotency-Key", "K2")
	// 幂等快照按租户隔离存储：模拟已登录请求的租户上下文。
	req = req.WithContext(tenant.With(req.Context(), 7))
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("first request: code=%d", w.Code)
	}
	if err := m.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func sha256Hex(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}
