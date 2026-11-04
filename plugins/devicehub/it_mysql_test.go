package devicehub

// 真实 MySQL 集成测试：验证 001 迁移、设备/测点 CRUD、采集落库（死区/缩放）、
// 告警评估与冷却、指令队列在真实引擎上的行为。
//
// 守卫：默认跳过；设置 NEXADMIN_IT_MYSQL_DSN 才执行（库名留空，框架规则）。
// 隔离：自建一次性数据库，跑完 DROP，与既有库无交集。

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"sort"
	"strings"
	"testing"
	"time"

	"gorm.io/driver/mysql"
	"gorm.io/gorm"

	"nexadmin/plugins/devicehub/driver"
)

// collectLogger 收集日志：storeReading 的失败被吞进 Warn，测试要看得到。
type collectLogger struct{ msgs []string }

func (l *collectLogger) Warn(msg string, kv ...any)  { l.msgs = append(l.msgs, msg) }
func (l *collectLogger) Error(msg string, kv ...any) { l.msgs = append(l.msgs, msg) }

func itDevicehubService(t *testing.T) (*Service, *gorm.DB, *collectLogger) {
	t.Helper()
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
	dbName := fmt.Sprintf("nexadmin_devhubit_%d", time.Now().UnixNano())
	if _, err := admin.Exec("CREATE DATABASE `" + dbName + "` CHARACTER SET utf8mb4"); err != nil {
		t.Fatalf("create throwaway db: %v", err)
	}
	var gdb *gorm.DB
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
	if gormDSN == dsn {
		t.Fatalf("NEXADMIN_IT_MYSQL_DSN 形如 user:pass@tcp(host:port)/（库名留空），当前 DSN 换不出一次性库：%s", dsn)
	}
	gdb, err = gorm.Open(mysql.Open(gormDSN), &gorm.Config{})
	if err != nil {
		t.Fatalf("open gorm: %v", err)
	}
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
	lg := &collectLogger{}
	return NewService(gdb, lg), gdb, lg
}

