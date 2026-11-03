package realname

import (
	"github.com/gin-gonic/gin"

	"nexadmin/core/auth"
	"nexadmin/core/datascope"
	"nexadmin/core/response"
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

// GET /plugins/realname/verifications?keyword=&status=&page=&pageSize=
func (h *handler) list(c *gin.Context) {
	sc, err := h.scopeOf(c)
	if err != nil {
		response.Fail(c, err)
		return
	}
	page, size := response.ParsePage(c)
	rows, total, err := h.svc.ListVerifications(c.Request.Context(), ListQuery{
		Keyword: c.Query("keyword"), Status: c.Query("status"),
		Scope: sc, Latest: c.Query("all") != "1", Page: page, PageSize: size,
	})
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.Page(c, rows, total, page, size)
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
