package servermonitor

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"nexadmin/core/metrics"
	"nexadmin/core/plugin"
)

func TestAggregateTakesWorst(t *testing.T) {
	ok, warn, fail := Check{Status: StatusOK}, Check{Status: StatusWarn}, Check{Status: StatusFail}
	cases := []struct {
		name string
		in   map[string]Check
		want string
	}{
		{"all ok", map[string]Check{"a": ok, "b": ok}, StatusOK},
		{"warn wins over ok", map[string]Check{"a": ok, "b": warn}, StatusWarn},
		{"fail wins over warn", map[string]Check{"a": warn, "b": fail, "c": ok}, StatusFail},
		{"empty is fail", map[string]Check{}, StatusFail},
		{"unknown status counts as fail", map[string]Check{"a": ok, "b": {Status: "weird"}}, StatusFail},
	}
	for _, c := range cases {
		if got := aggregate(c.in); got != c.want {
			t.Errorf("%s: aggregate = %s, want %s", c.name, got, c.want)
		}
	}
}

func TestRunChecksAggregatesAndIsolates(t *testing.T) {
	fns := map[string]checkFn{
		"fast":   func(context.Context) Check { return Check{Status: StatusOK, Message: "ok"} },
		"warny":  func(context.Context) Check { return Check{Status: StatusWarn, Message: "meh"} },
		"panics": func(context.Context) Check { panic("boom") },
		"hangs": func(ctx context.Context) Check {
			time.Sleep(2 * time.Second) // 不理会 ctx：由 runOne 的超时兜底
			return Check{Status: StatusOK}
		},
		"slow": func(context.Context) Check {
			time.Sleep(30 * time.Millisecond)
			return Check{Status: StatusOK}
		},
	}
	start := time.Now()
	h := runChecks(context.Background(), fns, 200*time.Millisecond)
	if time.Since(start) > time.Second {
		t.Fatalf("runChecks took %s, hanging check was not cut off", time.Since(start))
	}
	if h.Status != StatusFail {
		t.Errorf("overall = %s, want fail", h.Status)
	}
	if got := h.Checks["panics"]; got.Status != StatusFail || !strings.Contains(got.Message, "panic") {
		t.Errorf("panics = %+v", got)
	}
	if got := h.Checks["hangs"]; got.Status != StatusFail || !strings.Contains(got.Message, "timeout") {
		t.Errorf("hangs = %+v", got)
	}
	if h.Checks["fast"].Status != StatusOK || h.Checks["warny"].Status != StatusWarn {
		t.Errorf("fast/warny = %+v / %+v", h.Checks["fast"], h.Checks["warny"])
	}
	if h.Checks["slow"].LatencyMs < 25 {
		t.Errorf("latency not measured: %+v", h.Checks["slow"])
	}
	if h.TS == 0 || len(h.Checks) != len(fns) {
		t.Errorf("ts=%d checks=%d", h.TS, len(h.Checks))
	}
}

func TestProbeCheck(t *testing.T) {
	ctx := context.Background()
	if c := probeCheck(func(context.Context) error { return nil }, time.Second)(ctx); c.Status != StatusOK {
		t.Errorf("ok probe = %+v", c)
	}
	if c := probeCheck(func(context.Context) error { return errors.New("conn refused") }, time.Second)(ctx); c.Status != StatusFail || c.Message != "conn refused" {
		t.Errorf("failing probe = %+v", c)
	}
	slow := probeCheck(func(context.Context) error { time.Sleep(20 * time.Millisecond); return nil }, 5*time.Millisecond)
	if c := slow(ctx); c.Status != StatusWarn {
		t.Errorf("slow probe = %+v", c)
	}
}

func TestMigrationsCheck(t *testing.T) {
	if c := migrationsCheck(0, nil); c.Status != StatusOK {
		t.Errorf("none pending = %+v", c)
	}
	if c := migrationsCheck(2, nil); c.Status != StatusWarn || !strings.Contains(c.Message, "2") {
		t.Errorf("2 pending = %+v", c)
	}
	if c := migrationsCheck(0, errors.New("checksum mismatch")); c.Status != StatusFail {
		t.Errorf("error = %+v", c)
	}
	if c := migrationsCheck(0, metrics.ErrNoSource); c.Status != StatusWarn {
		t.Errorf("no source = %+v", c)
	}
}

func TestPluginsCheck(t *testing.T) {
	if c := pluginsCheck(nil); c.Status != StatusOK {
		t.Errorf("no plugins = %+v", c)
	}
	active := plugin.Info{ID: "crm", Status: plugin.StatusActive}
	unlicensed := plugin.Info{ID: "bi", Status: plugin.StatusUnlicensed}
	if c := pluginsCheck([]plugin.Info{active, unlicensed}); c.Status != StatusOK {
		t.Errorf("unlicensed should not warn: %+v", c)
	}
	if c := pluginsCheck([]plugin.Info{active, {ID: "old", Status: plugin.StatusExpired}}); c.Status != StatusWarn || !strings.Contains(c.Message, "old:expired") {
		t.Errorf("expired = %+v", c)
	}
	soon := active
	soon.ExpiresSoon = true
	if c := pluginsCheck([]plugin.Info{soon}); c.Status != StatusWarn || !strings.Contains(c.Message, "crm") {
		t.Errorf("expires soon = %+v", c)
	}
}

func TestDiskCheck(t *testing.T) {
	cases := []struct {
		name  string
		disks []Disk
		want  string
	}{
		{"none", nil, StatusWarn},
		{"healthy", []Disk{{Mount: "/", TotalGB: 100, UsedGB: 40}}, StatusOK},
		{"warn at 85", []Disk{{Mount: "/", TotalGB: 100, UsedGB: 85}}, StatusWarn},
		{"fail at 95", []Disk{{Mount: "/", TotalGB: 100, UsedGB: 40}, {Mount: "/data", TotalGB: 100, UsedGB: 96}}, StatusFail},
		{"zero total ignored", []Disk{{Mount: "/x", TotalGB: 0, UsedGB: 0}, {Mount: "/", TotalGB: 10, UsedGB: 1}}, StatusOK},
	}
	for _, c := range cases {
		if got := diskCheck(c.disks); got.Status != c.want {
			t.Errorf("%s: %+v, want %s", c.name, got, c.want)
		}
	}
	if c := diskCheck([]Disk{{Mount: "/data", TotalGB: 100, UsedGB: 96}}); !strings.Contains(c.Message, "/data") {
		t.Errorf("message should name the worst mount: %q", c.Message)
	}
}
