package realname

import (
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"nexadmin/core/auth"
	"nexadmin/core/datascope"
	"nexadmin/core/response"
	"nexadmin/pkg/idgen"
)

// 路由前缀 /api/v1/plugins/realname。权限码：realname:list 查看与解析、
// realname:verify 发起核验、realname:config 配置在线核验。

type handler struct{ svc *Service }

func respond(c *gin.Context, data any, err error) {
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, data)
}

type parseForm struct {
	IdType string `json:"idType"`
	Number string `json:"number"`
}

// POST /plugins/realname/parse —— 校验并解析号码，不落库、不出网。
func (h *handler) parse(c *gin.Context) {
	var f parseForm
	if !response.BindJSON(c, &f) {
		return
	}
	v, err := h.svc.Parse(f.IdType, f.Number)
	respond(c, v, err)
}

type verifyForm struct {
	IdType string `json:"idType"`
	Number string `json:"number"`
	Name   string `json:"name"`
	Remark string `json:"remark"`
}

// POST /plugins/realname/verify —— 校验号码并尝试在线核验，结论写台账。
func (h *handler) verify(c *gin.Context) {
	var f verifyForm
	if !response.BindJSON(c, &f) {
		return
	}
	res, err := h.svc.Verify(c.Request.Context(), VerifyInput{
		IdType: f.IdType, Number: f.Number, Name: f.Name, Remark: f.Remark,
	})
	respond(c, res, err)
}

// GET /plugins/realname/verifications?keyword=&status=&from=&to=&page=&pageSize=
func (h *handler) list(c *gin.Context) {
	sc, err := h.scopeOf(c)
	if err != nil {
		response.Fail(c, err)
		return
	}
	page, size := response.ParsePage(c)
	q, err := listQueryFrom(c, sc)
	if err != nil {
		response.Fail(c, err)
		return
	}
	q.Page, q.PageSize = page, size
	rows, total, err := h.svc.ListVerifications(c.Request.Context(), *q)
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.Page(c, rows, total, page, size)
}

// GET /plugins/realname/verifications/export —— 按当前筛选导出台账 xlsx（与列表同筛选、同数据范围）。
func (h *handler) exportVerifications(c *gin.Context) {
	sc, err := h.scopeOf(c)
	if err != nil {
		response.Fail(c, err)
		return
	}
	q, err := listQueryFrom(c, sc)
	if err != nil {
		response.Fail(c, err)
		return
	}
	rows, err := h.svc.ExportVerifications(c.Request.Context(), *q)
	if err != nil {
		response.Fail(c, err)
		return
	}
	c.Header("Content-Disposition", `attachment; filename="realname-`+time.Now().Format("20060102150405")+`.xlsx"`)
	c.Header("Content-Type", "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet")
	if err := h.svc.WriteVerificationsXLSX(c.Request.Context(), c.Writer, rows); err != nil {
		response.Fail(c, err)
	}
}

// listQueryFrom 汇总列表/导出共用的筛选参数：keyword、status、from/to（yyyy-MM-dd，左闭右开）。
func listQueryFrom(c *gin.Context, sc *datascope.Scope) (*ListQuery, error) {
	from, err := parseDay(c.Query("from"))
	if err != nil {
		return nil, err
	}
	to, err := parseDay(c.Query("to"))
	if err != nil {
		return nil, err
	}
	if !to.IsZero() {
		to = to.AddDate(0, 0, 1) // 右端开区间到次日零点，含 to 当天
	}
	return &ListQuery{
		Keyword: c.Query("keyword"), Status: c.Query("status"),
		Scope: sc, Latest: c.Query("all") != "1",
		From: from, To: to,
	}, nil
}

// parseDay 解析 yyyy-MM-dd（UTC 零点）；空串 = 不限，格式不对直接 400。
func parseDay(s string) (time.Time, error) {
	if s == "" {
		return time.Time{}, nil
	}
	t, err := time.Parse("2006-01-02", s)
	if err != nil {
		return time.Time{}, response.BadRequest("error.bad_request")
	}
	return t, nil
}

// idsParam 解析 ?ids=1,2 形式的批量 id，与日志模块的批量删除保持一致。
func idsParam(c *gin.Context) ([]idgen.ID, bool) {
	var ids []idgen.ID
	for _, s := range strings.Split(c.Query("ids"), ",") {
		if s = strings.TrimSpace(s); s == "" {
			continue
		}
		id, err := idgen.Parse(s)
		if err != nil {
			response.Fail(c, response.BadRequest("error.bad_request"))
			return nil, false
		}
		ids = append(ids, id)
	}
	if len(ids) == 0 {
		response.Fail(c, response.BadRequest("error.bad_request"))
		return nil, false
	}
	return ids, true
}

// DELETE /plugins/realname/verifications?ids=1,2
func (h *handler) deleteVerifications(c *gin.Context) {
	ids, ok := idsParam(c)
	if !ok {
		return
	}
	sc, err := h.scopeOf(c)
	if err != nil {
		response.Fail(c, err)
		return
	}
	n, err := h.svc.DeleteVerifications(c.Request.Context(), ids, sc)
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, gin.H{"deleted": n})
}

// scopeOf 解析当前请求者的数据范围；无身份 401，解析失败即整请求失败（不放宽成"看全部"）。
func (h *handler) scopeOf(c *gin.Context) (*datascope.Scope, error) {
	cl := auth.ClaimsFrom(c)
	if cl == nil {
		return nil, response.Unauthorized("error.unauthorized")
	}
	return datascope.FromClaims(c.Request.Context(), h.svc.db, cl)
}

// GET /plugins/realname/config
func (h *handler) getConfig(c *gin.Context) {
	v, err := h.svc.GetConfig(c.Request.Context())
	respond(c, v, err)
}

// PUT /plugins/realname/config
func (h *handler) putConfig(c *gin.Context) {
	var in ConfigInput
	if !response.BindJSON(c, &in) {
		return
	}
	v, err := h.svc.SaveConfig(c.Request.Context(), in)
	respond(c, v, err)
}
