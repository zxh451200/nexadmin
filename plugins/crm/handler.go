package crm

import (
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"nexadmin/core/auth"
	"nexadmin/core/datascope"
	"nexadmin/core/region"
	"nexadmin/core/response"
	"nexadmin/pkg/idcard"
	"nexadmin/pkg/idgen"
	"nexadmin/pkg/mask"
)

// 路由前缀：/api/v1/plugins/crm。权限码：crm:list 查看；crm:edit 客户维护；
// crm:opp 线索/商机维护。数据范围在入口统一解析：受限账号只能看到/操作
// owner 归属（本人 + 所辖部门）范围内的客户与商机。

type handler struct{ svc *Service }

// scopeOf 解析当前请求者的数据范围；失败即整请求失败（无身份 401）。
func (h *handler) scopeOf(c *gin.Context) (*datascope.Scope, bool) {
	sc, err := h.svc.scopeFromContext(c.Request.Context())
	if err != nil {
		response.Fail(c, err)
		return nil, false
	}
	return sc, true
}

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

type customerForm struct {
	Name           string `json:"name"`
	Code           string `json:"code"`
	Level          string `json:"level"`
	Status         string `json:"status"`
	Type           string `json:"type"`
	ExternalSource string `json:"externalSource"`
	ExternalNo     string `json:"externalNo"`
	Owner          string `json:"owner"`
	Phone          string `json:"phone"`
	Email          string `json:"email"`
	Country        string `json:"country"`
	Nationality    string `json:"nationality"`
	IdType         string `json:"idType"`
	IdNo           string `json:"idNo"`
	TaxNo          string `json:"taxNo"`
	Industry       string `json:"industry"`
	Address        string `json:"address"`
	Remark         string `json:"remark"`
}

func (f customerForm) input() CustomerInput {
	return CustomerInput{
		Name: f.Name, Code: f.Code, Level: f.Level, Status: f.Status, Type: f.Type,
		ExternalSource: f.ExternalSource, ExternalNo: f.ExternalNo,
		Owner: f.Owner, Phone: f.Phone, Email: f.Email, Remark: f.Remark,
		Country: f.Country, Nationality: f.Nationality,
		IdType: f.IdType, IdNo: f.IdNo,
		TaxNo: f.TaxNo, Industry: f.Industry, Address: f.Address,
	}
}

// PermRevealID 是查看明文证件号所需的权限点，由 realname 插件声明；
// 每次查看都进操作日志，事后能回答"谁看过谁的证件号"。
const PermRevealID = "realname:reveal"

// canRevealID 判断当前请求者能否看明文。Claims.Perms 不进 JWT（只有开放平台
// 中间件会填），所以走 Profile 取真实权限集合；取不到就按"不能"处理。
func canRevealID(c *gin.Context) bool {
	cl := auth.ClaimsFrom(c)
	if cl == nil {
		return false
	}
	if cl.Super {
		return true
	}
	if svc := auth.Default(); svc != nil && svc.Provider() != nil {
		if u, err := svc.Profile(c.Request.Context(), cl); err == nil {
			return auth.HasPerm(u.Perms, PermRevealID)
		}
	}
	return false
}

// maskIDRows 把证件号脱敏。列表/详情/导出三条读路都走这里，漏一条就是明文事故。
func maskIDRows(c *gin.Context, rows []Customer) {
	if canRevealID(c) {
		return
	}
	for i := range rows {
		rows[i].IdNo = mask.IDCard(rows[i].IdNo)
	}
}

// GET /plugins/crm/stats —— CRM 概览卡片（按类型/状态的客户计数 + 在途商机数）。
func (h *handler) crmStats(c *gin.Context) {
	sc, ok := h.scopeOf(c)
	if !ok {
		return
	}
	st, err := h.svc.Stats(c.Request.Context(), sc)
	respond(c, st, err)
}

// GET /plugins/crm/customers?keyword=&type=&status=&source=&page=&pageSize=
func (h *handler) listCustomers(c *gin.Context) {
	sc, ok := h.scopeOf(c)
	if !ok {
		return
	}
	page, size := response.ParsePage(c)
	rows, total, err := h.svc.ListCustomers(c.Request.Context(), customerQueryFrom(c, sc))
	if err != nil {
		response.Fail(c, err)
		return
	}
	maskIDRows(c, rows)
	response.Page(c, rows, total, page, size)
}

// customerQueryFrom 把列表/导出共用的查询参数收拢。
func customerQueryFrom(c *gin.Context, sc *datascope.Scope) CustomerQuery {
	return CustomerQuery{
		Keyword: c.Query("keyword"), Type: c.Query("type"),
		Status: c.Query("status"), Source: c.Query("source"),
		Scope: sc, Mine: c.Query("mine") == "1",
	}
}

