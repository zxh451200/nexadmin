package report

import (
	"encoding/json"
	"strings"
	"unicode/utf8"

	"nexadmin/core/response"
)

// ---- 各类对象存进 config TEXT 列里的 JSON 结构 ----

// 图表类型。
const (
	ChartBar    = "bar"
	ChartLine   = "line"
	ChartPie    = "pie"
	ChartTable  = "table"
	ChartMetric = "metric"
)

// 语义颜色 token：前端映射成 hsl(var(--token))，不存任何硬编码颜色值。
var colorTokens = map[string]bool{
	"chart-1": true, "chart-2": true, "chart-3": true, "chart-4": true, "chart-5": true,
	"primary": true, "success": true, "warning": true, "destructive": true, "info": true,
}

const (
	maxReportItems  = 50
	maxReportFilter = 10
	maxScreenItems  = 40
	maxGroupLen     = 64
	gridCols        = 24 // 大屏画布栅格列数
	gridMaxRows     = 200
)

// DatasetConfig 是数据集配置：选中的列 + 管理员固定的过滤条件（对使用该数据集的所有图表生效）。
type DatasetConfig struct {
	Columns []string `json:"columns"`
	Filters []Filter `json:"filters"`
}

// ChartConfig 是图表配置：查询描述 + 展示选项。
type ChartConfig struct {
	Spec
	Colors  []string `json:"colors"`            // 语义 token，按系列顺序使用
	Stacked bool     `json:"stacked,omitempty"` // 柱/折：堆叠
	Unit    string   `json:"unit,omitempty"`    // 指标卡/坐标轴单位文案
}

// ReportItem 是报表页里的一个图表块。Span 是栅格宽度 1..12（报表页按 12 列流式排布）。
type ReportItem struct {
	ChartID string `json:"chartId"`
	Group   string `json:"group"`
	Span    int    `json:"span"`
}

// ReportFilter 是报表级筛选器，作用于「数据集 == DatasetID」的图表的 Column 字段。
// Type: dateRange（time 列，between）| select（任意列，= 或 in）。
type ReportFilter struct {
	Key       string `json:"key"`
	Label     string `json:"label"`
	Type      string `json:"type"`
	DatasetID string `json:"datasetId"`
	Column    string `json:"column"`
	Multiple  bool   `json:"multiple,omitempty"`
}

// ReportConfig 是报表页配置。
type ReportConfig struct {
	Items   []ReportItem   `json:"items"`
	Filters []ReportFilter `json:"filters"`
}

// ScreenItem 是大屏画布上的一个组件：栅格坐标（24 列），不存像素。
type ScreenItem struct {
	ChartID string `json:"chartId"`
	X       int    `json:"x"`
	Y       int    `json:"y"`
	W       int    `json:"w"`
	H       int    `json:"h"`
	Title   string `json:"title,omitempty"`
}

// ScreenConfig 是大屏配置。RefreshSec = 0 表示不自动刷新，否则 5..3600。
type ScreenConfig struct {
	Items      []ScreenItem `json:"items"`
	RefreshSec int          `json:"refreshSec"`
	Title      string       `json:"title,omitempty"`
}

// decodeStrict 解码存储的配置；未知字段视为损坏。
func decodeStrict(s string, v any) error {
	if strings.TrimSpace(s) == "" {
		s = "{}"
	}
	dec := json.NewDecoder(strings.NewReader(s))
	dec.DisallowUnknownFields()
	return dec.Decode(v)
}

func encodeJSON(v any) (string, error) {
	b, err := json.Marshal(v)
	return string(b), err
}

func badText(s string, max int, required bool) bool {
	s = strings.TrimSpace(s)
	if required && s == "" {
		return true
	}
	return utf8.RuneCountInString(s) > max
}

// validateColors 校验颜色 token。
func validateColors(cs []string) *response.Error {
	if len(cs) > 12 {
		return specErr("颜色数量过多")
	}
	for _, c := range cs {
		if !colorTokens[c] {
			return specErr("颜色必须使用语义 token")
		}
	}
	return nil
}

// validateChartShape 按图表类型检查维度/度量个数。
func validateChartShape(typ string, sp Spec) *response.Error {
	nd, nm := len(sp.Dimensions), len(sp.Measures)
	switch typ {
	case ChartBar, ChartLine:
		if nd < 1 || nm < 1 {
			return specErr("柱状图/折线图需要至少 1 个维度和 1 个度量")
		}
	case ChartPie:
		if nd != 1 || nm != 1 {
			return specErr("饼图需要 1 个维度和 1 个度量")
		}
	case ChartMetric:
		if nd != 0 || nm != 1 {
			return specErr("指标卡需要 1 个度量，且不能有维度")
		}
	case ChartTable:
		if nd+nm < 1 {
			return specErr("表格至少需要 1 列")
		}
	default:
		return specErr("图表类型无效")
	}
	return nil
}

// validateScreenItems 校验画布布局：坐标/尺寸都落在栅格内，且组件两两不重叠。
func validateScreenItems(items []ScreenItem) *response.Error {
	if len(items) > maxScreenItems {
		return specErr("大屏组件过多")
	}
	for i, a := range items {
		if a.ChartID == "" || a.W < 1 || a.H < 1 || a.X < 0 || a.Y < 0 || a.X+a.W > gridCols || a.Y+a.H > gridMaxRows {
			return specErr("大屏布局超出画布")
		}
		if badText(a.Title, 100, false) {
			return specErr("组件标题过长")
		}
		for _, b := range items[:i] {
			if a.X < b.X+b.W && b.X < a.X+a.W && a.Y < b.Y+b.H && b.Y < a.Y+a.H {
				return specErr("大屏组件不能重叠")
			}
		}
	}
	return nil
}
