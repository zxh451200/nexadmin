package crm

// 联系人：一个客户多个联系人，is_primary 标主联系人（客户内最多一个，
// 由这里的事务保证；不做数据库唯一约束，切换主联系人就是两条 UPDATE）。

import (
	"context"
	"errors"
	"strings"

	"gorm.io/gorm"

	"nexadmin/core/datascope"
	"nexadmin/core/response"
	"nexadmin/pkg/idgen"
)

// ContactInput 是联系入参。
type ContactInput struct {
	Name      string
	Title     string
	Phone     string
	Email     string
	IsPrimary bool
	Remark    string
}

func (in *ContactInput) normalize() error {
	in.Name = strings.TrimSpace(in.Name)
	in.Title = truncate(strings.TrimSpace(in.Title), 100)
	in.Phone = truncate(strings.TrimSpace(in.Phone), 32)
	in.Email = truncate(strings.TrimSpace(in.Email), 128)
	in.Remark = truncate(strings.TrimSpace(in.Remark), 500)
	if in.Name == "" {
		return response.BadRequest("crm.contact_name_required")
	}
	return nil
}

// ListContacts 客户下的全部联系人（数量小，不分页）；主联系人排最前。
func (s *Service) ListContacts(ctx context.Context, sc *datascope.Scope, customerID idgen.ID) ([]Contact, error) {
	if _, err := s.visibleCustomer(ctx, sc, customerID); err != nil {
		return nil, err
	}
	var rows []Contact
	err := s.db.WithContext(ctx).Where("customer_id = ?", int64(customerID)).
		Order("is_primary DESC, id ASC").Find(&rows).Error
	return rows, err
}

// CreateContact 创建联系人；指定主联系人时在事务里把原主联系人降级。
func (s *Service) CreateContact(ctx context.Context, sc *datascope.Scope, customerID idgen.ID, in ContactInput) (*Contact, error) {
	if err := in.normalize(); err != nil {
		return nil, err
	}
	if _, err := s.visibleCustomer(ctx, sc, customerID); err != nil {
		return nil, err
	}
	c := &Contact{
		CustomerID: customerID, Name: in.Name, Title: in.Title,
		Phone: in.Phone, Email: in.Email, IsPrimary: in.IsPrimary, Remark: in.Remark,
	}
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if in.IsPrimary {
			if err := tx.Model(&Contact{}).Where("customer_id = ?", int64(customerID)).
				Update("is_primary", false).Error; err != nil {
				return err
			}
		}
		return tx.Create(c).Error
	})
	if err != nil {
		return nil, err
	}
	return c, nil
}

// UpdateContact 更新联系人；主联系人变更在事务里完成降级与提升。
func (s *Service) UpdateContact(ctx context.Context, sc *datascope.Scope, id idgen.ID, in ContactInput) (*Contact, error) {
	if err := in.normalize(); err != nil {
		return nil, err
	}
	var ct Contact
	if err := s.db.WithContext(ctx).Take(&ct, id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, response.NotFound("crm.not_found")
		}
		return nil, err
	}
	if _, err := s.visibleCustomer(ctx, sc, ct.CustomerID); err != nil {
		return nil, err
	}
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if in.IsPrimary && !ct.IsPrimary {
			if err := tx.Model(&Contact{}).Where("customer_id = ?", int64(ct.CustomerID)).
				Update("is_primary", false).Error; err != nil {
				return err
			}
		}
		updates := map[string]any{
			"name": in.Name, "title": in.Title, "phone": in.Phone,
			"email": in.Email, "is_primary": in.IsPrimary, "remark": in.Remark,
		}
		return tx.Model(&Contact{}).Where("id = ?", int64(id)).Updates(updates).Error
	})
	if err != nil {
		return nil, err
	}
	ct.Name, ct.Title, ct.Phone, ct.Email, ct.IsPrimary, ct.Remark =
		in.Name, in.Title, in.Phone, in.Email, in.IsPrimary, in.Remark
	return &ct, nil
}

// DeleteContact 删除联系人。删掉主联系人后客户暂时没有主联系人，是合法状态。
func (s *Service) DeleteContact(ctx context.Context, sc *datascope.Scope, id idgen.ID) error {
	var ct Contact
	if err := s.db.WithContext(ctx).Take(&ct, id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return response.NotFound("crm.not_found")
		}
		return err
	}
	if _, err := s.visibleCustomer(ctx, sc, ct.CustomerID); err != nil {
		return err
	}
	return s.db.WithContext(ctx).Delete(&Contact{}, id).Error
}
