package im

import (
	"bytes"
	"crypto/subtle"
	"io"
	"mime"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"nexadmin/core"
	"nexadmin/core/auth"
	"nexadmin/core/cache"
	"nexadmin/core/i18n"
	"nexadmin/core/middleware"
	"nexadmin/core/response"
	"nexadmin/core/storage"
	"nexadmin/pkg/excel"
	"nexadmin/pkg/idgen"
)

// 路由前缀：/api/v1/plugins/im。权限码：im:list 查看会话与消息；im:reply 回复/认领/快捷回复查看；im:edit 关闭/标签/快捷回复维护。
// 访客公开接口挂在 d.Public（无登录），靠 IP 限流 + 不可枚举的会话号兜底。

type handler struct {
	svc     *Service
	tickets *streamTicket
	cache   cache.Cache
	store   storage.Storage
}

func respond(c *gin.Context, data any, err error) {
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, data)
}

// respondConv 是会话响应的空指针安全版本：服务层出错时 conv 为 nil，不能先解引用。
func respondConv(c *gin.Context, conv *Conversation, err error) {
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, convView(conv))
}

func convView(c *Conversation) ConversationView {
	return ConversationView{Conversation: *c}
}

// agentOf 取当前坐席标识（ID / 用户名）。
func agentOf(c *gin.Context) (string, string) {
	cl := auth.ClaimsFrom(c)
	if cl == nil {
		return "", ""
	}
	name := cl.Username
	if name == "" {
		name = "agent"
	}
	return strconv.FormatInt(int64(cl.UserID), 10), name
}

// ---- 坐席端 ----

// GET /plugins/im/conversations?status=&bizType=&keyword=&assignee=&tag=&page=&pageSize=
// assignee=me 只看我的；assignee=unassigned 只看未分配。
func (h *handler) list(c *gin.Context) {
	page, size := response.ParsePage(c)
	q := convQueryFrom(c)
	q.Page, q.PageSize = page, size
	list, total, err := h.svc.ListConversations(c.Request.Context(), q)
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.Page(c, list, total, page, size)
}

// convQueryFrom 解析列表/导出共用的筛选条件（分页由调用方补）。
func convQueryFrom(c *gin.Context) ConversationQuery {
	q := ConversationQuery{
		Status: c.Query("status"), BizType: c.Query("bizType"), Keyword: c.Query("keyword"),
		Tag: c.Query("tag"),
	}
	switch c.Query("assignee") {
	case "me":
		if id, _ := agentOf(c); id != "" {
			q.AssigneeID = id
		}
	case "unassigned":
		q.Unassigned = true
	case "":
	default:
		q.AssigneeID = c.Query("assignee")
	}
	return q
}

// GET /plugins/im/conversations/:no/messages?since=&before=&limit= —— 坐席拉取并推进已读游标。
// since 增量；before 向上翻页；都不带取最近一页。
func (h *handler) adminMessages(c *gin.Context) {
	conv, rows, hasMore, err := h.svc.Messages(c.Request.Context(), c.Param("no"), messageQuery(c))
	if err != nil {
		response.Fail(c, err)
		return
	}
	if n := len(rows); n > 0 {
		if err := h.svc.MarkAgentRead(c.Request.Context(), conv.ConvNo, rows[n-1].Seq); err != nil {
			response.Fail(c, err)
			return
		}
	}
	// 坐席正在看这个会话：短窗口内不再给站内信提醒（见 notify.go）。
	if h.cache != nil {
		_ = h.cache.Set(c.Request.Context(), activeKey(conv.ConvNo), "1", agentActiveTTL)
	}
	response.OK(c, gin.H{"conversation": convView(conv), "messages": rows, "hasMore": hasMore})
}

// POST /plugins/im/conversations/:no/messages  {"content":"..."} —— 坐席回复。
func (h *handler) adminSend(c *gin.Context) {
	var f struct {
		Content         string `json:"content"`
		AttachmentToken string `json:"attachmentToken"`
	}
	if !response.BindJSON(c, &f) {
		return
	}
	senderID, senderName := agentOf(c)
	if senderID == "" {
		senderID, senderName = "", "agent"
	}
	m, err := h.svc.Send(c.Request.Context(), c.Param("no"), SendInput{
		SenderType: SenderAgent, SenderID: senderID, SenderName: senderName,
		Content: f.Content, Attachment: f.AttachmentToken,
	})
	respond(c, m, err)
}

