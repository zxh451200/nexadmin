package clienterror

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"go.uber.org/zap"
	"gorm.io/gorm"

	"nexadmin/core/tenant"
	"nexadmin/pkg/idgen"
)

// 上限。请求体上限 MaxBody 在 handler 里强制；字段上限在 Sanitize 里强制（客户端的限制不可信）。
const (
	MaxBody       = 8 * 1024
	MaxMessage    = 500
	MaxStackLines = 20
	MaxStack      = 4000
	MaxRoute      = 255
	MaxUA         = 255
	MaxVersion    = 32
	MaxUsername   = 64

	Retention      = 14 * 24 * time.Hour
	purgeEvery     = time.Hour
	tenantPerMin   = 600 // 每租户每分钟最多落库次数（IP 限流之外的总闸，防分布式刷）
	redacted       = "[REDACTED]"
	maxThrottleKey = 4096
)

// 错误。
var (
	ErrEmpty     = errors.New("clienterror: empty message")
	ErrThrottled = errors.New("clienterror: tenant throttled")
)

// Report 是前端上报的内容（只收这几项）。
type Report struct {
	Route   string `json:"route"`
	Message string `json:"message"`
	Stack   string `json:"stack"`
	UA      string `json:"ua"`
	Version string `json:"version"`
}

// Event 是清洗后、待落库的一次出现。
type Event struct {
	Report
	UserID      idgen.ID
	Username    string
	Fingerprint string
	At          time.Time
}

// ---- 敏感信息过滤 ----

var (
	// key=value / "key":"value" 形式的敏感项：只抹值，保留键名方便排查。
	secretKV = regexp.MustCompile(`(?i)((?:"|')?(?:proxy-authorization|authorization|access[_-]?token|refresh[_-]?token|id[_-]?token|token|password|passwd|pwd|secret|api[_-]?key|set-cookie|cookie|credential)s?(?:"|')?\s*[:=]\s*)(?:"[^"]*"|'[^']*'|(?:Bearer|Basic)\s+[^\s,;&"'}]+|[^\s,;&"'}]+)`)
	bearer   = regexp.MustCompile(`(?i)\b(?:Bearer|Basic)\s+[A-Za-z0-9._~+/=\-]{8,}`)
	jwt      = regexp.MustCompile(`\beyJ[A-Za-z0-9_\-]{5,}\.[A-Za-z0-9_\-]{5,}\.[A-Za-z0-9_\-]*`)
	userinfo = regexp.MustCompile(`(?i)(\b[a-z][a-z0-9+.\-]*://)[^/\s:@]+:[^/\s@]+@`)
)

// Scrub 抹掉字符串里的令牌、密码、Authorization 等敏感值。
func Scrub(s string) string {
	if s == "" {
		return s
	}
	s = secretKV.ReplaceAllString(s, "${1}"+redacted)
	s = bearer.ReplaceAllString(s, "Bearer "+redacted)
	s = jwt.ReplaceAllString(s, redacted)
	s = userinfo.ReplaceAllString(s, "${1}"+redacted+"@")
	return s
}

func truncRunes(s string, n int) string {
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	return string([]rune(s)[:n])
}

func firstLines(s string, n int) string {
	lines := strings.Split(strings.ReplaceAll(s, "\r\n", "\n"), "\n")
	if len(lines) > n {
		lines = lines[:n]
	}
	return strings.Join(lines, "\n")
}

// routeOnly 只保留路径：去掉 ? 和 # 之后的部分（查询串里常带令牌）。
func routeOnly(s string) string {
	if i := strings.IndexAny(s, "?#"); i >= 0 {
		s = s[:i]
	}
	return s
}

// Sanitize 清洗一条上报：脱敏 -> 去控制字符 -> 堆栈只留前 20 行 -> 逐字段截断。
func Sanitize(r Report) Report {
	clean := func(s string) string { return strings.TrimSpace(strings.Map(dropControl, s)) }
	stack := Scrub(firstLines(strings.TrimSpace(r.Stack), MaxStackLines))
	return Report{
		Route:   truncRunes(clean(Scrub(routeOnly(r.Route))), MaxRoute),
		Message: truncRunes(clean(Scrub(r.Message)), MaxMessage),
		Stack:   truncRunes(strings.Map(dropControlKeepNL, stack), MaxStack),
		UA:      truncRunes(clean(r.UA), MaxUA),
		Version: truncRunes(clean(r.Version), MaxVersion),
	}
}

func dropControl(r rune) rune {
	if r < 0x20 || r == 0x7f {
		return -1
	}
	return r
}

func dropControlKeepNL(r rune) rune {
	if r == '\n' || r == '\t' {
		return r
	}
	return dropControl(r)
}

// ---- 指纹 ----

var (
	digits    = regexp.MustCompile(`\d+`)
	chunkHash = regexp.MustCompile(`[-.][A-Za-z0-9_]{8,}\.(m?js|css)\b`)
	spaces    = regexp.MustCompile(`\s+`)
)