// TestITMySQLDevicehub 走通 001 迁移 + 设备/测点 CRUD + 采集落库（缩放/死区）+ 告警评估与冷却。
func TestITMySQLDevicehub(t *testing.T) {
	svc, gdb, lg := itDevicehubService(t)
	ctx := context.Background()

	// 设备 CRUD
	d, err := svc.SaveDevice(ctx, 0, DeviceInput{
		Name: "1# 变频器", Protocol: "modbus-tcp", Endpoint: "127.0.0.1:1502",
		Config: `{"unitId":1,"pollIntervalMs":1000}`, Enabled: false, // disabled：不让 worker 真连
	})
	if err != nil {
		t.Fatalf("save device: %v", err)
	}
	if d.DeviceNo == "" {
		t.Fatal("device_no 应自动生成")
	}

	// 测点 CRUD（scale=10：工程量 = raw/10）
	p, err := svc.SavePoint(ctx, d.ID, 0, PointInput{
		Name: "输出频率", Address: "hr:40001", Unit: "Hz", RW: RWBoth,
		DataType: TypeFloat, Scale: 10, Deadband: 0.1, Enabled: true,
	})
	if err != nil {
		t.Fatalf("save point: %v", err)
	}

	// 采集落库：42.7Hz → raw 427 落库
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

	// 死区：偏差 0.05 ≤ 0.1 → 只刷 last_*，不落新行
	svc.onReading(ctx, d, p.Address, driver.Value{Num: 427.5}, time.Second)
	if err := gdb.Model(&Reading{}).Where("point_id = ?", int64(p.ID)).Count(&readings).Error; err != nil {
		t.Fatal(err)
	}
	if readings != 1 {
		t.Fatalf("deadband 应挡住落库，readings = %d", readings)
	}

	// 告警评估：42.7 > 40 触发；冷却期内再触发不落第二事件
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
		t.Fatalf("alarm events = %d, want 1", events)
	}
	svc.evaluateAlarms(ctx, d, p, pointTick{PointID: p.ID, Num: float64ptr(45), TS: time.Now().UTC()})
	if err := gdb.Model(&AlarmEvent{}).Where("point_id = ?", int64(p.ID)).Count(&events).Error; err != nil {
		t.Fatal(err)
	}
	if events != 1 {
		t.Fatalf("冷却期内不应重复告警，events = %d", events)
	}

	// 指令：只允许 rw 测点；只读测点拒绝
	if _, err := svc.CreateCommand(ctx, CommandInput{
		DeviceNo: d.DeviceNo, PointID: p.ID, Value: 35, OperatorName: "tester",
	}); err != nil {
		t.Fatalf("create command: %v", err)
	}
	ro, err := svc.SavePoint(ctx, d.ID, 0, PointInput{
		Name: "母线电压", Address: "ir:30001", RW: RWRead, DataType: TypeUint16, Enabled: true,
	})
	if err != nil {
		t.Fatalf("save readonly point: %v", err)
	}
	if _, err := svc.CreateCommand(ctx, CommandInput{
		DeviceNo: d.DeviceNo, PointID: ro.ID, Value: 1, OperatorName: "tester",
	}); err == nil {
		t.Fatal("只读测点的指令应被拒绝")
	}

	// 聚合行与告警规则是「只有 point_id、没有 device_id」的两类子表：
	// 级联删除必须顺着测点找，否则 SQL 直接报 Unknown column（2026-11-04 真机跑出来的）。
	if err := gdb.Create(&ReadingAgg{PointID: p.ID, Bucket: time.Now().UTC().Truncate(time.Hour),
		NumMin: 42.7, NumMax: 42.7, NumAvg: 42.7, Cnt: 1}).Error; err != nil {
		t.Fatalf("seed reading agg: %v", err)
	}
	if _, err := svc.CreateCommand(ctx, CommandInput{
		DeviceNo: d.DeviceNo, PointID: p.ID, Value: 50, OperatorName: "tester",
	}); err != nil {
		t.Fatalf("create command: %v", err)
	}

	// 删测点：测点与它的告警规则一起走，历史读数按设计保留（趋势不因删测点而丢）。
	p2, err := svc.SavePoint(ctx, d.ID, 0, PointInput{
		Name: "备用频率", Address: "hr:40002", RW: RWRead, DataType: TypeFloat, Enabled: true,
	})
	if err != nil {
		t.Fatalf("save second point: %v", err)
	}
	if _, err := svc.SaveAlarmRule(ctx, 0, AlarmRuleInput{
		PointID: p2.ID, Name: "备用超限", Op: "gt", Threshold: 10, Level: LevelWarn, Enabled: true, CooldownSeconds: 60,
	}); err != nil {
		t.Fatalf("save alarm rule for second point: %v", err)
	}
	svc.onReading(ctx, d, p2.Address, driver.Value{Num: 99}, time.Second)
	if err := svc.DeletePoint(ctx, d.ID, p2.ID); err != nil {
		t.Fatalf("delete point: %v", err)
	}
	var left int64
	if err := gdb.Model(&Point{}).Where("id = ?", int64(p2.ID)).Count(&left).Error; err != nil {
		t.Fatal(err)
	}
	if left != 0 {
		t.Error("测点没删掉")
	}
	if err := gdb.Model(&AlarmRule{}).Where("point_id = ?", int64(p2.ID)).Count(&left).Error; err != nil {
		t.Fatal(err)
	}
	if left != 0 {
		t.Errorf("删测点后残留 %d 条指向已删测点的告警规则", left)
	}
	if err := gdb.Model(&Reading{}).Where("point_id = ?", int64(p2.ID)).Count(&left).Error; err != nil {
		t.Fatal(err)
	}
	if left == 0 {
		t.Error("测点历史读数应保留（趋势数据不因删测点而丢）")
	}

	// 级联删除：设备删掉后测点/读数/聚合/指令/告警全清。
	// 谓词按各表实际拥有的列写：聚合行与告警规则没有 device_id 列，写 device_id 会让用例自己报 1054。
	if err := svc.DeleteDevice(ctx, d.ID); err != nil {
		t.Fatalf("delete device: %v", err)
	}
	for _, tc := range []struct {
		table, where string
		args         []any
	}{
		{"plg_devhub_point", "device_id = ?", []any{int64(d.ID)}},
		{"plg_devhub_reading", "device_id = ? OR point_id IN (?, ?)", []any{int64(d.ID), int64(p.ID), int64(p2.ID)}},
		{"plg_devhub_reading_agg", "point_id = ?", []any{int64(p.ID)}},
		{"plg_devhub_alarm_rule", "point_id IN (?, ?)", []any{int64(p.ID), int64(p2.ID)}},
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

func float64ptr(v float64) *float64 { return &v }
