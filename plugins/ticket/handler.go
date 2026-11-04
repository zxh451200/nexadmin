package ticket

import (
	"mime"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"

	"nexadmin/core"
	"nexadmin/core/auth"
	"nexadmin/core/middleware"
	"nexadmin/core/response"
	"nexadmin/core/storage"
	"nexadmin/pkg/idgen"
)

// 路由前缀：/api/v1/plugins/ticket。权限码：ticket:list 查看；ticket:reply 回复/认领；ticket:edit 状态与改派。
// 访客公开接口挂在 d.Public（无登录），按 IP 限流；工单号是不可枚举的雪花 ID，充当能力令牌。

type handler struct {
	svc   *Service
	store storage.Storage
	// log 只用于导出这类「不进操作日志」的审计行；测试里手工构造 handler 时为 nil。
	log *zap.Logger
}

func respond(c *gin.Context, data any, err error) {
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, data)
}

// ---- 坐席端 ----

// GET /plugins/ticket/tickets?status=&priority=&bizType=&keyword=&overdue=1&assigneeId=&unassigned=1&mine=1&groupId=&tag=&sort=&order=&page=&pageSize=
func (h *handler) list(c *gin.Context) {
	page, size := response.ParsePage(c)
	q := ticketQueryFrom(c)
	q.Page, q.PageSize = page, size
	list, total, err := h.svc.ListTickets(c.Request.Context(), q)
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.Page(c, list, total, page, size)
}

// ticketQueryFrom 解析列表（与导出）的筛选条件；mine=1 走当前登录坐席。
func ticketQueryFrom(c *gin.Context) TicketQuery {
	uid, _ := claimsOperator(c)
	q := TicketQuery{
		Status: c.Query("status"), Priority: c.Query("priority"), BizType: c.Query("bizType"),
		Keyword: c.Query("keyword"), Overdue: c.Query("overdue"), Tag: c.Query("tag"),
		Sort: c.Query("sort"), Order: c.Query("order"),
		Unassigned: c.Query("unassigned") == "1",
	}
	if c.Query("mine") == "1" {
		q.AssigneeID = uid
	}
	if v := c.Query("assigneeId"); v != "" {
		if id, err := strconv.ParseInt(v, 10, 64); err == nil {
			q.AssigneeID = idgen.ID(id)
		}
	}
	if v := c.Query("groupId"); v != "" {
		if id, err := strconv.ParseInt(v, 10, 64); err == nil {
			q.GroupID = idgen.ID(id)
		}
	}
	return q
}

// GET /plugins/ticket/tickets/:no/replies?since=0&limit=50
func (h *handler) replies(c *gin.Context) {
	t, rows, err := h.svc.Replies(c.Request.Context(), c.Param("no"), parseSince(c), parseLimit(c), true)
	if err != nil {
		response.Fail(c, err)
		return
	}
	atts, err := h.svc.ListAttachments(c.Request.Context(), c.Param("no"), true)
	if err != nil {
		response.Fail(c, err)
		return
	}
	acts, err := h.svc.TicketActivities(c.Request.Context(), t.ID, false)
	if err != nil {
		response.Fail(c, err)
		return
	}
	quotaMaxCount, quotaMaxBytes := attachmentQuota()
	response.OK(c, gin.H{
		"ticket":      t,
		"replies":     rows,
		"attachments": atts,
		"activities":  acts,
		// 上限与可收类型由后端说一次，前端不再自己抄一份
		"quota": gin.H{"maxCount": quotaMaxCount, "maxBytes": quotaMaxBytes, "accept": attachmentAccept()},
	})
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
	// 公开入口不收标签：让客户自填就是让他给自己打「vip」，还会反过来操纵规则命中。
	f.Tags = nil
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
	atts, err := h.svc.ListAttachments(c.Request.Context(), c.Param("no"), false)
	if err != nil {
		response.Fail(c, err)
		return
	}
	// 客户只看得懂的两类动态：提交与状态变化；内部协作细节不外泄。
	acts, err := h.svc.TicketActivities(c.Request.Context(), t.ID, true)
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, gin.H{"ticket": t, "replies": rows, "attachments": atts, "activities": acts})
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
	// 坐席这一侧要求把必填的自定义字段补齐；客户自助页不要求（见 CreateInput 注释）。
	f.EnforceRequiredFields = true
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

