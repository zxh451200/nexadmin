package pay

import (
	"context"
	"encoding/json"
	"html/template"
	"net/url"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"nexadmin/core/response"
)

// 模拟收银台：P0 mock 渠道的"用户支付页"。它不是玩具流程——确认支付走的是与真实网关
// 完全一致的回调管线（服务端代签 → 验签 → 幂等翻转 → 事件出站箱），换真实渠道时业务侧零改动。

// MockPay 以渠道身份签发一笔 mock 回调并送入 HandleCallback（收银台确认按钮用）。
func (s *Service) MockPay(ctx context.Context, paymentNo, outcome string) (*Order, error) {
	paymentNo, outcome = strings.TrimSpace(paymentNo), strings.TrimSpace(outcome)
	if paymentNo == "" || (outcome != "success" && outcome != "fail") {
		return nil, response.BadRequest("error.bad_request")
	}
	ch, err := s.loadChannelGlobal(ctx, "mock")
	if err != nil {
		return nil, err
	}
	cfg, err := decodeConfig(ch.ConfigJSON)
	if err != nil {
		return nil, err
	}
	ts := time.Now().Unix()
	body, err := json.Marshal(mockCallbackBody{
		PaymentNo: paymentNo, Result: outcome, ChannelTxnID: "mock_" + randomToken(8), Timestamp: ts,
	})
	if err != nil {
		return nil, err
	}
	headers := map[string]string{
		"x-timestamp": formatID(ts),
		"x-signature": SignMockCallback(cfg["secret"], body, ts),
	}
	return s.HandleCallback(ctx, CallbackInput{Channel: "mock", Body: body, Headers: headers})
}

// safeReturnURL 只允许 http/https 的回跳地址（收银台页面上的"返回商家"链接）。
func safeReturnURL(raw string) string {
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https" && u.Scheme != "") {
		return ""
	}
	return raw
}

var cashierTmpl = template.Must(template.New("cashier").Parse(`<!doctype html>
<html><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1">
<title>模拟收银台</title>
<style>
 body{font-family:system-ui,sans-serif;background:#f5f5f4;display:flex;justify-content:center;padding-top:8vh}
 .card{background:#fff;border-radius:12px;box-shadow:0 1px 4px rgba(0,0,0,.08);padding:32px;width:360px;text-align:center}
 .no{color:#888;font-size:13px;margin:4px 0 20px}
 .amt{font-size:32px;font-weight:700;margin-bottom:24px}
 button{width:100%;padding:12px;border:0;border-radius:8px;font-size:15px;cursor:pointer;margin-top:8px}
 .ok{background:#16a34a;color:#fff}.bad{background:#fff;color:#b91c1c;border:1px solid #fca5a5}
 .msg{margin-top:18px;font-size:14px;min-height:20px}
 a{display:inline-block;margin-top:12px;font-size:14px;color:#2563eb;text-decoration:none}
</style></head><body><div class="card">
 <div>模拟收银台（{{.Channel}}）</div>
 <div class="no">{{.PaymentNo}}</div>
 <div class="amt">{{.Amount}}</div>
 <button class="ok" onclick="pay('success')">确认支付</button>
 <button class="bad" onclick="pay('fail')">模拟支付失败</button>
 <div class="msg" id="msg"></div>
 <a id="back" href="{{.Return}}" style="{{.HideBack}}">返回商家</a>
</div>
<script>
async function pay(outcome){
  const r = await fetch('/api/v1/plugins/pay/mock/cashier/confirm',{
    method:'POST',headers:{'Content-Type':'application/json'},
    body:JSON.stringify({paymentNo:{{.PaymentNo}},outcome:outcome})});
  const j = await r.json();
  document.getElementById('msg').textContent = r.ok && (!j.code || j.code===0)
    ? '支付结果已提交：'+outcome : '提交失败：'+(j.reason||j.message||r.status);
}
</script></body></html>`))

// GET /plugins/pay/mock/cashier?no=PAY...&return=https://...
func (h *handler) mockCashier(c *gin.Context) {
	paymentNo := c.Query("no")
	if paymentNo == "" {
		response.Fail(c, response.BadRequest("error.bad_request"))
		return
	}
	// 收银台是公开页：跨租户定位订单后以其租户上下文读取。
	o, err := h.svc.queryByNoGlobal(c.Request.Context(), paymentNo)
	if err != nil {
		response.Fail(c, err)
		return
	}
	ret := safeReturnURL(c.Query("return"))
	hide := ""
	if ret == "" {
		hide = "display:none"
	}
	amount := fmtMoney(o.AmountMinor, o.Currency)
	c.Data(200, "text/html; charset=utf-8", []byte(renderCashier(paymentNo, amount, ret, hide)))
}

func renderCashier(paymentNo, amount, ret, hide string) string {
	var buf strings.Builder
	// html/template 会按上下文自动做 HTML/JS 转义（script 里的字符串按 JS 字符串转义）。
	_ = cashierTmpl.Execute(&buf, map[string]any{
		"Channel": "mock", "PaymentNo": paymentNo, "Amount": amount,
		"Return": template.URL(ret), "HideBack": hide,
	})
	return buf.String()
}

func fmtMoney(minor int64, currency string) string {
	return currency + " " + formatID(minor)
}

// POST /plugins/pay/mock/cashier/confirm  {"paymentNo":"...","outcome":"success|fail"}
func (h *handler) mockConfirm(c *gin.Context) {
	var f struct {
		PaymentNo string `json:"paymentNo"`
		Outcome   string `json:"outcome"`
	}
	if !response.BindJSON(c, &f) {
		return
	}
	o, err := h.svc.MockPay(c.Request.Context(), f.PaymentNo, f.Outcome)
	if err != nil {
		response.Fail(c, err)
		return
	}
	if o == nil {
		response.OK(c, gin.H{})
		return
	}
	response.OK(c, gin.H{"paymentNo": o.PaymentNo, "status": o.Status})
}