// GET /plugins/crm/customers/export?<同列表筛选> —— 导出当前筛选下的全部客户。
// 数据范围生效；CSV 由前端生成。
func (h *handler) exportCustomers(c *gin.Context) {
	sc, ok := h.scopeOf(c)
	if !ok {
		return
	}
	rows, err := h.svc.ExportCustomers(c.Request.Context(), customerQueryFrom(c, sc))
	if err != nil {
		response.Fail(c, err)
		return
	}
	maskIDRows(c, rows) // 导出默认脱敏：明文只能逐条按权限查看，不跟着 CSV 批量出去。
	response.OK(c, rows)
}

// GET /plugins/crm/customers/pool?<同列表筛选> —— 公海客户列表。
// 公海对持 crm:list 的所有人可见，不做数据范围（无主客户不属于任何人）。
func (h *handler) listPool(c *gin.Context) {
	page, size := response.ParsePage(c)
	rows, total, err := h.svc.ListPool(c.Request.Context(), customerQueryFrom(c, nil))
	if err != nil {
		response.Fail(c, err)
		return
	}
	maskIDRows(c, rows)
	response.Page(c, rows, total, page, size)
}

// POST /plugins/crm/customers/:id/claim —— 从公海领取客户：owner = 当前账号。
func (h *handler) claimCustomer(c *gin.Context) {
	id, ok := idParam(c)
	if !ok {
		return
	}
	cu, err := h.svc.ClaimCustomer(c.Request.Context(), id)
	respond(c, cu, err)
}

// POST /plugins/crm/customers/:id/release —— 把客户放回公海（owner 清空）。
func (h *handler) releaseCustomer(c *gin.Context) {
	sc, ok := h.scopeOf(c)
	if !ok {
		return
	}
	id, ok := idParam(c)
	if !ok {
		return
	}
	cu, err := h.svc.ReleaseCustomer(c.Request.Context(), sc, id)
	respond(c, cu, err)
}

// GET /plugins/crm/customers/:id
func (h *handler) getCustomer(c *gin.Context) {
	sc, ok := h.scopeOf(c)
	if !ok {
		return
	}
	id, ok := idParam(c)
	if !ok {
		return
	}
	cu, err := h.svc.GetCustomer(c.Request.Context(), sc, id)
	if err == nil && !canRevealID(c) {
		cu.IdNo = mask.IDCard(cu.IdNo)
	}
	respond(c, cu, err)
}

// GET /plugins/crm/customers/:id/idcard —— 明文证件号与由号码派生的实名信息。
// 单独一条路由而不是列表加字段：权限点独立、操作日志独立，谁看了什么留得下来。
func (h *handler) revealID(c *gin.Context) {
	sc, ok := h.scopeOf(c)
	if !ok {
		return
	}
	id, ok := idParam(c)
	if !ok {
		return
	}
	cu, err := h.svc.GetCustomer(c.Request.Context(), sc, id)
	if err != nil {
		response.Fail(c, err)
		return
	}
	if cu.IdNo == "" {
		response.Fail(c, response.NotFound("crm.id_pair_invalid"))
		return
	}
	view := gin.H{"idType": cu.IdType, "idNo": cu.IdNo}
	if info, err := idcard.Parse(cu.IdNo, region.Trail); err == nil {
		view["kind"] = info.Kind
		view["regionNames"] = info.RegionNames
		view["birthday"] = info.Birthday.Format("2006-01-02")
		view["sex"] = info.Sex
		view["age"] = info.AgeAt(time.Now().UTC())
	} else {
		// 存量脏数据（历史导入的错号）照样给原文，但要如实标出来，别让人以为号码是合法的。
		view["valid"] = false
		view["reason"] = string(idcard.ReasonOf(err))
	}
	response.OK(c, view)
}

// POST /plugins/crm/customers
func (h *handler) createCustomer(c *gin.Context) {
	var f customerForm
	if !response.BindJSON(c, &f) {
		return
	}
	cu, err := h.svc.CreateCustomer(c.Request.Context(), f.input())
	respond(c, cu, err)
}

// PUT /plugins/crm/customers/:id
func (h *handler) updateCustomer(c *gin.Context) {
	sc, ok := h.scopeOf(c)
	if !ok {
		return
	}
	id, ok := idParam(c)
	if !ok {
		return
	}
	var f customerForm
	if !response.BindJSON(c, &f) {
		return
	}
	cu, err := h.svc.UpdateCustomer(c.Request.Context(), sc, id, f.input())
	respond(c, cu, err)
}

// DELETE /plugins/crm/customers/:id
func (h *handler) deleteCustomer(c *gin.Context) {
	sc, ok := h.scopeOf(c)
	if !ok {
		return
	}
	id, ok := idParam(c)
	if !ok {
		return
	}
	respond(c, nil, h.svc.DeleteCustomer(c.Request.Context(), sc, id))
}

