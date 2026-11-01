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
	"nexadmin/pkg/idgen"
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
			// 只观察不消费：必须非阻塞，否则缓冲写满后总线的分发协程会卡死在这条
			// 订阅上，把同一个进程里后面的用例（service_test 等真的收事件的）全拖黄。
			select {
			case events <- e:
			default:
			}
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

// ==================== 资金闸门 IT（两引擎共用夹具） ====================
//
// 单测层的 sqlmock 从不执行 SQL、不认 FOR UPDATE / 条件更新的 RowsAffected，
// 本批次新增的几道资金闸门（同键预授权只冻结一次、代付逐笔认领、退款补查节流、
// 回调通知号取证、出站箱死信只转一次）在单测里是零覆盖。下面的用例把它们放到
// 真实引擎上跑，夹具沿用本文件既有做法：一次性库 + 真实迁移 DDL + 连接池 >1。
//
// 方言差异（列/索引探针）由各引擎文件注入 itDDLProbe，共用用例里不写 if 分支。

// TestITMySQLFundGates 在真实 MySQL 8 上跑五条资金闸门用例。
//
// 守卫同本文件既有用例：未设 NEXADMIN_IT_MYSQL_DSN 直接跳过；一次性库跑完即 DROP。
func TestITMySQLFundGates(t *testing.T) {
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
	// 收尾顺序同既有用例：先关业务连接池 → 删库 → 关管理连接。
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
	sqlDB.SetMaxOpenConns(8) // 池 > 1：并发闸门只有在池>1 时才可能真的互相抢

	for _, pl := range []string{"pay", "crm", "aigateway"} {
		runPluginMigrations(t, "../"+pl+"/migrations/mysql", gdb)
	}

	ctx := tenant.With(context.Background(), 7)
	itRunFundGates(t, gdb, ctx, itDDLProbe{
		hasColumn: func(table, column string) bool {
			var n int64
			err := gdb.Raw(`SELECT COUNT(*) FROM information_schema.COLUMNS WHERE table_schema = ? AND table_name = ? AND column_name = ?`,
				dbName, table, column).Scan(&n).Error
			if err != nil {
				t.Fatalf("probe column %s.%s: %v", table, column, err)
			}
			return n > 0
		},
		hasIndex: func(table, index string) bool {
			var n int64
			err := gdb.Raw(`SELECT COUNT(*) FROM information_schema.STATISTICS WHERE table_schema = ? AND table_name = ? AND index_name = ?`,
				dbName, table, index).Scan(&n).Error
			if err != nil {
				t.Fatalf("probe index %s.%s: %v", table, index, err)
			}
			return n > 0
		},
	})
}

// itDDLProbe 是引擎侧的 DDL 取证探针（002 迁移新增的列与索引是否真在库里）。
type itDDLProbe struct {
	hasColumn func(table, column string) bool
	hasIndex  func(table, index string) bool
}

// itGateProvider 是资金闸门用例的渠道桩：除被计数的渠道动作（Freeze/Transfer/
// QueryRefund）外全部委托 mockProvider，链路语义与真渠道一致。
//
// 计数器必须带锁——本文件的存在意义就是并发；latency 用来把两次渠道调用拉开，
// 保证第二个实例真的在第一个实例调用渠道的途中扫到同一行（池=1 时不会发生）。
type itGateProvider struct {
	mockProvider
	name    string
	latency time.Duration

	mu              sync.Mutex
	freezeCalls     int
	transferCalls   map[string]int
	refundQueryCall int
}

func newITGateProvider(name string, latency time.Duration) *itGateProvider {
	p := &itGateProvider{name: name, latency: latency, transferCalls: map[string]int{}}
	RegisterProvider(p)
	return p
}

func (p *itGateProvider) Name() string { return p.name }

func (p *itGateProvider) wait() {
	if p.latency > 0 {
		time.Sleep(p.latency)
	}
}

