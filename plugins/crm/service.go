package crm

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"gorm.io/gorm"

	"nexadmin/core/auth"
	"nexadmin/core/datascope"
	"nexadmin/core/region"
	"nexadmin/core/response"
	"nexadmin/pkg/idcard"
	"nexadmin/pkg/idgen"
)

// 名称/编码/等级/国家的上限与列宽一致（字符数，不是字节数）。
const (
	nameMaxLen    = 200
	codeMaxLen    = 64
	levelMaxLen   = 16
	countryMaxLen = 2 // ISO 3166-1 alpha-2
)

// Service 是 crm 业务层。宿主模块（hotel/mall 的 member）通过 client.go 对接。
type Service struct {
	db  *gorm.DB
	log logger
}

// logger 是 Service 需要的最小日志面。
type logger interface {
	Info(msg string, keysAndValues ...any)
	Warn(msg string, keysAndValues ...any)
	Error(msg string, keysAndValues ...any)
}

// NewService 创建 crm 服务。
func NewService(db *gorm.DB, log logger) *Service { return &Service{db: db, log: log} }

// CustomerInput 是客户主数据入参。
type CustomerInput struct {
	Name           string
	Code           string
	Level          string
	Status         string
	Type           string
	ExternalSource string
	ExternalNo     string
	Owner          string
	Phone          string
	Email          string
	Country        string
	Nationality    string
	IdType         string
	IdNo           string
	TaxNo          string
	Industry       string
	Address        string
	Remark         string
}

// normalizeISOCountry 把国籍/国家输入收口成 ISO 3166-1 alpha-2：去空格、统一大写，
// 空串表示未登记（合法）；非 2 位字母视为无效（ok=false）。
func normalizeISOCountry(raw string) (string, bool) {
	code := strings.ToUpper(strings.TrimSpace(raw))
	if code == "" {
		return "", true
	}
	if len(code) != countryMaxLen {
		return "", false
	}
	for _, r := range code {
		if r < 'A' || r > 'Z' {
			return "", false
		}
	}
	return code, true
}

func (in *CustomerInput) normalize() error {
	in.Name = truncate(strings.TrimSpace(in.Name), nameMaxLen)
	in.Code = truncate(strings.TrimSpace(in.Code), codeMaxLen)
	// 等级是短档位（A/B/C），统一大写，避免 "a" 与 "A" 各算一档。
	in.Level = truncate(strings.ToUpper(strings.TrimSpace(in.Level)), levelMaxLen)
	// 联系字段在这里统一裁长：normalize 是唯一入口，调用方（创建/更新/导入）不必各抄一遍。
	in.Owner = truncate(strings.TrimSpace(in.Owner), 64)
	in.Phone = truncate(strings.TrimSpace(in.Phone), 32)
	in.Email = truncate(strings.TrimSpace(in.Email), 128)
	in.Remark = truncate(strings.TrimSpace(in.Remark), 500)
	in.ExternalSource, in.ExternalNo = strings.TrimSpace(in.ExternalSource), strings.TrimSpace(in.ExternalNo)
	in.Type = strings.ToLower(strings.TrimSpace(in.Type))
	if in.Type == "" {
		// 存量会员（酒店住客、商城会员）都是自然人，不填按个人。
		in.Type = TypePerson
	}
	if !TypeSet[in.Type] {
		return response.BadRequest("crm.type_invalid")
	}
	in.IdType = strings.ToLower(strings.TrimSpace(in.IdType))
	in.IdNo = strings.TrimSpace(in.IdNo)
	in.TaxNo = strings.ToUpper(strings.TrimSpace(in.TaxNo))
	in.Industry = strings.TrimSpace(in.Industry)
	in.Address = strings.TrimSpace(in.Address)
	// 类型决定哪组身份字段生效，另一组清空：客户从个人改成企业，不该还背着一条护照号。
	if in.Type == TypeCompany {
		in.Nationality, in.IdType, in.IdNo = "", "", ""
	} else {
		in.TaxNo, in.Industry, in.Address = "", "", ""
	}
	country, ok := normalizeISOCountry(in.Country)
	if !ok {
		return response.BadRequest("crm.country_invalid")
	}
	in.Country = country
	nationality, ok := normalizeISOCountry(in.Nationality)
	if !ok {
		return response.BadRequest("crm.nationality_invalid")
	}
	in.Nationality = nationality
	if in.IdType != "" && !IdTypeSet[in.IdType] {
		return response.BadRequest("crm.id_type_invalid")
	}
	// 证件类型与证件号成对：只填一项既没法核验也没法复用到入住登记。
	if (in.IdType == "") != (in.IdNo == "") {
		return response.BadRequest("crm.id_pair_invalid")
	}
	// 大陆居民身份证：归一化（去空白/全角、末位 x 大写、15 位升 18 位）后判合法性，
	// 存归一化结果。带 * 的是列表/详情回显的脱敏串，视为"没改过"，由 UpdateCustomer 沿用原值。
	if in.IdType == IdTypeIDCard && in.IdNo != "" && !strings.Contains(in.IdNo, "*") {
		info, err := idcard.Parse(in.IdNo, region.Trail)
		if err != nil {
			return response.BadRequest("crm.id_no_invalid")
		}
		in.IdNo = info.Number
	}
	in.IdNo = truncate(in.IdNo, 64)
	in.TaxNo = truncate(in.TaxNo, 32)
	in.Industry = truncate(in.Industry, 64)
	in.Address = truncate(in.Address, 200)
	if in.Name == "" {
		return response.BadRequest("crm.name_required")
	}
	if in.ExternalSource == "" && in.ExternalNo != "" {
		return response.BadRequest("crm.external_invalid")
	}
	if in.ExternalSource != "" {
		if !ExternalSourceSet[in.ExternalSource] {
			return response.BadRequest("crm.external_invalid")
		}
		if in.ExternalNo == "" {
			return response.BadRequest("crm.external_invalid")
		}
	}
	if in.Status == "" {
		in.Status = StatusActive
	}
	if in.Status != StatusActive && in.Status != StatusInactive {
		return response.BadRequest("crm.status_invalid")
	}
	return nil
}

