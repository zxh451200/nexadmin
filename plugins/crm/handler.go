package crm

import (
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	"nexadmin/core/datascope"
	"nexadmin/core/response"
	"nexadmin/pkg/idgen"
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
	ExternalSource string `json:"externalSource"`
	ExternalNo     string `json:"externalNo"`
	Owner          string `json:"owner"`
	Phone          string `json:"phone"`
	Email          string `json:"email"`
	Remark         string `json:"remark"`
}

func (f customerForm) input() CustomerInput {
	return CustomerInput{
		Name: f.Name, Code: f.Code, Level: f.Level, Status: f.Status,
		ExternalSource: f.ExternalSource, ExternalNo: f.ExternalNo,
		Owner: f.Owner, Phone: f.Phone, Email: f.Email, Remark: f.Remark,
	}
}

// GET /plugins/crm/customers?keyword=&status=&source=&page=&pageSize=
func (h *handler) listCustomers(c *gin.Context) {
	sc, ok := h.scopeOf(c)
	if !ok {
		return
	}
	page, size := response.ParsePage(c)
	rows, total, err := h.svc.ListCustomers(c.Request.Context(), CustomerQuery{
		Keyword: c.Query("keyword"), Status: c.Query("status"), Source: c.Query("source"),
		Scope: sc, Mine: c.Query("mine") == "1", Page: page, PageSize: size,
	})
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.Page(c, rows, total, page, size)
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
	respond(c, cu, err)
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