// POST /plugins/crm/customers/bind-external  {"customerId":1,"source":"hotel","externalNo":"M0001"}
func (h *handler) bindExternal(c *gin.Context) {
	var f struct {
		CustomerID idgen.ID `json:"customerId"`
		Source     string   `json:"source"`
		ExternalNo string   `json:"externalNo"`
	}
	if !response.BindJSON(c, &f) {
		return
	}
	cu, err := h.svc.BindExternal(c.Request.Context(), f.CustomerID, f.Source, f.ExternalNo)
	respond(c, cu, err)
}

// POST /plugins/crm/customers/merge  {"targetId":1,"sourceId":2}
// 把 source 并入 target：联系人/跟进/商机整体迁移，主客户补空字段，删除从客户。
func (h *handler) mergeCustomers(c *gin.Context) {
	sc, ok := h.scopeOf(c)
	if !ok {
		return
	}
	var f MergeInput
	if !response.BindJSON(c, &f) {
		return
	}
	cu, err := h.svc.MergeCustomers(c.Request.Context(), sc, f)
	respond(c, cu, err)
}

// GET /plugins/crm/customers/resolve-external?source=hotel&externalNo=M0001&fallbackName=...
func (h *handler) resolveExternal(c *gin.Context) {
	cu, created, err := h.svc.ResolveByExternal(c.Request.Context(),
		c.Query("source"), c.Query("externalNo"), c.Query("fallbackName"))
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, gin.H{"customer": cu, "created": created})
}

type opportunityForm struct {
	CustomerID  idgen.ID `json:"customerId"`
	ContactID   idgen.ID `json:"contactId"`
	Title       string   `json:"title"`
	Stage       string   `json:"stage"`
	AmountMinor int64    `json:"amountMinor"`
	Currency    string   `json:"currency"`
	ExpectedAt  string   `json:"expectedAt"`
	Owner       string   `json:"owner"`
	Remark      string   `json:"remark"`
}

func (f opportunityForm) input() OpportunityInput {
	return OpportunityInput{
		CustomerID: f.CustomerID, ContactID: f.ContactID, Title: f.Title, Stage: f.Stage,
		AmountMinor: f.AmountMinor, Currency: strings.TrimSpace(f.Currency),
		ExpectedAt: f.ExpectedAt, Owner: f.Owner, Remark: f.Remark,
	}
}

// GET /plugins/crm/opportunities?keyword=&customerId=&stage=&page=&pageSize=
func (h *handler) listOpportunities(c *gin.Context) {
	sc, ok := h.scopeOf(c)
	if !ok {
		return
	}
	page, size := response.ParsePage(c)
	customerID, _ := strconv.ParseInt(c.Query("customerId"), 10, 64)
	rows, total, err := h.svc.ListOpportunities(c.Request.Context(), OpportunityQuery{
		Keyword: c.Query("keyword"), CustomerID: idgen.ID(customerID), Stage: c.Query("stage"),
		Scope: sc, Mine: c.Query("mine") == "1", Page: page, PageSize: size,
	})
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.Page(c, rows, total, page, size)
}

// GET /plugins/crm/opportunities/:id
func (h *handler) getOpportunity(c *gin.Context) {
	sc, ok := h.scopeOf(c)
	if !ok {
		return
	}
	id, ok := idParam(c)
	if !ok {
		return
	}
	o, err := h.svc.GetOpportunity(c.Request.Context(), sc, id)
	respond(c, o, err)
}

// POST /plugins/crm/opportunities
func (h *handler) createOpportunity(c *gin.Context) {
	sc, ok := h.scopeOf(c)
	if !ok {
		return
	}
	var f opportunityForm
	if !response.BindJSON(c, &f) {
		return
	}
	o, err := h.svc.CreateOpportunity(c.Request.Context(), sc, f.input())
	respond(c, o, err)
}

// PUT /plugins/crm/opportunities/:id
func (h *handler) updateOpportunity(c *gin.Context) {
	sc, ok := h.scopeOf(c)
	if !ok {
		return
	}
	id, ok := idParam(c)
	if !ok {
		return
	}
	var f opportunityForm
	if !response.BindJSON(c, &f) {
		return
	}
	o, err := h.svc.UpdateOpportunity(c.Request.Context(), sc, id, f.input())
	respond(c, o, err)
}

// DELETE /plugins/crm/opportunities/:id
func (h *handler) deleteOpportunity(c *gin.Context) {
	sc, ok := h.scopeOf(c)
	if !ok {
		return
	}
	id, ok := idParam(c)
	if !ok {
		return
	}
	respond(c, nil, h.svc.DeleteOpportunity(c.Request.Context(), sc, id))
}

