package pay

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func at(s string) time.Time {
	v, err := time.Parse("2006-01-02 15:04:05Z", s+"Z")
	if err != nil {
		panic(err)
	}
	return v.UTC()
}

func tp(t time.Time) *time.Time { return &t }

var locUTC = time.UTC

func window() (time.Time, time.Time) {
	return at("2026-10-21 00:00:00"), at("2026-10-24 00:00:00") // 右界排他：21/22/23 三天
}

// 金额不得跨币种相加：两个币种必须各成一行，趋势只给笔数。
func TestAggregatePayKeepsCurrenciesApart(t *testing.T) {
	from, to := window()
	rows := []payStatRow{
		{CreatedAt: at("2026-10-21 01:00:00"), Status: StatusSucceeded, Currency: "CNY", AmountMinor: 10000, FeeMinor: 60, ChannelCode: "wechat"},
		{CreatedAt: at("2026-10-21 02:00:00"), Status: StatusSucceeded, Currency: "USD", AmountMinor: 5000, FeeMinor: 30, ChannelCode: "stripe"},
		{CreatedAt: at("2026-10-22 03:00:00"), Status: StatusPending, Currency: "CNY", AmountMinor: 99999, ChannelCode: "wechat"},
		{CreatedAt: at("2026-10-22 04:00:00"), Status: StatusPartiallyRefunded, Currency: "CNY", AmountMinor: 20000, RefundedMinor: 5000, ChannelCode: "alipay"},
	}
	st := aggregatePay(rows, locUTC, from, to)
	if st.Orders != 4 || st.Paid != 3 {
		t.Fatalf("笔数错误：orders=%d paid=%d", st.Orders, st.Paid)
	}
	if len(st.ByCurrency) != 2 {
		t.Fatalf("两个币种必须分开输出：%+v", st.ByCurrency)
	}
	cny := st.ByCurrency[0]
	if cny.Currency != "CNY" || cny.GmvMinor != 30000 || cny.RefundedMinor != 5000 || cny.NetMinor != 25000 || cny.FeeMinor != 60 {
		t.Errorf("CNY 口径错误：%+v（GMV 应只算已支付，未支付那笔 99999 不能进）", cny)
	}
	if cny.RefundRate != 16.7 {
		t.Errorf("退款率应为 5000/30000=16.7%%，得到 %v", cny.RefundRate)
	}
	usd := st.ByCurrency[1]
	if usd.Currency != "USD" || usd.GmvMinor != 5000 {
		t.Errorf("USD 口径错误：%+v", usd)
	}
	// 趋势只给笔数，绝不出现金额字段被跨币种相加的地方。
	if len(st.Days) != 3 || st.Days[0].Orders != 2 || st.Days[1].Orders != 2 || st.Days[2].Orders != 0 {
		t.Errorf("按天笔数/空白天错误：%+v", st.Days)
	}
	if st.Days[0].Paid != 2 || st.Days[1].Paid != 1 {
		t.Errorf("按天成交笔数错误：%+v", st.Days)
	}
}

func TestAggregatePayChannelBreakdownCarriesCurrency(t *testing.T) {
	from, to := window()
	rows := []payStatRow{
		{CreatedAt: at("2026-10-21 01:00:00"), Status: StatusSucceeded, Currency: "USD", AmountMinor: 900, ChannelCode: "stripe"},
		{CreatedAt: at("2026-10-21 02:00:00"), Status: StatusSucceeded, Currency: "USD", AmountMinor: 100, ChannelCode: "stripe"},
		{CreatedAt: at("2026-10-21 03:00:00"), Status: StatusSucceeded, Currency: "CNY", AmountMinor: 50, ChannelCode: "stripe"},
	}
	st := aggregatePay(rows, locUTC, from, to)
	if len(st.ByChannel) != 2 {
		t.Fatalf("同渠道不同币种必须分行：%+v", st.ByChannel)
	}
	if st.ByChannel[0].Channel != "stripe" || st.ByChannel[0].Currency != "USD" || st.ByChannel[0].GmvMinor != 1000 {
		t.Errorf("渠道聚合错误：%+v", st.ByChannel)
	}
}

