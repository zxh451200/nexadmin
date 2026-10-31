package ticket

// 真实 MySQL 集成测试：验证 003 迁移、状态机、内部备注过滤、批量操作、快捷回复与统计在真实引擎上的行为。
//
// 守卫：默认跳过；设置 NEXADMIN_IT_MYSQL_DSN 才执行，例如
//
//	NEXADMIN_IT_MYSQL_DSN='root:123456@tcp(127.0.0.1:3306)/' go test ./plugins/ticket -run TestITMySQL -v
//
// 隔离：自建一次性数据库 nexadmin_ticketit_<纳秒>，跑完整库 DROP，不触碰既有数据库。

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"gorm.io/driver/mysql"
	"gorm.io/gorm"

	"nexadmin/core/event"
	"nexadmin/core/notify"
	"nexadmin/core/response"
	"nexadmin/core/storage"
	"nexadmin/pkg/excel"
	"nexadmin/pkg/idgen"
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
	if err := admin.Ping(); err != nil {
		t.Fatalf("mysql not reachable: %v", err)
	}

	dbName := fmt.Sprintf("nexadmin_ticketit_%d", time.Now().UnixNano())
	if _, err := admin.Exec("CREATE DATABASE `" + dbName + "` CHARACTER SET utf8mb4"); err != nil {
		t.Fatalf("create throwaway db: %v", err)
	}
	var gdb *gorm.DB
	// 收尾顺序必须是：关业务连接池 → 删库 → 关管理连接。
	// 之前 admin.Close() 是 defer，比 t.Cleanup 先跑，DROP 永远报 "sql: database is closed"
	// 且错误被吞掉，每跑一次就在实例上留一个 nexadmin_ticketit_* 库。
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

	gormDSN := strings.Replace(dsn, ")/", ")/"+dbName+"", 1)
	gdb, err = gorm.Open(mysql.Open(gormDSN), &gorm.Config{})
	if err != nil {
		t.Fatalf("open gorm: %v", err)
	}
	runTicketMigrations(t, gdb)

	// 极简用户表：让负责人姓名/租户反查在真库上成立（真实项目由框架迁移提供）。
	for _, stmt := range []string{
		`CREATE TABLE sys_user (
			id BIGINT NOT NULL,
			tenant_id BIGINT NOT NULL,
			username VARCHAR(64) NOT NULL DEFAULT '',
			nickname VARCHAR(64) NOT NULL DEFAULT '',
			status VARCHAR(2) NOT NULL DEFAULT '1',
			is_super TINYINT(1) NOT NULL DEFAULT 0,
			deleted_at DATETIME(3) NULL,
			PRIMARY KEY (id)
		)`,
		`INSERT INTO sys_user (id, tenant_id, username, nickname, status) VALUES
			(9, 1, 'agent9', 'Agent9', '1'),
			(11, 1, 'alice', 'Alice', '1'),
			(12, 1, 'bob', 'Bob', '1')`,
	} {
		if err := gdb.Exec(stmt).Error; err != nil {
			t.Fatalf("seed sys_user: %v", err)
		}
	}

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
	if tk.Rating != nil {
		t.Fatalf("rating should be cleared on reopen: %+v", tk.Rating)
	}
	// 再次解决并重新评价：重开清掉旧评价后允许二次评价
	tk, err = svc.SetStatus(ctx, no, StatusResolved, "alice")
	must(err)
	ratedAgain, err := svc.Rate(ctx, no, RateInput{Score: 5})
	must(err)
	if ratedAgain.Rating == nil || *ratedAgain.Rating != 5 {
		t.Fatalf("re-rating after reopen failed: %+v", ratedAgain)
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
	if st.Satisfaction.Rated != 1 || st.Satisfaction.AvgScore != 5 || len(st.Satisfaction.ByScore) != 5 {
		t.Fatalf("unexpected satisfaction stats: %+v", st.Satisfaction)
	}

	// 分组与自动分派：组内成员按未完结单最少优先，并列取较小 user_id
	group, err := svc.CreateGroup(ctx, GroupInput{Name: "售后一组", AutoAssign: true, Enabled: true})
	must(err)
	view, err := svc.SetGroupMembers(ctx, group.ID, []idgen.ID{11, 12})
	must(err)
	if len(view.Members) != 2 {
		t.Fatalf("group members: %+v", view.Members)
	}
	makeTicket := func(title string) *Ticket {
		row, err := svc.Create(ctx, CreateInput{Title: title, Content: "x", ReporterType: ReplierReporter})
		must(err)
		return row
	}
	a := makeTicket("auto a")
	if a.GroupID != group.ID || int64(a.AssigneeID) != 11 {
		t.Fatalf("first auto assign: %+v", a)
	}
	b := makeTicket("auto b")
	if int64(b.AssigneeID) != 12 {
		t.Fatalf("second auto assign should pick the other member: %+v", b)
	}
	c := makeTicket("auto c")
	if int64(c.AssigneeID) != 11 {
		t.Fatalf("tie should pick the smaller user id: %+v", c)
	}

	// 改名要同步工单上的冗余组名；停用后不能转入，但存量工单仍可移出
	if _, err := svc.UpdateGroup(ctx, group.ID, GroupInput{Name: "售后一组（改名）", AutoAssign: true, Enabled: true}); err != nil {
		t.Fatal(err)
	}
	var renamed Ticket
	if err := gdb.Where("ticket_no = ?", a.TicketNo).Take(&renamed).Error; err != nil {
		t.Fatal(err)
	}
	if renamed.GroupName != "售后一组（改名）" {
		t.Fatalf("rename should sync ticket group_name, got %q", renamed.GroupName)
	}
	if _, err := svc.UpdateGroup(ctx, group.ID, GroupInput{Name: "售后一组（改名）", AutoAssign: true, Enabled: false}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.AssignGroup(ctx, c.TicketNo, group.ID); err == nil || err.Error() != "ticket.group_disabled" {
		t.Fatalf("disabled group should reject moving in with ticket.group_disabled, got %v", err)
	}
	if _, err := svc.CreateRule(ctx, RuleInput{Name: "指向停用组", Enabled: true, MatchKeyword: "x", SetGroupID: group.ID}); err == nil || err.Error() != "ticket.group_disabled" {
		t.Fatalf("rule should not target a disabled group, got %v", err)
	}

	moved, err := svc.AssignGroup(ctx, c.TicketNo, 0)
	must(err)
	if moved.GroupID != 0 || moved.GroupName != "" {
		t.Fatalf("move out of group: %+v", moved)
	}
	must(svc.DeleteGroup(ctx, group.ID))
	var members int64
	if err := gdb.Model(&GroupMember{}).Where("group_id = ?", int64(group.ID)).Count(&members).Error; err != nil {
		t.Fatal(err)
	}
	if members != 0 {
		t.Fatalf("group members should be deleted, got %d", members)
	}
	var kept Ticket
	if err := gdb.Where("ticket_no = ?", a.TicketNo).Take(&kept).Error; err != nil {
		t.Fatal(err)
	}
	if kept.GroupID != 0 || kept.GroupName != "" {
		t.Fatalf("ticket should fall back to no group: %+v", kept)
	}

	// 自动化规则：高优先 → 升级为紧急 + 转组 + 内部备注；停用后不再生效
	ruleGroup, err := svc.CreateGroup(ctx, GroupInput{Name: "规则组", AutoAssign: true, Enabled: true})
	must(err)
	_, err = svc.SetGroupMembers(ctx, ruleGroup.ID, []idgen.ID{11})
	must(err)
	rule, err := svc.CreateRule(ctx, RuleInput{
		Name: "高优先转组", Enabled: true, MatchPriority: PriorityHigh,
		SetPriority: PriorityUrgent, SetGroupID: ruleGroup.ID, Note: "自动规则已处理",
	})
	must(err)
	applied, err := svc.Create(ctx, CreateInput{
		Title: "urgent order", Content: "x", Priority: PriorityHigh, ReporterType: ReplierReporter,
	})
	must(err)
	if applied.Priority != PriorityUrgent || applied.GroupID != ruleGroup.ID || int64(applied.AssigneeID) != 11 {
		t.Fatalf("rule actions not applied: %+v", applied)
	}
	_, allRows2, err := svc.Replies(ctx, applied.TicketNo, 0, 100, true)
	must(err)
	noteFound := false
	for _, row := range allRows2 {
		if row.Internal && strings.Contains(row.Content, "自动规则已处理") {
			noteFound = true
		}
	}
	if !noteFound {
		t.Fatalf("rule note missing: %+v", allRows2)
	}
	if _, err := svc.UpdateRule(ctx, rule.ID, RuleInput{
		Name: "高优先转组", Enabled: false, MatchPriority: PriorityHigh,
		SetPriority: PriorityUrgent, SetGroupID: ruleGroup.ID, Note: "自动规则已处理",
	}); err != nil {
		t.Fatal(err)
	}
	// 关掉组的自动分派，避免默认分派把“规则未生效”的断言带偏
	if _, err := svc.UpdateGroup(ctx, ruleGroup.ID, GroupInput{Name: "规则组", Enabled: true, AutoAssign: false}); err != nil {
		t.Fatal(err)
	}
	notApplied, err := svc.Create(ctx, CreateInput{
		Title: "normal high", Content: "x", Priority: PriorityHigh, ReporterType: ReplierReporter,
	})
	must(err)
	if notApplied.Priority != PriorityHigh || notApplied.GroupID != 0 {
		t.Fatalf("disabled rule still applied: %+v", notApplied)
	}
	// 删除分组要清掉规则里对它的引用：只剩转组这一个动作的规则顺带停用，有其他动作的保持启用
	groupOnly, err := svc.CreateRule(ctx, RuleInput{Name: "只转组", Enabled: true, MatchKeyword: "vip", SetGroupID: ruleGroup.ID})
	must(err)
	withNote, err := svc.CreateRule(ctx, RuleInput{
		Name: "转组加备注", Enabled: true, MatchKeyword: "vip", SetGroupID: ruleGroup.ID, Note: "已登记",
	})
	must(err)
	must(svc.DeleteGroup(ctx, ruleGroup.ID))
	afterDelete, err := svc.ListRules(ctx)
	must(err)
	if len(afterDelete) != 3 {
		t.Fatalf("rules should survive group deletion, got %+v", afterDelete)
	}
	seenGroupOnly, seenNote := false, false
	for _, r := range afterDelete {
		if r.SetGroupID != 0 {
			t.Fatalf("dangling group reference kept: %+v", r)
		}
		if r.ID == groupOnly.ID {
			seenGroupOnly = !r.Enabled
		}
		if r.ID == withNote.ID {
			seenNote = r.Enabled && r.Note == "已登记"
		}
	}
	if !seenGroupOnly || !seenNote {
		t.Fatalf("group deletion did not adjust rules: %+v", afterDelete)
	}
	must(svc.DeleteRule(ctx, groupOnly.ID))
	must(svc.DeleteRule(ctx, withNote.ID))
	must(svc.DeleteRule(ctx, rule.ID))
	remaining, err := svc.ListRules(ctx)
	must(err)
	if len(remaining) != 0 {
		t.Fatalf("rule should be deleted, got %+v", remaining)
	}

	// 规则条件的业务类型候选：由已用过的工单类型与既有规则派生，去重且不含空值
	bizTicket, err := svc.Create(ctx, CreateInput{Title: "biz order", Content: "x", BizType: "order", ReporterType: ReplierReporter})
	must(err)
	if bizTicket.BizType != "order" {
		t.Fatalf("ticket biz type: %+v", bizTicket)
	}
	if _, err := svc.Create(ctx, CreateInput{Title: "biz order 2", Content: "x", BizType: "order", ReporterType: ReplierReporter}); err != nil {
		t.Fatal(err)
	}
	refundRule, err := svc.CreateRule(ctx, RuleInput{Name: "退款规则", Enabled: true, MatchBizType: "refund", Note: "退款待跟进"})
	must(err)
	options, err := svc.BizTypeOptions(ctx)
	must(err)
	if len(options) != 2 || options[0] != "order" || options[1] != "refund" {
		t.Fatalf("biz type options should be derived and sorted, got %+v", options)
	}
	must(svc.DeleteRule(ctx, refundRule.ID))

	// 附件登记与可见性：内部附件对访客不可见
	store, err := storage.NewLocal(t.TempDir())
	must(err)
	svc.SetStorage(store)
	attTicket := makeTicket("with attachments")
	publicAtt, err := svc.SaveAttachment(ctx, attTicket.TicketNo, ReplierReporter, "visitor", false, "photo.png", "image/png", 1234, "202610/aaa.png")
	must(err)
	internalAtt, err := svc.SaveAttachment(ctx, attTicket.TicketNo, ReplierAgent, "alice", true, "note.txt", "text/plain", 12, "202610/bbb.txt")
	must(err)
	visitorAtts, err := svc.ListAttachments(ctx, attTicket.TicketNo, false)
	must(err)
	if len(visitorAtts) != 1 || visitorAtts[0].Token != publicAtt.Token {
		t.Fatalf("visitor attachments: %+v", visitorAtts)
	}
	allAtts, err := svc.ListAttachments(ctx, attTicket.TicketNo, true)
	must(err)
	if len(allAtts) != 2 {
		t.Fatalf("agent attachments: %+v", allAtts)
	}
	if _, err := svc.AttachmentByToken(ctx, attTicket.TicketNo, internalAtt.Token, false); err == nil {
		t.Fatal("visitor must not resolve internal attachment")
	}
	if _, err := svc.AttachmentByToken(ctx, attTicket.TicketNo, internalAtt.Token, true); err != nil {
		t.Fatalf("agent should resolve internal attachment: %v", err)
	}
	if attTicket.TicketNo == "" || publicAtt.Name != "photo.png" {
		t.Fatalf("attachment metadata unexpected: %+v", publicAtt)
	}

	// 标签：规范化存储、按边界筛选
	tagged, err := svc.SetTags(ctx, attTicket.TicketNo, []string{"vip", "refund", "vip"})
	must(err)
	if len(tagged.TagList) != 2 || tagged.TagList[0] != "vip" {
		t.Fatalf("tags not normalized: %+v", tagged.TagList)
	}
	tags, err := svc.ListTags(ctx)
	must(err)
	if len(tags) != 2 {
		t.Fatalf("distinct tags: %+v", tags)
	}
	filtered, total, err := svc.ListTickets(ctx, TicketQuery{Tag: "vip", Page: 1, PageSize: 10})
	must(err)
	if total != 1 || len(filtered) != 1 || filtered[0].TicketNo != attTicket.TicketNo {
		t.Fatalf("tag filter: total=%d rows=%+v", total, filtered)
	}
	if _, total, err = svc.ListTickets(ctx, TicketQuery{Tag: "vi", Page: 1, PageSize: 10}); err != nil || total != 0 {
		t.Fatalf("partial tag should not match: total=%d err=%v", total, err)
	}
	cleared, err := svc.SetTags(ctx, attTicket.TicketNo, nil)
	must(err)
	if len(cleared.TagList) != 0 {
		t.Fatalf("tags not cleared: %+v", cleared.TagList)
	}

	// ticket.overdue 按负责人租户发布，openapi webhook 可订阅
	events := make(chan event.Event, 16)
	event.Subscribe(event.SubscriberFunc(func(e event.Event) {
		if e.Name == EventOverdue {
			events <- e
		}
	}))
	overdueTk := makeTicket("sla overdue")
	if _, err := svc.Assign(ctx, overdueTk.TicketNo, 11, "", 0); err != nil {
		t.Fatal(err)
	}
	pastDue := time.Now().UTC().Add(-2 * time.Hour)
	if err := gdb.Model(&Ticket{}).Where("ticket_no = ?", overdueTk.TicketNo).
		Updates(map[string]any{"due_at": pastDue, "overdue": false, "status": StatusOpen}).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := svc.MarkOverdue(ctx, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	found := false
	deadline := time.After(3 * time.Second)
loop:
	for {
		select {
		case ev := <-events:
			data, ok := ev.Data.(EventData)
			if !ok || data.TicketNo != overdueTk.TicketNo {
				continue
			}
			if ev.TenantID != 1 || data.SLAKind != "response" {
				t.Fatalf("unexpected overdue event: tenant=%d data=%+v", ev.TenantID, data)
			}
			found = true
			break loop
		case <-deadline:
			break loop
		}
	}
	if !found {
		t.Fatal("ticket.overdue event not published")
	}

	// 导出：与列表共用筛选，真实引擎跑通 COALESCE 排序 + tags LIKE + LIMIT；超上限在写文件前拒绝。
	useBuiltinLocale(t)
	exp := makeTicket("export me")
	if _, err := svc.SetTags(ctx, exp.TicketNo, []string{"vip"}); err != nil {
		t.Fatal(err)
	}
	exported, err := svc.ExportTickets(ctx, TicketQuery{Tag: "vip"}, exportLimit)
	must(err)
	if len(exported) != 1 || exported[0].TicketNo != exp.TicketNo {
		t.Fatalf("export by tag = %+v", exported)
	}
	if _, err := svc.ExportTickets(ctx, TicketQuery{}, 1); err == nil {
		t.Fatal("超过上限的导出应被拒绝")
	} else {
		var e *response.Error
		if !errors.As(err, &e) || e.Reason != ExportTooManyReason {
			t.Fatalf("limit error = %v", err)
		}
	}
	var exportFile bytes.Buffer
	must(svc.WriteTicketsXLSX(ctx, &exportFile, exported))
	cells, err := excel.ReadAll(bytes.NewReader(exportFile.Bytes()), int64(exportFile.Len()), excel.ReadOptions{})
	must(err)
	if len(cells) != 2 || cells[0][0] != "工单号" || cells[1][0] != exp.TicketNo {
		t.Fatalf("导出内容 = %v", cells)
	}
	if cells[1][9] != "vip" || cells[1][12] != "FALSE" {
		t.Errorf("标签/解决超时列 = %q / %q", cells[1][9], cells[1][12])
	}

	// 动态轨迹：真实引擎上一次生命周期的变更顺序、公开视图的过滤，以及改优先级重算 SLA 时钟。
	trail := makeTicket("activity trail")
	if _, _, err := svc.Reply(ctx, trail.TicketNo, ReplyInput{
		ReplierType: ReplierAgent, ReplierID: "11", ReplierName: "kf-11", Content: "看下",
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.SetStatus(ctx, trail.TicketNo, StatusResolved, "kf-11"); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.SetTags(ctx, trail.TicketNo, []string{"退款"}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Rate(ctx, trail.TicketNo, RateInput{Score: 4, Comment: "还行"}); err != nil {
		t.Fatal(err)
	}
	acts, err := svc.TicketActivities(ctx, trail.ID, false)
	must(err)
	if len(acts) == 0 {
		t.Fatal("生命周期没有留下任何动态")
	}
	if acts[0].Action != ActCreated {
		t.Errorf("第一条动态应是建单，得到 %q", acts[0].Action)
	}
	seen := map[string]int{}
	for i := 1; i < len(acts); i++ {
		if int64(acts[i].ID) <= int64(acts[i-1].ID) {
			t.Fatalf("动态未按时间升序：%d 在 %d 之后", acts[i-1].ID, acts[i].ID)
		}
		seen[acts[i].Action]++
	}
	if seen[ActStatusChanged] < 2 || seen[ActAssigneeChanged] < 1 || seen[ActTagsChanged] != 1 || seen[ActRatingAdded] != 1 {
		t.Errorf("动态覆盖不全：%+v", seen)
	}
	if acts[len(acts)-1].Action != ActRatingAdded || acts[len(acts)-1].ToValue != "4" {
		t.Errorf("最后一条动态 = %+v", acts[len(acts)-1])
	}
	// 客户自助页只能看到提交与状态变化。
	publicActs, err := svc.TicketActivities(ctx, trail.ID, true)
	must(err)
	for _, a := range publicActs {
		if a.Action != ActCreated && a.Action != ActStatusChanged {
			t.Errorf("公开动态里出现内部动作 %q", a.Action)
		}
	}
	if len(publicActs) >= len(acts) {
		t.Errorf("公开视图没有过滤：%d 条 vs 全部 %d 条", len(publicActs), len(acts))
	}
	// 改优先级按策略重算两个时钟（不是加死常量小时数）。
	prio := makeTicket("priority reprice")
	var preTicket Ticket
	must(gdb.Where("ticket_no = ?", prio.TicketNo).Take(&preTicket).Error)
	if preTicket.ResolutionDueAt == nil {
		t.Fatal("建单时就该有解决时限")
	}
	if _, err := svc.Bulk(ctx, BulkInput{TicketNos: []string{prio.TicketNo}, Action: "priority", Priority: PriorityUrgent}, 11, "kf-11"); err != nil {
		t.Fatal(err)
	}
	var postTicket Ticket
	must(gdb.Where("ticket_no = ?", prio.TicketNo).Take(&postTicket).Error)
	if !postTicket.DueAt.Before(*preTicket.DueAt) || !postTicket.ResolutionDueAt.Before(*preTicket.ResolutionDueAt) {
		t.Errorf("提到紧急后两个时钟都应收紧：due %v→%v，resolution %v→%v",
			preTicket.DueAt, postTicket.DueAt, preTicket.ResolutionDueAt, postTicket.ResolutionDueAt)
	}
	prioActs, err := svc.TicketActivities(ctx, postTicket.ID, false)
	must(err)
	last := prioActs[len(prioActs)-1]
	if last.TicketNo != prio.TicketNo {
		t.Errorf("动态应带上工单号，得到 %q", last.TicketNo)
	}
	if last.Action != ActPriorityChanged || last.FromValue != PriorityNormal || last.ToValue != PriorityUrgent {
		t.Errorf("改优先级的动态 = %+v", last)
	}
}
