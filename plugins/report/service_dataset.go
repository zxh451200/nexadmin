package report

import (
	"context"
	"errors"

	"gorm.io/gorm"

	"nexadmin/core/auth"
	"nexadmin/pkg/idgen"
)

// DatasetForm 是创建/更新数据集的请求体。
type DatasetForm struct {
	Name    string   `json:"name"`
	Source  string   `json:"source"`
	Columns []string `json:"columns"`
	Filters []Filter `json:"filters"`
	Remark  string   `json:"remark"`
}

// DatasetView 是数据集的返回形状。
type DatasetView struct {
	ID        idgen.ID `json:"id"`
	Name      string   `json:"name"`
	Source    string   `json:"source"`
	Columns   []string `json:"columns"`
	Filters   []Filter `json:"filters"`
	Remark    string   `json:"remark"`
	CreatedAt string   `json:"createdAt"`
	UpdatedAt string   `json:"updatedAt"`
}

func datasetView(d Dataset, cfg DatasetConfig) DatasetView {
	if cfg.Columns == nil {
		cfg.Columns = []string{}
	}
	if cfg.Filters == nil {
		cfg.Filters = []Filter{}
	}
	return DatasetView{ID: d.ID, Name: d.Name, Source: d.Source, Columns: cfg.Columns, Filters: cfg.Filters, Remark: d.Remark,
		CreatedAt: d.CreatedAt.UTC().Format(timeFmt), UpdatedAt: d.UpdatedAt.UTC().Format(timeFmt)}
}

const timeFmt = "2006-01-02T15:04:05Z"

// Sources 返回可选的白名单表（含列元数据）。
func (s *Service) Sources() []*Source { return s.reg.List() }

// checkDataset 校验表单并解析出查询目标。
func (s *Service) checkDataset(f DatasetForm) (*Target, DatasetConfig, error) {
	if badText(f.Name, 100, true) || badText(f.Remark, 255, false) {
		return nil, DatasetConfig{}, specErr("report.name_invalid")
	}
	tg, err := s.reg.Target(f.Source, f.Columns)
	if err != nil {
		return nil, DatasetConfig{}, err
	}
	if len(f.Filters) > maxFilters {
		return nil, DatasetConfig{}, specErr("report.fixed_filters_too_many")
	}
	// 固定过滤条件用同一套校验（列必须在所选列里、操作符白名单、值类型）。
	probe := Spec{Dimensions: []Dim{{Column: f.Columns[0]}}, Filters: f.Filters, Limit: 1}
	if err := s.eng.ValidateSpec(tg, probe); err != nil {
		return nil, DatasetConfig{}, err
	}
	cols := make([]string, 0, len(f.Columns))
	seen := map[string]bool{}
	for _, c := range f.Columns {
		if !seen[c] {
			seen[c] = true
			cols = append(cols, c)
		}
	}
	fs := f.Filters
	if fs == nil {
		fs = []Filter{}
	}
	return tg, DatasetConfig{Columns: cols, Filters: fs}, nil
}

// SaveDataset 创建（id=0）或更新数据集。
func (s *Service) SaveDataset(ctx context.Context, id idgen.ID, f DatasetForm) (*DatasetView, error) {
	_, cfg, err := s.checkDataset(f)
	if err != nil {
		return nil, err
	}
	raw, err := encodeJSON(cfg)
	if err != nil {
		return nil, dbErr(err)
	}
	tid, err := tenantOf(ctx)
	if err != nil {
		return nil, err
	}
	var row Dataset
	if id == 0 {
		row = Dataset{Name: f.Name, Source: f.Source, Config: raw, Remark: f.Remark}
		row.TenantID = idgen.ID(tid)
		if cl := auth.ClaimsFromContext(ctx); cl != nil {
			row.CreatedBy = cl.UserID
		}
		if err := s.db.WithContext(ctx).Create(&row).Error; err != nil {
			return nil, dbErr(err)
		}
	} else {
		tx, _ := s.scoped(ctx)
		if err := tx.Where("id = ?", id).First(&row).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return nil, notFound()
			}
			return nil, dbErr(err)
		}
		tx, _ = s.scoped(ctx)
		upd := map[string]any{"name": f.Name, "source": f.Source, "config": raw, "remark": f.Remark, "updated_at": now()}
		if err := tx.Model(&Dataset{}).Where("id = ?", id).Updates(upd).Error; err != nil {
			return nil, dbErr(err)
		}
		row.Name, row.Source, row.Config, row.Remark = f.Name, f.Source, raw, f.Remark
	}
	v := datasetView(row, cfg)
	return &v, nil
}

