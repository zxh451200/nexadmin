package pay

import (
	"errors"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"nexadmin/core/response"
	"nexadmin/pkg/idgen"
)

// 路由前缀：/api/v1/plugins/pay（core.Module.Register 收到的 r 已是 /api/v1），
// 契约 02-openapi-pay-v1.yaml 的 /pay/* 在插件形态下即 /plugins/pay/*。
// 权限码：pay:list 查看；pay:edit 关单/补查；pay:channel 渠道配置；
// pay:refund 手动退款；pay:auth 预授权；pay:settle 对账与结算。
type handler struct{ svc *Service }

func respond(c *gin.Context, data any, err error) {
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, data)
}

func idParam(c *gin.Context, name string) (int64, bool) {
	id, err := strconv.ParseInt(c.Param(name), 10, 64)
	if err != nil || id == 0 {
		response.Fail(c, response.BadRequest("error.bad_request"))
		return 0, false
	}
	return id, true
}

// ---- orders ----

// POST /plugins/pay/orders —— 契约 createPayOrder（支持 Idempotency-Key）。
func (h *handler) createOrder(c *gin.Context) {
	var f struct {
		BizType         string            `json:"bizType"`
		BizNo           string            `json:"bizNo"`
		SubjectType     string            `json:"subjectType"`
		SubjectID       string            `json:"subjectId"`
		Subject         string            `json:"subject"`
		AmountMinor     int64             `json:"amountMinor"`
		AmountOrigMinor int64             `json:"amountOrigMinor"`
		OrigCurrency    string            `json:"origCurrency"`
		FxRateMicro     int64             `json:"fxRateMicro"`
		Currency        string            `json:"currency"`
		ChannelCode     string            `json:"channelCode"`
		Method          string            `json:"method"`
		Scene           string            `json:"scene"`
		PayerID         string            `json:"payerId"`
		PayerOpenID     string            `json:"payerOpenId"`
		OperatorID      string            `json:"operatorId"`
		ReturnURL       string            `json:"returnUrl"`
		ExpireSec       int               `json:"expireSec"`
		Metadata        map[string]string `json:"metadata"`
	}
	if !response.BindJSON(c, &f) {
		return
	}
	o, params, err := h.svc.Create(c.Request.Context(), CreateInput{
		BizType: f.BizType, BizNo: f.BizNo, SubjectType: f.SubjectType, SubjectID: f.SubjectID,
		Subject: f.Subject, AmountMinor: f.AmountMinor, AmountOrigMinor: f.AmountOrigMinor,
		OrigCurrency: f.OrigCurrency, FxRateMicro: f.FxRateMicro, Currency: f.Currency,
		ChannelCode: f.ChannelCode, Method: f.Method, Scene: f.Scene,
		PayerID: f.PayerID, PayerOpenID: f.PayerOpenID, OperatorID: f.OperatorID,
		ReturnURL: f.ReturnURL, ExpireSec: f.ExpireSec, Metadata: f.Metadata,
	})
	if err != nil {
		response.Fail(c, err)
		return
	}
	respond(c, orderView(o, params), nil)
}

// GET /plugins/pay/orders —— 管理端分页列表（契约外附加，前端列表用）。
func (h *handler) listOrders(c *gin.Context) {
	page, size := response.ParsePage(c)
	rows, total, err := h.svc.ListOrders(c.Request.Context(), OrderQuery{
		Keyword: c.Query("keyword"), Status: c.Query("status"), Channel: c.Query("channelCode"),
		SubjectID: c.Query("subjectId"), Page: page, PageSize: size,
	})
	if err != nil {
		response.Fail(c, err)
		return
	}
	out := make([]OrderView, len(rows))
	for i := range rows {
		out[i] = orderView(&rows[i], nil)
	}
	response.Page(c, out, total, page, size)
}

