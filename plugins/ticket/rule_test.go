package ticket

import (
	"database/sql"
	"errors"
	"fmt"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	"gorm.io/gorm"

	"nexadmin/core/database"
	"nexadmin/core/response"
)

func TestRuleMatches(t *testing.T) {
	tk := Ticket{Priority: PriorityHigh, BizType: "order", Title: "Refund for order 42"}
	cases := []struct {
		name string
		rule Rule
		want bool
	}{
		{"no conditions", Rule{}, true},
		{"priority hit", Rule{MatchPriority: PriorityHigh}, true},
		{"priority miss", Rule{MatchPriority: PriorityLow}, false},
		{"biz type hit", Rule{MatchBizType: "order"}, true},
		{"biz type miss", Rule{MatchBizType: "refund"}, false},
		{"keyword hit case-insensitive", Rule{MatchKeyword: "REFUND"}, true},
		{"keyword miss", Rule{MatchKeyword: "invoice"}, false},
		{"all conditions", Rule{MatchPriority: PriorityHigh, MatchBizType: "order", MatchKeyword: "42"}, true},
		{"one condition fails", Rule{MatchPriority: PriorityHigh, MatchKeyword: "invoice"}, false},
	}
	for _, c := range cases {
		if got := ruleMatches(c.rule, tk); got != c.want {
			t.Errorf("%s: got %v, want %v", c.name, got, c.want)
		}
	}
}

func TestRuleInputNormalize(t *testing.T) {
	var bad RuleInput
	if err := bad.normalize(); err == nil {
		t.Fatal("empty rule accepted")
	}
	// 没有动作
	bad = RuleInput{Name: "x"}
	if err := bad.normalize(); err == nil {
		t.Fatal("rule without actions accepted")
	}
	// 非法优先级
	bad = RuleInput{Name: "x", MatchPriority: "nonsense", SetPriority: PriorityHigh}
	if err := bad.normalize(); err == nil {
		t.Fatal("invalid match priority accepted")
	}
	good := RuleInput{Name: "  高优先转组  ", Sort: -1, MatchPriority: PriorityHigh, SetPriority: PriorityUrgent, SetGroupID: 9, Note: "  自动备注  "}
	if err := good.normalize(); err != nil {
		t.Fatal(err)
	}
	if good.Name != "高优先转组" || good.Sort != 0 || good.Note != "自动备注" {
		t.Fatalf("unexpected normalized rule: %+v", good)
	}
}

// match_tags 的多标签语义是「与」：缺一个就不命中。改成 OR 时「缺一不命中」两行立刻变红。
func TestRuleMatchesTags(t *testing.T) {
	tagged := Ticket{Priority: PriorityHigh, Title: "x", Tags: canonicalTags([]string{"vip", "refund"})}
	cases := []struct {
		name   string
		rule   Rule
		ticket Ticket
		want   bool
	}{
		{"空标签条件不限", Rule{}, Ticket{Title: "x"}, true},
		{"单标签命中", Rule{MatchTags: []string{"vip"}}, tagged, true},
		{"多标签全命中", Rule{MatchTags: []string{"refund", "vip"}}, tagged, true},
		{"多标签缺一不命中", Rule{MatchTags: []string{"vip", "invoice"}}, tagged, false},
		{"标签全不命中", Rule{MatchTags: []string{"invoice"}}, tagged, false},
		{"无标签工单不命中", Rule{MatchTags: []string{"vip"}}, Ticket{Title: "x"}, false},
		{"边界：vip 不命中 viptag", Rule{MatchTags: []string{"vip"}}, Ticket{Tags: canonicalTags([]string{"viptag"})}, false},
		{"边界：vip 不命中 super,vip 之外的子串", Rule{MatchTags: []string{"ip"}}, Ticket{Tags: canonicalTags([]string{"vip"})}, false},
		// 建单链路里的 Ticket 是内存态，Tags 可能还没落到 AfterFind 拆好的 TagList。
		{"内存态 TagList 也算命中", Rule{MatchTags: []string{"vip"}}, Ticket{TagList: []string{"vip"}}, true},
		{"与标签配合其它条件", Rule{MatchPriority: PriorityHigh, MatchTags: []string{"vip"}}, tagged, true},
		{"与标签配合但优先级不符", Rule{MatchPriority: PriorityLow, MatchTags: []string{"vip"}}, tagged, false},
	}
	for _, c := range cases {
		if got := ruleMatches(c.rule, c.ticket); got != c.want {
			t.Errorf("%s: got %v, want %v", c.name, got, c.want)
		}
	}
	// 规则数组只能来自存储串：AfterFind 漏拆会让条件永远为空（= 恒真）。
	r := Rule{MatchTagsRaw: ",vip,refund,", AddTagsRaw: "", AddTags: []string{"stale"}}
	if err := r.AfterFind(nil); err != nil {
		t.Fatal(err)
	}
	if want := []string{"vip", "refund"}; !reflect.DeepEqual(r.MatchTags, want) {
		t.Errorf("MatchTags after find = %v, want %v", r.MatchTags, want)
	}
	if len(r.AddTags) != 0 {
		t.Errorf("AddTags from empty raw = %v, want empty", r.AddTags)
	}
	if got := tagsDisplay(",vip,refund,"); got != "vip,refund" {
		t.Errorf("tagsDisplay = %q", got)
	}
	if got := tagsDisplay(""); got != "" {
		t.Errorf("tagsDisplay of empty = %q, want empty", got)
	}
}

