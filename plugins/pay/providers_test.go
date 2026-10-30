package pay

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/gin-gonic/gin"
)

var (
	rsaOnce sync.Once
	rsaKey  *rsa.PrivateKey
	rsaPriv string
	rsaPub  string
)

// testRSA 生成一次 2048 位密钥（PEM），支付宝渠道的私钥/公钥配置都用它。
func testRSA(t *testing.T) (*rsa.PrivateKey, string, string) {
	t.Helper()
	rsaOnce.Do(func() {
		k, err := rsa.GenerateKey(rand.Reader, 2048)
		if err != nil {
			panic(err)
		}
		privDER, err := x509.MarshalPKCS8PrivateKey(k)
		if err != nil {
			panic(err)
		}
		pubDER, err := x509.MarshalPKIXPublicKey(&k.PublicKey)
		if err != nil {
			panic(err)
		}
		rsaKey = k
		rsaPriv = string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: privDER}))
		rsaPub = string(pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: pubDER}))
	})
	return rsaKey, rsaPriv, rsaPub
}

// alipayCanonicalIndependent 按支付宝文档独立拼待签串（不复用被测实现），
// 用来证明被测实现的排序/排除规则真的符合协议而不是自证。
func alipayCanonicalIndependent(params map[string]string, dropSignType bool) string {
	keys := make([]string, 0, len(params))
	for k, v := range params {
		if k == "sign" || (dropSignType && k == "sign_type") {
			continue
		}
		if strings.TrimSpace(v) == "" {
			continue
		}
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		parts = append(parts, k+"="+params[k])
	}
	return strings.Join(parts, "&")
}

// alipaySignedNotify 用支付宝私钥对异步通知表单签名（通知验签去掉 sign/sign_type）。
func alipaySignedNotify(key *rsa.PrivateKey, params map[string]string) []byte {
	v := url.Values{}
	for k, val := range params {
		v.Set(k, val)
	}
	v.Set("sign_type", "RSA2")
	v.Set("sign", rsaSignSHA256(key, alipayCanonicalIndependent(params, true)))
	return []byte(v.Encode())
}

