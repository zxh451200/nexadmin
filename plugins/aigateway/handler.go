package aigateway

import (
	"strconv"

	"github.com/gin-gonic/gin"

	"nexadmin/core/response"
	"nexadmin/pkg/idgen"
)

// 路由前缀：/api/v1/plugins/aigateway。权限码：aigateway:list 查看；
// aigateway:edit 模型配置维护与连通性测试。用量记录只走进程内 API（宿主调用），
// 不开 HTTP 写入口。

type handler struct{ svc *Service }

func respond(c *gin.Context, data any, err error) {
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, data)
}

func idParam(c *gin.Context) (idgen.ID, bool) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id == 0 {
		response.Fail(c, response.BadRequest("error.bad_request"))
		return 0, false
	}
	return idgen.ID(id), true
}

type modelForm struct {
	Kind      string `json:"kind"`
	Name      string `json:"name"`
	Provider  string `json:"provider"`
	BaseURL   string `json:"baseUrl"`
	Model     string `json:"model"`
	APIKey    string `json:"apiKey"` // 空或打码值 = 保持原密钥
	Dimension int    `json:"dimension"`
	Enabled   bool   `json:"enabled"`
	IsDefault bool   `json:"isDefault"`
}

func (f modelForm) input() ModelInput {
	return ModelInput{
		Kind: f.Kind, Name: f.Name, Provider: f.Provider, BaseURL: f.BaseURL,
		Model: f.Model, APIKey: f.APIKey, Dimension: f.Dimension,
		Enabled: f.Enabled, IsDefault: f.IsDefault,
	}
}

// GET /plugins/aigateway/models?keyword=&kind=&page=&pageSize=
func (h *handler) listModels(c *gin.Context) {
	page, size := response.ParsePage(c)
	rows, total, err := h.svc.ListModels(c.Request.Context(), c.Query("keyword"), c.Query("kind"), page, size)
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.Page(c, rows, total, page, size)
}

// GET /plugins/aigateway/models/:id
func (h *handler) getModel(c *gin.Context) {
	id, ok := idParam(c)
	if !ok {
		return
	}
	m, err := h.svc.GetModel(c.Request.Context(), id)
	respond(c, m, err)
}

// POST /plugins/aigateway/models —— upsert（id 为 0 即新建）
func (h *handler) saveModel(c *gin.Context) {
	var f modelForm
	if !response.BindJSON(c, &f) {
		return
	}
	var id idgen.ID
	if v, err := strconv.ParseInt(c.Param("id"), 10, 64); err == nil {
		id = idgen.ID(v)
	}
	m, err := h.svc.SaveModel(c.Request.Context(), id, f.input())
	respond(c, m, err)
}

// DELETE /plugins/aigateway/models/:id
func (h *handler) deleteModel(c *gin.Context) {
	id, ok := idParam(c)
	if !ok {
		return
	}
	respond(c, nil, h.svc.DeleteModel(c.Request.Context(), id))
}

// POST /plugins/aigateway/models/:id/ping —— 连通性测试（SSRF 三层防线内）。
func (h *handler) pingModel(c *gin.Context) {
	id, ok := idParam(c)
	if !ok {
		return
	}
	res, err := h.svc.Ping(c.Request.Context(), id)
	respond(c, res, err)
}

// GET /plugins/aigateway/usage?kind=&bizType=&page=&pageSize=
func (h *handler) listUsage(c *gin.Context) {
	page, size := response.ParsePage(c)
	rows, total, err := h.svc.ListUsage(c.Request.Context(), UsageQuery{
		Kind: c.Query("kind"), BizType: c.Query("bizType"), Page: page, PageSize: size,
	})
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.Page(c, rows, total, page, size)
}

// GET /plugins/aigateway/usage/summary —— 按 kind 聚合 token 用量。
func (h *handler) usageSummary(c *gin.Context) {
	rows, err := h.svc.UsageSummary(c.Request.Context())
	respond(c, rows, err)
}
