package report

import (
	"context"
	"errors"

	"gorm.io/gorm"

	"nexadmin/core/auth"
	"nexadmin/core/response"
	"nexadmin/pkg/idgen"
)

func conflict(msg string) *response.Error { return response.Conflict("error.conflict").WithMsg(msg) }

// ChartForm 是创建/更新图表的请求体。
type ChartForm struct {
	Name      string      `json:"name"`
	DatasetID string      `json:"datasetId"`
	Type      string      `json:"type"`
	Config    ChartConfig `json:"config"`
	Remark    string      `json:"remark"`
}

// ChartView 是图表的返回形状。
type ChartView struct {
	ID        idgen.ID    `json:"id"`
	Name      string      `json:"name"`
	DatasetID idgen.ID    `json:"datasetId"`
	Type      string      `json:"type"`
	Config    ChartConfig `json:"config"`
	Remark    string      `json:"remark"`
	CreatedAt string      `json:"createdAt"`
	UpdatedAt string      `json:"updatedAt"`
}

func chartView(c Chart) ChartView {
	var cfg ChartConfig
	_ = decodeStrict(c.Config, &cfg)
	if cfg.Dimensions == nil {
		cfg.Dimensions = []Dim{}
	}
	if cfg.Measures == nil {
		cfg.Measures = []Measure{}
	}
	if cfg.Filters == nil {
		cfg.Filters = []Filter{}
	}
	if cfg.OrderBy == nil {
		cfg.OrderBy = []Order{}
	}
	if cfg.Colors == nil {
		cfg.Colors = []string{}
	}
	return ChartView{ID: c.ID, Name: c.Name, DatasetID: c.DatasetID, Type: c.Type, Config: cfg, Remark: c.Remark,
		CreatedAt: c.CreatedAt.UTC().Format(timeFmt), UpdatedAt: c.UpdatedAt.UTC().Format(timeFmt)}
}

// datasetTarget 读取数据集并解析成查询目标（每次都重新过白名单/拒绝列表）。
func (s *Service) datasetTarget(ctx context.Context, id idgen.ID) (*DatasetConfig, *Target, error) {
	ds, cfg, err := s.loadDataset(ctx, id)
	if err != nil {
		return nil, nil, err
	}
	tg, err := s.reg.Target(ds.Source, cfg.Columns)
	if err != nil {
		return nil, nil, err
	}
	return cfg, tg, nil
}

// fullSpec 合成最终查询：数据集固定过滤 + 图表自身过滤 + 运行时过滤（报表筛选器）。
// 三者只能 AND 叠加，没有任何一层能删除或改写另一层，更不能碰租户条件。
func fullSpec(ds *DatasetConfig, c ChartConfig, runtime []Filter) Spec {
	sp := c.Spec
	fs := make([]Filter, 0, len(ds.Filters)+len(sp.Filters)+len(runtime))
	fs = append(fs, ds.Filters...)
	fs = append(fs, sp.Filters...)
	fs = append(fs, runtime...)
	sp.Filters = fs
	return sp
}

func (s *Service) checkChart(ctx context.Context, f ChartForm) (idgen.ID, string, error) {
	if badText(f.Name, 100, true) || badText(f.Remark, 255, false) {
		return 0, "", specErr("名称不能为空且不超过 100 字")
	}
	dsID, err := idgen.Parse(f.DatasetID)
	if err != nil || dsID == 0 {
		return 0, "", specErr("请选择数据集")
	}
	cfg, tg, err := s.datasetTarget(ctx, dsID)
	if err != nil {
		return 0, "", err
	}
	if e := validateChartShape(f.Type, f.Config.Spec); e != nil {
		return 0, "", e
	}
	if e := validateColors(f.Config.Colors); e != nil {
		return 0, "", e
	}
	if badText(f.Config.Unit, 16, false) {
		return 0, "", specErr("单位过长")
	}
	if err := s.eng.ValidateSpec(tg, fullSpec(cfg, f.Config, nil)); err != nil {
		return 0, "", err
	}
	raw, err := encodeJSON(f.Config)
	if err != nil {
		return 0, "", dbErr(err)
	}
	return dsID, raw, nil
}

