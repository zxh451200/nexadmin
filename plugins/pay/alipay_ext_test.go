package pay

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"nexadmin/core/response"
)

// 支付宝渠道「可选能力」这一批的测试：回调防重放、退款查询、预授权、账单下载、代付、
// JSAPI/当面付下单与能力矩阵。网关桩复用 providers_test.go 的 newAlipayTestGateway。

var (
	_ Provider            = alipayProvider{}
	_ CapabilityProvider  = alipayProvider{}
	_ RefundQueryProvider = alipayProvider{}
	_ AuthProvider        = alipayProvider{}
	_ BillProvider        = alipayProvider{}
	_ PayoutProvider      = alipayProvider{}
	_ SchemaProvider      = alipayProvider{}
	_ AckProvider         = alipayProvider{}
)

// alipayNotifyBody 用指定 sign_type 给异步通知签名；signType 传 "" 表示报文里不带该字段。
// 待签串按支付宝规则独立拼（alipayCanonicalIndependent），不复用被测实现自证。
func alipayNotifyBody(key *rsa.PrivateKey, params map[string]string, signType string) []byte {
	v := url.Values{}
	for k, val := range params {
		v.Set(k, val)
	}
	if signType != "" {
		v.Set("sign_type", signType)
	}
	v.Set("sign", rsaSignSHA256(key, alipayCanonicalIndependent(params, true)))
	return []byte(v.Encode())
}

// alipayNotifyBase 是一条合法的支付成功通知，notify_time 由用例决定。
func alipayNotifyBase(notifyTime string) map[string]string {
	return map[string]string{
		"app_id":       "2021000000000001",
		"seller_id":    "2088000000000001",
		"notify_id":    "2026110100012345678901234567890001",
		"notify_type":  "trade_status_sync",
		"out_trade_no": "PAY-1",
		"trade_no":     "2026102922001234567890",
		"trade_status": "TRADE_SUCCESS",
		"total_amount": "12.34",
		"notify_time":  notifyTime,
	}
}

func alipayCallbackCfg(priv, pub string) map[string]string {
	cfg := map[string]string{"appId": "2021000000000001", "alipayPublicKey": pub, "sellerId": "2088000000000001"}
	if priv != "" {
		cfg["privateKey"] = priv
	}
	return cfg
}

// TestAlipayNotifyClockSkew 通知时间只拒「超前」：伪造/时钟异常的信号；
// 超前 10 分钟拒，超前 1 分钟（时钟正常抖动）放行。
func TestAlipayNotifyClockSkew(t *testing.T) {
	key, _, pub := testRSA(t)
	cfg := alipayCallbackCfg("", pub)

	future := time.Now().In(alipayLoc).Add(10 * time.Minute).Format(alipayTimeLayout)
	res, err := (alipayProvider{}).VerifyCallback(
		CallbackInput{Body: alipayNotifyBody(key, alipayNotifyBase(future), "RSA2")}, cfg)
	if err == nil {
		t.Fatalf("notify_time 超前 10 分钟必须被拒，结果 %+v", res)
	}
	if !errors.Is(err, errAlipayStaleNotify) {
		t.Fatalf("超前拒绝必须是 stale 哨兵，got %v", err)
	}
	var rerr *response.Error
	if !errors.As(err, &rerr) || rerr.Key != errNotifyStaleKey {
		t.Fatalf("stale 拒绝必须带 PAY_NOTIFY_STALE 错误码，got %#v", err)
	}

	near := time.Now().In(alipayLoc).Add(1 * time.Minute).Format(alipayTimeLayout)
	res, err = (alipayProvider{}).VerifyCallback(
		CallbackInput{Body: alipayNotifyBody(key, alipayNotifyBase(near), "RSA2")}, cfg)
	if err != nil {
		t.Fatalf("超前 1 分钟是时钟抖动，不该拒: %v", err)
	}
	if !res.Paid {
		t.Fatalf("unexpected result %+v", res)
	}
}

// TestAlipayNotifyOldRetryStillAccepted 是这批最重要的用例：支付宝收到非 success 应答后
// 会用**同一个 notify_time**重投到 15 小时量级，20 小时前的合法重试仍然要入账。
// 过去方向一旦套硬窗，表现就是「用户已付款、订单永远不落账」。
func TestAlipayNotifyOldRetryStillAccepted(t *testing.T) {
	key, _, pub := testRSA(t)
	cfg := alipayCallbackCfg("", pub)
	old := time.Now().In(alipayLoc).Add(-20 * time.Hour).Format(alipayTimeLayout)
	res, err := (alipayProvider{}).VerifyCallback(
		CallbackInput{Body: alipayNotifyBody(key, alipayNotifyBase(old), "RSA2")}, cfg)
	if err != nil {
		t.Fatalf("20 小时前的合法重投被丢了: %v", err)
	}
	if !res.Paid || res.PaymentNo != "PAY-1" || res.AmountMinor != 1234 {
		t.Fatalf("unexpected result %+v", res)
	}
	// 更老的（3 天）同样入账：防重放不靠时间窗，靠 out_trade_no + 金额 + 状态机幂等。
	older := time.Now().In(alipayLoc).Add(-72 * time.Hour).Format(alipayTimeLayout)
	if _, err := (alipayProvider{}).VerifyCallback(
		CallbackInput{Body: alipayNotifyBody(key, alipayNotifyBase(older), "RSA2")}, cfg); err != nil {
		t.Fatalf("历史重投被拒: %v", err)
	}
}

