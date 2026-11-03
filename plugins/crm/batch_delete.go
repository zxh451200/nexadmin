package crm

// 批量删除：语义与单条删除逐条对齐，只是把「一条一条点」换成「一次点名」。
// 三条硬规则：
//   - ids 由 response.ParseIDs 校验，空或非法直接 400，绝不落到「无条件全表删」；
//   - 数据范围外的 id 不删（与 visibleCustomer 的 403 同口径，只是批量不报错，计入 blocked）；
//   - 仍挂商机的客户不删（与单条的 409 crm.customer_in_use 同口径），整批不因它失败，
//     除非一个都删不了——那时才按单条的口径返回 404/409，让调用方知道白点了一趟。

import (
	"context"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"nexadmin/core/datascope"
	"nexadmin/core/response"
	"nexadmin/pkg/idgen"
)

// scopedIDs 返回 ids 中当前操作者数据范围可见的那部分（owner 列裁剪，与单条校验同源）。
func (s *Service) scopedIDs(ctx context.Context, sc *datascope.Scope, dest any, ids []idgen.ID) ([]idgen.ID, error) {
	tx := s.db.WithContext(ctx).Model(dest).Where("id IN ?", ids)
	if cond, args, limited := sc.UsernameCond("owner"); limited {
		tx = tx.Where(cond, args...)
	}
	var got []idgen.ID
	if err := tx.Pluck("id", &got).Error; err != nil {
		return nil, err
	}
	return got, nil
}

// customerIDsInUse 返回 ownable 中仍有商机的客户 ID（与 DeleteCustomer 的占用判定同源）。
func (s *Service) customerIDsInUse(ctx context.Context, ownable []idgen.ID) (map[idgen.ID]bool, error) {
	out := map[idgen.ID]bool{}
	var got []idgen.ID
	if err := s.db.WithContext(ctx).Model(&Opportunity{}).
		Where("customer_id IN ?", ownable).Distinct().Pluck("customer_id", &got).Error; err != nil {
		return nil, err
	}
	for _, id := range got {
		out[id] = true
	}
	return out, nil
}

// exclude 丢掉 keep 里点名的行，顺序保持不变。
func exclude(ids []idgen.ID, skip map[idgen.ID]bool) []idgen.ID {
	out := make([]idgen.ID, 0, len(ids))
	for _, id := range ids {
		if !skip[id] {
			out = append(out, id)
		}
	}
	return out
}

// DeleteCustomers 批量删除客户，删掉的连同其联系人与跟进记录一起清（单条事务的批量版）。
func (s *Service) DeleteCustomers(ctx context.Context, sc *datascope.Scope, ids []idgen.ID) (deleted, blocked int64, err error) {
	ownable, err := s.scopedIDs(ctx, sc, &Customer{}, ids)
	if err != nil {
		return 0, 0, err
	}
	if len(ownable) == 0 {
		return 0, int64(len(ids)), response.NotFound("crm.not_found")
	}
	inUse, err := s.customerIDsInUse(ctx, ownable)
	if err != nil {
		return 0, 0, err
	}
	free := exclude(ownable, inUse)
	if len(free) == 0 {
		return 0, int64(len(ids)), response.Conflict("crm.customer_in_use")
	}
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("customer_id IN ?", free).Delete(&Contact{}).Error; err != nil {
			return err
		}
		if err := tx.Where("customer_id IN ?", free).Delete(&Activity{}).Error; err != nil {
			return err
		}
		res := tx.Where("id IN ?", free).Delete(&Customer{})
		if res.Error != nil {
			return res.Error
		}
		deleted = res.RowsAffected
		return nil
	})
	if err != nil {
		return 0, 0, err
	}
	return deleted, int64(len(ids)) - deleted, nil
}

// DeleteOpportunities 批量删除线索/商机（范围外的计入 blocked，不删）。
func (s *Service) DeleteOpportunities(ctx context.Context, sc *datascope.Scope, ids []idgen.ID) (deleted, blocked int64, err error) {
	ownable, err := s.scopedIDs(ctx, sc, &Opportunity{}, ids)
	if err != nil {
		return 0, 0, err
	}
	if len(ownable) == 0 {
		return 0, int64(len(ids)), response.NotFound("crm.not_found")
	}
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// 与单条删除同口径：先把挂在这批商机上的跟进记录摘掉引用，记录本身留给客户时间线。
		if err := tx.Model(&Activity{}).Where("opportunity_id IN ?", ownable).
			Update("opportunity_id", 0).Error; err != nil {
			return err
		}
		res := tx.Where("id IN ?", ownable).Delete(&Opportunity{})
		if res.Error != nil {
			return res.Error
		}
		deleted = res.RowsAffected
		return nil
	})
	if err != nil {
		return 0, 0, err
	}
	return deleted, int64(len(ids)) - deleted, nil
}

// DELETE /plugins/crm/customers?ids=1,2,3 —— 批量删除客户（连带联系人与跟进）。
// blocked 是「点名了但没删」的条数：范围外的、或仍有商机的。
func (h *handler) deleteCustomers(c *gin.Context) {
	sc, ok := h.scopeOf(c)
	if !ok {
		return
	}
	ids, err := response.ParseIDsQuery(c)
	if err != nil {
		response.Fail(c, err)
		return
	}
	deleted, blocked, err := h.svc.DeleteCustomers(c.Request.Context(), sc, ids)
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, gin.H{"deleted": deleted, "blocked": blocked})
}

// DELETE /plugins/crm/opportunities?ids=1,2,3 —— 批量删除线索/商机。
func (h *handler) deleteOpportunities(c *gin.Context) {
	sc, ok := h.scopeOf(c)
	if !ok {
		return
	}
	ids, err := response.ParseIDsQuery(c)
	if err != nil {
		response.Fail(c, err)
		return
	}
	deleted, blocked, err := h.svc.DeleteOpportunities(c.Request.Context(), sc, ids)
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, gin.H{"deleted": deleted, "blocked": blocked})
}
