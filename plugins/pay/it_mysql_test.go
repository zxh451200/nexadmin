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
	"strings"
	"sync"
	"testing"
	"time"

	"gorm.io/driver/mysql"
	"gorm.io/gorm"

	"nexadmin/core/event"
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
	defer func() { _ = admin.Close() }()
	if err := admin.Ping(); err != nil {
		t.Fatalf("mysql not reachable: %v", err)
	}

	dbName := fmt.Sprintf("nexadmin_payit_%d", time.Now().UnixNano())
	if _, err := admin.Exec("CREATE DATABASE `" + dbName + "` CHARACTER SET utf8mb4"); err != nil {
		t.Fatalf("create throwaway db: %v", err)
	}
	t.Cleanup(func() {
		_, _ = admin.Exec("DROP DATABASE IF EXISTS `" + dbName + "`")
	})

	gormDSN := strings.Replace(dsn, ")/", ")/"+dbName+"", 1)
	gdb, err := gorm.Open(mysql.Open(gormDSN), &gorm.Config{})
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