// 标签条件/动作的入参校验与配额：跟人工 SetTags 同一套限制，且 add_tags 单独也算合法动作。
func TestRuleInputNormalizeTags(t *testing.T) {
	// 只有 add_tags 一个动作：合法（「命中即打标」是 trigger 的基本形态）。
	good := RuleInput{Name: "打标", MatchTags: []string{" vip ", "vip", "", "order"}, AddTags: []string{"退款", " 退款 ", "vip"}}
	if err := good.normalize(); err != nil {
		t.Fatalf("tag-only rule rejected: %v", err)
	}
	if want := []string{"vip", "order"}; !reflect.DeepEqual(good.MatchTags, want) {
		t.Errorf("matchTags = %v, want %v", good.MatchTags, want)
	}
	if want := []string{"退款", "vip"}; !reflect.DeepEqual(good.AddTags, want) {
		t.Errorf("addTags = %v, want %v", good.AddTags, want)
	}
	// 只有 match_tags 条件、没有任何动作：仍然拒。
	rejected := func(name string, in RuleInput) {
		t.Helper()
		if err := in.normalize(); err == nil {
			t.Errorf("%s accepted", name)
		}
	}
	accepted := func(name string, in RuleInput) {
		t.Helper()
		if err := in.normalize(); err != nil {
			t.Errorf("%s rejected: %v", name, err)
		}
	}
	rejected("只有标签条件没有动作", RuleInput{Name: "x", MatchTags: []string{"vip"}})
	long := strings.Repeat("长", maxTagRunes+1)
	rejected("match_tags 超上限", RuleInput{Name: "x", SetPriority: PriorityHigh, MatchTags: manyTags(maxTags + 1)})
	rejected("add_tags 超上限", RuleInput{Name: "x", AddTags: manyTags(maxTags + 1)})
	rejected("add_tags 含逗号", RuleInput{Name: "x", AddTags: []string{"a,b"}})
	rejected("match_tags 含逗号", RuleInput{Name: "x", SetPriority: PriorityHigh, MatchTags: []string{"a,b"}})
	rejected("add_tags 超长", RuleInput{Name: "x", AddTags: []string{long}})
	rejected("match_tags 超长", RuleInput{Name: "x", SetPriority: PriorityHigh, MatchTags: []string{long}})
	rejected("add_tags 全空白等于没有动作", RuleInput{Name: "x", AddTags: []string{"  "}})
	// 上限本身要放行：正好 10 个、正好 20 rune。
	accepted("正好 10 个 add_tags", RuleInput{Name: "x", AddTags: manyTags(maxTags)})
	accepted("正好 20 rune", RuleInput{Name: "x", AddTags: []string{strings.Repeat("长", maxTagRunes)}})
}