func (p *itGateProvider) Freeze(_ context.Context, in AuthFreezeRequest, _ map[string]string) (*AuthResult, error) {
	p.mu.Lock()
	p.freezeCalls++
	p.mu.Unlock()
	p.wait()
	return &AuthResult{ChannelAuthNo: "itca_" + in.AuthNo, Status: "success", AmountMinor: in.AmountMinor}, nil
}

func (p *itGateProvider) Transfer(_ context.Context, in TransferRequest, _ map[string]string) (*TransferResult, error) {
	p.mu.Lock()
	if p.transferCalls == nil {
		p.transferCalls = map[string]int{}
	}
	p.transferCalls[in.ItemNo]++
	p.mu.Unlock()
	p.wait()
	return &TransferResult{
		Accepted: true, State: "SUCCESS",
		ChannelBatchNo: "itbt_" + in.BatchNo, ChannelDetailNo: "itdt_" + in.ItemNo,
	}, nil
}

// QueryRefund 永远回「查无此退款」：退款保持 pending，只留下节流与计数。
func (p *itGateProvider) QueryRefund(_ context.Context, _ RefundQueryRequest, _ map[string]string) (*RefundStatusResult, error) {
	p.mu.Lock()
	p.refundQueryCall++
	p.mu.Unlock()
	p.wait()
	return &RefundStatusResult{Found: false, Reason: "it: 渠道查无这笔退款"}, nil
}

func (p *itGateProvider) snapshot() (freeze int, transfers map[string]int, transferTotal int, refundQuery int) {
	p.mu.Lock()
	defer p.mu.Unlock()
	out := make(map[string]int, len(p.transferCalls))
	for k, v := range p.transferCalls {
		out[k] = v
		transferTotal += v
	}
	return p.freezeCalls, out, transferTotal, p.refundQueryCall
}

// itWarnLogger 按消息文本（并按 eventId 细分）计 Warn 条数（并发安全）：
// 死信「只迁移一次」这种事实只有留痕条数能证明——末次写入的 dead_at 看不出
// 有几个实例尝试过，抢输的一方 UPDATE 影响 0 行、库里长得一模一样。
type itWarnLogger struct {
	mu      sync.Mutex
	byMsg   map[string]int
	byEvent map[string]int
}

func (l *itWarnLogger) Info(string, ...any)  {}
func (l *itWarnLogger) Error(string, ...any) {}

func (l *itWarnLogger) Warn(msg string, kv ...any) {
	eventID := ""
	for i := 0; i+1 < len(kv); i += 2 {
		if k, ok := kv[i].(string); ok && k == "eventId" {
			if v, ok := kv[i+1].(string); ok {
				eventID = v
			}
		}
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.byMsg == nil {
		l.byMsg = map[string]int{}
	}
	l.byMsg[msg]++
	if eventID != "" {
		if l.byEvent == nil {
			l.byEvent = map[string]int{}
		}
		l.byEvent[msg+"\x00"+eventID]++
	}
}

func (l *itWarnLogger) count(msg string) int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.byMsg[msg]
}

func (l *itWarnLogger) countEvent(msg, eventID string) int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.byEvent[msg+"\x00"+eventID]
}

// itRunFundGates 跑五条闸门；两个引擎文件各自建库后调用。
func itRunFundGates(t *testing.T, gdb *gorm.DB, ctx context.Context, ddl itDDLProbe) {
	t.Run("ConcurrentAuthFreezesOnce", func(t *testing.T) { itGateConcurrentAuth(t, gdb, ctx, ddl) })
	t.Run("ConcurrentPayoutClaimsEachItemOnce", func(t *testing.T) { itGateConcurrentPayout(t, gdb, ctx) })
	t.Run("RefundSyncThrottleIsPersisted", func(t *testing.T) { itGateRefundSyncThrottle(t, gdb, ctx) })
	t.Run("CallbackNotifyIDForensics", func(t *testing.T) { itGateCallbackNotifyID(t, gdb, ctx, ddl) })
	t.Run("OutboxDeadLetterTransitionsOnce", func(t *testing.T) { itGateOutboxDeadLetter(t, gdb, ctx) })
}