// TestAlipayNotifyUnparsableTimeRejected notify_time 缺失或格式不对 → 拒。
func TestAlipayNotifyUnparsableTimeRejected(t *testing.T) {
	key, _, pub := testRSA(t)
	cfg := alipayCallbackCfg("", pub)
	for _, tc := range []struct{ name, v string }{{"empty", ""}, {"garbage", "not-a-time"}, {"iso", "2026-11-01T10:00:00Z"}} {
		p := alipayNotifyBase(tc.v)
		if tc.v == "" {
			delete(p, "notify_time")
		}
		if _, err := (alipayProvider{}).VerifyCallback(CallbackInput{Body: alipayNotifyBody(key, p, "RSA2")}, cfg); err == nil {
			t.Fatalf("notify_time=%q 应该被拒", tc.name)
		}
	}
}

// TestAlipayNotifySignTypeDowngrade 显式声明非 RSA2 的报文一律拒（防签名降级）；
// RSA2 与不带该字段（老报文）都放行。
func TestAlipayNotifySignTypeDowngrade(t *testing.T) {
	key, _, pub := testRSA(t)
	cfg := alipayCallbackCfg("", pub)
	now := time.Now().In(alipayLoc).Format(alipayTimeLayout)
	for _, st := range []string{"RSA", "rsa", "RSA1", "MD5", "HMAC-SHA256"} {
		if _, err := (alipayProvider{}).VerifyCallback(
			CallbackInput{Body: alipayNotifyBody(key, alipayNotifyBase(now), st)}, cfg); err == nil {
			t.Fatalf("sign_type=%s 的降级报文被接受", st)
		}
	}
	for _, st := range []string{"RSA2", "rsa2", ""} {
		if _, err := (alipayProvider{}).VerifyCallback(
			CallbackInput{Body: alipayNotifyBody(key, alipayNotifyBase(now), st)}, cfg); err != nil {
			t.Fatalf("sign_type=%q 是合法形态，不该拒: %v", st, err)
		}
	}
}

// TestAlipayNotifyIDCarriedThrough notify_id 必须透传到回调审计（只取证，不做唯一约束）。
func TestAlipayNotifyIDCarriedThrough(t *testing.T) {
	key, _, pub := testRSA(t)
	cfg := alipayCallbackCfg("", pub)
	now := time.Now().In(alipayLoc).Format(alipayTimeLayout)
	res, err := (alipayProvider{}).VerifyCallback(
		CallbackInput{Body: alipayNotifyBody(key, alipayNotifyBase(now), "RSA2")}, cfg)
	if err != nil {
		t.Fatal(err)
	}
	if res.NotifyID != "2026110100012345678901234567890001" {
		t.Fatalf("notify_id 没透传: %+v", res)
	}
}

// TestAlipayQueryRefund 退款结果查询：成功 / 处理中 / 渠道查无 三分支。
// 「查无」既不是失败也不是成功，服务层要保持 pending，不能收敛成终态。
func TestAlipayQueryRefund(t *testing.T) {
	key, priv, pub := testRSA(t)
	cfg := func(url string) map[string]string {
		return map[string]string{"appId": "2021000000000001", "privateKey": priv, "alipayPublicKey": pub, "gateway": url}
	}
	req := RefundQueryRequest{RefundNo: "R-1", PaymentNo: "PAY-1", IdempotencyKey: "IDK-1", AmountMinor: 100}

	t.Run("命中成功", func(t *testing.T) {
		srv, form := newAlipayTestGateway(t, key, "alipay_trade_fastpay_refund_query_response", map[string]any{
			"code": "10000", "msg": "Success", "trade_no": "2026102922001234567890",
			"out_trade_no": "PAY-1", "refund_amount": "1.00", "fund_change": "Y",
		})
		res, err := (alipayProvider{}).QueryRefund(context.Background(), req, cfg(srv.URL))
		if err != nil {
			t.Fatal(err)
		}
		if !res.Found || !res.OK || res.Pending {
			t.Fatalf("unexpected %+v", res)
		}
		if res.ChannelRefundID != "2026102922001234567890" {
			t.Fatalf("channel refund id = %q", res.ChannelRefundID)
		}
		if form.Get("method") != "alipay.trade.fastpay.refund.query" {
			t.Fatalf("method = %s", form.Get("method"))
		}
		biz := map[string]string{}
		if err := json.Unmarshal([]byte(form.Get("biz_content")), &biz); err != nil {
			t.Fatal(err)
		}
		// 幂等号必须是发起退款时那一个，否则渠道当另一笔退款处理。
		if biz["out_request_no"] != "IDK-1" || biz["out_trade_no"] != "PAY-1" {
			t.Fatalf("unexpected biz %v", biz)
		}
	})

	t.Run("命中处理中", func(t *testing.T) {
		srv, _ := newAlipayTestGateway(t, key, "alipay_trade_fastpay_refund_query_response", map[string]any{
			"code": "10000", "msg": "Success", "trade_no": "T1",
			"refund_amount": "0.00", "fund_change": "N", "refund_status": "REFUND_IN_PROCESS",
		})
		res, err := (alipayProvider{}).QueryRefund(context.Background(), req, cfg(srv.URL))
		if err != nil {
			t.Fatal(err)
		}
		if !res.Found || res.OK || !res.Pending {
			t.Fatalf("处理中不该收敛成终态: %+v", res)
		}
	})

	t.Run("渠道查无", func(t *testing.T) {
		srv, _ := newAlipayTestGateway(t, key, "alipay_trade_fastpay_refund_query_response", map[string]any{
			"code": "40004", "msg": "Business Failed", "sub_code": "ACQ.TRADE_NOT_EXIST", "sub_msg": "交易不存在",
		})
		res, err := (alipayProvider{}).QueryRefund(context.Background(), req, cfg(srv.URL))
		if err != nil {
			t.Fatalf("查无此笔不该报渠道错误: %v", err)
		}
		if res.Found || res.OK || res.Pending {
			t.Fatalf("查无必须是 Found=false: %+v", res)
		}
	})

	t.Run("金额为零按查无", func(t *testing.T) {
		srv, _ := newAlipayTestGateway(t, key, "alipay_trade_fastpay_refund_query_response", map[string]any{
			"code": "10000", "msg": "Success", "trade_no": "T1", "refund_amount": "0.00", "fund_change": "N",
		})
		res, err := (alipayProvider{}).QueryRefund(context.Background(), req, cfg(srv.URL))
		if err != nil || res.Found || res.OK {
			t.Fatalf("退款金额 0 不能当成功: %+v err=%v", res, err)
		}
	})

	t.Run("渠道拒绝算错误", func(t *testing.T) {
		srv, _ := newAlipayTestGateway(t, key, "alipay_trade_fastpay_refund_query_response", map[string]any{
			"code": "40004", "msg": "Business Failed", "sub_code": "ACQ.REFUND_AMOUNT_NOT_MATCH", "sub_msg": "退款金额不匹配",
		})
		if _, err := (alipayProvider{}).QueryRefund(context.Background(), req, cfg(srv.URL)); err == nil {
			t.Fatal("渠道明确拒绝应返回错误")
		}
	})
}

