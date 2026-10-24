package servermonitor

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"nexadmin/core"
	"nexadmin/core/cache"
	"nexadmin/core/metrics"
	"nexadmin/core/plugin"
	"nexadmin/core/storage"
)

// 健康检查状态；严重程度 ok < warn < fail。
const (
	StatusOK   = "ok"
	StatusWarn = "warn"
	StatusFail = "fail"
)

const (
	checkTimeout     = 3 * time.Second
	slowDB           = 500 * time.Millisecond
	slowCache        = 200 * time.Millisecond
	slowStorage      = 1500 * time.Millisecond
	diskWarnPercent  = 85.0
	diskFailPercent  = 95.0
	storageProbeKey  = "healthcheck/probe"
	cacheProbeTTL    = 10 * time.Second
	cacheProbeKeyTag = "health"
)

// Check 是一项依赖检查的结果，与前端 HealthCheck 一致。
type Check struct {
	Status    string `json:"status"` // ok | warn | fail
	LatencyMs int64  `json:"latencyMs"`
	Message   string `json:"message"`
}

// Health 是 GET /monitor/health 的返回：整体状态取各项里最差的。
type Health struct {
	Status string           `json:"status"`
	TS     int64            `json:"ts"`
	Checks map[string]Check `json:"checks"`
}

// CheckNames 是固定的检查项，前端按这个顺序展示。
var CheckNames = []string{"db", "cache", "storage", "migrations", "plugins", "disk"}

type checkFn func(ctx context.Context) Check

func severity(s string) int {
	switch s {
	case StatusOK:
		return 0
	case StatusWarn:
		return 1
	}
	return 2
}

// aggregate 取最严重的状态；没有任何检查项时视为 fail（说明检查本身没跑起来）。
func aggregate(checks map[string]Check) string {
	if len(checks) == 0 {
		return StatusFail
	}
	worst := StatusOK
	for _, c := range checks {
		if severity(c.Status) > severity(worst) {
			worst = c.Status
			if severity(worst) == 2 {
				worst = StatusFail // 未知状态按 fail 归一
			}
		}
	}
	return worst
}

// runChecks 并发执行全部检查：每项独立超时、隔离 panic，延迟由这里统一量取。
func runChecks(ctx context.Context, fns map[string]checkFn, timeout time.Duration) Health {
	out := make(map[string]Check, len(fns))
	var mu sync.Mutex
	var wg sync.WaitGroup
	for name, fn := range fns {
		wg.Add(1)
		go func() {
			defer wg.Done()
			c := runOne(ctx, fn, timeout)
			mu.Lock()
			out[name] = c
			mu.Unlock()
		}()
	}
	wg.Wait()
	return Health{Status: aggregate(out), TS: time.Now().UnixMilli(), Checks: out}
}

func runOne(ctx context.Context, fn checkFn, timeout time.Duration) Check {
	cctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	start := time.Now()
	done := make(chan Check, 1)
	go func() {
		defer func() {
			if r := recover(); r != nil {
				done <- Check{Status: StatusFail, Message: fmt.Sprintf("panic: %v", r)}
			}
		}()
		done <- fn(cctx)
	}()
	var c Check
	select {
	case c = <-done:
	case <-cctx.Done():
		c = Check{Status: StatusFail, Message: "timeout after " + timeout.String()}
	}
	c.LatencyMs = time.Since(start).Milliseconds()
	if c.Status == "" {
		c.Status = StatusFail
	}
	return c
}

// probeCheck 是“调一次探测函数”的通用检查：出错 fail；慢于 slow 则 warn。
func probeCheck(probe func(ctx context.Context) error, slow time.Duration) checkFn {
	return func(ctx context.Context) Check {
		start := time.Now()
		if err := probe(ctx); err != nil {
			return Check{Status: StatusFail, Message: err.Error()}
		}
		if d := time.Since(start); d > slow {
			return Check{Status: StatusWarn, Message: "slow response: " + d.Round(time.Millisecond).String()}
		}
		return Check{Status: StatusOK, Message: "ok"}
	}
}