// 闸门 1：并发同 (biz_type,biz_no) 预授权只向渠道冻结一次。
// 钉住 auths.go CreateAuth 的 `if !claimed { return a, nil }` 与 claimAuth 的
// 「先落 authorizing 行、抢到才打渠道」；uk_pay_auth_biz 由末尾的重复键探针单独取证。
func itGateConcurrentAuth(t *testing.T, gdb *gorm.DB, ctx context.Context, ddl itDDLProbe) {
	prov := newITGateProvider("it-auth-gate", 120*time.Millisecond)
	s := NewService(gdb, nopLogger{})
	if _, err := s.UpsertChannel(ctx, SaveChannelInput{
		Code: "itauth", Name: "并发冻结闸门", Provider: prov.Name(), Enabled: true,
		Config: map[string]string{"secret": "it-auth-secret"},
	}); err != nil {
		t.Fatalf("upsert auth channel: %v", err)
	}

	in := CreateAuthInput{
		BizType: "it_preauth", BizNo: "PA-DUP-1", ChannelCode: "itauth",
		AmountMinor: 5000, Currency: "CNY", SubjectID: "it-subj-auth", PayerID: "it-buyer", Remark: "it",
	}
	const racers = 2
	got := make([]*Authorization, racers)
	errs := make([]error, racers)
	start := make(chan struct{})
	var wg sync.WaitGroup
	for i := 0; i < racers; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			<-start
			got[n], errs[n] = s.CreateAuth(ctx, in)
		}(i)
	}
	close(start)
	wg.Wait()
	for i := range errs {
		if errs[i] != nil {
			t.Fatalf("CreateAuth racer #%d: %v", i, errs[i])
		}
	}

	freezes, _, _, _ := prov.snapshot()
	if freezes != 1 {
		t.Fatalf("channel Freeze calls = %d, want exactly 1：同键并发把钱冻了两遍", freezes)
	}

	var rows []Authorization
	if err := gdb.WithContext(ctx).Where("biz_type = ? AND biz_no = ?", in.BizType, in.BizNo).Order("id").Find(&rows).Error; err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 {
		t.Fatalf("authorization rows for %s/%s = %d, want 1", in.BizType, in.BizNo, len(rows))
	}
	if got[0].AuthNo != rows[0].AuthNo || got[1].AuthNo != rows[0].AuthNo {
		t.Fatalf("两个并发请求拿到不同的单：racer0=%s racer1=%s 库里=%s",
			got[0].AuthNo, got[1].AuthNo, rows[0].AuthNo)
	}
	if rows[0].Status != AuthAuthorized || rows[0].ChannelAuthNo != "itca_"+rows[0].AuthNo {
		t.Fatalf("auth row status=%q channelAuthNo=%q, want authorized + 渠道回填的授权号",
			rows[0].Status, rows[0].ChannelAuthNo)
	}

	// 幂等键的真库取证：同 (tenant,biz_type,biz_no) 的第二行必须被唯一索引拒掉。
	dup := rows[0]
	dup.ID = 0
	dup.AuthNo = rows[0].AuthNo + "-dup"
	dup.CreatedAt = time.Time{}
	dup.UpdatedAt = time.Time{}
	const bizKeyIndex = "uk_pay_auth_biz"
	if err := gdb.WithContext(ctx).Create(&dup).Error; err == nil {
		t.Fatalf("%s 没拦住同键的第二行", bizKeyIndex)
	} else if !strings.Contains(strings.ToLower(err.Error()), bizKeyIndex) {
		// 按索引名取证：两引擎都把它写进错误里，且不受服务端语言（lc_messages）影响。
		t.Fatalf("同键重复应报 %s 唯一索引冲突，实际：%v", bizKeyIndex, err)
	}
	if !ddl.hasColumn("plg_pay_auth", "payer_id") {
		t.Fatal("002 迁移的 plg_pay_auth.payer_id 列不在库里（真实渠道冻结必需）")
	}
}

