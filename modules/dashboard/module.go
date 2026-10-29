// Package dashboard 是工作台聚合接口 GET /dashboard/stats，以及按用户保存的卡片布局 /dashboard/layout（表 sys_dashboard_layout）。
//
// 各项数据从其他模块的表里只读统计。框架内置模块之间不 import，所以这里用原生 SQL；
// 某个模块被停用（表不存在）或字段对不上时，对应指标退化为 0 / 空列表，不会让整个接口 500。
// 原生 SQL 不经过租户 scope，下面每条语句都显式带 tenant_id。
package dashboard

import (
	"context"
	"time"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
	"gorm.io/gorm"

	"nexadmin/core"
	"nexadmin/core/auth"
	"nexadmin/core/middleware"
	"nexadmin/core/response"
	"nexadmin/core/tenant"
	"nexadmin/pkg/apistat"
	"nexadmin/pkg/idgen"
)

func init() { core.RegisterBuiltin(module{}) }

const trendDays = 7

// 登录日志里“成功”的判定条件。依赖 log 模块 sys_login_log.status 的取值约定，如有出入只改这两行。
const (
	loginOK   = "status = 'success'"
	loginFail = "status <> 'success'"
)

type module struct{}

func (module) Name() string { return "dashboard" }

// 工作台只要求登录（页面本身不挂权限点），各区块的数据在下面按需降级。
func (module) Register(r *gin.RouterGroup, d *core.Deps) {
	apistat.Install(r, d.Public)
	loc := time.UTC
	if l, err := time.LoadLocation(d.Config.App.Timezone); err == nil && d.Config.App.Timezone != "" {
		loc = l
	}
	s := &service{db: d.DB, log: d.Log, loc: loc, dataDir: d.Config.DataDir}
	r.GET("/dashboard/stats", func(c *gin.Context) {
		response.OK(c, s.stats(c))
	})
	// 布局：登录即可、只读写自己的，不挂权限点；按用户限流（规则 dashboard_layout）。
	rl := middleware.RateLimitRule(d.Cache, d.Log, d.Config.RateLimit, "dashboard_layout", middleware.ByUser())
	r.GET("/dashboard/layout", rl, s.getLayoutHandler)
	r.PUT("/dashboard/layout", rl, s.putLayoutHandler)
	r.DELETE("/dashboard/layout", rl, s.deleteLayoutHandler)
}

// ---- 响应结构，与前端 DashboardStats 一致 ----

type StatValue struct {
	Value int64   `json:"value"`
	Delta float64 `json:"delta"`
	// Comparable=false 表示上期基线太小，百分比没有意义，前端应当隐藏涨跌标签。
	Comparable bool `json:"comparable"`
}

type Storage struct {
	UsedGB     float64 `json:"usedGb"`
	TotalGB    float64 `json:"totalGb"`
	Percent    float64 `json:"percent"`
	Delta      float64 `json:"delta"`
	Comparable bool    `json:"comparable"`
}

type LoginPoint struct {
	Date     string `json:"date"`
	Logins   int64  `json:"logins"`
	Failures int64  `json:"failures"`
}

type RequestPoint struct {
	Date     string `json:"date"`
	Requests int64  `json:"requests"`
	Errors   int64  `json:"errors"`
}

type OperType struct {
	Type  string `json:"type"`
	Value int64  `json:"value"`
}

type Todo struct {
	ID    string    `json:"id"`
	Kind  string    `json:"kind"` // message | notice
	Title string    `json:"title"`
	Time  time.Time `json:"time"`
}

type Notice struct {
	ID        string    `json:"id"`
	Title     string    `json:"title"`
	Type      string    `json:"type"`
	Level     string    `json:"level"`
	PublishAt time.Time `json:"publishAt"`
}

type Stats struct {
	Users          StatValue      `json:"users"`
	TodayLogin     StatValue      `json:"todayLogin"`
	UnreadMessages StatValue      `json:"unreadMessages"`
	Storage        Storage        `json:"storage"`
	LoginTrend     []LoginPoint   `json:"loginTrend"`
	RequestTrend   []RequestPoint `json:"requestTrend"`
	OperTypes      []OperType     `json:"operTypes"`
	Todos          []Todo         `json:"todos"`
	RecentNotices  []Notice       `json:"recentNotices"`
}

type service struct {
	db      *gorm.DB
	log     *zap.Logger
	loc     *time.Location
	dataDir string
}

