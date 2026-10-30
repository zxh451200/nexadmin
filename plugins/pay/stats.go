package pay

// 支付统计：一条 SELECT 取窄投影明细，金额汇总与漏斗都在 Go 里算。
//
// 两条硬口径：
//  1. 金额绝不跨币种相加——所有钱都按 currency 分组输出（ByCurrency / ByChannel），
//     趋势只给「笔数」这种无币种含义的量，避免一张图里混着 CNY 和 USD 的分。
//  2. GMV 只算已支付（succeeded / partially_refunded / refunded / refunding 都代表钱到过账），
//     退款额单独列，净额 = GMV - 退款；手续费另列。口径写在字段名与注释里，不靠前端猜。

import (
	"context"
	"encoding/json"
	"sort"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"nexadmin/core/response"
)

// statsScanLimit 单次统计最多读多少笔订单，超出即截断并如实标 sampled。
const statsScanLimit = 20000

const (
	statsDefaultDays = 30
	statsMaxDays     = 182
)

// StatsQuery 是统计筛选条件；与列表接口同名字段同义。
type StatsQuery struct {
	Status      string
	BizType     string
	ChannelCode string
	Scene       string
	Keyword     string
	StartDate   string
	EndDate     string
	// All 忽略起始时间，从最早一笔开始统计；受 statsScanLimit 采样上限约束。
	All bool
}

// StatsFacets 是筛选下拉的可选值：只按时间窗口与渠道收敛，不受其它结构筛选影响，
// 否则选了业务类型之后，其它类型就从下拉里消失了。
type StatsFacets struct {
	BizTypes []string `json:"bizTypes"`
	Scenes   []string `json:"scenes"`
}

// MarshalJSON 让空 facets 也输出 []：前端直接 map，nil 会变成 null。
func (f StatsFacets) MarshalJSON() ([]byte, error) {
	type plain StatsFacets
	if f.BizTypes == nil {
		f.BizTypes = []string{}
	}
	if f.Scenes == nil {
		f.Scenes = []string{}
	}
	return json.Marshal(plain(f))
}

// CurrencyTotals 是单一币种下的金额汇总（分）。
type CurrencyTotals struct {
	Currency      string  `json:"currency"`
	Orders        int64   `json:"orders"`
	Paid          int64   `json:"paid"`
	GmvMinor      int64   `json:"gmvMinor"`
	RefundedMinor int64   `json:"refundedMinor"`
	FeeMinor      int64   `json:"feeMinor"`
	NetMinor      int64   `json:"netMinor"`
	RefundRate    float64 `json:"refundRate"`
}

// ChannelTotals 是某渠道在某一币种下的成交（分）。
type ChannelTotals struct {
	Channel  string `json:"channel"`
	Currency string `json:"currency"`
	Paid     int64  `json:"paid"`
	GmvMinor int64  `json:"gmvMinor"`
}

// PayDayPoint 是按天的笔数分桶（不含金额，避免跨币种求和）。
type PayDayPoint struct {
	Date   string `json:"date"`
	Orders int64  `json:"orders"`
	Paid   int64  `json:"paid"`
}

// NameValue 是「名字 + 数量」。
type NameValue struct {
	Name  string `json:"name"`
	Value int64  `json:"value"`
}

// PayStats 是 GET /plugins/pay/orders/stats 的响应。
type PayStats struct {
	From        string           `json:"from"`
	To          string           `json:"to"`
	Orders      int64            `json:"orders"`
	Paid        int64            `json:"paid"`
	Sampled     bool             `json:"sampled"`
	Days        []PayDayPoint    `json:"days"`
	ByCurrency  []CurrencyTotals `json:"byCurrency"`
	ByChannel   []ChannelTotals  `json:"byChannel"`
	ByStatus    []NameValue      `json:"byStatus"`
	ByScene     []NameValue      `json:"byScene"`
	ByMethod    []NameValue      `json:"byMethod"`
	ByBizType   []NameValue      `json:"byBizType"`
	FailReasons []NameValue      `json:"failReasons"`
	Facets      StatsFacets      `json:"facets"`
}

// payStatRow 是统计用的窄投影：不带 metadata/subject 等长字段。
type payStatRow struct {
	CreatedAt     time.Time
	PaidAt        *time.Time
	Status        string
	Currency      string
	AmountMinor   int64
	RefundedMinor int64
	FeeMinor      int64
	ChannelCode   string
	Method        string
	Scene         string
	BizType       string
	FailReason    string
}

// settledStatus 是「钱到过账」的状态集合。
var settledStatus = map[string]bool{
	StatusSucceeded: true, StatusPartiallyRefunded: true, StatusRefunded: true, StatusRefunding: true,
}

func settled(status string) bool { return settledStatus[status] }