// 闸门 2：并发代付时每一笔只被认领一次（bill.go PayPayoutViaChannel 的条件更新）。
func itGateConcurrentPayout(t *testing.T, gdb *gorm.DB, ctx context.Context) {
	prov := newITGateProvider("it-payout-gate", 80*time.Millisecond)
	s := NewService(gdb, nopLogger{})
	if _, err := s.UpsertChannel(ctx, SaveChannelInput{
		Code: "itpayout", Name: "并发代付闸门", Provider: prov.Name(), Enabled: true,
		Config: map[string]string{"secret": "it-pay-secret"},
	}); err != nil {
		t.Fatalf("upsert payout channel: %v", err)
	}

	var settlementIDs []int64
	for i := 0; i < 3; i++ {
		st := Settlement{
			SubjectID:   fmt.Sprintf("it-pay-subj-%d", i),
			PeriodStart: "2026-11-01", PeriodEnd: "2026-11-01",
			GrossMinor: 2000, CommissionMinor: 100, NetMinor: int64(1000 + i),
			Currency: "CNY", Status: SettlementConfirmed, OrderCount: 1,
		}
		if err := gdb.WithContext(ctx).Create(&st).Error; err != nil {
			t.Fatalf("create settlement %d: %v", i, err)
		}
		settlementIDs = append(settlementIDs, int64(st.ID))
	}
	batch, err := s.CreatePayout(ctx, CreatePayoutInput{SettlementIDs: settlementIDs, Method: "channel", Note: "it gates"})
	if err != nil {
		t.Fatalf("CreatePayout: %v", err)
	}
	var items []PayoutItem
	if err := gdb.WithContext(ctx).Where("batch_id = ?", int64(batch.ID)).Order("id").Find(&items).Error; err != nil {
		t.Fatal(err)
	}
	if len(items) != 3 {
		t.Fatalf("payout items = %d, want 3", len(items))
	}

	var wg sync.WaitGroup
	errs := make([]error, 2)
	start := make(chan struct{})
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			<-start
			_, errs[n] = s.PayPayoutViaChannel(ctx, int64(batch.ID), "itpayout")
		}(i)
	}
	close(start)
	wg.Wait()
	for i := range errs {
		if errs[i] != nil {
			t.Fatalf("PayPayoutViaChannel racer #%d: %v", i, errs[i])
		}
	}

	_, transfers, total, _ := prov.snapshot()
	if total != len(items) {
		t.Fatalf("渠道 Transfer 调用总数 = %d, want %d（明细笔数）：%v", total, len(items), transfers)
	}
	for _, it := range items {
		itemNo := "PI" + formatID(int64(it.ID))
		if transfers[itemNo] != 1 {
			t.Fatalf("笔 %s（结算单 %d）被渠道发起 %d 次，want 1：认领闸门被绕过就是重复出款",
				itemNo, int64(it.SettlementID), transfers[itemNo])
		}
	}

	var final []PayoutItem
	if err := gdb.WithContext(ctx).Where("batch_id = ?", int64(batch.ID)).Order("id").Find(&final).Error; err != nil {
		t.Fatal(err)
	}
	for _, it := range final {
		if it.Status != PayoutItemSuccess {
			t.Fatalf("笔 %d 终态 %q（原因 %q），want success", int64(it.ID), it.Status, it.FailReason)
		}
	}
	// 批次状态由最后一次 finalize 决定：全部成功时聚合口径必须收敛到 paid，
	// 且收敛调用不允许再打渠道（幂等）。
	again, err := s.PayPayoutViaChannel(ctx, int64(batch.ID), "itpayout")
	if err != nil {
		t.Fatalf("收敛调用 PayPayoutViaChannel: %v", err)
	}
	if again.Status != PayoutPaid {
		t.Fatalf("batch status = %q after convergence, want paid（failReason=%q）", again.Status, again.FailReason)
	}
	if _, _, totalAfter, _ := prov.snapshot(); totalAfter != total {
		t.Fatalf("paid 批次重试又发起了 %d 笔转账（应为 0）", totalAfter-total)
	}
}