// SaveChart 创建（id=0）或更新图表。
func (s *Service) SaveChart(ctx context.Context, id idgen.ID, f ChartForm) (*ChartView, error) {
	dsID, raw, err := s.checkChart(ctx, f)
	if err != nil {
		return nil, err
	}
	tid, err := tenantOf(ctx)
	if err != nil {
		return nil, err
	}
	var row Chart
	if id == 0 {
		row = Chart{Name: f.Name, DatasetID: dsID, Type: f.Type, Config: raw, Remark: f.Remark}
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
		upd := map[string]any{"name": f.Name, "dataset_id": dsID, "type": f.Type, "config": raw, "remark": f.Remark, "updated_at": now()}
		if err := tx.Model(&Chart{}).Where("id = ?", id).Updates(upd).Error; err != nil {
			return nil, dbErr(err)
		}
		row.Name, row.DatasetID, row.Type, row.Config, row.Remark = f.Name, dsID, f.Type, raw, f.Remark
	}
	v := chartView(row)
	return &v, nil
}

// ListCharts 分页列出图表；datasetID 非 0 时按数据集过滤。
func (s *Service) ListCharts(ctx context.Context, keyword string, datasetID idgen.ID, page, size int) ([]ChartView, int64, error) {
	tx, err := s.scoped(ctx)
	if err != nil {
		return nil, 0, err
	}
	tx = tx.Model(&Chart{})
	if keyword != "" {
		tx = tx.Where("name LIKE ?", "%"+likeEscape(keyword)+"%")
	}
	if datasetID != 0 {
		tx = tx.Where("dataset_id = ?", datasetID)
	}
	var total int64
	if err := tx.Count(&total).Error; err != nil {
		return nil, 0, dbErr(err)
	}
	off, lim := pageOf(page, size)
	var rows []Chart
	if err := tx.Order("id DESC").Offset(off).Limit(lim).Find(&rows).Error; err != nil {
		return nil, 0, dbErr(err)
	}
	out := make([]ChartView, len(rows))
	for i, r := range rows {
		out[i] = chartView(r)
	}
	return out, total, nil
}

func (s *Service) loadChart(ctx context.Context, id idgen.ID) (*Chart, error) {
	tx, err := s.scoped(ctx)
	if err != nil {
		return nil, err
	}
	var row Chart
	if err := tx.Where("id = ?", id).First(&row).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, notFound()
		}
		return nil, dbErr(err)
	}
	return &row, nil
}

// GetChart 取单个图表。
func (s *Service) GetChart(ctx context.Context, id idgen.ID) (*ChartView, error) {
	row, err := s.loadChart(ctx, id)
	if err != nil {
		return nil, err
	}
	v := chartView(*row)
	return &v, nil
}

// DeleteChart 删除图表（报表/大屏里引用到它的位置会显示「图表已删除」）。
func (s *Service) DeleteChart(ctx context.Context, id idgen.ID) error {
	if _, err := s.loadChart(ctx, id); err != nil {
		return err
	}
	tx, _ := s.scoped(ctx)
	return dbErr(tx.Where("id = ?", id).Delete(&Chart{}).Error)
}

// runChart 执行一个已保存的图表；extra 是运行时附加的过滤条件。
func (s *Service) runChart(ctx context.Context, ch *Chart, extra []Filter, v Viewer, limit int) (*Result, error) {
	var cc ChartConfig
	if err := decodeStrict(ch.Config, &cc); err != nil {
		return nil, dbErr(err)
	}
	cfg, tg, err := s.datasetTarget(ctx, ch.DatasetID)
	if err != nil {
		return nil, err
	}
	env, err := s.env(ctx, v)
	if err != nil {
		return nil, err
	}
	sp := fullSpec(cfg, cc, extra)
	if limit > 0 { // 导出等场景改用调用方给的行数（引擎仍会夹到 MaxLimit / 配置上限内）
		sp.Limit = limit
	}
	return s.eng.Run(ctx, tg, sp, env)
}

// QueryChart 编辑器里直接运行一个已保存的图表（不带运行时过滤）。
func (s *Service) QueryChart(ctx context.Context, id idgen.ID, v Viewer) (*Result, error) {
	ch, err := s.loadChart(ctx, id)
	if err != nil {
		return nil, err
	}
	return s.runChart(ctx, ch, nil, v, 0)
}

// PreviewChart 运行表单里尚未保存的图表定义（行数压到 200）。
func (s *Service) PreviewChart(ctx context.Context, f ChartForm, v Viewer) (*Result, error) {
	dsID, _, err := s.checkChart(ctx, f)
	if err != nil {
		return nil, err
	}
	cfg, tg, err := s.datasetTarget(ctx, dsID)
	if err != nil {
		return nil, err
	}
	env, err := s.env(ctx, v)
	if err != nil {
		return nil, err
	}
	sp := fullSpec(cfg, f.Config, nil)
	if sp.Limit == 0 || sp.Limit > 200 {
		sp.Limit = 200
	}
	return s.eng.Run(ctx, tg, sp, env)
}
