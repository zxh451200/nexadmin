package website

// 官网访问统计（第一方轻量）：公开页浏览行级采集 + 坐席端聚合查询。
//
// 采集口径：
//   - 只记官网公开路由的 GET（pub 组内的页面；/website-assets 静态资源、robots/sitemap/rss、
//     HEAD、非 GET 不记）。NoRoute 分发的 404 不经过 pub 组，不在统计内。
//   - bot（UA 含 bot/crawl/spider/slurp）不采集，统计口径是真实访客。
//   - 身份只有一枚匿名会话 cookie（nex-vid 随机 token，HttpOnly）：不含 IP、不关联账号，
//     泄露也无法回溯个人——隐私披露只需一句「匿名统计」。
//   - 写入异步批量：浏览是高频写，同步 INSERT 会拖慢页面响应；channel 缓冲 + 定时批量落库，
//     队列满时丢弃（统计允许有损）。

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
	"gorm.io/gorm"

	"nexadmin/core/response"
	"nexadmin/pkg/idgen"
)

const (
	vidCookie   = "nex-vid"
	vidMaxAge   = 365 * 24 * time.Hour
	flushEvery  = 2 * time.Second
	flushAtSize = 64
	queueCap    = 512
	// 时效范围上限：统计窗口最大 30 天（与趋势窗口同一档）。
	analyticsMaxDays = 30
)

var botUARe = regexp.MustCompile(`(?i)bot|crawl|spider|slurp`)

// refKind 是流量来源的分类口径：direct/search/social/external/internal（站内跳转）。
const (
	refDirect   = "direct"
	refSearch   = "search"
	refSocial   = "social"
	refExternal = "external"
	refInternal = "internal"
)

var searchHosts = map[string]bool{
	"google.com": true, "bing.com": true, "baidu.com": true, "duckduckgo.com": true,
	"yandex.com": true, "sogou.com": true, "so.com": true, "search.brave.com": true,
}

var socialHosts = map[string]bool{
	"x.com": true, "twitter.com": true, "linkedin.com": true, "facebook.com": true,
	"instagram.com": true, "youtube.com": true, "t.co": true, "lnkd.in": true,
}

// PageView 是浏览行的落库形态：day 按站点时区归日，聚合查询按它 GROUP BY。
type PageView struct {
	ID         int64     `gorm:"column:id"`
	Day        string    `gorm:"column:day"`
	Path       string    `gorm:"column:path"`
	RefHost    string    `gorm:"column:ref_host"`
	RefKind    string    `gorm:"column:ref_kind"`
	SessionKey string    `gorm:"column:session_key"`
	Browser    string    `gorm:"column:browser"`
	OS         string    `gorm:"column:os"`
	CreatedAt  time.Time `gorm:"column:created_at"`
	UpdatedAt  time.Time `gorm:"column:updated_at"`
}

func (PageView) TableName() string { return "plg_website_page_view" }

// viewRow 是采集中间件产出的一条待落库记录；flush 时折成 PageView 批量插入。
type viewRow struct {
	id         int64
	day        string
	path       string
	refHost    string
	refKind    string
	sessionKey string
	browser    string
	os         string
	createdAt  time.Time
}

// viewRecorder 持有异步写入队列；Register 时装配，Start 时起 flusher。
type viewRecorder struct {
	db    *gorm.DB
	loc   *time.Location
	queue chan viewRow
	log   *zap.Logger
	// secureCookies 是匿名 cookie nex-vid 的 Secure 标记，来自 server.secure_cookies：
	// 反代终结 TLS 时请求侧 TLS 为空，按 c.Request.TLS 判断会漏标，改由配置显式声明。
	secureCookies bool
}

func newViewRecorder(db *gorm.DB, loc *time.Location, secureCookies bool, log *zap.Logger) *viewRecorder {
	return &viewRecorder{db: db, loc: loc, queue: make(chan viewRow, queueCap), log: log, secureCookies: secureCookies}
}

