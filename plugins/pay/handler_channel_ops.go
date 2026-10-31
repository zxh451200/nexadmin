package pay

// 渠道能力矩阵与资金事件出站箱的管理端接口。这两块是「运维面」：
// 能力矩阵决定前端把哪些币种/支付方式/动作按钮禁用，出站箱是资金事件的投递台账。
// 路由在 plugin.go 里注册（权限：查看 pay:list，处置 pay:outbox）。

import (
	"sort"

	"github.com/gin-gonic/gin"

	"nexadmin/core/response"
)

// GET /plugins/pay/capabilities —— 每个已注册渠道类型的真实能力。
// 渠道实现没声明 CapabilityProvider 时按零值返回（只 CNY、四个能力全 false），
// 这样新渠道不会凭空获得能力，前端也不会给出点了就失败的按钮。
func (h *handler) capabilities(c *gin.Context) {
	names := ProviderNames()
	sort.Strings(names)
	out := make([]ProviderCapabilityView, 0, len(names))
	for _, n := range names {
		caps, err := capsOf(n)
		if err != nil {
			response.Fail(c, err)
			return
		}
		if caps.Currencies == nil {
			caps.Currencies = []string{"CNY"}
		}
		if caps.Methods == nil {
			caps.Methods = []string{}
		}
		out = append(out, ProviderCapabilityView{Provider: n, Capabilities: caps})
	}
	response.OK(c, out)
}

// GET /plugins/pay/outbox —— 出站箱分页列表（载荷只在详情接口回显）。
func (h *handler) listOutbox(c *gin.Context) {
	page, size := response.ParsePage(c)
	rows, total, err := h.svc.ListOutbox(c.Request.Context(), c.Query("status"), c.Query("keyword"), page, size)
	if err != nil {
		response.Fail(c, err)
		return
	}
	out := make([]OutboxView, len(rows))
	for i := range rows {
		out[i] = outboxView(&rows[i], false)
	}
	response.Page(c, out, total, page, size)
}

// GET /plugins/pay/outbox/counts —— 各状态行数；死信数用于页签上的告警徽标。
// 静态段注册在 /outbox/:eventId 之前（与 /orders/stats 同一套匹配约定）。
func (h *handler) outboxCounts(c *gin.Context) {
	counts, err := h.svc.CountOutbox(c.Request.Context())
	respond(c, counts, err)
}

// GET /plugins/pay/outbox/:eventId —— 单条详情，带载荷。
func (h *handler) getOutbox(c *gin.Context) {
	row, err := h.svc.GetOutbox(c.Request.Context(), c.Param("eventId"))
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, outboxView(row, true))
}

// POST /plugins/pay/outbox/:eventId/retry —— 人工重投一条死信。
// 只从 dead 转回 pending：对已投递或已丢弃的行按 409 拒绝，避免把「重放资金事件」
// 当成随手可用的按钮。
func (h *handler) retryOutbox(c *gin.Context) {
	row, err := h.svc.RetryOutbox(c.Request.Context(), c.Param("eventId"))
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, outboxView(row, false))
}

// POST /plugins/pay/outbox/:eventId/discard —— 人工确认丢弃一条死信（留原因）。
func (h *handler) discardOutbox(c *gin.Context) {
	var f struct {
		Note string `json:"note"`
	}
	if !response.BindJSON(c, &f) {
		return
	}
	row, err := h.svc.DiscardOutbox(c.Request.Context(), c.Param("eventId"), f.Note)
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, outboxView(row, false))
}
