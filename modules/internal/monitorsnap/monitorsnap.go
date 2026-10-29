// Package monitorsnap 把服务/缓存/接口监控的数值指标按固定周期落库，供历史趋势查询。
// 三个监控模块各自采样后调用 Loop 写入 sys_monitor_snapshot（一行一组扁平指标），
// 历史查询在 Go 里降采样：三引擎的日期分桶与百分位写法不一致，SQL 只做「按 kind+时间区间取行」。
package monitorsnap

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/http"
	"sort"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
	"gorm.io/gorm"

	"nexadmin/core/database"
	"nexadmin/core/response"
)

const (
	// SampleEvery 是采样周期：30 秒 × 3 类 × 7 天 ≈ 6 万行，量级可控。
	SampleEvery = 30 * time.Second
	// Retention 是保留时长，超期由同一循环每小时清理一次。
	Retention  = 7 * 24 * time.Hour
	purgeEvery = time.Hour

	// DefaultHours / MaxHours 是历史查询的窗口边界。
	DefaultHours = 6
	MaxHours     = 7 * 24
	// MaxPoints 限制单次返回的点数，超出则按桶降采样。
	MaxPoints = 300
)

// 三类监控的 kind 取值，与前端历史接口一一对应。
const (
	KindServer = "server"
	KindCache  = "cache"
	KindAPI    = "api"
)

// Snapshot 对应 sys_monitor_snapshot 的一行。
type Snapshot struct {
	database.PlatformModel
	Kind    string    `gorm:"size:16;not null" json:"kind"`
	TS      time.Time `gorm:"column:ts;not null" json:"ts"`
	Payload string    `gorm:"type:text;not null" json:"-"`
}

func (Snapshot) TableName() string { return "sys_monitor_snapshot" }

// Metrics 是一组可平均的扁平数值指标，键名与前端图表约定一致。
type Metrics map[string]float64

// Row 是一次采样记录（查询用，不含主键等落库细节）。
type Row struct {
	TS      time.Time
	Metrics Metrics
}

// Store 是落库与查询接口；单测用内存实现替换。
type Store interface {
	Save(ctx context.Context, kind string, at time.Time, m Metrics) error
	Query(ctx context.Context, kind string, from, to time.Time) ([]Row, error)
	Purge(ctx context.Context, before time.Time) (int64, error)
}

type dbStore struct{ db *gorm.DB }

// NewStore 返回基于 GORM 的实现。
func NewStore(db *gorm.DB) Store { return &dbStore{db: db} }

func (s *dbStore) Save(ctx context.Context, kind string, at time.Time, m Metrics) error {
	payload, err := json.Marshal(m)
	if err != nil {
		return fmt.Errorf("monitorsnap: 序列化 %s 指标失败: %w", kind, err)
	}
	row := Snapshot{Kind: kind, TS: at.UTC(), Payload: string(payload)}
	return s.db.WithContext(ctx).Create(&row).Error
}

func (s *dbStore) Query(ctx context.Context, kind string, from, to time.Time) ([]Row, error) {
	var rows []Snapshot
	err := s.db.WithContext(ctx).Where("kind = ? AND ts >= ? AND ts < ?", kind, from.UTC(), to.UTC()).
		Order("ts ASC").Find(&rows).Error
	if err != nil {
		return nil, err
	}
	out := make([]Row, 0, len(rows))
	for _, r := range rows {
		m := Metrics{}
		if err := json.Unmarshal([]byte(r.Payload), &m); err != nil {
			return nil, fmt.Errorf("monitorsnap: 解析 %s 在 %s 的指标失败: %w", r.Kind, r.TS.Format(time.RFC3339), err)
		}
		out = append(out, Row{TS: r.TS, Metrics: m})
	}
	return out, nil
}

func (s *dbStore) Purge(ctx context.Context, before time.Time) (int64, error) {
	res := s.db.WithContext(ctx).Where("ts < ?", before.UTC()).Delete(&Snapshot{})
	return res.RowsAffected, res.Error
}

// Loop 立即采样一次，之后每 SampleEvery 一次，并每小时清理一次过期快照；stop 关闭时退出。
func Loop(stop <-chan struct{}, store Store, kind string, sample func() Metrics, log *zap.Logger) {
	var purgeAt time.Time
	tick := func() { collect(store, kind, sample, log, &purgeAt, time.Now()) }
	tick()
	t := time.NewTicker(SampleEvery)
	defer t.Stop()
	for {
		select {
		case <-t.C:
			tick()
		case <-stop:
			return
		}
	}
}

