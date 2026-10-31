package pay

// 真实 MySQL 集成测试（01 号方案验收门禁第 5/6 条的实库部分）。
//
// 守卫：默认跳过；设置 NEXADMIN_IT_MYSQL_DSN 才执行，例如
//
//	NEXADMIN_IT_MYSQL_DSN='root@tcp(127.0.0.1:3306)/' go test ./plugins/pay -run TestITMySQL -v
//
// 隔离：测试自建一次性数据库 nexadmin_payit_<纳秒>，实跑本插件与 crm/aigateway 的
// mysql 迁移 SQL（验证 DDL 在真实引擎上可执行），跑完连接池 >1 的并发用例后整库 DROP，
// 不触碰任何既有数据库。

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"gorm.io/driver/mysql"
	"gorm.io/gorm"

	"nexadmin/core/event"
	"nexadmin/core/plugin"
	"nexadmin/core/response"
	"nexadmin/core/tenant"
)

func TestITMySQLMigrationsAndConcurrency(t *testing.T) {
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
	if !strings.Contains(dsn, "parseTime=true") && !strings.Contains(dsn, "parseTime=True") {
		dsn += "&parseTime=true&loc=UTC"
	}

	admin, err := sql.Open("mysql", dsn)
	if err != nil {
		t.Fatalf("open admin conn: %v", err)
	}
	if err := admin.Ping(); err != nil {
		t.Fatalf("mysql not reachable: %v", err)
	}

	dbName := fmt.Sprintf("nexadmin_payit_%d", time.Now().UnixNano())
	if _, err := admin.Exec("CREATE DATABASE `" + dbName + "` CHARACTER SET utf8mb4"); err != nil {
		t.Fatalf("create throwaway db: %v", err)
	}
	var gdb *gorm.DB
	// 收尾顺序：关业务连接池 → 删库 → 关管理连接。admin.Close() 放 defer 会先于 t.Cleanup 执行，
	// DROP 报 "sql: database is closed" 且错误被吞掉，每跑一次就在实例上留一个临时库。
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
	sqlDB, err := gdb.DB()
	if err != nil {
		t.Fatal(err)
	}
	sqlDB.SetMaxOpenConns(8) // 池 > 1：门禁第 6 条要求连接池大于 1 才算有效仪器

	// 1) 实跑三个插件的 mysql 迁移（真实引擎执行 DDL）。
	for _, plugin := range []string{"pay", "crm", "aigateway"} {
		runPluginMigrations(t, "../"+plugin+"/migrations/mysql", gdb)
	}

	ctx := tenant.With(context.Background(), 7)
	s := NewService(gdb, nopLogger{})

	// 渠道：真实加密（secret 默认未初始化时退化为明文，链路一致）。
	if _, err := s.UpsertChannel(ctx, SaveChannelInput{
		Code: "mock", Name: "模拟渠道", Provider: "mock", Enabled: true,
		Config: map[string]string{"secret": "it-secret"},
	}); err != nil {
		t.Fatal(err)
	}

	// 2) 下单 → 回调翻转 → 出站箱同事务落库。
	o, _, err := s.Create(ctx, CreateInput{
		BizType: "trade_group", BizNo: "IT-1", AmountMinor: 1000, Currency: "CNY", Subject: "it",
	})
	if err != nil {
		t.Fatal(err)
	}
	body := []byte(`{"paymentNo":"` + o.PaymentNo + `","result":"success","channelTxnId":"it_txn_1","timestamp":` + fmt.Sprint(time.Now().Unix()) + `}`)
	ts := time.Now().Unix()
	if _, err := s.HandleCallback(ctx, CallbackInput{
		Channel: "mock", Body: body,
		Headers: map[string]string{"x-timestamp": formatID(ts), "x-signature": SignMockCallback("it-secret", body, ts)},
	}); err != nil {
		t.Fatal(err)
	}
	var outboxCount int64
	if err := gdb.WithContext(ctx).Model(&Outbox{}).
		Where("event_name = ?", EventSucceeded).Count(&outboxCount).Error; err != nil {
		t.Fatal(err)
	}
	if outboxCount != 1 {
		t.Fatalf("outbox rows for pay.succeeded = %d, want 1", outboxCount)
	}

	// 3) 并发退款（10 路 × 300，池>1）：恰好 3 笔成功（≤1000），其余 409，无超退。
	const goroutines, each = 10, 300
	var wg sync.WaitGroup
	var mu sync.Mutex
	succeeded, bounded := 0, 0
	var errTexts []string
	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			_, err := s.CreateRefund(ctx, RefundInput{
				PaymentNo: o.PaymentNo, BizType: "it_refund", BizNo: fmt.Sprintf("R-%d", n),
				AmountMinor: each, Reason: "it",
			})
			mu.Lock()
			defer mu.Unlock()
			if err == nil {
				succeeded++
				return
			}
			var re *response.Error
			if errors.As(err, &re) && (re.Key == errRefundExceeds || re.Status == 409) {
				bounded++
			}
			errTexts = append(errTexts, fmt.Sprintf("R-%d: %v", n, err))
		}(i)
	}
	wg.Wait()
	if succeeded != 3 || bounded != goroutines-3 {
		t.Fatalf("concurrent refunds: succeeded=%d bounded=%d, want 3/%d; errors: %s",
			succeeded, bounded, goroutines-3, strings.Join(errTexts, " | "))
	}
	var refunded int64
	if err := gdb.WithContext(ctx).Model(&Order{}).Select("refunded_minor").
		Where("payment_no = ?", o.PaymentNo).Scan(&refunded).Error; err != nil {
		t.Fatal(err)
	}
	if refunded != 900 {
		t.Fatalf("refunded_minor = %d, want 900 (Σrefund ≤ amount 不变式)", refunded)
	}

	// 4) 并发重复回调：只翻一次、出站箱只多一行不重复发事件。
	events := make(chan event.Event, 8)
	event.Subscribe(event.SubscriberFunc(func(e event.Event) {
		if e.Name == EventSucceeded {
			events <- e
		}
	}))
	var before int64
	if err := gdb.WithContext(ctx).Model(&Outbox{}).Count(&before).Error; err != nil {
		t.Fatal(err)
	}
	o2, _, err := s.Create(ctx, CreateInput{
		BizType: "trade_group", BizNo: "IT-2", AmountMinor: 500, Currency: "CNY", Subject: "it2",
	})
	if err != nil {
		t.Fatal(err)
	}
	var wg2 sync.WaitGroup
	for i := 0; i < 6; i++ {
		wg2.Add(1)
		go func() {
			defer wg2.Done()
			ts := time.Now().Unix()
			body := []byte(`{"paymentNo":"` + o2.PaymentNo + `","result":"success","channelTxnId":"it_txn_2","timestamp":` + fmt.Sprint(ts) + `}`)
			_, _ = s.HandleCallback(ctx, CallbackInput{
				Channel: "mock", Body: body,
				Headers: map[string]string{"x-timestamp": formatID(ts), "x-signature": SignMockCallback("it-secret", body, ts)},
			})
		}()
	}
	wg2.Wait()
	var after int64
	if err := gdb.WithContext(ctx).Model(&Outbox{}).Count(&after).Error; err != nil {
		t.Fatal(err)
	}
	if after-before != 1 {
		t.Fatalf("concurrent duplicate callbacks produced %d outbox rows, want exactly 1", after-before)
	}
}

