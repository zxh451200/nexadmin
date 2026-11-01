package ticket

// 合并与关联单的 HTTP 入口。业务规则都在 merge.go（自包含、被并发泳道挡过门禁），
// 这里只做参数解析、操作人识别与响应封装。路由注册见 handler.go 的 Register。

import (
	"github.com/gin-gonic/gin"

	"nexadmin/core/response"
)

// POST /plugins/ticket/tickets/:no/merge  {"intoNo":"TK..."} —— 把本单合并进另一张。
// 被合并方进入 closed 并记下指针；回复与附件留在原单，不迁移。
func (h *handler) mergeTicket(c *gin.Context) {
	var f struct {
		IntoNo string `json:"intoNo" binding:"required"`
	}
	if !response.BindJSON(c, &f) {
		return
	}
	res, err := h.svc.Merge(c.Request.Context(), c.Param("no"), f.IntoNo, operatorName(c))
	respond(c, res, err)
}

// GET /plugins/ticket/tickets/:no/links —— 一次给全「双向关联 + 合并痕迹」两类视图。
// 前端只需要一个接口就能同时渲染关联面板与「已合并到 TK…」的提示。
func (h *handler) links(c *gin.Context) {
	no := c.Param("no")
	list, err := h.svc.ListLinks(c.Request.Context(), no)
	if err != nil {
		response.Fail(c, err)
		return
	}
	summary, err := h.svc.MergeSummary(c.Request.Context(), no)
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, gin.H{"links": list, "merge": summary})
}

// POST /plugins/ticket/tickets/:no/links  {"ticketNo":"TK..."} —— 建立双向关联。
func (h *handler) addLink(c *gin.Context) {
	var f struct {
		TicketNo string `json:"ticketNo" binding:"required"`
	}
	if !response.BindJSON(c, &f) {
		return
	}
	link, err := h.svc.Link(c.Request.Context(), c.Param("no"), f.TicketNo, operatorName(c))
	respond(c, link, err)
}

// DELETE /plugins/ticket/tickets/:no/links/:other —— 解除关联（合并不可撤销，见 merge.go 顶注）。
func (h *handler) removeLink(c *gin.Context) {
	respond(c, gin.H{"ok": true}, h.svc.Unlink(c.Request.Context(), c.Param("no"), c.Param("other"), operatorName(c)))
}

// operatorName 取登录坐席的显示名，用于动态轨迹的发起人；无登录态时交服务层兜底。
func operatorName(c *gin.Context) string {
	_, name := claimsOperator(c)
	return name
}
