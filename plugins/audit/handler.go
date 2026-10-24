package audit

import (
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	"nexadmin/core/auth"
	"nexadmin/core/response"
	"nexadmin/pkg/idgen"
)

// 路径前缀：/api/v1/plugins/audit。
// 权限码：audit:list 查看变更记录；audit:config 设置审计范围；audit:clean 清理记录。

type handler struct{ svc *Service }

func respond(c *gin.Context, data any, err error) {
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, data)
}

func username(c *gin.Context) string {
	if cl := auth.ClaimsFrom(c); cl != nil {
		return cl.Username
	}
	return ""
}

// operatorMiddleware 把当前登录用户写进 request context，供 GORM 回调记录操作人。
// 只对在它之后注册的路由生效（见 plugin.go 的说明）。
func operatorMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		if cl := auth.ClaimsFrom(c); cl != nil {
			c.Request = c.Request.WithContext(WithOperator(c.Request.Context(), int64(cl.UserID), cl.Username))
		}
		c.Next()
	}
}

// GET /plugins/audit/logs?table=&operator=&action=&from=&to=&page=&pageSize=
func (h *handler) listLogs(c *gin.Context) {
	page, size := response.ParsePage(c)
	action := c.Query("action")
	if action != "" && action != ActionCreate && action != ActionUpdate && action != ActionDelete {
		response.Fail(c, response.BadRequest("error.bad_request"))
		return
	}
	list, total, err := h.svc.ListLogs(c.Request.Context(), LogQuery{
		Table: strings.TrimSpace(c.Query("table")), Operator: strings.TrimSpace(c.Query("operator")), Action: action,
		From: c.Query("from"), To: c.Query("to"), Page: page, PageSize: size,
	})
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.Page(c, list, total, page, size)
}

// GET /plugins/audit/logs/:id
func (h *handler) getLog(c *gin.Context) {
	id, err := idgen.Parse(c.Param("id"))
	if err != nil || id == 0 {
		response.Fail(c, response.BadRequest("error.bad_request"))
		return
	}
	v, err := h.svc.GetLog(c.Request.Context(), id)
	respond(c, v, err)
}

// GET /plugins/audit/log-tables
func (h *handler) logTables(c *gin.Context) {
	v, err := h.svc.LogTables(c.Request.Context())
	respond(c, v, err)
}

// GET /plugins/audit/config
func (h *handler) getConfig(c *gin.Context) {
	v, err := h.svc.GetConfig(c.Request.Context())
	respond(c, v, err)
}

// PUT /plugins/audit/config  {"tables":["sys_user", ...]}
func (h *handler) saveConfig(c *gin.Context) {
	var body struct {
		Tables []string `json:"tables"`
	}
	if !response.BindJSON(c, &body) {
		return
	}
	if len(body.Tables) > 500 {
		response.Fail(c, response.BadRequest("error.bad_request"))
		return
	}
	v, err := h.svc.SaveConfig(c.Request.Context(), body.Tables, username(c))
	respond(c, v, err)
}

// DELETE /plugins/audit/logs?days=90  （保留最近 days 天，days 1-3650）
func (h *handler) clean(c *gin.Context) {
	days, err := strconv.Atoi(c.Query("days"))
	if err != nil || days < 1 || days > 3650 {
		response.Fail(c, response.BadRequest("error.bad_request"))
		return
	}
	n, err := h.svc.Clean(c.Request.Context(), days, username(c))
	respond(c, gin.H{"deleted": n}, err)
}