// viewMiddleware 挂在官网公开路由组上：命中页面 GET 时发一枚匿名会话 cookie 并排队一条浏览。
func (r *viewRecorder) viewMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		if c.Request.Method != http.MethodGet {
			c.Next()
			return
		}
		// 静态资产与机器文件不占浏览口径；订单状态轮询是 AJAX 数据端点（3s 一次），
		// 记进来会把 PV 淹掉——订单页本身照记。
		path := c.Request.URL.Path
		if strings.HasPrefix(path, "/website-assets") ||
			path == "/robots.txt" || path == "/sitemap.xml" || path == "/rss.xml" || path == "/atom.xml" ||
			(strings.HasPrefix(path, "/website/orders/") && strings.HasSuffix(path, "/status")) {
			c.Next()
			return
		}
		ua := c.GetHeader("User-Agent")
		if botUARe.MatchString(ua) {
			c.Next()
			return
		}
		vid, err := c.Cookie(vidCookie)
		if err != nil || vid == "" {
			b := make([]byte, 16)
			if _, e := rand.Read(b); e != nil {
				c.Next()
				return
			}
			vid = hex.EncodeToString(b)
			http.SetCookie(c.Writer, &http.Cookie{
				Name:     vidCookie,
				Value:    vid,
				Path:     "/",
				MaxAge:   int(vidMaxAge.Seconds()),
				HttpOnly: true,
				SameSite: http.SameSiteLaxMode,
				Secure:   r.secureCookies,
			})
		}
		now := time.Now().In(r.loc)
		id, e := idgen.Next()
		if e != nil {
			c.Next()
			return
		}
		refHost, refKind := classifyRef(c.Request.Header.Get("Referer"), c.Request.Host)
		browser, os := parseUA(ua)
		row := viewRow{
			id:         int64(id),
			day:        now.Format("2006-01-02"),
			path:       trimLen(path, 200),
			refHost:    trimLen(refHost, 120),
			refKind:    refKind,
			sessionKey: trimLen(vid, 64),
			browser:    browser,
			os:         os,
			createdAt:  time.Now().UTC(),
		}
		r.enqueue(row)
		c.Next()
	}
}

// classifyRef 把 Referer 归类成来源口径：无=直接访问，本站=站内跳转，
// 搜索/社交按域名后缀匹配（google.co.jp 这类国别域也算），其余外域一律 external。
func classifyRef(referer, requestHost string) (host, kind string) {
	raw := strings.TrimSpace(referer)
	if raw == "" {
		return "", refDirect
	}
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return "", refDirect
	}
	h := strings.ToLower(strings.TrimSuffix(u.Hostname(), "."))
	// Host 头在非默认端口会带端口（dev :8081、内网 :8080），Referer 的 Hostname() 不带；
	// 同站判定按不带端口的主机名对齐，端口不同只影响大小写无关的相等比较。
	reqHost := strings.ToLower(strings.TrimSuffix(requestHost, "."))
	if _, port, err := net.SplitHostPort(reqHost); err == nil && port != "" {
		reqHost = strings.TrimSuffix(reqHost, ":"+port)
	}
	if strings.EqualFold(h, reqHost) {
		return h, refInternal
	}
	for domain := range searchHosts {
		if h == domain || strings.HasSuffix(h, "."+domain) {
			return h, refSearch
		}
	}
	for domain := range socialHosts {
		if h == domain || strings.HasSuffix(h, "."+domain) {
			return h, refSocial
		}
	}
	return h, refExternal
}

// parseUA 从 UA 里解出浏览器与操作系统（坐席端设备画像用）。只求大类准确，不引
// 第三方库：竞品仪表盘（Plausible/Umami）的设备卡也是五六个大类，细分版本对
// 官网运营没有决策价值。解析不了的归 other（聚合时照样占一格，口径诚实）。
func parseUA(ua string) (browser, os string) {
	ua = strings.TrimSpace(ua)
	if ua == "" {
		return "other", "other"
	}
	// 浏览器按特化程度判，顺序敏感：Edge/Opera 的 UA 里也含 Chrome/，要先判它们。
	switch {
	case strings.Contains(ua, "Edg/"), strings.Contains(ua, "Edge/"):
		browser = "Edge"
	case strings.Contains(ua, "OPR/"), strings.Contains(ua, "Opera"):
		browser = "Opera"
	case strings.Contains(ua, "Firefox/"), strings.Contains(ua, "FxiOS/"):
		browser = "Firefox"
	case strings.Contains(ua, "CriOS/"):
		browser = "Chrome"
	case strings.Contains(ua, "Chrome/"):
		browser = "Chrome"
	case strings.Contains(ua, "Safari/"):
		browser = "Safari"
	default:
		browser = "other"
	}
	// 系统按 UA 出现的平台段判；iPad 的桌面化 UA（Macintosh 且无 Touch）识别不了，归 macOS 可接受。
	switch {
	case strings.Contains(ua, "Windows NT"), strings.Contains(ua, "Win32"):
		os = "Windows"
	case strings.Contains(ua, "iPhone"), strings.Contains(ua, "iPod"):
		os = "iOS"
	case strings.Contains(ua, "iPad"), strings.Contains(ua, "Touch"):
		os = "iPadOS"
	case strings.Contains(ua, "Android"):
		os = "Android"
	case strings.Contains(ua, "Mac OS X"), strings.Contains(ua, "Macintosh"):
		os = "macOS"
	case strings.Contains(ua, "CrOS"):
		os = "ChromeOS"
	case strings.Contains(ua, "Linux"):
		os = "Linux"
	default:
		os = "other"
	}
	return browser, os
}