func TestAlipayCreateJSAPI(t *testing.T) {
	key, priv, pub := testRSA(t)
	srv, form := newAlipayTestGateway(t, key, "alipay_trade_create_response", map[string]any{
		"code": "10000", "msg": "Success", "trade_no": "2026110122001400000123456789", "out_trade_no": "PAY-1",
	})
	cfg := map[string]string{"appId": "2021000000000001", "privateKey": priv, "alipayPublicKey": pub, "gateway": srv.URL}
	req := PayRequest{
		PaymentNo: "PAY-1", Subject: "测试商品", AmountMinor: 1234, Currency: "CNY", Method: "jsapi",
		NotifyURL: "https://shop.example.com/api/v1/plugins/pay/notify/ali01",
	}
	// 缺买家标识：渠道必拒，本地先报凭据错误，不发无意义的请求。
	if _, err := (alipayProvider{}).Create(context.Background(), req, cfg); err == nil {
		t.Fatal("JSAPI 缺少买家标识应报错")
	}
	if form.Get("method") == "alipay.trade.create" {
		t.Fatal("缺买家标识时不该把请求发到网关")
	}
	req.PayerOpenID = "2088000000000001"
	params, err := (alipayProvider{}).Create(context.Background(), req, cfg)
	if err != nil {
		t.Fatal(err)
	}
	if form.Get("method") != "alipay.trade.create" {
		t.Fatalf("method = %s", form.Get("method"))
	}
	biz := map[string]string{}
	if err := json.Unmarshal([]byte(form.Get("biz_content")), &biz); err != nil {
		t.Fatal(err)
	}
	if biz["buyer_id"] != "2088000000000001" || biz["product_code"] != "JSAPI_PAY" {
		t.Fatalf("unexpected biz %v", biz)
	}
	// my.tradePay 要的就是渠道返回的 trade_no，不是本地编的号。
	if params.Params["tradeNo"] != "2026110122001400000123456789" {
		t.Fatalf("tradeNo 没透传给前端: %+v", params)
	}
	if params.PrepayID != "2026110122001400000123456789" {
		t.Fatalf("prepayID = %q", params.PrepayID)
	}

	// 非 2088 形态按开放平台 open_id 下发。
	t.Run("open_id 形态", func(t *testing.T) {
		srv2, form2 := newAlipayTestGateway(t, key, "alipay_trade_create_response", map[string]any{
			"code": "10000", "trade_no": "T2",
		})
		cfg2 := map[string]string{"appId": "2021000000000001", "privateKey": priv, "alipayPublicKey": pub, "gateway": srv2.URL}
		req2 := req
		req2.PayerOpenID = "072Ah1v3Kp0dmEXAMPLE027x"
		if _, err := (alipayProvider{}).Create(context.Background(), req2, cfg2); err != nil {
			t.Fatal(err)
		}
		b := map[string]string{}
		_ = json.Unmarshal([]byte(form2.Get("biz_content")), &b)
		if b["buyer_open_id"] != req2.PayerOpenID || b["buyer_id"] != "" {
			t.Fatalf("unexpected biz %v", b)
		}
	})

	// 渠道没返回 trade_no 就是没有这笔单：报错，不伪造。
	srv3, _ := newAlipayTestGateway(t, key, "alipay_trade_create_response", map[string]any{"code": "10000"})
	cfg3 := map[string]string{"appId": "2021000000000001", "privateKey": priv, "alipayPublicKey": pub, "gateway": srv3.URL}
	if _, err := (alipayProvider{}).Create(context.Background(), req, cfg3); err == nil {
		t.Fatal("渠道未返回 trade_no 时必须报错")
	}
}

