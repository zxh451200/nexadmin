package audit

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"go.uber.org/zap"
	"gorm.io/gorm"

	"nexadmin/core/response"
	"nexadmin/pkg/idgen"
)

// Service 是审计范围配置与变更记录的查询/清理。
type Service struct {
	db    *gorm.DB
	log   *zap.Logger
	scope *Scope
}

func NewService(db *gorm.DB, log *zap.Logger, scope *Scope) *Service {
	if log == nil {
		log = zap.NewNop()
	}
	return &Service{db: db, log: log, scope: scope}
}

// ConfigView 是审计范围接口的返回：已选择的表 + 可选的全部表。
type ConfigView struct {
	Tables    []string `json:"tables"`
	Available []string `json:"available"`
}

// LogQuery 是列表筛选条件。From/To 是 yyyy-MM-dd（UTC，含当天）。
type LogQuery struct {
	Table, Operator, Action, From, To string
	Page, PageSize                    int
}

func (s *Service) tx(ctx context.Context) *gorm.DB {
	return s.db.WithContext(markSkip(ctx))
}

// availableTables 从 DB 元数据读出可选表（排除自己和日志类大表）。
func (s *Service) availableTables(ctx context.Context) ([]string, error) {
	all, err := s.tx(ctx).Migrator().GetTables()
	if err != nil {
		return nil, err
	}
	return Available(all), nil
}

// GetConfig 返回当前租户的审计范围；从没配置过就是空（默认不审计任何表）。
func (s *Service) GetConfig(ctx context.Context) (*ConfigView, error) {
	avail, err := s.availableTables(ctx)
	if err != nil {
		return nil, err
	}
	var c Config
	err = s.tx(ctx).Limit(1).Find(&c).Error
	if err != nil {
		return nil, err
	}
	sel := []string{}
	if c.ID != 0 {
		// 已被删除/排除的表不再展示
		sel, _ = CleanSelection(decodeTables(c.TableList), avail)
	}
	return &ConfigView{Tables: sel, Available: avail}, nil
}

// SaveConfig 保存审计范围，并立即更新本实例的内存范围（其他实例最多 30 秒后收敛）。
func (s *Service) SaveConfig(ctx context.Context, tables []string, by string) (*ConfigView, error) {
	avail, err := s.availableTables(ctx)
	if err != nil {
		return nil, err
	}
	clean, bad := CleanSelection(tables, avail)
	if len(bad) > 0 {
		return nil, response.BadRequest("error.bad_request").WithMsg("tables not auditable: " + strings.Join(bad, ","))
	}
	raw, _ := json.Marshal(clean)
	if clean == nil {
		raw = []byte("[]")
	}
	var c Config
	if err := s.tx(ctx).Limit(1).Find(&c).Error; err != nil {
		return nil, err
	}
	c.TableList, c.UpdatedBy = string(raw), trim(by, 100)
	if c.ID == 0 {
		err = s.tx(ctx).Create(&c).Error
	} else {
		err = s.tx(ctx).Save(&c).Error
	}
	if err != nil {
		return nil, err
	}
	s.scope.Set(tenantOf(ctx), clean)
	s.log.Info("audit: scope changed", zap.Int64("tenant", tenantOf(ctx)), zap.Strings("tables", clean), zap.String("by", by))
	if clean == nil {
		clean = []string{}
	}
	return &ConfigView{Tables: clean, Available: avail}, nil
}

func trim(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return strings.ToValidUTF8(s[:n], "")
}

func parseDay(s string) (time.Time, bool) {
	t, err := time.ParseInLocation("2006-01-02", s, time.UTC)
	return t, err == nil
}

func likeEscape(s string) string {
	return strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(s)
}

// ListLogs 按条件分页（新的在前）。列表不带 changes。
func (s *Service) ListLogs(ctx context.Context, q LogQuery) ([]Log, int64, error) {
	tx := s.tx(ctx).Model(&Log{})
	if q.Table != "" {
		tx = tx.Where("table_name = ?", q.Table)
	}
	if q.Action != "" {
		tx = tx.Where("action = ?", q.Action)
	}
	if q.Operator != "" {
		tx = tx.Where("operator_name LIKE ?", "%"+likeEscape(q.Operator)+"%")
	}
	if t, ok := parseDay(q.From); ok {
		tx = tx.Where("created_at >= ?", t)
	}
	if t, ok := parseDay(q.To); ok {
		tx = tx.Where("created_at < ?", t.AddDate(0, 0, 1))
	}
	var total int64
	if err := tx.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var list []Log
	err := tx.Omit("changes").Order("created_at DESC, id DESC").Offset((q.Page - 1) * q.PageSize).Limit(q.PageSize).Find(&list).Error
	return list, total, err
}

// GetLog 返回一条记录和它的字段级 diff。
func (s *Service) GetLog(ctx context.Context, id idgen.ID) (*LogDetail, error) {
	var l Log
	if err := s.tx(ctx).Limit(1).Find(&l, "id = ?", id).Error; err != nil {
		return nil, err
	}
	if l.ID == 0 {
		return nil, response.NotFound("error.not_found")
	}
	raw := json.RawMessage(l.Changes)
	if !json.Valid(raw) {
		raw = json.RawMessage("{}")
	}
	return &LogDetail{Log: l, Changes: raw}, nil
}

// LogTables 返回记录里出现过的表名（供筛选下拉）。
func (s *Service) LogTables(ctx context.Context) ([]string, error) {
	names := []string{}
	err := s.tx(ctx).Model(&Log{}).Distinct().Order("table_name").Limit(1000).Pluck("table_name", &names).Error
	return names, err
}

// Clean 删除 days 天前的记录（只清理当前租户）。
func (s *Service) Clean(ctx context.Context, days int, by string) (int64, error) {
	cutoff := time.Now().UTC().AddDate(0, 0, -days)
	res := s.tx(ctx).Where("created_at < ?", cutoff).Delete(&Log{})
	if res.Error != nil {
		return 0, res.Error
	}
	s.log.Info("audit: records cleaned", zap.Int64("tenant", tenantOf(ctx)), zap.Int("keepDays", days), zap.Int64("deleted", res.RowsAffected), zap.String("by", by))
	return res.RowsAffected, nil
}
