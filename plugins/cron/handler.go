package cron

import (
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	"nexadmin/core/response"
	"nexadmin/pkg/idgen"
)

// 路径前缀：/api/v1/plugins/cron（core.Module.Register 收到的 r 已是 /api/v1）。
// 权限码：cron:list 查看任务与处理器；cron:edit 增删改与启停；cron:run 立即执行；cron:log 看执行日志；cron:clean 清理日志。

type handler struct{ svc *Service }

func paramID(c *gin.Context) (idgen.ID, bool) {
	id, err := idgen.Parse(c.Param("id"))
	if err != nil || id == 0 {
		response.Fail(c, response.BadRequest("error.bad_request"))
		return 0, false
	}
	return id, true
}

func respond(c *gin.Context, data any, err error) {
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, data)
}

// GET /plugins/cron/handlers
func (h *handler) handlers(c *gin.Context) {
	type item struct {
		Name        string `json:"name"`
		Description string `json:"description"`
	}
	specs := Handlers()
	out := make([]item, len(specs))
	for i, s := range specs {
		out[i] = item{s.Name, s.Description}
	}
	response.OK(c, out)
}

// GET /plugins/cron/jobs?keyword=&handler=&enabled=&page=&pageSize=
func (h *handler) listJobs(c *gin.Context) {
	page, size := response.ParsePage(c)
	list, total, err := h.svc.ListJobs(c.Request.Context(), JobQuery{
		Keyword: strings.TrimSpace(c.Query("keyword")), Handler: c.Query("handler"), Enabled: c.Query("enabled"),
		Page: page, PageSize: size,
	})
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.Page(c, list, total, page, size)
}

func (h *handler) save(c *gin.Context, id idgen.ID) {
	var f JobForm
	if !response.BindJSON(c, &f) {
		return
	}
	v, err := h.svc.SaveJob(c.Request.Context(), id, f)
	respond(c, v, err)
}

// POST /plugins/cron/jobs
func (h *handler) createJob(c *gin.Context) { h.save(c, 0) }

// PUT /plugins/cron/jobs/:id
func (h *handler) updateJob(c *gin.Context) {
	if id, ok := paramID(c); ok {
		h.save(c, id)
	}
}

// PUT /plugins/cron/jobs/:id/enabled  {"enabled":true}
func (h *handler) setEnabled(c *gin.Context) {
	id, ok := paramID(c)
	if !ok {
		return
	}
	var body struct {
		Enabled bool `json:"enabled"`
	}
	if !response.BindJSON(c, &body) {
		return
	}
	v, err := h.svc.SetEnabled(c.Request.Context(), id, body.Enabled)
	respond(c, v, err)
}

// DELETE /plugins/cron/jobs/:id
func (h *handler) deleteJob(c *gin.Context) {
	if id, ok := paramID(c); ok {
		respond(c, nil, h.svc.DeleteJob(c.Request.Context(), id))
	}
}

// POST /plugins/cron/jobs/:id/run
func (h *handler) runJob(c *gin.Context) {
	if id, ok := paramID(c); ok {
		row, err := h.svc.RunNow(c.Request.Context(), id)
		respond(c, row, err)
	}
}

// GET /plugins/cron/logs?jobId=&status=&page=&pageSize=
func (h *handler) listLogs(c *gin.Context) {
	page, size := response.ParsePage(c)
	list, total, err := h.svc.ListLogs(c.Request.Context(), LogQuery{
		JobID: c.Query("jobId"), Status: c.Query("status"), Page: page, PageSize: size,
	})
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.Page(c, list, total, page, size)
}

// GET /plugins/cron/logs/:id（完整输出与错误）
func (h *handler) getLog(c *gin.Context) {
	if id, ok := paramID(c); ok {
		v, err := h.svc.GetLog(c.Request.Context(), id)
		respond(c, v, err)
	}
}

// DELETE /plugins/cron/logs?days=30&jobId=  （days=0 清理全部已结束的日志）
func (h *handler) cleanLogs(c *gin.Context) {
	days, err := strconv.Atoi(c.DefaultQuery("days", "0"))
	if err != nil || days < 0 || days > 3650 {
		response.Fail(c, response.BadRequest("error.bad_request"))
		return
	}
	var jobID idgen.ID
	if s := c.Query("jobId"); s != "" {
		if jobID, err = idgen.Parse(s); err != nil {
			response.Fail(c, response.BadRequest("error.bad_request"))
			return
		}
	}
	n, err := h.svc.CleanLogs(c.Request.Context(), days, jobID)
	respond(c, gin.H{"deleted": n}, err)
}