// GET /plugins/pay/orders/by-biz —— 契约 getPayOrderByBiz（收款侧主查询入口）。
func (h *handler) orderByBiz(c *gin.Context) {
	o, err := h.svc.QueryByBiz(c.Request.Context(), c.Query("bizType"), c.Query("bizNo"))
	if err != nil {
		response.Fail(c, err)
		return
	}
	respond(c, orderView(o, nil), nil)
}

// GET /plugins/pay/orders/:no —— 契约 getPayOrder。
func (h *handler) getOrder(c *gin.Context) {
	o, err := h.svc.QueryByNo(c.Request.Context(), c.Param("no"))
	if err != nil {
		response.Fail(c, err)
		return
	}
	respond(c, orderView(o, nil), nil)
}

// POST /plugins/pay/orders/:no/close —— 契约 closePayOrder。
func (h *handler) closeOrder(c *gin.Context) {
	o, err := h.svc.Close(c.Request.Context(), c.Param("no"))
	respond(c, orderView(o, nil), err)
}

// POST /plugins/pay/orders/:no/sync —— 契约 syncPayOrder（pending 的补查兜底）。
func (h *handler) syncOrder(c *gin.Context) {
	o, err := h.svc.QueryByNo(c.Request.Context(), c.Param("no"))
	if err != nil {
		response.Fail(c, err)
		return
	}
	respond(c, orderView(o, nil), nil)
}

// ---- refunds ----

// POST /plugins/pay/refunds —— 契约 createRefund。
func (h *handler) createRefund(c *gin.Context) {
	var f struct {
		PaymentNo   string `json:"paymentNo"`
		BizType     string `json:"bizType"`
		BizNo       string `json:"bizNo"`
		AmountMinor int64  `json:"amountMinor"`
		Reason      string `json:"reason"`
		OperatorID  string `json:"operatorId"`
	}
	if !response.BindJSON(c, &f) {
		return
	}
	// 契约要求 bizType/bizNo 必填；管理端手动退款给一个稳定可辨识的缺省键。
	bizType, bizNo := strings.TrimSpace(f.BizType), strings.TrimSpace(f.BizNo)
	if bizType == "" {
		bizType = "manual"
	}
	if bizNo == "" {
		bizNo = "manual-" + randomToken(6)
	}
	r, err := h.svc.CreateRefund(c.Request.Context(), RefundInput{
		PaymentNo: f.PaymentNo, BizType: bizType, BizNo: bizNo,
		AmountMinor: f.AmountMinor, Reason: f.Reason, OperatorID: f.OperatorID,
	})
	if r != nil {
		respond(c, refundView(r), err)
		return
	}
	response.Fail(c, err)
}

// GET /plugins/pay/refunds —— 管理端分页列表（契约外附加）。
func (h *handler) listRefunds(c *gin.Context) {
	page, size := response.ParsePage(c)
	rows, total, err := h.svc.ListRefunds(c.Request.Context(), RefundQuery{
		Status: c.Query("status"), Keyword: c.Query("keyword"), Page: page, PageSize: size,
	})
	if err != nil {
		response.Fail(c, err)
		return
	}
	out := make([]RefundView, len(rows))
	for i := range rows {
		out[i] = refundView(&rows[i])
	}
	response.Page(c, out, total, page, size)
}

// GET /plugins/pay/refunds/:no —— 契约 getRefund。
func (h *handler) getRefund(c *gin.Context) {
	var r Refund
	err := h.svc.db.WithContext(c.Request.Context()).Where("refund_no = ?", c.Param("no")).Take(&r).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			response.Fail(c, response.NotFound(errNotFound))
			return
		}
		response.Fail(c, err)
		return
	}
	respond(c, refundView(&r), nil)
}

// ---- authorizations ----

