package online

import (
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"nexadmin/core/response"
	"nexadmin/modules/internal/session"
	"nexadmin/pkg/idgen"
)

// Handler 是在线用户的 HTTP 层。
type Handler struct{ store *session.Store }

// item 与前端 OnlineUser 一致。部门名拿不到（会话里只有令牌信息），省略该字段。
type item struct {
	ID           string    `json:"id"`
	Username     string    `json:"username"`
	Nickname     string    `json:"nickname"`
	Device       string    `json:"device"`
	Browser      string    `json:"browser,omitempty"`
	OS           string    `json:"os,omitempty"`
	IP           string    `json:"ip"`
	LoginAt      time.Time `json:"loginAt"`
	LastActiveAt time.Time `json:"lastActiveAt"`
}

// List GET /online-users?keyword=&page=&pageSize=
func (h *Handler) List(c *gin.Context) {
	page, size := response.ParsePage(c)
	all, err := h.store.List(c.Request.Context(), 0)
	if err != nil {
		response.Fail(c, err)
		return
	}
	kw := strings.ToLower(strings.TrimSpace(c.Query("keyword")))
	rows := make([]item, 0, len(all))
	for _, s := range all {
		if kw != "" && !strings.Contains(strings.ToLower(s.Username+" "+s.Nickname+" "+s.IP), kw) {
			continue
		}
		rows = append(rows, item{ID: s.ID, Username: s.Username, Nickname: s.Nickname, Device: s.Device,
			Browser: s.Browser, OS: s.OS, IP: s.IP, LoginAt: s.LoginAt, LastActiveAt: s.LastActiveAt})
	}
	total := int64(len(rows))
	from := min((page-1)*size, len(rows))
	to := min(from+size, len(rows))
	response.Page(c, rows[from:to], total, page, size)
}

// Kick DELETE /online-users/:id：强制下线（吊销该会话最近一次见到的 access / refresh 令牌）。
func (h *Handler) Kick(c *gin.Context) {
	sid := c.Param("id")
	if _, err := idgen.Parse(sid); err != nil {
		response.Fail(c, response.BadRequest("error.bad_request"))
		return
	}
	if sid == session.Current(c) {
		response.Fail(c, response.BadRequest("error.bad_request").WithMsg("不能强制下线当前会话，请直接退出登录"))
		return
	}
	_, found, err := h.store.Kick(c.Request.Context(), sid, 0)
	if err != nil {
		response.Fail(c, err)
		return
	}
	if !found {
		response.Fail(c, response.NotFound("error.not_found"))
		return
	}
	response.OK(c, nil)
}