// ListDatasets 分页列出本租户的数据集。
func (s *Service) ListDatasets(ctx context.Context, keyword string, page, size int) ([]DatasetView, int64, error) {
	tx, err := s.scoped(ctx)
	if err != nil {
		return nil, 0, err
	}
	tx = tx.Model(&Dataset{})
	if keyword != "" {
		tx = tx.Where("name LIKE ?", "%"+likeEscape(keyword)+"%")
	}
	var total int64
	if err := tx.Count(&total).Error; err != nil {
		return nil, 0, dbErr(err)
	}
	off, lim := pageOf(page, size)
	var rows []Dataset
	if err := tx.Order("id DESC").Offset(off).Limit(lim).Find(&rows).Error; err != nil {
		return nil, 0, dbErr(err)
	}
	out := make([]DatasetView, 0, len(rows))
	for _, r := range rows {
		var cfg DatasetConfig
		_ = decodeStrict(r.Config, &cfg)
		out = append(out, datasetView(r, cfg))
	}
	return out, total, nil
}

func (s *Service) loadDataset(ctx context.Context, id idgen.ID) (*Dataset, *DatasetConfig, error) {
	tx, err := s.scoped(ctx)
	if err != nil {
		return nil, nil, err
	}
	var row Dataset
	if err := tx.Where("id = ?", id).First(&row).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil, notFound()
		}
		return nil, nil, dbErr(err)
	}
	var cfg DatasetConfig
	if err := decodeStrict(row.Config, &cfg); err != nil {
		return nil, nil, dbErr(err)
	}
	return &row, &cfg, nil
}

// GetDataset 取单个数据集。
func (s *Service) GetDataset(ctx context.Context, id idgen.ID) (*DatasetView, error) {
	row, cfg, err := s.loadDataset(ctx, id)
	if err != nil {
		return nil, err
	}
	v := datasetView(*row, *cfg)
	return &v, nil
}

// DeleteDataset 删除数据集；还有图表在用时拒绝。
func (s *Service) DeleteDataset(ctx context.Context, id idgen.ID) error {
	if _, _, err := s.loadDataset(ctx, id); err != nil {
		return err
	}
	tx, _ := s.scoped(ctx)
	var n int64
	if err := tx.Model(&Chart{}).Where("dataset_id = ?", id).Count(&n).Error; err != nil {
		return dbErr(err)
	}
	if n > 0 {
		return conflict("report.dataset_in_use")
	}
	tx, _ = s.scoped(ctx)
	return dbErr(tx.Where("id = ?", id).Delete(&Dataset{}).Error)
}

// PreviewDataset 用表单里的数据集定义取前 20 行（不保存）。
func (s *Service) PreviewDataset(ctx context.Context, f DatasetForm, v Viewer) (*Result, error) {
	tg, cfg, err := s.checkDataset(f)
	if err != nil {
		return nil, err
	}
	env, err := s.env(ctx, v)
	if err != nil {
		return nil, err
	}
	dims := make([]Dim, len(cfg.Columns))
	for i, c := range cfg.Columns {
		dims[i] = Dim{Column: c}
	}
	return s.eng.Run(ctx, tg, Spec{Dimensions: dims, Filters: cfg.Filters, Limit: 20}, env)
}

// DatasetOption 是图表/报表编辑器里的数据集下拉项：带上所选列的元数据，
// 这样没有 report:dataset 权限的图表、报表编辑者也不需要读取完整白名单。
type DatasetOption struct {
	DatasetView
	Fields []Column `json:"fields"`
}

// ListDatasetOptions 列出本租户的数据集及其可用字段。白名单已撤销的数据集字段为空（编辑器里不可再选）。
func (s *Service) ListDatasetOptions(ctx context.Context) ([]DatasetOption, error) {
	list, _, err := s.ListDatasets(ctx, "", 1, 200)
	if err != nil {
		return nil, err
	}
	out := make([]DatasetOption, len(list))
	for i, d := range list {
		out[i] = DatasetOption{DatasetView: d, Fields: []Column{}}
		tg, err := s.reg.Target(d.Source, d.Columns)
		if err != nil {
			continue
		}
		out[i].Fields = tg.Columns()
	}
	return out, nil
}
