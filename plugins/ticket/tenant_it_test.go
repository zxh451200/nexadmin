package ticket

// 跨租户隔离用例。夹具走 database.Open（生产构造器），租户条件由它内部装的 tenantPlugin
// 追加 —— 用裸 gorm.Open 跑这套断言会全部空转，因为那道闸门根本不存在。
//
// 守卫同 it_mysql_test.go：设置 NEXADMIN_IT_MYSQL_DSN 才执行。

import (
	"context"
	"testing"
	"time"

	"nexadmin/core/tenant"
	"nexadmin/pkg/idgen"
)

func TestITMySQLTenantIsolation(t *testing.T) {
	svc, gdb, _ := itMySQLService(t)
	ctx1 := itCtx()                               // 租户 1
	ctx2 := tenant.With(itBaseCtx(), idgen.ID(2)) // 租户 2

	open := func(ctx context.Context, tag string) *Ticket {
		t.Helper()
		tk, err := svc.Create(ctx, CreateInput{
			Title: "tenant " + tag, Content: "正文 " + tag,
			Priority: PriorityNormal, ReporterType: ReplierReporter, ReporterID: tag,
		})
		if err != nil {
			t.Fatalf("create %s: %v", tag, err)
		}
		return tk
	}
	t1 := open(ctx1, "T1")
	t2 := open(ctx2, "T2")
	if t1.TenantID != 1 {
		t.Fatalf("租户1 建的单 tenant_id = %d, want 1（BeforeCreate 没填上）", t1.TenantID)
	}
	if t2.TenantID != 2 {
		t.Fatalf("租户2 建的单 tenant_id = %d, want 2", t2.TenantID)
	}

	t.Run("列表互相看不见", func(t *testing.T) {
		cases := []struct {
			name string
			ctx  context.Context
			want string
		}{
			{"租户1 视角", ctx1, t1.TicketNo},
			{"租户2 视角", ctx2, t2.TicketNo},
		}
		for _, tc := range cases {
			rows, total, err := svc.ListTickets(tc.ctx, TicketQuery{Page: 1, PageSize: 50})
			if err != nil {
				t.Fatalf("%s: ListTickets: %v", tc.name, err)
			}
			if total != 1 || len(rows) != 1 || rows[0].TicketNo != tc.want {
				t.Fatalf("%s: 看到 %d 条 %v, want 仅 %s", tc.name, total, tenantNosOf(rows), tc.want)
			}
		}
	})

	t.Run("按单号读别人的单等于不存在", func(t *testing.T) {
		if _, err := svc.Ticket(ctx2, t1.TicketNo); err == nil {
			t.Fatal("租户2 读到了租户1 的工单")
		}
		if _, _, err := svc.Replies(ctx2, t1.TicketNo, 0, 50, true); err == nil {
			t.Fatal("租户2 读到了租户1 的回复流")
		}
		// 正控制：同一条调用换成自己的单号必须成功，证明上面的错来自租户而不是方法本身。
		if _, err := svc.Ticket(ctx2, t2.TicketNo); err != nil {
			t.Fatalf("正控制失败：租户2 读自己的单也报错 %v", err)
		}
	})

	t.Run("写路径同样越不过去", func(t *testing.T) {
		if _, _, err := svc.Reply(ctx2, t1.TicketNo, ReplyInput{Content: "串台回复", ReplierType: ReplierAgent, ReplierName: "bob"}); err == nil {
			t.Fatal("租户2 往租户1 的单里写了回复")
		}
		if _, err := svc.SetStatus(ctx2, t1.TicketNo, StatusProcessing, "bob"); err == nil {
			t.Fatal("租户2 改了租户1 的工单状态")
		}
		if _, err := svc.Merge(ctx2, t2.TicketNo, t1.TicketNo, "bob"); err == nil {
			t.Fatal("租户2 把自己并进租户1 的单")
		}
		if _, err := svc.Link(ctx2, t2.TicketNo, t1.TicketNo, "bob"); err == nil {
			t.Fatal("租户2 关联到了租户1 的单")
		}
		// 被拒的串台写入不该在租户1 的回复流里留下痕迹。
		if _, rows, err := svc.Replies(ctx1, t1.TicketNo, 0, 50, true); err != nil || len(rows) != 1 {
			t.Fatalf("租户1 的回复流被污染：%d 条 err=%v", len(rows), err)
		}
	})

	// mergeTicket 是 plg_ticket 的窄投影，不走 Ticket 模型：租户插件按 schema 里有没有
	// tenant_id 字段决定要不要加条件，漏了这个字段合并路径就会静默跨租户。
	t.Run("窄投影也带租户条件", func(t *testing.T) {
		if _, err := takeMergeTicket(gdb.WithContext(ctx2).Where("ticket_no = ?", t1.TicketNo)); err == nil {
			t.Fatal("mergeTicket 投影能取到别的租户的行 —— 租户插件没认这张表")
		}
		if _, err := takeMergeTicket(gdb.WithContext(ctx2).Where("ticket_no = ?", t2.TicketNo)); err != nil {
			t.Fatalf("正控制失败：本租户的投影取不到 %v", err)
		}
	})

	// fillMergedInto 是 Table("plg_ticket") 原生查询，插件不经过，条件得自己带。
	// 手工把租户1 的单指向租户2 的单，验证列表不会把别人的单号填进 MergedIntoNo。
	t.Run("原生回填不漏对侧单号", func(t *testing.T) {
		skip := tenant.Skip(itBaseCtx())
		if err := gdb.WithContext(skip).Model(&Ticket{}).
			Where("ticket_no = ?", t1.TicketNo).UpdateColumn("merged_into_id", int64(t2.ID)).Error; err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() {
			_ = gdb.WithContext(tenant.Skip(itBaseCtx())).Model(&Ticket{}).
				Where("ticket_no = ?", t1.TicketNo).UpdateColumn("merged_into_id", nil).Error
		})
		rows, _, err := svc.ListTickets(ctx1, TicketQuery{Page: 1, PageSize: 50})
		if err != nil {
			t.Fatal(err)
		}
		// 正控制前先确认指针真的种上了：没种上的话下面的断言是空转。
		var raw int64
		if err := gdb.WithContext(skip).Model(&Ticket{}).Select("merged_into_id").
			Where("ticket_no = ?", t1.TicketNo).Scan(&raw).Error; err != nil {
			t.Fatal(err)
		}
		if raw != int64(t2.ID) {
			t.Fatalf("正控制失败：跨租户指针没种上，raw=%d", raw)
		}
		for _, r := range rows {
			if r.TicketNo == t1.TicketNo && r.MergedIntoNo != "" {
				t.Fatalf("跨租户的合并目标单号漏进了列表：%q", r.MergedIntoNo)
			}
		}
	})

	t.Run("徽标按租户数", func(t *testing.T) {
		cases := []struct {
			name string
			ctx  context.Context
		}{{"租户1", ctx1}, {"租户2", ctx2}}
		for _, tc := range cases {
			n, err := svc.BadgeCount(tc.ctx, 0)
			if err != nil {
				t.Fatalf("%s: BadgeCount: %v", tc.name, err)
			}
			if n != 1 {
				t.Fatalf("%s 徽标 = %d, want 1（只该数到自己那张）", tc.name, n)
			}
		}
	})

	// 清扫器没有租户上下文，必须仍然跨租户扫得到：让两张单都已过首响时限。
	// 夹具的 NowFunc 固定在 testNow，往前挪一天即成「已过期」。
	t.Run("清扫器跨租户仍生效", func(t *testing.T) {
		past := testNow.AddDate(0, 0, -1)
		if err := gdb.WithContext(tenant.Skip(itBaseCtx())).Model(&Ticket{}).Where("1 = 1").
			UpdateColumns(map[string]any{"due_at": past, "overdue": false}).Error; err != nil {
			t.Fatal(err)
		}
		n, err := svc.MarkOverdue(itBaseCtx(), past.Add(24*time.Hour))
		if err != nil {
			t.Fatal(err)
		}
		if n != 2 {
			t.Fatalf("一轮清扫标记 %d 张, want 2（两个租户各一张）", n)
		}
	})
}