// 闸门 3：退款补查的节流必须真落库（service_refund_sync.go 的 sync_attempts/last_sync_at）。
func itGateRefundSyncThrottle(t *testing.T, gdb *gorm.DB, ctx context.Context) {
	prov := newITGateProvider("it-rsync-gate", 0)
	s := NewService(gdb, nopLogger{})
	if _, err := s.UpsertChannel(ctx, SaveChannelInput{
		Code: "itrsync", Name: "退款补查节流", Provider: prov.Name(), Enabled: true,
		Config: map[string]string{"secret": "it-rs-secret", "refundOutcome": "pending"},
	}); err != nil {
		t.Fatalf("upsert refund-sync channel: %v", err)
	}

	o, _, err := s.Create(ctx, CreateInput{
		BizType: "it_rsync", BizNo: "RS-1", ChannelCode: "itrsync",
		AmountMinor: 1000, Currency: "CNY", Subject: "it refund sync",
	})
	if err != nil {
		t.Fatalf("create order: %v", err)
	}
	ts := time.Now().Unix()
	body := []byte(`{"paymentNo":"` + o.PaymentNo + `","result":"success","channelTxnId":"it_rs_txn","timestamp":` + fmt.Sprint(ts) + `}`)
	if _, err := s.HandleCallback(ctx, CallbackInput{
		Channel: "itrsync", Body: body,
		Headers: map[string]string{"x-timestamp": formatID(ts), "x-signature": SignMockCallback("it-rs-secret", body, ts)},
	}); err != nil {
		t.Fatalf("callback: %v", err)
	}
	r, err := s.CreateRefund(ctx, RefundInput{
		PaymentNo: o.PaymentNo, BizType: "it_rsync_refund", BizNo: "RSR-1", AmountMinor: 400, Reason: "it",
	})
	if err != nil {
		t.Fatalf("create refund: %v", err)
	}
	if r.Status != RefundPending {
		t.Fatalf("refund status = %q, want pending（渠道受理待回调才会进补查路径）", r.Status)
	}

	// 把 updated_at 推到扫描窗口之外（模拟「这笔已经卡了很久」）。
	pushBack := func(at time.Time) {
		if err := gdb.Exec("UPDATE plg_pay_refund SET updated_at = ? WHERE id = ?", at, int64(r.ID)).Error; err != nil {
			t.Fatalf("push refund updated_at: %v", err)
		}
	}
	type throttleRow struct {
		SyncAttempts int
		LastSyncAt   *time.Time
	}
	readPersisted := func() throttleRow {
		var got throttleRow
		if err := gdb.Raw("SELECT sync_attempts, last_sync_at FROM plg_pay_refund WHERE id = ?", int64(r.ID)).
			Scan(&got).Error; err != nil {
			t.Fatalf("read throttle columns: %v", err)
		}
		return got
	}

	// 第 1 轮：到点，打渠道，节流列必须落库。
	now1 := time.Now().UTC()
	pushBack(now1.Add(-30 * time.Minute))
	sum1, err := s.SyncStuckRefunds(ctx, now1, 10*time.Minute, 10)
	if err != nil {
		t.Fatalf("first sync pass: %v", err)
	}
	if sum1.Checked != 1 || sum1.Flipped != 0 {
		t.Fatalf("first pass summary = %+v, want checked=1 flipped=0", sum1)
	}
	if _, _, _, calls := prov.snapshot(); calls != 1 {
		t.Fatalf("channel QueryRefund calls after pass 1 = %d, want 1", calls)
	}
	p1 := readPersisted()
	if p1.SyncAttempts != 1 {
		t.Fatalf("persisted sync_attempts = %d, want 1（节流计数没落库）", p1.SyncAttempts)
	}
	if p1.LastSyncAt == nil {
		t.Fatal("persisted last_sync_at is NULL（节流时间没落库 → 每轮都会重打渠道）")
	}

	// 第 2 轮：还在节流窗口内，一次都不许打渠道。
	// 时钟基准取库里真的落下的 last_sync_at：两引擎的时间列语义不同
	// （MySQL DATETIME(3)+loc=UTC / PG 无时区 TIMESTAMP），用库里的值才不把
	// 驱动换算差异算成闸门的失效。
	base := *p1.LastSyncAt
	pushBack(base.Add(-30 * time.Minute))
	sum2, err := s.SyncStuckRefunds(ctx, base, 10*time.Minute, 10)
	if err != nil {
		t.Fatalf("second sync pass: %v", err)
	}
	if sum2.Checked != 0 {
		_, _, _, calls := prov.snapshot()
		t.Fatalf("throttled pass checked = %d, want 0（channel calls=%d）：last_sync_at 没起作用", sum2.Checked, calls)
	}
	if _, _, _, calls := prov.snapshot(); calls != 1 {
		t.Fatalf("channel QueryRefund calls after throttled pass = %d, want still 1", calls)
	}

	// 第 3 轮：过节流窗口，必须再打一次渠道，并且节流列继续推进。
	window := base.Add(refundSyncThrottle + time.Minute)
	pushBack(window.Add(-30 * time.Minute))
	sum3, err := s.SyncStuckRefunds(ctx, window, 10*time.Minute, 10)
	if err != nil {
		t.Fatalf("third sync pass: %v", err)
	}
	if sum3.Checked != 1 {
		t.Fatalf("pass after the throttle window checked = %d, want 1", sum3.Checked)
	}
	if _, _, _, calls := prov.snapshot(); calls != 2 {
		t.Fatalf("channel QueryRefund calls after window = %d, want 2", calls)
	}
	if p3 := readPersisted(); p3.SyncAttempts != 2 || p3.LastSyncAt == nil {
		t.Fatalf("persisted throttle after pass 3 = %+v, want sync_attempts=2", p3)
	}
}

