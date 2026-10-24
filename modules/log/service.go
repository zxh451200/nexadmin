package log

import (
	"context"
	"strings"
	"sync/atomic"
	"time"

	"go.uber.org/zap"
	"gorm.io/gorm"

	"nexadmin/pkg/idgen"
)

// Service 既服务管理端查询，也是提供给中间件/登录流程写日志的入口：
//
//	log.Default().WriteOper(ctx, &log.OperLog{...})   // 操作日志
//	log.Default().WriteLogin(ctx, &log.LoginLog{...}) // 登录日志（失败只写分类，细节进服务端日志）
//
// 写入时 ctx 里需要租户（或在记录上显式设置 TenantID）。
type Service struct {
	db  *gorm.DB
	loc *time.Location
	log *zap.Logger

	exportMaxRows int // log.export_max_rows；<=0 用默认值（见 export.go）
}

var defaultSvc atomic.Pointer[Service]

// Default 返回模块注册后可用的 Service；模块被停用时为 nil，调用方需判空。
func Default() *Service { return defaultSvc.Load() }

// NewService 创建 Service；tz 是应用时区（用于解释日期筛选），非法时按 UTC。
func NewService(db *gorm.DB, tz string, log *zap.Logger) *Service {
	loc, err := time.LoadLocation(tz)
	if err != nil {
		loc = time.UTC
	}
	return &Service{db: db, loc: loc, log: log}
}

// WriteOper 写一条操作日志。
func (s *Service) WriteOper(ctx context.Context, l *OperLog) error {
	return s.db.WithContext(ctx).Create(l).Error
}

// WriteLogin 写一条登录日志。
func (s *Service) WriteLogin(ctx context.Context, l *LoginLog) error {
	return s.db.WithContext(ctx).Create(l).Error
}

func likeArg(s string) string {
	return "%" + strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(s) + "%"
}

// filter 追加两种日志共用的条件：关键字、状态、日期区间（含首尾两天，按应用时区）。
func (s *Service) filter(tx *gorm.DB, q Query, keywordCols ...string) *gorm.DB {
	if kw := strings.TrimSpace(q.Keyword); kw != "" {
		conds := make([]string, len(keywordCols))
		args := make([]any, len(keywordCols))
		for i, col := range keywordCols {
			conds[i], args[i] = col+" LIKE ?", likeArg(kw)
		}
		tx = tx.Where("("+strings.Join(conds, " OR ")+")", args...)
	}
	if q.Status != "" {
		tx = tx.Where("status = ?", q.Status)
	}
	if t, err := time.ParseInLocation("2006-01-02", q.StartDate, s.loc); err == nil {
		tx = tx.Where("created_at >= ?", t.UTC())
	}
	if t, err := time.ParseInLocation("2006-01-02", q.EndDate, s.loc); err == nil {
		tx = tx.Where("created_at < ?", t.AddDate(0, 0, 1).UTC())
	}
	return tx
}

// operQuery / loginQuery 是列表与导出共用的“筛选 + 数据范围”查询，导出不另写一套条件。
func (s *Service) operQuery(ctx context.Context, q Query) *gorm.DB {
	tx := scoped(s.filter(s.db.WithContext(ctx).Model(&OperLog{}), q, "module", "operator", "url"), q.Scope, operCol)
	if q.Type != "" {
		tx = tx.Where("type = ?", q.Type)
	}
	return tx
}

func (s *Service) loginQuery(ctx context.Context, q Query) *gorm.DB {
	tx := scoped(s.filter(s.db.WithContext(ctx).Model(&LoginLog{}), q, "username", "ip"), q.Scope, loginCol)
	if q.LoginType != "" {
		tx = tx.Where("login_type = ?", q.LoginType)
	}
	return tx
}

// ListOper 操作日志分页，最新的在前。
func (s *Service) ListOper(ctx context.Context, q Query) ([]OperView, int64, error) {
	tx := s.operQuery(ctx, q).Session(&gorm.Session{})
	var total int64
	if err := tx.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var rows []OperLog
	if err := tx.Order("created_at DESC").Offset((q.Page - 1) * q.PageSize).Limit(q.PageSize).Find(&rows).Error; err != nil {
		return nil, 0, err
	}
	out := make([]OperView, len(rows))
	for i, r := range rows {
		out[i] = toOperView(r)
	}
	return out, total, nil
}

// ListLogin 登录日志分页，最新的在前。
func (s *Service) ListLogin(ctx context.Context, q Query) ([]LoginView, int64, error) {
	tx := s.loginQuery(ctx, q).Session(&gorm.Session{})
	var total int64
	if err := tx.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var rows []LoginLog
	if err := tx.Order("created_at DESC").Offset((q.Page - 1) * q.PageSize).Limit(q.PageSize).Find(&rows).Error; err != nil {
		return nil, 0, err
	}
	out := make([]LoginView, len(rows))
	for i, r := range rows {
		out[i] = toLoginView(r)
	}
	return out, total, nil
}

func ids64(ids []idgen.ID) []int64 {
	out := make([]int64, len(ids))
	for i, v := range ids {
		out[i] = int64(v)
	}
	return out
}

// DeleteOper / DeleteLogin 按 ID 批量删除（自动限定当前租户）。
func (s *Service) DeleteOper(ctx context.Context, ids []idgen.ID) error {
	return s.db.WithContext(ctx).Where("id IN ?", ids64(ids)).Delete(&OperLog{}).Error
}

func (s *Service) DeleteLogin(ctx context.Context, ids []idgen.ID) error {
	return s.db.WithContext(ctx).Where("id IN ?", ids64(ids)).Delete(&LoginLog{}).Error
}

// CleanOper 清理 days 天前的操作日志；days=0 表示全部清空。返回删除条数。
func (s *Service) CleanOper(ctx context.Context, days int) (int64, error) {
	cutoff := cleanCutoff(days)
	res := s.db.WithContext(ctx).Where("created_at < ?", cutoff).Delete(&OperLog{})
	return res.RowsAffected, res.Error
}
