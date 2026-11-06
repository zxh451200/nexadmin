package crm

// 公海池：owner 为空的客户对全员开放。设计取向：
// - 不新建表：Owner 空 = 在公海，创建客户不填归属即自然入池；
// - 领取用条件 UPDATE（owner = '' 才生效）：两人同时抢同一客户，后到者拿到 409 而不是悄悄覆盖；
// - 释放走数据范围：只有看得到这个客户的人能把它放入公海；
// - 公海列表不做数据范围：无主客户不属于任何人，受限账号也看得到——这正是公海的意义。

import (
	"context"
	"strings"

	"gorm.io/gorm"

	"nexadmin/core/auth"
	"nexadmin/core/datascope"
	"nexadmin/core/response"
	"nexadmin/pkg/idgen"
)

// applyCustomerFilter 是客户列表/公海/导出共用的业务筛选（关键词与类型/状态/来源）。
// 数据范围与「只看我的」是调用方的语义，不在这里。
func applyCustomerFilter(db *gorm.DB, q CustomerQuery) *gorm.DB {
	if kw := strings.TrimSpace(q.Keyword); kw != "" {
		// 拿手机号/邮箱找客户与拿证件号找人同为高频：联系人字段一并进 LIKE。
		like := "%" + kw + "%"
		db = db.Where("LOWER(name) LIKE LOWER(?) OR LOWER(code) LIKE LOWER(?) OR LOWER(external_no) LIKE LOWER(?) OR LOWER(id_no) LIKE LOWER(?) OR LOWER(tax_no) LIKE LOWER(?) OR LOWER(phone) LIKE LOWER(?) OR LOWER(email) LIKE LOWER(?)",
			like, like, like, like, like, like, like)
	}
	if q.Type != "" {
		db = db.Where("type = ?", q.Type)
	}
	if q.Status != "" {
		db = db.Where("status = ?", q.Status)
	}
	if q.Source != "" {
		db = db.Where("external_source = ?", q.Source)
	}
	return db
}

// ListPool 分页列出公海客户（owner 为空）。公海对持 crm:list 的所有人可见，
// 不叠加数据范围；筛选与客户列表一致。
func (s *Service) ListPool(ctx context.Context, q CustomerQuery) ([]Customer, int64, error) {
	q.Type = strings.ToLower(q.Type)
	if q.Type != "" && !TypeSet[q.Type] {
		return nil, 0, response.BadRequest("crm.type_invalid")
	}
	db := applyCustomerFilter(s.db.WithContext(ctx).Model(&Customer{}).Where("owner = ''"), q)
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

// ClaimCustomer 从公海领取客户：owner 改为当前账号。条件 UPDATE 保证两人同时
// 抢同一客户时只有一个成功；rows=0 时区分「不存在」与「已被领走」两种结果。
func (s *Service) ClaimCustomer(ctx context.Context, id idgen.ID) (*Customer, error) {
	_, name, ok := auth.UserFromContext(ctx)
	if !ok {
		return nil, response.Unauthorized("error.unauthorized")
	}
	res := s.db.WithContext(ctx).Model(&Customer{}).
		Where("id = ? AND owner = ''", int64(id)).
		Update("owner", name)
	if res.Error != nil {
		return nil, res.Error
	}
	if res.RowsAffected == 0 {
		var n int64
		if err := s.db.WithContext(ctx).Model(&Customer{}).Where("id = ?", int64(id)).Count(&n).Error; err != nil {
			return nil, err
		}
		if n == 0 {
			return nil, response.NotFound("crm.not_found")
		}
		return nil, response.Conflict("crm.pool_taken")
	}
	var c Customer
	if err := s.db.WithContext(ctx).Take(&c, id).Error; err != nil {
		return nil, err
	}
	return &c, nil
}

// ReleaseCustomer 把客户放回公海（owner 清空）。可见性按数据范围校验：
// 受限账号只能释放自己范围内的客户。已在公海视为幂等成功。
func (s *Service) ReleaseCustomer(ctx context.Context, sc *datascope.Scope, id idgen.ID) (*Customer, error) {
	c, err := s.visibleCustomer(ctx, sc, id)
	if err != nil {
		return nil, err
	}
	if c.Owner == "" {
		return c, nil
	}
	if err := s.db.WithContext(ctx).Model(&Customer{}).Where("id = ?", int64(id)).Update("owner", "").Error; err != nil {
		return nil, err
	}
	c.Owner = ""
	return c, nil
}
