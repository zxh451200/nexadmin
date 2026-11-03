package report

// 批量删除：与各自的单条删除同一套判定，只是把「一条条点」换成「一次点名」。
//   - ids 由 response.ParseIDs 校验，空或非法直接 400，绝不退化成全表删；
//   - 只删本租户实际存在的行（existingIDs），点名了但不存在的计入 blocked；
//   - 数据集仍被图表引用时跳过（与单条的 409 report.dataset_in_use 同口径），
//     整批不因它失败；一个都删不了时才返回 409，让调用方知道白点了一趟。

import (
	"context"

	"github.com/gin-gonic/gin"

	"nexadmin/core/response"
	"nexadmin/pkg/idgen"
)

// deletableIDs 取 ids 中本租户真实存在的那些，顺序保持调用方点名的顺序。
func (s *Service) deletableIDs(ctx context.Context, table string, ids []idgen.ID) ([]idgen.ID, error) {
	have, err := s.existingIDs(ctx, table, ids)
	if err != nil {
		return nil, err
	}
	out := make([]idgen.ID, 0, len(ids))
	for _, id := range ids {
		if have[id] {
			out = append(out, id)
		}
	}
	return out, nil
}

// datasetsInUse 返回 ids 中仍被图表引用的数据集。
func (s *Service) datasetsInUse(ctx context.Context, ids []idgen.ID) (map[idgen.ID]bool, error) {
	out := map[idgen.ID]bool{}
	tx, err := s.scoped(ctx)
	if err != nil {
		return nil, dbErr(err)
	}
	var got []idgen.ID
	if err := tx.Model(&Chart{}).Where("dataset_id IN ?", ids).Distinct().Pluck("dataset_id", &got).Error; err != nil {
		return nil, dbErr(err)
	}
	for _, id := range got {
		out[id] = true
	}
	return out, nil
}

// deleteBatch 删除已确认可删的 ids，返回实删条数。model 只来自本包实体。
func (s *Service) deleteBatch(ctx context.Context, model any, ids []idgen.ID) (int64, error) {
	tx, err := s.scoped(ctx)
	if err != nil {
		return 0, dbErr(err)
	}
	res := tx.Where("id IN ?", ids).Delete(model)
	if res.Error != nil {
		return 0, dbErr(res.Error)
	}
	return res.RowsAffected, nil
}

// DeleteDatasets 批量删除数据集。
func (s *Service) DeleteDatasets(ctx context.Context, ids []idgen.ID) (deleted, blocked int64, err error) {
	existing, e := s.deletableIDs(ctx, Dataset{}.TableName(), ids)
	if e != nil {
		return 0, 0, e
	}
	if len(existing) == 0 {
		return 0, int64(len(ids)), notFound()
	}
	inUse, e := s.datasetsInUse(ctx, existing)
	if e != nil {
		return 0, 0, e
	}
	free := make([]idgen.ID, 0, len(existing))
	for _, id := range existing {
		if !inUse[id] {
			free = append(free, id)
		}
	}
	if len(free) == 0 {
		return 0, int64(len(ids)), conflict("report.dataset_in_use")
	}
	d, e := s.deleteBatch(ctx, &Dataset{}, free)
	if e != nil {
		return 0, 0, e
	}
	return d, int64(len(ids)) - d, nil
}

// DeleteCharts 批量删除图表（报表/大屏里引用到它的位置会显示「图表已删除」，与单条一致）。
func (s *Service) DeleteCharts(ctx context.Context, ids []idgen.ID) (deleted, blocked int64, err error) {
	existing, e := s.deletableIDs(ctx, Chart{}.TableName(), ids)
	if e != nil {
		return 0, 0, e
	}
	if len(existing) == 0 {
		return 0, int64(len(ids)), notFound()
	}
	d, e := s.deleteBatch(ctx, &Chart{}, existing)
	if e != nil {
		return 0, 0, e
	}
	return d, int64(len(ids)) - d, nil
}

// deleteSubscriptions 清掉这组报表的订阅行——报表没了还留着订阅就是孤儿，
// RunDue 每次扫到都取不到目标并 warn 一条，且跳过时不记 last_sent_date，会反复刷。
func (s *Service) deleteSubscriptions(ctx context.Context, ids []idgen.ID) error {
	tx, err := s.scoped(ctx)
	if err != nil {
		return dbErr(err)
	}
	return dbErr(tx.Where("report_id IN ?", ids).Delete(&Subscription{}).Error)
}

// DeleteReports 批量删除报表，并一起清掉它们的订阅（与单条 DeleteReport 同口径）。
func (s *Service) DeleteReports(ctx context.Context, ids []idgen.ID) (deleted, blocked int64, err error) {
	existing, e := s.deletableIDs(ctx, ReportDef{}.TableName(), ids)
	if e != nil {
		return 0, 0, e
	}
	if len(existing) == 0 {
		return 0, int64(len(ids)), notFound()
	}
	if err := s.deleteSubscriptions(ctx, existing); err != nil {
		return 0, 0, err
	}
	d, e := s.deleteBatch(ctx, &ReportDef{}, existing)
	if e != nil {
		return 0, 0, e
	}
	return d, int64(len(ids)) - d, nil
}

// batchHandlers 把三个批量端点收敛成一个：形状与单条删除完全一致，只是 ids 走查询参数。
func (h *handler) batchDelete(c *gin.Context, fn func(context.Context, []idgen.ID) (int64, int64, error)) {
	ids, err := response.ParseIDsQuery(c)
	if err != nil {
		response.Fail(c, err)
		return
	}
	deleted, blocked, err := fn(c.Request.Context(), ids)
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, gin.H{"deleted": deleted, "blocked": blocked})
}

func (h *handler) deleteDatasets(c *gin.Context) {
	h.batchDelete(c, h.svc.DeleteDatasets)
}

func (h *handler) deleteCharts(c *gin.Context) {
	h.batchDelete(c, h.svc.DeleteCharts)
}

func (h *handler) deleteReports(c *gin.Context) {
	h.batchDelete(c, h.svc.DeleteReports)
}