// POST /plugins/im/conversations/:no/close
func (h *handler) close(c *gin.Context) {
	id, _ := agentOf(c)
	conv, _, err := h.svc.Close(c.Request.Context(), c.Param("no"), id)
	respondConv(c, conv, err)
}

// POST /plugins/im/conversations/:no/claim —— 认领（首次回复也会自动认领）。
func (h *handler) claim(c *gin.Context) {
	id, name := agentOf(c)
	conv, err := h.svc.Claim(c.Request.Context(), c.Param("no"), id, name)
	respondConv(c, conv, err)
}

// POST /plugins/im/conversations/:no/release —— 释放认领。
func (h *handler) release(c *gin.Context) {
	conv, err := h.svc.Release(c.Request.Context(), c.Param("no"))
	respondConv(c, conv, err)
}

// PUT /plugins/im/conversations/:no/tags  {"tags":"退款,物流"}。
func (h *handler) tags(c *gin.Context) {
	var f struct {
		Tags string `json:"tags"`
	}
	if !response.BindJSON(c, &f) {
		return
	}
	conv, err := h.svc.SetTags(c.Request.Context(), c.Param("no"), f.Tags)
	respondConv(c, conv, err)
}

// POST /plugins/im/conversations/:no/typing —— 坐席"正在输入"（不落库）。
func (h *handler) adminTyping(c *gin.Context) {
	h.svc.Typing(c.Param("no"), SenderAgent)
	response.OK(c, gin.H{"ok": true})
}

// PUT /plugins/im/conversations/:no/note  {"note":"..."} —— 内部备注（仅坐席可见，不会发给访客）。
func (h *handler) note(c *gin.Context) {
	var f struct {
		Note string `json:"note"`
	}
	if !response.BindJSON(c, &f) {
		return
	}
	conv, err := h.svc.SetNote(c.Request.Context(), c.Param("no"), f.Note)
	respondConv(c, conv, err)
}

// DELETE /plugins/im/conversations/:no —— 超管删除一个已结束的会话（级联消息、附件与存储对象）。
func (h *handler) deleteConv(c *gin.Context) {
	res, err := h.svc.DeleteConversation(c.Request.Context(), c.Param("no"))
	respond(c, res, err)
}

// POST /plugins/im/conversations-delete  {"nos":["IM..."]} —— 超管批量删除，只删已结束的，其余记入 skipped。
func (h *handler) batchDelete(c *gin.Context) {
	var f struct {
		Nos []string `json:"nos"`
	}
	if !response.BindJSON(c, &f) {
		return
	}
	if len(f.Nos) == 0 {
		response.Fail(c, response.BadRequest("error.bad_request"))
		return
	}
	res, err := h.svc.DeleteConversations(c.Request.Context(), f.Nos)
	respond(c, res, err)
}

// POST /plugins/im/presence/heartbeat —— 坐席心跳；开启自动分配时顺带领走一个排队会话。
func (h *handler) heartbeat(c *gin.Context) {
	id, name := agentOf(c)
	if id == "" {
		response.Fail(c, response.Unauthorized("error.unauthorized"))
		return
	}
	h.svc.Heartbeat(c.Request.Context(), id)
	assigned, err := h.svc.AssignNext(c.Request.Context(), id, name)
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, gin.H{"ok": true, "assigned": assigned})
}

// GET /plugins/im/stats?days=7 —— 会话统计（首响 / 满意度 / 按天趋势 / 坐席工作量）。
func (h *handler) stats(c *gin.Context) {
	days, _ := strconv.Atoi(c.Query("days"))
	st, err := h.svc.Stats(c.Request.Context(), days)
	respond(c, st, err)
}

// POST /plugins/im/conversations/:no/messages/:seq/recall —— 撤回本人 2 分钟内发的消息。
func (h *handler) recall(c *gin.Context) {
	seq, err := strconv.ParseInt(c.Param("seq"), 10, 64)
	if err != nil || seq <= 0 {
		response.Fail(c, response.BadRequest("error.bad_request"))
		return
	}
	id, _ := agentOf(c)
	m, rerr := h.svc.Recall(c.Request.Context(), c.Param("no"), seq, id)
	respond(c, m, rerr)
}

