package ticket

import (
	"strconv"
	"time"

	"github.com/gin-gonic/gin"

	"nexadmin/core"
	"nexadmin/core/auth"
	"nexadmin/core/middleware"
	"nexadmin/core/response"
	"nexadmin/pkg/idgen"
)

// 路由前缀：/api/v1/plugins/ticket。权限码：ticket:list 查看；ticket:reply 回复/认领；ticket:edit 状态与改派。
// 访客公开接口挂在 d.Public（无登录），按 IP 限流；工单号是不可枚举的雪花 ID，充当能力令牌。

type handler struct{ svc *Service }

func respond(c *gin.Context, data any, err error) {
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, data)
}

// ---- 坐席端 ----

// GET /plugins/ticket/tickets?status=&priority=&bizType=&keyword=&overdue=1&assigneeId=&unassigned=1&mine=1&page=&pageSize=
func (h *handler) list(c *gin.Context) {
	page, size := response.ParsePage(c)
	uid, _ := claimsOperator(c)
	q := TicketQuery{
		Status: c.Query("status"), Priority: c.Query("priority"), BizType: c.Query("bizType"),
		Keyword: c.Query("keyword"), Overdue: c.Query("overdue"),
		Unassigned: c.Query("unassigned") == "1",
		Page:       page, PageSize: size,
	}
	if c.Query("mine") == "1" {
		q.AssigneeID = uid
	}
	if v := c.Query("assigneeId"); v != "" {
		if id, err := strconv.ParseInt(v, 10, 64); err == nil {
			q.AssigneeID = idgen.ID(id)
		}
	}
	list, total, err := h.svc.ListTickets(c.Request.Context(), q)
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.Page(c, list, total, page, size)
}

// GET /plugins/ticket/tickets/:no/replies?since=0&limit=50
func (h *handler) replies(c *gin.Context) {
	t, rows, err := h.svc.Replies(c.Request.Context(), c.Param("no"), parseSince(c), parseLimit(c), true)
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, gin.H{"ticket": t, "replies": rows})
}

// POST /plugins/ticket/tickets/:no/replies  {"content":"...","internal":false} —— 坐席回复/内部备注。
func (h *handler) adminReply(c *gin.Context) {
	var f struct {
		Content  string `json:"content" binding:"required"`
		Internal bool   `json:"internal"`
	}
	if !response.BindJSON(c, &f) {
		return
	}
	uid, name := claimsOperator(c)
	replierID := ""
	if uid != 0 {
		replierID = strconv.FormatInt(int64(uid), 10)
	}
	m, t, err := h.svc.Reply(c.Request.Context(), c.Param("no"), ReplyInput{
		ReplierType: ReplierAgent, ReplierID: replierID, ReplierName: name,
		Content: f.Content, Internal: f.Internal,
	})
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, gin.H{"reply": m, "ticket": t})
}

// POST /plugins/ticket/tickets/:no/status  {"status":"resolved|closed|processing"}
func (h *handler) setStatus(c *gin.Context) {
	var f struct {
		Status string `json:"status" binding:"required"`
	}
	if !response.BindJSON(c, &f) {
		return
	}
	operator := "agent"
	if cl := auth.ClaimsFrom(c); cl != nil && cl.Username != "" {
		operator = cl.Username
	}
	t, err := h.svc.SetStatus(c.Request.Context(), c.Param("no"), f.Status, operator)
	respond(c, t, err)
}

// POST /plugins/ticket/tickets/:no/assign  {"assigneeId":0,"assigneeName":""} —— assigneeId=0 表示认领给自己。
func (h *handler) assign(c *gin.Context) {
	var f struct {
		AssigneeID   int64  `json:"assigneeId"`
		AssigneeName string `json:"assigneeName"`
	}
	if !response.BindJSON(c, &f) {
		return
	}
	uid, name := claimsOperator(c)
	assigneeID, assigneeName := f.AssigneeID, f.AssigneeName
	if assigneeID == 0 { // 认领：取当前登录人
		if uid == 0 {
			response.Fail(c, response.Forbidden("error.forbidden"))
			return
		}
		assigneeID = int64(uid)
		if assigneeName == "" {
			assigneeName = name
		}
	}
	t, err := h.svc.Assign(c.Request.Context(), c.Param("no"), idgen.ID(assigneeID), assigneeName, uid)
	respond(c, t, err)
}

// ---- 访客公开端 ----

