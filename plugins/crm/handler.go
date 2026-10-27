package crm

import (
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	"nexadmin/core/response"
	"nexadmin/pkg/idgen"
)

// 路由前缀：/api/v1/plugins/crm。权限码：crm:list 查看；crm:edit 客户维护；
// crm:opp 线索/商机维护。

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
	page, size := response.ParsePage(c)
	rows, total, err := h.svc.ListCustomers(c.Request.Context(), CustomerQuery{
		Keyword: c.Query("keyword"), Status: c.Query("status"), Source: c.Query("source"),
		Page: page, PageSize: size,
	})
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.Page(c, rows, total, page, size)
}

// GET /plugins/crm/customers/:id
func (h *handler) getCustomer(c *gin.Context) {
	id, ok := idParam(c)
	if !ok {
		return
	}
	cu, err := h.svc.GetCustomer(c.Request.Context(), id)
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
	id, ok := idParam(c)
	if !ok {
		return
	}
	var f customerForm
	if !response.BindJSON(c, &f) {
		return
	}
	cu, err := h.svc.UpdateCustomer(c.Request.Context(), id, f.input())
	respond(c, cu, err)
}

// DELETE /plugins/crm/customers/:id
func (h *handler) deleteCustomer(c *gin.Context) {
	id, ok := idParam(c)
	if !ok {
		return
	}
	respond(c, nil, h.svc.DeleteCustomer(c.Request.Context(), id))
}

// POST /plugins/crm/customers/bind-external  {"customerId":1,"source":"hotel","externalNo":"M0001"}
func (h *handler) bindExternal(c *gin.Context) {
	var f struct {
		CustomerID int64  `json:"customerId"`
		Source     string `json:"source"`
		ExternalNo string `json:"externalNo"`
	}
	if !response.BindJSON(c, &f) {
		return
	}
	cu, err := h.svc.BindExternal(c.Request.Context(), idgen.ID(f.CustomerID), f.Source, f.ExternalNo)
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
	CustomerID  int64  `json:"customerId"`
	Title       string `json:"title"`
	Stage       string `json:"stage"`
	AmountMinor int64  `json:"amountMinor"`
	Currency    string `json:"currency"`
	ExpectedAt  string `json:"expectedAt"`
	Owner       string `json:"owner"`
	Remark      string `json:"remark"`
}

func (f opportunityForm) input() OpportunityInput {
	return OpportunityInput{
		CustomerID: f.CustomerID, Title: f.Title, Stage: f.Stage,
		AmountMinor: f.AmountMinor, Currency: strings.TrimSpace(f.Currency),
		ExpectedAt: f.ExpectedAt, Owner: f.Owner, Remark: f.Remark,
	}
}

// GET /plugins/crm/opportunities?keyword=&customerId=&stage=&page=&pageSize=
func (h *handler) listOpportunities(c *gin.Context) {
	page, size := response.ParsePage(c)
	customerID, _ := strconv.ParseInt(c.Query("customerId"), 10, 64)
	rows, total, err := h.svc.ListOpportunities(c.Request.Context(), OpportunityQuery{
		Keyword: c.Query("keyword"), CustomerID: customerID, Stage: c.Query("stage"),
		Page: page, PageSize: size,
	})
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.Page(c, rows, total, page, size)
}

// GET /plugins/crm/opportunities/:id
func (h *handler) getOpportunity(c *gin.Context) {
	id, ok := idParam(c)
	if !ok {
		return
	}
	o, err := h.svc.GetOpportunity(c.Request.Context(), id)
	respond(c, o, err)
}

// POST /plugins/crm/opportunities
func (h *handler) createOpportunity(c *gin.Context) {
	var f opportunityForm
	if !response.BindJSON(c, &f) {
		return
	}
	o, err := h.svc.CreateOpportunity(c.Request.Context(), f.input())
	respond(c, o, err)
}

// PUT /plugins/crm/opportunities/:id
func (h *handler) updateOpportunity(c *gin.Context) {
	id, ok := idParam(c)
	if !ok {
		return
	}
	var f opportunityForm
	if !response.BindJSON(c, &f) {
		return
	}
	o, err := h.svc.UpdateOpportunity(c.Request.Context(), id, f.input())
	respond(c, o, err)
}

// DELETE /plugins/crm/opportunities/:id
func (h *handler) deleteOpportunity(c *gin.Context) {
	id, ok := idParam(c)
	if !ok {
		return
	}
	respond(c, nil, h.svc.DeleteOpportunity(c.Request.Context(), id))
}
