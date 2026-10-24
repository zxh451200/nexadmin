package report

import (
	"context"
	"testing"
)

// 标识符注入：列名/表名/排序/聚合字段/分桶/操作符里塞引号、分号、注释、子查询，全部被拒绝，且不会有任何 SQL 发到库。
func TestIdentifierInjectionRejected(t *testing.T) {
	payloads := []string{
		"status`; DROP TABLE plg_cron_log; --", `status"; DROP TABLE x; --`, "status; DELETE FROM sys_user", "status -- c", "status/**/",
		"status/*x*/OR/**/1=1", "status OR 1=1", "status) UNION SELECT password FROM sys_user --", "(SELECT password FROM sys_user LIMIT 1)",
		"`status`", `"status"`, "[status]", "'status'", "STATUS", "Status", "plg_cron_log.status", "public.plg_cron_log.status",
		"tenant_id", "id", "password", "1", "1=1", "*", "status,password", "status\n", " status", "status ", "", "d0", "m0",
		"COUNT(*)", "status AS x", "status\x00", "𝒔tatus",
	}
	e := newEngine(nil2DB(t), nil)
	tg := logTarget(t)
	env := testEnv()
	run := func(what string, sp Spec) {
		if _, _, err := e.build(context.Background(), tg, sp, env); err == nil {
			t.Errorf("%s: accepted %+v", what, sp)
		}
	}
	for _, p := range payloads {
		run("dimension", Spec{Dimensions: []Dim{{Column: p}}})
		if p != "" { // 空列 = COUNT(*)，是合法写法
			run("measure column (count)", Spec{Measures: []Measure{{Agg: "count", Column: p}}})
		}
		run("measure column (sum)", Spec{Measures: []Measure{{Agg: "sum", Column: p}}})
		run("filter column", Spec{Dimensions: []Dim{{Column: "status"}}, Filters: []Filter{{Column: p, Op: "=", Value: "x"}}})
		if p != "" { // 空分桶 = 不分桶
			run("bucket", Spec{Dimensions: []Dim{{Column: "started_at", Bucket: p}}})
		}
		run("agg", Spec{Measures: []Measure{{Agg: p, Column: "cost_ms"}}})
		run("op", Spec{Dimensions: []Dim{{Column: "status"}}, Filters: []Filter{{Column: "status", Op: p, Value: "x"}}})
		if p != "d0" { // d0 是合法的排序引用
			run("order", Spec{Dimensions: []Dim{{Column: "status"}}, OrderBy: []Order{{Ref: p}}})
		}
	}
	reg := newRegistry(defaultRaw(), nil)
	for _, p := range payloads {
		if _, err := reg.Target(p, []string{"status"}); err == nil {
			t.Errorf("table %q accepted", p)
		}
		if _, err := reg.Target("plg_cron_log", []string{p}); err == nil {
			t.Errorf("dataset column %q accepted", p)
		}
	}
	for _, tb := range []string{"plg_cron_log; DROP TABLE x", "plg_cron_log`", "plg_cron_log --", "(select 1)", "plg_cron_log, sys_user", "plg_cron_log JOIN sys_user", "PLG_CRON_LOG"} {
		if _, err := reg.Target(tb, []string{"status"}); err == nil {
			t.Errorf("table %q accepted", tb)
		}
	}
}

