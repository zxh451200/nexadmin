package crm

import (
	"context"
	"errors"
	"strconv"
	"strings"

	"gorm.io/gorm"

	"nexadmin/core/response"
	"nexadmin/pkg/idgen"
)

// 编码/等级的上限与列宽一致。
const (
	codeMaxLen  = 64
	levelMaxLen = 16
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
	ExternalSource string
	ExternalNo     string
	Owner          string
	Phone          string
	Email          string
	Remark         string
}

func (in *CustomerInput) normalize() error {
	in.Name = strings.TrimSpace(in.Name)
	in.Code = truncate(strings.TrimSpace(in.Code), codeMaxLen)
	// 等级是短档位（A/B/C），统一大写，避免 "a" 与 "A" 各算一档。
	in.Level = truncate(strings.ToUpper(strings.TrimSpace(in.Level)), levelMaxLen)
	in.ExternalSource, in.ExternalNo = strings.TrimSpace(in.ExternalSource), strings.TrimSpace(in.ExternalNo)
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
func (s *Service) CreateCustomer(ctx context.Context, in CustomerInput) (*Customer, error) {
	if err := in.normalize(); err != nil {
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
		Name: in.Name, Code: in.Code, Level: in.Level, Status: in.Status,
		Owner: truncate(in.Owner, 64), Phone: truncate(in.Phone, 32),
		Email: truncate(in.Email, 128), Remark: truncate(in.Remark, 500),
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
func (s *Service) UpdateCustomer(ctx context.Context, id idgen.ID, in CustomerInput) (*Customer, error) {
	if err := in.normalize(); err != nil {
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
	updates := map[string]any{
		"name": in.Name, "code": in.Code, "level": in.Level, "status": in.Status,
		"owner": truncate(in.Owner, 64), "phone": truncate(in.Phone, 32),
		"email": truncate(in.Email, 128), "remark": truncate(in.Remark, 500),
	}
	if err := s.db.WithContext(ctx).Model(&Customer{}).Where("id = ?", int64(id)).Updates(updates).Error; err != nil {
		return nil, err
	}
	c.Name, c.Code, c.Level, c.Status = in.Name, in.Code, in.Level, in.Status
	c.Owner, c.Phone, c.Email, c.Remark = updates["owner"].(string), updates["phone"].(string), updates["email"].(string), updates["remark"].(string)
	s.publishCustomer(ctx, EventCustomerUpdated, &c)
	return &c, nil
}

// DeleteCustomer 删除客户主数据。还有商机挂在该客户下时拒绝（主数据不级联消失）。
func (s *Service) DeleteCustomer(ctx context.Context, id idgen.ID) error {
	var n int64
	if err := s.db.WithContext(ctx).Model(&Opportunity{}).Where("customer_id = ?", int64(id)).Count(&n).Error; err != nil {
		return err
	}
	if n > 0 {
		return response.Conflict("crm.customer_in_use")
	}
	return s.db.WithContext(ctx).Delete(&Customer{}, id).Error
}

// GetCustomer 按 ID 取客户。
func (s *Service) GetCustomer(ctx context.Context, id idgen.ID) (*Customer, error) {
	var c Customer
	if err := s.db.WithContext(ctx).Take(&c, id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, response.NotFound("crm.not_found")
		}
		return nil, err
	}
	return &c, nil
}

// CustomerQuery 过滤客户列表。
type CustomerQuery struct {
	Keyword  string // 名称 / 编码 / 锚点
	Status   string
	Source   string
	Page     int
	PageSize int
}

// ListCustomers 分页列出客户。
func (s *Service) ListCustomers(ctx context.Context, q CustomerQuery) ([]Customer, int64, error) {
	db := s.db.WithContext(ctx).Model(&Customer{})
	if kw := strings.TrimSpace(q.Keyword); kw != "" {
		like := "%" + kw + "%"
		db = db.Where("name LIKE ? OR code LIKE ? OR external_no LIKE ?", like, like, like)
	}
	if q.Status != "" {
		db = db.Where("status = ?", q.Status)
	}
	if q.Source != "" {
		db = db.Where("external_source = ?", q.Source)
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
	CustomerID  int64
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

// CreateOpportunity 建一条线索/商机（stage=lead 即线索）。
func (s *Service) CreateOpportunity(ctx context.Context, in OpportunityInput) (*Opportunity, error) {
	if err := in.normalize(); err != nil {
		return nil, err
	}
	if in.CustomerID != 0 {
		if _, err := s.GetCustomer(ctx, idgen.ID(in.CustomerID)); err != nil {
			return nil, err
		}
	}
	o := &Opportunity{
		CustomerID: in.CustomerID, Title: in.Title, Stage: in.Stage,
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
func (s *Service) UpdateOpportunity(ctx context.Context, id idgen.ID, in OpportunityInput) (*Opportunity, error) {
	if err := in.normalize(); err != nil {
		return nil, err
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
			"title": in.Title, "stage": in.Stage, "amount_minor": in.AmountMinor,
			"currency": in.Currency, "expected_at": truncate(in.ExpectedAt, 10),
			"owner": truncate(in.Owner, 64), "remark": truncate(in.Remark, 500),
		}
		if in.CustomerID != 0 {
			updates["customer_id"] = in.CustomerID
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
	if in.CustomerID != 0 {
		o.CustomerID = in.CustomerID
	}
	if fromStage != in.Stage {
		s.publishOppStage(ctx, &o, fromStage)
	}
	return &o, nil
}

func isFinalStage(stage string) bool { return stage == StageWon || stage == StageLost }

// DeleteOpportunity 删除线索/商机。
func (s *Service) DeleteOpportunity(ctx context.Context, id idgen.ID) error {
	return s.db.WithContext(ctx).Delete(&Opportunity{}, id).Error
}

// GetOpportunity 按 ID 取线索/商机。
func (s *Service) GetOpportunity(ctx context.Context, id idgen.ID) (*Opportunity, error) {
	var o Opportunity
	if err := s.db.WithContext(ctx).Take(&o, id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, response.NotFound("crm.not_found")
		}
		return nil, err
	}
	return &o, nil
}

// OpportunityQuery 过滤线索/商机列表。
type OpportunityQuery struct {
	Keyword    string
	CustomerID int64
	Stage      string
	Page       int
	PageSize   int
}

// ListOpportunities 分页列出线索/商机。
func (s *Service) ListOpportunities(ctx context.Context, q OpportunityQuery) ([]Opportunity, int64, error) {
	db := s.db.WithContext(ctx).Model(&Opportunity{})
	if kw := strings.TrimSpace(q.Keyword); kw != "" {
		db = db.Where("title LIKE ?", "%"+kw+"%")
	}
	if q.CustomerID != 0 {
		db = db.Where("customer_id = ?", q.CustomerID)
	}
	if q.Stage != "" {
		db = db.Where("stage = ?", q.Stage)
	}
	var total int64
	if err := db.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var rows []Opportunity
	if err := db.Order("id DESC").Offset((q.Page - 1) * q.PageSize).Limit(q.PageSize).Find(&rows).Error; err != nil {
		return nil, 0, err
	}
	return rows, total, nil
}

func truncate(s string, n int) string {
	if len(s) > n {
		return s[:n]
	}
	return s
}

// customerCode 由雪花 ID 派生客户编码（C- 前缀 + 36 进制）：ID 全局唯一且不可变，
// 编码因此天然唯一、稳定，生成时也不用查库或抢序列号。
func customerCode(id idgen.ID) string {
	return "C-" + strings.ToUpper(strconv.FormatInt(int64(id), 36))
}
