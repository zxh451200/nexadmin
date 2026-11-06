package report

// 指标卡同环比：Compare=period 时跑上一等长区间，Compare=year 时跑去年同期。
// 区间从「最终生效的过滤条件」里推——同一时间列上同时有下界（>=/>）与上界（</<=/between）才算窗口；
// 只有一侧（比如只有「近 30 天」的下界）推不出区间长度，按无对比返回，而不是瞎猜。
// 对比查询失败不拖垮主数据：主结果照常返回，对比信息缺省（前端显示无对比）。

import (
	"context"
	"math"
	"time"

	"go.uber.org/zap"
)

// 对比模式。
const (
	CompareOff    = ""
	ComparePeriod = "period" // 环比：向回平移一个等长区间
	CompareYear   = "year"   // 同比：去年同区间
)

// compareModes 是合法的对比模式集合（保存图表时校验）。
var compareModes = map[string]bool{CompareOff: true, ComparePeriod: true, CompareYear: true}

// CompareInfo 是指标卡的对比结果：本期值、上期值与变化百分比。
type CompareInfo struct {
	Mode     string   `json:"mode"`
	Current  float64  `json:"current"`
	Previous *float64 `json:"previous,omitempty"` // 上期无数据（如新店开业）时缺省
	DeltaPct *float64 `json:"deltaPct,omitempty"` // 上期为 0 时不给百分比（除零没有意义）
}

// timeWindow 是从过滤条件里推出来的当前区间 [lo, hi]。
type timeWindow struct {
	lo, hi time.Time
}

// currentWindow 找第一个「同一时间列上同时有下界与上界」的窗口。
// 列类型必须确是 time：数字列的 between 是金额区间，拿来平移没有意义。
func currentWindow(tg *Target, filters []Filter) (string, *timeWindow) {
	los := map[string]time.Time{}
	his := map[string]time.Time{}
	order := []string{}
	remember := func(col string, t time.Time, lower bool) {
		if _, seen := los[col]; !seen {
			if _, seen2 := his[col]; !seen2 {
				order = append(order, col)
			}
		}
		if lower {
			if old, ok := los[col]; !ok || t.After(old) {
				los[col] = t // 下界取最大：交集
			}
		} else {
			if old, ok := his[col]; !ok || t.Before(old) {
				his[col] = t // 上界取最小：交集
			}
		}
	}
	for _, f := range filters {
		c, ok := tg.cols[f.Column]
		if !ok || c.Type != TypeTime {
			continue
		}
		switch f.Op {
		case ">=", ">":
			if t, ok := timeValue(f.Value); ok {
				remember(f.Column, t, true)
			}
		case "<", "<=":
			if t, ok := timeValue(f.Value); ok {
				remember(f.Column, t, false)
			}
		case "between":
			arr, ok := f.Value.([]any)
			if !ok || len(arr) != 2 {
				continue
			}
			lo, ok1 := timeValue(arr[0])
			hi, ok2 := timeValue(arr[1])
			if ok1 && ok2 && lo.Before(hi) {
				remember(f.Column, lo, true)
				remember(f.Column, hi, false)
			}
		}
	}
	for _, col := range order {
		lo, hasLo := los[col]
		hi, hasHi := his[col]
		if hasLo && hasHi && lo.Before(hi) {
			return col, &timeWindow{lo: lo, hi: hi}
		}
	}
	return "", nil
}

// timeValue 把过滤值解析成时间（runtimeFilters 落下来的都是字符串）。
func timeValue(v any) (time.Time, bool) {
	s, ok := v.(string)
	if !ok {
		return time.Time{}, false
	}
	return parseTime(s)
}

// shiftWindowFilters 把时间列上的范围条件整体平移（环比=区间长度，同比=一年），
// 其余条件原样保留。返回新切片，不改原 spec。
func shiftWindowFilters(filters []Filter, col string, win *timeWindow, mode string) []Filter {
	span := win.hi.Sub(win.lo)
	shift := func(t time.Time) time.Time {
		if mode == CompareYear {
			return t.AddDate(-1, 0, 0)
		}
		return t.Add(-span)
	}
	out := make([]Filter, len(filters))
	copy(out, filters)
	for i := range out {
		f := &out[i]
		if f.Column != col {
			continue
		}
		switch f.Op {
		case ">=", ">", "<", "<=":
			if t, ok := timeValue(f.Value); ok {
				f.Value = shift(t).Format(time.RFC3339)
			}
		case "between":
			if arr, ok := f.Value.([]any); ok && len(arr) == 2 {
				lo, ok1 := timeValue(arr[0])
				hi, ok2 := timeValue(arr[1])
				if ok1 && ok2 {
					f.Value = []any{shift(lo).Format(time.RFC3339), shift(hi).Format(time.RFC3339)}
				}
			}
		}
	}
	return out
}

// metricValue 取指标卡结果里的第一个度量值（m0），转成 float64；无行/无值算 0。
func metricValue(res *Result) float64 {
	if res == nil {
		return 0
	}
	for _, row := range res.Rows {
		if v, ok := row["m0"]; ok && v != nil {
			switch x := v.(type) {
			case int64:
				return float64(x)
			case float64:
				return x
			}
		}
		break // 指标卡只有一行
	}
	return 0
}

// compareInfo 组装对比结果；上期行缺失 → Previous 缺省；上期为 0 → DeltaPct 缺省。
func compareInfo(mode string, cur, prev float64, hasPrev bool) *CompareInfo {
	info := &CompareInfo{Mode: mode, Current: cur}
	if !hasPrev {
		return info
	}
	info.Previous = &prev
	if prev != 0 {
		delta := math.Round((cur-prev)/math.Abs(prev)*1000) / 10 // 一位小数；上期为负时分母取绝对值
		info.DeltaPct = &delta
	}
	return info
}

// attachCompare 给指标卡结果补对比信息：跑一遍平移后的查询，失败只记日志不拖垮主数据。
func (s *Service) attachCompare(ctx context.Context, cc ChartConfig, tg *Target, sp Spec, env Env, res *Result) *Result {
	if cc.Compare == "" || cc.Compare == CompareOff {
		return res
	}
	col, win := currentWindow(tg, sp.Filters)
	if win == nil {
		return res // 没有可比较的时间窗：按无对比返回，不瞎猜
	}
	prevSpec := sp
	prevSpec.Filters = shiftWindowFilters(sp.Filters, col, win, cc.Compare)
	prevRes, err := s.eng.Run(ctx, tg, prevSpec, env)
	if err != nil {
		if s.log != nil {
			s.log.Warn("report: compare query failed", zap.Error(err))
		}
		return res
	}
	cur, hasCur := firstMeasure(res)
	prev, hasPrev := firstMeasure(prevRes)
	if !hasCur {
		return res
	}
	res.Compare = compareInfo(cc.Compare, cur, prev, hasPrev)
	return res
}

// firstMeasure 取结果第一行的 m0（存在性独立于值：0 也是有效上期值）。
func firstMeasure(res *Result) (float64, bool) {
	if res == nil || len(res.Rows) == 0 {
		return 0, false
	}
	v, ok := res.Rows[0]["m0"]
	if !ok || v == nil {
		return 0, false
	}
	switch x := v.(type) {
	case int64:
		return float64(x), true
	case float64:
		return x, true
	}
	return 0, false
}