// POST /plugins/im/conversations/:no/merge  {"target":"IM..."} —— 把本会话合并进目标会话。
func (h *handler) merge(c *gin.Context) {
	var f struct {
		Target string `json:"target" binding:"required"`
	}
	if !response.BindJSON(c, &f) {
		return
	}
	id, _ := agentOf(c)
	res, err := h.svc.Merge(c.Request.Context(), c.Param("no"), f.Target, id)
	respond(c, res, err)
}

// exportLimit 是一次导出的行数上限（与列表筛选一致，避免拖垮接口）。
const exportLimit = 5000

// GET /plugins/im/conversations-export?<与列表相同筛选> —— 导出会话记录（xlsx）。
func (h *handler) export(c *gin.Context) {
	ctx := c.Request.Context()
	rows, err := h.svc.ExportConversations(ctx, convQueryFrom(c), exportLimit)
	if err != nil {
		response.Fail(c, err)
		return
	}
	hd := c.Writer.Header()
	hd.Set("Content-Type", "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet")
	hd.Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{
		"filename": "im-conversations-" + time.Now().Format("20060102-150405") + ".xlsx",
	}))
	hd.Set("Cache-Control", "no-store")
	hd.Set("X-Content-Type-Options", "nosniff")

	x := excel.NewWriter(c.Writer)
	sh, err := x.NewSheet(i18n.T(ctx, "im.export_sheet"), excel.SheetOptions{Header: []string{
		i18n.T(ctx, "im.export_conv_no"), i18n.T(ctx, "im.export_visitor"), i18n.T(ctx, "im.export_biz"),
		i18n.T(ctx, "im.export_assignee"), i18n.T(ctx, "im.export_status"), i18n.T(ctx, "im.export_tags"),
		i18n.T(ctx, "im.export_note"), i18n.T(ctx, "im.export_rating"), i18n.T(ctx, "im.export_created_at"),
		i18n.T(ctx, "im.export_closed_at"),
	}})
	if err != nil {
		if h.svc.log != nil {
			h.svc.log.Warn("im: export open sheet failed", "err", err.Error())
		}
		return
	}
	for _, r := range rows {
		visitor := r.VisitorName
		if visitor == "" {
			visitor = r.VisitorToken
		}
		biz := r.BizNo
		if r.BizType != "" && biz != "" {
			biz = r.BizType + ": " + biz
		}
		closed, rating := "", ""
		if r.ClosedAt != nil {
			closed = r.ClosedAt.Local().Format("2006-01-02 15:04:05")
		}
		if r.Rating > 0 {
			rating = strconv.Itoa(r.Rating)
		}
		if err := sh.WriteRow(r.ConvNo, visitor, biz, r.AssigneeName, i18n.T(ctx, "im.status_"+r.Status),
			r.Tags, r.Note, rating, r.CreatedAt.Local().Format("2006-01-02 15:04:05"), closed); err != nil {
			break
		}
	}
	if err := x.Close(); err != nil && h.svc.log != nil {
		h.svc.log.Warn("im: export failed midway", "rows", len(rows), "err", err.Error())
	}
}

// GET /plugins/im/quick-replies
func (h *handler) quickReplyList(c *gin.Context) {
	rows, err := h.svc.QuickReplyList(c.Request.Context())
	respond(c, rows, err)
}

// POST /plugins/im/quick-replies  {"title":"","content":"","sort":0}
func (h *handler) quickReplyCreate(c *gin.Context) {
	var f struct {
		Title   string `json:"title" binding:"required"`
		Content string `json:"content" binding:"required"`
		Sort    int    `json:"sort"`
	}
	if !response.BindJSON(c, &f) {
		return
	}
	q, err := h.svc.QuickReplySave(c.Request.Context(), 0, f.Title, f.Content, f.Sort)
	respond(c, q, err)
}

