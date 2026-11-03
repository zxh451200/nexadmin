package ticket

// 数据范围的真引擎用例（见 scope.go）。
//
// 为什么必须跑在真 MySQL 上而不是 sqlmock：scopeCond 拼的是裸 SQL 片段
// （assignee_id = 0 OR (...)），sqlmock 只做正则匹配、从不执行，删掉整个守卫
// 它也照样绿。这里每条断言都在数「裁剪后到底剩哪几张单」，去掉守卫就会多出来。
//
// 守卫：默认跳过；设置 NEXADMIN_IT_MYSQL_DSN 才执行（库名留空）：
//
//	NEXADMIN_IT_MYSQL_DSN='root:123456@tcp(127.0.0.1:3306)/' go test ./plugins/ticket -run TestITMySQLScope -v

import (
	"context"
	"testing"
	"time"

	"gorm.io/gorm"

	"nexadmin/core/datascope"
	"nexadmin/pkg/idgen"
)

// itScopeTables 在夹具那次性库上补齐 datascope.Resolve 要读的角色表。
// 真实项目由框架迁移提供；这里只建 Resolve 实际 SELECT 到的列。
func itScopeTables(t *testing.T, gdb *gorm.DB) {
	t.Helper()
	stmts := []string{
		`ALTER TABLE sys_user ADD COLUMN dept_id BIGINT NOT NULL DEFAULT 0`,
		`UPDATE sys_user SET dept_id = 100 WHERE id IN (9, 11)`,
		`UPDATE sys_user SET dept_id = 200 WHERE id = 12`,
		`CREATE TABLE sys_role (
			id BIGINT NOT NULL,
			data_scope VARCHAR(16) NOT NULL DEFAULT 'self',
			status VARCHAR(2) NOT NULL DEFAULT '1',
			PRIMARY KEY (id)
		)`,
		`CREATE TABLE sys_user_role (
			user_id BIGINT NOT NULL,
			role_id BIGINT NOT NULL
		)`,
		`INSERT INTO sys_role (id, data_scope, status) VALUES (500, 'dept', '1'), (501, 'all', '1')`,
		// 11/12 同属「本部门」角色但部门不同；9 挂「全部」。
		`INSERT INTO sys_user_role (user_id, role_id) VALUES (11, 500), (12, 500), (9, 501)`,
	}
	for _, s := range stmts {
		if err := gdb.Exec(s).Error; err != nil {
			t.Fatalf("scope fixture: %v", err)
		}
	}
}

// seedScopedTicket 直接落一张最小可查询的工单，绕开建单流程（本用例只关心读裁剪）。
func seedScopedTicket(t *testing.T, gdb *gorm.DB, no string, assignee int64) {
	t.Helper()
	row := &Ticket{
		Title: "scope " + no, Status: StatusOpen, Priority: PriorityNormal,
		ReporterType: "visitor", CustomValuesRaw: "{}", AssigneeID: idgen.ID(assignee),
	}
	row.TicketNo = no
	if err := gdb.WithContext(itCtx()).Create(row).Error; err != nil {
		t.Fatalf("seed ticket %s: %v", no, err)
	}
}

func withScope(ctx context.Context, sc *datascope.Scope) context.Context {
	return context.WithValue(ctx, scopeKey{}, sc)
}

// listNos 取列表结果的工单号集合，断言时按集合比而不是按下标比。
func listNos(t *testing.T, svc *Service, ctx context.Context) map[string]bool {
	t.Helper()
	rows, _, err := svc.ListTickets(ctx, TicketQuery{Page: 1, PageSize: 50})
	if err != nil {
		t.Fatalf("ListTickets: %v", err)
	}
	got := make(map[string]bool, len(rows))
	for _, r := range rows {
		got[r.TicketNo] = true
	}
	return got
}

func wantNos(t *testing.T, got map[string]bool, want ...string) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("可见工单数 = %d %v, want %d %v", len(got), keysOf(got), len(want), want)
	}
	for _, no := range want {
		if !got[no] {
			t.Fatalf("缺少应可见的 %s（实际 %v）", no, keysOf(got))
		}
	}
}

