package report

import (
	"context"
	"errors"
	"regexp"

	"gorm.io/gorm"

	"nexadmin/core/auth"
	"nexadmin/pkg/idgen"
)

var filterKeyRe = regexp.MustCompile(`^[a-z][a-z0-9_]{0,31}$`)

// ReportForm 是创建/更新报表的请求体。
type ReportForm struct {
	Name      string       `json:"name"`
	GroupName string       `json:"groupName"`
	Remark    string       `json:"remark"`
	Config    ReportConfig `json:"config"`
}

// ReportView 是报表的返回形状；Charts 只在查看详情时填充。
type ReportView struct {
	ID        idgen.ID     `json:"id"`
	Name      string       `json:"name"`
	GroupName string       `json:"groupName"`
	Remark    string       `json:"remark"`
	Config    ReportConfig `json:"config"`
	Charts    []ChartView  `json:"charts,omitempty"`
	CreatedAt string       `json:"createdAt"`
	UpdatedAt string       `json:"updatedAt"`
}

func reportView(r ReportDef) ReportView {
	var cfg ReportConfig
	_ = decodeStrict(r.Config, &cfg)
	if cfg.Items == nil {
		cfg.Items = []ReportItem{}
	}
	if cfg.Filters == nil {
		cfg.Filters = []ReportFilter{}
	}
	return ReportView{ID: r.ID, Name: r.Name, GroupName: r.GroupName, Remark: r.Remark, Config: cfg,
		CreatedAt: r.CreatedAt.UTC().Format(timeFmt), UpdatedAt: r.UpdatedAt.UTC().Format(timeFmt)}
}

// checkReport 校验并规范化报表配置（图表/数据集必须属于本租户，筛选器列必须在数据集里）。
func (s *Service) checkReport(ctx context.Context, f *ReportForm) (string, error) {
	if badText(f.Name, 100, true) || badText(f.GroupName, maxGroupLen, false) || badText(f.Remark, 255, false) {
		return "", specErr("report.name_and_remark_invalid")
	}
	c := &f.Config
	if len(c.Items) > maxReportItems || len(c.Filters) > maxReportFilter {
		return "", specErr("report.report_items_too_many")
	}
	ids := make([]string, len(c.Items))
	for i, it := range c.Items {
		ids[i] = it.ChartID
	}
	parsed, e := parseIDs(ids)
	if e != nil {
		return "", e
	}
	have, err := s.existingIDs(ctx, "plg_report_chart", parsed)
	if err != nil {
		return "", err
	}
	for i := range c.Items {
		id, _ := idgen.Parse(c.Items[i].ChartID)
		if !have[id] {
			return "", specErr("report.chart_not_found")
		}
		it := &c.Items[i]
		it.ChartID = id.String()
		if it.Span == 0 {
			it.Span = 12
		}
		if it.Span < 1 || it.Span > 12 || badText(it.Group, maxGroupLen, false) {
			return "", specErr("report.chart_layout_invalid")
		}
	}
	keys := map[string]bool{}
	for i := range c.Filters {
		fl := &c.Filters[i]
		if !filterKeyRe.MatchString(fl.Key) || keys[fl.Key] || badText(fl.Label, 50, true) {
			return "", specErr("report.filter_ident_invalid")
		}
		keys[fl.Key] = true
		if fl.Type != "dateRange" && fl.Type != "select" {
			return "", specErr("report.filter_type_invalid")
		}
		dsID, err := idgen.Parse(fl.DatasetID)
		if err != nil || dsID == 0 {
			return "", specErr("report.filter_dataset_required")
		}
		ds, cfg, err := s.loadDataset(ctx, dsID)
		if err != nil {
			return "", err
		}
		fl.DatasetID = dsID.String()
		inDS := false
		for _, col := range cfg.Columns {
			inDS = inDS || col == fl.Column
		}
		src, ok := s.reg.Get(ds.Source)
		if !inDS || !ok {
			return "", errNotAllowed
		}
		col, ok := src.Col(fl.Column)
		if !ok || deniedColumn(fl.Column) {
			return "", errNotAllowed
		}
		if fl.Type == "dateRange" && col.Type != TypeTime {
			return "", specErr("report.filter_date_time_only")
		}
		if fl.Type == "dateRange" {
			fl.Multiple = false
		}
	}
	return encodeJSON(*c)
}

