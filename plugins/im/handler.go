package im

import (
	"strconv"
	"time"

	"github.com/gin-gonic/gin"

	"nexadmin/core"
	"nexadmin/core/auth"
	"nexadmin/core/middleware"
	"nexadmin/core/response"
)

// 路由前缀：/api/v1/plugins/im。权限码：im:list 查看会话与消息；im:reply 回复；im:edit 关闭会话。
// 访客公开接口挂在 d.Public（无登录），靠 IP 限流 + 不可枚举的会话号兜底。

type handler struct {
	svc     *Service
	tickets *streamTicket
}

func respond(c *gin.Context, data any, err error) {
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, data)
}

func convView(c *Conversation) ConversationView {
	return ConversationView{Conversation: *c}
}

// ---- 坐席端 ----

// GET /plugins/im/conversations?status=&bizType=&keyword=&page=&pageSize=
func (h *handler) list(c *gin.Context) {
	page, size := response.ParsePage(c)
	list, total, err := h.svc.ListConversations(c.Request.Context(), ConversationQuery{
		Status: c.Query("status"), BizType: c.Query("bizType"), Keyword: c.Query("keyword"),
		Page: page, PageSize: size,
	})
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.Page(c, list, total, page, size)
}

// GET /plugins/im/conversations/:no/messages?since=0&limit=50 —— 坐席拉取并推进已读游标。
func (h *handler) adminMessages(c *gin.Context) {
	conv, rows, err := h.svc.Messages(c.Request.Context(), c.Param("no"), parseSince(c), parseLimit(c))
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
	response.OK(c, gin.H{"conversation": convView(conv), "messages": rows})
}

// POST /plugins/im/conversations/:no/messages  {"content":"..."} —— 坐席回复。
func (h *handler) adminSend(c *gin.Context) {
	var f struct {
		Content string `json:"content" binding:"required"`
	}
	if !response.BindJSON(c, &f) {
		return
	}
	senderID, senderName := "", "agent"
	if cl := auth.ClaimsFrom(c); cl != nil {
		senderID = strconv.FormatInt(int64(cl.UserID), 10)
		if cl.Username != "" {
			senderName = cl.Username
		}
	}
	m, err := h.svc.Send(c.Request.Context(), c.Param("no"), SendInput{
		SenderType: SenderAgent, SenderID: senderID, SenderName: senderName, Content: f.Content,
	})
	respond(c, m, err)
}

// POST /plugins/im/conversations/:no/close
func (h *handler) close(c *gin.Context) {
	conv, err := h.svc.Close(c.Request.Context(), c.Param("no"))
	respond(c, convView(conv), err)
}

// ---- 访客公开端 ----

// POST /plugins/im/conversations  {"visitorToken":"...","visitorName":"","bizType":"","bizNo":"","subject":""}
func (h *handler) open(c *gin.Context) {
	var f OpenInput
	if !response.BindJSON(c, &f) {
		return
	}
	conv, err := h.svc.Open(c.Request.Context(), f)
	respond(c, convView(conv), err)
}

// GET /plugins/im/conversations/:no/messages?since=0&limit=50 —— 访客轮询。
func (h *handler) visitorMessages(c *gin.Context) {
	conv, rows, err := h.svc.Messages(c.Request.Context(), c.Param("no"), parseSince(c), parseLimit(c))
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, gin.H{"conversation": convView(conv), "messages": rows})
}

// POST /plugins/im/conversations/:no/messages  {"senderId":"...","senderName":"...","content":"..."}
func (h *handler) visitorSend(c *gin.Context) {
	var f struct {
		SenderID   string `json:"senderId"`
		SenderName string `json:"senderName"`
		Content    string `json:"content" binding:"required"`
	}
	if !response.BindJSON(c, &f) {
		return
	}
	m, err := h.svc.Send(c.Request.Context(), c.Param("no"), SendInput{
		SenderType: SenderVisitor, SenderID: f.SenderID, SenderName: f.SenderName, Content: f.Content,
	})
	respond(c, m, err)
}

func parseSince(c *gin.Context) int64 {
	since, _ := strconv.ParseInt(c.Query("since"), 10, 64)
	return since
}

func parseLimit(c *gin.Context) int {
	limit, _ := strconv.Atoi(c.Query("limit"))
	return limit
}

// Register 注册坐席与访客路由（由 plugin.go 的 module 调用）。
func (h *handler) Register(r *gin.RouterGroup, d *core.Deps) {
	g := r.Group("/plugins/im")
	g.GET("/conversations", core.Perm("im:list"), h.list)
	g.GET("/conversations/:no/messages", core.Perm("im:list"), h.adminMessages)
	g.POST("/conversations/:no/messages", core.Perm("im:reply"), h.adminSend)
	g.POST("/conversations/:no/close", core.Perm("im:edit"), h.close)
	g.POST("/stream-ticket", core.Perm("im:list"), h.streamTicket)
	g.GET("/stream/:no", core.Perm("im:list"), h.adminStream)

	// 访客公开接口：限流按 IP；会话号是不可枚举的雪花 ID，充当能力令牌。
	// 路径带 /visitor 前缀，与坐席接口分开——同一个 engine 上两组 (protected 与 Public)
	// 不能注册相同的 method+path，否则 gin 在 addRoute 时 panic。
	p := d.Public.Group("/plugins/im/visitor")
	p.POST("/conversations",
		middleware.RateLimit(d.Cache, d.Log, "im-open", middleware.ByIP(), 30, time.Minute), h.open)
	p.GET("/conversations/:no/messages",
		middleware.RateLimit(d.Cache, d.Log, "im-poll", middleware.ByIP(), 120, time.Minute), h.visitorMessages)
	p.POST("/conversations/:no/messages",
		middleware.RateLimit(d.Cache, d.Log, "im-send", middleware.ByIP(), 60, time.Minute), h.visitorSend)
	p.GET("/stream/:no",
		middleware.RateLimit(d.Cache, d.Log, "im-stream", middleware.ByIP(), 60, time.Minute), h.visitorStream)
}
