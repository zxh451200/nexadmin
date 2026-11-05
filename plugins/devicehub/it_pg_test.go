package devicehub

// 真实 PostgreSQL 集成测试。devicehub 有三引擎迁移，但执行过它们的只有 MySQL 一条 IT——
// 而这个插件的两个产品级 bug 都长在方言层：alarm_event 三引擎都缺 updated_at（插入即 500），
// DeleteDevice 对「只有 point_id、没有 device_id」的聚合表/告警规则发 device_id 谓词。
// 这两条在 MySQL 上是被跑出来的，在 PG 上此前没人跑过 postgres/001_init.sql。
//
// 与 it_mysql_test.go 的分工：行为语义（缩放/死区/冷却/只读拒写）以 MySQL 那份为准，
// 这份负责「PG 方言真能在引擎上执行 + 同一套级联删除在 PG 上也成立」。
//
// 守卫：默认跳过；设置 NEXADMIN_IT_PG_DSN 才执行，例如
//
//	NEXADMIN_IT_PG_DSN='host=localhost port=5432 user=postgres password=123456 dbname=postgres sslmode=disable' \
//	  go test ./plugins/devicehub -run TestITPostgresDevicehub -v
//
// 库名要有真值（pgx 的 keyword DSN 没有空值写法，见 AGENTS.md 第 3 节）。
// 隔离：自建一次性库，收尾按「关业务池 → DROP ... WITH (FORCE) → 关管理连接」的顺序，
// 少一步就在实例上留一座库。

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"sort"
	"strings"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
	gormpg "gorm.io/driver/postgres"
	"gorm.io/gorm"

	"nexadmin/plugins/devicehub/driver"
)

func itDevicehubPG(t *testing.T) (*Service, *gorm.DB, *collectLogger) {
	t.Helper()
	dsn := os.Getenv("NEXADMIN_IT_PG_DSN")
	if dsn == "" {
		t.Skip("NEXADMIN_IT_PG_DSN not set; skipping real-PostgreSQL integration test")
	}
	admin, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("open admin conn: %v", err)
	}
	if err := admin.Ping(); err != nil {
		_ = admin.Close()
		t.Fatalf("postgres not reachable: %v", err)
	}
	dbName := fmt.Sprintf("nexadmin_devhubit_%d", time.Now().UnixNano())
	if _, err := admin.Exec(`CREATE DATABASE ` + pgQuoteIdent(dbName)); err != nil {
		_ = admin.Close()
		t.Fatalf("create throwaway db: %v", err)
	}
	var gdb *gorm.DB
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

	gdb, err = gorm.Open(gormpg.New(gormpg.Config{
		DSN: pgWithDBName(dsn, dbName), PreferSimpleProtocol: true,
	}), &gorm.Config{})
	if err != nil {
		t.Fatalf("open gorm: %v", err)
	}
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
	sort.Strings(names)
	for _, name := range names {
		b, err := os.ReadFile("migrations/postgres/" + name)
		if err != nil {
			t.Fatal(err)
		}
		if err := gdb.Exec(string(b)).Error; err != nil {
			t.Fatalf("exec migrations/postgres/%s: %v", name, err)
		}
	}
	lg := &collectLogger{}
	return NewService(gdb, lg), gdb, lg
}

