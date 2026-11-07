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
	ChartArea   = "area" // 面积图：折线 + 填充，趋势总量对比比纯折线更直观
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
	// 自动刷新间隔的允许范围（报表与大屏同口径）：0 = 关闭，否则 minRefreshSec..maxRefreshSec。
	// 下限 5 秒不是拍脑袋：间隔是前端拿来轮询取数接口的，直调 API 写 1 秒就等于
	// 让每个打开报表页的浏览器按秒打数据库，属于自我 DoS，必须在服务端夹住。
	minRefreshSec = 5
	maxRefreshSec = 3600
)

// DatasetConfig 是数据集配置：选中的列 + 管理员固定的过滤条件 + 可选的关联表声明
// （对使用该数据集的所有图表生效）。列引用可以是 "关联表.列" 形态，但指向的关联必须
// 在 Joins 里声明过——图表与运行时筛选都无法新增关联，安全边界与单表一致。
type DatasetConfig struct {
	Columns []string `json:"columns"`
	Filters []Filter `json:"filters"`
	Joins   []Join   `json:"joins,omitempty"`
}

// ChartConfig 是图表配置：查询描述 + 展示选项。
type ChartConfig struct {
	Spec
	Colors  []string `json:"colors"`            // 语义 token，按系列顺序使用
	Stacked bool     `json:"stacked,omitempty"` // 柱/折：堆叠
	Unit    string   `json:"unit,omitempty"`    // 指标卡/坐标轴单位文案
	// Compare 是指标卡的同环比模式："" 关闭，period 环比（上一等长区间），year 同比（去年同期）。
	// 只对 metric 图表有意义；区间从报表筛选器/图表过滤里的时间条件推出，推不出时前端显示无对比。
	Compare string `json:"compare,omitempty"`
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
	// RefreshSec 是自动刷新间隔（秒）；0 = 不自动刷新（默认）。查看页按它轮询图表数据，
	// 供挂在大屏/监控室的报表页使用；与 ScreenConfig.RefreshSec 同语义。
	// 取值由服务端校验（见 validateRefreshSec）：0 或 5..3600，越界一律保存失败。
	RefreshSec int `json:"refreshSec,omitempty"`
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

// validateRefreshSec 校验自动刷新间隔：0（关闭）或 5..3600 秒，与 checkScreen 里大屏的口径一致。
// 越界只报错、不静默夹紧——静默改数据会让「我明明填了 30」这类反馈无从排查。
// key 写成 response.BadRequest 字面量（不是 specErr 传参），这样 contract_check 的 E 类
// 才扫得到它：漏一种语言时用户会直接看到 key 原文。
func validateRefreshSec(sec int) *response.Error {
	if sec == 0 || (sec >= minRefreshSec && sec <= maxRefreshSec) {
		return nil
	}
	return response.BadRequest("report.refresh_interval_invalid")
}

// encodeJSON 编码要写进 config TEXT 列的配置。
// ReportConfig 的兜底校验放在这里，因为「所有要落库的报表配置」只从这一个出口过：
// 报表保存路径 service_report.go 的 checkReport 本轮不在可改文件范围内，先由落库出口拦下
// API 直写的越界值；checkReport 之后补上 validateRefreshSec 调用也不冲突（同口径、幂等）。
func encodeJSON(v any) (string, error) {
	if rc, ok := v.(ReportConfig); ok {
		if e := validateRefreshSec(rc.RefreshSec); e != nil {
			return "", e
		}
	}
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
		return specErr("report.color_too_many")
	}
	for _, c := range cs {
		if !colorTokens[c] {
			return specErr("report.color_token_required")
		}
	}
	return nil
}

// validateChartShape 按图表类型检查维度/度量个数。
func validateChartShape(typ string, sp Spec) *response.Error {
	nd, nm := len(sp.Dimensions), len(sp.Measures)
	switch typ {
	case ChartBar, ChartLine, ChartArea:
		if nd < 1 || nm < 1 {
			return specErr("report.chart_axes_required")
		}
	case ChartPie:
		if nd != 1 || nm != 1 {
			return specErr("report.pie_axes_required")
		}
	case ChartMetric:
		if nd != 0 || nm != 1 {
			return specErr("report.metric_measure_required")
		}
	case ChartTable:
		if nd+nm < 1 {
			return specErr("report.table_columns_required")
		}
	default:
		return specErr("report.chart_type_invalid")
	}
	return nil
}

// validateScreenItems 校验画布布局：坐标/尺寸都落在栅格内，且组件两两不重叠。
func validateScreenItems(items []ScreenItem) *response.Error {
	if len(items) > maxScreenItems {
		return specErr("report.screen_widgets_too_many")
	}
	for i, a := range items {
		if a.ChartID == "" || a.W < 1 || a.H < 1 || a.X < 0 || a.Y < 0 || a.X+a.W > gridCols || a.Y+a.H > gridMaxRows {
			return specErr("report.screen_layout_overflow")
		}
		if badText(a.Title, 100, false) {
			return specErr("report.widget_title_too_long")
		}
		for _, b := range items[:i] {
			if a.X < b.X+b.W && b.X < a.X+a.W && a.Y < b.Y+b.H && b.Y < a.Y+a.H {
				return specErr("report.screen_widgets_overlap")
			}
		}
	}
	return nil
}