// CreateCustomer 创建客户主数据（幂等键 = external 锚点；同锚点重复创建返回已有客户）。
// checkCustomerDuplicate 是保存前的查重：同租户下，个人客户手机号、企业客户统一社会信用代码
// 不允许与既有客户重复（手机号是联系与合并的主键级线索，信用代码对企业同理）。
// excludeID 用于更新时排除自身；锚点查重已由 external 唯一索引兜住，这里管的是手工录入的主数据。
// 对标竞品的 duplicate detection：创建时当场拦截，而不是让销售名单里躺着两份同一个客户。
func (s *Service) checkCustomerDuplicate(ctx context.Context, excludeID idgen.ID, in CustomerInput) error {
	check := func(cond string, arg any) bool {
		db := s.db.WithContext(ctx).Model(&Customer{}).Where(cond, arg)
		if excludeID != 0 {
			db = db.Where("id <> ?", int64(excludeID))
		}
		var n int64
		return db.Count(&n).Error == nil && n > 0
	}
	switch {
	case in.Type == TypePerson && in.Phone != "":
		if check("phone = ?", in.Phone) {
			return response.New(http.StatusConflict, "crm.duplicate_customer")
		}
	case in.Type == TypeCompany && in.TaxNo != "":
		if check("tax_no = ?", in.TaxNo) {
			return response.New(http.StatusConflict, "crm.duplicate_customer")
		}
	}
	return nil
}

// exportMax 是单次导出的行数上限：CRM 名单宁可让用户补筛选条件，也不静默截断。
const exportMax = 10000

// ExportCustomers 导出当前筛选下的全部客户（不带分页）。筛选与 ListCustomers 同源，
// 数据范围同样生效——受限操作者拿不到范围外的客户。
func (s *Service) ExportCustomers(ctx context.Context, q CustomerQuery) ([]Customer, error) {
	q.Page, q.PageSize = 1, exportMax
	rows, _, err := s.ListCustomers(ctx, q)
	if err != nil {
		return nil, err
	}
	if len(rows) >= exportMax {
		return nil, response.BadRequest("crm.export_too_many")
	}
	return rows, nil
}

