package ticket

// 真实 PostgreSQL 集成测试：三引擎迁移一致性闸只比对结构，「PG 方言真能在引擎上执行」
// 只能在这里验——TEXT 列默认值、BOOLEAN 列、独立 CREATE UNIQUE INDEX、TEXT NOT NULL 的 ALTER。
//
// 守卫：默认跳过；设置 NEXADMIN_IT_PG_DSN 才执行，例如
//
//	NEXADMIN_IT_PG_DSN='host=localhost port=5432 user=postgres password=postgres dbname=postgres sslmode=disable' \
//	  go test ./plugins/ticket -run TestITPostgres -v
//
// 隔离：自建一次性库 nexadmin_ticketpg_<纳秒>，跑完 DROP ... WITH (FORCE)，不碰既有数据库。

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
	gormpg "gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func pgQuoteIdent(name string) string {
	return `"` + strings.ReplaceAll(name, `"`, `""`) + `"`
}

// pgAppendDBName 覆盖 key=value 形式 DSN 里的 dbname（没有就追加）。
func pgAppendDBName(dsn, dbName string) string {
	parts := strings.Fields(dsn)
	out := make([]string, 0, len(parts)+1)
	replaced := false
	for _, p := range parts {
		if strings.HasPrefix(p, "dbname=") {
			out = append(out, "dbname="+dbName)
			replaced = true
			continue
		}
		out = append(out, p)
	}
	if !replaced {
		out = append(out, "dbname="+dbName)
	}
	return strings.Join(out, " ")
}

func runTicketMigrationsPG(t *testing.T, gdb *gorm.DB) {
	t.Helper()
	entries, err := os.ReadDir("migrations/postgres")
	if err != nil {
		t.Fatal(err)
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".sql") {
			names = append(names, e.Name())
		}
	}
	// 按文件名升序执行（ReadDir 本身有序，但显式排一次免得依赖实现细节）。
	for i := 0; i < len(names); i++ {
		for j := i + 1; j < len(names); j++ {
			if names[j] < names[i] {
				names[i], names[j] = names[j], names[i]
			}
		}
	}
	for _, name := range names {
		b, err := os.ReadFile("migrations/postgres/" + name)
		if err != nil {
			t.Fatal(err)
		}
		// PreferSimpleProtocol 下允许一次 Exec 多语句，与真实 runner 的语句粒度一致。
		if err := gdb.Exec(string(b)).Error; err != nil {
			t.Fatalf("%s: %v", name, err)
		}
	}
}