// POST /plugins/pay/authorizations —— 契约 createAuthorization。
func (h *handler) createAuth(c *gin.Context) {
	var f struct {
		BizType     string `json:"bizType"`
		BizNo       string `json:"bizNo"`
		SubjectID   string `json:"subjectId"`
		ChannelCode string `json:"channelCode"`
		Method      string `json:"method"`
		AmountMinor int64  `json:"amountMinor"`
		Currency    string `json:"currency"`
		CardLast4   string `json:"cardLast4"`
		ValidDays   int    `json:"validDays"`
		Remark      string `json:"remark"`
	}
	if !response.BindJSON(c, &f) {
		return
	}
	a, err := h.svc.CreateAuth(c.Request.Context(), CreateAuthInput{
		BizType: f.BizType, BizNo: f.BizNo, SubjectID: f.SubjectID, ChannelCode: f.ChannelCode,
		Method: f.Method, AmountMinor: f.AmountMinor, Currency: f.Currency,
		CardLast4: f.CardLast4, ValidDays: f.ValidDays, Remark: f.Remark,
	})
	if a != nil {
		respond(c, authView(a), err)
		return
	}
	response.Fail(c, err)
}

// POST /plugins/pay/authorizations/:no/capture —— 契约 captureAuthorization。
func (h *handler) captureAuth(c *gin.Context) {
	var f struct {
		AmountMinor int64  `json:"amountMinor"`
		BizType     string `json:"bizType"`
		BizNo       string `json:"bizNo"`
		OperatorID  string `json:"operatorId"`
	}
	if !response.BindJSON(c, &f) {
		return
	}
	a, err := h.svc.CaptureAuth(c.Request.Context(), c.Param("no"), CaptureAuthInput{
		AmountMinor: f.AmountMinor, BizType: f.BizType, BizNo: f.BizNo, OperatorID: f.OperatorID,
	})
	respond(c, authView(a), err)
}

// POST /plugins/pay/authorizations/:no/void —— 契约 voidAuthorization。
func (h *handler) voidAuth(c *gin.Context) {
	a, err := h.svc.VoidAuth(c.Request.Context(), c.Param("no"))
	respond(c, authView(a), err)
}

// GET /plugins/pay/authorizations/:no —— 管理端查询（契约外附加）。
func (h *handler) getAuth(c *gin.Context) {
	a, err := h.svc.QueryAuth(c.Request.Context(), c.Param("no"))
	respond(c, authView(a), err)
}

// GET /plugins/pay/authorizations —— 管理端分页列表（契约外附加）。
func (h *handler) listAuths(c *gin.Context) {
	page, size := response.ParsePage(c)
	rows, total, err := h.svc.ListAuths(c.Request.Context(), c.Query("status"), c.Query("keyword"), page, size)
	if err != nil {
		response.Fail(c, err)
		return
	}
	out := make([]AuthView, len(rows))
	for i := range rows {
		out[i] = authView(&rows[i])
	}
	response.Page(c, out, total, page, size)
}

// ---- channels ----

// GET /plugins/pay/channels —— 契约 listChannels。
func (h *handler) listChannels(c *gin.Context) {
	rows, err := h.svc.ListChannels(c.Request.Context(), c.Query("subjectId"), c.Query("enabled") == "true")
	if err != nil {
		response.Fail(c, err)
		return
	}
	out := make([]*ChannelView, len(rows))
	for i := range rows {
		out[i] = channelToView(&rows[i])
	}
	response.OK(c, out)
}

// POST /plugins/pay/channels —— 契约 upsertChannel（按 code 新建或更新；凭据不回显）。
func (h *handler) upsertChannel(c *gin.Context) {
	var f ChannelForm
	if !response.BindJSON(c, &f) {
		return
	}
	ch, err := h.svc.UpsertChannel(c.Request.Context(), SaveChannelInput{
		Code: f.Code, Name: f.Name, Provider: f.Provider, Method: f.Method, Scene: f.Scene,
		SubjectID: f.SubjectID, FeeRateBp: f.FeeRateBp, NotifyURL: f.NotifyURL,
		Enabled: f.Enabled, Sort: f.Sort, Config: f.Config,
	})
	respond(c, channelToView(ch), err)
}

// DELETE /plugins/pay/channels/:id —— 管理端删除（契约外附加）。
func (h *handler) deleteChannel(c *gin.Context) {
	id, ok := idParam(c, "id")
	if !ok {
		return
	}
	respond(c, nil, h.svc.DeleteChannel(c.Request.Context(), idgen.ID(id)))
}

