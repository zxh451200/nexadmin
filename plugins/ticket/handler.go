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

// GET /plugins/ticket/tickets?status=&priority=&bizType=&keyword=&overdue=1&page=&pageSize=
func (h *handler) list(c *gin.Context) {
	page, size := response.ParsePage(c)
	list, total, err := h.svc.ListTickets(c.Request.Context(), TicketQuery{
		Status: c.Query("status"), Priority: c.Query("priority"), BizType: c.Query("bizType"),
		Keyword: c.Query("keyword"), Overdue: c.Query("overdue"),
		Page: page, PageSize: size,
	})
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.Page(c, list, total, page, size)
}

// GET /plugins/ticket/tickets/:no/replies?since=0&limit=50
func (h *handler) replies(c *gin.Context) {
	t, rows, err := h.svc.Replies(c.Request.Context(), c.Param("no"), parseSince(c), parseLimit(c))
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, gin.H{"ticket": t, "replies": rows})
}

// POST /plugins/ticket/tickets/:no/replies  {"content":"..."} —— 坐席回复。
func (h *handler) adminReply(c *gin.Context) {
	var f struct {
		Content string `json:"content" binding:"required"`
	}
	if !response.BindJSON(c, &f) {
		return
	}
	replierID, replierName := "", "agent"
	if cl := auth.ClaimsFrom(c); cl != nil {
		replierID = strconv.FormatInt(int64(cl.UserID), 10)
		if cl.Username != "" {
			replierName = cl.Username
		}
	}
	m, t, err := h.svc.Reply(c.Request.Context(), c.Param("no"), ReplyInput{
		ReplierType: ReplierAgent, ReplierID: replierID, ReplierName: replierName, Content: f.Content,
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
	assigneeID, assigneeName := f.AssigneeID, f.AssigneeName
	if assigneeID == 0 { // 认领：取当前登录人
		cl := auth.ClaimsFrom(c)
		if cl == nil || cl.UserID == 0 {
			response.Fail(c, response.Forbidden("error.forbidden"))
			return
		}
		assigneeID = int64(cl.UserID)
		if assigneeName == "" && cl.Username != "" {
			assigneeName = cl.Username
		}
	}
	t, err := h.svc.Assign(c.Request.Context(), c.Param("no"), idgen.ID(assigneeID), assigneeName)
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

// GET /plugins/ticket/tickets/:no —— 访客查自己的工单（含回复）。
func (h *handler) visitorGet(c *gin.Context) {
	t, rows, err := h.svc.Replies(c.Request.Context(), c.Param("no"), 0, 200)
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
	g.GET("/tickets/:no/replies", core.Perm("ticket:list"), h.replies)
	g.POST("/tickets/:no/replies", core.Perm("ticket:reply"), h.adminReply)
	g.POST("/tickets/:no/status", core.Perm("ticket:edit"), h.setStatus)
	g.POST("/tickets/:no/assign", core.Perm("ticket:edit"), h.assign)

	// 访客公开接口：限流按 IP；路径带 /visitor 前缀与坐席接口分开——同一个 engine 上
	// protected 与 Public 两组不能注册相同的 method+path，否则 gin 在 addRoute 时 panic。
	p := d.Public.Group("/plugins/ticket/visitor")
	p.POST("/tickets",
		middleware.RateLimit(d.Cache, d.Log, "ticket-open", middleware.ByIP(), 10, time.Minute), h.open)
	p.GET("/tickets/:no",
		middleware.RateLimit(d.Cache, d.Log, "ticket-view", middleware.ByIP(), 60, time.Minute), h.visitorGet)
	p.POST("/tickets/:no/replies",
		middleware.RateLimit(d.Cache, d.Log, "ticket-reply", middleware.ByIP(), 30, time.Minute), h.visitorReply)
}