// runPluginMigrations 按文件名顺序执行一个引擎目录下的全部 SQL（与 core/migrate 的执行语义一致）。
func runPluginMigrations(t *testing.T, dir string, db *gorm.DB) {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read %s: %v", dir, err)
	}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".sql") {
			continue
		}
		raw, err := os.ReadFile(dir + "/" + e.Name())
		if err != nil {
			t.Fatalf("read %s: %v", e.Name(), err)
		}
		if err := db.Exec(string(raw)).Error; err != nil {
			t.Fatalf("exec %s/%s: %v", dir, e.Name(), err)
		}
	}
}

// TestITMySQLMigrateBaselineMixed 是 04 号灰度方案 G1 编排的预演：
// 模拟 mall 改名后的库（plg_payment_* → plg_pay_*，带旧列集），再用真实的
// core/plugin.RunMigrations 跑 pay 的迁移——验证 runner 的基线语义在真实引擎上
// 是"按语句"粒度：改名过的 4 张表整组跳过，pay 独有的 9 张表正常创建，
// 迁移记录入库且可重复执行。这决定 G1 是"先改名后装插件"还是"先装插件后改名"。
func TestITMySQLMigrateBaselineMixed(t *testing.T) {
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
	if !strings.Contains(dsn, "parseTime=true") && !strings.Contains(dsn, "parseTime=True") {
		dsn += "&parseTime=true&loc=UTC"
	}
	admin, err := sql.Open("mysql", dsn)
	if err != nil {
		t.Fatalf("open admin conn: %v", err)
	}
	if err := admin.Ping(); err != nil {
		t.Fatalf("mysql not reachable: %v", err)
	}
	dbName := fmt.Sprintf("nexadmin_payit_%d", time.Now().UnixNano())
	if _, err := admin.Exec("CREATE DATABASE `" + dbName + "` CHARACTER SET utf8mb4"); err != nil {
		t.Fatalf("create throwaway db: %v", err)
	}
	var gdb *gorm.DB
	// 收尾顺序同 TestITMySQLPayMigration：先关业务连接池，再删库，最后关管理连接。
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
		t.Fatal(err)
	}

	// 1) 模拟 mall 改名后的四张表：沿用 mall 的旧列集（channel、无 subject_id/fee_minor 等）。
	legacy := []string{
		`CREATE TABLE plg_pay_order (
		  id BIGINT NOT NULL, tenant_id BIGINT NOT NULL DEFAULT 0,
		  payment_no VARCHAR(40) NOT NULL, biz_type VARCHAR(32) NOT NULL, biz_no VARCHAR(64) NOT NULL,
		  subject VARCHAR(200) NOT NULL, amount_minor BIGINT NOT NULL, refunded_minor BIGINT NOT NULL DEFAULT 0,
		  currency VARCHAR(8) NOT NULL, channel VARCHAR(32) NOT NULL, channel_txn_id VARCHAR(128) NOT NULL DEFAULT '',
		  status VARCHAR(24) NOT NULL, fail_reason VARCHAR(200) NOT NULL DEFAULT '',
		  paid_at DATETIME(3) NULL, expire_at DATETIME(3) NULL,
		  created_at DATETIME(3) NOT NULL, updated_at DATETIME(3) NOT NULL, PRIMARY KEY (id))`,
		`CREATE TABLE plg_pay_channel (
		  id BIGINT NOT NULL, tenant_id BIGINT NOT NULL DEFAULT 0,
		  code VARCHAR(32) NOT NULL, name VARCHAR(64) NOT NULL, enabled TINYINT(1) NOT NULL DEFAULT 1,
		  sort INT NOT NULL DEFAULT 0, config_json VARCHAR(512) NOT NULL DEFAULT '',
		  created_at DATETIME(3) NOT NULL, updated_at DATETIME(3) NOT NULL, PRIMARY KEY (id))`,
		`CREATE TABLE plg_pay_callback (
		  id BIGINT NOT NULL, tenant_id BIGINT NOT NULL DEFAULT 0,
		  channel VARCHAR(32) NOT NULL, payment_no VARCHAR(40) NOT NULL DEFAULT '',
		  ok TINYINT(1) NOT NULL DEFAULT 0, result VARCHAR(200) NOT NULL DEFAULT '', payload TEXT,
		  created_at DATETIME(3) NOT NULL, updated_at DATETIME(3) NOT NULL, PRIMARY KEY (id))`,
		`CREATE TABLE plg_pay_refund (
		  id BIGINT NOT NULL, tenant_id BIGINT NOT NULL DEFAULT 0,
		  refund_no VARCHAR(40) NOT NULL, payment_no VARCHAR(40) NOT NULL,
		  biz_type VARCHAR(32) NOT NULL, biz_no VARCHAR(64) NOT NULL,
		  amount_minor BIGINT NOT NULL, currency VARCHAR(8) NOT NULL,
		  reason VARCHAR(200) NOT NULL DEFAULT '', status VARCHAR(16) NOT NULL,
		  channel_refund_id VARCHAR(128) NOT NULL DEFAULT '', succeeded_at DATETIME(3) NULL,
		  operator VARCHAR(64) NOT NULL DEFAULT '',
		  created_at DATETIME(3) NOT NULL, updated_at DATETIME(3) NOT NULL, PRIMARY KEY (id))`,
	}
	for _, stmt := range legacy {
		if err := gdb.Exec(stmt).Error; err != nil {
			t.Fatalf("create legacy table: %v", err)
		}
	}

	// 2) 用真实迁移运行器跑 pay 插件（走 plugins/pay/migrations/mysql + 清单）。
	repoRoot, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	if err := plugin.RunMigrations(gdb, repoRoot, "pay"); err != nil {
		t.Fatalf("RunMigrations: %v", err)
	}

	// 3) 断言混合结果：改名表未被 DDL 触碰（旧列仍在、新列未加），pay 独有 9 表已创建。
	hasCol := func(table, column string) bool {
		var n int64
		if err := gdb.Raw("SELECT COUNT(*) FROM information_schema.COLUMNS WHERE table_schema = ? AND table_name = ? AND column_name = ?",
			dbName, table, column).Scan(&n).Error; err != nil {
			t.Fatal(err)
		}
		return n > 0
	}
	hasTable := func(table string) bool {
		var n int64
		if err := gdb.Raw("SELECT COUNT(*) FROM information_schema.TABLES WHERE table_schema = ? AND table_name = ?",
			dbName, table).Scan(&n).Error; err != nil {
			t.Fatal(err)
		}
		return n > 0
	}
	if !hasCol("plg_pay_order", "channel") || hasCol("plg_pay_order", "subject_id") {
		t.Fatal("renamed plg_pay_order must keep legacy columns and gain none from 001")
	}
	for _, tb := range []string{"plg_pay_auth", "plg_pay_reconcile_task", "plg_pay_commission_rule", "plg_pay_settlement", "plg_pay_payout_batch", "plg_pay_outbox", "plg_pay_idem"} {
		if !hasTable(tb) {
			t.Fatalf("pay-only table %s should have been created", tb)
		}
	}
	// 迁移记录入库（scope plugin:pay，001 已应用；sys_migration 的文件列叫 version）。
	var rec int64
	if err := gdb.Raw("SELECT COUNT(*) FROM sys_migration WHERE scope = ? AND version = ?", "plugin:pay", "001_init.sql").Scan(&rec).Error; err != nil {
		t.Fatal(err)
	}
	if rec != 1 {
		t.Fatalf("sys_migration rows for plugin:pay/001_init.sql = %d, want 1", rec)
	}

	// 4) 幂等：再跑一遍零变更。
	if err := plugin.RunMigrations(gdb, repoRoot, "pay"); err != nil {
		t.Fatalf("re-run RunMigrations: %v", err)
	}
}
