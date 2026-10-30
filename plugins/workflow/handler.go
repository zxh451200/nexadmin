package workflow

import (
	"strconv"

	"github.com/gin-gonic/gin"

	"nexadmin/core"
	"nexadmin/core/auth"
	"nexadmin/core/response"
	"nexadmin/pkg/idgen"
)

// 路由前缀：/api/v1/plugins/workflow。权限码：workflow:list 查看/待办；workflow:start 发起/撤回；
// workflow:approve 审批动作；workflow:edit 流程定义管理。租户级数据，租户隔离自动生效。

type handler struct{ svc *Service }

func respond(c *gin.Context, data any, err error) {
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, data)
}

// actor 从登录态取当前用户 ID 串（审批人/提交人标识）。
func actor(c *gin.Context) string {
	if cl := auth.ClaimsFrom(c); cl != nil {
		return strconv.FormatInt(int64(cl.UserID), 10)
	}
	return ""
}

// ---- 流程定义 ----

// GET /plugins/workflow/defs?keyword=&status=&page=&pageSize=
func (h *handler) defList(c *gin.Context) {
	page, size := response.ParsePage(c)
	list, total, err := h.svc.DefList(c.Request.Context(), DefQuery{
		Keyword: c.Query("keyword"), Status: c.Query("status"), Page: page, PageSize: size,
	})
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.Page(c, list, total, page, size)
}

// GET /plugins/workflow/defs/:id
func (h *handler) defGet(c *gin.Context) {
	id, ok := paramID(c)
	if !ok {
		return
	}
	v, err := h.svc.DefGet(c.Request.Context(), id)
	respond(c, v, err)
}

// POST /plugins/workflow/defs
func (h *handler) defCreate(c *gin.Context) {
	var f DefForm
	if !response.BindJSON(c, &f) {
		return
	}
	v, err := h.svc.DefSave(c.Request.Context(), 0, f)
	respond(c, v, err)
}

// PUT /plugins/workflow/defs/:id
func (h *handler) defUpdate(c *gin.Context) {
	id, ok := paramID(c)
	if !ok {
		return
	}
	var f DefForm
	if !response.BindJSON(c, &f) {
		return
	}
	v, err := h.svc.DefSave(c.Request.Context(), id, f)
	respond(c, v, err)
}

// DELETE /plugins/workflow/defs/:id
func (h *handler) defDelete(c *gin.Context) {
	id, ok := paramID(c)
	if !ok {
		return
	}
	respond(c, nil, h.svc.DefDelete(c.Request.Context(), id))
}

// GET /plugins/workflow/defs/:id/xml —— BPMN 2.0 XML 导出。
func (h *handler) defXML(c *gin.Context) {
	id, ok := paramID(c)
	if !ok {
		return
	}
	v, err := h.svc.DefGet(c.Request.Context(), id)
	if err != nil {
		response.Fail(c, err)
		return
	}
	xml, err := ExportXML(v.Definition)
	if err != nil {
		response.Fail(c, response.BadRequest("workflow.def_invalid").Wrap(err))
		return
	}
	c.Data(200, "application/xml; charset=utf-8", []byte(xml))
}

// GET /plugins/workflow/templates —— 内置模板（只读）。
func (h *handler) templates(c *gin.Context) {
	response.OK(c, Templates())
}

// ---- 流程实例 ----

// POST /plugins/workflow/instances —— 发起。
func (h *handler) instStart(c *gin.Context) {
	var f struct {
		DefID       string            `json:"defId"`
		DefKey      string            `json:"defKey"`
		Title       string            `json:"title"`
		BizType     string            `json:"bizType"`
		BusinessKey string            `json:"businessKey"`
		Vars        map[string]string `json:"vars"`
	}
	if !response.BindJSON(c, &f) {
		return
	}
	v, err := h.svc.InstStart(c.Request.Context(), StartInput{
		DefID: f.DefID, DefKey: f.DefKey, Title: f.Title, BizType: f.BizType,
		BusinessKey: f.BusinessKey, Vars: f.Vars,
	}, actor(c))
	respond(c, v, err)
}

// GET /plugins/workflow/instances?keyword=&status=&bizType=&todo=1&done=1&page=&pageSize=
func (h *handler) instList(c *gin.Context) {
	page, size := response.ParsePage(c)
	list, total, err := h.svc.InstList(c.Request.Context(), InstQuery{
		Keyword: c.Query("keyword"), Status: c.Query("status"), BizType: c.Query("bizType"),
		Todo: c.Query("todo"), Done: c.Query("done"), Page: page, PageSize: size,
	}, actor(c))
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.Page(c, list, total, page, size)
}

// GET /plugins/workflow/instances/:id
func (h *handler) instGet(c *gin.Context) {
	id, ok := paramID(c)
	if !ok {
		return
	}
	v, err := h.svc.InstGet(c.Request.Context(), id)
	respond(c, v, err)
}

