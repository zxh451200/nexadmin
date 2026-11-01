package ticket

// 自定义字段与多套 SLA 策略的 HTTP 入口。权限口径与其他配置类资源一致：
// 读要 ticket:list，写要 ticket:edit 并落操作日志（分组/规则/快捷回复同规则）。

import (
	"github.com/gin-gonic/gin"

	"nexadmin/core/response"
)

// GET /plugins/ticket/fields —— 字段定义列表（含停用项，管理页要能看见并重启）。
func (h *handler) listFields(c *gin.Context) {
	rows, err := h.svc.ListFields(c.Request.Context())
	respond(c, rows, err)
}

// POST /plugins/ticket/fields {"code","label","type","options","required","enabled","sort","groupId"}
func (h *handler) createField(c *gin.Context) {
	var f Field
	if !response.BindJSON(c, &f) {
		return
	}
	// 新建不接受带 id：带 id 的写入走 PUT /fields/:id，混用会让「改哪一条」变得含糊。
	if f.ID != 0 {
		response.Fail(c, response.BadRequest("error.bad_request"))
		return
	}
	out, err := h.svc.SaveField(c.Request.Context(), f)
	respond(c, out, err)
}

// PUT /plugins/ticket/fields/:id
func (h *handler) updateField(c *gin.Context) {
	id, ok := pathID(c)
	if !ok {
		return
	}
	var f Field
	if !response.BindJSON(c, &f) {
		return
	}
	f.ID = id
	out, err := h.svc.SaveField(c.Request.Context(), f)
	respond(c, out, err)
}

// DELETE /plugins/ticket/fields/:id —— 删定义；存量工单里的该 code 不清洗，只是不再显示。
func (h *handler) deleteField(c *gin.Context) {
	id, ok := pathID(c)
	if !ok {
		return
	}
	respond(c, gin.H{"id": id}, h.svc.DeleteField(c.Request.Context(), int64(id)))
}

// CustomValuesInput 是编辑一张工单附加值的入参。
type CustomValuesInput struct {
	// Values 里显式给 null 表示删除该字段；未定义或已停用的 code 会被拒绝。
	Values map[string]any `json:"values"`
}

// POST /plugins/ticket/tickets/:no/custom-values {"values":{"order_channel":"app"}}
func (h *handler) setCustomValues(c *gin.Context) {
	var f CustomValuesInput
	if !response.BindJSON(c, &f) {
		return
	}
	t, err := h.svc.SetCustomValues(c.Request.Context(), c.Param("no"), f.Values)
	respond(c, t, err)
}

// GET /plugins/ticket/sla/policies —— 全部 SLA 策略；默认策略的 code 是空串，也在列表里。
func (h *handler) listSLAPolicies(c *gin.Context) {
	rows, err := h.svc.ListSLAPolicies(c.Request.Context())
	respond(c, rows, err)
}

// POST /plugins/ticket/sla/policy —— 按 code upsert 一套策略；code 为空即保存默认策略（同时刷新缓存）。
func (h *handler) saveSLAPolicy(c *gin.Context) {
	var v SLASetting
	if !response.BindJSON(c, &v) {
		return
	}
	out, err := h.svc.SaveSLAPolicy(c.Request.Context(), v)
	respond(c, out, err)
}

// DELETE /plugins/ticket/sla/policy/:code —— 删一套策略；默认策略不可删（服务层拒绝）。
func (h *handler) deleteSLAPolicy(c *gin.Context) {
	code := c.Param("code")
	respond(c, gin.H{"code": code}, h.svc.DeleteSLAPolicy(c.Request.Context(), code))
}