// GET /plugins/pay/providers —— 已注册渠道类型（前端建渠道下拉用，契约外附加）。
func (h *handler) providers(c *gin.Context) {
	response.OK(c, ProviderNames())
}

// ---- callbacks ----

// GET /plugins/pay/callbacks —— 契约 listCallbacks（回调审计）。
func (h *handler) listCallbacks(c *gin.Context) {
	page, size := response.ParsePage(c)
	q := CallbackQuery{
		Channel: c.Query("channelCode"), Keyword: c.Query("paymentNo"), Page: page, PageSize: size,
	}
	if v, err := strconv.ParseBool(c.Query("ok")); err == nil {
		q.OK = &v
	}
	rows, total, err := h.svc.ListCallbacks(c.Request.Context(), q)
	if err != nil {
		response.Fail(c, err)
		return
	}
	out := make([]CallbackView, len(rows))
	for i := range rows {
		out[i] = callbackView(&rows[i])
	}
	response.Page(c, out, total, page, size)
}

// ---- reconcile ----

// GET /plugins/pay/reconcile/tasks —— 契约 listReconcileTasks。
func (h *handler) listReconcileTasks(c *gin.Context) {
	rows, err := h.svc.ListReconcileTasks(c.Request.Context(), c.Query("billDate"), c.Query("channelCode"), c.Query("status"))
	respond(c, rows, err)
}

// POST /plugins/pay/reconcile/tasks —— 契约 runReconcileTask（Phase 0 渠道金额入参提供）。
func (h *handler) runReconcileTask(c *gin.Context) {
	var f struct {
		ChannelCode    string `json:"channelCode"`
		BillDate       string `json:"billDate"`
		SubjectID      string `json:"subjectId"`
		ChannelAmounts []struct {
			PaymentNo   string `json:"paymentNo"`
			AmountMinor int64  `json:"amountMinor"`
		} `json:"channelAmounts"`
	}
	if !response.BindJSON(c, &f) {
		return
	}
	entries := make([]ChannelAmountEntry, len(f.ChannelAmounts))
	for i, e := range f.ChannelAmounts {
		entries[i] = ChannelAmountEntry{PaymentNo: e.PaymentNo, AmountMinor: e.AmountMinor}
	}
	task, err := h.svc.RunReconcileTask(c.Request.Context(), f.ChannelCode, f.BillDate, f.SubjectID, entries)
	respond(c, task, err)
}

// GET /plugins/pay/reconcile/tasks/:id/diffs —— 任务差异明细（契约外附加）。
func (h *handler) listReconcileDiffs(c *gin.Context) {
	id, ok := idParam(c, "id")
	if !ok {
		return
	}
	rows, err := h.svc.ListReconcileDiffs(c.Request.Context(), id)
	respond(c, rows, err)
}

// POST /plugins/pay/reconcile/diffs/:id/resolve —— 契约 resolveReconcileDiff。
func (h *handler) resolveReconcileDiff(c *gin.Context) {
	id, ok := idParam(c, "id")
	if !ok {
		return
	}
	var f struct {
		Action string `json:"action"`
		Note   string `json:"note"`
	}
	if !response.BindJSON(c, &f) {
		return
	}
	diff, err := h.svc.ResolveDiff(c.Request.Context(), id, ResolveDiffInput{Action: f.Action, Note: f.Note, By: operatorOf(c)})
	respond(c, diff, err)
}

// ---- settlement ----

// GET /plugins/pay/commission-rules —— 契约 listCommissionRules。
func (h *handler) listCommissionRules(c *gin.Context) {
	rows, err := h.svc.ListCommissionRules(c.Request.Context())
	respond(c, rows, err)
}

// POST /plugins/pay/commission-rules —— 契约 upsertCommissionRule。
func (h *handler) upsertCommissionRule(c *gin.Context) {
	var f CommissionRule
	if !response.BindJSON(c, &f) {
		return
	}
	rule, err := h.svc.UpsertCommissionRule(c.Request.Context(), f)
	respond(c, rule, err)
}