// ---- 联系人 ----

type contactForm struct {
	Name      string `json:"name"`
	Title     string `json:"title"`
	Phone     string `json:"phone"`
	Email     string `json:"email"`
	IsPrimary bool   `json:"isPrimary"`
	Remark    string `json:"remark"`
}

func (f contactForm) input() ContactInput {
	return ContactInput{Name: f.Name, Title: f.Title, Phone: f.Phone, Email: f.Email, IsPrimary: f.IsPrimary, Remark: f.Remark}
}

// GET /plugins/crm/customers/:id/contacts
func (h *handler) listContacts(c *gin.Context) {
	sc, ok := h.scopeOf(c)
	if !ok {
		return
	}
	id, ok := idParam(c)
	if !ok {
		return
	}
	rows, err := h.svc.ListContacts(c.Request.Context(), sc, id)
	respond(c, rows, err)
}

// POST /plugins/crm/customers/:id/contacts
func (h *handler) createContact(c *gin.Context) {
	sc, ok := h.scopeOf(c)
	if !ok {
		return
	}
	id, ok := idParam(c)
	if !ok {
		return
	}
	var f contactForm
	if !response.BindJSON(c, &f) {
		return
	}
	ct, err := h.svc.CreateContact(c.Request.Context(), sc, id, f.input())
	respond(c, ct, err)
}

// PUT /plugins/crm/contacts/:id
func (h *handler) updateContact(c *gin.Context) {
	sc, ok := h.scopeOf(c)
	if !ok {
		return
	}
	id, ok := idParam(c)
	if !ok {
		return
	}
	var f contactForm
	if !response.BindJSON(c, &f) {
		return
	}
	ct, err := h.svc.UpdateContact(c.Request.Context(), sc, id, f.input())
	respond(c, ct, err)
}

// DELETE /plugins/crm/contacts/:id
func (h *handler) deleteContact(c *gin.Context) {
	sc, ok := h.scopeOf(c)
	if !ok {
		return
	}
	id, ok := idParam(c)
	if !ok {
		return
	}
	respond(c, nil, h.svc.DeleteContact(c.Request.Context(), sc, id))
}

// ---- 跟进记录 ----

type activityForm struct {
	OpportunityID idgen.ID `json:"opportunityId"`
	Kind          string   `json:"kind"`
	Content       string   `json:"content"`
	NextAt        string   `json:"nextAt"`
}

func (f activityForm) input(customerID idgen.ID) ActivityInput {
	return ActivityInput{CustomerID: customerID, OpportunityID: f.OpportunityID, Kind: f.Kind, Content: f.Content, NextAt: f.NextAt}
}

// GET /plugins/crm/customers/:id/activities?opportunityId=&kind=&openOnly=&page=&pageSize=
func (h *handler) listActivities(c *gin.Context) {
	sc, ok := h.scopeOf(c)
	if !ok {
		return
	}
	id, ok := idParam(c)
	if !ok {
		return
	}
	page, size := response.ParsePage(c)
	oppID, _ := strconv.ParseInt(c.Query("opportunityId"), 10, 64)
	rows, total, err := h.svc.ListActivities(c.Request.Context(), sc, ActivityQuery{
		CustomerID: id, OpportunityID: idgen.ID(oppID), Kind: c.Query("kind"),
		OpenOnly: c.Query("openOnly") == "1", Page: page, PageSize: size,
	})
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.Page(c, rows, total, page, size)
}

// POST /plugins/crm/customers/:id/activities
func (h *handler) createActivity(c *gin.Context) {
	sc, ok := h.scopeOf(c)
	if !ok {
		return
	}
	id, ok := idParam(c)
	if !ok {
		return
	}
	var f activityForm
	if !response.BindJSON(c, &f) {
		return
	}
	a, err := h.svc.CreateActivity(c.Request.Context(), sc, f.input(id))
	respond(c, a, err)
}

// PUT /plugins/crm/activities/:id/done  {"done":true}
func (h *handler) markActivityDone(c *gin.Context) {
	sc, ok := h.scopeOf(c)
	if !ok {
		return
	}
	id, ok := idParam(c)
	if !ok {
		return
	}
	var f struct {
		Done bool `json:"done"`
	}
	if !response.BindJSON(c, &f) {
		return
	}
	respond(c, nil, h.svc.MarkActivityDone(c.Request.Context(), sc, id, f.Done))
}

// DELETE /plugins/crm/activities/:id
func (h *handler) deleteActivity(c *gin.Context) {
	sc, ok := h.scopeOf(c)
	if !ok {
		return
	}
	id, ok := idParam(c)
	if !ok {
		return
	}
	respond(c, nil, h.svc.DeleteActivity(c.Request.Context(), sc, id))
}