// SaveReport 创建（id=0）或更新报表。
func (s *Service) SaveReport(ctx context.Context, id idgen.ID, f ReportForm) (*ReportView, error) {
	raw, err := s.checkReport(ctx, &f)
	if err != nil {
		return nil, err
	}
	tid, err := tenantOf(ctx)
	if err != nil {
		return nil, err
	}
	var row ReportDef
	if id == 0 {
		row = ReportDef{Name: f.Name, GroupName: f.GroupName, Remark: f.Remark, Config: raw}
		row.TenantID = idgen.ID(tid)
		if cl := auth.ClaimsFromContext(ctx); cl != nil {
			row.CreatedBy = cl.UserID
		}
		if err := s.db.WithContext(ctx).Create(&row).Error; err != nil {
			return nil, dbErr(err)
		}
	} else {
		if row, err = s.loadReport(ctx, id); err != nil {
			return nil, err
		}
		tx, _ := s.scoped(ctx)
		upd := map[string]any{"name": f.Name, "group_name": f.GroupName, "remark": f.Remark, "config": raw, "updated_at": now()}
		if err := tx.Model(&ReportDef{}).Where("id = ?", id).Updates(upd).Error; err != nil {
			return nil, dbErr(err)
		}
		row.Name, row.GroupName, row.Remark, row.Config = f.Name, f.GroupName, f.Remark, raw
	}
	v := reportView(row)
	return &v, nil
}

func (s *Service) loadReport(ctx context.Context, id idgen.ID) (ReportDef, error) {
	tx, err := s.scoped(ctx)
	if err != nil {
		return ReportDef{}, err
	}
	var row ReportDef
	if err := tx.Where("id = ?", id).First(&row).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return ReportDef{}, notFound()
		}
		return ReportDef{}, dbErr(err)
	}
	return row, nil
}

// ListReports 列出本租户的报表（管理与查看共用；不含图表详情）。
func (s *Service) ListReports(ctx context.Context, keyword string, page, size int) ([]ReportView, int64, error) {
	tx, err := s.scoped(ctx)
	if err != nil {
		return nil, 0, err
	}
	tx = tx.Model(&ReportDef{})
	if keyword != "" {
		tx = tx.Where("name LIKE ?", "%"+likeEscape(keyword)+"%")
	}
	var total int64
	if err := tx.Count(&total).Error; err != nil {
		return nil, 0, dbErr(err)
	}
	off, lim := pageOf(page, size)
	var rows []ReportDef
	if err := tx.Order("group_name, id DESC").Offset(off).Limit(lim).Find(&rows).Error; err != nil {
		return nil, 0, dbErr(err)
	}
	out := make([]ReportView, len(rows))
	for i, r := range rows {
		out[i] = reportView(r)
		out[i].Config = ReportConfig{Items: []ReportItem{}, Filters: []ReportFilter{}}
	}
	return out, total, nil
}

// DeleteReport 删除报表。
func (s *Service) DeleteReport(ctx context.Context, id idgen.ID) error {
	if _, err := s.loadReport(ctx, id); err != nil {
		return err
	}
	tx, _ := s.scoped(ctx)
	return dbErr(tx.Where("id = ?", id).Delete(&ReportDef{}).Error)
}

// chartsByIDs 读取一组图表（本租户）。
func (s *Service) chartsByIDs(ctx context.Context, ids []string) ([]ChartView, error) {
	parsed, e := parseIDs(ids)
	if e != nil || len(parsed) == 0 {
		return []ChartView{}, nil
	}
	tx, err := s.scoped(ctx)
	if err != nil {
		return nil, err
	}
	var rows []Chart
	if err := tx.Where("id IN ?", parsed).Find(&rows).Error; err != nil {
		return nil, dbErr(err)
	}
	out := make([]ChartView, len(rows))
	for i, r := range rows {
		out[i] = chartView(r)
	}
	return out, nil
}

// GetReport 返回报表详情（含所引用图表的定义）。
func (s *Service) GetReport(ctx context.Context, id idgen.ID) (*ReportView, error) {
	row, err := s.loadReport(ctx, id)
	if err != nil {
		return nil, err
	}
	v := reportView(row)
	ids := make([]string, len(v.Config.Items))
	for i, it := range v.Config.Items {
		ids[i] = it.ChartID
	}
	if v.Charts, err = s.chartsByIDs(ctx, ids); err != nil {
		return nil, err
	}
	return &v, nil
}