// collect 落一次采样并按需清理：sample 返回空指标时跳过写入
// （例如非 Linux 读不到 /proc，落 0 会让历史图变成一条假平线）。
func collect(store Store, kind string, sample func() Metrics, log *zap.Logger, purgeAt *time.Time, now time.Time) {
	if log == nil {
		log = zap.NewNop()
	}
	if m := sample(); len(m) > 0 {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := store.Save(ctx, kind, now, m); err != nil {
			log.Warn("monitorsnap: 落库失败", zap.String("kind", kind), zap.Error(err))
		}
	}
	if now.Sub(*purgeAt) < purgeEvery {
		return
	}
	*purgeAt = now
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	if n, err := store.Purge(ctx, now.Add(-Retention)); err != nil {
		log.Warn("monitorsnap: 清理失败", zap.Error(err))
	} else if n > 0 {
		log.Info("monitorsnap: 清理过期快照", zap.Int64("rows", n))
	}
}

// Point 是降采样后的一个历史点；TS 为毫秒时间戳，与实时接口的 ts 字段同一口径。
type Point struct {
	TS      int64   `json:"ts"`
	Metrics Metrics `json:"metrics"`
}

// HistoryView 是 GET /monitor/<x>/history 的响应。
type HistoryView struct {
	From        string  `json:"from"`
	To          string  `json:"to"`
	StepSeconds int64   `json:"stepSeconds"`
	Points      []Point `json:"points"`
}

// Downsample 按固定桶宽对行做均值聚合；桶宽取 max(SampleEvery, 区间/MaxPoints) 并对齐到 SampleEvery。
// 返回按时间升序的点，空输入返回空切片（不是 nil，前端直接 map）。
func Downsample(rows []Row, from, to time.Time) HistoryView {
	span := to.Sub(from)
	if span <= 0 {
		span = SampleEvery
	}
	step := span / time.Duration(MaxPoints)
	step = step.Truncate(SampleEvery) + SampleEvery // 向上取到 SampleEvery 的整数倍
	if step < SampleEvery {
		step = SampleEvery
	}
	view := HistoryView{
		From:        from.Format(time.RFC3339),
		To:          to.Format(time.RFC3339),
		StepSeconds: int64(step.Seconds()),
		Points:      make([]Point, 0, MaxPoints+1),
	}
	type acc struct {
		sum   map[string]float64
		count map[string]int
	}
	buckets := map[int64]*acc{}
	for _, r := range rows {
		if r.TS.Before(from) || !r.TS.Before(to) {
			continue
		}
		key := from.Add(time.Duration(int64(r.TS.Sub(from)) / int64(step) * int64(step))).UnixMilli()
		b := buckets[key]
		if b == nil {
			b = &acc{sum: map[string]float64{}, count: map[string]int{}}
			buckets[key] = b
		}
		for k, v := range r.Metrics {
			if math.IsNaN(v) || math.IsInf(v, 0) {
				continue
			}
			b.sum[k] += v
			b.count[k]++
		}
	}
	keys := make([]int64, 0, len(buckets))
	for k := range buckets {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool { return keys[i] < keys[j] })
	for _, k := range keys {
		b := buckets[k]
		m := make(Metrics, len(b.sum))
		for name, sum := range b.sum {
			if b.count[name] == 0 {
				continue
			}
			m[name] = sum / float64(b.count[name])
		}
		view.Points = append(view.Points, Point{TS: k, Metrics: m})
	}
	return view
}

// ErrBadHours 是非法的 hours 参数。
var ErrBadHours = errors.New("monitorsnap: invalid hours")

// Hours 解析 ?hours= 并夹到 [1, MaxHours]。
func Hours(c *gin.Context) (int, error) {
	raw := c.Query("hours")
	if raw == "" {
		return DefaultHours, nil
	}
	h, err := strconv.Atoi(raw)
	if err != nil || h < 1 || h > MaxHours {
		return 0, ErrBadHours
	}
	return h, nil
}

// HandleHistory 是 GET /monitor/<x>/history 的公共实现。
func HandleHistory(store Store, kind string) gin.HandlerFunc {
	return func(c *gin.Context) {
		hours, err := Hours(c)
		if err != nil {
			response.Fail(c, response.New(http.StatusBadRequest, "error.invalid_param"))
			return
		}
		to := time.Now()
		from := to.Add(-time.Duration(hours) * time.Hour)
		rows, err := store.Query(c.Request.Context(), kind, from, to)
		if err != nil {
			response.Fail(c, err)
			return
		}
		response.OK(c, Downsample(rows, from, to))
	}
}