func (s *Service) CreateCustomer(ctx context.Context, in CustomerInput) (*Customer, error) {
	if strings.Contains(in.IdNo, "*") {
		// 新建时没有"原值"可沿用，脱敏串当输入就是脏数据。
		return nil, response.BadRequest("crm.id_no_masked")
	}
	if err := in.normalize(); err != nil {
		return nil, err
	}
	if err := s.checkCustomerDuplicate(ctx, 0, in); err != nil {
		return nil, err
	}
	if in.ExternalSource != "" {
		var existing Customer
		err := s.db.WithContext(ctx).
			Where("external_source = ? AND external_no = ?", in.ExternalSource, in.ExternalNo).
			Take(&existing).Error
		if err == nil {
			return &existing, nil
		}
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, err
		}
	}
	c := &Customer{
		Name: in.Name, Code: in.Code, Level: in.Level, Status: in.Status, Type: in.Type,
		Owner: truncate(in.Owner, 64), Phone: truncate(in.Phone, 32),
		Email: truncate(in.Email, 128), Remark: truncate(in.Remark, 500),
		Country: in.Country, Nationality: in.Nationality,
		IdType: in.IdType, IdNo: in.IdNo,
		TaxNo: in.TaxNo, Industry: in.Industry, Address: in.Address,
	}
	if c.Code == "" {
		// 先取 ID 再算编码：编码由 ID 派生，建库前就定下来，不需要回写一次。
		id, err := idgen.Next()
		if err != nil {
			return nil, err
		}
		c.ID, c.Code = id, customerCode(id)
	}
	if in.ExternalSource != "" {
		src, no := in.ExternalSource, in.ExternalNo
		c.ExternalSource, c.ExternalNo = &src, &no
	}
	if err := s.db.WithContext(ctx).Create(c).Error; err != nil {
		// 并发同锚点命中唯一索引：读回胜者。
		if in.ExternalSource != "" {
			var dup Customer
			if take := s.db.WithContext(ctx).
				Where("external_source = ? AND external_no = ?", in.ExternalSource, in.ExternalNo).
				Take(&dup).Error; take == nil {
				return &dup, nil
			}
		}
		return nil, err
	}
	s.publishCustomer(ctx, EventCustomerCreated, c)
	return c, nil
}

// UpdateCustomer 更新客户主数据（external 锚点不随编辑变更——锚点是宿主体系的稳定锚）。
// sc 非 nil 且受限时，范围外客户得到 403。
func (s *Service) UpdateCustomer(ctx context.Context, sc *datascope.Scope, id idgen.ID, in CustomerInput) (*Customer, error) {
	if err := in.normalize(); err != nil {
		return nil, err
	}
	if _, err := s.visibleCustomer(ctx, sc, id); err != nil {
		return nil, err
	}
	var c Customer
	if err := s.db.WithContext(ctx).Take(&c, id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, response.NotFound("crm.not_found")
		}
		return nil, err
	}
	// 清空编码视为"沿用原编码"：导入按手机号更新时不会把手工编码冲掉。
	if in.Code == "" {
		in.Code = c.Code
	}
	// 回显的是脱敏串（110101********803X）时同样视为"没改过"：否则一次普通保存
	// 就会把星号写进库，证件号彻底废掉。
	if strings.Contains(in.IdNo, "*") {
		in.IdNo, in.IdType = c.IdNo, c.IdType
	}
	// 查重放在可见性与取行之后：先确认能改这条，再谈是否撞了别人。
	if err := s.checkCustomerDuplicate(ctx, id, in); err != nil {
		return nil, err
	}
	updates := map[string]any{
		"name": in.Name, "code": in.Code, "level": in.Level, "status": in.Status,
		"type":  in.Type,
		"owner": truncate(in.Owner, 64), "phone": truncate(in.Phone, 32),
		"email": truncate(in.Email, 128), "remark": truncate(in.Remark, 500),
		"country": in.Country, "nationality": in.Nationality,
		"id_type": in.IdType, "id_no": in.IdNo,
		"tax_no": in.TaxNo, "industry": in.Industry, "address": in.Address,
	}
	if err := s.db.WithContext(ctx).Model(&Customer{}).Where("id = ?", int64(id)).Updates(updates).Error; err != nil {
		return nil, err
	}
	c.Name, c.Code, c.Level, c.Status, c.Type = in.Name, in.Code, in.Level, in.Status, in.Type
	c.Owner, c.Phone, c.Email, c.Remark = updates["owner"].(string), updates["phone"].(string), updates["email"].(string), updates["remark"].(string)
	c.Country, c.Nationality = in.Country, in.Nationality
	c.IdType, c.IdNo = in.IdType, in.IdNo
	c.TaxNo, c.Industry, c.Address = in.TaxNo, in.Industry, in.Address
	s.publishCustomer(ctx, EventCustomerUpdated, &c)
	return &c, nil
}