// POST /plugins/workflow/instances/:id/approve  {"comment":"..."}
func (h *handler) instApprove(c *gin.Context) {
	var f struct {
		Comment string `json:"comment"`
	}
	if !response.BindJSON(c, &f) {
		return
	}
	v, err := h.svc.InstApprove(c.Request.Context(), paramIDv(c), actor(c), f.Comment)
	respond(c, v, err)
}

// POST /plugins/workflow/instances/:id/reject  {"comment":"..."}
func (h *handler) instReject(c *gin.Context) {
	var f struct {
		Comment string `json:"comment"`
	}
	if !response.BindJSON(c, &f) {
		return
	}
	v, err := h.svc.InstReject(c.Request.Context(), paramIDv(c), actor(c), f.Comment)
	respond(c, v, err)
}

// POST /plugins/workflow/instances/:id/countersign  {"approver":"用户ID","comment":"..."}
func (h *handler) instCountersign(c *gin.Context) {
	var f struct {
		Approver string `json:"approver" binding:"required"`
		Comment  string `json:"comment"`
	}
	if !response.BindJSON(c, &f) {
		return
	}
	v, err := h.svc.InstCountersign(c.Request.Context(), paramIDv(c), actor(c), f.Approver, f.Comment)
	respond(c, v, err)
}

// POST /plugins/workflow/instances/:id/rollback  {"toNode":"节点key","comment":"..."}
func (h *handler) instRollback(c *gin.Context) {
	var f struct {
		ToNode  string `json:"toNode" binding:"required"`
		Comment string `json:"comment"`
	}
	if !response.BindJSON(c, &f) {
		return
	}
	v, err := h.svc.InstRollback(c.Request.Context(), paramIDv(c), actor(c), f.ToNode, f.Comment)
	respond(c, v, err)
}

// POST /plugins/workflow/instances/:id/escalate  {"comment":"..."} —— 手动超时升级。
func (h *handler) instEscalate(c *gin.Context) {
	var f struct {
		Comment string `json:"comment"`
	}
	if !response.BindJSON(c, &f) {
		return
	}
	v, err := h.svc.InstEscalate(c.Request.Context(), paramIDv(c), actor(c), f.Comment)
	respond(c, v, err)
}

// POST /plugins/workflow/instances/:id/withdraw  {"comment":"..."} —— 提交人撤回。
func (h *handler) instWithdraw(c *gin.Context) {
	var f struct {
		Comment string `json:"comment"`
	}
	if !response.BindJSON(c, &f) {
		return
	}
	v, err := h.svc.InstWithdraw(c.Request.Context(), paramIDv(c), actor(c), f.Comment)
	respond(c, v, err)
}

func paramID(c *gin.Context) (idgen.ID, bool) {
	id, err := idgen.Parse(c.Param("id"))
	if err != nil || id == 0 {
		response.Fail(c, response.BadRequest("error.bad_request"))
		return 0, false
	}
	return id, true
}

func paramIDv(c *gin.Context) idgen.ID {
	id, _ := paramID(c)
	return id
}

// Register 注册路由（由 plugin.go 的 module 调用）。
func (h *handler) Register(r *gin.RouterGroup, d *core.Deps) {
	g := r.Group("/plugins/workflow")
	g.GET("/defs", core.Perm("workflow:list"), h.defList)
	g.GET("/defs/:id", core.Perm("workflow:list"), h.defGet)
	g.POST("/defs", core.Perm("workflow:edit"), h.defCreate)
	g.PUT("/defs/:id", core.Perm("workflow:edit"), h.defUpdate)
	g.DELETE("/defs/:id", core.Perm("workflow:edit"), h.defDelete)
	g.GET("/defs/:id/xml", core.Perm("workflow:list"), h.defXML)
	g.GET("/templates", core.Perm("workflow:list"), h.templates)
	g.POST("/instances", core.Perm("workflow:start"), h.instStart)
	g.GET("/instances", core.Perm("workflow:list"), h.instList)
	g.GET("/instances/:id", core.Perm("workflow:list"), h.instGet)
	g.POST("/instances/:id/approve", core.Perm("workflow:approve"), h.instApprove)
	g.POST("/instances/:id/reject", core.Perm("workflow:approve"), h.instReject)
	g.POST("/instances/:id/countersign", core.Perm("workflow:approve"), h.instCountersign)
	g.POST("/instances/:id/rollback", core.Perm("workflow:approve"), h.instRollback)
	g.POST("/instances/:id/escalate", core.Perm("workflow:approve"), h.instEscalate)
	g.POST("/instances/:id/withdraw", core.Perm("workflow:start"), h.instWithdraw)
}