func TestITPostgresTicketBatch(t *testing.T) {
	dsn := os.Getenv("NEXADMIN_IT_PG_DSN")
	if dsn == "" {
		t.Skip("NEXADMIN_IT_PG_DSN not set; skipping real-PostgreSQL integration test")
	}
	admin, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("open admin conn: %v", err)
	}
	if err := admin.Ping(); err != nil {
		t.Fatalf("postgres not reachable: %v", err)
	}

	dbName := fmt.Sprintf("nexadmin_ticketpg_%d", time.Now().UnixNano())
	if _, err := admin.Exec(`CREATE DATABASE ` + pgQuoteIdent(dbName)); err != nil {
		t.Fatalf("create throwaway db: %v", err)
	}
	var gdb *gorm.DB
	// 收尾顺序：关业务连接池 → 删库 → 关管理连接（反过来 DROP 会撞 "database is closed"）。
	t.Cleanup(func() {
		if gdb != nil {
			if sqlDB, err := gdb.DB(); err == nil {
				_ = sqlDB.Close()
			}
		}
		if _, err := admin.Exec(`DROP DATABASE ` + pgQuoteIdent(dbName) + ` WITH (FORCE)`); err != nil {
			t.Errorf("drop throwaway db %s: %v", dbName, err)
		}
		_ = admin.Close()
	})

	gdb, err = gorm.Open(gormpg.New(gormpg.Config{DSN: pgAppendDBName(dsn, dbName), PreferSimpleProtocol: true}), &gorm.Config{})
	if err != nil {
		t.Fatalf("open gorm: %v", err)
	}
	runTicketMigrationsPG(t, gdb)

	ctx := context.Background()
	svc := NewService(gdb, nopLogger{}).WithTimezone("UTC")
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}

	// 013：策略的 code/label/holidays（TEXT 列）与队列绑定
	_, err = svc.SaveSLA(ctx, SLASetting{
		Workdays: []int{1, 2, 3, 4, 5}, StartMinute: 9 * 60, EndMinute: 18 * 60,
		Targets: map[string]SLATarget{PriorityNormal: {FirstMinutes: 60, ResolutionMinutes: 600}},
	})
	must(err)
	vip, err := svc.SaveSLAPolicy(ctx, SLASetting{
		Code: "vip-desk", Label: "VIP", Workdays: []int{1, 2, 3, 4, 5}, StartMinute: 9 * 60, EndMinute: 18 * 60,
		Holidays: []string{"2026-01-02", "2026-01-01"},
		Targets:  map[string]SLATarget{PriorityNormal: {FirstMinutes: 15, ResolutionMinutes: 60}},
	})
	must(err)
	if len(vip.Holidays) != 2 || vip.Holidays[0] != "2026-01-01" {
		t.Fatalf("节假日没在 PG 上往返（去重升序）：%+v", vip.Holidays)
	}
	list, err := svc.ListSLAPolicies(ctx)
	must(err)
	if len(list) != 2 {
		t.Fatalf("策略行数 = %d，应为 2", len(list))
	}

	grp, err := svc.CreateGroup(ctx, GroupInput{Name: "VIP 队列", Enabled: true, SLACode: "vip-desk"})
	must(err)
	var reloaded Group
	must(gdb.Where("id = ?", int64(grp.ID)).Take(&reloaded).Error)
	if reloaded.SLACode != "vip-desk" || !reloaded.Enabled {
		t.Fatalf("分组在 PG 上没按预期落库：%+v", reloaded)
	}

	// 014：字段定义（TEXT options、BOOLEAN required/enabled）+ 工单 custom_values
	_, err = svc.SaveField(ctx, Field{
		Code: "order_channel", Label: "下单渠道", Type: FieldTypeSelect,
		OptionList: []string{"App", "门店"}, Required: true, Enabled: true,
	})
	must(err)
	if _, err = svc.SaveField(ctx, Field{Code: "serial_no", Label: "序列号", Type: FieldTypeText, Enabled: true}); err != nil {
		t.Fatal(err)
	}
	defs, err := svc.EnabledFields(ctx)
	must(err)
	if len(defs) != 2 {
		t.Fatalf("PG 上启用字段数 = %d，应为 2：%+v", len(defs), defs)
	}
	for _, d := range defs {
		if d.Code == "order_channel" && (len(d.OptionList) != 2 || !d.Required) {
			t.Fatalf("候选项/必填没从 TEXT/BOOLEAN 列读回来：%+v", d)
		}
	}

	fresh, err := svc.Create(ctx, CreateInput{Title: "pg t", Content: "c", ReporterType: ReplierReporter})
	must(err)
	moved, err := svc.AssignGroup(ctx, fresh.TicketNo, grp.ID)
	must(err)
	if moved.DueAt == nil {
		t.Fatal("PG 上转组后 due_at 丢了")
	}
	want := businessDue(moved.CreatedAt, vip.TargetFor(PriorityNormal).FirstMinutes, vip, time.UTC)
	if moved.DueAt.Unix() != want.Unix() {
		t.Fatalf("due_at = %v，按 vip-desk 应为 %v", moved.DueAt, want)
	}

	agentTk, err := svc.Create(ctx, CreateInput{
		Title: "pg agent", Content: "c", ReporterType: ReplierReporter, EnforceRequiredFields: true,
		CustomValues: map[string]any{"order_channel": "App", "serial_no": "SN-9"},
	})
	must(err)
	var stored Ticket
	must(gdb.Where("ticket_no = ?", agentTk.TicketNo).Take(&stored).Error)
	if stored.CustomValuesRaw != `{"order_channel":"App","serial_no":"SN-9"}` {
		t.Fatalf("custom_values 列 = %q", stored.CustomValuesRaw)
	}
	// nil 删除非必填项 + 同时改必填项的值
	updated, err := svc.SetCustomValues(ctx, agentTk.TicketNo, map[string]any{"serial_no": nil, "order_channel": "门店"})
	must(err)
	if _, ok := updated.CustomValues["serial_no"]; ok {
		t.Fatalf("nil 应删掉该字段：%+v", updated.CustomValues)
	}
	if updated.CustomValues["order_channel"] != "门店" {
		t.Fatalf("改值没生效：%+v", updated.CustomValues)
	}
	// 删必填项必须被拒（合并后整份再校验）
	if _, err := svc.SetCustomValues(ctx, agentTk.TicketNo, map[string]any{"order_channel": nil}); err == nil {
		t.Fatal("删掉必填字段必须被拒")
	}

	// status 多值在 PG 上落成 IN
	both, _, err := svc.ListTickets(ctx, TicketQuery{Status: "open,pending", Page: 1, PageSize: 20})
	must(err)
	onlyOpen, _, err := svc.ListTickets(ctx, TicketQuery{Status: StatusOpen, Page: 1, PageSize: 20})
	must(err)
	if len(both) < len(onlyOpen) {
		t.Fatalf("多值结果应覆盖单值：%d < %d", len(both), len(onlyOpen))
	}
	if _, _, err := svc.ListTickets(ctx, TicketQuery{Status: "open,vip"}); err == nil {
		t.Fatal("拼错的状态必须报错")
	}
}