// 自动分派数的是「这个坐席手上全部未完结单」，不是本租户那一部分。队列与成员是平台级
// （迁移 017），坐席跨租户服务，所以 leastLoadedMember 的 JOIN 故意不带 tenant_id。
// 负载种成互相翻转的两档：按全量数该派 Bob(12)，按本租户数会派 Alice(11)——
// 给那条 JOIN 加租户条件，这条用例必须红。
func TestITMySQLAutoAssignCountsCrossTenantLoad(t *testing.T) {
	svc, gdb, _ := itMySQLService(t)
	ctx1 := itCtx()
	ctx2 := tenant.With(itBaseCtx(), idgen.ID(2))
	skip := tenant.Skip(itBaseCtx())

	// 先建单再建组：还没有启用的自动分派组时 Create 不派单，负载才种得死。
	seed := func(ctx context.Context, n int, assignee idgen.ID) {
		t.Helper()
		for i := 0; i < n; i++ {
			tk, err := svc.Create(ctx, CreateInput{
				Title: "load", Content: "load", Priority: PriorityNormal,
				ReporterType: ReplierReporter, ReporterID: "load",
			})
			if err != nil {
				t.Fatal(err)
			}
			if err := gdb.WithContext(skip).Model(&Ticket{}).Where("id = ?", int64(tk.ID)).
				UpdateColumn("assignee_id", int64(assignee)).Error; err != nil {
				t.Fatal(err)
			}
		}
	}
	seed(ctx2, 3, 11) // Alice：本租户 0 单，别租户 3 单
	seed(ctx1, 1, 12) // Bob：本租户 1 单

	count := func(assignee idgen.ID, tid int64) int64 {
		t.Helper()
		var n int64
		if err := gdb.WithContext(skip).Raw(
			"SELECT COUNT(*) FROM plg_ticket WHERE assignee_id = ? AND tenant_id = ? AND status IN (?,?,?)",
			int64(assignee), tid, StatusOpen, StatusProcessing, StatusPending).Scan(&n).Error; err != nil {
			t.Fatal(err)
		}
		return n
	}
	// 正控制：负载没种上的话，下面「派给谁」的断言就是空转。
	for _, c := range []struct {
		assignee idgen.ID
		tid      int64
		want     int64
	}{{11, 1, 0}, {11, 2, 3}, {12, 1, 1}, {12, 2, 0}} {
		if got := count(c.assignee, c.tid); got != c.want {
			t.Fatalf("种负载失败：assignee=%d tenant=%d 数到 %d, want %d", c.assignee, c.tid, got, c.want)
		}
	}

	g, err := svc.CreateGroup(ctx1, GroupInput{Name: "shared desk", AutoAssign: true, Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.SetGroupMembers(ctx1, g.ID, []idgen.ID{11, 12}); err != nil {
		t.Fatal(err)
	}

	fresh, err := svc.Create(ctx1, CreateInput{
		Title: "new", Content: "new", Priority: PriorityNormal,
		ReporterType: ReplierReporter, ReporterID: "new",
	})
	if err != nil {
		t.Fatal(err)
	}
	if fresh.AssigneeID != 12 {
		t.Fatalf("新单派给了 %d(%s), want 12(Bob)：全量负载 Bob=1 < Alice=3，"+
			"按本租户裁剪才会反过来选 Alice", fresh.AssigneeID, fresh.AssigneeName)
	}
}

func tenantNosOf(rows []Ticket) []string {
	out := make([]string, 0, len(rows))
	for _, r := range rows {
		out = append(out, r.TicketNo)
	}
	return out
}