// GET /plugins/ticket/tickets/badge —— 侧栏菜单徽标：待我注意的未完结数（见 badge.go）。
func (h *handler) badge(c *gin.Context) {
	uid, _ := claimsOperator(c)
	n, err := h.svc.BadgeCount(c.Request.Context(), int64(uid))
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, gin.H{"count": n})
}

// GET /plugins/ticket/tickets/:no/reporter-tickets —— 报告人最近的其他工单（详情侧栏）。
func (h *handler) reporterTickets(c *gin.Context) {
	rows, err := h.svc.ReporterHistory(c.Request.Context(), c.Param("no"))
	respond(c, rows, err)
}

// GET /plugins/ticket/settings —— 通知设置（邮件通知开关 / 门户地址）。
func (h *handler) getSettings(c *gin.Context) {
	respond(c, h.svc.SettingsFresh(c.Request.Context()), nil)
}

// PUT /plugins/ticket/settings  {"emailNotifyEnabled":true,"portalUrl":"https://…"}
func (h *handler) saveSettings(c *gin.Context) {
	var f SettingsInput
	if !response.BindJSON(c, &f) {
		return
	}
	row, err := h.svc.SaveSettings(c.Request.Context(), f)
	respond(c, row, err)
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

// GET /plugins/ticket/groups —— 分组与成员。
func (h *handler) listGroups(c *gin.Context) {
	rows, err := h.svc.ListGroups(c.Request.Context())
	respond(c, rows, err)
}

// POST /plugins/ticket/groups {"name","description","autoAssign","enabled","sort"}
func (h *handler) createGroup(c *gin.Context) {
	var f GroupInput
	if !response.BindJSON(c, &f) {
		return
	}
	g, err := h.svc.CreateGroup(c.Request.Context(), f)
	respond(c, g, err)
}

// PUT /plugins/ticket/groups/:id
func (h *handler) updateGroup(c *gin.Context) {
	id, ok := pathID(c)
	if !ok {
		return
	}
	var f GroupInput
	if !response.BindJSON(c, &f) {
		return
	}
	g, err := h.svc.UpdateGroup(c.Request.Context(), id, f)
	respond(c, g, err)
}

// DELETE /plugins/ticket/groups/:id
func (h *handler) deleteGroup(c *gin.Context) {
	id, ok := pathID(c)
	if !ok {
		return
	}
	respond(c, gin.H{"id": id}, h.svc.DeleteGroup(c.Request.Context(), id))
}

// PUT /plugins/ticket/groups/:id/members {"userIds":[1,2]}
func (h *handler) setGroupMembers(c *gin.Context) {
	id, ok := pathID(c)
	if !ok {
		return
	}
	var f struct {
		UserIDs []int64 `json:"userIds"`
	}
	if !response.BindJSON(c, &f) {
		return
	}
	ids := make([]idgen.ID, 0, len(f.UserIDs))
	for _, uid := range f.UserIDs {
		if uid > 0 {
			ids = append(ids, idgen.ID(uid))
		}
	}
	view, err := h.svc.SetGroupMembers(c.Request.Context(), id, ids)
	respond(c, view, err)
}

// POST /plugins/ticket/tickets/:no/group {"groupId":0} —— 0 表示移出分组。
func (h *handler) assignGroup(c *gin.Context) {
	var f struct {
		GroupID int64 `json:"groupId"`
	}
	if !response.BindJSON(c, &f) {
		return
	}
	t, err := h.svc.AssignGroup(c.Request.Context(), c.Param("no"), idgen.ID(f.GroupID))
	respond(c, t, err)
}

// GET /plugins/ticket/rules —— 自动化规则列表。
func (h *handler) listRules(c *gin.Context) {
	rows, err := h.svc.ListRules(c.Request.Context())
	respond(c, rows, err)
}

// GET /plugins/ticket/biz-types —— 规则条件可用的业务类型候选（由已有工单与既有规则派生）。
func (h *handler) listBizTypes(c *gin.Context) {
	rows, err := h.svc.BizTypeOptions(c.Request.Context())
	respond(c, rows, err)
}

// POST /plugins/ticket/rules
func (h *handler) createRule(c *gin.Context) {
	var f RuleInput
	if !response.BindJSON(c, &f) {
		return
	}
	r, err := h.svc.CreateRule(c.Request.Context(), f)
	respond(c, r, err)
}

// PUT /plugins/ticket/rules/:id
func (h *handler) updateRule(c *gin.Context) {
	id, ok := pathID(c)
	if !ok {
		return
	}
	var f RuleInput
	if !response.BindJSON(c, &f) {
		return
	}
	r, err := h.svc.UpdateRule(c.Request.Context(), id, f)
	respond(c, r, err)
}

// DELETE /plugins/ticket/rules/:id
func (h *handler) deleteRule(c *gin.Context) {
	id, ok := pathID(c)
	if !ok {
		return
	}
	respond(c, gin.H{"id": id}, h.svc.DeleteRule(c.Request.Context(), id))
}

// GET /plugins/ticket/tags —— 使用中的标签（筛选与建议）。
func (h *handler) listTags(c *gin.Context) {
	rows, err := h.svc.ListTags(c.Request.Context())
	respond(c, rows, err)
}

// PUT /plugins/ticket/tickets/:no/tags {"tags":["vip","refund"]}
func (h *handler) setTags(c *gin.Context) {
	var f TagsInput
	if !response.BindJSON(c, &f) {
		return
	}
	t, err := h.svc.SetTags(c.Request.Context(), c.Param("no"), f.Tags)
	respond(c, t, err)
}

// GET /plugins/ticket/tickets-export?<与列表完全相同的筛选> —— 导出工单记录（xlsx，最多 5000 行）。
// 不挂 OperLogMiddleware：操作日志中间件会把响应体前 8KB 当文本入库，对 zip 容器没有意义
// （clip 会裁到合法 UTF-8，存进去是一堆乱码），所以这里改成往应用日志记一条结构化审计：
// 谁、按什么条件、导出了几行。
func (h *handler) export(c *gin.Context) {
	ctx := c.Request.Context()
	rows, err := h.svc.ExportTickets(ctx, ticketQueryFrom(c), maxExportRows())
	if err != nil {
		response.Fail(c, err)
		return
	}
	hd := c.Writer.Header()
	hd.Set("Content-Type", "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet")
	hd.Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": h.svc.ExportFilename()}))
	hd.Set("Cache-Control", "no-store")
	hd.Set("X-Content-Type-Options", "nosniff")
	c.Status(http.StatusOK)
	if err := h.svc.WriteTicketsXLSX(ctx, c.Writer, rows); err != nil {
		if h.svc.log != nil {
			h.svc.log.Warn("ticket: export failed midway", "rows", len(rows), "err", err.Error())
		}
		return
	}
	if h.log != nil {
		_, operator := claimsOperator(c)
		h.log.Info("ticket export", zap.String("operator", operator),
			zap.Int("rows", len(rows)), zap.String("filters", c.Request.URL.RawQuery))
	}
}

func pathID(c *gin.Context) (idgen.ID, bool) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id <= 0 {
		response.Fail(c, response.BadRequest("error.bad_request"))
		return 0, false
	}
	return idgen.ID(id), true
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

// agentActorContext 把登录坐席放进 context，服务层在同一条事务里记动态时取用。
// 公开接口与清扫器没有登录态，发起人就是系统，不冒充成某个人。
func (h *handler) agentActorContext(c *gin.Context) {
	uid, name := claimsOperator(c)
	if uid != 0 {
		c.Request = c.Request.WithContext(WithActor(c.Request.Context(),
			ActivityActor{Type: ActorAgent, ID: strconv.FormatInt(int64(uid), 10), Name: name}))
	}
	c.Next()
}

// Register 注册坐席与访客路由（由 plugin.go 的 module 调用）。
func (h *handler) Register(r *gin.RouterGroup, d *core.Deps) {
	g := r.Group("/plugins/ticket")
	g.Use(h.agentActorContext)
	// 数据范围先解析进 ctx：ticketFilter 裁列表，inScope 裁单条，两者读同一份范围。
	g.Use(h.resolveScope)
	g.GET("/tickets", core.Perm("ticket:list"), h.list)
	// 统计与列表同一组筛选条件；注册在 /tickets/:no 之前，静态段优先匹配。
	g.GET("/tickets/stats", core.Perm("ticket:list"), h.Stats)
	// 导出沿用列表的全部筛选（静态段 /tickets-export 与 :no 不冲突）。
	g.GET("/tickets-export", core.Perm("ticket:list"), h.export)
	// 后台代客建单与批量操作（静态段 /tickets/bulk 在 :no 之前注册）。
	g.POST("/tickets", core.Perm("ticket:create"), d.OperLogMiddleware("ticket", "create"), h.create)
	g.POST("/tickets/bulk", core.Perm("ticket:edit"), d.OperLogMiddleware("ticket", "update"), h.bulk)
	g.GET("/tickets/:no/replies", core.Perm("ticket:list"), h.inScope, h.replies)
	g.POST("/tickets/:no/replies", core.Perm("ticket:reply"), h.inScope, d.OperLogMiddleware("ticket", "reply"), h.adminReply)
	g.POST("/tickets/:no/status", core.Perm("ticket:edit"), h.inScope, d.OperLogMiddleware("ticket", "update"), h.setStatus)
	g.POST("/tickets/:no/assign", core.Perm("ticket:edit"), h.inScope, d.OperLogMiddleware("ticket", "update"), h.assign)
	g.POST("/tickets/:no/attachments", core.Perm("ticket:reply"), h.inScope, h.uploadAdmin)
	g.GET("/tickets/:no/attachments/:token", core.Perm("ticket:list"), h.inScope, h.downloadAdmin)
	// 删附件＝改附件能力的对偶，沿用 ticket:reply；删除本身要进操作日志（它是普通 JSON 响应）。
	g.DELETE("/tickets/:no/attachments/:token", core.Perm("ticket:reply"), h.inScope, d.OperLogMiddleware("ticket", "delete"), h.deleteAdmin)

	g.GET("/canned", core.Perm("ticket:list"), h.listCanned)
	g.POST("/canned", core.Perm("ticket:edit"), d.OperLogMiddleware("ticket", "create"), h.createCanned)
	g.PUT("/canned/:id", core.Perm("ticket:edit"), d.OperLogMiddleware("ticket", "update"), h.updateCanned)
	g.DELETE("/canned/:id", core.Perm("ticket:edit"), d.OperLogMiddleware("ticket", "delete"), h.deleteCanned)

	g.GET("/sla", core.Perm("ticket:list"), h.getSLA)
	g.PUT("/sla", core.Perm("ticket:edit"), d.OperLogMiddleware("ticket", "update"), h.saveSLA)

	g.GET("/groups", core.Perm("ticket:list"), h.listGroups)
	g.POST("/groups", core.Perm("ticket:edit"), d.OperLogMiddleware("ticket", "create"), h.createGroup)
	g.PUT("/groups/:id", core.Perm("ticket:edit"), d.OperLogMiddleware("ticket", "update"), h.updateGroup)
	g.DELETE("/groups/:id", core.Perm("ticket:edit"), d.OperLogMiddleware("ticket", "delete"), h.deleteGroup)
	g.PUT("/groups/:id/members", core.Perm("ticket:edit"), d.OperLogMiddleware("ticket", "update"), h.setGroupMembers)
	g.POST("/tickets/:no/group", core.Perm("ticket:edit"), h.inScope, d.OperLogMiddleware("ticket", "update"), h.assignGroup)

	g.GET("/rules", core.Perm("ticket:list"), h.listRules)
	g.GET("/biz-types", core.Perm("ticket:list"), h.listBizTypes)
	g.POST("/rules", core.Perm("ticket:edit"), d.OperLogMiddleware("ticket", "create"), h.createRule)
	g.PUT("/rules/:id", core.Perm("ticket:edit"), d.OperLogMiddleware("ticket", "update"), h.updateRule)
	g.DELETE("/rules/:id", core.Perm("ticket:edit"), d.OperLogMiddleware("ticket", "delete"), h.deleteRule)

	g.GET("/tags", core.Perm("ticket:list"), h.listTags)
	g.PUT("/tickets/:no/tags", core.Perm("ticket:edit"), h.inScope, d.OperLogMiddleware("ticket", "update"), h.setTags)

	// 自定义字段（见 field.go）：定义是配置类资源，复用 ticket:list / ticket:edit，不新增权限码。
	g.GET("/fields", core.Perm("ticket:list"), h.listFields)
	g.POST("/fields", core.Perm("ticket:edit"), d.OperLogMiddleware("ticket", "create"), h.createField)
	g.PUT("/fields/:id", core.Perm("ticket:edit"), d.OperLogMiddleware("ticket", "update"), h.updateField)
	g.DELETE("/fields/:id", core.Perm("ticket:edit"), d.OperLogMiddleware("ticket", "delete"), h.deleteField)
	g.POST("/tickets/:no/custom-values", core.Perm("ticket:edit"), h.inScope, d.OperLogMiddleware("ticket", "update"), h.setCustomValues)

	// 多套 SLA 策略与节假日（见 sla.go）：默认策略仍走上面的 GET/PUT /sla，这里只管命名策略。
	g.GET("/sla/policies", core.Perm("ticket:list"), h.listSLAPolicies)
	g.POST("/sla/policy", core.Perm("ticket:edit"), d.OperLogMiddleware("ticket", "update"), h.saveSLAPolicy)
	g.DELETE("/sla/policy/:code", core.Perm("ticket:edit"), d.OperLogMiddleware("ticket", "delete"), h.deleteSLAPolicy)

	// 合并与关联单（见 merge.go / merge_handler.go）：复用 ticket:edit / ticket:list，不新增权限码。
	// 对侧工单号在请求体里，中间件只挡得住 :no，所以 merge.go 里还要单独校对侧（见 visibleOther）。
	g.POST("/tickets/:no/merge", core.Perm("ticket:edit"), h.inScope, d.OperLogMiddleware("ticket", "update"), h.mergeTicket)
	g.GET("/tickets/:no/links", core.Perm("ticket:list"), h.inScope, h.links)
	g.POST("/tickets/:no/links", core.Perm("ticket:edit"), h.inScope, d.OperLogMiddleware("ticket", "update"), h.addLink)
	g.DELETE("/tickets/:no/links/:other", core.Perm("ticket:edit"), h.inScope, d.OperLogMiddleware("ticket", "delete"), h.removeLink)

	// 侧栏面板：菜单徽标计数与报告人历史工单（见 badge.go）。徽标走静默轮询，不进操作日志。
	g.GET("/tickets/badge", core.Perm("ticket:list"), h.badge)
	g.GET("/tickets/:no/reporter-tickets", core.Perm("ticket:list"), h.inScope, h.reporterTickets)

	// 插件设置（邮件通知开关 / 客服门户地址，见 notify.go）：读复用 list，写要求 edit。
	g.GET("/settings", core.Perm("ticket:list"), h.getSettings)
	g.PUT("/settings", core.Perm("ticket:edit"), core.RequirePlatform(), d.OperLogMiddleware("ticket", "update"), h.saveSettings)

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
	p.POST("/tickets/:no/attachments",
		middleware.RateLimit(d.Cache, d.Log, "ticket-upload", middleware.ByIP(), 20, time.Minute), h.uploadVisitor)
	p.GET("/tickets/:no/attachments/:token",
		middleware.RateLimit(d.Cache, d.Log, "ticket-download", middleware.ByIP(), 120, time.Minute), h.downloadVisitor)
	// 访客只能删自己上传的公开附件（服务层判定），能力令牌与下载同口径；限流收紧到 20 次/分。
	p.DELETE("/tickets/:no/attachments/:token",
		middleware.RateLimit(d.Cache, d.Log, "ticket-attachment-delete", middleware.ByIP(), 20, time.Minute), h.deleteVisitor)

	// 邮件进单：任何能 POST 原始 MIME 的组件都能接（IMAP 轮询器 / 邮件网关 / 厂商 inbound）。
	// 无登录，靠 X-Ticket-Secret；未配置密钥时整体 503。IP 限流只是兜底（网关会突发）。
	in := d.Public.Group("/plugins/ticket/inbound")
	in.POST("/email",
		middleware.RateLimit(d.Cache, d.Log, "ticket-inbound", middleware.ByIP(), 120, time.Minute), h.inboundEmail)

	// 客户自助页挂在根路径：页面本身不需要登录，提交/查询仍走上面的公开 JSON 接口。
	d.Root.GET("/ticket/new",
		middleware.RateLimit(d.Cache, d.Log, "ticket-form-page", middleware.ByIP(), 60, time.Minute), h.formPage)
	d.Root.GET("/ticket/view",
		middleware.RateLimit(d.Cache, d.Log, "ticket-view-page", middleware.ByIP(), 60, time.Minute), h.viewPage)
}
