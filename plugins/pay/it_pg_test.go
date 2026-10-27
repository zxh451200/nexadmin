package pay

// 真实 PostgreSQL 集成测试（与 it_mysql_test.go 同一套门禁语义，PG 方言）。
//
// 守卫：默认跳过；设置 NEXADMIN_IT_PG_DSN 才执行，例如
//
//	NEXADMIN_IT_PG_DSN='host=127.0.0.1 port=5432 user=postgres password=postgres dbname=postgres sslmode=disable' \
//	  go test ./plugins/pay -run TestITPostgres -v
//
// 隔离：自建一次性数据库（CREATE DATABASE + DROP WITH (FORCE)），实跑 pay/crm/aigateway
// 的 postgres 迁移 SQL（简单协议允许一次 Exec 多语句），跑完并发用例后整库删除。
// 迁移一致性门禁只做结构比对，本测试负责"PG 方言真的能在引擎上执行"这一层。

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

	gormpg "gorm.io/driver/postgres"
	"gorm.io/gorm"

	"nexadmin/core/event"
	"nexadmin/core/response"
	"nexadmin/core/tenant"
)

func TestITPostgresMigrationsAndConcurrency(t *testing.T) {
	dsn := os.Getenv("NEXADMIN_IT_PG_DSN")
	if dsn == "" {
		t.Skip("NEXADMIN_IT_PG_DSN not set; skipping real-PostgreSQL integration test")
	}

	admin, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("open admin conn: %v", err)
	}
	defer func() { _ = admin.Close() }()
	if err := admin.Ping(); err != nil {
		t.Fatalf("postgres not reachable: %v", err)
	}

	dbName := fmt.Sprintf("nexadmin_payit_%d", time.Now().UnixNano())
	if _, err := admin.Exec(`CREATE DATABASE ` + quoteIdent(dbName)); err != nil {
		t.Fatalf("create throwaway db: %v", err)
	}
	t.Cleanup(func() {
		_, _ = admin.Exec(`DROP DATABASE ` + quoteIdent(dbName) + ` WITH (FORCE)`)
	})

	gormDSN := appendDBName(dsn, dbName)
	gdb, err := gorm.Open(gormpg.New(gormpg.Config{DSN: gormDSN, PreferSimpleProtocol: true}), &gorm.Config{})
	if err != nil {
		t.Fatalf("open gorm: %v", err)
	}
	sqlDB, err := gdb.DB()
	if err != nil {
		t.Fatal(err)
	}
	sqlDB.SetMaxOpenConns(8)

	for _, plugin := range []string{"pay", "crm", "aigateway"} {
		runPluginMigrations(t, "../"+plugin+"/migrations/postgres", gdb)
	}

	ctx := tenant.With(context.Background(), 7)
	s := NewService(gdb, nopLogger{})

	if _, err := s.UpsertChannel(ctx, SaveChannelInput{
		Code: "mock", Name: "模拟渠道", Provider: "mock", Enabled: true,
		Config: map[string]string{"secret": "it-secret"},
	}); err != nil {
		t.Fatal(err)
	}

	o, _, err := s.Create(ctx, CreateInput{
		BizType: "trade_group", BizNo: "IT-1", AmountMinor: 1000, Currency: "CNY", Subject: "it",
	})
	if err != nil {
		t.Fatal(err)
	}
	ts := time.Now().Unix()
	body := []byte(`{"paymentNo":"` + o.PaymentNo + `","result":"success","channelTxnId":"it_txn_1","timestamp":` + fmt.Sprint(ts) + `}`)
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
		t.Fatalf("refunded_minor = %d, want 900", refunded)
	}

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

// quoteIdent 安全引用 PG 标识符（库名由测试自拼，无注入面，仍按规范引用）。
func quoteIdent(name string) string {
	return `"` + strings.ReplaceAll(name, `"`, `""`) + `"`
}

// appendDBName 把 dbname 追加进 key=value 形式的 DSN（覆盖已有 dbname）。
func appendDBName(dsn, dbName string) string {
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