// Stats 汇总支付订单。
func (s *Service) Stats(ctx context.Context, q StatsQuery) (PayStats, error) {
	loc := s.loc
	if loc == nil {
		loc = time.UTC
	}
	from, to := statsWindow(q, loc)
	// 时间与渠道是公共范围；结构筛选只收窄主体统计，不参与 facets 取值，
	// 否则选了业务类型之后，其它类型就从下拉里消失了。
	scope := func() *gorm.DB {
		tx := s.db.WithContext(ctx).Model(&Order{}).Where("created_at < ?", to.UTC())
		if !from.IsZero() {
			tx = tx.Where("created_at >= ?", from.UTC())
		}
		if q.ChannelCode != "" {
			tx = tx.Where("channel_code = ?", q.ChannelCode)
		}
		return tx
	}
	var bizTypes, scenes []string
	if err := scope().Distinct().Order("biz_type").Pluck("biz_type", &bizTypes).Error; err != nil {
		return PayStats{}, err
	}
	if err := scope().Distinct().Order("scene").Pluck("scene", &scenes).Error; err != nil {
		return PayStats{}, err
	}

	db := scope()
	if q.Status != "" {
		db = db.Where("status = ?", q.Status)
	}
	if q.BizType != "" {
		db = db.Where("biz_type = ?", q.BizType)
	}
	if q.Scene != "" {
		db = db.Where("scene = ?", q.Scene)
	}
	if kw := strings.TrimSpace(q.Keyword); kw != "" {
		like := "%" + escapeLike(kw) + "%"
		db = db.Where("payment_no LIKE ? OR biz_no LIKE ?", like, like)
	}

	var rows []payStatRow
	if err := db.Select("created_at", "paid_at", "status", "currency", "amount_minor", "refunded_minor", "fee_minor", "channel_code", "method", "scene", "biz_type", "fail_reason").
		Order("created_at DESC").Limit(statsScanLimit + 1).Find(&rows).Error; err != nil {
		return PayStats{}, err
	}
	chartFrom := chartWindow(from, to)
	st := aggregatePay(rows, loc, chartFrom, to)
	st.From, st.To = statsFromLabel(rows, from, chartFrom, loc), to.AddDate(0, 0, -1).Format("2006-01-02")
	st.Facets = StatsFacets{BizTypes: nonEmpty(bizTypes), Scenes: nonEmpty(scenes)}
	return st, nil
}

// chartWindow 是趋势图的起始日：全量历史只画最近 statsMaxDays 天，避免横轴被拉到十年。
func chartWindow(from, to time.Time) time.Time {
	if from.IsZero() || to.Sub(from) > statsMaxDays*24*time.Hour {
		return to.AddDate(0, 0, -statsMaxDays)
	}
	return from
}

// statsFromLabel 是响应里的起始日：固定窗口用窗口起点，全量历史用最早一笔样本。
func statsFromLabel(rows []payStatRow, from, chartFrom time.Time, loc *time.Location) string {
	if !from.IsZero() {
		return from.Format("2006-01-02")
	}
	if len(rows) > 0 {
		return rows[len(rows)-1].CreatedAt.In(loc).Format("2006-01-02")
	}
	return chartFrom.Format("2006-01-02")
}

// nonEmpty 去掉空值并去重，避免筛选下拉出现空白选项。
func nonEmpty(vals []string) []string {
	out := make([]string, 0, len(vals))
	seen := make(map[string]bool, len(vals))
	for _, v := range vals {
		v = strings.TrimSpace(v)
		if v == "" || seen[v] {
			continue
		}
		seen[v] = true
		out = append(out, v)
	}
	return out
}

func escapeLike(s string) string {
	return strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(s)
}

func statsWindow(q StatsQuery, loc *time.Location) (time.Time, time.Time) {
	to := time.Now().In(loc)
	if e, err := time.ParseInLocation("2006-01-02", q.EndDate, loc); err == nil {
		to = e.AddDate(0, 0, 1)
	}
	if q.All {
		return time.Time{}, to
	}
	from := to.AddDate(0, 0, -statsDefaultDays)
	if st, err := time.ParseInLocation("2006-01-02", q.StartDate, loc); err == nil {
		from = st
	}
	if to.Sub(from) > statsMaxDays*24*time.Hour {
		from = to.AddDate(0, 0, -statsMaxDays)
	}
	return from, to
}