// PUT /plugins/im/quick-replies/:id
func (h *handler) quickReplyUpdate(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id <= 0 {
		response.Fail(c, response.BadRequest("error.bad_request"))
		return
	}
	var f struct {
		Title   string `json:"title" binding:"required"`
		Content string `json:"content" binding:"required"`
		Sort    int    `json:"sort"`
	}
	if !response.BindJSON(c, &f) {
		return
	}
	q, err := h.svc.QuickReplySave(c.Request.Context(), idgen.ID(id), f.Title, f.Content, f.Sort)
	respond(c, q, err)
}

// DELETE /plugins/im/quick-replies/:id
func (h *handler) quickReplyDelete(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id <= 0 {
		response.Fail(c, response.BadRequest("error.bad_request"))
		return
	}
	respond(c, gin.H{"ok": true}, h.svc.QuickReplyDelete(c.Request.Context(), idgen.ID(id)))
}

// ---- 访客公开端 ----

// POST /plugins/im/conversations  {"visitorToken":"...","visitorName":"","bizType":"","bizNo":"","subject":""}
func (h *handler) open(c *gin.Context) {
	var f OpenInput
	if !response.BindJSON(c, &f) {
		return
	}
	f.IP = c.ClientIP()
	f.UA = c.Request.UserAgent()
	f.Ref = c.Request.Referer()
	conv, err := h.svc.Open(c.Request.Context(), f)
	respondConv(c, conv, err)
}

// GET /plugins/im/visitor/status —— 挂件探测本实例是否装了在线客服，并带回服务时间与邀请配置。
// 插件未挂载时整组路由 404，挂件据此静默隐藏，不需要额外的编译期开关。
func (h *handler) visitorStatus(c *gin.Context) {
	st := h.svc.Settings(c.Request.Context())
	response.OK(c, gin.H{
		"ok": true, "online": st.OnlineAt(time.Now()),
		"businessHours": st.BusinessHours, "hoursStart": st.HoursStart, "hoursEnd": st.HoursEnd,
		"workdays": st.Workdays, "timezone": st.Timezone,
		"offlineText": st.OfflineText, "inviteDelay": st.InviteDelay, "inviteText": st.InviteText,
	})
}

// GET /plugins/im/settings —— 工作时间 / 离线提示 / 主动邀请。
func (h *handler) getSettings(c *gin.Context) {
	response.OK(c, h.svc.Settings(c.Request.Context()))
}

// PUT /plugins/im/settings
func (h *handler) putSettings(c *gin.Context) {
	var f Settings
	if !response.BindJSON(c, &f) {
		return
	}
	st, err := h.svc.SaveSettings(c.Request.Context(), f)
	respond(c, st, err)
}

// visitorToken 取访客令牌：普通请求走请求头，EventSource 不能设头，回落到查询参数。
func visitorToken(c *gin.Context) string {
	if t := strings.TrimSpace(c.GetHeader("X-Visitor-Token")); t != "" {
		return t
	}
	return strings.TrimSpace(c.Query("visitorToken"))
}

// checkVisitor 校验会话归属：会话号是可枚举的雪花 ID，不能单独充当凭证。
func (h *handler) checkVisitor(c *gin.Context, conv *Conversation) bool {
	// 两边都为空时 ConstantTimeCompare 也返回 1，历史异常的会话必须显式拒绝。
	if conv.VisitorToken == "" || subtle.ConstantTimeCompare([]byte(visitorToken(c)), []byte(conv.VisitorToken)) != 1 {
		response.Fail(c, response.Unauthorized("im.visitor_required"))
		return false
	}
	return true
}

// GET /plugins/im/conversations/:no/messages?since=0&limit=50 —— 访客轮询。
func (h *handler) visitorMessages(c *gin.Context) {
	conv, err := h.svc.Conversation(c.Request.Context(), c.Param("no"))
	if err != nil {
		response.Fail(c, err)
		return
	}
	if !h.checkVisitor(c, conv) {
		return
	}
	conv, rows, hasMore, err := h.svc.Messages(c.Request.Context(), conv.ConvNo, messageQuery(c))
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, gin.H{"conversation": convView(conv), "messages": rows, "hasMore": hasMore})
}