// DeleteCustomer 删除客户主数据。还有商机挂在客户下时拒绝（主数据不级联消失）；
// 联系人与跟进记录随客户一并清理（事务内，不留孤儿）。sc 非 nil 且受限时，范围外客户得到 403。
func (s *Service) DeleteCustomer(ctx context.Context, sc *datascope.Scope, id idgen.ID) error {
	if _, err := s.visibleCustomer(ctx, sc, id); err != nil {
		return err
	}
	var n int64
	if err := s.db.WithContext(ctx).Model(&Opportunity{}).Where("customer_id = ?", int64(id)).Count(&n).Error; err != nil {
		return err
	}
	if n > 0 {
		return response.Conflict("crm.customer_in_use")
	}
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("customer_id = ?", int64(id)).Delete(&Contact{}).Error; err != nil {
			return err
		}
		if err := tx.Where("customer_id = ?", int64(id)).Delete(&Activity{}).Error; err != nil {
			return err
		}
		return tx.Delete(&Customer{}, id).Error
	})
}

// GetCustomer 按 ID 取客户。
// GetCustomer 按 ID 取客户（HTTP 入口需先解析数据范围；sc 为 nil 表示不裁剪，仅供内部调用）。
func (s *Service) GetCustomer(ctx context.Context, sc *datascope.Scope, id idgen.ID) (*Customer, error) {
	return s.visibleCustomer(ctx, sc, id)
}

// CustomerQuery 过滤客户列表。Scope 由 HTTP 入口解析后传入；nil 表示不裁剪（内部调用）。
type CustomerQuery struct {
	Keyword  string // 名称 / 编码 / 锚点 / 证件号 / 信用代码
	Type     string // person / company，空 = 不限
	Status   string
	Source   string
	Scope    *datascope.Scope
	Mine     bool // 只看我的：owner = 当前账号（与角色范围叠加）
	Page     int
	PageSize int
}

// ListCustomers 分页列出客户。受限操作者按 owner（账号）+ 所属部门裁剪，
// 复用 core/datascope 的 UsernameCond，与系统模块语义一致。
func (s *Service) ListCustomers(ctx context.Context, q CustomerQuery) ([]Customer, int64, error) {
	db := s.db.WithContext(ctx).Model(&Customer{})
	if kw := strings.TrimSpace(q.Keyword); kw != "" {
		// 前台更多是「拿着证件号 / 信用代码找人」，不是按名称翻列表。
		like := "%" + kw + "%"
		db = db.Where("LOWER(name) LIKE LOWER(?) OR LOWER(code) LIKE LOWER(?) OR LOWER(external_no) LIKE LOWER(?) OR LOWER(id_no) LIKE LOWER(?) OR LOWER(tax_no) LIKE LOWER(?)",
			like, like, like, like, like)
	}
	q.Type = strings.ToLower(strings.TrimSpace(q.Type))
	if q.Type != "" {
		if !TypeSet[q.Type] {
			return nil, 0, response.BadRequest("crm.type_invalid")
		}
		db = db.Where("type = ?", q.Type)
	}
	if q.Status != "" {
		db = db.Where("status = ?", q.Status)
	}
	if q.Source != "" {
		db = db.Where("external_source = ?", q.Source)
	}
	if cond, args, limited := q.Scope.UsernameCond("owner"); limited {
		db = db.Where(cond, args...)
	}
	// 「只看我的」是显式语义（与角色范围叠加）：owner = 当前账号。
	if q.Mine {
		_, name, ok := auth.UserFromContext(ctx)
		if !ok {
			return nil, 0, response.Unauthorized("error.unauthorized")
		}
		db = db.Where("owner = ?", name)
	}
	var total int64
	if err := db.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var rows []Customer
	if err := db.Order("id DESC").Offset((q.Page - 1) * q.PageSize).Limit(q.PageSize).Find(&rows).Error; err != nil {
		return nil, 0, err
	}
	return rows, total, nil
}