func normalize(s string) string {
	s = chunkHash.ReplaceAllString(s, ".$1")
	s = digits.ReplaceAllString(s, "#")
	return spaces.ReplaceAllString(strings.TrimSpace(s), " ")
}

// Fingerprint 由归一化的消息 + 堆栈前 3 行算出：数字（ID、行列号）和构建产物的哈希文件名不影响指纹，
// 路由与用户也不参与，所以同一个 bug 在不同页面 / 不同用户身上聚合成一行。
func Fingerprint(r Report) string {
	lines := strings.Split(r.Stack, "\n")
	if len(lines) > 3 {
		lines = lines[:3]
	}
	sum := sha256.Sum256([]byte(normalize(r.Message) + "\n" + normalize(strings.Join(lines, "\n"))))
	return hex.EncodeToString(sum[:16])
}

// ---- 服务端总闸 ----

// throttle 是按 key 的固定窗口计数器（进程内）。
type throttle struct {
	mu     sync.Mutex
	max    int
	window time.Duration
	now    func() time.Time
	m      map[string]*win
}

type win struct {
	start time.Time
	n     int
}

func newThrottle(max int, window time.Duration) *throttle {
	return &throttle{max: max, window: window, now: time.Now, m: map[string]*win{}}
}

// Allow 记一次并返回是否放行。
func (t *throttle) Allow(key string) bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	now := t.now()
	if len(t.m) > maxThrottleKey { // 过期窗口清理，防止 key 无限增长
		for k, w := range t.m {
			if now.Sub(w.start) >= t.window {
				delete(t.m, k)
			}
		}
	}
	w := t.m[key]
	if w == nil || now.Sub(w.start) >= t.window {
		w = &win{start: now}
		t.m[key] = w
	}
	if w.n >= t.max {
		return false
	}
	w.n++
	return true
}

// ---- Service ----

// Service 负责清洗、指纹、总闸与落库。
type Service struct {
	store    Store
	db       *gorm.DB // 仅查询用；单测可为 nil
	log      *zap.Logger
	throttle *throttle
	now      func() time.Time
}

// NewService 用数据库创建 Service。
func NewService(db *gorm.DB, log *zap.Logger) *Service {
	s := newService(dbStore{db: db}, log)
	s.db = db
	return s
}

func newService(store Store, log *zap.Logger) *Service {
	if log == nil {
		log = zap.NewNop()
	}
	return &Service{store: store, log: log, throttle: newThrottle(tenantPerMin, time.Minute), now: time.Now}
}

// Ingest 处理一次上报。ctx 里需要租户（Tenant 中间件已写入）。
func (s *Service) Ingest(ctx context.Context, userID idgen.ID, username string, r Report, headerUA string) error {
	if strings.TrimSpace(r.UA) == "" {
		r.UA = headerUA
	}
	r = Sanitize(r)
	if r.Message == "" {
		return ErrEmpty
	}
	key := "t:0"
	if id, ok := tenant.From(ctx); ok {
		key = "t:" + strconv.FormatInt(int64(id), 10)
	}
	if !s.throttle.Allow(key) {
		return ErrThrottled
	}
	return s.store.Upsert(ctx, &Event{
		Report: r, UserID: userID, Username: truncRunes(Scrub(username), MaxUsername),
		Fingerprint: Fingerprint(r), At: s.now().UTC(),
	})
}

// Query 是列表查询条件。
type Query struct {
	Keyword  string
	Page     int
	PageSize int
}

func likeArg(s string) string {
	return "%" + strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(s) + "%"
}

// List 分页列出当前租户的错误，最近出现的在前。
func (s *Service) List(ctx context.Context, q Query) ([]ClientError, int64, error) {
	tx := s.db.WithContext(ctx).Model(&ClientError{})
	if kw := strings.TrimSpace(q.Keyword); kw != "" {
		tx = tx.Where("(message LIKE ? OR route LIKE ? OR username LIKE ?)", likeArg(kw), likeArg(kw), likeArg(kw))
	}
	tx = tx.Session(&gorm.Session{})
	var total int64
	if err := tx.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var rows []ClientError
	err := tx.Order("last_seen_at DESC").Offset((q.Page - 1) * q.PageSize).Limit(q.PageSize).Find(&rows).Error
	return rows, total, err
}

// purgeLoop 立即清理一次，之后每小时一次；stop 关闭时退出。
func (s *Service) purgeLoop(stop <-chan struct{}) {
	run := func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
		defer cancel()
		if _, err := s.store.Purge(ctx, s.now().Add(-Retention)); err != nil {
			s.log.Warn("clienterror: purge failed", zap.Error(err))
		}
	}
	run()
	t := time.NewTicker(purgeEvery)
	defer t.Stop()
	for {
		select {
		case <-t.C:
			run()
		case <-stop:
			return
		}
	}
}
