package payment

import (
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	"nexadmin/core/response"
	"nexadmin/pkg/idgen"
)

// 路由前缀：/api/v1/plugins/payment（core.Module.Register 收到的 r 已是 /api/v1）。
// 权限码：payment:list 查看；payment:edit 关闭支付单；payment:channel 渠道配置；payment:refund 手动退款。

type handler struct{ svc *Service }

func respond(c *gin.Context, data any, err error) {
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, data)
}

// GET /plugins/payment/orders?keyword=&status=&channel=&page=&pageSize=
func (h *handler) listOrders(c *gin.Context) {
	page, size := response.ParsePage(c)
	list, total, err := h.svc.ListOrders(c.Request.Context(), OrderQuery{
		Keyword: c.Query("keyword"), Status: c.Query("status"), Channel: c.Query("channel"),
		Page: page, PageSize: size,
	})
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.Page(c, list, total, page, size)
}

// GET /plugins/payment/orders/:no
func (h *handler) getOrder(c *gin.Context) {
	o, err := h.svc.QueryByNo(c.Request.Context(), c.Param("no"))
	if err != nil {
		response.Fail(c, err)
		return
	}
	respond(c, orderView(o, ""), nil)
}

// POST /plugins/payment/orders/:no/close
func (h *handler) closeOrder(c *gin.Context) {
	o, err := h.svc.Close(c.Request.Context(), c.Param("no"))
	respond(c, orderView(o, ""), err)
}

// GET /plugins/payment/channels
func (h *handler) listChannels(c *gin.Context) {
	rows, err := h.svc.ListChannels(c.Request.Context())
	if err != nil {
		response.Fail(c, err)
		return
	}
	out := make([]ChannelView, len(rows))
	for i := range rows {
		out[i] = ChannelView{
			ID: rows[i].ID, Code: rows[i].Code, Name: rows[i].Name, Enabled: rows[i].Enabled,
			Sort: rows[i].Sort, HasConfig: rows[i].ConfigJSON != "", UpdatedAt: rows[i].UpdatedAt,
		}
	}
	response.OK(c, out)
}

// POST /plugins/payment/channels
func (h *handler) createChannel(c *gin.Context) {
	var f ChannelForm
	if !response.BindJSON(c, &f) {
		return
	}
	ch, err := h.svc.SaveChannel(c.Request.Context(), 0, f.Code, f.Name, f.Enabled, f.Sort, f.Config)
	respond(c, channelToView(ch), err)
}

// PUT /plugins/payment/channels/:id
func (h *handler) updateChannel(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id == 0 {
		response.Fail(c, response.BadRequest("error.bad_request"))
		return
	}
	var f ChannelForm
	if !response.BindJSON(c, &f) {
		return
	}
	ch, err := h.svc.SaveChannel(c.Request.Context(), idgen.ID(id), f.Code, f.Name, f.Enabled, f.Sort, f.Config)
	respond(c, channelToView(ch), err)
}

// DELETE /plugins/payment/channels/:id
func (h *handler) deleteChannel(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id == 0 {
		response.Fail(c, response.BadRequest("error.bad_request"))
		return
	}
	respond(c, nil, h.svc.DeleteChannel(c.Request.Context(), idgen.ID(id)))
}

func channelToView(ch *Channel) *ChannelView {
	if ch == nil {
		return nil
	}
	return &ChannelView{
		ID: ch.ID, Code: ch.Code, Name: ch.Name, Enabled: ch.Enabled,
		Sort: ch.Sort, HasConfig: ch.ConfigJSON != "", UpdatedAt: ch.UpdatedAt,
	}
}

// GET /plugins/payment/providers —— 已注册渠道类型（前端建渠道下拉用）。
func (h *handler) providers(c *gin.Context) {
	response.OK(c, ProviderNames())
}

// GET /plugins/payment/callbacks?channel=&keyword=&page=&pageSize=
func (h *handler) listCallbacks(c *gin.Context) {
	page, size := response.ParsePage(c)
	list, total, err := h.svc.ListCallbacks(c.Request.Context(), CallbackQuery{
		Channel: c.Query("channel"), Keyword: c.Query("keyword"), Page: page, PageSize: size,
	})
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.Page(c, list, total, page, size)
}

// GET /plugins/payment/refunds?status=&keyword=&page=&pageSize=
func (h *handler) listRefunds(c *gin.Context) {
	page, size := response.ParsePage(c)
	list, total, err := h.svc.ListRefunds(c.Request.Context(), RefundQuery{
		Status: c.Query("status"), Keyword: c.Query("keyword"), Page: page, PageSize: size,
	})
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.Page(c, list, total, page, size)
}

// POST /plugins/payment/refunds  {"paymentNo":"...","amountMinor":100,"reason":"..."} —— 平台侧手动退款。
func (h *handler) createRefund(c *gin.Context) {
	var f struct {
		PaymentNo   string `json:"paymentNo"`
		AmountMinor int64  `json:"amountMinor"`
		Reason      string `json:"reason"`
	}
	if !response.BindJSON(c, &f) {
		return
	}
	bizNo := "manual-" + randomToken(6)
	r, err := h.svc.CreateRefund(c.Request.Context(), RefundInput{
		PaymentNo: strings.TrimSpace(f.PaymentNo), BizType: "manual", BizNo: bizNo,
		AmountMinor: f.AmountMinor, Reason: f.Reason, Operator: "manual",
	})
	if r != nil {
		respond(c, refundView(r), err)
		return
	}
	response.Fail(c, err)
}

// POST /plugins/payment/notify/:channel —— 渠道网关异步回调入口（无登录，验签兜底）。
// 返回 200 JSON 即视为接收；具体渠道对 ack 报文的要求由该渠道的 Provider 实现负责适配。
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
		Channel: c.Param("channel"), Body: body, Headers: headers,
	})
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, gin.H{"paymentNo": o.PaymentNo, "status": o.Status})
}