func TestITPostgresDevicehub(t *testing.T) {
	svc, gdb, lg := itDevicehubPG(t)
	ctx := context.Background()

	d, err := svc.SaveDevice(ctx, 0, DeviceInput{
		Name: "1# 变频器", Protocol: "modbus-tcp", Endpoint: "127.0.0.1:1502",
		Config: `{"unitId":1,"pollIntervalMs":1000}`, Enabled: false,
	})
	if err != nil {
		t.Fatalf("save device: %v", err)
	}
	if d.DeviceNo == "" {
		t.Fatal("device_no 应自动生成")
	}

	p, err := svc.SavePoint(ctx, d.ID, 0, PointInput{
		Name: "输出频率", Address: "hr:40001", Unit: "Hz", RW: RWBoth,
		DataType: TypeFloat, Scale: 10, Deadband: 0.1, Enabled: true,
	})
	if err != nil {
		t.Fatalf("save point: %v", err)
	}

	// 采集落库：raw 427 / scale 10 = 42.7；PG 侧 last_value_num 是可空 numeric，写回要能读出来。
	svc.onReading(ctx, d, p.Address, driver.Value{Num: 427}, time.Second)
	var stored Point
	if err := gdb.Where("id = ?", int64(p.ID)).Take(&stored).Error; err != nil {
		t.Fatal(err)
	}
	if stored.LastValueNum == nil || *stored.LastValueNum != 42.7 {
		t.Fatalf("last_value_num = %v, want 42.7", stored.LastValueNum)
	}
	var readings int64
	if err := gdb.Model(&Reading{}).Where("point_id = ?", int64(p.ID)).Count(&readings).Error; err != nil {
		t.Fatal(err)
	}
	if readings != 1 {
		t.Fatalf("readings = %d, want 1; logs=%v", readings, lg.msgs)
	}

	// 告警事件写入要带 updated_at——三引擎迁移都曾缺这一列，PG 侧少它就是 insert 报错。
	if _, err := svc.SaveAlarmRule(ctx, 0, AlarmRuleInput{
		PointID: p.ID, Name: "超频", Op: "gt", Threshold: 40, Level: LevelWarn, Enabled: true, CooldownSeconds: 3600,
	}); err != nil {
		t.Fatalf("save alarm rule: %v", err)
	}
	svc.evaluateAlarms(ctx, d, p, pointTick{PointID: p.ID, Num: float64ptr(42.7), TS: time.Now().UTC()})
	var events int64
	if err := gdb.Model(&AlarmEvent{}).Where("point_id = ?", int64(p.ID)).Count(&events).Error; err != nil {
		t.Fatal(err)
	}
	if events != 1 {
		t.Fatalf("alarm events = %d, want 1; logs=%v", events, lg.msgs)
	}

	// 聚合行 + 指令：只有 point_id 的表和带 device_id 的表都要能写。
	if err := gdb.Create(&ReadingAgg{PointID: p.ID, Bucket: time.Now().UTC().Truncate(time.Hour),
		NumMin: 42.7, NumMax: 42.7, NumAvg: 42.7, Cnt: 1}).Error; err != nil {
		t.Fatalf("seed reading agg: %v", err)
	}
	if _, err := svc.CreateCommand(ctx, CommandInput{
		DeviceNo: d.DeviceNo, PointID: p.ID, Value: 35, OperatorName: "tester",
	}); err != nil {
		t.Fatalf("create command: %v", err)
	}

	// 删测点：规则跟着测点走，历史读数保留。
	svc.onReading(ctx, d, p.Address, driver.Value{Num: 500}, time.Second)
	if err := svc.DeletePoint(ctx, d.ID, p.ID); err != nil {
		t.Fatalf("delete point: %v", err)
	}
	var left int64
	if err := gdb.Model(&AlarmRule{}).Where("point_id = ?", int64(p.ID)).Count(&left).Error; err != nil {
		t.Fatal(err)
	}
	if left != 0 {
		t.Errorf("删测点后残留 %d 条指向已删测点的告警规则", left)
	}
	if err := gdb.Model(&Reading{}).Where("point_id = ?", int64(p.ID)).Count(&left).Error; err != nil {
		t.Fatal(err)
	}
	if left == 0 {
		t.Error("测点历史读数应保留（趋势数据不因删测点而丢）")
	}

	// 级联删除：device_id 谓词与「顺着测点找」的子查询在 PG 上同样得成立。
	if err := svc.DeleteDevice(ctx, d.ID); err != nil {
		t.Fatalf("delete device: %v", err)
	}
	for _, tc := range []struct {
		table, where string
		args         []any
	}{
		{"plg_devhub_point", "device_id = ?", []any{int64(d.ID)}},
		{"plg_devhub_reading", "device_id = ? OR point_id = ?", []any{int64(d.ID), int64(p.ID)}},
		{"plg_devhub_reading_agg", "point_id = ?", []any{int64(p.ID)}},
		{"plg_devhub_alarm_rule", "point_id = ?", []any{int64(p.ID)}},
		{"plg_devhub_alarm_event", "device_id = ? OR point_id = ?", []any{int64(d.ID), int64(p.ID)}},
		{"plg_devhub_command", "device_id = ? OR point_id = ?", []any{int64(d.ID), int64(p.ID)}},
	} {
		var n int64
		if err := gdb.Table(tc.table).Where(tc.where, tc.args...).Count(&n).Error; err != nil {
			t.Fatalf("%s: %v", tc.table, err)
		}
		if n != 0 {
			t.Fatalf("%s 残留 %d 行", tc.table, n)
		}
	}
}

func pgQuoteIdent(name string) string {
	return `"` + strings.ReplaceAll(name, `"`, `""`) + `"`
}

// pgWithDBName 用一次性库名覆盖 DSN 里的 dbname（没有就补一个）。
func pgWithDBName(dsn, dbName string) string {
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