// GET /plugins/pay/settlements —— 契约 listSettlements。
func (h *handler) listSettlements(c *gin.Context) {
	rows, err := h.svc.ListSettlements(c.Request.Context(), c.Query("subjectId"), c.Query("status"))
	respond(c, rows, err)
}

// POST /plugins/pay/settlements/generate —— 账期结算生成（契约外附加；list 的数据来源）。
func (h *handler) generateSettlement(c *gin.Context) {
	var f struct {
		SubjectID   string `json:"subjectId"`
		PeriodStart string `json:"periodStart"`
		PeriodEnd   string `json:"periodEnd"`
	}
	if !response.BindJSON(c, &f) {
		return
	}
	rows, err := h.svc.GenerateSettlement(c.Request.Context(), f.SubjectID, f.PeriodStart, f.PeriodEnd)
	respond(c, rows, err)
}

// POST /plugins/pay/settlements/:id/confirm —— 结算确认（契约外附加；打款前置）。
func (h *handler) confirmSettlement(c *gin.Context) {
	id, ok := idParam(c, "id")
	if !ok {
		return
	}
	st, err := h.svc.ConfirmSettlement(c.Request.Context(), id)
	respond(c, st, err)
}

// POST /plugins/pay/payouts —— 契约 createPayoutBatch。
func (h *handler) createPayout(c *gin.Context) {
	var f struct {
		SettlementIDs []int64 `json:"settlementIds"`
		Method        string  `json:"method"`
		Note          string  `json:"note"`
	}
	if !response.BindJSON(c, &f) {
		return
	}
	batch, err := h.svc.CreatePayout(c.Request.Context(), CreatePayoutInput{
		SettlementIDs: f.SettlementIDs, Method: f.Method, Note: f.Note,
	})
	respond(c, batch, err)
}

// GET /plugins/pay/payouts —— 批次列表（契约外附加）。
func (h *handler) listPayouts(c *gin.Context) {
	rows, err := h.svc.ListPayouts(c.Request.Context(), c.Query("status"))
	respond(c, rows, err)
}

// POST /plugins/pay/payouts/:id/mark-paid —— 人工回执（契约外附加）。
func (h *handler) markPayoutPaid(c *gin.Context) {
	id, ok := idParam(c, "id")
	if !ok {
		return
	}
	var f struct {
		ReceiptNo string `json:"receiptNo"`
	}
	if !response.BindJSON(c, &f) {
		return
	}
	batch, err := h.svc.MarkPayoutPaid(c.Request.Context(), id, f.ReceiptNo)
	respond(c, batch, err)
}

// ---- public ----

// POST /plugins/pay/notify/:channelCode —— 契约 handleChannelNotify。
// 匿名、必验签、成败都落审计、重复回调不重复发事件。返回 200 JSON 即视为接收；
// 各渠道对 ack 报文的要求由该渠道的 Provider 实现负责适配。
func (h *handler) notify(c *gin.Context) {
	body, err := c.GetRawData()
	if err != nil {
		response.Fail(c, response.BadRequest("error.bad_request"))
		return
	}
	headers := map[string]string{}
	for k := range c.Request.Header {
		headers[strings.ToLower(k)] = c.Request.Header.Get(k)
	}
	o, err := h.svc.HandleCallback(c.Request.Context(), CallbackInput{
		Channel: c.Param("channelCode"), Body: body, Headers: headers,
	})
	if err != nil {
		response.Fail(c, err)
		return
	}
	if o == nil {
		response.OK(c, gin.H{"result": "refund_receipt"})
		return
	}
	response.OK(c, gin.H{"paymentNo": o.PaymentNo, "status": o.Status})
}

// operatorOf 取当前登录者标识（审计字段；匿名上下文为空）。
func operatorOf(c *gin.Context) string {
	if v := c.GetString("operatorId"); v != "" {
		return v
	}
	return c.GetString("username")
}