// POST /plugins/im/conversations/:no/messages  {"senderId":"...","senderName":"...","content":"..."}
func (h *handler) visitorSend(c *gin.Context) {
	var f struct {
		SenderID        string `json:"senderId"`
		SenderName      string `json:"senderName"`
		Content         string `json:"content"`
		AttachmentToken string `json:"attachmentToken"`
	}
	if !response.BindJSON(c, &f) {
		return
	}
	conv, err := h.svc.Conversation(c.Request.Context(), c.Param("no"))
	if err != nil {
		response.Fail(c, err)
		return
	}
	if !h.checkVisitor(c, conv) {
		return
	}
	m, err := h.svc.Send(c.Request.Context(), conv.ConvNo, SendInput{
		SenderType: SenderVisitor, SenderID: f.SenderID, SenderName: f.SenderName,
		Content: f.Content, Attachment: f.AttachmentToken,
	})
	respond(c, m, err)
}

// POST /plugins/im/visitor/conversations/:no/read  {"seq":12} —— 访客已读上报。
func (h *handler) visitorRead(c *gin.Context) {
	var f struct {
		Seq int64 `json:"seq"`
	}
	if !response.BindJSON(c, &f) {
		return
	}
	conv, err := h.svc.Conversation(c.Request.Context(), c.Param("no"))
	if err != nil {
		response.Fail(c, err)
		return
	}
	if !h.checkVisitor(c, conv) {
		return
	}
	respond(c, gin.H{"ok": true}, h.svc.SetVisitorRead(c.Request.Context(), conv.ConvNo, f.Seq))
}

// POST /plugins/im/visitor/conversations/:no/typing —— 访客"正在输入"（不落库）。
func (h *handler) visitorTyping(c *gin.Context) {
	conv, err := h.svc.Conversation(c.Request.Context(), c.Param("no"))
	if err != nil {
		response.Fail(c, err)
		return
	}
	if !h.checkVisitor(c, conv) {
		return
	}
	h.svc.Typing(conv.ConvNo, SenderVisitor)
	response.OK(c, gin.H{"ok": true})
}

// POST /plugins/im/visitor/conversations/:no/rating  {"score":5,"comment":"..."} —— 满意度评价（会话结束后一次性）。
func (h *handler) visitorRate(c *gin.Context) {
	var f struct {
		Score   int    `json:"score" binding:"required"`
		Comment string `json:"comment"`
	}
	if !response.BindJSON(c, &f) {
		return
	}
	conv, err := h.svc.Conversation(c.Request.Context(), c.Param("no"))
	if err != nil {
		response.Fail(c, err)
		return
	}
	if !h.checkVisitor(c, conv) {
		return
	}
	conv, err = h.svc.Rate(c.Request.Context(), conv.ConvNo, f.Score, f.Comment)
	respondConv(c, conv, err)
}

func messageQuery(c *gin.Context) MessageQuery {
	since, _ := strconv.ParseInt(c.Query("since"), 10, 64)
	before, _ := strconv.ParseInt(c.Query("before"), 10, 64)
	limit, _ := strconv.Atoi(c.Query("limit"))
	return MessageQuery{Since: since, Before: before, Limit: limit}
}