func TestAlipayVerifyCallback(t *testing.T) {
	key, _, pub := testRSA(t)
	cfg := map[string]string{
		"appId": "2021000000000001", "alipayPublicKey": pub, "sellerId": "2088000000000001",
	}
	base := func() map[string]string {
		return map[string]string{
			"app_id":       "2021000000000001",
			"seller_id":    "2088000000000001",
			"out_trade_no": "PAY-1",
			"trade_no":     "2026102922001234567890",
			"trade_status": "TRADE_SUCCESS",
			"total_amount": "12.34",
			"notify_time":  "2026-10-29 18:30:00",
			"notify_type":  "trade_status_sync",
		}
	}

	res, err := (alipayProvider{}).VerifyCallback(CallbackInput{Body: alipaySignedNotify(key, base())}, cfg)
	if err != nil {
		t.Fatalf("valid notify rejected: %v", err)
	}
	if !res.Paid || res.Failed || res.PaymentNo != "PAY-1" || res.AmountMinor != 1234 ||
		res.ChannelTxnID != "2026102922001234567890" || res.GatewayStatus != "TRADE_SUCCESS" {
		t.Fatalf("unexpected result %+v", res)
	}

	tamper := func(mutate func(map[string]string)) []byte {
		p := base()
		mutate(p)
		return alipaySignedNotify(key, p)
	}

	if _, err := (alipayProvider{}).VerifyCallback(CallbackInput{
		Body: []byte(strings.Replace(string(alipaySignedNotify(key, base())), "total_amount=12.34", "total_amount=0.01", 1)),
	}, cfg); err == nil {
		t.Fatal("tampered amount accepted")
	}
	if _, err := (alipayProvider{}).VerifyCallback(CallbackInput{
		Body: tamper(func(p map[string]string) { p["app_id"] = "2021000000000002" }),
	}, cfg); err == nil {
		t.Fatal("app_id mismatch accepted")
	}
	if _, err := (alipayProvider{}).VerifyCallback(CallbackInput{
		Body: tamper(func(p map[string]string) { p["seller_id"] = "2088000000000002" }),
	}, cfg); err == nil {
		t.Fatal("seller_id mismatch accepted")
	}

	// 另一个私钥签的通知必须被拒。
	other, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := (alipayProvider{}).VerifyCallback(CallbackInput{Body: alipaySignedNotify(other, base())}, cfg); err == nil {
		t.Fatal("notify signed by foreign key accepted")
	}

	unsigned := url.Values{}
	for k, v := range base() {
		unsigned.Set(k, v)
	}
	if _, err := (alipayProvider{}).VerifyCallback(CallbackInput{Body: []byte(unsigned.Encode())}, cfg); err == nil {
		t.Fatal("unsigned notify accepted")
	}
	if _, err := (alipayProvider{}).VerifyCallback(CallbackInput{
		Body: tamper(func(p map[string]string) { delete(p, "out_trade_no") }),
	}, cfg); err == nil {
		t.Fatal("notify without out_trade_no accepted")
	}

	wait, err := (alipayProvider{}).VerifyCallback(CallbackInput{
		Body: tamper(func(p map[string]string) { p["trade_status"] = "WAIT_BUYER_PAY" }),
	}, cfg)
	if err != nil || wait.Paid || wait.Failed {
		t.Fatalf("WAIT_BUYER_PAY should neither pay nor fail: %+v err=%v", wait, err)
	}

	closed, err := (alipayProvider{}).VerifyCallback(CallbackInput{
		Body: tamper(func(p map[string]string) { p["trade_status"] = "TRADE_CLOSED" }),
	}, cfg)
	if err != nil || !closed.Failed || closed.Paid {
		t.Fatalf("TRADE_CLOSED should fail: %+v err=%v", closed, err)
	}
}

func TestAlipayCanonical(t *testing.T) {
	v := url.Values{}
	v.Set("b", "2")
	v.Set("a", "1")
	v.Set("sign", "sig")
	v.Set("sign_type", "RSA2")
	v.Set("empty", "")
	v.Set("cn", "中文")
	if got := alipayCanonical(v, true); got != "a=1&b=2&cn=中文" {
		t.Fatalf("callback canonical = %q", got)
	}
	if got := alipayCanonical(v, false); got != "a=1&b=2&cn=中文&sign_type=RSA2" {
		t.Fatalf("request canonical = %q", got)
	}
}

func TestProviderInfosExposeSchemas(t *testing.T) {
	infos := ProviderInfos()
	byName := map[string]ProviderInfo{}
	for i, in := range infos {
		if i > 0 && infos[i-1].Name > in.Name {
			t.Fatalf("ProviderInfos not sorted: %s before %s", infos[i-1].Name, in.Name)
		}
		byName[in.Name] = in
	}
	if len(byName["mock"].Fields) != 0 {
		t.Fatalf("mock should not declare credential fields")
	}
	for _, name := range []string{"alipay", "wechat", "epay"} {
		if len(byName[name].Fields) == 0 {
			t.Fatalf("%s exposes no credential fields", name)
		}
	}
	required := map[string]bool{}
	secret := map[string]bool{}
	for _, f := range byName["alipay"].Fields {
		required[f.Key] = f.Required
		secret[f.Key] = f.Secret
	}
	for _, k := range []string{"appId", "privateKey", "alipayPublicKey"} {
		if !required[k] {
			t.Fatalf("alipay field %s should be required", k)
		}
	}
	if !secret["privateKey"] || !secret["alipayPublicKey"] || secret["appId"] {
		t.Fatalf("alipay secret flags wrong: %v", secret)
	}
}