func keysOf(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

func TestITMySQLScope(t *testing.T) {
	svc, gdb, _ := itMySQLService(t)
	itScopeTables(t, gdb)
	ctx := itCtx()

	// 三张单：派给 11（本部门）、派给 12（别的部门）、未派发（公共待认领队列）
	seedScopedTicket(t, gdb, "TK-OWN-11", 11)
	seedScopedTicket(t, gdb, "TK-OWN-12", 12)
	seedScopedTicket(t, gdb, "TK-FREE", 0)

	t.Run("仅本人", func(t *testing.T) {
		// Depts 为空 -> UserIDCond 退化成 assignee_id = 我，但未派发仍要放行，
		// 否则待认领队列对受限坐席整体消失，认领入口再也点不到。
		scoped := withScope(ctx, &datascope.Scope{UserID: 11})
		wantNos(t, listNos(t, svc, scoped), "TK-OWN-11", "TK-FREE")
	})

	t.Run("本部门", func(t *testing.T) {
		scoped := withScope(ctx, &datascope.Scope{UserID: 11, Depts: []int64{100}, Preset: "dept"})
		wantNos(t, listNos(t, svc, scoped), "TK-OWN-11", "TK-FREE")
	})

	t.Run("全部", func(t *testing.T) {
		scoped := withScope(ctx, &datascope.Scope{All: true, UserID: 11})
		wantNos(t, listNos(t, svc, scoped), "TK-OWN-11", "TK-OWN-12", "TK-FREE")
	})

	t.Run("无范围不裁剪", func(t *testing.T) {
		// ctx 里没挂范围 = 内部调用方（访客接口、邮件进单、超时清扫），必须看到全量。
		wantNos(t, listNos(t, svc, ctx), "TK-OWN-11", "TK-OWN-12", "TK-FREE")
	})

	// 走框架的 Resolve，验证「角色 -> 部门集合 -> 条件」这条线在真表形状上成立。
	// 只测 scopeCond 不够：Resolve 读的是 sys_user_role/sys_role/sys_user.dept_id，
	// 列名或状态码对不上时它会静默返回空 Depts，条件退化成「仅本人」而看起来仍然像在工作。
	t.Run("Resolve按角色解析", func(t *testing.T) {
		sc, err := datascope.Resolve(ctx, gdb, idgen.ID(11), false)
		if err != nil {
			t.Fatalf("Resolve: %v", err)
		}
		if sc.All {
			t.Fatal("11 挂的是 dept 角色，不该解析成全部")
		}
		if len(sc.Depts) != 1 || sc.Depts[0] != 100 {
			t.Fatalf("Resolve Depts = %v, want [100]", sc.Depts)
		}
		wantNos(t, listNos(t, svc, withScope(ctx, sc)), "TK-OWN-11", "TK-FREE")

		// 同一条线对 9（全部角色）必须不裁剪——证明不是「谁都只看见自己那一张」。
		all, err := datascope.Resolve(ctx, gdb, idgen.ID(9), false)
		if err != nil {
			t.Fatalf("Resolve(9): %v", err)
		}
		if !all.All {
			t.Fatal("9 挂的是 all 角色，应解析为全部")
		}
		wantNos(t, listNos(t, svc, withScope(ctx, all)), "TK-OWN-11", "TK-OWN-12", "TK-FREE")
	})

	t.Run("单条可见性", func(t *testing.T) {
		sc := &datascope.Scope{UserID: 11, Depts: []int64{100}, Preset: "dept"}
		for _, tc := range []struct {
			no   string
			want bool
		}{
			{"TK-OWN-11", true},
			{"TK-OWN-12", false},
			{"TK-FREE", true},
			{"TK-NOT-EXIST", false},
			{"", false},
		} {
			got, err := svc.ticketVisible(ctx, tc.no, sc)
			if err != nil {
				t.Fatalf("ticketVisible(%q): %v", tc.no, err)
			}
			if got != tc.want {
				t.Errorf("ticketVisible(%q) = %v, want %v", tc.no, got, tc.want)
			}
		}
	})

	// 批量与合并的对侧单号都不在 :no 路径里，中间件够不着，靠服务层 guardNo 兜住。
	t.Run("批量越权整批拒绝", func(t *testing.T) {
		scoped := withScope(ctx, &datascope.Scope{UserID: 11, Depts: []int64{100}, Preset: "dept"})
		if _, err := svc.Bulk(scoped, BulkInput{TicketNos: []string{"TK-OWN-11", "TK-OWN-12"}, Action: "status", Status: StatusProcessing}, 11, "alice"); err == nil {
			t.Fatal("批量改到范围外的单应当报错，不能静默跳过")
		}
		// 同一套参数只圈范围内那张必须成功，证明上一条的红来自范围而不是批量本身。
		if _, err := svc.Bulk(scoped, BulkInput{TicketNos: []string{"TK-OWN-11"}, Action: "status", Status: StatusProcessing}, 11, "alice"); err != nil {
			t.Fatalf("范围内的批量操作被误拒: %v", err)
		}
	})

	t.Run("合并到范围外拒绝", func(t *testing.T) {
		scoped := withScope(ctx, &datascope.Scope{UserID: 11, Depts: []int64{100}, Preset: "dept"})
		if _, err := svc.Merge(scoped, "TK-OWN-11", "TK-OWN-12", "alice"); err == nil {
			t.Fatal("合并进范围外的单应当报错")
		}
		if _, err := svc.Link(scoped, "TK-FREE", "TK-OWN-12", "alice"); err == nil {
			t.Fatal("关联范围外的单应当报错")
		}
	})

	// 关联列表读路径：范围外的对侧单不该出现在我的关联面板里（loadLinkedTickets 取不到即跳过）。
	t.Run("关联列表隐藏范围外", func(t *testing.T) {
		if _, err := svc.Link(ctx, "TK-OWN-11", "TK-OWN-12", "alice"); err != nil {
			t.Fatalf("内部口径建关联失败: %v", err)
		}
		scoped := withScope(ctx, &datascope.Scope{UserID: 11, Depts: []int64{100}, Preset: "dept"})
		rows, err := svc.ListLinks(scoped, "TK-OWN-11")
		if err != nil {
			t.Fatalf("ListLinks: %v", err)
		}
		for _, r := range rows {
			if r.TicketNo == "TK-OWN-12" {
				t.Fatalf("范围外的对侧单漏进了关联列表: %+v", rows)
			}
		}
		// 正控制：不限范围时这条关联确实在，证明上面的空不是「关联根本没建成」。
		all, err := svc.ListLinks(ctx, "TK-OWN-11")
		if err != nil {
			t.Fatalf("ListLinks(all): %v", err)
		}
		found := false
		for _, r := range all {
			if r.TicketNo == "TK-OWN-12" {
				found = true
			}
		}
		if !found {
			t.Fatal("正控制失败：不限范围也看不到 TK-OWN-12，说明上面的断言是空转")
		}
	})
}

// TestITMySQLSort 在真引擎上验排序：CASE 表达式、COALESCE 和未知键退回都是只有真跑一遍
// 才说得清的（sqlmock 从不执行语句，ORDER BY 写错列它也一样绿）。
func TestITMySQLSort(t *testing.T) {
	svc, gdb, _ := itMySQLService(t)
	ctx := itCtx()

	base := time.Date(2025, 6, 1, 0, 0, 0, 0, time.UTC)
	seed := func(no, prio string, createdMin, repliedMin int) {
		t.Helper()
		created := base.Add(time.Duration(createdMin) * time.Minute)
		replied := base.Add(time.Duration(repliedMin) * time.Minute)
		row := &Ticket{
			Title: "sort " + no, Status: StatusOpen, Priority: prio,
			ReporterType: "visitor", CustomValuesRaw: "{}",
		}
		row.TicketNo = no
		row.CreatedAt = created
		row.LastReplyAt = &replied
		if err := gdb.WithContext(itCtx()).Create(row).Error; err != nil {
			t.Fatalf("seed %s: %v", no, err)
		}
	}
	// 三张单刻意让三个序互相打架：接错列时结果顺序必然不同，只验默认序照不出来。
	seed("TK-S-URGENT", "urgent", 0, 200) // 最早创建、最近回复
	seed("TK-S-LOW", "low", 200, 100)     // 最近创建、中间回复
	seed("TK-S-NORMAL", "normal", 100, 0) // 中间创建、最早回复

	order := func(t *testing.T, sort, dir string) []string {
		t.Helper()
		rows, _, err := svc.ListTickets(ctx, TicketQuery{Page: 1, PageSize: 50, Sort: sort, Order: dir})
		if err != nil {
			t.Fatalf("ListTickets(sort=%q order=%q): %v", sort, dir, err)
		}
		out := make([]string, 0, len(rows))
		for _, r := range rows {
			out = append(out, r.TicketNo)
		}
		return out
	}
	want := func(t *testing.T, got []string, exp ...string) {
		t.Helper()
		if len(got) != len(exp) {
			t.Fatalf("顺序 = %v, want %v", got, exp)
		}
		for i := range exp {
			if got[i] != exp[i] {
				t.Fatalf("顺序 = %v, want %v（第 %d 位不符）", got, exp, i)
			}
		}
	}

	t.Run("按最后回复降序", func(t *testing.T) { want(t, order(t, "lastReply", "desc"), "TK-S-URGENT", "TK-S-LOW", "TK-S-NORMAL") })
	t.Run("按创建时间降序", func(t *testing.T) { want(t, order(t, "createdAt", "desc"), "TK-S-LOW", "TK-S-NORMAL", "TK-S-URGENT") })
	t.Run("按优先级降序紧急在前", func(t *testing.T) { want(t, order(t, "priority", "desc"), "TK-S-URGENT", "TK-S-NORMAL", "TK-S-LOW") })
	t.Run("按优先级升序", func(t *testing.T) { want(t, order(t, "priority", "asc"), "TK-S-LOW", "TK-S-NORMAL", "TK-S-URGENT") })
	t.Run("默认列", func(t *testing.T) { want(t, order(t, "", ""), "TK-S-URGENT", "TK-S-LOW", "TK-S-NORMAL") })

	// 白名单外来的键一律退回默认列：这既是「不报错」的行为契约，也是注入面为零的证据。
	t.Run("未知键退回默认", func(t *testing.T) {
		want(t, order(t, "id; DROP TABLE plg_ticket", "desc; --"), "TK-S-URGENT", "TK-S-LOW", "TK-S-NORMAL")
		// 正控制：库还在、行还在，证明上面不是「查询静默失败返回空」冒充通过。
		var n int64
		if err := gdb.Model(&Ticket{}).Count(&n).Error; err != nil || n < 3 {
			t.Fatalf("变异键之后表应当完好，count=%d err=%v", n, err)
		}
	})
}
