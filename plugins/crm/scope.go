package crm

// 数据范围：客户/商机的 owner 是账号列，复用 core/datascope 的 UsernameCond。
// 约定与框架一致——HTTP 入口必须先解析范围；nil 的 *Scope 表示不裁剪，
// 只给内部调用方用（client.go 的 member 对接不走 HTTP，因此不受影响）。

import (
	"context"
	"errors"

	"gorm.io/gorm"

	"nexadmin/core/auth"
	"nexadmin/core/datascope"
	"nexadmin/core/response"
	"nexadmin/pkg/idgen"
)

// scopeFromContext 解析当前请求者的数据范围；无登录身份由 FromClaims 返回 401。
func (s *Service) scopeFromContext(ctx context.Context) (*datascope.Scope, error) {
	return datascope.FromClaims(ctx, s.db, auth.ClaimsFromContext(ctx))
}

// visibleCustomer 按 ID 取客户并做范围校验：受限操作者对范围外客户得到 403，
// 与 datascope.Forbidden 的防枚举语义一致（联系人与跟进都继承客户的可见性）。
func (s *Service) visibleCustomer(ctx context.Context, sc *datascope.Scope, id idgen.ID) (*Customer, error) {
	var c Customer
	if err := s.db.WithContext(ctx).Take(&c, id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, response.NotFound("crm.not_found")
		}
		return nil, err
	}
	if cond, args, limited := sc.UsernameCond("owner"); limited {
		var n int64
		if err := s.db.WithContext(ctx).Model(&Customer{}).
			Where("id = ?", int64(id)).Where(cond, args...).Count(&n).Error; err != nil {
			return nil, err
		}
		if n == 0 {
			return nil, datascope.Forbidden(ctx, "datascope.resource.customer", 1)
		}
	}
	return &c, nil
}

// visibleOpportunity 对商机单条读写做范围校验（按商机自己的 owner 列裁剪）。
func (s *Service) visibleOpportunity(ctx context.Context, sc *datascope.Scope, id idgen.ID) (*Opportunity, error) {
	var o Opportunity
	if err := s.db.WithContext(ctx).Take(&o, id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, response.NotFound("crm.not_found")
		}
		return nil, err
	}
	if cond, args, limited := sc.UsernameCond("owner"); limited {
		var n int64
		if err := s.db.WithContext(ctx).Model(&Opportunity{}).
			Where("id = ?", int64(id)).Where(cond, args...).Count(&n).Error; err != nil {
			return nil, err
		}
		if n == 0 {
			return nil, datascope.Forbidden(ctx, "datascope.resource.customer", 1)
		}
	}
	return &o, nil
}