func TestProviderAckFormats(t *testing.T) {
	cases := []struct {
		p    Provider
		ct   string
		body string
	}{
		{alipayProvider{}, "text/plain; charset=utf-8", "success"},
		{epayProvider{}, "text/plain; charset=utf-8", "success"},
		{wechatProvider{}, "application/json; charset=utf-8", `{"code":"SUCCESS","message":"成功"}`},
	}
	for _, c := range cases {
		ack, ok := c.p.(AckProvider)
		if !ok {
			t.Fatalf("%s should implement AckProvider", c.p.Name())
		}
		if ct, body := ack.CallbackAck(); ct != c.ct || body != c.body {
			t.Fatalf("%s ack = %q %q", c.p.Name(), ct, body)
		}
	}
}

func TestAlipayCreatePageURL(t *testing.T) {
	_, priv, pub := testRSA(t)
	cfg := map[string]string{"appId": "2021000000000001", "privateKey": priv, "alipayPublicKey": pub}
	req := PayRequest{
		PaymentNo: "PAY-1", Subject: "测试商品", AmountMinor: 1234, Currency: "CNY", Method: "page",
		NotifyURL: "https://shop.example.com/api/v1/plugins/pay/notify/ali01",
		ReturnURL: "https://shop.example.com/pay/return",
	}
	params, err := (alipayProvider{}).Create(context.Background(), req, cfg)
	if err != nil {
		t.Fatal(err)
	}
	u, err := url.Parse(params.PayURL)
	if err != nil || u.Scheme != "https" || u.Host != "openapi.alipay.com" {
		t.Fatalf("unexpected pay url %q", params.PayURL)
	}
	q := u.Query()
	if q.Get("method") != "alipay.trade.page.pay" || q.Get("app_id") != "2021000000000001" || q.Get("sign_type") != "RSA2" {
		t.Fatalf("bad common params: %v", q)
	}
	if q.Get("notify_url") != req.NotifyURL || q.Get("return_url") != req.ReturnURL {
		t.Fatalf("notify/return url not carried: %v", q)
	}
	body := map[string]string{}
	for k := range q {
		body[k] = q.Get(k)
	}
	pubKey, err := alipayPublicKey(pub)
	if err != nil {
		t.Fatal(err)
	}
	if !rsaVerifySHA256(pubKey, alipayCanonicalIndependent(body, false), q.Get("sign")) {
		t.Fatal("page pay signature does not verify")
	}
	biz := map[string]string{}
	if err := json.Unmarshal([]byte(q.Get("biz_content")), &biz); err != nil {
		t.Fatal(err)
	}
	if biz["out_trade_no"] != "PAY-1" || biz["total_amount"] != "12.34" || biz["product_code"] != "FAST_INSTANT_TRADE_PAY" {
		t.Fatalf("unexpected biz_content %v", biz)
	}

	// 真实网关必须拿到完整回调地址；外币单直接拒绝。
	req.NotifyURL = ""
	if _, err := (alipayProvider{}).Create(context.Background(), req, cfg); err == nil {
		t.Fatal("missing notify url accepted")
	}
	req.NotifyURL = "https://shop.example.com/notify"
	req.Currency = "USD"
	if _, err := (alipayProvider{}).Create(context.Background(), req, cfg); err == nil {
		t.Fatal("foreign currency accepted")
	}
}

// newAlipayTestGateway 模拟支付宝网关：记录请求表单，返回带签名的应答节点。
func newAlipayTestGateway(t *testing.T, signKey *rsa.PrivateKey, node string, reply map[string]any) (*httptest.Server, *url.Values) {
	t.Helper()
	raw, err := json.Marshal(reply)
	if err != nil {
		t.Fatal(err)
	}
	payload := `{"` + node + `":` + string(raw) + `,"sign":"` + rsaSignSHA256(signKey, string(raw)) + `"}`
	form := &url.Values{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			t.Errorf("gateway parse form: %v", err)
			return
		}
		*form = r.PostForm
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(payload))
	}))
	t.Cleanup(srv.Close)
	return srv, form
}

