package report

import (
	"context"
	"fmt"
	"io"
	"strings"
	"time"

	"nexadmin/pkg/excel"
	"nexadmin/pkg/idgen"
)

// runtimeFilters 把报表筛选器的取值翻译成查询过滤条件。
// 客户端只能提交「筛选器 key -> 值」；列、操作符都由报表配置决定，请求里没有任何列名或操作符可写。
// 只作用于绑定了同一数据集的图表。
func runtimeFilters(cfg ReportConfig, datasetID idgen.ID, vals map[string]any) ([]Filter, error) {
	var out []Filter
	for _, f := range cfg.Filters {
		if f.DatasetID != datasetID.String() {
			continue
		}
		v, ok := vals[f.Key]
		if !ok || v == nil {
			continue
		}
		switch f.Type {
		case "dateRange":
			arr, ok := v.([]any)
			if !ok || len(arr) != 2 {
				return nil, specErr("日期范围需要起止两个值")
			}
			from, _ := arr[0].(string)
			to, _ := arr[1].(string)
			if from != "" {
				out = append(out, Filter{Column: f.Column, Op: ">=", Value: from})
			}
			if to != "" {
				if d, err := time.Parse("2006-01-02", to); err == nil { // 只给了日期：包含当天，即 < 次日 0 点
					out = append(out, Filter{Column: f.Column, Op: "<", Value: d.AddDate(0, 0, 1).Format("2006-01-02")})
				} else {
					out = append(out, Filter{Column: f.Column, Op: "<=", Value: to})
				}
			}
		case "select":
			switch x := v.(type) {
			case []any:
				if len(x) == 0 {
					continue
				}
				if !f.Multiple && len(x) > 1 {
					return nil, specErr("该筛选器不支持多选")
				}
				out = append(out, Filter{Column: f.Column, Op: "in", Value: x})
			case string:
				if x != "" {
					out = append(out, Filter{Column: f.Column, Op: "=", Value: x})
				}
			default:
				out = append(out, Filter{Column: f.Column, Op: "=", Value: x})
			}
		}
	}
	return out, nil
}

// reportChart 在报表里定位图表并按筛选器取值运行。图表必须是这张报表引用的（不能借报表入口跑任意图表）。
func (s *Service) reportChart(ctx context.Context, reportID, chartID idgen.ID, vals map[string]any, v Viewer, limit int) (*Chart, *Result, error) {
	rep, err := s.loadReport(ctx, reportID)
	if err != nil {
		return nil, nil, err
	}
	rv := reportView(rep)
	found := false
	for _, it := range rv.Config.Items {
		found = found || it.ChartID == chartID.String()
	}
	if !found {
		return nil, nil, notFound()
	}
	ch, err := s.loadChart(ctx, chartID)
	if err != nil {
		return nil, nil, err
	}
	extra, err := runtimeFilters(rv.Config, ch.DatasetID, vals)
	if err != nil {
		return nil, nil, err
	}
	res, err := s.runChart(ctx, ch, extra, v, limit)
	return ch, res, err
}

// QueryReportChart 运行报表中的一个图表。
func (s *Service) QueryReportChart(ctx context.Context, reportID, chartID idgen.ID, vals map[string]any, v Viewer) (*Result, error) {
	_, res, err := s.reportChart(ctx, reportID, chartID, vals, v, 0)
	return res, err
}

// ReportFilterOptions 返回下拉筛选器的候选值（去重，最多 200 个，已带租户与数据范围）。
func (s *Service) ReportFilterOptions(ctx context.Context, reportID idgen.ID, key string, v Viewer) ([]any, error) {
	rep, err := s.loadReport(ctx, reportID)
	if err != nil {
		return nil, err
	}
	rv := reportView(rep)
	for _, f := range rv.Config.Filters {
		if f.Key != key || f.Type != "select" {
			continue
		}
		dsID, _ := idgen.Parse(f.DatasetID)
		cfg, tg, err := s.datasetTarget(ctx, dsID)
		if err != nil {
			return nil, err
		}
		env, err := s.env(ctx, v)
		if err != nil {
			return nil, err
		}
		sp := Spec{Dimensions: []Dim{{Column: f.Column}}, Filters: cfg.Filters, Distinct: true, Limit: 200}
		res, err := s.eng.Run(ctx, tg, sp, env)
		if err != nil {
			return nil, err
		}
		out := make([]any, 0, len(res.Rows))
		for _, r := range res.Rows {
			if r["d0"] != nil {
				out = append(out, r["d0"])
			}
		}
		return out, nil
	}
	return nil, notFound()
}

// ExportFile 是导出结果：文件名（不含扩展名）与是否被行数上限截断。
type ExportFile struct {
	Name      string
	Truncated bool
}

// ExportReportChart 把报表里的表格图表导出为 xlsx（行数受 MaxRows 限制，最多 5000）。
func (s *Service) ExportReportChart(ctx context.Context, w func(ExportFile) io.Writer, reportID, chartID idgen.ID, vals map[string]any, v Viewer) error {
	ch, res, err := s.reportChart(ctx, reportID, chartID, vals, v, s.set.MaxRows)
	if err != nil {
		return err
	}
	if ch.Type != ChartTable {
		return specErr("只有表格图表可以导出")
	}
	return writeXLSX(w(ExportFile{Name: ch.Name, Truncated: res.Truncated}), ch.Name, res)
}

func writeXLSX(out io.Writer, sheet string, res *Result) error {
	header := make([]string, len(res.Columns))
	for i, c := range res.Columns {
		header[i] = c.Label
	}
	xw := excel.NewWriter(out) // 默认的公式注入防护：文本单元格一律按字符串写入
	sh, err := xw.NewSheet(sheet, excel.SheetOptions{Header: header})
	if err != nil {
		return dbErr(err)
	}
	for _, r := range res.Rows {
		vals := make([]any, len(res.Columns))
		for i, c := range res.Columns {
			vals[i] = r[c.Key]
		}
		if err := sh.WriteRow(vals...); err != nil {
			return dbErr(err)
		}
	}
	return dbErr(xw.Close())
}

// safeFileName 生成安全的下载文件名（去掉路径/控制字符/引号，限长）。
func safeFileName(name string) string {
	var b strings.Builder
	for _, r := range name {
		switch {
		case r < 0x20 || r == '/' || r == '\\' || r == '"' || r == ';' || r == ':' || r == '*' || r == '?' || r == '<' || r == '>' || r == '|' || r == '%':
			b.WriteRune('_')
		default:
			b.WriteRune(r)
		}
	}
	out := strings.TrimSpace(b.String())
	if rs := []rune(out); len(rs) > 60 {
		out = string(rs[:60])
	}
	if out == "" {
		out = "report"
	}
	return fmt.Sprintf("%s-%s", out, time.Now().UTC().Format("20060102"))
}
