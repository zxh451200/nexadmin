package crm

// 合并客户：把重复档案并进一个主客户。取向：
// - 主客户是显式选择（mergeInto），不从「谁新谁旧」猜——销售最清楚哪份资料是真的；
// - 从客户的联系人、跟进、商机整体迁到主客户名下，商机照旧归其原 owner（跟进跟着客户走）；
// - 主客户空着的字段用从客户的补上（名称、归属、电话等），不覆盖已有值——补空不覆盖是合并的默认安全语义；
// - 从客户删除（复用级联）。整个过程一个事务：并一半失败就整体回滚，不留半个客户两份档案。

import (
	"context"
	"errors"

	"gorm.io/gorm"

	"nexadmin/core/datascope"
	"nexadmin/core/response"
	"nexadmin/pkg/idgen"
)

// MergeInput 是一次合并请求：把 Source 并进 Target。
type MergeInput struct {
	Target idgen.ID `json:"targetId"` // 主客户（保留方）
	Source idgen.ID `json:"sourceId"` // 从客户（被并方，删除）
}

// MergeCustomers 把 source 并入 target：关联记录整体迁移、主客户补空字段、删除从客户。
// 可见性按数据范围：两个客户都要在操作者的范围内才允许合并。
func (s *Service) MergeCustomers(ctx context.Context, sc *datascope.Scope, in MergeInput) (*Customer, error) {
	if in.Target == 0 || in.Source == 0 {
		return nil, response.BadRequest("crm.merge_ids_required")
	}
	if in.Target == in.Source {
		return nil, response.BadRequest("crm.merge_self")
	}
	if _, err := s.visibleCustomer(ctx, sc, in.Target); err != nil {
		return nil, err
	}
	if _, err := s.visibleCustomer(ctx, sc, in.Source); err != nil {
		return nil, err
	}

	var merged *Customer
	var mergedFromName, mergedFromCode string
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var target, source Customer
		if err := tx.Take(&target, int64(in.Target)).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return response.NotFound("crm.not_found")
			}
			return err
		}
		if err := tx.Take(&source, int64(in.Source)).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return response.NotFound("crm.not_found")
			}
			return err
		}
		// 关联记录整体迁移：联系人、跟进、商机都挂在 customer_id 上。
		// is_primary 的「客户内最多一个」由服务层语义保证：从客户的主联系人迁过去后
		// 如果主客户已有主联系人，把迁来的降级为普通联系人。
		if err := tx.Model(&Contact{}).Where("customer_id = ?", int64(in.Source)).
			Update("customer_id", int64(in.Target)).Error; err != nil {
			return err
		}
		if err := tx.Model(&Activity{}).Where("customer_id = ?", int64(in.Source)).
			Update("customer_id", int64(in.Target)).Error; err != nil {
			return err
		}
		if err := tx.Model(&Opportunity{}).Where("customer_id = ?", int64(in.Source)).
			Update("customer_id", int64(in.Target)).Error; err != nil {
			return err
		}
		if err := demoteExtraPrimary(tx, in.Target); err != nil {
			return err
		}
		// 主客户空着的字段用从客户的补上：先到的主数据说了算，缺失的细节不丢。
		fills := mergeFills(&target, &source)
		// 标签做并集：两边都不丢词（不覆盖、不替换，单客户 10 个上限由 normalize 兜住）。
		unionTags, err := normalizeTags(append(splitTags(target.Tags), splitTags(source.Tags)...))
		if err != nil {
			return err
		}
		if canonicalTags(unionTags) != target.Tags {
			fills["tags"] = canonicalTags(unionTags)
		}
		if len(fills) > 0 {
			if err := tx.Model(&Customer{}).Where("id = ?", int64(in.Target)).Updates(fills).Error; err != nil {
				return err
			}
		}
		// 从客户删除：数据（联系人/跟进/商机/标签）已整体迁走，这里必须物理删——
		// GORM 会把普通 Delete 翻译成软删除，被并方会变成回收站里的一具空壳，Unscoped 绕开。
		if err := tx.Unscoped().Delete(&Customer{}, int64(in.Source)).Error; err != nil {
			return err
		}
		// 留痕要的从客户身份先记下（行马上就删了）；留痕本身在事务提交后写，
		// 不让一条「合并了」的跟进挂在可能回滚的事务外面变成孤儿。
		mergedFromName, mergedFromCode = source.Name, source.Code
		if err := tx.Take(&target, int64(in.Target)).Error; err != nil {
			return err
		}
		merged = &target
		return nil
	})
	if err != nil {
		return nil, err
	}
	s.noteSystem(ctx, in.Target, "合并了客户「"+mergedFromName+"」（"+mergedFromCode+"）的资料与关联记录")
	s.publishCustomer(ctx, EventCustomerUpdated, merged)
	return merged, nil
}

// demoteExtraPrimary 保证主客户仍只有一条主联系人：迁入的 is_primary 与原有的冲突时，
// 保留最早创建的那条，其余降级。
func demoteExtraPrimary(tx *gorm.DB, customerID idgen.ID) error {
	var primaries []Contact
	if err := tx.Where("customer_id = ? AND is_primary = ?", int64(customerID), true).
		Order("id").Find(&primaries).Error; err != nil {
		return err
	}
	if len(primaries) <= 1 {
		return nil
	}
	ids := make([]int64, 0, len(primaries)-1)
	for _, p := range primaries[1:] {
		ids = append(ids, int64(p.ID))
	}
	return tx.Model(&Contact{}).Where("id IN ?", ids).Update("is_primary", false).Error
}

// mergeFills 找出主客户空着而从客户有值的字段：补空不覆盖。
// 类型化的身份字段只在同类型下互填——个人客户不会用企业客户的信用代码补空。
func mergeFills(target, source *Customer) map[string]any {
	fills := map[string]any{}
	pick := func(col, tv, sv string) {
		if tv == "" && sv != "" {
			fills[col] = sv
		}
	}
	pick("level", target.Level, source.Level)
	pick("owner", target.Owner, source.Owner)
	pick("phone", target.Phone, source.Phone)
	pick("email", target.Email, source.Email)
	pick("remark", target.Remark, source.Remark)
	if target.Type == source.Type {
		if target.Type == TypePerson {
			pick("nationality", target.Nationality, source.Nationality)
			pick("id_type", target.IdType, source.IdType)
			pick("id_no", target.IdNo, source.IdNo)
		} else {
			pick("tax_no", target.TaxNo, source.TaxNo)
			pick("industry", target.Industry, source.Industry)
			pick("address", target.Address, source.Address)
		}
	}
	pick("country", target.Country, source.Country)
	if target.ExternalSource == nil && source.ExternalSource != nil {
		// 锚点是宿主体系的稳定锚：从客户带着锚点并入时锚点跟着迁移，否则外部系统反查不到。
		fills["external_source"] = *source.ExternalSource
		if source.ExternalNo != nil {
			fills["external_no"] = *source.ExternalNo
		}
	}
	return fills
}