// BindExternal 把一个外部身份（hotel/mall 的 member）绑到已有客户上；
// 幂等：锚点已绑定的客户不变，重复绑定同一客户返回成功。
func (s *Service) BindExternal(ctx context.Context, customerID idgen.ID, source, externalNo string) (*Customer, error) {
	source, externalNo = strings.TrimSpace(source), strings.TrimSpace(externalNo)
	if source == "" || externalNo == "" || !ExternalSourceSet[source] {
		return nil, response.BadRequest("crm.external_invalid")
	}
	var c Customer
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Take(&c, customerID).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return response.NotFound("crm.not_found")
			}
			return err
		}
		var clash Customer
		err := tx.Where("external_source = ? AND external_no = ?", source, externalNo).Take(&clash).Error
		if err == nil {
			if clash.ID == c.ID {
				return nil // 已绑定：幂等
			}
			return response.Conflict("crm.external_taken")
		}
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		c.ExternalSource, c.ExternalNo = &source, &externalNo
		return tx.Model(&Customer{}).Where("id = ?", int64(customerID)).
			Updates(map[string]any{"external_source": source, "external_no": externalNo}).Error
	})
	if err != nil {
		return nil, err
	}
	s.publishCustomer(ctx, EventCustomerUpdated, &c)
	return &c, nil
}

// ResolveByExternal 按外部锚点反查客户（宿主 member 侧的主入口）。
// optIn 允许在未命中时按给定资料自动建档——这就是"让 member 认同同一个客户 ID"的接缝。
func (s *Service) ResolveByExternal(ctx context.Context, source, externalNo, fallbackName string) (*Customer, bool, error) {
	source, externalNo = strings.TrimSpace(source), strings.TrimSpace(externalNo)
	if source == "" || externalNo == "" || !ExternalSourceSet[source] {
		return nil, false, response.BadRequest("crm.external_invalid")
	}
	var c Customer
	err := s.db.WithContext(ctx).
		Where("external_source = ? AND external_no = ?", source, externalNo).
		Take(&c).Error
	if err == nil {
		return &c, false, nil
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, false, err
	}
	name := strings.TrimSpace(fallbackName)
	if name == "" {
		return nil, false, response.NotFound("crm.not_found")
	}
	created, err := s.CreateCustomer(ctx, CustomerInput{
		Name: name, ExternalSource: source, ExternalNo: externalNo,
	})
	return created, created != nil, err
}

// OpportunityInput 是线索/商机入参。
type OpportunityInput struct {
	CustomerID  idgen.ID
	ContactID   idgen.ID
	Title       string
	Stage       string
	AmountMinor int64
	Currency    string
	ExpectedAt  string
	Owner       string
	Remark      string
}

func (in *OpportunityInput) normalize() error {
	in.Title = strings.TrimSpace(in.Title)
	if in.Title == "" {
		return response.BadRequest("crm.title_required")
	}
	if in.Stage == "" {
		in.Stage = StageLead
	}
	if !StageSet[in.Stage] {
		return response.BadRequest("crm.stage_invalid")
	}
	if in.AmountMinor < 0 {
		return response.BadRequest("crm.amount_invalid")
	}
	if in.Currency == "" {
		in.Currency = "CNY"
	}
	in.Currency = strings.ToUpper(in.Currency)
	return nil
}

// contactInCustomer 校验联系人存在且属于该客户（商机挂联系人时的引用完整性）。
func (s *Service) contactInCustomer(ctx context.Context, contactID, customerID idgen.ID) error {
	var ct Contact
	if err := s.db.WithContext(ctx).Take(&ct, int64(contactID)).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return response.NotFound("crm.not_found")
		}
		return err
	}
	if ct.CustomerID != customerID {
		return response.Conflict("crm.contact_customer_mismatch")
	}
	return nil
}