// migrationsCheck：检查本身出错 fail（拿不到状态）；有待执行迁移 warn。
func migrationsCheck(pending int, err error) Check {
	switch {
	case errors.Is(err, metrics.ErrNoSource):
		return Check{Status: StatusWarn, Message: "migration status unavailable"}
	case err != nil:
		return Check{Status: StatusFail, Message: err.Error()}
	case pending > 0:
		return Check{Status: StatusWarn, Message: fmt.Sprintf("%d pending migration(s)", pending)}
	}
	return Check{Status: StatusOK, Message: "up to date"}
}

// pluginsCheck：已过期 / 无效 / 已吊销 / 即将到期 warn；未授权的 pro 插件不算异常（本来就没启用）。
func pluginsCheck(list []plugin.Info) Check {
	if len(list) == 0 {
		return Check{Status: StatusOK, Message: "no plugins installed"}
	}
	var bad, soon []string
	active := 0
	for _, p := range list {
		switch {
		case p.Status == plugin.StatusExpired || p.Status == plugin.StatusInvalid || p.Status == plugin.StatusRevoked:
			bad = append(bad, p.ID+":"+p.Status)
		case p.Status == plugin.StatusActive:
			active++
			if p.ExpiresSoon {
				soon = append(soon, p.ID)
			}
		}
	}
	sort.Strings(bad)
	sort.Strings(soon)
	switch {
	case len(bad) > 0:
		return Check{Status: StatusWarn, Message: "license problem: " + strings.Join(bad, ", ")}
	case len(soon) > 0:
		return Check{Status: StatusWarn, Message: "license expires soon: " + strings.Join(soon, ", ")}
	}
	return Check{Status: StatusOK, Message: fmt.Sprintf("%d active of %d", active, len(list))}
}

// diskCheck：取使用率最高的分区；>= 85% warn，>= 95% fail。
func diskCheck(disks []Disk) Check {
	var worst *Disk
	var worstPct float64
	for i := range disks {
		d := &disks[i]
		if d.TotalGB <= 0 {
			continue
		}
		if pct := d.UsedGB / d.TotalGB * 100; worst == nil || pct > worstPct {
			worst, worstPct = d, pct
		}
	}
	if worst == nil {
		return Check{Status: StatusWarn, Message: "disk information unavailable"}
	}
	msg := fmt.Sprintf("%s %.0f%% used", worst.Mount, worstPct)
	switch {
	case worstPct >= diskFailPercent:
		return Check{Status: StatusFail, Message: msg}
	case worstPct >= diskWarnPercent:
		return Check{Status: StatusWarn, Message: msg}
	}
	return Check{Status: StatusOK, Message: msg}
}

// healthRunner 持有各依赖，按需（首次调用时）创建存储客户端。
type healthRunner struct {
	d *core.Deps

	stOnce sync.Once
	st     storage.Storage
	stErr  error
}

func (h *healthRunner) storage() (storage.Storage, error) {
	h.stOnce.Do(func() {
		h.st, h.stErr = storage.New(h.d.Config.Storage, filepath.Join(h.d.Config.DataDir, "uploads", "files"))
	})
	return h.st, h.stErr
}

func (h *healthRunner) checks() map[string]checkFn {
	return map[string]checkFn{
		"db": probeCheck(func(ctx context.Context) error {
			sqlDB, err := h.d.DB.DB()
			if err != nil {
				return err
			}
			return sqlDB.PingContext(ctx)
		}, slowDB),
		"cache": probeCheck(func(ctx context.Context) error {
			key := cache.GlobalKey(cacheProbeKeyTag, "probe")
			if err := h.d.Cache.Set(ctx, key, "1", cacheProbeTTL); err != nil {
				return err
			}
			_, err := h.d.Cache.Get(ctx, key)
			return err
		}, slowCache),
		"storage": probeCheck(func(ctx context.Context) error {
			st, err := h.storage()
			if err != nil {
				return err
			}
			_, err = st.Exists(ctx, storageProbeKey)
			return err
		}, slowStorage),
		"migrations": func(context.Context) Check {
			n, err := metrics.PendingMigrations()
			return migrationsCheck(n, err)
		},
		"plugins": func(context.Context) Check { return pluginsCheck(plugin.Default().List()) },
		"disk":    func(context.Context) Check { return diskCheck(readDisks()) },
	}
}

func (h *healthRunner) run(ctx context.Context) Health {
	return runChecks(ctx, h.checks(), checkTimeout)
}