func manyTags(n int) []string {
	out := make([]string, 0, n)
	for i := 0; i < n; i++ {
		out = append(out, fmt.Sprintf("t%d", i))
	}
	return out
}

// add_tags 的去重、保序与配额截断。
func TestMergeRuleTags(t *testing.T) {
	cases := []struct {
		name        string
		raw         string
		add         []string
		wantMerged  []string
		wantDropped []string
		wantChanged bool
	}{
		{"空工单追加", "", []string{"vip", "退款"}, []string{"vip", "退款"}, nil, true},
		{"原有顺序不动，追加在尾部", ",b,", []string{"a"}, []string{"b", "a"}, nil, true},
		{"已存在的不重复加", ",vip,refund,", []string{"refund", "vip"}, []string{"vip", "refund"}, nil, false},
		{"部分已存在只补缺", ",vip,", []string{"vip", "invoice"}, []string{"vip", "invoice"}, nil, true},
		{"重复入参只加一次", ",vip,", []string{"invoice", "invoice"}, []string{"vip", "invoice"}, nil, true},
		{"配额满时截断并记丢弃", canonicalTags(manyTags(maxTags)), []string{"extra"}, manyTags(maxTags), []string{"extra"}, false},
		{"配额边界：只塞得下一个", canonicalTags(manyTags(maxTags - 1)), []string{"x", "y"}, append(manyTags(maxTags-1), "x"), []string{"y"}, true},
		{"空追加入参", ",vip,", nil, []string{"vip"}, nil, false},
	}
	for _, c := range cases {
		merged, dropped := mergeRuleTags(c.raw, c.add)
		if !reflect.DeepEqual(merged, c.wantMerged) {
			t.Errorf("%s: merged = %v, want %v", c.name, merged, c.wantMerged)
		}
		if !reflect.DeepEqual(dropped, c.wantDropped) {
			t.Errorf("%s: dropped = %v, want %v", c.name, dropped, c.wantDropped)
		}
		if len(merged) > maxTags {
			t.Errorf("%s: merged exceeds quota: %d", c.name, len(merged))
		}
		if got := canonicalTags(merged) != c.raw; got != c.wantChanged {
			t.Errorf("%s: changed = %v, want %v", c.name, got, c.wantChanged)
		}
	}
}