// Register 注册坐席与访客路由（由 plugin.go 的 module 调用）。
func (h *handler) Register(r *gin.RouterGroup, d *core.Deps) {
	g := r.Group("/plugins/im")
	g.GET("/conversations", core.Perm("im:list"), h.list)
	g.GET("/conversations/:no/messages", core.Perm("im:list"), h.adminMessages)
	g.POST("/conversations/:no/messages", core.Perm("im:reply"), h.adminSend)
	g.POST("/conversations/:no/close", core.Perm("im:edit"), h.close)
	g.POST("/conversations/:no/claim", core.Perm("im:reply"), h.claim)
	g.POST("/conversations/:no/release", core.Perm("im:reply"), h.release)
	g.PUT("/conversations/:no/tags", core.Perm("im:edit"), h.tags)
	g.POST("/conversations/:no/typing", core.Perm("im:reply"), h.adminTyping)
	g.PUT("/conversations/:no/note", core.Perm("im:edit"), h.note)
	g.POST("/conversations/:no/messages/:seq/recall", core.Perm("im:reply"), h.recall)
	g.POST("/conversations/:no/merge", core.Perm("im:edit"), h.merge)
	g.POST("/presence/heartbeat", core.Perm("im:list"), h.heartbeat)
	g.GET("/stats", core.Perm("im:list"), h.stats)
	// 删除是超管专属：会话含客户隐私与纠纷证据，运营角色不给删除入口；操作记操作日志。
	g.DELETE("/conversations/:no", middleware.RequireSuper(), d.OperLogMiddleware("im", "delete"), h.deleteConv)
	g.POST("/conversations-delete", middleware.RequireSuper(), d.OperLogMiddleware("im", "delete"), h.batchDelete)
	g.GET("/conversations-export", core.Perm("im:list"), h.export)
	g.POST("/conversations/:no/attachments", core.Perm("im:reply"), h.uploadAdmin)
	g.GET("/agents", core.Perm("im:list"), h.agents)
	g.GET("/settings", core.Perm("im:list"), h.getSettings)
	g.PUT("/settings", core.Perm("im:edit"), h.putSettings)
	g.POST("/conversations/:no/transfer", core.Perm("im:edit"), h.transfer)
	g.GET("/quick-replies", core.Perm("im:reply"), h.quickReplyList)
	g.POST("/quick-replies", core.Perm("im:edit"), h.quickReplyCreate)
	g.PUT("/quick-replies/:id", core.Perm("im:edit"), h.quickReplyUpdate)
	g.DELETE("/quick-replies/:id", core.Perm("im:edit"), h.quickReplyDelete)
	g.POST("/stream-ticket", core.Perm("im:list"), h.streamTicket)
	g.GET("/stream/:no", core.Perm("im:list"), h.adminStream)

	// 访客公开接口：限流按 IP；读写与订阅都要带创建会话时的访客令牌（会话号可枚举，不能单独当凭证）。
	// 路径带 /visitor 前缀，与坐席接口分开——同一个 engine 上两组 (protected 与 Public)
	// 不能注册相同的 method+path，否则 gin 在 addRoute 时 panic。
	p := d.Public.Group("/plugins/im/visitor")
	p.GET("/status",
		middleware.RateLimit(d.Cache, d.Log, "im-status", middleware.ByIP(), 120, time.Minute), h.visitorStatus)
	p.POST("/conversations",
		middleware.RateLimit(d.Cache, d.Log, "im-open", middleware.ByIP(), 30, time.Minute), h.open)
	p.GET("/conversations/:no/messages",
		middleware.RateLimit(d.Cache, d.Log, "im-poll", middleware.ByIP(), 120, time.Minute), h.visitorMessages)
	p.POST("/conversations/:no/messages",
		middleware.RateLimit(d.Cache, d.Log, "im-send", middleware.ByIP(), 60, time.Minute), h.visitorSend)
	p.POST("/conversations/:no/read",
		middleware.RateLimit(d.Cache, d.Log, "im-read", middleware.ByIP(), 240, time.Minute), h.visitorRead)
	p.POST("/conversations/:no/typing",
		middleware.RateLimit(d.Cache, d.Log, "im-typing", middleware.ByIP(), 240, time.Minute), h.visitorTyping)
	p.POST("/conversations/:no/rating",
		middleware.RateLimit(d.Cache, d.Log, "im-rating", middleware.ByIP(), 30, time.Minute), h.visitorRate)
	p.POST("/conversations/:no/attachments",
		middleware.RateLimit(d.Cache, d.Log, "im-upload", middleware.ByIP(), 30, time.Minute), h.uploadVisitor)
	p.GET("/attachments/:token",
		middleware.RateLimit(d.Cache, d.Log, "im-att", middleware.ByIP(), 600, time.Minute), h.serveAttachment)
	p.GET("/stream/:no",
		middleware.RateLimit(d.Cache, d.Log, "im-stream", middleware.ByIP(), 60, time.Minute), h.visitorStream)
}

// POST /plugins/im/conversations/:no/attachments —— 坐席上传附件。
func (h *handler) uploadAdmin(c *gin.Context) { h.upload(c, SenderAgent) }

// POST /plugins/im/visitor/conversations/:no/attachments —— 访客上传附件。
func (h *handler) uploadVisitor(c *gin.Context) { h.upload(c, SenderVisitor) }