func TestAlipayCreateMicropay(t *testing.T) {
	key, priv, pub := testRSA(t)
	cfg := func(u string) map[string]string {
		return map[string]string{"appId": "2021000000000001", "privateKey": priv, "alipayPublicKey": pub, "gateway": u}
	}
	req := PayRequest{
		PaymentNo: "PAY-1", Subject: "便利店", AmountMinor: 999, Currency: "CNY", Method: "micropay",
		NotifyURL: "https://shop.example.com/api/v1/plugins/pay/notify/ali01",
	}
	if _, err := (alipayProvider{}).Create(context.Background(), req, cfg("http://127.0.0.1:0")); err == nil {
		t.Fatal("付款码支付缺少 auth_code 应报错")
	}
	req.PayerOpenID = "287812345678901234"

	t.Run("同步成功", func(t *testing.T) {
		srv, form := newAlipayTestGateway(t, key, "alipay_trade_pay_response", map[string]any{
			"code": "10000", "msg": "Success", "trade_no": "T9", "out_trade_no": "PAY-1",
			"trade_status": "TRADE_SUCCESS", "total_amount": "9.99",
		})
		params, err := (alipayProvider{}).Create(context.Background(), req, cfg(srv.URL))
		if err != nil {
			t.Fatal(err)
		}
		if form.Get("method") != "alipay.trade.pay" {
			t.Fatalf("method = %s", form.Get("method"))
		}
		biz := map[string]string{}
		if err := json.Unmarshal([]byte(form.Get("biz_content")), &biz); err != nil {
			t.Fatal(err)
		}
		if biz["auth_code"] != req.PayerOpenID || biz["scene"] != "barcode" || biz["product_code"] != "FACE_TO_FACE_PAYMENT" {
			t.Fatalf("unexpected biz %v", biz)
		}
		// 当面付是同步接口：渠道回什么状态就透传什么，状态机仍由回调/查询翻转。
		if params.Params["tradeStatus"] != "TRADE_SUCCESS" || params.Params["tradeNo"] != "T9" {
			t.Fatalf("同步状态没透传: %+v", params)
		}
	})

	t.Run("等待输密码", func(t *testing.T) {
		srv, _ := newAlipayTestGateway(t, key, "alipay_trade_pay_response", map[string]any{
			"code": "10003", "msg": "Success", "out_trade_no": "PAY-1",
		})
		params, err := (alipayProvider{}).Create(context.Background(), req, cfg(srv.URL))
		if err != nil {
			t.Fatalf("10003 是渠道已受理待用户输密码，不是失败: %v", err)
		}
		if params.Params["tradeStatus"] != "WAITING_PAYMENT" {
			t.Fatalf("unexpected params %+v", params)
		}
	})

	t.Run("渠道拒绝", func(t *testing.T) {
		srv, _ := newAlipayTestGateway(t, key, "alipay_trade_pay_response", map[string]any{
			"code": "40004", "msg": "Business Failed", "sub_code": "ACQ.PAYMENT_AUTH_CODE_INVALID", "sub_msg": "付款码无效",
		})
		if _, err := (alipayProvider{}).Create(context.Background(), req, cfg(srv.URL)); err == nil {
			t.Fatal("无效付款码必须报错")
		}
	})
}

// ---- 预授权 ----

