package formbuilder

import (
	"bytes"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"nexadmin/core/auth"
	"nexadmin/core/response"
	"nexadmin/pkg/idgen"
)

// 路径前缀：/api/v1/plugins/formbuilder。权限码：
//
//	formbuilder:design     建表单、改草稿、删除、导入导出定义、看版本
//	formbuilder:publish    发布、下线、上线
//	formbuilder:fill       填写（还受表单的可见范围限制）
//	formbuilder:submission 看提交列表与详情、删除提交
//	formbuilder:export     导出 xlsx
type handler struct{ svc *Service }

func paramID(c *gin.Context, name string) (idgen.ID, bool) {
	id, err := idgen.Parse(c.Param(name))
	if err != nil || id <= 0 {
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

func limitBody(c *gin.Context) {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, MaxRequestBytes)
}

// actorOf 取当前登录用户。开放平台的应用令牌没有用户身份，不能用来填写或留下操作者记录。
func actorOf(c *gin.Context) (Actor, bool) {
	cl := auth.ClaimsFrom(c)
	if cl == nil || cl.UserID == 0 {
		response.Fail(c, forbidden("需要用户身份"))
		return Actor{}, false
	}
	return Actor{ID: cl.UserID, Username: cl.Username, Super: cl.Super}, true
}

func (h *handler) ctxActor(c *gin.Context) Actor {
	if cl := auth.ClaimsFrom(c); cl != nil {
		return Actor{ID: cl.UserID, Username: cl.Username, Super: cl.Super}
	}
	return Actor{}
}

// ---- 设计 ----

// GET /forms?keyword=&status=&page=&pageSize=
func (h *handler) listForms(c *gin.Context) {
	page, size := response.ParsePage(c)
	list, total, err := h.svc.ListForms(c.Request.Context(), FormQuery{
		Keyword: strings.TrimSpace(c.Query("keyword")), Status: c.Query("status"), Page: page, PageSize: size,
	})
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.Page(c, list, total, page, size)
}

// POST /forms {name, description}
func (h *handler) createForm(c *gin.Context) {
	limitBody(c)
	var in struct {
		Name        string `json:"name"`
		Description string `json:"description"`
	}
	if !response.BindJSON(c, &in) {
		return
	}
	v, err := h.svc.CreateForm(c.Request.Context(), h.ctxActor(c), strings.TrimSpace(in.Name), in.Description)
	respond(c, v, err)
}

// GET /forms/:id
func (h *handler) getForm(c *gin.Context) {
	if id, ok := paramID(c, "id"); ok {
		v, err := h.svc.GetForm(c.Request.Context(), id)
		respond(c, v, err)
	}
}

// PUT /forms/:id {definition, audience}
func (h *handler) saveForm(c *gin.Context) {
	id, ok := paramID(c, "id")
	if !ok {
		return
	}
	limitBody(c)
	var in SaveInput
	if !response.BindJSON(c, &in) {
		return
	}
	v, err := h.svc.SaveDraft(c.Request.Context(), id, in)
	respond(c, v, err)
}

// DELETE /forms/:id
func (h *handler) deleteForm(c *gin.Context) {
	if id, ok := paramID(c, "id"); ok {
		respond(c, nil, h.svc.DeleteForm(c.Request.Context(), id))
	}
}

// POST /forms/import  请求体 = 导出的定义文件
func (h *handler) importForm(c *gin.Context) {
	limitBody(c)
	raw, err := io.ReadAll(c.Request.Body)
	if err != nil {
		response.Fail(c, bad("请求体过大或读取失败"))
		return
	}
	v, err := h.svc.ImportDefinition(c.Request.Context(), h.ctxActor(c), raw)
	respond(c, v, err)
}

// GET /forms/:id/definition?version=  导出定义文件（version 缺省 = 草稿）
func (h *handler) exportDefinition(c *gin.Context) {
	id, ok := paramID(c, "id")
	if !ok {
		return
	}
	ver, _ := strconv.Atoi(c.Query("version"))
	env, err := h.svc.ExportDefinition(c.Request.Context(), id, max(ver, 0))
	respond(c, env, err)
}

// GET /forms/:id/versions
func (h *handler) listVersions(c *gin.Context) {
	if id, ok := paramID(c, "id"); ok {
		v, err := h.svc.ListVersions(c.Request.Context(), id)
		respond(c, v, err)
	}
}

// GET /forms/:id/versions/:v
func (h *handler) getVersion(c *gin.Context) {
	id, ok := paramID(c, "id")
	if !ok {
		return
	}
	ver, err := strconv.Atoi(c.Param("v"))
	if err != nil || ver < 1 {
		response.Fail(c, response.BadRequest("error.bad_request"))
		return
	}
	v, err := h.svc.GetVersionDefinition(c.Request.Context(), id, ver)
	respond(c, v, err)
}

// GET /roles、GET /users?keyword=  可见范围选择器的候选项
func (h *handler) roles(c *gin.Context) {
	v, err := h.svc.Roles(c.Request.Context())
	respond(c, v, err)
}

func (h *handler) users(c *gin.Context) {
	v, err := h.svc.Users(c.Request.Context(), strings.TrimSpace(c.Query("keyword")))
	respond(c, v, err)
}

// ---- 发布 ----

// POST /forms/:id/publish
func (h *handler) publish(c *gin.Context) {
	if id, ok := paramID(c, "id"); ok {
		v, err := h.svc.Publish(c.Request.Context(), h.ctxActor(c), id)
		respond(c, v, err)
	}
}

func (h *handler) setOnline(online bool) gin.HandlerFunc {
	return func(c *gin.Context) {
		if id, ok := paramID(c, "id"); ok {
			v, err := h.svc.SetOnline(c.Request.Context(), id, online)
			respond(c, v, err)
		}
	}
}

// ---- 填写 ----

// GET /fill/forms
func (h *handler) fillList(c *gin.Context) {
	actor, ok := actorOf(c)
	if !ok {
		return
	}
	v, err := h.svc.FillList(c.Request.Context(), actor)
	respond(c, v, err)
}

// GET /fill/stats  工作台卡片：待填表单数
func (h *handler) fillStats(c *gin.Context) {
	actor, ok := actorOf(c)
	if !ok {
		return
	}
	n, err := h.svc.PendingCount(c.Request.Context(), actor)
	respond(c, gin.H{"pending": n}, err)
}

// GET /fill/forms/:id
func (h *handler) fillGet(c *gin.Context) {
	actor, ok := actorOf(c)
	if !ok {
		return
	}
	if id, ok := paramID(c, "id"); ok {
		v, err := h.svc.FillGet(c.Request.Context(), actor, id)
		respond(c, v, err)
	}
}

// POST /fill/forms/:id/submit {data, idemKey}
func (h *handler) submit(c *gin.Context) {
	actor, ok := actorOf(c)
	if !ok {
		return
	}
	id, ok := paramID(c, "id")
	if !ok {
		return
	}
	limitBody(c)
	var in SubmitInput
	if !response.BindJSON(c, &in) {
		return
	}
	v, err := h.svc.Submit(c.Request.Context(), actor, id, in)
	respond(c, v, err)
}

// ---- 收集 ----

func parseTime(s string, endOfDay bool) *time.Time {
	if s == "" {
		return nil
	}
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return &t
	}
	if t, err := time.Parse("2006-01-02", s); err == nil {
		if endOfDay {
			t = t.Add(24*time.Hour - time.Millisecond)
		}
		return &t
	}
	return nil
}

