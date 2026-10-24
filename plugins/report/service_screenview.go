package report

import (
	"context"

	"nexadmin/pkg/idgen"
)

// ScreenBrief 是大屏列表项。
type ScreenBrief struct {
	ID     idgen.ID `json:"id"`
	Name   string   `json:"name"`
	Remark string   `json:"remark"`
}

// userRoleIDs 读取操作者在本租户的启用角色 id。读不到就返回错误（调用方按无权限处理）。
func (s *Service) userRoleIDs(ctx context.Context, userID idgen.ID) ([]idgen.ID, error) {
	if userID == 0 {
		return nil, nil
	}
	tid, err := tenantOf(ctx)
	if err != nil {
		return nil, err
	}
	var ids []idgen.ID
	err = s.db.WithContext(ctx).Table(userRoleTable+" AS ur").Select("ur.role_id").
		Joins("JOIN "+roleTable+" r ON r.id = ur.role_id AND r.status = ?", "1").
		Where("ur.user_id = ? AND r.tenant_id = ?", int64(userID), tid).Pluck("ur.role_id", &ids).Error
	if err != nil {
		return nil, dbErr(err)
	}
	return ids, nil
}

// canSeeScreen 判断操作者能否查看某个大屏：超管 / 大屏管理者可看全部；其他人需要有一个角色被授权。
func (s *Service) canSeeScreen(ctx context.Context, v Viewer, screenID idgen.ID) (bool, error) {
	if v.Super || v.Manager {
		return true, nil
	}
	roles, err := s.userRoleIDs(ctx, v.UserID)
	if err != nil || len(roles) == 0 {
		return false, err
	}
	tx, err := s.scoped(ctx)
	if err != nil {
		return false, err
	}
	var n int64
	if err := tx.Model(&ScreenRole{}).Where("screen_id = ? AND role_id IN ?", screenID, roles).Count(&n).Error; err != nil {
		return false, dbErr(err)
	}
	return n > 0, nil
}

// ListScreensManage 列出本租户全部大屏（管理端，含角色授权）。
func (s *Service) ListScreensManage(ctx context.Context, keyword string, page, size int) ([]ScreenView, int64, error) {
	tx, err := s.scoped(ctx)
	if err != nil {
		return nil, 0, err
	}
	tx = tx.Model(&Screen{})
	if keyword != "" {
		tx = tx.Where("name LIKE ?", "%"+likeEscape(keyword)+"%")
	}
	var total int64
	if err := tx.Count(&total).Error; err != nil {
		return nil, 0, dbErr(err)
	}
	off, lim := pageOf(page, size)
	var rows []Screen
	if err := tx.Order("id DESC").Offset(off).Limit(lim).Find(&rows).Error; err != nil {
		return nil, 0, dbErr(err)
	}
	out := make([]ScreenView, len(rows))
	for i, r := range rows {
		out[i] = screenView(r)
		out[i].Config = ScreenConfig{Items: []ScreenItem{}, RefreshSec: out[i].Config.RefreshSec}
		if out[i].RoleIDs, err = s.screenRoleIDs(ctx, r.ID); err != nil {
			return nil, 0, err
		}
	}
	return out, total, nil
}

// ListScreensView 列出操作者可见的大屏。
func (s *Service) ListScreensView(ctx context.Context, v Viewer) ([]ScreenBrief, error) {
	tx, err := s.scoped(ctx)
	if err != nil {
		return nil, err
	}
	tx = tx.Model(&Screen{})
	if !v.Super && !v.Manager {
		roles, err := s.userRoleIDs(ctx, v.UserID)
		if err != nil {
			return nil, err
		}
		if len(roles) == 0 {
			return []ScreenBrief{}, nil
		}
		sub, _ := s.scoped(ctx)
		tx = tx.Where("id IN (?)", sub.Model(&ScreenRole{}).Select("screen_id").Where("role_id IN ?", roles))
	}
	var rows []Screen
	if err := tx.Order("id DESC").Limit(200).Find(&rows).Error; err != nil {
		return nil, dbErr(err)
	}
	out := make([]ScreenBrief, len(rows))
	for i, r := range rows {
		out[i] = ScreenBrief{ID: r.ID, Name: r.Name, Remark: r.Remark}
	}
	return out, nil
}

// GetScreenView 返回操作者有权查看的大屏（含图表定义）。无权与不存在返回同样的 404，避免枚举。
func (s *Service) GetScreenView(ctx context.Context, id idgen.ID, v Viewer) (*ScreenView, error) {
	row, err := s.loadScreen(ctx, id)
	if err != nil {
		return nil, err
	}
	ok, err := s.canSeeScreen(ctx, v, id)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, notFound()
	}
	sv := screenView(row)
	ids := make([]string, len(sv.Config.Items))
	for i, it := range sv.Config.Items {
		ids[i] = it.ChartID
	}
	if sv.Charts, err = s.chartsByIDs(ctx, ids); err != nil {
		return nil, err
	}
	return &sv, nil
}

// QueryScreenChart 运行大屏里的一个图表：先校验大屏授权，再确认图表确实属于这块大屏。
func (s *Service) QueryScreenChart(ctx context.Context, screenID, chartID idgen.ID, v Viewer) (*Result, error) {
	row, err := s.loadScreen(ctx, screenID)
	if err != nil {
		return nil, err
	}
	ok, err := s.canSeeScreen(ctx, v, screenID)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, notFound()
	}
	found := false
	for _, it := range screenView(row).Config.Items {
		found = found || it.ChartID == chartID.String()
	}
	if !found {
		return nil, notFound()
	}
	ch, err := s.loadChart(ctx, chartID)
	if err != nil {
		return nil, err
	}
	return s.runChart(ctx, ch, nil, v, 0)
}