func TestAlipayAuthProvider(t *testing.T) {
	key, priv, pub := testRSA(t)
	cfg := func(u string) map[string]string {
		return map[string]string{"appId": "2021000000000001", "privateKey": priv, "alipayPublicKey": pub, "gateway": u}
	}
	freeze := AuthFreezeRequest{
		AuthNo: "AUTH-1", AmountMinor: 500000, Currency: "CNY", Subject: "酒店押金",
		PayerID: "2088000000000009", ExpireAt: time.Now().Add(24 * time.Hour),
	}

	t.Run("冻结拿到真实授权号", func(t *testing.T) {
		srv, form := newAlipayTestGateway(t, key, "alipay_fund_auth_order_freeze_response", map[string]any{
			"code": "10000", "msg": "Success", "auth_no": "20261101000012345678", "operation_id": "OP1", "amount": "5000.00",
		})
		res, err := (alipayProvider{}).Freeze(context.Background(), freeze, cfg(srv.URL))
		if err != nil {
			t.Fatal(err)
		}
		if res.ChannelAuthNo != "20261101000012345678" || res.AmountMinor != 500000 {
			t.Fatalf("unexpected %+v", res)
		}
		if form.Get("method") != "alipay.fund.auth.order.freeze" {
			t.Fatalf("method = %s", form.Get("method"))
		}
		biz := map[string]string{}
		_ = json.Unmarshal([]byte(form.Get("biz_content")), &biz)
		if biz["out_order_no"] != "AUTH-1" || biz["buyer_id"] != "2088000000000009" || biz["product_code"] != "PRE_AUTH_ONLINE" {
			t.Fatalf("unexpected biz %v", biz)
		}
		if biz["total_amount"] != "5000.00" || biz["expiry_time"] == "" {
			t.Fatalf("unexpected biz %v", biz)
		}
	})

	t.Run("缺买家标识报凭据错误", func(t *testing.T) {
		srv, form := newAlipayTestGateway(t, key, "alipay_fund_auth_order_freeze_response", map[string]any{"code": "10000"})
		in := freeze
		in.PayerID = ""
		if _, err := (alipayProvider{}).Freeze(context.Background(), in, cfg(srv.URL)); err == nil {
			t.Fatal("缺买家标识应报凭据错误")
		}
		if form.Get("method") != "" {
			t.Fatal("凭据不足时不该发请求到网关")
		}
	})

	t.Run("应答没有授权号不伪造", func(t *testing.T) {
		srv, _ := newAlipayTestGateway(t, key, "alipay_fund_auth_order_freeze_response", map[string]any{
			"code": "10000", "operation_id": "OP1",
		})
		if _, err := (alipayProvider{}).Freeze(context.Background(), freeze, cfg(srv.URL)); err == nil {
			t.Fatal("渠道没给 auth_no 时必须报错，不能本地造号")
		}
	})

	t.Run("未签约回 PAY_CHANNEL_ERROR", func(t *testing.T) {
		srv, _ := newAlipayTestGateway(t, key, "alipay_fund_auth_order_freeze_response", map[string]any{
			"code": "40004", "msg": "Business Failed", "sub_code": "ACQ.PERMISSION_NOT_MATCH", "sub_msg": "商户没有签约该功能",
		})
		_, err := (alipayProvider{}).Freeze(context.Background(), freeze, cfg(srv.URL))
		if err == nil {
			t.Fatal("未签约必须报错")
		}
		var rerr *response.Error
		if !errors.As(err, &rerr) || rerr.Key != errChannelError {
			t.Fatalf("未签约要报 PAY_CHANNEL_ERROR，got %#v", err)
		}
		if !strings.Contains(err.Error(), "ACQ.PERMISSION_NOT_MATCH") || !strings.Contains(err.Error(), "商户没有签约该功能") {
			t.Fatalf("渠道 sub_code/sub_msg 要原样回传: %v", err)
		}
	})

	t.Run("请款", func(t *testing.T) {
		srv, form := newAlipayTestGateway(t, key, "alipay_fund_auth_order_confirm_response", map[string]any{
			"code": "10000", "auth_no": "20261101000012345678", "trade_no": "T-CONFIRM", "amount": "100.00",
		})
		res, err := (alipayProvider{}).CompleteAuth(context.Background(), AuthCompleteRequest{
			AuthNo: "AUTH-1", ChannelAuthNo: "20261101000012345678", AmountMinor: 10000, PaymentNo: "PAY-2",
		}, cfg(srv.URL))
		if err != nil {
			t.Fatal(err)
		}
		if res.ChannelAuthNo != "20261101000012345678" {
			t.Fatalf("unexpected %+v", res)
		}
		biz := map[string]string{}
		_ = json.Unmarshal([]byte(form.Get("biz_content")), &biz)
		if biz["out_request_no"] != "PAY-2" || biz["auth_no"] != "20261101000012345678" || biz["amount"] != "100.00" {
			t.Fatalf("unexpected biz %v", biz)
		}
		// 没有渠道授权号就请款：本地拦下，不去猜。
		bad := AuthCompleteRequest{AuthNo: "AUTH-1", AmountMinor: 1, PaymentNo: "PAY-3"}
		if _, err := (alipayProvider{}).CompleteAuth(context.Background(), bad, cfg(srv.URL)); err == nil {
			t.Fatal("缺少 auth_no 的请款必须报错")
		}
	})

	t.Run("撤销", func(t *testing.T) {
		srv, form := newAlipayTestGateway(t, key, "alipay_fund_auth_operation_cancel_response", map[string]any{
			"code": "10000", "auth_no": "20261101000012345678", "operation_id": "OP2",
		})
		res, err := (alipayProvider{}).CancelAuth(context.Background(), AuthCancelRequest{
			AuthNo: "AUTH-1", ChannelAuthNo: "20261101000012345678", Reason: "客人离店",
		}, cfg(srv.URL))
		if err != nil {
			t.Fatal(err)
		}
		if res.ChannelAuthNo != "20261101000012345678" {
			t.Fatalf("unexpected %+v", res)
		}
		biz := map[string]string{}
		_ = json.Unmarshal([]byte(form.Get("biz_content")), &biz)
		if biz["operation_type"] != "CANCEL" || biz["out_request_no"] != "CANCEL-AUTH-1" {
			t.Fatalf("unexpected biz %v", biz)
		}
	})

	t.Run("查询", func(t *testing.T) {
		srv, form := newAlipayTestGateway(t, key, "alipay_fund_auth_order_query_response", map[string]any{
			"code": "10000", "auth_no": "20261101000012345678", "order_status": "AUTHORIZED", "rest_amount": "4000.00",
		})
		res, err := (alipayProvider{}).QueryAuth(context.Background(), "AUTH-1", cfg(srv.URL))
		if err != nil {
			t.Fatal(err)
		}
		if res.ChannelAuthNo != "20261101000012345678" || res.Status != "AUTHORIZED" || res.AmountMinor != 400000 {
			t.Fatalf("unexpected %+v", res)
		}
		if !strings.Contains(form.Get("biz_content"), `"out_order_no":"AUTH-1"`) {
			t.Fatalf("biz = %s", form.Get("biz_content"))
		}
		// 查无此授权：Status=NOT_FOUND 而不是 error，交给上层判断，不算渠道故障。
		srv2, _ := newAlipayTestGateway(t, key, "alipay_fund_auth_order_query_response", map[string]any{
			"code": "40004", "sub_code": "ACQ.AUTH_NOT_EXIST", "sub_msg": "授权不存在",
		})
		notFound, err := (alipayProvider{}).QueryAuth(context.Background(), "AUTH-X", cfg(srv2.URL))
		if err != nil {
			t.Fatalf("查无授权不该报错: %v", err)
		}
		if notFound.Status != "NOT_FOUND" || notFound.ChannelAuthNo != "" {
			t.Fatalf("unexpected %+v", notFound)
		}
	})
}

// ---- 账单下载 ----

// alipayBillCSV 是一份形状接近真实的交易账单：# 说明段、列序打乱的表头、
// 千分位金额、退款行、汇总段（含分隔线）。按列名取值，所以列序怎么排都要解析对。
const alipayBillCSV = "" +
	"#支付宝（中国）网络技术有限公司 交易对账单\n" +
	"#说明：以下为账户流水明细，金额单位为元\n" +
	"账号,业务凭证号,金额（元）\n" + // 元信息行：有金额列但没有单号列，不能当表头
	"服务费(元),支付宝交易号,金额（元）,商户订单号,退款金额,业务名称,商户退款单号\n" +
	"0.06,2026110122001,\"1,234.56\",PAY-1,,在线支付 JSAPI,\n" +
	",2026110122001,,PAY-1,12.00,交易退款,R-1\n" +
	"0.01,2026110122002,0.50,PAY-2,,扫码支付,\n" +
	"---------支付宝（中国）网络技术有限公司---------\n" +
	"合计,,,1,247.06,,,,\n" +
	"#说明：以上数据仅供参考\n"

func alipayBillWant(t *testing.T, bill *Bill) {
	t.Helper()
	if bill.RowCount != len(bill.Rows) {
		t.Fatalf("RowCount=%d 与行数 %d 不一致（0 行与解析失败必须区分）", bill.RowCount, len(bill.Rows))
	}
	if len(bill.Rows) != 3 {
		t.Fatalf("rows = %d, want 3: %+v", len(bill.Rows), bill.Rows)
	}
	sum := sha256.Sum256([]byte(alipayBillCSV))
	if bill.Digest != hex.EncodeToString(sum[:]) {
		t.Fatalf("digest 不是账单原文的 sha256: %s", bill.Digest)
	}
	trade := bill.Rows[0]
	if trade.Kind != BillKindTrade || trade.PaymentNo != "PAY-1" || trade.ChannelTxnID != "2026110122001" ||
		trade.AmountMinor != 123456 || trade.FeeMinor != 6 {
		t.Fatalf("交易行解析错: %+v", trade)
	}
	refund := bill.Rows[1]
	if refund.Kind != BillKindRefund || refund.RefundNo != "R-1" || refund.PaymentNo != "PAY-1" ||
		refund.AmountMinor != 1200 {
		t.Fatalf("退款行解析错: %+v", refund)
	}
	third := bill.Rows[2]
	if third.Kind != BillKindTrade || third.AmountMinor != 50 || third.PaymentNo != "PAY-2" {
		t.Fatalf("第三行解析错: %+v", third)
	}
}