func TestAlipayQueryViaGateway(t *testing.T) {
	key, priv, pub := testRSA(t)
	srv, form := newAlipayTestGateway(t, key, "alipay_trade_query_response", map[string]any{
		"code": "10000", "msg": "Success",
		"trade_no": "2026102922001234567890", "out_trade_no": "PAY-1", "trade_status": "TRADE_SUCCESS",
	})
	cfg := map[string]string{
		"appId": "2021000000000001", "privateKey": priv, "alipayPublicKey": pub, "gateway": srv.URL,
	}
	st, err := (alipayProvider{}).Query(context.Background(), "PAY-1", cfg)
	if err != nil {
		t.Fatal(err)
	}
	if !st.Paid || st.Failed || st.ChannelTxnID != "2026102922001234567890" || st.GatewayStatus != "TRADE_SUCCESS" {
		t.Fatalf("unexpected status %+v", st)
	}
	if form.Get("method") != "alipay.trade.query" || form.Get("app_id") != "2021000000000001" {
		t.Fatalf("unexpected request form %v", *form)
	}
	if !strings.Contains(form.Get("biz_content"), `"out_trade_no":"PAY-1"`) {
		t.Fatalf("biz_content missing out_trade_no: %s", form.Get("biz_content"))
	}
	req := map[string]string{}
	for k := range *form {
		req[k] = form.Get(k)
	}
	if !rsaVerifySHA256(&key.PublicKey, alipayCanonicalIndependent(req, false), form.Get("sign")) {
		t.Fatal("request signature does not verify")
	}
}

func TestAlipayQueryTradeNotExist(t *testing.T) {
	key, priv, pub := testRSA(t)
	srv, _ := newAlipayTestGateway(t, key, "alipay_trade_query_response", map[string]any{
		"code": "40004", "msg": "Business Failed", "sub_code": "ACQ.TRADE_NOT_EXIST", "sub_msg": "交易不存在",
	})
	cfg := map[string]string{
		"appId": "2021000000000001", "privateKey": priv, "alipayPublicKey": pub, "gateway": srv.URL,
	}
	st, err := (alipayProvider{}).Query(context.Background(), "PAY-X", cfg)
	if err != nil {
		t.Fatalf("trade not exist should stay undecided, got %v", err)
	}
	if st.Paid || st.Failed {
		t.Fatalf("unexpected status %+v", st)
	}
}

func TestAlipayQueryRejectsForgedResponse(t *testing.T) {
	_, priv, pub := testRSA(t)
	forger, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	srv, _ := newAlipayTestGateway(t, forger, "alipay_trade_query_response", map[string]any{
		"code": "10000", "trade_no": "T1", "out_trade_no": "PAY-1", "trade_status": "TRADE_SUCCESS",
	})
	cfg := map[string]string{
		"appId": "2021000000000001", "privateKey": priv, "alipayPublicKey": pub, "gateway": srv.URL,
	}
	if _, err := (alipayProvider{}).Query(context.Background(), "PAY-1", cfg); err == nil {
		t.Fatal("forged gateway response accepted")
	}
}

func TestAlipayRefund(t *testing.T) {
	key, priv, pub := testRSA(t)
	srv, form := newAlipayTestGateway(t, key, "alipay_trade_refund_response", map[string]any{
		"code": "10000", "msg": "Success", "trade_no": "2026102922001234567890", "refund_fee": "1.00",
	})
	cfg := map[string]string{
		"appId": "2021000000000001", "privateKey": priv, "alipayPublicKey": pub, "gateway": srv.URL,
	}
	res, err := (alipayProvider{}).Refund(context.Background(), RefundRequest{
		RefundNo: "R-1", PaymentNo: "PAY-1", AmountMinor: 100, IdempotencyKey: "IDK-1",
	}, cfg)
	if err != nil {
		t.Fatal(err)
	}
	if !res.OK || res.Pending {
		t.Fatalf("unexpected refund result %+v", res)
	}
	if form.Get("method") != "alipay.trade.refund" {
		t.Fatalf("unexpected method %s", form.Get("method"))
	}
	biz := map[string]string{}
	if err := json.Unmarshal([]byte(form.Get("biz_content")), &biz); err != nil {
		t.Fatal(err)
	}
	if biz["out_request_no"] != "IDK-1" || biz["refund_amount"] != "1.00" || biz["out_trade_no"] != "PAY-1" {
		t.Fatalf("unexpected refund biz_content %v", biz)
	}
}