// 闸门 4：回调通知号取证列（002 新增 plg_pay_callback.notify_id + idx_pay_callback_notify）。
func itGateCallbackNotifyID(t *testing.T, gdb *gorm.DB, ctx context.Context, ddl itDDLProbe) {
	s := NewService(gdb, nopLogger{})
	if _, err := s.UpsertChannel(ctx, SaveChannelInput{
		Code: "itnotify", Name: "回调取证", Provider: "mock", Enabled: true,
		Config: map[string]string{"secret": "it-notify-secret"},
	}); err != nil {
		t.Fatalf("upsert notify channel: %v", err)
	}
	o, _, err := s.Create(ctx, CreateInput{
		BizType: "it_notify", BizNo: "NT-1", ChannelCode: "itnotify",
		AmountMinor: 700, Currency: "CNY", Subject: "it notify",
	})
	if err != nil {
		t.Fatalf("create order: %v", err)
	}
	ts := time.Now().Unix()
	body := []byte(`{"paymentNo":"` + o.PaymentNo + `","result":"success","channelTxnId":"it_notify_txn","timestamp":` + fmt.Sprint(ts) + `}`)
	for i := 0; i < 2; i++ {
		if _, err := s.HandleCallback(ctx, CallbackInput{
			Channel: "itnotify", Body: body,
			Headers: map[string]string{"x-timestamp": formatID(ts), "x-signature": SignMockCallback("it-notify-secret", body, ts)},
		}); err != nil {
			t.Fatalf("callback %d: %v", i, err)
		}
	}

	if !ddl.hasColumn("plg_pay_callback", "notify_id") {
		t.Fatal("库里没有 plg_pay_callback.notify_id（002 迁移未生效）")
	}
	if !ddl.hasIndex("plg_pay_callback", "idx_pay_callback_notify") {
		t.Fatal("库里没有 idx_pay_callback_notify 索引（取证检索会全表扫）")
	}

	type auditRow struct {
		NotifyID string
		Result   string
	}
	var rows []auditRow
	if err := gdb.Raw("SELECT notify_id, result FROM plg_pay_callback WHERE payment_no = ? ORDER BY id", o.PaymentNo).
		Scan(&rows).Error; err != nil {
		t.Fatalf("read callback audit: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("callback audit rows = %d, want 2", len(rows))
	}
	for i, r := range rows {
		if strings.TrimSpace(r.NotifyID) == "" {
			t.Fatalf("audit row %d 的 notify_id 为空：渠道通知号没落库（重复回调只翻一次是既有用例覆盖，这里钉新列）", i)
		}
	}
	if rows[0].NotifyID != rows[1].NotifyID {
		t.Fatalf("同一条通知的两次下发记下不同 notify_id：%q vs %q", rows[0].NotifyID, rows[1].NotifyID)
	}
	var byNotify int64
	if err := gdb.Table("plg_pay_callback").Where("notify_id = ?", rows[0].NotifyID).Count(&byNotify).Error; err != nil {
		t.Fatalf("query by notify_id: %v", err)
	}
	if byNotify != 2 {
		t.Fatalf("按 notify_id 检索到 %d 行, want 2（渠道重试取证检索）", byNotify)
	}

	var status string
	if err := gdb.Raw("SELECT status FROM plg_pay_order WHERE payment_no = ?", o.PaymentNo).Scan(&status).Error; err != nil {
		t.Fatal(err)
	}
	if status != StatusSucceeded {
		t.Fatalf("order status = %q, want succeeded", status)
	}
	var outbox int64
	if err := gdb.WithContext(ctx).Model(&Outbox{}).
		Where("event_name = ? AND payload LIKE ?", EventSucceeded, `%`+o.PaymentNo+`%`).Count(&outbox).Error; err != nil {
		t.Fatal(err)
	}
	if outbox != 1 {
		t.Fatalf("重复回调产生 %d 行出站箱, want exactly 1", outbox)
	}
}

// 闸门 5：出站箱死信不静默（outbox.go flushOutbox 的抢占 + markOutboxDead）。
func itGateOutboxDeadLetter(t *testing.T, gdb *gorm.DB, ctx context.Context) {
	lg := &itWarnLogger{}
	s := NewService(gdb, lg)
	if _, err := s.UpsertChannel(ctx, SaveChannelInput{
		Code: "itdead", Name: "死信闸门", Provider: "mock", Enabled: true,
		Config: map[string]string{"secret": "it-dead-secret"},
	}); err != nil {
		t.Fatalf("upsert dead-letter channel: %v", err)
	}
	o, _, err := s.Create(ctx, CreateInput{
		BizType: "it_dead", BizNo: "DL-1", ChannelCode: "itdead",
		AmountMinor: 600, Currency: "CNY", Subject: "it dead letter",
	})
	if err != nil {
		t.Fatalf("create order: %v", err)
	}
	ts := time.Now().Unix()
	body := []byte(`{"paymentNo":"` + o.PaymentNo + `","result":"success","channelTxnId":"it_dead_txn","timestamp":` + fmt.Sprint(ts) + `}`)
	if _, err := s.HandleCallback(ctx, CallbackInput{
		Channel: "itdead", Body: body,
		Headers: map[string]string{"x-timestamp": formatID(ts), "x-signature": SignMockCallback("it-dead-secret", body, ts)},
	}); err != nil {
		t.Fatalf("callback: %v", err)
	}

	var row Outbox
	if err := gdb.WithContext(ctx).Where("event_name = ? AND payload LIKE ?", EventSucceeded, `%`+o.PaymentNo+`%`).
		Order("id desc").Take(&row).Error; err != nil {
		t.Fatalf("load outbox row: %v", err)
	}
	// 把这条真出站箱改成毒丸：载荷不可解析 + 退回 pending + 立即可投。
	poisonPayload := `{"eventName": 123, "data": "不是对象"}`
	past := time.Now().UTC().Add(-time.Hour)
	if err := gdb.Exec("UPDATE plg_pay_outbox SET status = ?, payload = ?, attempts = 0, next_retry_at = ? WHERE id = ?",
		OutboxPending, poisonPayload, past, int64(row.ID)).Error; err != nil {
		t.Fatalf("poison outbox payload: %v", err)
	}

	// 在同一条之前插一段积压毒丸（id 更小 → flushOutbox 的 Order("id") 先处理它们）：
	// 「两个实例都扫到这一行」需要真实的时间窗，否则一方一口气跑完，
	// 另一方连 pending 都看不到，删掉租约抢占也是全绿（本文件的存在理由）。
	const backlog = 80
	for i := 0; i < backlog; i++ {
		fill := Outbox{
			EventID: fmt.Sprintf("itdead-fill-%d", i), EventName: EventSucceeded,
			Payload: poisonPayload, Status: OutboxPending, NextRetryAt: past,
		}
		fill.ID = idgen.ID(int64(row.ID) - int64(i) - 1)
		fill.TenantID = row.TenantID
		if err := gdb.WithContext(ctx).Create(&fill).Error; err != nil {
			t.Fatalf("insert backlog row %d: %v", i, err)
		}
	}

	// 两个实例同时扫同一批行。
	var wg sync.WaitGroup
	errs := make([]error, 2)
	start := make(chan struct{})
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			<-start
			errs[n] = s.flushOutbox(tenant.Skip(ctx))
		}(i)
	}
	close(start)
	wg.Wait()
	for i := range errs {
		if errs[i] != nil {
			t.Fatalf("flushOutbox racer #%d: %v", i, errs[i])
		}
	}

	var dead Outbox
	if err := gdb.WithContext(ctx).Where("id = ?", int64(row.ID)).Take(&dead).Error; err != nil {
		t.Fatalf("reload outbox row: %v", err)
	}
	if dead.Status != OutboxDead {
		t.Fatalf("outbox status = %q, want dead（毒丸载荷必须进死信，不许静默标 sent）", dead.Status)
	}
	if dead.DeadAt == nil {
		t.Fatal("dead_at is NULL：死信没有留死亡时间")
	}
	if !strings.Contains(dead.LastError, "载荷不可解析") {
		t.Fatalf("last_error = %q, want 含「载荷不可解析」", dead.LastError)
	}
	const deadMsg = "pay: outbox event dead"
	if n := lg.countEvent(deadMsg, row.EventID); n != 1 {
		t.Fatalf("这一行的死信迁移留痕 %d 条, want exactly 1：两个实例同时扫同一行，只有一个能转死信", n)
	}
	if n, want := lg.count(deadMsg), backlog+1; n != want {
		t.Fatalf("死信迁移留痕总数 = %d, want %d（每条积压行只允许被迁移一次）", n, want)
	}

	// 死信不再被取走：再跑一轮，状态与留痕都不许动。
	deadAt := *dead.DeadAt
	if err := s.flushOutbox(tenant.Skip(ctx)); err != nil {
		t.Fatalf("second flush pass: %v", err)
	}
	var after Outbox
	if err := gdb.WithContext(ctx).Where("id = ?", int64(row.ID)).Take(&after).Error; err != nil {
		t.Fatal(err)
	}
	if after.Status != OutboxDead || !after.DeadAt.Equal(deadAt) {
		t.Fatalf("死信行被再次取走：status=%q dead_at=%v", after.Status, after.DeadAt)
	}
	if n := lg.count(deadMsg); n != backlog+1 {
		t.Fatalf("第二轮又产生死信留痕：总数 %d, want 仍为 %d（死信不许再被取走）", n, backlog+1)
	}
	counts, err := s.CountOutbox(ctx)
	if err != nil {
		t.Fatalf("CountOutbox: %v", err)
	}
	if counts.Dead != int64(backlog+1) {
		t.Fatalf("CountOutbox dead = %d, want %d", counts.Dead, backlog+1)
	}
	if counts.Pending != 0 {
		t.Fatalf("CountOutbox pending = %d, want 0（本轮积压已全部收敛为死信）", counts.Pending)
	}
}