// newAlipayBillServers 起两个桩：网关（换 download_url）与文件服务器（回账单原文）。
func newAlipayBillServers(t *testing.T, key *rsa.PrivateKey, priv, pub string, payload []byte, status int) map[string]string {
	t.Helper()
	fileSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if status != http.StatusOK {
			w.WriteHeader(status)
		}
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		_, _ = w.Write(payload)
	}))
	t.Cleanup(fileSrv.Close)
	gwSrv, _ := newAlipayTestGateway(t, key, "alipay_data_dataservice_bill_downloadurl_query_response", map[string]any{
		"code": "10000", "msg": "Success", "bill_download_url": fileSrv.URL + "/bill.csv",
	})
	return map[string]string{
		"appId": "2021000000000001", "privateKey": priv, "alipayPublicKey": pub, "gateway": gwSrv.URL,
	}
}

func TestAlipayFetchBill(t *testing.T) {
	key, priv, pub := testRSA(t)
	cfg := newAlipayBillServers(t, key, priv, pub, []byte(alipayBillCSV), http.StatusOK)
	bill, err := (alipayProvider{}).FetchBill(context.Background(), BillRequest{BillDate: "2026-10-31"}, cfg)
	if err != nil {
		t.Fatal(err)
	}
	alipayBillWant(t, bill)
}

func TestAlipayFetchBillZip(t *testing.T) {
	key, priv, pub := testRSA(t)
	// 支付宝常把多个 CSV 打成 ZIP 返回，还夹带 macOS 噪声条目。
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	addEntry := func(name, data string) {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		_, _ = w.Write([]byte(data))
	}
	addEntry("__MACOSX/._business.csv", "junk")
	addEntry("business_bill_20261031.csv", alipayBillCSV)
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	cfg := newAlipayBillServers(t, key, priv, pub, buf.Bytes(), http.StatusOK)
	bill, err := (alipayProvider{}).FetchBill(context.Background(), BillRequest{BillDate: "2026-10-31"}, cfg)
	if err != nil {
		t.Fatal(err)
	}
	if bill.RowCount != 3 {
		t.Fatalf("ZIP 账单行数 = %d, want 3", bill.RowCount)
	}
	// digest 是下载原文（ZIP 本体）的 sha256，不是解出来的 CSV。
	sum := sha256.Sum256(buf.Bytes())
	if bill.Digest != hex.EncodeToString(sum[:]) {
		t.Fatalf("digest 应是原文 sha256")
	}
}

// TestAlipayFetchBillErrorPage 渠道把 HTML 错误页当账单返回时，必须报错——
// 返回 0 行成功在数据上跟「今天渠道确实没有交易」长得一样，对账会静默空洞。
func TestAlipayFetchBillErrorPage(t *testing.T) {
	key, priv, pub := testRSA(t)
	cfg := newAlipayBillServers(t, key, priv, pub,
		[]byte("<html><body><h1>500 Internal Server Error</h1></body></html>"), http.StatusOK)
	bill, err := (alipayProvider{}).FetchBill(context.Background(), BillRequest{BillDate: "2026-10-31"}, cfg)
	if err == nil {
		t.Fatalf("HTML 错误页被当成账单: %+v", bill)
	}
	if bill != nil {
		t.Fatal("解析失败不该返回 Bill")
	}
	// HTTP 非 200 同样报错。
	cfg500 := newAlipayBillServers(t, key, priv, pub, []byte("gateway error"), http.StatusInternalServerError)
	if _, err := (alipayProvider{}).FetchBill(context.Background(), BillRequest{BillDate: "2026-10-31"}, cfg500); err == nil {
		t.Fatal("HTTP 500 被当成空账单")
	}
	// 账单里没有流水行（只有说明段与汇总段）也要报错。
	cfgEmpty := newAlipayBillServers(t, key, priv, pub,
		[]byte("#说明段\n服务费(元),支付宝交易号,金额（元）,商户订单号\n合计,,,\n"), http.StatusOK)
	if _, err := (alipayProvider{}).FetchBill(context.Background(), BillRequest{BillDate: "2026-10-31"}, cfgEmpty); err == nil {
		t.Fatal("解析不出任何行必须报错，不能当账单为空")
	}
}

func TestAlipayFetchBillValidatesDate(t *testing.T) {
	_, priv, pub := testRSA(t)
	cfg := map[string]string{"appId": "a", "privateKey": priv, "alipayPublicKey": pub}
	for _, bad := range []string{"", "20261031", "2026/10/31", "2026-13-45"} {
		if _, err := (alipayProvider{}).FetchBill(context.Background(), BillRequest{BillDate: bad}, cfg); err == nil {
			t.Fatalf("账期 %q 应该被拒", bad)
		}
	}
}