func subQuery(c *gin.Context, formID idgen.ID) SubQuery {
	ver, _ := strconv.Atoi(c.Query("version"))
	return SubQuery{
		FormID: formID, Version: max(ver, 0), Username: strings.TrimSpace(c.Query("username")),
		Field: c.Query("field"), Value: strings.TrimSpace(c.Query("value")),
		From: parseTime(c.Query("from"), false), To: parseTime(c.Query("to"), true),
	}
}

// GET /forms/:id/submission-meta
func (h *handler) subMeta(c *gin.Context) {
	if id, ok := paramID(c, "id"); ok {
		v, err := h.svc.SubMeta(c.Request.Context(), id)
		respond(c, v, err)
	}
}

// GET /forms/:id/submissions?version=&username=&field=&value=&from=&to=&page=&pageSize=
func (h *handler) listSubmissions(c *gin.Context) {
	id, ok := paramID(c, "id")
	if !ok {
		return
	}
	q := subQuery(c, id)
	q.Page, q.PageSize = response.ParsePage(c)
	v, err := h.svc.ListSubmissions(c.Request.Context(), q)
	respond(c, v, err)
}

// GET /forms/:id/submissions/:sid
func (h *handler) getSubmission(c *gin.Context) {
	id, ok := paramID(c, "id")
	if !ok {
		return
	}
	if sid, ok := paramID(c, "sid"); ok {
		v, err := h.svc.GetSubmission(c.Request.Context(), id, sid)
		respond(c, v, err)
	}
}

// DELETE /forms/:id/submissions/:sid
func (h *handler) deleteSubmission(c *gin.Context) {
	id, ok := paramID(c, "id")
	if !ok {
		return
	}
	if sid, ok := paramID(c, "sid"); ok {
		respond(c, nil, h.svc.DeleteSubmission(c.Request.Context(), id, sid))
	}
}

const xlsxContentType = "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet"

func langOf(c *gin.Context) string {
	if strings.HasPrefix(strings.ToLower(c.GetHeader("Accept-Language")), "en") {
		return "en-US"
	}
	return "zh-CN"
}

// GET /forms/:id/export  同步导出（最多 MaxSyncExport 行，更多请用 export-job）
func (h *handler) exportSync(c *gin.Context) {
	id, ok := paramID(c, "id")
	if !ok {
		return
	}
	var buf bytes.Buffer
	if _, err := h.svc.WriteExport(c.Request.Context(), &buf, subQuery(c, id), MaxSyncExport, langOf(c)); err != nil {
		response.Fail(c, err)
		return
	}
	c.Header("Content-Disposition", `attachment; filename="formbuilder-`+id.String()+`.xlsx"`)
	c.Data(http.StatusOK, xlsxContentType, buf.Bytes())
}

// POST /forms/:id/export-job  提交到导出中心，返回任务 ID
func (h *handler) exportJobSubmit(c *gin.Context) {
	actor, ok := actorOf(c)
	if !ok {
		return
	}
	id, ok := paramID(c, "id")
	if !ok {
		return
	}
	v, err := h.svc.SubmitExportJob(c.Request.Context(), actor, subQuery(c, id), langOf(c))
	respond(c, v, err)
}