func TestAlipayRefundRejected(t *testing.T) {
	key, priv, pub := testRSA(t)
	srv, _ := newAlipayTestGateway(t, key, "alipay_trade_refund_response", map[string]any{
		"code": "40004", "msg": "Business Failed",
		"sub_code": "ACQ.SELLER_BALANCE_NOT_ENOUGH", "sub_msg": "卖家余额不足",
	})
	cfg := map[string]string{
		"appId": "2021000000000001", "privateKey": priv, "alipayPublicKey": pub, "gateway": srv.URL,
	}
	res, err := (alipayProvider{}).Refund(context.Background(), RefundRequest{
		RefundNo: "R-1", PaymentNo: "PAY-1", AmountMinor: 100, IdempotencyKey: "IDK-1",
	}, cfg)
	if err != nil {
		t.Fatal(err)
	}
	if res.OK || res.Pending || !strings.Contains(res.Reason, "ACQ.SELLER_BALANCE_NOT_ENOUGH") {
		t.Fatalf("unexpected refund result %+v", res)
	}
}

func TestAlipayRefundKeyFallsBackToRefundNo(t *testing.T) {
	if got := alipayRefundKey(RefundRequest{RefundNo: "R-1", IdempotencyKey: "IDK-1"}); got != "IDK-1" {
		t.Fatalf("idempotency key should win, got %s", got)
	}
	if got := alipayRefundKey(RefundRequest{RefundNo: "R-1"}); got != "R-1" {
		t.Fatalf("fallback should be refund no, got %s", got)
	}
}

func TestExpireExpress(t *testing.T) {
	if got := expireExpress(time.Time{}, 30*time.Minute); got != "30m" {
		t.Fatalf("zero time should fall back, got %s", got)
	}
	if got := expireExpress(time.Now().Add(2*time.Hour+10*time.Minute), 30*time.Minute); got != "130m" {
		t.Fatalf("want 130m, got %s", got)
	}
	if got := expireExpress(time.Now().Add(10*time.Second), 0); got != "1m" {
		t.Fatalf("minimum should be 1m, got %s", got)
	}
}

// ackTestProvider 用最少的实现验证 notify 会按渠道要求回写 ack 报文。
type ackTestProvider struct{}

func (ackTestProvider) Name() string { return "acktest" }

func (ackTestProvider) Create(context.Context, PayRequest, map[string]string) (*PayParams, error) {
	return &PayParams{}, nil
}

func (ackTestProvider) Query(context.Context, string, map[string]string) (*ChannelStatus, error) {
	return &ChannelStatus{}, nil
}

func (ackTestProvider) Refund(context.Context, RefundRequest, map[string]string) (*RefundResult, error) {
	return &RefundResult{OK: true}, nil
}

func (ackTestProvider) VerifyCallback(in CallbackInput, _ map[string]string) (*CallbackResult, error) {
	return &CallbackResult{
		PaymentNo: strings.TrimSpace(string(in.Body)), Paid: true, GatewayStatus: "SUCCESS",
	}, nil
}

func (ackTestProvider) CallbackAck() (string, string) {
	return "text/plain; charset=utf-8", "success"
}

// 无 AckProvider 的渠道（默认 JSON 应答）用来证明既有通道的行为没被改掉。
type plainTestProvider struct{}

func (plainTestProvider) Name() string { return "plaintest" }

func (plainTestProvider) Create(context.Context, PayRequest, map[string]string) (*PayParams, error) {
	return &PayParams{}, nil
}

func (plainTestProvider) Query(context.Context, string, map[string]string) (*ChannelStatus, error) {
	return &ChannelStatus{}, nil
}