func (r *viewRecorder) enqueue(row viewRow) {
	select {
	case r.queue <- row:
	default:
		if r.log != nil {
			r.log.Debug("website: view queue full, sample dropped", zap.String("path", row.path))
		}
	}
}

// runFlusher 批量落库：攒够 flushAtSize 条或每 flushEvery 秒刷一次；退出时冲掉余量。
func (r *viewRecorder) runFlusher(ctx context.Context) {
	buf := make([]PageView, 0, flushAtSize)
	flush := func() {
		if len(buf) == 0 {
			return
		}
		// 统计写入允许有损：落库失败只告警，不重试不阻塞页面
		if err := r.db.WithContext(context.Background()).Create(&buf).Error; err != nil && r.log != nil {
			r.log.Warn("website: view batch insert failed", zap.Int("rows", len(buf)), zap.Error(err))
		}
		buf = buf[:0]
	}
	t := time.NewTicker(flushEvery)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			flush()
			return
		case row := <-r.queue:
			now := time.Now().UTC()
			buf = append(buf, PageView{
				ID: row.id, Day: row.day, Path: row.path, RefHost: row.refHost,
				RefKind: row.refKind, SessionKey: row.sessionKey,
				Browser: row.browser, OS: row.os,
				CreatedAt: row.createdAt, UpdatedAt: now,
			})
			if len(buf) >= flushAtSize {
				flush()
			}
		case <-t.C:
			flush()
		}
	}
}

// ---- 聚合查询（坐席端） ----

// AnalyticsPoint 是趋势卡的一天：浏览 / 去重访客 / 询盘（浏览→询盘漏斗的两级）。
type AnalyticsPoint struct {
	Day       string `json:"day"`
	Views     int64  `json:"views"`
	Sessions  int64  `json:"sessions"`
	Inquiries int64  `json:"inquiries"`
}

// AnalyticsPath 是热门入口页排行的一行。
type AnalyticsPath struct {
	Path  string `json:"path"`
	Views int64  `json:"views"`
}

// AnalyticsRef 是流量来源的一行：按来源域聚合的去重会话数。
// 字段名对齐列名（ref_kind/ref_host → RefKind/RefHost）：GORM Raw Scan 按列名映射，
// JSON 输出仍是 kind/host。
type AnalyticsRef struct {
	RefKind  string `json:"kind"`
	RefHost  string `json:"host"`
	Sessions int64  `json:"sessions"`
}

// AnalyticsDevice 是设备画像的一行（browser 或 os 各一组）。
type AnalyticsDevice struct {
	Key      string `json:"key"`
	Sessions int64  `json:"sessions"`
}

// AnalyticsPath 也可承载入口页（path = 会话首个浏览页，views = 会话数）。

// AnalyticsOut 是 GET /plugins/website/analytics 的应答。
type AnalyticsOut struct {
	Days      int   `json:"days"`
	Views     int64 `json:"views"`
	Sessions  int64 `json:"sessions"`
	Inquiries int64 `json:"inquiries"`
	Realtime  int64 `json:"realtime"`
	// BounceRate 是跳出率（0-100，一位小数由前端格式化）：只有一次浏览的会话占比，
	// 与 Plausible 的 bounce 口径一致（会话没有心跳事件，多浏览≈未跳出）。
	BounceRate float64 `json:"bounceRate"`
	// AvgDuration 是平均访问时长（秒）：会话首末浏览的间隔均值；单浏览会话计 0。
	AvgDuration  int64             `json:"avgDuration"`
	Trend        []AnalyticsPoint  `json:"trend"`
	TopPaths     []AnalyticsPath   `json:"topPaths"`
	EntryPages   []AnalyticsPath   `json:"entryPages"`
	Referrers    []AnalyticsRef    `json:"referrers"`
	RefKindStats []AnalyticsDevice `json:"refKindStats"`
	Browsers     []AnalyticsDevice `json:"browsers"`
	OSes         []AnalyticsDevice `json:"oses"`
}

