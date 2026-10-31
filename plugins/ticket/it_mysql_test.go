package ticket

// 真实 MySQL 集成测试：验证 003 迁移、状态机、内部备注过滤、批量操作、快捷回复与统计在真实引擎上的行为。
//
// 守卫：默认跳过；设置 NEXADMIN_IT_MYSQL_DSN 才执行，例如
//
//	NEXADMIN_IT_MYSQL_DSN='root:123456@tcp(127.0.0.1:3306)/' go test ./plugins/ticket -run TestITMySQL -v
//
// 隔离：自建一次性数据库 nexadmin_ticketit_<纳秒>，跑完整库 DROP，不触碰既有数据库。

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"gorm.io/driver/mysql"
	"gorm.io/gorm"

	"nexadmin/core/notify"
)

type fakeSender struct {
	mu   sync.Mutex
	msgs []notify.Msg
}

func (f *fakeSender) Send(_ context.Context, m notify.Msg) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.msgs = append(f.msgs, m)
	return nil
}

func (f *fakeSender) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.msgs)
}

func runTicketMigrations(t *testing.T, gdb *gorm.DB) {
	t.Helper()
	entries, err := os.ReadDir("migrations/mysql")
	if err != nil {
		t.Fatal(err)
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		names = append(names, e.Name())
	}
	sort.Strings(names)
	for _, name := range names {
		b, err := os.ReadFile("migrations/mysql/" + name)
		if err != nil {
			t.Fatal(err)
		}
		if err := gdb.Exec(string(b)).Error; err != nil {
			t.Fatalf("%s: %v", name, err)
		}
	}
}

