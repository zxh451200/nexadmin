package monitorsnap

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"strings"
	"testing"
	"time"
)

type fakeStore struct {
	saved  []Row
	kinds  []string
	purged []time.Time
	err    error
}

func (f *fakeStore) Save(_ context.Context, kind string, at time.Time, m Metrics) error {
	if f.err != nil {
		return f.err
	}
	f.saved = append(f.saved, Row{TS: at, Metrics: m})
	f.kinds = append(f.kinds, kind)
	return nil
}

func (f *fakeStore) Query(context.Context, string, time.Time, time.Time) ([]Row, error) {
	return f.saved, f.err
}

func (f *fakeStore) Purge(_ context.Context, before time.Time) (int64, error) {
	f.purged = append(f.purged, before)
	var keep []Row
	var n int64
	for _, r := range f.saved {
		if r.TS.Before(before) {
			n++
			continue
		}
		keep = append(keep, r)
	}
	f.saved = keep
	return n, f.err
}

func TestCollectSkipsEmptySample(t *testing.T) {
	st := &fakeStore{}
	at := time.Unix(1700000000, 0)
	var purgeAt time.Time
	collect(st, KindServer, func() Metrics { return nil }, nil, &purgeAt, at)
	if len(st.saved) != 0 {
		t.Fatalf("空样本不该落库，实际 %d 行", len(st.saved))
	}
	collect(st, KindServer, func() Metrics { return Metrics{"cpuUsage": 12} }, nil, &purgeAt, at)
	if len(st.saved) != 1 || st.kinds[0] != KindServer || st.saved[0].Metrics["cpuUsage"] != 12 {
		t.Fatalf("一次采样应落一行： %+v", st.saved)
	}
}

// 首轮就要清理（进程可能停机多天），之后每小时最多一次。
func TestCollectPurgesFirstAndThenHourly(t *testing.T) {
	st := &fakeStore{}
	var purgeAt time.Time
	base := time.Unix(1700000000, 0)
	collect(st, KindAPI, func() Metrics { return Metrics{"qps": 1} }, nil, &purgeAt, base)
	if len(st.purged) != 1 {
		t.Fatalf("首轮必须清理一次，实际 %d 次", len(st.purged))
	}
	if want := base.Add(-Retention); !st.purged[0].Equal(want) {
		t.Fatalf("清理边界应为 now-Retention(%s)，实际 %s", want, st.purged[0])
	}
	collect(st, KindAPI, func() Metrics { return Metrics{"qps": 1} }, nil, &purgeAt, base.Add(time.Minute))
	if len(st.purged) != 1 {
		t.Fatalf("一小时内不该再清理，实际 %d 次", len(st.purged))
	}
	collect(st, KindAPI, func() Metrics { return Metrics{"qps": 1} }, nil, &purgeAt, base.Add(purgeEvery+time.Second))
	if len(st.purged) != 2 {
		t.Fatalf("超过 purgeEvery 必须再清理，实际 %d 次", len(st.purged))
	}
}

// 存储报错只记日志：既不 panic，也不能把失败算成已写入。
func TestCollectKeepsGoingWhenStoreFails(t *testing.T) {
	st := &fakeStore{err: fmt.Errorf("boom")}
	var purgeAt time.Time
	collect(st, KindCache, func() Metrics { return Metrics{"keys": 3} }, nil, &purgeAt, time.Unix(1700000000, 0))
	if len(st.saved) != 0 {
		t.Fatal("存储失败不该记为已写入")
	}
}