// TestAlipayGatewayHTMLPage 未签约的产品（沙箱实测资金授权）网关直接回一段 HTML
// 错误页而不是 JSON：错误文案必须把形状与「未签约」线索说清，别让人从
//「不是 JSON」里猜是网络断了还是权限没开。
func TestAlipayGatewayHTMLPage(t *testing.T) {
	_, priv, pub := testRSA(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte("<!DOCTYPE html><html><head><title>支付宝 - 网上支付</title></head><body></body></html>"))
	}))
	t.Cleanup(srv.Close)
	cfg := map[string]string{"appId": "2021000000000001", "privateKey": priv, "alipayPublicKey": pub, "gateway": srv.URL}
	_, err := (alipayProvider{}).Query(context.Background(), "PAY-HTML-1", cfg)
	if err == nil {
		t.Fatal("HTML 错误页被当成正常应答")
	}
	msg := err.Error()
	if !strings.Contains(msg, "HTML") || !strings.Contains(msg, "未签约") {
		t.Fatalf("错误文案应说明是 HTML 错误页并给未签约线索：%v", msg)
	}
	if !strings.Contains(msg, "支付宝 - 网上支付") {
		t.Fatalf("错误文案应带上页面开头原文便于辨认：%v", msg)
	}
}

// ---- 代付 ----

func TestAlipayPayoutProvider(t *testing.T) {
	key, priv, pub := testRSA(t)
	cfg := func(u string) map[string]string {
		return map[string]string{"appId": "2021000000000001", "privateKey": priv, "alipayPublicKey": pub, "gateway": u}
	}
	in := TransferRequest{
		BatchNo: "PB-1", ItemNo: "PB-1-01", AmountMinor: 88800, Currency: "CNY",
		ReceiverType: "merchant", ReceiverID: "2088000000000009", ReceiverName: "某某商户", Remark: "10 月结算",
	}
	t.Run("转账成功", func(t *testing.T) {
		srv, form := newAlipayTestGateway(t, key, "alipay_fund_trans_uni_transfer_response", map[string]any{
			"code": "10000", "order_id": "202611010001", "pay_fund_order_id": "202611010002",
			"out_biz_no": "PB-1-01", "status": "SUCCESS", "trans_amount": "888.00",
		})
		res, err := (alipayProvider{}).Transfer(context.Background(), in, cfg(srv.URL))
		if err != nil {
			t.Fatal(err)
		}
		if !res.Accepted || res.Pending || res.State != "SUCCESS" {
			t.Fatalf("unexpected %+v", res)
		}
		if res.ChannelBatchNo != "202611010001" || res.ChannelDetailNo != "202611010002" {
			t.Fatalf("渠道单号没透传: %+v", res)
		}
		if form.Get("method") != "alipay.fund.trans.uni.transfer" {
			t.Fatalf("method = %s", form.Get("method"))
		}
		biz := map[string]any{}
		if err := json.Unmarshal([]byte(form.Get("biz_content")), &biz); err != nil {
			t.Fatal(err)
		}
		// out_biz_no 用 ItemNo：同一条重试用同一个号，渠道才不会重复出金。
		if biz["out_biz_no"] != "PB-1-01" || biz["trans_amount"] != "888.00" {
			t.Fatalf("unexpected biz %v", biz)
		}
		if biz["product_code"] != "TRANS_ACCOUNT_NO_PWD" || biz["biz_scene"] != "DIRECT_TRANSFER" {
			t.Fatalf("unexpected biz %v", biz)
		}
		payee, _ := biz["payee_info"].(map[string]any)
		if payee == nil || payee["account_type"] != "ALIPAY_USER_ID" || payee["identity"] != "2088000000000009" {
			t.Fatalf("收款方映射错: %v (%T)", biz["payee_info"], biz["payee_info"])
		}
	})

	t.Run("处理中", func(t *testing.T) {
		srv, _ := newAlipayTestGateway(t, key, "alipay_fund_trans_uni_transfer_response", map[string]any{
			"code": "10000", "order_id": "O1", "status": "DEALING",
		})
		res, err := (alipayProvider{}).Transfer(context.Background(), in, cfg(srv.URL))
		if err != nil {
			t.Fatal(err)
		}
		if !res.Accepted || !res.Pending {
			t.Fatalf("DEALING 必须是已受理未定: %+v", res)
		}
	})

	t.Run("渠道失败", func(t *testing.T) {
		srv, _ := newAlipayTestGateway(t, key, "alipay_fund_trans_uni_transfer_response", map[string]any{
			"code": "10000", "order_id": "O1", "status": "FAIL", "fail_reason": "收款账户户名与账号不一致",
		})
		res, err := (alipayProvider{}).Transfer(context.Background(), in, cfg(srv.URL))
		if err != nil {
			t.Fatal(err)
		}
		if res.Accepted || res.Pending || !strings.Contains(res.Reason, "户名") {
			t.Fatalf("FAIL 要报明确拒绝: %+v", res)
		}
	})

	t.Run("未签约", func(t *testing.T) {
		srv, _ := newAlipayTestGateway(t, key, "alipay_fund_trans_uni_transfer_response", map[string]any{
			"code": "40004", "msg": "Business Failed", "sub_code": "ACQ.SCENE_NOT_EXIST", "sub_msg": "商户未签约转账产品",
		})
		_, err := (alipayProvider{}).Transfer(context.Background(), in, cfg(srv.URL))
		if err == nil {
			t.Fatal("未签约必须报错")
		}
		var rerr *response.Error
		if !errors.As(err, &rerr) || rerr.Key != errChannelError {
			t.Fatalf("未签约要报 PAY_CHANNEL_ERROR，got %#v", err)
		}
		if !strings.Contains(err.Error(), "ACQ.SCENE_NOT_EXIST") {
			t.Fatalf("sub_code 要原样回传: %v", err)
		}
	})

	t.Run("收款方与幂等号校验", func(t *testing.T) {
		srv, form := newAlipayTestGateway(t, key, "alipay_fund_trans_uni_transfer_response", map[string]any{
			"code": "10000", "status": "SUCCESS",
		})
		bad := in
		bad.ReceiverID = "6222020000000000000" // 银行卡号：这个接口不收
		if _, err := (alipayProvider{}).Transfer(context.Background(), bad, cfg(srv.URL)); err == nil {
			t.Fatal("不支持的收款方标识要报凭据错误")
		}
		noKey := in
		noKey.ItemNo, noKey.BatchNo = "", ""
		if _, err := (alipayProvider{}).Transfer(context.Background(), noKey, cfg(srv.URL)); err == nil {
			t.Fatal("缺幂等号要报错")
		}
		// ItemNo 缺失时退回批次号，仍是确定性的幂等号。
		fallback := in
		fallback.ItemNo = ""
		if _, err := (alipayProvider{}).Transfer(context.Background(), fallback, cfg(srv.URL)); err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(form.Get("biz_content"), `"out_biz_no":"PB-1"`) {
			t.Fatalf("幂等号没退回批次号: %s", form.Get("biz_content"))
		}
	})

	t.Run("查询", func(t *testing.T) {
		srv, form := newAlipayTestGateway(t, key, "alipay_fund_trans_common_query_response", map[string]any{
			"code": "10000", "order_id": "O1", "status": "SUCCESS", "arr_amount": "888.00",
		})
		res, err := (alipayProvider{}).QueryTransfer(context.Background(), "PB-1-01", cfg(srv.URL))
		if err != nil {
			t.Fatal(err)
		}
		if res.State != "SUCCESS" || res.Pending || !res.Accepted {
			t.Fatalf("unexpected %+v", res)
		}
		if !strings.Contains(form.Get("biz_content"), `"out_biz_no":"PB-1-01"`) {
			t.Fatalf("查询没按幂等号: %s", form.Get("biz_content"))
		}
		// 渠道查无此单：保持未定（我们提交过，判失败会导致重复出金）。
		srv2, _ := newAlipayTestGateway(t, key, "alipay_fund_trans_common_query_response", map[string]any{
			"code": "40004", "sub_code": "ACQ.TRANSBILL_R_ERROR", "sub_msg": "单据不存在",
		})
		missing, err := (alipayProvider{}).QueryTransfer(context.Background(), "PB-X", cfg(srv2.URL))
		if err != nil {
			t.Fatalf("查无此单不该报错: %v", err)
		}
		if !missing.Pending || missing.Accepted || missing.State != "NOT_FOUND" {
			t.Fatalf("查无此单必须保持未定: %+v", missing)
		}
	})
}