func (plainTestProvider) Refund(context.Context, RefundRequest, map[string]string) (*RefundResult, error) {
	return &RefundResult{OK: true}, nil
}

func (plainTestProvider) VerifyCallback(in CallbackInput, _ map[string]string) (*CallbackResult, error) {
	return &CallbackResult{
		PaymentNo: strings.TrimSpace(string(in.Body)), Paid: true, GatewayStatus: "SUCCESS",
	}, nil
}

func newNotifyRequest(t *testing.T, s *Service, channel string) *httptest.ResponseRecorder {
	t.Helper()
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/v1/plugins/pay/notify/"+channel, strings.NewReader("PAY1"))
	c.Params = gin.Params{{Key: "channelCode", Value: channel}}
	(&handler{svc: s}).notify(c)
	return w
}

func expectDuplicateCallback(m sqlmock.Sqlmock, channel, provider string) {
	m.ExpectQuery("SELECT \\* FROM `plg_pay_channel` WHERE code = \\?").
		WillReturnRows(channelRow(channel, provider, ""))
	m.ExpectBegin()
	m.ExpectQuery("SELECT \\* FROM `plg_pay_order` WHERE payment_no = \\?").
		WillReturnRows(orderRow("PAY1", StatusSucceeded, 500, 0))
	m.ExpectCommit()
	m.ExpectBegin()
	m.ExpectExec("INSERT INTO `plg_pay_callback`").WillReturnResult(sqlmock.NewResult(1, 1))
	m.ExpectCommit()
}

func TestNotifyUsesProviderAck(t *testing.T) {
	RegisterProvider(ackTestProvider{})
	db, m := mockDB(t)
	s := NewService(db, nopLog())
	m.MatchExpectationsInOrder(true)
	expectDuplicateCallback(m, "ack01", "acktest")
	// CallbackAck 再次按 code 取渠道（应答格式与验签路径一致）。
	m.ExpectQuery("SELECT \\* FROM `plg_pay_channel` WHERE code = \\?").
		WillReturnRows(channelRow("ack01", "acktest", ""))

	w := newNotifyRequest(t, s, "ack01")
	if w.Code != http.StatusOK {
		t.Fatalf("status %d body %s", w.Code, w.Body.String())
	}
	if w.Body.String() != "success" {
		t.Fatalf("alipay-style ack should be bare success, got %q", w.Body.String())
	}
	if ct := w.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/plain") {
		t.Fatalf("ack content-type = %q", ct)
	}
	if err := m.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestNotifyStillReturnsJSONWithoutAckProvider(t *testing.T) {
	RegisterProvider(plainTestProvider{})
	db, m := mockDB(t)
	s := NewService(db, nopLog())
	m.MatchExpectationsInOrder(true)
	expectDuplicateCallback(m, "plain01", "plaintest")
	// 没有 AckProvider 也要查一次渠道才知道：查完退回 JSON 应答。
	m.ExpectQuery("SELECT \\* FROM `plg_pay_channel` WHERE code = \\?").
		WillReturnRows(channelRow("plain01", "plaintest", ""))

	w := newNotifyRequest(t, s, "plain01")
	if w.Code != http.StatusOK {
		t.Fatalf("status %d body %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), `"paymentNo"`) {
		t.Fatalf("expected JSON body, got %q", w.Body.String())
	}
	if err := m.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestUpsertChannelRequiresDeclaredCredentials(t *testing.T) {
	db, _ := mockDB(t)
	s := NewService(db, nopLog())
	_, err := s.UpsertChannel(context.Background(), SaveChannelInput{
		Code: "ali01", Name: "支付宝", Provider: "alipay",
	})
	if err == nil {
		t.Fatal("channel without credentials accepted")
	}
	for _, k := range []string{"appId", "privateKey", "alipayPublicKey"} {
		if !strings.Contains(err.Error(), k) {
			t.Fatalf("error should name missing %s: %v", k, err)
		}
	}
}