// POST /plugins/ticket/tickets  {"title","content","priority?","bizType?","bizNo?","reporterId","reporterName"}
func (h *handler) open(c *gin.Context) {
	var f CreateInput
	if !response.BindJSON(c, &f) {
		return
	}
	if f.ReporterType == "" {
		f.ReporterType = ReplierReporter
	}
	t, err := h.svc.Create(c.Request.Context(), f)
	respond(c, t, err)
}

// GET /plugins/ticket/tickets/:no —— 访客查自己的工单（含回复，不含内部备注）。
func (h *handler) visitorGet(c *gin.Context) {
	t, rows, err := h.svc.Replies(c.Request.Context(), c.Param("no"), 0, 200, false)
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, gin.H{"ticket": t, "replies": rows})
}

// POST /plugins/ticket/tickets/:no/replies —— 访客补充回复（resolved/closed 自动重开）。
func (h *handler) visitorReply(c *gin.Context) {
	var f struct {
		ReporterID   string `json:"reporterId"`
		ReporterName string `json:"reporterName"`
		Content      string `json:"content" binding:"required"`
	}
	if !response.BindJSON(c, &f) {
		return
	}
	m, t, err := h.svc.Reply(c.Request.Context(), c.Param("no"), ReplyInput{
		ReplierType: ReplierReporter, ReplierID: f.ReporterID, ReplierName: f.ReporterName, Content: f.Content,
	})
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, gin.H{"reply": m, "ticket": t})
}

// POST /plugins/ticket/visitor/tickets/:no/rating {"score":1..5,"comment":""} —— 满意度评价。
func (h *handler) visitorRate(c *gin.Context) {
	var f RateInput
	if !response.BindJSON(c, &f) {
		return
	}
	t, err := h.svc.Rate(c.Request.Context(), c.Param("no"), f)
	respond(c, t, err)
}

// ---- 坐席侧写操作 ----

// POST /plugins/ticket/tickets —— 后台代客建单。报告人始终是客户，创建人记登录坐席。
func (h *handler) create(c *gin.Context) {
	var f CreateInput
	if !response.BindJSON(c, &f) {
		return
	}
	uid, name := claimsOperator(c)
	f.CreatorID, f.CreatorName = uid, name
	f.ReporterType = ReplierReporter
	t, err := h.svc.Create(c.Request.Context(), f)
	respond(c, t, err)
}

// POST /plugins/ticket/tickets/bulk —— 批量认领/改派、改状态、改优先级（最多 100 张）。
func (h *handler) bulk(c *gin.Context) {
	var f BulkInput
	if !response.BindJSON(c, &f) {
		return
	}
	uid, name := claimsOperator(c)
	if f.Action == "assign" && f.AssigneeID == 0 {
		if uid == 0 {
			response.Fail(c, response.Forbidden("error.forbidden"))
			return
		}
		f.AssigneeID, f.AssigneeName = uid, name
	}
	res, err := h.svc.Bulk(c.Request.Context(), f, uid, name)
	respond(c, res, err)
}

// GET /plugins/ticket/canned —— 快捷回复列表。
func (h *handler) listCanned(c *gin.Context) {
	rows, err := h.svc.ListCanned(c.Request.Context())
	respond(c, rows, err)
}

// POST /plugins/ticket/canned  {"title","content","sort?"}
func (h *handler) createCanned(c *gin.Context) {
	var f CannedInput
	if !response.BindJSON(c, &f) {
		return
	}
	row, err := h.svc.CreateCanned(c.Request.Context(), f)
	respond(c, row, err)
}

// PUT /plugins/ticket/canned/:id  {"title","content","sort?"}
func (h *handler) updateCanned(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id <= 0 {
		response.Fail(c, response.BadRequest("error.bad_request"))
		return
	}
	var f CannedInput
	if !response.BindJSON(c, &f) {
		return
	}
	row, err := h.svc.UpdateCanned(c.Request.Context(), idgen.ID(id), f)
	respond(c, row, err)
}

// DELETE /plugins/ticket/canned/:id
func (h *handler) deleteCanned(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id <= 0 {
		response.Fail(c, response.BadRequest("error.bad_request"))
		return
	}
	respond(c, gin.H{"id": id}, h.svc.DeleteCanned(c.Request.Context(), idgen.ID(id)))
}

// GET /plugins/ticket/sla —— 当前 SLA 策略（进程内缓存，启动时从库加载）。
func (h *handler) getSLA(c *gin.Context) {
	respond(c, h.svc.CurrentSLA(), nil)
}