// 24 小时窗口必须降采样：桶宽对齐采样周期整数倍，点数不超上限，均值保持常量。
func TestDownsampleBucketsAndCapsPoints(t *testing.T) {
	from := time.Unix(1700000000, 0).UTC()
	to := from.Add(24 * time.Hour)
	var rows []Row
	for ts := from; ts.Before(to); ts = ts.Add(SampleEvery) {
		rows = append(rows, Row{TS: ts, Metrics: Metrics{"cpuUsage": 10, "goroutines": 100}})
	}
	view := Downsample(rows, from, to)
	if view.StepSeconds != 300 {
		t.Fatalf("24 小时窗口的桶宽应为 300s，实际 %ds", view.StepSeconds)
	}
	if len(view.Points) == 0 || len(view.Points) > MaxPoints+1 {
		t.Fatalf("点数 %d 不在 (0, %d] 内", len(view.Points), MaxPoints+1)
	}
	for _, p := range view.Points {
		if p.Metrics["cpuUsage"] != 10 || p.Metrics["goroutines"] != 100 {
			t.Fatalf("均值算错：%+v", p.Metrics)
		}
	}
	for i := 1; i < len(view.Points); i++ {
		if view.Points[i-1].TS >= view.Points[i].TS {
			t.Fatalf("点必须按时间严格升序：%d >= %d", view.Points[i-1].TS, view.Points[i].TS)
		}
	}
}

// 同桶取平均；窗口右界排他；NaN 不进均值（该键在该桶缺失，其它键照常）。
func TestDownsampleAveragesWithinBucket(t *testing.T) {
	from := time.Unix(1700000000, 0).UTC()
	to := from.Add(24 * time.Hour) // 桶宽 300s
	rows := []Row{
		{TS: from, Metrics: Metrics{"qps": 10}},
		{TS: from.Add(2 * time.Minute), Metrics: Metrics{"qps": 20}}, // 与上一行同桶 → 15
		{TS: from.Add(6 * time.Minute), Metrics: Metrics{"qps": 30}}, // 第二桶
		{TS: to.Add(-time.Second), Metrics: Metrics{"qps": 40}},      // 最后一桶，窗口内
		{TS: to, Metrics: Metrics{"qps": 77}},                        // 右界排他
		{TS: to.Add(time.Minute), Metrics: Metrics{"qps": 88}},       // 窗口外
		{TS: from.Add(30 * time.Minute), Metrics: Metrics{"qps": math.NaN(), "keys": 5}},
	}
	view := Downsample(rows, from, to)
	byTS := make(map[int64]Metrics, len(view.Points))
	for _, p := range view.Points {
		byTS[p.TS] = p.Metrics
	}
	if got := byTS[from.UnixMilli()]["qps"]; got != 15 {
		t.Fatalf("首桶均值应为 15，实际 %v", got)
	}
	if got := byTS[from.Add(5*time.Minute).UnixMilli()]["qps"]; got != 30 { // 6 分钟那行落在 5 分钟起的桶
		t.Fatalf("第二桶应为 30，实际 %v", got)
	}
	nan := byTS[from.Add(30*time.Minute).UnixMilli()]
	if _, ok := nan["qps"]; ok {
		t.Fatalf("NaN 不该进均值：%v", nan["qps"])
	}
	if nan["keys"] != 5 {
		t.Fatalf("同桶的其它键必须保留：%+v", nan)
	}
	for ts, m := range byTS {
		if v, ok := m["qps"]; ok && (v == 77 || v == 88) {
			t.Fatalf("ts=%d 的窗口外值 %v 不该出现", ts, v)
		}
	}
	if _, ok := byTS[to.UnixMilli()]; ok {
		t.Fatal("右界 to 必须排他")
	}
	if last := byTS[to.Add(-5*time.Minute).UnixMilli()]; last["qps"] != 40 {
		t.Fatalf("最后一桶应为 40，实际 %+v", last)
	}
}

// 空历史必须是 []：前端直接 points.map，nil 会变 null 崩页。
func TestDownsampleEmptyMarshalsAsArray(t *testing.T) {
	from := time.Unix(1700000000, 0).UTC()
	raw, err := json.Marshal(Downsample(nil, from, from.Add(time.Hour)))
	if err != nil {
		t.Fatal(err)
	}
	s := string(raw)
	if !strings.Contains(s, `"points":[]`) {
		t.Fatalf("空历史应序列化为 \"points\":[]，实际：%s", s)
	}
	if strings.Contains(s, "null") {
		t.Fatalf("不应出现 null：%s", s)
	}
}