// TestITMySQLRuleTags 在真实 MySQL 上验证 012 迁移与标签条件/动作的端到端行为：
// 规则字段落库与读回、前序规则的 add_tags 被后一条规则的 match_tags（与）命中、
// 跨规则去重、tags_changed 动态、以及 match_tags 缺一不命中。
//
// 守卫：默认跳过；设置 NEXADMIN_IT_MYSQL_DSN 才执行。隔离：自建一次性库
// nexadmin_ticketrule_<纳秒>，结束 DROP，不触碰任何既有库。
func TestITMySQLRuleTags(t *testing.T) {
	dsn := os.Getenv("NEXADMIN_IT_MYSQL_DSN")
	if dsn == "" {
		t.Skip("NEXADMIN_IT_MYSQL_DSN not set; skipping real-MySQL rule tags test")
	}
	if !strings.Contains(dsn, "multiStatements=true") {
		if strings.Contains(dsn, "?") {
			dsn += "&multiStatements=true"
		} else {
			dsn += "?multiStatements=true"
		}
	}
	if !strings.Contains(dsn, "parseTime=true") {
		dsn += "&parseTime=true&loc=UTC"
	}
	admin, err := sql.Open("mysql", dsn)
	if err != nil {
		t.Fatalf("open admin conn: %v", err)
	}
	if err := admin.Ping(); err != nil {
		_ = admin.Close()
		t.Fatalf("mysql not reachable: %v", err)
	}
	dbName := fmt.Sprintf("nexadmin_ticketrule_%d", time.Now().UnixNano())
	if _, err := admin.Exec("CREATE DATABASE `" + dbName + "` CHARACTER SET utf8mb4"); err != nil {
		_ = admin.Close()
		t.Fatalf("create throwaway db: %v", err)
	}
	var gdb *gorm.DB
	// 收尾顺序：关业务连接池 → 删库 → 关管理连接（admin.Close() 放 defer 会先于 t.Cleanup 跑，DROP 必失败）。
	t.Cleanup(func() {
		if gdb != nil {
			if sqlDB, err := gdb.DB(); err == nil {
				_ = sqlDB.Close()
			}
		}
		if _, err := admin.Exec("DROP DATABASE IF EXISTS `" + dbName + "`"); err != nil {
			t.Errorf("drop throwaway db %s: %v", dbName, err)
		}
		_ = admin.Close()
	})
	itRunMigrationsOn(t, dsn, dbName)
	cfg, err := itMySQLConfig(dsn, dbName)
	if err != nil {
		t.Fatalf("%v", err)
	}
	gdb, err = database.Open(cfg)
	if err != nil {
		t.Fatalf("database.Open: %v", err)
	}
	// 句柄默认绑租户 1：夹具里几十处 gdb.X 逐个 WithContext 不如在这里一次性定住，
	// 需要别的租户的用例（scope_it_test）再显式覆盖。
	gdb = gdb.WithContext(itCtx())

	ctx := testCtx()
	svc := NewService(gdb, nopLogger{}).WithTimezone("UTC")
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}

	// R1 只加标签（add_tags 单独成动作）；R2 需要 R1 打上的两个标签（与）才升级到 urgent；
	// R3 只要 vip 就补 invoice——真库上验证跨规则去重与配额之外的追加。
	r1, err := svc.CreateRule(ctx, RuleInput{Name: "订单打标", Enabled: true, Sort: 1,
		MatchBizType: "order", AddTags: []string{"vip", "退款"}})
	must(err)
	r2, err := svc.CreateRule(ctx, RuleInput{Name: "vip 退款升紧急", Enabled: true, Sort: 2,
		MatchTags: []string{"vip", "退款"}, SetPriority: PriorityUrgent})
	must(err)
	if got := canonicalTags([]string{"vip", "退款"}); r1.AddTagsRaw != got {
		t.Fatalf("R1 add_tags raw = %q, want %q", r1.AddTagsRaw, got)
	}

	rules, err := svc.ListRules(ctx)
	must(err)
	if len(rules) != 2 {
		t.Fatalf("rules = %d, want 2", len(rules))
	}
	if !reflect.DeepEqual(rules[0].AddTags, []string{"vip", "退款"}) {
		t.Fatalf("R1 add tags read back = %v", rules[0].AddTags)
	}
	if !reflect.DeepEqual(rules[1].MatchTags, []string{"vip", "退款"}) {
		t.Fatalf("R2 match tags read back = %v", rules[1].MatchTags)
	}

	// 命中链：order 单 → R1 打两个标签 → R2 的「与」条件成立 → 优先级 urgent。
	tk, err := svc.Create(ctx, CreateInput{BizType: "order", Title: "order broken", Content: "x",
		Priority: PriorityNormal, ReporterType: ReplierReporter, ReporterName: "visitor"})
	must(err)
	if tk.Tags != canonicalTags([]string{"vip", "退款"}) {
		t.Fatalf("ticket tags = %q", tk.Tags)
	}
	if !reflect.DeepEqual(tk.TagList, []string{"vip", "退款"}) {
		t.Fatalf("ticket tag list = %v", tk.TagList)
	}
	if tk.Priority != PriorityUrgent {
		t.Fatalf("chained match_tags rule did not fire: priority %q tags %q", tk.Priority, tk.Tags)
	}

	acts, err := svc.TicketActivities(ctx, tk.ID, false)
	must(err)
	var tagsRows, ruleRows int
	for _, a := range acts {
		if a.Action == ActTagsChanged {
			tagsRows++
			if a.ActorType != ActorSystem || a.ActorName != "rule" {
				t.Errorf("tags_changed actor = %s/%s, want system/rule", a.ActorType, a.ActorName)
			}
			if a.FromValue != "" || a.ToValue != "vip,退款" {
				t.Errorf("tags_changed values = %q -> %q, want \"\" -> \"vip,退款\"", a.FromValue, a.ToValue)
			}
		}
		if a.Action == ActRuleApplied {
			ruleRows++
		}
	}
	if tagsRows != 1 {
		t.Errorf("tags_changed rows = %d, want 1 (activities %+v)", tagsRows, acts)
	}
	if ruleRows != 2 {
		t.Errorf("rule_applied rows = %d, want 2 (R1+R2 both hit)", ruleRows)
	}

	// 与语义在真库上的反例：只打上 vip 而没有 退款 的工单，R2 不生效。
	other, err := svc.Create(ctx, CreateInput{BizType: "refund", Title: "refund request", Content: "x",
		Priority: PriorityNormal, ReporterType: ReplierReporter, ReporterName: "visitor"})
	must(err)
	if other.Tags != "" || other.Priority != PriorityNormal {
		t.Fatalf("non-order ticket should not fire tag rules: tags %q priority %q", other.Tags, other.Priority)
	}

	// 跨规则去重：R1 已打 vip，再加一条同标签规则不应产生第二份。
	dup, err := svc.CreateRule(ctx, RuleInput{Name: "重复打标", Enabled: true, Sort: 3, AddTags: []string{"vip", "invoice"}})
	must(err)
	if dup.AddTagsRaw != canonicalTags([]string{"vip", "invoice"}) {
		t.Fatalf("R3 raw = %q", dup.AddTagsRaw)
	}
	reloaded, err := svc.ListRules(ctx)
	must(err)
	if len(reloaded) != 3 || !reloaded[2].Enabled {
		t.Fatalf("reloaded rules = %+v", reloaded)
	}
	dupTk, err := svc.Create(ctx, CreateInput{BizType: "order", Title: "dup", Content: "x",
		Priority: PriorityNormal, ReporterType: ReplierReporter, ReporterName: "visitor"})
	must(err)
	if want := canonicalTags([]string{"vip", "退款", "invoice"}); dupTk.Tags != want {
		t.Fatalf("dedup across rules: tags = %q, want %q", dupTk.Tags, want)
	}

	// 修改规则：标签条件/动作可整体清空（map 更新漏列的话这里会残留旧值）。
	cleared, err := svc.UpdateRule(ctx, r2.ID, RuleInput{Name: r2.Name, Enabled: true, Sort: 2,
		MatchPriority: PriorityUrgent, SetPriority: PriorityUrgent})
	must(err)
	if len(cleared.MatchTags) != 0 || cleared.MatchTagsRaw != "" {
		t.Fatalf("cleared rule match tags = %+v raw %q", cleared.MatchTags, cleared.MatchTagsRaw)
	}
	stored, err := svc.ListRules(ctx)
	must(err)
	for _, row := range stored {
		if row.ID == r2.ID && (row.MatchTagsRaw != "" || row.AddTagsRaw != "") {
			t.Fatalf("r2 tag columns not cleared in db: %+v", row)
		}
	}

	// 校验在写库之前生效：11 个 add_tags 落不进去，也不能把脏数据留在表里。
	if _, err := svc.CreateRule(ctx, RuleInput{Name: "超限", AddTags: manyTags(maxTags + 1)}); err == nil {
		t.Error("over-quota add_tags accepted")
	} else if !isBadRequest(err) {
		t.Errorf("over-quota error = %v", err)
	}
	stored, err = svc.ListRules(ctx)
	must(err)
	if len(stored) != 3 {
		t.Fatalf("rules after rejected create = %d, want 3", len(stored))
	}
}

func isBadRequest(err error) bool {
	var resp *response.Error
	return errors.As(err, &resp) && resp.Status == 400
}