// CreateOpportunity 建一条线索/商机（stage=lead 即线索）。sc 非 nil 且受限时，
// 不允许把商机挂到范围外客户名下（防借客户字段泄漏）。
func (s *Service) CreateOpportunity(ctx context.Context, sc *datascope.Scope, in OpportunityInput) (*Opportunity, error) {
	if err := in.normalize(); err != nil {
		return nil, err
	}
	var customerName string
	if in.CustomerID != 0 {
		cu, err := s.visibleCustomer(ctx, sc, in.CustomerID)
		if err != nil {
			return nil, err
		}
		customerName = cu.Name
	}
	if in.ContactID != 0 {
		if in.CustomerID == 0 {
			return nil, response.BadRequest("crm.contact_customer_mismatch")
		}
		if err := s.contactInCustomer(ctx, in.ContactID, in.CustomerID); err != nil {
			return nil, err
		}
	}
	o := &Opportunity{
		CustomerID: in.CustomerID, CustomerName: customerName, ContactID: in.ContactID,
		Title: in.Title, Stage: in.Stage,
		AmountMinor: in.AmountMinor, Currency: in.Currency,
		ExpectedAt: truncate(in.ExpectedAt, 10), Owner: truncate(in.Owner, 64),
		Remark: truncate(in.Remark, 500),
	}
	if err := s.db.WithContext(ctx).Create(o).Error; err != nil {
		return nil, err
	}
	s.publishOppStage(ctx, o, "")
	return o, nil
}

// UpdateOpportunity 更新线索/商机；阶段推进合法且发事件（won/lost 为终态）。
// sc 非 nil 且受限时，范围外商机 403，且不允许把所属客户改到范围外。
func (s *Service) UpdateOpportunity(ctx context.Context, sc *datascope.Scope, id idgen.ID, in OpportunityInput) (*Opportunity, error) {
	if err := in.normalize(); err != nil {
		return nil, err
	}
	if _, err := s.visibleOpportunity(ctx, sc, id); err != nil {
		return nil, err
	}
	var customerName string
	if in.CustomerID != 0 {
		cu, err := s.visibleCustomer(ctx, sc, in.CustomerID)
		if err != nil {
			return nil, err
		}
		customerName = cu.Name
	}
	if in.ContactID != 0 {
		if in.CustomerID == 0 {
			return nil, response.BadRequest("crm.contact_customer_mismatch")
		}
		if err := s.contactInCustomer(ctx, in.ContactID, in.CustomerID); err != nil {
			return nil, err
		}
	}
	var o Opportunity
	var fromStage string
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Take(&o, id).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return response.NotFound("crm.not_found")
			}
			return err
		}
		fromStage = o.Stage
		if isFinalStage(o.Stage) && o.Stage != in.Stage {
			return response.Conflict("crm.stage_final")
		}
		updates := map[string]any{
			"customer_id": in.CustomerID,
			"contact_id":  in.ContactID,
			"title":       in.Title, "stage": in.Stage, "amount_minor": in.AmountMinor,
			"currency": in.Currency, "expected_at": truncate(in.ExpectedAt, 10),
			"owner": truncate(in.Owner, 64), "remark": truncate(in.Remark, 500),
		}
		if err := tx.Model(&Opportunity{}).Where("id = ?", int64(id)).Updates(updates).Error; err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	o.Title, o.Stage, o.AmountMinor, o.Currency = in.Title, in.Stage, in.AmountMinor, in.Currency
	o.ExpectedAt, o.Owner, o.Remark = truncate(in.ExpectedAt, 10), truncate(in.Owner, 64), truncate(in.Remark, 500)
	// PUT 是全量更新：清空所属客户也要落库（表单提供「暂不关联客户」）。
	o.CustomerID, o.CustomerName = in.CustomerID, customerName
	o.ContactID = in.ContactID
	if fromStage != in.Stage {
		s.publishOppStage(ctx, &o, fromStage)
	}
	return &o, nil
}

func isFinalStage(stage string) bool { return stage == StageWon || stage == StageLost }

// DeleteOpportunity 删除线索/商机（sc 非 nil 且受限时，范围外 403）。
func (s *Service) DeleteOpportunity(ctx context.Context, sc *datascope.Scope, id idgen.ID) error {
	if _, err := s.visibleOpportunity(ctx, sc, id); err != nil {
		return err
	}
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// 跟进记录必挂客户、可选挂商机：商机删掉后把 opportunity_id 归零，记录本身留在客户
		// 时间线上，不留指向已删商机的悬空引用（那条跟进在时间轴里会显示成「无商机」而不是报错）。
		if err := tx.Model(&Activity{}).Where("opportunity_id = ?", int64(id)).
			Update("opportunity_id", 0).Error; err != nil {
			return err
		}
		return tx.Delete(&Opportunity{}, id).Error
	})
}