func aggregatePay(rows []payStatRow, loc *time.Location, from, to time.Time) PayStats {
	out := PayStats{}
	out.Sampled = int64(len(rows)) > statsScanLimit
	if out.Sampled {
		rows = rows[:statsScanLimit] // 先截断：所有分桶只统计被计入的样本
	}
	out.Orders = int64(len(rows))
	status := map[string]int64{}
	scene := map[string]int64{}
	method := map[string]int64{}
	biz := map[string]int64{}
	fails := map[string]int64{}
	type curAgg struct {
		orders, paid, gmv, refunded, fee int64
	}
	curs := map[string]*curAgg{}
	type chAgg struct {
		key  [2]string
		paid int64
		gmv  int64
	}
	chs := map[[2]string]*chAgg{}
	days := map[string]*PayDayPoint{}

	for _, r := range rows {
		incName(status, r.Status)
		incName(scene, r.Scene)
		incName(method, r.Method)
		incName(biz, r.BizType)
		if r.Status == StatusFailed {
			incName(fails, r.FailReason) // 空原因归到占位符 -，不静默丢掉一条失败
		}
		key := r.CreatedAt.In(loc).Format("2006-01-02")
		dp := days[key]
		if dp == nil {
			dp = &PayDayPoint{Date: key}
			days[key] = dp
		}
		dp.Orders++
		c := curs[r.Currency]
		if c == nil {
			c = &curAgg{}
			curs[r.Currency] = c
		}
		c.orders++
		if settled(r.Status) {
			out.Paid++
			c.paid++
			dp.Paid++
			c.gmv += r.AmountMinor
			ck := [2]string{r.ChannelCode, r.Currency}
			ch := chs[ck]
			if ch == nil {
				ch = &chAgg{key: ck}
				chs[ck] = ch
			}
			ch.paid++
			ch.gmv += r.AmountMinor
		}
		c.refunded += r.RefundedMinor
		c.fee += r.FeeMinor
	}

	out.Days = fillPayDays(days, from, to)
	names := make([]string, 0, len(curs))
	for k := range curs {
		names = append(names, k)
	}
	sort.Strings(names)
	// 空结果必须是 []，不能让 nil 切片序列化成 null：前端直接 days.map/byCurrency[0]。
	out.ByCurrency = make([]CurrencyTotals, 0, len(names))
	for _, k := range names {
		c := curs[k]
		rate := 0.0
		if c.gmv > 0 {
			rate = round1(float64(c.refunded) / float64(c.gmv) * 100)
		}
		out.ByCurrency = append(out.ByCurrency, CurrencyTotals{
			Currency: k, Orders: c.orders, Paid: c.paid, GmvMinor: c.gmv,
			RefundedMinor: c.refunded, FeeMinor: c.fee, NetMinor: c.gmv - c.refunded, RefundRate: rate,
		})
	}
	keys := make([][2]string, 0, len(chs))
	for k := range chs {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool {
		a, bb := chs[keys[i]], chs[keys[j]]
		if a.gmv != bb.gmv {
			return a.gmv > bb.gmv
		}
		if a.key[0] != bb.key[0] {
			return a.key[0] < bb.key[0]
		}
		return a.key[1] < bb.key[1]
	})
	out.ByChannel = make([]ChannelTotals, 0, len(keys))
	for _, k := range keys {
		a := chs[k]
		out.ByChannel = append(out.ByChannel, ChannelTotals{Channel: a.key[0], Currency: a.key[1], Paid: a.paid, GmvMinor: a.gmv})
	}
	out.ByStatus = topNames(status, 8)
	out.ByScene = topNames(scene, 8)
	out.ByMethod = topNames(method, 8)
	out.ByBizType = topNames(biz, 8)
	out.FailReasons = topNames(fails, 8)
	return out
}

// fillPayDays 补齐窗口内没有订单的日期；右界 to 是排他的，循环必须用 d.Before(to)。
func fillPayDays(bucket map[string]*PayDayPoint, from, to time.Time) []PayDayPoint {
	seen := map[string]bool{}
	for k := range bucket {
		seen[k] = true
	}
	out := make([]PayDayPoint, 0, len(bucket)+1)
	for d := time.Date(from.Year(), from.Month(), from.Day(), 0, 0, 0, 0, from.Location()); d.Before(to) && len(out) <= statsMaxDays; d = d.AddDate(0, 0, 1) {
		key := d.Format("2006-01-02")
		if dp := bucket[key]; dp != nil {
			out = append(out, *dp)
			delete(seen, key)
			continue
		}
		out = append(out, PayDayPoint{Date: key})
	}
	extra := make([]string, 0, len(seen))
	for k := range seen {
		extra = append(extra, k)
	}
	sort.Strings(extra)
	for _, k := range extra {
		out = append(out, *bucket[k])
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Date < out[j].Date })
	return out
}

func incName(m map[string]int64, key string) {
	if strings.TrimSpace(key) == "" {
		key = "-"
	}
	m[key]++
}

func topNames(m map[string]int64, n int) []NameValue {
	items := make([]NameValue, 0, len(m))
	for name, value := range m {
		items = append(items, NameValue{Name: name, Value: value})
	}
	sort.SliceStable(items, func(i, j int) bool {
		if items[i].Value != items[j].Value {
			return items[i].Value > items[j].Value
		}
		return items[i].Name < items[j].Name
	})
	if len(items) > n {
		items = items[:n]
	}
	return items
}

func round1(v float64) float64 {
	return float64(int64(v*10+0.5)) / 10
}

// Stats GET /plugins/pay/orders/stats：成交、渠道与退款概览。
func (h *handler) Stats(c *gin.Context) {
	q := StatsQuery{
		Status:      c.Query("status"),
		BizType:     c.Query("bizType"),
		ChannelCode: c.Query("channelCode"),
		Scene:       c.Query("scene"),
		Keyword:     c.Query("keyword"),
		StartDate:   c.Query("startDate"),
		EndDate:     c.Query("endDate"),
		All:         c.Query("all") == "1" || strings.EqualFold(c.Query("all"), "true"),
	}
	st, err := h.svc.Stats(c.Request.Context(), q)
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, st)
}