func TestAggregateTypeRules(t *testing.T) {
	e := newEngine(nil2DB(t), nil)
	tg := logTarget(t)
	bad := []Spec{
		{Measures: []Measure{{Agg: "sum", Column: "status"}}},
		{Measures: []Measure{{Agg: "avg", Column: "started_at"}}},
		{Measures: []Measure{{Agg: "max", Column: "status"}}},
		{Measures: []Measure{{Agg: "sum"}}},
		{Measures: []Measure{{Agg: "SUM", Column: "cost_ms"}}},
		{Measures: []Measure{{Agg: "count distinct", Column: "status"}}},
		{Dimensions: []Dim{{Column: "status", Bucket: "day"}}},
		{Dimensions: []Dim{{Column: "started_at", Bucket: "hour' OR 1=1"}}},
		{Dimensions: []Dim{{Column: "started_at", Bucket: "DAY"}}},
		{},
		{Dimensions: []Dim{{Column: "status"}}, Limit: -1},
		{Dimensions: []Dim{{Column: "status"}}, OrderBy: []Order{{Ref: "d1"}}},
		{Dimensions: []Dim{{Column: "status"}}, OrderBy: []Order{{Ref: "m0"}}},
	}
	for i, sp := range bad {
		if _, _, err := e.build(context.Background(), tg, sp, testEnv()); err == nil {
			t.Errorf("case %d accepted: %+v", i, sp)
		}
	}
	good := []Spec{
		{Measures: []Measure{{Agg: "count"}, {Agg: "sum", Column: "cost_ms"}, {Agg: "avg", Column: "attempts"}, {Agg: "min", Column: "started_at"}, {Agg: "max", Column: "cost_ms"}}},
		{Measures: []Measure{{Agg: "count", Column: "status"}}},
	}
	for i, sp := range good {
		if _, _, err := e.build(context.Background(), tg, sp, testEnv()); err != nil {
			t.Errorf("good case %d rejected: %v", i, err)
		}
	}
}

func TestOperatorWhitelist(t *testing.T) {
	e := newEngine(nil2DB(t), nil)
	tg := logTarget(t)
	build := func(f Filter) error {
		_, _, err := e.build(context.Background(), tg, Spec{Dimensions: []Dim{{Column: "status"}}, Filters: []Filter{f}}, testEnv())
		return err
	}
	if len(opSQL) != 9 {
		t.Errorf("whitelist has %d ops, want exactly 9", len(opSQL))
	}
	for _, op := range []string{"=", "!=", ">", "<", ">=", "<=", "in", "like", "between"} {
		if _, ok := opSQL[op]; !ok {
			t.Errorf("op %q missing from whitelist", op)
		}
	}
	for _, op := range []string{"<>", "IN", "LIKE", "Like", "is", "is null", "not in", "regexp", "rlike", "or", "||", "=1 OR 1=1 --", "= ", " =", "==", "!", "ilike", "~", "similar to", "between and", "exists", ""} {
		if build(Filter{Column: "status", Op: op, Value: "x"}) == nil {
			t.Errorf("operator %q accepted", op)
		}
	}
	cases := []struct {
		f  Filter
		ok bool
	}{
		{Filter{"status", "=", "ok"}, true}, {Filter{"status", "!=", "ok"}, true}, {Filter{"status", "like", "ok"}, true},
		{Filter{"status", ">", "ok"}, false}, {Filter{"status", "between", []any{"a", "b"}}, false},
		{Filter{"status", "=", 5.0}, false}, {Filter{"status", "=", nil}, false}, {Filter{"status", "=", []any{"a"}}, false},
		{Filter{"status", "in", "a"}, false}, {Filter{"status", "in", []any{}}, false}, {Filter{"status", "in", []any{"a", "b"}}, true},
		{Filter{"attempts", ">", 3.0}, true}, {Filter{"attempts", "=", "3"}, true}, {Filter{"attempts", "=", "3 OR 1=1"}, false},
		{Filter{"attempts", "like", "3"}, false}, {Filter{"attempts", "between", []any{1.0, 5.0}}, true},
		{Filter{"attempts", "between", []any{1.0}}, false}, {Filter{"attempts", "between", []any{1.0, 2.0, 3.0}}, false},
		{Filter{"started_at", ">=", "2026-01-01"}, true}, {Filter{"started_at", ">=", "yesterday"}, false},
		{Filter{"started_at", "=", "2026-01-01' OR '1'='1"}, false},
	}
	for i, tc := range cases {
		if err := build(tc.f); (err == nil) != tc.ok {
			t.Errorf("case %d %+v: err=%v, want ok=%v", i, tc.f, err, tc.ok)
		}
	}
	big := make([]any, 101)
	for i := range big {
		big[i] = "v"
	}
	if build(Filter{Column: "status", Op: "in", Value: big}) == nil {
		t.Error("101-value IN accepted")
	}
	if build(Filter{Column: "status", Op: "in", Value: big[:100]}) != nil {
		t.Error("100-value IN rejected")
	}
}