func (s *service) stats(c *gin.Context) Stats {
	ctx := c.Request.Context()
	tid, _ := tenant.From(ctx)
	var uid idgen.ID
	if cl := auth.ClaimsFrom(c); cl != nil {
		uid = cl.UserID
	}
	now := time.Now().In(s.loc)
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, s.loc)

	out := Stats{LoginTrend: []LoginPoint{}, RequestTrend: []RequestPoint{}, OperTypes: []OperType{}, Todos: []Todo{}, RecentNotices: []Notice{}}

	// 用户总数及近 7 天增长率
	total := s.count(ctx, "users", "SELECT COUNT(*) FROM sys_user WHERE tenant_id = ? AND deleted_at IS NULL", tid)
	added := s.count(ctx, "users.added", "SELECT COUNT(*) FROM sys_user WHERE tenant_id = ? AND deleted_at IS NULL AND created_at >= ?", tid, today.AddDate(0, 0, -7).UTC())
	ud, uok := pctOf(total, total-added)
	out.Users = statValue(total, ud, uok)

	// 今日登录与较昨日变化
	todayN := s.count(ctx, "login.today", "SELECT COUNT(*) FROM sys_login_log WHERE tenant_id = ? AND "+loginOK+" AND created_at >= ?", tid, today.UTC())
	yesterdayN := s.count(ctx, "login.yesterday", "SELECT COUNT(*) FROM sys_login_log WHERE tenant_id = ? AND "+loginOK+" AND created_at >= ? AND created_at < ?",
		tid, today.AddDate(0, 0, -1).UTC(), today.UTC())
	ld, lok := pctOf(todayN, yesterdayN)
	out.TodayLogin = statValue(todayN, ld, lok)

	out.UnreadMessages = StatValue{Value: s.count(ctx, "message.unread", "SELECT COUNT(*) FROM sys_message WHERE tenant_id = ? AND user_id = ? AND read_at IS NULL", tid, uid)}
	out.Storage = s.storage(ctx, today)

	// 近 7 天登录趋势
	for i := trendDays - 1; i >= 0; i-- {
		from := today.AddDate(0, 0, -i)
		to := from.AddDate(0, 0, 1)
		q := "SELECT COUNT(*) FROM sys_login_log WHERE tenant_id = ? AND created_at >= ? AND created_at < ? AND "
		out.LoginTrend = append(out.LoginTrend, LoginPoint{
			Date:     from.Format("01-02"),
			Logins:   s.count(ctx, "login.trend.ok", q+loginOK, tid, from.UTC(), to.UTC()),
			Failures: s.count(ctx, "login.trend.fail", q+loginFail, tid, from.UTC(), to.UTC()),
		})
	}
	// 请求趋势来自进程内统计（apistat），重启后清零
	for _, d := range apistat.Default.Days(trendDays) {
		out.RequestTrend = append(out.RequestTrend, RequestPoint{Date: d.Date.In(s.loc).Format("01-02"), Requests: d.Requests, Errors: d.Errors})
	}

	var ops []OperType
	s.try("oper.types", func() error {
		return s.db.WithContext(ctx).Raw("SELECT type, COUNT(*) AS value FROM sys_oper_log WHERE tenant_id = ? AND created_at >= ? GROUP BY type ORDER BY value DESC LIMIT 5",
			tid, today.AddDate(0, 0, -(trendDays-1)).UTC()).Scan(&ops).Error
	})
	if ops != nil {
		out.OperTypes = ops
	}

	var msgs []struct {
		ID        idgen.ID
		Title     string
		CreatedAt time.Time
	}
	s.try("todos", func() error {
		return s.db.WithContext(ctx).Raw("SELECT id, title, created_at FROM sys_message WHERE tenant_id = ? AND user_id = ? AND read_at IS NULL ORDER BY created_at DESC LIMIT 5",
			tid, uid).Scan(&msgs).Error
	})
	for _, m := range msgs {
		out.Todos = append(out.Todos, Todo{ID: m.ID.String(), Kind: "message", Title: m.Title, Time: m.CreatedAt.UTC()})
	}

	var notices []struct {
		ID        idgen.ID
		Title     string
		Type      string
		Level     string
		PublishAt time.Time
	}
	s.try("notices", func() error {
		return s.db.WithContext(ctx).Raw("SELECT id, title, type, level, publish_at FROM sys_notice WHERE tenant_id = ? AND publish_at IS NOT NULL AND publish_at <= ? ORDER BY publish_at DESC LIMIT 4",
			tid, time.Now().UTC()).Scan(&notices).Error
	})
	for _, n := range notices {
		out.RecentNotices = append(out.RecentNotices, Notice{ID: n.ID.String(), Title: n.Title, Type: n.Type, Level: n.Level, PublishAt: n.PublishAt.UTC()})
	}
	return out
}

// storage 用 sys_file（本模块同包的 file 表，只读）累计已用空间，总量取数据目录所在磁盘。
func (s *service) storage(ctx context.Context, today time.Time) Storage {
	tid, _ := tenant.From(ctx)
	used := s.count(ctx, "storage.used", "SELECT COALESCE(SUM(size), 0) FROM sys_file WHERE tenant_id = ?", tid)
	week := s.count(ctx, "storage.week", "SELECT COALESCE(SUM(size), 0) FROM sys_file WHERE tenant_id = ? AND created_at >= ?", tid, today.AddDate(0, 0, -7).UTC())
	const gb = 1024 * 1024 * 1024
	d, ok := pctOf(used, used-week)
	st := Storage{UsedGB: round1(float64(used) / gb), TotalGB: round1(float64(diskTotal(s.dataDir)) / gb), Delta: d, Comparable: ok}
	if st.TotalGB > 0 {
		st.Percent = round1(float64(used) / gb / st.TotalGB * 100)
	}
	return st
}

// count 执行 COUNT/SUM 查询，失败（表不存在等）返回 0 并记一条 debug 日志。
func (s *service) count(ctx context.Context, what, sql string, args ...any) int64 {
	var n int64
	s.try(what, func() error { return s.db.WithContext(ctx).Raw(sql, args...).Scan(&n).Error })
	return n
}

func (s *service) try(what string, f func() error) {
	if err := f(); err != nil {
		s.log.Debug("dashboard: metric unavailable", zap.String("metric", what), zap.Error(err))
	}
}

func round1(v float64) float64 { return float64(int64(v*10+0.5)) / 10 }

// pct 返回 cur 相对 prev 的变化百分比；prev 为 0 时无法比较，返回 0。
// minDeltaBaseline：上期样本少于此数时，百分比涨跌只是噪声（1 → 0 就是 -100%）。
const minDeltaBaseline = 5

func pctOf(cur, prev int64) (float64, bool) {
	if prev < minDeltaBaseline {
		return 0, false
	}
	return round1(float64(cur-prev) / float64(prev) * 100), true
}

func statValue(value int64, delta float64, comparable bool) StatValue {
	return StatValue{Value: value, Delta: delta, Comparable: comparable}
}