// TestAlipayCapabilities 能力矩阵必须与 Create/可选接口的真实实现一致：
// 虚报能力会让前端放开一个服务端必拒的入口。
func TestAlipayCapabilities(t *testing.T) {
	caps := ProviderCaps(alipayProvider{})
	if len(caps.Currencies) != 1 || caps.Currencies[0] != "CNY" {
		t.Fatalf("支付宝只收 CNY，got %v", caps.Currencies)
	}
	if !caps.supportsCurrency("cny") || caps.supportsCurrency("USD") {
		t.Fatalf("币种判定错: %v", caps)
	}
	for _, m := range []string{"native", "qr", "page", "wap", "h5", "app", "jsapi", "micropay"} {
		if !caps.supportsMethod(m) {
			t.Fatalf("声明缺方法 %s", m)
		}
	}
	if caps.supportsMethod("bank") {
		t.Fatal("未实现的下单方式不该出现在能力矩阵里")
	}
	if !caps.RefundQuery || !caps.Auth || !caps.Bill || !caps.Payout {
		t.Fatalf("本轮实现的可选能力都要声明: %+v", caps)
	}
	// 能力矩阵里的每个 method 都要被 Create 认（否则是虚报）。走桩网关，不碰真实网络：
	// 桩应答的节点名和下单接口对不上，会让 call 报「缺少节点」，但那不是「不支持的支付方式」，
	// 正好用来只判定 method 分派。
	key, priv, pub := testRSA(t)
	srv, _ := newAlipayTestGateway(t, key, "unexpected_node", map[string]any{"code": "10000"})
	cfg := map[string]string{"appId": "a", "privateKey": priv, "alipayPublicKey": pub, "gateway": srv.URL}
	for _, m := range caps.Methods {
		req := PayRequest{PaymentNo: "PAY-1", AmountMinor: 1, Currency: "CNY", Method: m,
			PayerOpenID: "2088000000000001", NotifyURL: "https://shop.example.com/notify"}
		if _, err := (alipayProvider{}).Create(context.Background(), req, cfg); err != nil &&
			strings.Contains(err.Error(), "不支持的支付方式") {
			t.Fatalf("Capabilities 声明了 %s，但 Create 说不支持", m)
		}
	}
}

// TestAlipayStaleNotifyClassificationIsVisible 固定住「主会话怎么分类」的契约：
// 超前拒必须是 PAY_NOTIFY_STALE 错误码 + 包内哨兵，两条路都能识别，
// 否则回调审计会退化成 sign_invalid，把时钟问题当成伪造。
func TestAlipayStaleNotifyClassificationIsVisible(t *testing.T) {
	key, _, pub := testRSA(t)
	cfg := alipayCallbackCfg("", pub)
	future := time.Now().In(alipayLoc).Add(30 * time.Minute).Format(alipayTimeLayout)
	_, err := (alipayProvider{}).VerifyCallback(
		CallbackInput{Body: alipayNotifyBody(key, alipayNotifyBase(future), "RSA2")}, cfg)
	var rerr *response.Error
	if !errors.As(err, &rerr) {
		t.Fatalf("stale 拒绝要带业务错误码: %#v", err)
	}
	if rerr.Status != http.StatusBadRequest {
		t.Fatalf("status = %d", rerr.Status)
	}
	if rerr.Key != errNotifyStaleKey {
		t.Fatalf("key = %s, want %s", rerr.Key, errNotifyStaleKey)
	}
	if !strings.Contains(fmt.Sprint(err), "notify_time") {
		t.Fatalf("错误里要能看到出问题的字段: %v", err)
	}
}