// GetOpportunity 按 ID 取线索/商机（sc 非 nil 且受限时，范围外 403）。
func (s *Service) GetOpportunity(ctx context.Context, sc *datascope.Scope, id idgen.ID) (*Opportunity, error) {
	o, err := s.visibleOpportunity(ctx, sc, id)
	if err != nil {
		return nil, err
	}
	brief := s.customerBrief(ctx, o.CustomerID)
	o.CustomerName, o.CustomerType = brief.Name, brief.Type
	return o, nil
}

// customerBrief 取客户的名与类型；客户已被删除时返回零值，不影响商机本身可读。
func (s *Service) customerBrief(ctx context.Context, id idgen.ID) Customer {
	if id == 0 {
		return Customer{}
	}
	var c Customer
	if err := s.db.WithContext(ctx).Select("id", "name", "type").Take(&c, id).Error; err != nil {
		return Customer{}
	}
	return c
}

// fillCustomerBrief 批量回填客户名与类型，列表一页只多一次查询。
func (s *Service) fillCustomerBrief(ctx context.Context, rows []Opportunity) error {
	ids := make([]int64, 0, len(rows))
	seen := make(map[int64]bool, len(rows))
	for _, o := range rows {
		id := int64(o.CustomerID)
		if id != 0 && !seen[id] {
			seen[id] = true
			ids = append(ids, id)
		}
	}
	if len(ids) == 0 {
		return nil
	}
	var customers []Customer
	if err := s.db.WithContext(ctx).Select("id", "name", "type").Where("id IN ?", ids).Find(&customers).Error; err != nil {
		return err
	}
	briefs := make(map[int64]Customer, len(customers))
	for _, c := range customers {
		briefs[int64(c.ID)] = c
	}
	for i := range rows {
		brief := briefs[int64(rows[i].CustomerID)]
		rows[i].CustomerName, rows[i].CustomerType = brief.Name, brief.Type
	}
	return nil
}

// OpportunityQuery 过滤线索/商机列表。Scope 由 HTTP 入口解析后传入；nil 表示不裁剪（内部调用）。
type OpportunityQuery struct {
	Keyword    string
	CustomerID idgen.ID
	Stage      string
	Scope      *datascope.Scope
	Mine       bool // 只看我的：owner = 当前账号（与角色范围叠加）
	Page       int
	PageSize   int
}

// ListOpportunities 分页列出线索/商机。受限操作者按 owner（账号）+ 所属部门裁剪。
func (s *Service) ListOpportunities(ctx context.Context, q OpportunityQuery) ([]Opportunity, int64, error) {
	db := s.db.WithContext(ctx).Model(&Opportunity{})
	if kw := strings.TrimSpace(q.Keyword); kw != "" {
		db = db.Where("LOWER(title) LIKE LOWER(?)", "%"+kw+"%")
	}
	if q.CustomerID != 0 {
		db = db.Where("customer_id = ?", q.CustomerID)
	}
	if q.Stage != "" {
		db = db.Where("stage = ?", q.Stage)
	}
	if cond, args, limited := q.Scope.UsernameCond("owner"); limited {
		db = db.Where(cond, args...)
	}
	if q.Mine {
		_, name, ok := auth.UserFromContext(ctx)
		if !ok {
			return nil, 0, response.Unauthorized("error.unauthorized")
		}
		db = db.Where("owner = ?", name)
	}
	var total int64
	if err := db.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var rows []Opportunity
	if err := db.Order("id DESC").Offset((q.Page - 1) * q.PageSize).Limit(q.PageSize).Find(&rows).Error; err != nil {
		return nil, 0, err
	}
	if err := s.fillCustomerBrief(ctx, rows); err != nil {
		return nil, 0, err
	}
	return rows, total, nil
}

// truncate 按字符数裁剪：三引擎的 VARCHAR(n) 数的都是字符，不是字节。
// 按字节切会把汉字切成半个码点，MySQL 以 1366 Incorrect string value 拒掉整行——
// 中文备注、地址这类字段一定会踩到。
func truncate(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n])
}

// customerCode 由雪花 ID 派生客户编码（C- 前缀 + 36 进制）：ID 全局唯一且不可变，
// 编码因此天然唯一、稳定，生成时也不用查库或抢序列号。
func customerCode(id idgen.ID) string {
	return "C-" + strings.ToUpper(strconv.FormatInt(int64(id), 36))
}