func TestAggregatePaySampledCapsAllBuckets(t *testing.T) {
	rows := make([]payStatRow, statsScanLimit+4)
	for i := range rows {
		rows[i] = payStatRow{CreatedAt: at("2026-10-21 01:00:00"), Status: StatusSucceeded, Currency: "CNY", AmountMinor: 10, ChannelCode: "wechat"}
	}
	from, to := window()
	st := aggregatePay(rows, locUTC, from, to)
	if !st.Sampled {
		t.Fatal("超过上限必须标 sampled")
	}
	if st.Orders != statsScanLimit || st.Paid != statsScanLimit {
		t.Errorf("total/paid 必须压到上限：%d/%d", st.Orders, st.Paid)
	}
	if st.ByCurrency[0].GmvMinor != statsScanLimit*10 {
		t.Errorf("金额也不能算进被丢弃的样本：%+v", st.ByCurrency[0])
	}
}

func TestPayTopNamesAndFailReasons(t *testing.T) {
	from, to := window()
	rows := []payStatRow{
		{CreatedAt: at("2026-10-21 01:00:00"), Status: StatusFailed, Currency: "CNY", FailReason: "", ChannelCode: "wechat"},
		{CreatedAt: at("2026-10-21 01:10:00"), Status: StatusFailed, Currency: "CNY", FailReason: "timeout", ChannelCode: "wechat"},
		{CreatedAt: at("2026-10-21 01:20:00"), Status: StatusSucceeded, Currency: "CNY", AmountMinor: 5, FailReason: "ignored", ChannelCode: "wechat"},
	}
	st := aggregatePay(rows, locUTC, from, to)
	if len(st.FailReasons) != 2 || st.FailReasons[0] != (NameValue{"-", 1}) {
		t.Errorf("空失败原因应归到占位符 -：%+v", st.FailReasons)
	}
	for _, r := range st.FailReasons {
		if r.Name == "ignored" {
			t.Errorf("已成交订单的 fail_reason 不该进失败原因榜：%+v", st.FailReasons)
		}
	}
	if got := topNames(map[string]int64{"b": 5, "a": 5, "c": 9}, 2); len(got) != 2 || got[0].Name != "c" || got[1].Name != "a" {
		t.Errorf("topNames 排序/截断错误：%+v", got)
	}
}

func TestPayStatsWindow(t *testing.T) {
	from, to := statsWindow(StatsQuery{}, locUTC)
	if d := int(to.Sub(from).Hours() / 24); d != statsDefaultDays {
		t.Errorf("默认窗口 %d 天，期望 %d", d, statsDefaultDays)
	}
	from, to = statsWindow(StatsQuery{StartDate: "2000-01-01", EndDate: "2026-10-20"}, locUTC)
	if d := int(to.Sub(from).Hours() / 24); d > statsMaxDays {
		t.Errorf("窗口必须压到 %d 天内，得到 %d", statsMaxDays, d)
	}
}

// 空聚合必须序列化成 []：前端对 byCurrency[0]、days.map 没有空值分支，
// nil 切片会变成 null 并让报表页整块崩掉。
func TestAggregatePayEmptyMarshalsAsArrays(t *testing.T) {
	from, to := window()
	raw, err := json.Marshal(aggregatePay(nil, locUTC, from, to))
	if err != nil {
		t.Fatalf("序列化失败：%v", err)
	}
	s := string(raw)
	for _, want := range []string{`"byCurrency":[]`, `"byChannel":[]`, `"byStatus":[]`, `"failReasons":[]`} {
		if !strings.Contains(s, want) {
			t.Errorf("空聚合应为 %s，实际：%s", want, s)
		}
	}
	if strings.Contains(s, "null") {
		t.Errorf("空聚合不应出现 null：%s", s)
	}
}