func TestITMySQLTicketOps(t *testing.T) {
	dsn := os.Getenv("NEXADMIN_IT_MYSQL_DSN")
	if dsn == "" {
		t.Skip("NEXADMIN_IT_MYSQL_DSN not set; skipping real-MySQL integration test")
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
	defer func() { _ = admin.Close() }()
	if err := admin.Ping(); err != nil {
		t.Fatalf("mysql not reachable: %v", err)
	}

	dbName := fmt.Sprintf("nexadmin_ticketit_%d", time.Now().UnixNano())
	if _, err := admin.Exec("CREATE DATABASE `" + dbName + "` CHARACTER SET utf8mb4"); err != nil {
		t.Fatalf("create throwaway db: %v", err)
	}
	t.Cleanup(func() { _, _ = admin.Exec("DROP DATABASE IF EXISTS `" + dbName + "`") })

	gormDSN := strings.Replace(dsn, ")/", ")/"+dbName+"", 1)
	gdb, err := gorm.Open(mysql.Open(gormDSN), &gorm.Config{})
	if err != nil {
		t.Fatalf("open gorm: %v", err)
	}
	runTicketMigrations(t, gdb)

	ctx := context.Background()
	sender := &fakeSender{}
	svc := NewService(gdb, nopLogger{}).WithTimezone("UTC").WithNotify(sender, "zh-CN")

	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}

	// SLA 策略落库并刷新缓存；未填的优先级由默认值补齐
	saved, err := svc.SaveSLA(ctx, SLASetting{
		Workdays: []int{1, 2, 3, 4, 5}, StartMinute: 9 * 60, EndMinute: 18 * 60,
		Targets: map[string]SLATarget{PriorityUrgent: {FirstMinutes: 60, ResolutionMinutes: 120}},
	})
	must(err)
	if len(saved.Workdays) != 5 || saved.StartMinute != 540 || saved.Targets[PriorityNormal].FirstMinutes == 0 {
		t.Fatalf("saved SLA unexpected: %+v", saved)
	}
	loaded, err := svc.LoadSLA(ctx)
	must(err)
	if loaded.StartMinute != 540 || loaded.Targets[PriorityUrgent].FirstMinutes != 60 {
		t.Fatalf("loaded SLA unexpected: %+v", loaded)
	}
	if svc.CurrentSLA().StartMinute != 540 {
		t.Fatal("cache not refreshed after save")
	}

	// 建单：open + 响应时钟
	tk, err := svc.Create(ctx, CreateInput{Title: "order broken", Content: "item damaged", Priority: PriorityHigh, ReporterType: ReplierReporter, ReporterName: "visitor"})
	must(err)
	if tk.Status != StatusOpen || tk.DueAt == nil {
		t.Fatalf("create: %+v", tk)
	}
	no := tk.TicketNo

	// 坐席公开回复 → pending、停表、首响、自动认领
	_, tk, err = svc.Reply(ctx, no, ReplyInput{ReplierType: ReplierAgent, ReplierID: "9", ReplierName: "alice", Content: "looking into it"})
	must(err)
	if tk.Status != StatusPending || tk.SlaPausedAt == nil || tk.FirstReplyAt == nil || int64(tk.AssigneeID) != 9 {
		t.Fatalf("agent reply: %+v", tk)
	}
	if sender.count() != 0 {
		t.Fatalf("agent reply must not notify, got %d", sender.count())
	}

	// 内部备注：落库但不改状态、不通知、访客不可见
	note, tk, err := svc.Reply(ctx, no, ReplyInput{ReplierType: ReplierAgent, ReplierID: "9", ReplierName: "alice", Content: "note: warehouse out of stock", Internal: true})
	must(err)
	if !note.Internal || tk.Status != StatusPending || sender.count() != 0 {
		t.Fatalf("internal note: reply=%+v ticket=%+v notified=%d", note, tk, sender.count())
	}

	// 访客回复 → processing、恢复时钟、通知负责人
	pausedDue := *tk.DueAt
	_, tk, err = svc.Reply(ctx, no, ReplyInput{ReplierType: ReplierReporter, ReplierName: "visitor", Content: "any update?"})
	must(err)
	if tk.Status != StatusProcessing || tk.SlaPausedAt != nil || tk.DueAt == nil || !tk.DueAt.After(pausedDue) {
		t.Fatalf("visitor resume: %+v", tk)
	}
	if sender.count() != 1 || len(sender.msgs[0].To) != 1 || int64(sender.msgs[0].To[0]) != 9 {
		t.Fatalf("reporter reply should notify assignee, got %+v", sender.msgs)
	}

	// 访客视角过滤内部备注，坐席视角保留
	_, visitorRows, err := svc.Replies(ctx, no, 0, 100, false)
	must(err)
	for _, r := range visitorRows {
		if r.Internal {
			t.Fatalf("visitor saw internal note: %+v", r)
		}
	}
	_, allRows, err := svc.Replies(ctx, no, 0, 100, true)
	must(err)
	internal := 0
	for _, r := range allRows {
		if r.Internal {
			internal++
		}
	}
	if internal != 1 {
		t.Fatalf("agent view should keep 1 internal note, got %d", internal)
	}

	// 解决 → resolved_at；报告人再回复重开
	tk, err = svc.SetStatus(ctx, no, StatusResolved, "alice")
	must(err)
	if tk.ResolvedAt == nil {
		t.Fatalf("resolved_at missing: %+v", tk)
	}
	// 满意度：解决后可评一次，重复评价被拒
	rated, err := svc.Rate(ctx, no, RateInput{Score: 4, Comment: "fast"})
	must(err)
	if rated.Rating == nil || *rated.Rating != 4 || rated.RatingComment != "fast" {
		t.Fatalf("rating not persisted: %+v", rated)
	}
	if _, err := svc.Rate(ctx, no, RateInput{Score: 5}); err == nil {
		t.Fatal("second rating accepted")
	}
	_, tk, err = svc.Reply(ctx, no, ReplyInput{ReplierType: ReplierReporter, ReplierName: "visitor", Content: "still broken"})
	must(err)
	if tk.Status != StatusProcessing || tk.ResolvedAt != nil {
		t.Fatalf("reopen after resolved: %+v", tk)
	}

	// 快捷回复 CRUD
	canned, err := svc.CreateCanned(ctx, CannedInput{Title: "refund", Content: "refund submitted"})
	must(err)
	rows, err := svc.ListCanned(ctx)
	must(err)
	if len(rows) != 1 || rows[0].Title != "refund" {
		t.Fatalf("canned list: %+v", rows)
	}
	if _, err := svc.UpdateCanned(ctx, canned.ID, CannedInput{Title: "refund v2", Content: "refund submitted, 1-3 days"}); err != nil {
		t.Fatal(err)
	}
	must(svc.DeleteCanned(ctx, canned.ID))
	rows, err = svc.ListCanned(ctx)
	must(err)
	if len(rows) != 0 {
		t.Fatalf("canned should be empty, got %+v", rows)
	}

	// 批量：认领、关闭、跳过不存在的工单
	var bulkNos []string
	for i := 0; i < 3; i++ {
		row, err := svc.Create(ctx, CreateInput{Title: fmt.Sprintf("bulk %d", i), Content: "x", ReporterType: ReplierReporter})
		must(err)
		bulkNos = append(bulkNos, row.TicketNo)
	}
	before := sender.count()
	res, err := svc.Bulk(ctx, BulkInput{TicketNos: bulkNos, Action: "assign", AssigneeID: 9, AssigneeName: "alice"}, 1, "admin")
	must(err)
	if res.Updated != 3 || res.Skipped != 0 {
		t.Fatalf("bulk assign: %+v", res)
	}
	if sender.count() != before+1 {
		t.Fatalf("bulk assign should send one summary notification, got %d", sender.count()-before)
	}
	res, err = svc.Bulk(ctx, BulkInput{TicketNos: append(bulkNos, "TKmissing"), Action: "status", Status: StatusClosed}, 1, "admin")
	must(err)
	if res.Updated != 3 || res.Skipped != 1 {
		t.Fatalf("bulk close: %+v", res)
	}
	res, err = svc.Bulk(ctx, BulkInput{TicketNos: bulkNos[:1], Action: "priority", Priority: PriorityUrgent}, 1, "admin")
	must(err)
	if res.Updated != 1 {
		t.Fatalf("bulk priority: %+v", res)
	}

	// pending 停表：把 due_at 调到过去也不应被清扫器标记
	pending, err := svc.Create(ctx, CreateInput{Title: "waiting", Content: "x", ReporterType: ReplierReporter})
	must(err)
	if _, err := svc.SetStatus(ctx, pending.TicketNo, StatusPending, "alice"); err != nil {
		t.Fatal(err)
	}
	past := time.Now().UTC().Add(-time.Hour)
	if err := gdb.Model(&Ticket{}).Where("ticket_no = ?", pending.TicketNo).Update("due_at", past).Error; err != nil {
		t.Fatal(err)
	}
	marked, err := svc.MarkOverdue(ctx, time.Now().UTC())
	must(err)
	if marked != 0 {
		t.Fatalf("pending ticket must not be marked overdue, got %d", marked)
	}

	// 统计：pending 计数与解决时长都不为空
	st, err := svc.Stats(ctx, TicketStatsQuery{})
	must(err)
	if st.Total == 0 || st.Pending != 1 || st.Days == nil || st.ByStatus == nil || st.SlaByPrio == nil {
		t.Fatalf("unexpected stats: %+v", st)
	}
	if st.Satisfaction.Rated != 1 || st.Satisfaction.AvgScore != 4 || len(st.Satisfaction.ByScore) != 5 {
		t.Fatalf("unexpected satisfaction stats: %+v", st.Satisfaction)
	}
}