// upload 读取 multipart 文件、按内容嗅探类型、落存储并登记附件。
func (h *handler) upload(c *gin.Context, uploader string) {
	if h.store == nil {
		response.Fail(c, response.Unavailable("im.attachment_unavailable"))
		return
	}
	conv, err := h.svc.Conversation(c.Request.Context(), c.Param("no"))
	if err != nil {
		response.Fail(c, err)
		return
	}
	if uploader == SenderVisitor && !h.checkVisitor(c, conv) {
		return
	}
	if conv.Status != StatusOpen {
		response.Fail(c, response.Conflict("im.conv_closed"))
		return
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, maxUploadBytes)
	fh, err := c.FormFile("file")
	if err != nil {
		response.Fail(c, response.BadRequest("im.attachment_required"))
		return
	}
	if fh.Size <= 0 || fh.Size > maxUploadBytes {
		response.Fail(c, response.BadRequest("im.attachment_too_large"))
		return
	}
	f, err := fh.Open()
	if err != nil {
		response.Fail(c, err)
		return
	}
	defer func() { _ = f.Close() }()
	head := make([]byte, 512)
	n, _ := io.ReadFull(f, head)
	mime := strings.TrimSpace(strings.Split(http.DetectContentType(head[:n]), ";")[0])
	rule, ok := attachmentRuleFor(mime, fh.Filename)
	if !ok {
		response.Fail(c, response.BadRequest("im.attachment_type"))
		return
	}
	if fh.Size > rule.max {
		response.Fail(c, response.BadRequest("im.attachment_too_large"))
		return
	}
	token := randomToken(16)
	key := time.Now().UTC().Format("200601") + "/" + token + rule.ext
	body := io.MultiReader(bytes.NewReader(head[:n]), f)
	if err := h.store.Put(c.Request.Context(), key, body, fh.Size, storage.PutOptions{ContentType: mime}); err != nil {
		response.Fail(c, err)
		return
	}
	att, err := h.svc.SaveAttachment(c.Request.Context(), conv.ConvNo, uploader, safeFilename(fh.Filename), mime, fh.Size, key)
	if err != nil {
		_ = h.store.Delete(c.Request.Context(), key)
		response.Fail(c, err)
		return
	}
	response.OK(c, att)
}

// GET /plugins/im/visitor/attachments/:token —— 按不可枚举 token 内联展示/下载附件。
func (h *handler) serveAttachment(c *gin.Context) {
	if h.store == nil {
		response.Fail(c, response.NotFound("im.attachment_invalid"))
		return
	}
	att, err := h.svc.Attachment(c.Request.Context(), c.Param("token"))
	if err != nil {
		response.Fail(c, err)
		return
	}
	obj, err := h.store.Get(c.Request.Context(), att.StorageKey, nil)
	if err != nil {
		response.Fail(c, response.NotFound("im.attachment_invalid"))
		return
	}
	defer func() { _ = obj.Body.Close() }()
	disp := "attachment"
	if strings.HasPrefix(att.Mime, "image/") {
		disp = "inline"
	}
	c.Header("Content-Type", att.Mime)
	c.Header("X-Content-Type-Options", "nosniff")
	c.Header("Content-Security-Policy", "default-src 'none'; sandbox")
	c.Header("Content-Disposition", disp+"; filename*=UTF-8''"+url.PathEscape(att.Name))
	c.Header("Cache-Control", "private, max-age=86400")
	c.DataFromReader(http.StatusOK, obj.Length, att.Mime, obj.Body, nil)
}

// GET /plugins/im/agents —— 可转接的坐席列表。
func (h *handler) agents(c *gin.Context) {
	rows, err := h.svc.Agents(c.Request.Context())
	respond(c, rows, err)
}

// POST /plugins/im/conversations/:no/transfer {"assigneeId":"...","assigneeName":"..."}
func (h *handler) transfer(c *gin.Context) {
	var f struct {
		AssigneeID   string `json:"assigneeId"`
		AssigneeName string `json:"assigneeName"`
	}
	if !response.BindJSON(c, &f) {
		return
	}
	conv, err := h.svc.Transfer(c.Request.Context(), c.Param("no"), f.AssigneeID, f.AssigneeName)
	respondConv(c, conv, err)
}
