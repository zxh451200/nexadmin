package crm

// 跟进记录时间线：挂客户（必填）或商机（可选）。author 记录人 = 当前账号，
// 读写都继承客户的可见性（数据范围按 customer.owner 裁，不按记录人裁）。

import (
	"context"
	"errors"
	"regexp"
	"strings"

	"gorm.io/gorm"

	"nexadmin/core/auth"
	"nexadmin/core/datascope"
	"nexadmin/core/response"
	"nexadmin/pkg/idgen"
)

// activityDateRe 校验下次跟进日期（YYYY-MM-DD）。
var activityDateRe = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}$`)

// ActivityInput 是写跟进入参。
type ActivityInput struct {
	CustomerID    idgen.ID
	OpportunityID idgen.ID
	Kind          string
	Content       string
	NextAt        string
}

func (in *ActivityInput) normalize() error {
	in.Content = strings.TrimSpace(in.Content)
	in.Kind = strings.TrimSpace(in.Kind)
	in.NextAt = strings.TrimSpace(in.NextAt)
	if in.Kind == "" {
		in.Kind = KindNote
	}
	if !ActivityKindSet[in.Kind] {
		return response.BadRequest("crm.activity_kind_invalid")
	}
	if in.Content == "" {
		return response.BadRequest("crm.activity_content_required")
	}
	if in.NextAt != "" && !activityDateRe.MatchString(in.NextAt) {
		return response.BadRequest("crm.date_invalid")
	}
	return nil
}

// ActivityQuery 过滤跟进时间线。
type ActivityQuery struct {
	CustomerID    idgen.ID
	OpportunityID idgen.ID
	Kind          string
	OpenOnly      bool // 只看待办（done=false；对非 todo 类型无意义，由前端组合）
	Page          int
	PageSize      int
}

// CreateActivity 记一条跟进。author 取当前账号；内部调用方（无身份）允许为空。
func (s *Service) CreateActivity(ctx context.Context, sc *datascope.Scope, in ActivityInput) (*Activity, error) {
	if err := in.normalize(); err != nil {
		return nil, err
	}
	if _, err := s.visibleCustomer(ctx, sc, in.CustomerID); err != nil {
		return nil, err
	}
	if in.OpportunityID != 0 {
		if err := s.db.WithContext(ctx).Take(&Opportunity{}, int64(in.OpportunityID)).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return nil, response.NotFound("crm.not_found")
			}
			return nil, err
		}
	}
	a := &Activity{
		CustomerID: in.CustomerID, OpportunityID: in.OpportunityID,
		Kind: in.Kind, Content: in.Content, NextAt: in.NextAt,
	}
	if _, name, ok := auth.UserFromContext(ctx); ok {
		a.Author = truncate(name, 64)
	}
	if err := s.db.WithContext(ctx).Create(a).Error; err != nil {
		return nil, err
	}
	return a, nil
}

// ListActivities 时间线：按创建时间倒序分页，最新跟进在最上。
func (s *Service) ListActivities(ctx context.Context, sc *datascope.Scope, q ActivityQuery) ([]Activity, int64, error) {
	if _, err := s.visibleCustomer(ctx, sc, q.CustomerID); err != nil {
		return nil, 0, err
	}
	db := s.db.WithContext(ctx).Model(&Activity{})
	if q.CustomerID != 0 {
		db = db.Where("customer_id = ?", int64(q.CustomerID))
	}
	if q.OpportunityID != 0 {
		db = db.Where("opportunity_id = ?", int64(q.OpportunityID))
	}
	if q.Kind != "" {
		db = db.Where("kind = ?", q.Kind)
	}
	if q.OpenOnly {
		db = db.Where("done = ?", false)
	}
	var total int64
	if err := db.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var rows []Activity
	if err := db.Order("id DESC").Offset((q.Page - 1) * q.PageSize).Limit(q.PageSize).Find(&rows).Error; err != nil {
		return nil, 0, err
	}
	return rows, total, nil
}

// MarkActivityDone 勾掉/恢复待办（todo）。非待办类型不改状态，直接成功——幂等语义。
func (s *Service) MarkActivityDone(ctx context.Context, sc *datascope.Scope, id idgen.ID, done bool) error {
	var a Activity
	if err := s.db.WithContext(ctx).Take(&a, id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return response.NotFound("crm.not_found")
		}
		return err
	}
	if _, err := s.visibleCustomer(ctx, sc, a.CustomerID); err != nil {
		return err
	}
	return s.db.WithContext(ctx).Model(&Activity{}).Where("id = ?", int64(id)).Update("done", done).Error
}

// DeleteActivity 删除跟进记录（写错的内容允许清理）。
func (s *Service) DeleteActivity(ctx context.Context, sc *datascope.Scope, id idgen.ID) error {
	var a Activity
	if err := s.db.WithContext(ctx).Take(&a, id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return response.NotFound("crm.not_found")
		}
		return err
	}
	if _, err := s.visibleCustomer(ctx, sc, a.CustomerID); err != nil {
		return err
	}
	return s.db.WithContext(ctx).Delete(&Activity{}, id).Error
}

// noteSystem 往客户时间线写一条系统留痕（归属变更/合并/标签/批量操作等）。
// author 取当前账号（内部调用可为空）；写入失败只记日志不拦主操作——主数据已经落了，
// 留痕是锦上添花。内容是存档文本，按部署主语言（中文）落库。
func (s *Service) noteSystem(ctx context.Context, customerID idgen.ID, content string) {
	a := Activity{CustomerID: customerID, Kind: KindSystem, Content: truncate(content, 2000)}
	if _, name, ok := auth.UserFromContext(ctx); ok {
		a.Author = truncate(name, 64)
	}
	if err := s.db.WithContext(ctx).Create(&a).Error; err != nil && s.log != nil {
		s.log.Warn("crm: record system activity failed", "customerId", customerID.String(), "err", err.Error())
	}
}
