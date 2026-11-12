package crm

// 公海池：owner 为空的客户对全员开放。设计取向：
// - 不新建表：Owner 空 = 在公海，创建客户不填归属即自然入池；
// - 领取用条件 UPDATE（owner = '' 才生效）：两人同时抢同一客户，后到者拿到 409 而不是悄悄覆盖；
// - 释放走数据范围：只有看得到这个客户的人能把它放入公海；
// - 公海列表不做数据范围：无主客户不属于任何人，受限账号也看得到——这正是公海的意义。

import (
	"context"
	"strings"
	"time"

	"gorm.io/gorm"

	"nexadmin/core/auth"
	"nexadmin/core/datascope"
	"nexadmin/core/response"
	"nexadmin/pkg/idgen"
)

// applyCustomerFilter 是客户列表/公海/导出共用的业务筛选（关键词与类型/状态/来源/等级/标签/建档时间）。
// 数据范围与「只看我的」是调用方的语义，不在这里。
func applyCustomerFilter(db *gorm.DB, q CustomerQuery) *gorm.DB {
	if kw := strings.TrimSpace(q.Keyword); kw != "" {
		// 拿手机号/邮箱找客户与拿证件号找人同为高频：联系人字段一并进 LIKE。
		like := "%" + kw + "%"
		db = db.Where("LOWER(name) LIKE LOWER(?) OR LOWER(code) LIKE LOWER(?) OR LOWER(external_no) LIKE LOWER(?) OR LOWER(id_no) LIKE LOWER(?) OR LOWER(tax_no) LIKE LOWER(?) OR LOWER(phone) LIKE LOWER(?) OR LOWER(email) LIKE LOWER(?)",
			like, like, like, like, like, like, like)
	}
	if len(q.Type) > 0 {
		db = db.Where("type IN ?", q.Type)
	}
	if len(q.Status) > 0 {
		db = db.Where("status IN ?", q.Status)
	}
	if len(q.Source) > 0 {
		db = db.Where("external_source IN ?", q.Source)
	}
	if len(q.Level) > 0 {
		db = db.Where("level IN ?", q.Level)
	}
	// 标签按逗号边界匹配：存储是「,a,b,」，查「vip」时 LIKE '%,vip,%' 不会命中 viptag。
	// 两侧 LOWER 折叠大小写（PG 的 LIKE 区分大小写；标签词大小写本来就不承载语义）。
	// 多标签取并集（OR）：含任一所选标签即命中。
	if len(q.Tag) > 0 {
		conds, args := make([]string, 0, len(q.Tag)), make([]any, 0, len(q.Tag))
		for _, tag := range q.Tag {
			if tag = strings.TrimSpace(tag); tag == "" {
				continue
			}
			conds = append(conds, "LOWER(tags) LIKE LOWER(?)")
			args = append(args, "%,"+tag+",%")
		}
		if len(conds) > 0 {
			db = db.Where("("+strings.Join(conds, " OR ")+")", args...)
		}
	}
	// 建档时间范围：to 按天取，含端点（< to+1day）。
	if q.CreatedFrom != "" {
		if t, err := time.Parse("2006-01-02", q.CreatedFrom); err == nil {
			db = db.Where("created_at >= ?", t)
		}
	}
	if q.CreatedTo != "" {
		if t, err := time.Parse("2006-01-02", q.CreatedTo); err == nil {
			db = db.Where("created_at < ?", t.AddDate(0, 0, 1))
		}
	}
	return db
}

// ListPool 分页列出公海客户（owner 为空）。公海对持 crm:list 的所有人可见，
// 不叠加数据范围；筛选与客户列表一致。
func (s *Service) ListPool(ctx context.Context, q CustomerQuery) ([]Customer, int64, error) {
	q.Type = lowerAll(q.Type)
	for _, t := range q.Type {
		if !TypeSet[t] {
			return nil, 0, response.BadRequest("crm.type_invalid")
		}
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
	s.noteSystem(ctx, id, "从公海领取客户")
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
	previousOwner := c.Owner
	if err := s.db.WithContext(ctx).Model(&Customer{}).Where("id = ?", int64(id)).Update("owner", "").Error; err != nil {
		return nil, err
	}
	c.Owner = ""
	s.noteSystem(ctx, id, "释放回公海（原归属："+previousOwner+"）")
	return c, nil
}