// PUT /plugins/ticket/sla —— 整份覆盖策略并刷新缓存。
func (h *handler) saveSLA(c *gin.Context) {
	var f SLASetting
	if !response.BindJSON(c, &f) {
		return
	}
	st, err := h.svc.SaveSLA(c.Request.Context(), f)
	respond(c, st, err)
}

// claimsOperator 取当前坐席的 ID 与显示名；未登录时返回 0/agent（调用方自行拒绝）。
func claimsOperator(c *gin.Context) (idgen.ID, string) {
	cl := auth.ClaimsFrom(c)
	if cl == nil {
		return 0, "agent"
	}
	name := cl.Username
	if name == "" {
		name = "agent"
	}
	return cl.UserID, name
}

func parseSince(c *gin.Context) int {
	since, _ := strconv.Atoi(c.Query("since"))
	return since
}

func parseLimit(c *gin.Context) int {
	limit, _ := strconv.Atoi(c.Query("limit"))
	return limit
}

// Register 注册坐席与访客路由（由 plugin.go 的 module 调用）。
func (h *handler) Register(r *gin.RouterGroup, d *core.Deps) {
	g := r.Group("/plugins/ticket")
	g.GET("/tickets", core.Perm("ticket:list"), h.list)
	// 统计与列表同一组筛选条件；注册在 /tickets/:no 之前，静态段优先匹配。
	g.GET("/tickets/stats", core.Perm("ticket:list"), h.Stats)
	// 后台代客建单与批量操作（静态段 /tickets/bulk 在 :no 之前注册）。
	g.POST("/tickets", core.Perm("ticket:create"), d.OperLogMiddleware("ticket", "create"), h.create)
	g.POST("/tickets/bulk", core.Perm("ticket:edit"), d.OperLogMiddleware("ticket", "update"), h.bulk)
	g.GET("/tickets/:no/replies", core.Perm("ticket:list"), h.replies)
	g.POST("/tickets/:no/replies", core.Perm("ticket:reply"), d.OperLogMiddleware("ticket", "reply"), h.adminReply)
	g.POST("/tickets/:no/status", core.Perm("ticket:edit"), d.OperLogMiddleware("ticket", "update"), h.setStatus)
	g.POST("/tickets/:no/assign", core.Perm("ticket:edit"), d.OperLogMiddleware("ticket", "update"), h.assign)

	g.GET("/canned", core.Perm("ticket:list"), h.listCanned)
	g.POST("/canned", core.Perm("ticket:edit"), d.OperLogMiddleware("ticket", "create"), h.createCanned)
	g.PUT("/canned/:id", core.Perm("ticket:edit"), d.OperLogMiddleware("ticket", "update"), h.updateCanned)
	g.DELETE("/canned/:id", core.Perm("ticket:edit"), d.OperLogMiddleware("ticket", "delete"), h.deleteCanned)

	g.GET("/sla", core.Perm("ticket:list"), h.getSLA)
	g.PUT("/sla", core.Perm("ticket:edit"), d.OperLogMiddleware("ticket", "update"), h.saveSLA)

	// 访客公开接口：限流按 IP；路径带 /visitor 前缀与坐席接口分开——同一个 engine 上
	// protected 与 Public 两组不能注册相同的 method+path，否则 gin 在 addRoute 时 panic。
	p := d.Public.Group("/plugins/ticket/visitor")
	p.POST("/tickets",
		middleware.RateLimit(d.Cache, d.Log, "ticket-open", middleware.ByIP(), 10, time.Minute), h.open)
	p.GET("/tickets/:no",
		middleware.RateLimit(d.Cache, d.Log, "ticket-view", middleware.ByIP(), 60, time.Minute), h.visitorGet)
	p.POST("/tickets/:no/replies",
		middleware.RateLimit(d.Cache, d.Log, "ticket-reply", middleware.ByIP(), 30, time.Minute), h.visitorReply)
	p.POST("/tickets/:no/rating",
		middleware.RateLimit(d.Cache, d.Log, "ticket-rate", middleware.ByIP(), 10, time.Minute), h.visitorRate)

	// 客户自助页挂在根路径：页面本身不需要登录，提交/查询仍走上面的公开 JSON 接口。
	d.Root.GET("/ticket/new",
		middleware.RateLimit(d.Cache, d.Log, "ticket-form-page", middleware.ByIP(), 60, time.Minute), h.formPage)
	d.Root.GET("/ticket/view",
		middleware.RateLimit(d.Cache, d.Log, "ticket-view-page", middleware.ByIP(), 60, time.Minute), h.viewPage)
}