// analyticsData 聚合出统计窗口内的全部指标；tz 是站点时区（day 的归日口径）。
func (h *handler) analyticsData(c *gin.Context, tz *time.Location) (*AnalyticsOut, error) {
	days := analyticsMaxDays
	switch c.Query("days") {
	case "7":
		days = 7
	case "14":
		days = 14
	case "30":
		days = 30
	}
	now := time.Now().In(tz)
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, tz)
	windowStart := today.AddDate(0, 0, -(days - 1))

	out := &AnalyticsOut{Days: days, Trend: []AnalyticsPoint{}, TopPaths: []AnalyticsPath{}, Referrers: []AnalyticsRef{}}
	ctx := c.Request.Context()

	// 浏览与去重访客：按 day 聚合（day 在写入时已按站点时区归日）
	var pvRows []struct {
		Day      string
		Views    int64
		Sessions int64
	}
	if err := h.svc.db.WithContext(ctx).
		Raw("SELECT day, COUNT(*) AS views, COUNT(DISTINCT session_key) AS sessions FROM plg_website_page_view WHERE day >= ? GROUP BY day ORDER BY day",
			windowStart.Format("2006-01-02")).Scan(&pvRows).Error; err != nil {
		return nil, err
	}
	byDay := make(map[string]struct {
		Views    int64
		Sessions int64
	}, len(pvRows))
	var totalViews, totalSessions int64
	for _, r := range pvRows {
		byDay[r.Day] = struct {
			Views    int64
			Sessions int64
		}{r.Views, r.Sessions}
		totalViews += r.Views
		totalSessions += r.Sessions
	}
	trend := make([]AnalyticsPoint, 0, days)
	for i := days - 1; i >= 0; i-- {
		key := today.AddDate(0, 0, -i).Format("2006-01-02")
		agg := byDay[key]
		trend = append(trend, AnalyticsPoint{Day: key[5:], Views: agg.Views, Sessions: agg.Sessions})
	}

	// 询盘（浏览→询盘漏斗的第二级）：逐日按站点时区窗口 COUNT，与浏览同口径
	var inquiries int64
	for i := days - 1; i >= 0; i-- {
		from := today.AddDate(0, 0, -i)
		to := from.AddDate(0, 0, 1)
		var n int64
		if err := h.svc.db.WithContext(ctx).
			Raw("SELECT COUNT(*) FROM plg_website_message WHERE created_at >= ? AND created_at < ?",
				from.UTC(), to.UTC()).Scan(&n).Error; err != nil {
			return nil, err
		}
		inquiries += n
		trend[len(trend)-1-(days-1-i)].Inquiries = n
	}

	// 热门入口页
	var paths []AnalyticsPath
	if err := h.svc.db.WithContext(ctx).
		Raw("SELECT path, COUNT(*) AS views FROM plg_website_page_view WHERE day >= ? GROUP BY path ORDER BY views DESC LIMIT 10",
			windowStart.Format("2006-01-02")).Scan(&paths).Error; err != nil {
		return nil, err
	}

	// 流量来源：按来源域聚合的去重会话数
	var refs []AnalyticsRef
	if err := h.svc.db.WithContext(ctx).
		Raw("SELECT ref_kind, ref_host, COUNT(DISTINCT session_key) AS sessions FROM plg_website_page_view WHERE day >= ? GROUP BY ref_kind, ref_host ORDER BY sessions DESC LIMIT 9",
			windowStart.Format("2006-01-02")).Scan(&refs).Error; err != nil {
		return nil, err
	}

	// 实时访客：近 5 分钟去重会话（近似 Studio 的 Live 块）
	var realtime int64
	if err := h.svc.db.WithContext(ctx).
		Raw("SELECT COUNT(DISTINCT session_key) FROM plg_website_page_view WHERE created_at >= ?", time.Now().UTC().Add(-5*time.Minute)).Scan(&realtime).Error; err != nil {
		return nil, err
	}

	out.Views = totalViews
	out.Sessions = totalSessions
	out.Inquiries = inquiries
	out.Realtime = realtime
	out.Trend = trend
	out.TopPaths = paths
	out.Referrers = refs

	// 会话级指标（跳出率 / 平均时长）：窗口内按会话聚合首末浏览时间与浏览数，
	// 在 Go 侧算均值——MIN/MAX 跨列运算各引擎方言不同（TIMESTAMPDIFF vs EXTRACT），
	// 会话行数是千级，拉回内存聚合比写方言 SQL 稳。跳出口径对齐 Plausible：只有
	// 一次浏览的会话记为跳出（无心跳事件，多浏览≈未跳出）。
	var sessRows []struct {
		SessionKey string
		Views      int64
		First      time.Time
		Last       time.Time
	}
	if err := h.svc.db.WithContext(ctx).
		Raw("SELECT session_key, COUNT(*) AS views, MIN(created_at) AS first, MAX(created_at) AS last FROM plg_website_page_view WHERE day >= ? GROUP BY session_key",
			windowStart.Format("2006-01-02")).Scan(&sessRows).Error; err != nil {
		return nil, err
	}
	if len(sessRows) > 0 {
		var bounces, totalSec int64
		for _, s := range sessRows {
			if s.Views <= 1 {
				bounces++
				continue
			}
			totalSec += int64(s.Last.Sub(s.First).Seconds())
		}
		out.BounceRate = float64(bounces) / float64(len(sessRows)) * 100
		// 时长分母是「多浏览会话」：单浏览会话没有时长概念，混进去会拉低均值
		var multi int64 = int64(len(sessRows)) - bounces
		if multi > 0 {
			out.AvgDuration = totalSec / multi
		}
	}

	// 设备画像：浏览器 / 操作系统各取 top5（按去重会话），与来源榜同口径。
	// 列名直接映射中间结构（不 AS key：KEY 在 MySQL 是保留字别名要转义）。
	var browserRows []struct {
		Browser  string
		Sessions int64
	}
	if err := h.svc.db.WithContext(ctx).
		Raw("SELECT k, COUNT(DISTINCT session_key) AS sessions FROM (SELECT COALESCE(NULLIF(browser, ''), 'other') AS k, session_key FROM plg_website_page_view WHERE day >= ?) t GROUP BY k ORDER BY sessions DESC LIMIT 5",
			windowStart.Format("2006-01-02")).Scan(&browserRows).Error; err != nil {
		return nil, err
	}
	out.Browsers = make([]AnalyticsDevice, 0, len(browserRows))
	for _, r := range browserRows {
		out.Browsers = append(out.Browsers, AnalyticsDevice{Key: r.Browser, Sessions: r.Sessions})
	}
	var osRows []struct {
		OS       string
		Sessions int64
	}
	if err := h.svc.db.WithContext(ctx).
		Raw("SELECT k, COUNT(DISTINCT session_key) AS sessions FROM (SELECT COALESCE(NULLIF(os, ''), 'other') AS k, session_key FROM plg_website_page_view WHERE day >= ?) t GROUP BY k ORDER BY sessions DESC LIMIT 5",
			windowStart.Format("2006-01-02")).Scan(&osRows).Error; err != nil {
		return nil, err
	}
	out.OSes = make([]AnalyticsDevice, 0, len(osRows))
	for _, r := range osRows {
		out.OSes = append(out.OSes, AnalyticsDevice{Key: r.OS, Sessions: r.Sessions})
	}

	// 入口页：每个会话的首个浏览页（ROW_NUMBER 窗口函数，MySQL 8/MariaDB/PG 通用）
	var entries []AnalyticsPath
	if err := h.svc.db.WithContext(ctx).
		Raw("SELECT path, COUNT(*) AS views FROM (SELECT path, ROW_NUMBER() OVER (PARTITION BY session_key ORDER BY created_at, id) AS rn FROM plg_website_page_view WHERE day >= ?) t WHERE rn = 1 GROUP BY path ORDER BY views DESC LIMIT 8",
			windowStart.Format("2006-01-02")).Scan(&entries).Error; err != nil {
		return nil, err
	}
	out.EntryPages = entries

	// 来源大类汇总：五类份额（与 referrers 的 kind+host 明细互补，跨 host 会话不可加，单独查）
	var kindRows []struct {
		RefKind  string
		Sessions int64
	}
	if err := h.svc.db.WithContext(ctx).
		Raw("SELECT ref_kind, COUNT(DISTINCT session_key) AS sessions FROM plg_website_page_view WHERE day >= ? GROUP BY ref_kind ORDER BY sessions DESC",
			windowStart.Format("2006-01-02")).Scan(&kindRows).Error; err != nil {
		return nil, err
	}
	out.RefKindStats = make([]AnalyticsDevice, 0, len(kindRows))
	for _, r := range kindRows {
		out.RefKindStats = append(out.RefKindStats, AnalyticsDevice{Key: r.RefKind, Sessions: r.Sessions})
	}

	return out, nil
}

// analytics 挂在管理端路由：GET /plugins/website/analytics?days=7|14|30
func (h *handler) analytics(c *gin.Context) {
	out, err := h.analyticsData(c, h.tz)
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, out)
}

var _ = zap.Error // 保持 zap 引用（recorder 日志在同包其他文件）
