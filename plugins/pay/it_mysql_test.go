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

// TestITMySQLFundGates 在真实 MySQL 8 上跑五条资金闸门用例 + 四条并发交叉路径用例
// （两者共用同一套夹具：一次性库 + 真迁移 + 连接池 8）。
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

	// ---- 交叉路径用例的桩扩展（零值 = 既有五条闸门的语义，互不影响）----
	// refundFlip：补查回「渠道已退款成功」，让 SyncRefund 真的走翻转分支。
	refundFlip bool
	// freezePending：冻结受理但不确认，占位行停在 authorizing（补查路径的前置）。
	freezePending bool
	// freezeViaLedger：冻结走 mock 台账（渠道侧真记下这笔授权），
	// 请款/撤销的交叉路径要用它——台账里没有的授权，请款会按「查无」被拒。
	freezeViaLedger bool
	// captureHold：请款在持锁事务内的停留时长——把「另一方正好撞在锁上」变成设计而不是运气。
	captureHold time.Duration
	// gate：渠道动作一进入就非阻塞投递的一次性闩，给并发另一方一个确定的起跑点。
	gate chan struct{}
	// authQueryPlan：预授权补查按「第几次进入」给不同结论（先确认后查无），
	// 用来把「谁先落库」变成断言里可核对的真值，而不是看调度运气。
	authQueryPlan  func(call int, authNo string) (*AuthResult, error)
	queryAuthCalls map[string]int
	// transferPending：代付只受理不给终局，明细停在 initiated（终局补查路径的前置）。
	transferPending bool
	// transferQueryPlan：代付终局补查按「第几次进入」给不同结论。两个 sweeper 拿到相反
	// 终局时，后到那个不许覆写已经落定的资金事实——守卫拿掉就红在这里。
	transferQueryPlan func(call int) (*TransferResult, error)
	transferQueryCall int
	actionCalls       map[string]int
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

func (p *itGateProvider) Freeze(ctx context.Context, in AuthFreezeRequest, cfg map[string]string) (*AuthResult, error) {
	p.mu.Lock()
	p.freezeCalls++
	unconfirmed := p.freezePending
	viaLedger := p.freezeViaLedger
	p.mu.Unlock()
	p.wait()
	switch {
	case unconfirmed:
		// 受理未确认：服务层必须把占位行留在 authorizing 交给补查，绝不落 authorized。
		return &AuthResult{Status: "pending", Reason: "it: 渠道受理未回执"}, nil
	case viaLedger:
		// 请款/撤销路径要求「渠道侧真有这笔授权」（mock 台账），交叉路径的请款用例走这里；
		// 渠道授权号由台账给出（mockauth_…），与它自己的请款/撤销结论自洽。
		return p.mockProvider.Freeze(ctx, in, cfg)
	}
	return &AuthResult{ChannelAuthNo: "itca_" + in.AuthNo, Status: "success", AmountMinor: in.AmountMinor}, nil
}

// setGate 装/摘「动作已进入」的闩（传 nil 摘掉，避免闩跨用例泄漏）。
func (p *itGateProvider) setGate(g chan struct{}) {
	p.mu.Lock()
	p.gate = g
	p.mu.Unlock()
}

// hitGate 非阻塞投递：闩是 cap=1 的一次性通道，重复投递丢弃（渠道动作可能被打多次）。
func (p *itGateProvider) hitGate() {
	p.mu.Lock()
	g := p.gate
	p.mu.Unlock()
	if g == nil {
		return
	}
	select {
	case g <- struct{}{}:
	default:
	}
}

// countAction 记一次渠道动作（并发安全，供交叉路径断言取数）。
func (p *itGateProvider) countAction(name string) {
	p.mu.Lock()
	if p.actionCalls == nil {
		p.actionCalls = map[string]int{}
	}
	p.actionCalls[name]++
	p.mu.Unlock()
}

// holdCapture 在渠道动作里造出「持锁事务还没提交」的时间窗。
func (p *itGateProvider) holdCapture() {
	p.mu.Lock()
	hold := p.captureHold
	p.mu.Unlock()
	if hold > 0 {
		time.Sleep(hold)
	}
}

// CompleteAuth 计数 + 落闩 + 在渠道侧停留，然后按 mock 的真语义回结论。
// 请款的渠道调用发生在持锁事务内，所以「落闩」就是「行锁已持有」的信号。
func (p *itGateProvider) CompleteAuth(ctx context.Context, in AuthCompleteRequest, cfg map[string]string) (*AuthResult, error) {
	p.countAction("complete")
	p.hitGate()
	p.holdCapture()
	return p.mockProvider.CompleteAuth(ctx, in, cfg)
}

func (p *itGateProvider) CancelAuth(ctx context.Context, in AuthCancelRequest, cfg map[string]string) (*AuthResult, error) {
	p.countAction("cancel")
	return p.mockProvider.CancelAuth(ctx, in, cfg)
}

// QueryAuth 按进入次序执行 authQueryPlan（计划自己在锁外 sleep，用来排出「谁先落库」）。
func (p *itGateProvider) QueryAuth(ctx context.Context, authNo string, cfg map[string]string) (*AuthResult, error) {
	p.mu.Lock()
	if p.queryAuthCalls == nil {
		p.queryAuthCalls = map[string]int{}
	}
	p.queryAuthCalls[authNo]++
	call := p.queryAuthCalls[authNo]
	plan := p.authQueryPlan
	p.mu.Unlock()
	if plan == nil {
		return p.mockProvider.QueryAuth(ctx, authNo, cfg)
	}
	return plan(call, authNo)
}

// crossSnapshot 取交叉路径的计次。
func (p *itGateProvider) crossSnapshot() (complete, cancel int, queryAuthByNo map[string]int) {
	p.mu.Lock()
	defer p.mu.Unlock()
	out := make(map[string]int, len(p.queryAuthCalls))
	for k, v := range p.queryAuthCalls {
		out[k] = v
	}
	return p.actionCalls["complete"], p.actionCalls["cancel"], out
}

func (p *itGateProvider) Transfer(_ context.Context, in TransferRequest, _ map[string]string) (*TransferResult, error) {
	p.mu.Lock()
	if p.transferCalls == nil {
		p.transferCalls = map[string]int{}
	}
	p.transferCalls[in.ItemNo]++
	pending := p.transferPending
	p.mu.Unlock()
	p.wait()
	if pending {
		// 渠道受理但终局没回来：明细只能留在 initiated，交给补查收敛。
		return &TransferResult{Pending: true, State: "PROCESSING",
			ChannelBatchNo: "itbt_" + in.BatchNo, ChannelDetailNo: "itdt_" + in.ItemNo}, nil
	}
	return &TransferResult{
		Accepted: true, State: "SUCCESS",
		ChannelBatchNo: "itbt_" + in.BatchNo, ChannelDetailNo: "itdt_" + in.ItemNo,
	}, nil
}

// QueryTransfer 按 transferQueryPlan 给「第几次进入」的结论（两个 sweeper 撞车用例用），
// 没装计划就退回 mock 台账的真语义。
func (p *itGateProvider) QueryTransfer(ctx context.Context, batchNo string, cfg map[string]string) (*TransferResult, error) {
	p.mu.Lock()
	p.transferQueryCall++
	call, plan := p.transferQueryCall, p.transferQueryPlan
	p.mu.Unlock()
	if plan != nil {
		if res, err := plan(call); res != nil || err != nil {
			return res, err
		}
	}
	return p.mockProvider.QueryTransfer(ctx, batchNo, cfg)
}

// QueryRefund 默认永远回「查无此退款」：退款保持 pending，只留下节流与计数。
// refundFlip=true 时回「渠道已退款成功」，让补查真的走翻转分支（交叉路径用例用）。
func (p *itGateProvider) QueryRefund(_ context.Context, req RefundQueryRequest, cfg map[string]string) (*RefundStatusResult, error) {
	p.mu.Lock()
	p.refundQueryCall++
	flip := p.refundFlip
	p.mu.Unlock()
	// 落闩 = 「补查已经查到结论，下一步就是进事务翻转」：并发的回调 racer 以此为起跑点。
	p.hitGate()
	p.wait()
	if flip {
		return &RefundStatusResult{
			Found: true, OK: true,
			ChannelRefundID: "itcb_" + mockHash(req.RefundNo), Reason: "it: 渠道已退款成功",
		}, nil
	}
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

// itRunFundGates 跑五条闸门 + 一条请款/撤销合法序列 + 五条并发交叉路径；两个引擎文件各自建库后调用。
func itRunFundGates(t *testing.T, gdb *gorm.DB, ctx context.Context, ddl itDDLProbe) {
	t.Run("ConcurrentAuthFreezesOnce", func(t *testing.T) { itGateConcurrentAuth(t, gdb, ctx, ddl) })
	t.Run("ConcurrentPayoutClaimsEachItemOnce", func(t *testing.T) { itGateConcurrentPayout(t, gdb, ctx) })
	t.Run("RefundSyncThrottleIsPersisted", func(t *testing.T) { itGateRefundSyncThrottle(t, gdb, ctx) })
	t.Run("CallbackNotifyIDForensics", func(t *testing.T) { itGateCallbackNotifyID(t, gdb, ctx, ddl) })
	t.Run("OutboxDeadLetterTransitionsOnce", func(t *testing.T) { itGateOutboxDeadLetter(t, gdb, ctx) })
	t.Run("RefundSyncVsCallbackFlipsOnce", func(t *testing.T) { itCrossRefundSyncVsCallback(t, gdb, ctx) })
	t.Run("CaptureVsVoidMutuallyExclusive", func(t *testing.T) { itCrossCaptureVsVoid(t, gdb, ctx) })
	t.Run("AuthReconcileBackfillsOnce", func(t *testing.T) { itCrossAuthReconcile(t, gdb, ctx) })
	t.Run("PayoutSettlementJoinsOneBatch", func(t *testing.T) { itCrossPayoutUnique(t, gdb, ctx) })
	t.Run("PayoutSweepWritesTerminalOnce", func(t *testing.T) { itCrossPayoutSweepRace(t, gdb, ctx) })
	t.Run("VoidAfterPartialCaptureKeepsCapturedFact", func(t *testing.T) { itGateVoidAfterPartialCapture(t, gdb, ctx) })
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

// ==================== 并发交叉路径 IT ====================
//
// 上面五条闸门各钉一道守卫；这一组钉的是**两条路径同时动手**的四条交叉路径。
// 共同方法学：
//   - 断言一律是「库里的计数与真值」（SELECT COUNT(*) / 状态列），不看日志文本；
//   - 重叠不是靠 sleep 碰运气：渠道动作（QueryRefund / CompleteAuth）本身就在
//     「即将进事务」或「正持行锁」的位置，桩在那里落闩，另一方收到闩立刻出发，
//     两边就落在同一个事务窗口里；重叠是否真的成立由「删守卫必红」反证（见回票）。
//   - 夹具沿用 itRunFundGates 的一次性库 + 真迁移 + 连接池 8（池=1 时这些用例
//     全部失去意义，删锁也全绿）。

// itPaySuccessCallback 走真实验签链路把支付单翻成 succeeded（交叉路径用例的前置）。
func itPaySuccessCallback(t *testing.T, s *Service, ctx context.Context, code, paymentNo, secret string) {
	t.Helper()
	ts := time.Now().Unix()
	body := []byte(`{"paymentNo":"` + paymentNo + `","result":"success","channelTxnId":"itxt_` + paymentNo +
		`","timestamp":` + fmt.Sprint(ts) + `}`)
	if _, err := s.HandleCallback(ctx, CallbackInput{
		Channel: code, Body: body,
		Headers: map[string]string{"x-timestamp": formatID(ts), "x-signature": SignMockCallback(secret, body, ts)},
	}); err != nil {
		t.Fatalf("%s 支付回调（%s）: %v", code, paymentNo, err)
	}
}

// itRefundSuccessCallback 发一笔退款成功回执。错误回给调用方：它跑在并发协程里，
// 不能在那里 t.Fatal。
func itRefundSuccessCallback(s *Service, ctx context.Context, code, paymentNo, refundNo, secret string) error {
	ts := time.Now().Unix()
	body := []byte(`{"paymentNo":"` + paymentNo + `","refundNo":"` + refundNo +
		`","result":"success","channelRefundId":"itcb_` + refundNo + `","timestamp":` + fmt.Sprint(ts) + `}`)
	_, err := s.HandleCallback(ctx, CallbackInput{
		Channel: code, Body: body,
		Headers: map[string]string{"x-timestamp": formatID(ts), "x-signature": SignMockCallback(secret, body, ts)},
	})
	return err
}

// itCountRows 原生 SQL 计数：交叉路径的断言只认库里真值。
func itCountRows(t *testing.T, gdb *gorm.DB, query string, args ...any) int64 {
	t.Helper()
	var n int64
	if err := gdb.Raw(query, args...).Scan(&n).Error; err != nil {
		t.Fatalf("count via %q: %v", query, err)
	}
	return n
}

// itWarmPool 先把连接池填到并发深度再开赛。
// 冷池上「起跑」本身就要花几十到上百毫秒（MySQL 握手、PG 的 SASL 认证），
// 两个 racer 的建连时间差会直接吃掉「同一瞬间都在同一行上」的窗口，
// 表现为删了守卫也全绿（实测冷池下补查交叉用例偶发有一方漏扫）。
func itWarmPool(t *testing.T, gdb *gorm.DB, n int) {
	t.Helper()
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			var v int
			if err := gdb.Raw("SELECT 1").Scan(&v).Error; err != nil {
				t.Errorf("warm pool: %v", err)
			}
		}()
	}
	wg.Wait()
}

// itScanOne 原生 SQL 取单个标量（状态/金额列）。
func itScanOne[T int64 | string](t *testing.T, gdb *gorm.DB, query string, args ...any) T {
	t.Helper()
	var v T
	if err := gdb.Raw(query, args...).Scan(&v).Error; err != nil {
		t.Fatalf("scan via %q: %v", query, err)
	}
	return v
}

// 交叉路径 1：同一笔 pending 退款，「补查翻转」与「回调翻转」同时发生。
//
// 资金语义：一笔退款只能入一次账。两条路径最终都走 transitionRefund，唯一的闸门是它对
// 退款单行的 FOR UPDATE + 「非 pending 直接返回」；出站箱的 event_id 是随机 UUID，
// 兜不住重复翻转，所以必须按库里计数断言：refunded_minor 只累计一次、
// pay.refund.succeeded 恰一行、退款单终态唯一。
func itCrossRefundSyncVsCallback(t *testing.T, gdb *gorm.DB, ctx context.Context) {
	const (
		rounds      = 6
		secret      = "it-xrefund-secret"
		orderMinor  = int64(1000)
		refundMinor = int64(400)
	)
	prov := newITGateProvider("it-xrefund", 0)
	prov.refundFlip = true
	s := NewService(gdb, nopLogger{})
	if _, err := s.UpsertChannel(ctx, SaveChannelInput{
		Code: "itxref", Name: "补查与回调交叉", Provider: prov.Name(), Enabled: true,
		Config: map[string]string{"secret": secret, "refundOutcome": "pending"},
	}); err != nil {
		t.Fatalf("upsert cross-refund channel: %v", err)
	}
	itWarmPool(t, gdb, 4) // 冷池建连时间会吃掉交叉窗口，先填满连接再开赛

	for i := 0; i < rounds; i++ {
		tag := fmt.Sprintf("XR-%d", i)
		o, _, err := s.Create(ctx, CreateInput{
			BizType: "it_xref", BizNo: tag, ChannelCode: "itxref",
			AmountMinor: orderMinor, Currency: "CNY", Subject: "it cross refund",
		})
		if err != nil {
			t.Fatalf("create order %s: %v", tag, err)
		}
		itPaySuccessCallback(t, s, ctx, "itxref", o.PaymentNo, secret)
		r, err := s.CreateRefund(ctx, RefundInput{
			PaymentNo: o.PaymentNo, BizType: "it_xref_refund", BizNo: "XRR-" + tag,
			AmountMinor: refundMinor, Reason: "it",
		})
		if err != nil {
			t.Fatalf("create refund %s: %v", tag, err)
		}
		if r.Status != RefundPending {
			t.Fatalf("round %s refund status = %q, want pending（异步受理才会进补查路径）", tag, r.Status)
		}

		gate := make(chan struct{}, 1)
		prov.setGate(gate)
		var syncErr, cbErr error
		var callbackFired bool
		var wg sync.WaitGroup
		wg.Add(2)
		go func() {
			defer wg.Done()
			_, syncErr = s.SyncRefund(ctx, r.RefundNo)
		}()
		go func() {
			defer wg.Done()
			// 起跑点：补查侧已经拿到渠道结论（它的下一步就是进事务翻转），
			// 回调侧此刻出发，两边就落在同一个行锁窗口里。
			select {
			case <-gate:
				callbackFired = true
			case <-time.After(3 * time.Second):
				t.Errorf("round %s: 补查侧没打渠道（闩没响），两条路径没交叉", tag)
				return
			}
			cbErr = itRefundSuccessCallback(s, ctx, "itxref", o.PaymentNo, r.RefundNo, secret)
		}()
		wg.Wait()
		prov.setGate(nil)

		if !callbackFired {
			continue // 已由上面的 Errorf 记为失败
		}
		if syncErr != nil {
			t.Errorf("round %s SyncRefund: %v", tag, syncErr)
		}
		if cbErr != nil {
			t.Errorf("round %s HandleCallback: %v", tag, cbErr)
		}

		if got := itScanOne[int64](t, gdb, `SELECT refunded_minor FROM plg_pay_order WHERE payment_no = ?`, o.PaymentNo); got != refundMinor {
			t.Fatalf("round %s: refunded_minor = %d, want %d（补查与回调各记一遍 = 同一笔退款重复入账）",
				tag, got, refundMinor)
		}
		if got := itScanOne[string](t, gdb, `SELECT status FROM plg_pay_order WHERE payment_no = ?`, o.PaymentNo); got != StatusPartiallyRefunded {
			t.Fatalf("round %s: 支付单状态 = %q, want %s（状态迁移只许发生一次）", tag, got, StatusPartiallyRefunded)
		}
		if n := itCountRows(t, gdb, `SELECT COUNT(*) FROM plg_pay_outbox WHERE event_name = ? AND payload LIKE ?`,
			EventRefundOK, `%`+r.RefundNo+`%`); n != 1 {
			t.Fatalf("round %s: 出站箱 pay.refund.succeeded = %d 行, want 恰 1（%s 被发了两遍，下游会重复入账）",
				tag, n, r.RefundNo)
		}
		if n := itCountRows(t, gdb, `SELECT COUNT(*) FROM plg_pay_refund WHERE biz_type = ? AND biz_no = ?`,
			"it_xref_refund", "XRR-"+tag); n != 1 {
			t.Fatalf("round %s: 退款单行数 = %d, want 1", tag, n)
		}
		var fin Refund
		if err := gdb.Raw(`SELECT status, fail_reason, succeeded_at FROM plg_pay_refund WHERE refund_no = ?`, r.RefundNo).
			Scan(&fin).Error; err != nil {
			t.Fatalf("round %s reload refund: %v", tag, err)
		}
		if fin.Status != RefundSucceeded || fin.SucceededAt == nil || fin.FailReason != "" {
			t.Fatalf("round %s: 退款终态不唯一（status=%q succeededAt=%v failReason=%q）", tag, fin.Status, fin.SucceededAt, fin.FailReason)
		}
	}
}

// 交叉路径 2：并发 CaptureAuth 与 VoidAuth 打在同一张 authorized 单上。
//
// 资金语义：全额请款与撤销互斥。两条路径都靠授权行的 FOR UPDATE + Freezable 判定；
// 请款的渠道调用就在持锁事务里，所以「请款已进入渠道动作」等价于「行锁已被持有」，
// 撤销此刻出发必然撞在同一把锁上。断言取库里真值：不能既是 voided 又带已请款额，
// 同 bizNo 的请款收款单不重复，Σcaptured ≤ amount。
func itCrossCaptureVsVoid(t *testing.T, gdb *gorm.DB, ctx context.Context) {
	const (
		secret    = "it-xcapture-secret"
		frozen    = int64(5000)
		capBiz    = "it_xcap_capture"
		capBizNo  = "XCC-1"
		lockHeld  = 150 * time.Millisecond
		startWait = 3 * time.Second
	)
	prov := newITGateProvider("it-xcapture", 0)
	prov.freezeViaLedger = true // 渠道侧要真记下这笔授权，请款/撤销才有得可请、可撤
	prov.captureHold = lockHeld
	s := NewService(gdb, nopLogger{})
	if _, err := s.UpsertChannel(ctx, SaveChannelInput{
		Code: "itxcap", Name: "请款与撤销交叉", Provider: prov.Name(), Enabled: true,
		Config: map[string]string{"secret": secret},
	}); err != nil {
		t.Fatalf("upsert capture/void channel: %v", err)
	}
	itWarmPool(t, gdb, 4) // 建连耗时不能算进「撤销撞上请款持锁」的那 150ms
	a, err := s.CreateAuth(ctx, CreateAuthInput{
		BizType: "it_xcap", BizNo: "XC-1", ChannelCode: "itxcap",
		AmountMinor: frozen, Currency: "CNY", SubjectID: "it-xcap-subj", PayerID: "it-buyer", Remark: "it",
	})
	if err != nil {
		t.Fatalf("CreateAuth: %v", err)
	}
	if a.Status != AuthAuthorized {
		t.Fatalf("预授权状态 = %q, want authorized（并发对象必须是同一张 authorized 单）", a.Status)
	}

	gate := make(chan struct{}, 1)
	prov.setGate(gate)
	var capErr, voidErr error
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		_, capErr = s.CaptureAuth(ctx, a.AuthNo, CaptureAuthInput{
			AmountMinor: frozen, BizType: capBiz, BizNo: capBizNo, OperatorID: "it",
		})
	}()
	go func() {
		defer wg.Done()
		select {
		case <-gate:
		case <-time.After(startWait):
			// 请款没进渠道动作（多半是它自己先被拒）：撤销照常出发，
			// 此时库里应当只有撤销一个终态，下面的互斥断言仍然成立。
		}
		_, voidErr = s.VoidAuth(ctx, a.AuthNo)
	}()
	wg.Wait()
	prov.setGate(nil)

	complete, cancel, _ := prov.crossSnapshot()
	if capErr == nil && voidErr == nil {
		t.Fatalf("全额请款与撤销双双成功（complete=%d cancel=%d）：行锁闸门失效，同一笔冻结被两头动用",
			complete, cancel)
	}
	if capErr != nil && voidErr != nil {
		t.Fatalf("并发请款与撤销都被拒（capErr=%v voidErr=%v）：没有一方推进，前置没建立", capErr, voidErr)
	}

	fresh, err := s.QueryAuth(ctx, a.AuthNo)
	if err != nil {
		t.Fatalf("reload auth: %v", err)
	}
	if fresh.CapturedMinor > fresh.AmountMinor {
		t.Fatalf("Σcaptured=%d > amount=%d：请款累计越界", fresh.CapturedMinor, fresh.AmountMinor)
	}
	orders := itCountRows(t, gdb, `SELECT COUNT(*) FROM plg_pay_order WHERE biz_type = ? AND biz_no = ?`, capBiz, capBizNo)
	switch fresh.Status {
	case AuthVoided:
		if fresh.CapturedMinor != 0 {
			t.Fatalf("撤销单上还挂着已请款额 %d：请款与撤销同时成立", fresh.CapturedMinor)
		}
		if fresh.VoidedAt == nil {
			t.Fatal("voided 却 voided_at IS NULL：撤销没留终态时间")
		}
		if orders != 0 {
			t.Fatalf("撤销成功的单留下 %d 张请款收款单, want 0（撤销成立就不该有收款事实）", orders)
		}
	default:
		if fresh.Status != AuthCaptured && fresh.Status != AuthPartialCaptured {
			t.Fatalf("终态 %q 既不是撤销也不是请款：并发把状态写花了", fresh.Status)
		}
		if fresh.VoidedAt != nil {
			t.Fatalf("已请款（%q）的单上写着撤销时间 %v：两个终态混在同一行", fresh.Status, fresh.VoidedAt)
		}
		if orders != 1 {
			t.Fatalf("同 bizNo %s 的请款收款单 = %d 张, want 恰 1（重复收款 = 重复扣款）", capBizNo, orders)
		}
		if fresh.CapturedMinor != frozen {
			t.Fatalf("全额请款后 captured_minor = %d, want %d", fresh.CapturedMinor, frozen)
		}
	}
	if voidErr == nil && fresh.Status != AuthVoided {
		t.Fatalf("撤销返回成功但库里状态是 %q：结论与真值不符", fresh.Status)
	}
	if capErr == nil && itCountRows(t, gdb,
		`SELECT COUNT(*) FROM plg_pay_order WHERE biz_type = ? AND biz_no = ? AND status = ?`,
		capBiz, capBizNo, StatusSucceeded) != 1 {
		t.Fatal("请款返回成功但没有对应的 succeeded 收款单")
	}
}

// 交叉路径 3：同一张滞留 authorizing 的预授权被两个实例同时补查。
//
// 渠道查询天然幂等，两次 QueryAuth 是允许的；不许发生的是「状态回填两次」：
// 回填走 applyAuthConfirmed / markAuthFailed 的条件更新（WHERE id = ? AND status = 'authorizing'），
// 所以断言取库里真值——两个实例都查过（重叠成立），但终态只由第一个写入者决定，
// 既不会被第二个「已冻结」结论覆盖，也不会被后来的「查无此单」落回 failed。
func itCrossAuthReconcile(t *testing.T, gdb *gorm.DB, ctx context.Context) {
	// 阶段 A：两个实例都拿到「渠道已冻结」结论，但授权号不同 ——
	// 库里留下的必须是先落库那一个，第二个写入被条件更新拒掉。
	itReconcileRound(t, gdb, ctx, 1, "两次都回填 authorized",
		func(call int, _ string) (*AuthResult, error) {
			if call == 1 {
				time.Sleep(400 * time.Millisecond)
				return &AuthResult{ChannelAuthNo: "itca_first", Status: AuthAuthorized}, nil
			}
			time.Sleep(900 * time.Millisecond) // 保证第二次结论一定排在第一次落库之后
			return &AuthResult{ChannelAuthNo: "itca_second", Status: AuthAuthorized}, nil
		}, AuthAuthorized, "itca_first")

	// 阶段 B：先确认后查无 —— 已经回填 authorized 的行不许被后来的「查无」落回 failed。
	itReconcileRound(t, gdb, ctx, 2, "回填后又落 failed",
		func(call int, authNo string) (*AuthResult, error) {
			if call == 1 {
				time.Sleep(400 * time.Millisecond)
				return &AuthResult{ChannelAuthNo: "itca_adopt", Status: AuthAuthorized}, nil
			}
			time.Sleep(900 * time.Millisecond)
			return nil, response.NotFound(errNotFound).WithMsg("it: 渠道查无这笔授权 " + authNo)
		}, AuthAuthorized, "itca_adopt")
}

func itReconcileRound(t *testing.T, gdb *gorm.DB, ctx context.Context, idx int, label string,
	plan func(call int, authNo string) (*AuthResult, error), wantStatus, wantChannelAuthNo string) {
	t.Helper()
	prov := newITGateProvider(fmt.Sprintf("it-xrecon-%d", idx), 0)
	prov.freezePending = true // 受理未确认：占位行停在 authorizing，交给补查收敛
	prov.authQueryPlan = plan
	code := fmt.Sprintf("itxr%d", idx)
	s := NewService(gdb, nopLogger{})
	if _, err := s.UpsertChannel(ctx, SaveChannelInput{
		Code: code, Name: fmt.Sprintf("补查交叉 %d", idx), Provider: prov.Name(), Enabled: true,
		Config: map[string]string{"secret": "it-xrecon-secret"},
	}); err != nil {
		t.Fatalf("upsert reconcile channel %d: %v", idx, err)
	}
	bizNo := fmt.Sprintf("XRA-%d", idx)
	if _, err := s.CreateAuth(ctx, CreateAuthInput{
		BizType: "it_xrecon", BizNo: bizNo, ChannelCode: code,
		AmountMinor: 3000, Currency: "CNY", SubjectID: fmt.Sprintf("it-xrecon-subj-%d", idx), Remark: "it",
	}); err == nil {
		t.Fatalf("%s: 渠道受理未确认时 CreateAuth 不该返回成功（假冻结）", label)
	}

	var a Authorization
	if err := gdb.WithContext(ctx).Where("biz_type = ? AND biz_no = ?", "it_xrecon", bizNo).Take(&a).Error; err != nil {
		t.Fatalf("%s: load authorizing row: %v", label, err)
	}
	if a.Status != AuthAuthorizing {
		t.Fatalf("%s: 前置状态 = %q, want authorizing", label, a.Status)
	}
	stale := time.Now().UTC().Add(-10 * time.Minute)
	if err := gdb.Exec("UPDATE plg_pay_auth SET updated_at = ? WHERE id = ?", stale, int64(a.ID)).Error; err != nil {
		t.Fatalf("%s: push auth updated_at stale: %v", label, err)
	}

	sums := make([]AuthReconcileSummary, 2)
	errs := make([]error, 2)
	start := make(chan struct{})
	itWarmPool(t, gdb, 4) // 两个实例都要在第一次回填落库前扫到这一行
	var wg sync.WaitGroup
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			<-start
			sums[n], errs[n] = s.ReconcileStuckAuths(ctx, time.Now().UTC())
		}(i)
	}
	close(start)
	wg.Wait()
	for i := range errs {
		if errs[i] != nil {
			t.Fatalf("%s: ReconcileStuckAuths racer #%d: %v", label, i, errs[i])
		}
	}

	// 重叠取证：两个实例都扫到了这一行、都打了渠道查询（查询幂等，多打不算错）。
	for i := range sums {
		if sums[i].Checked < 1 {
			t.Fatalf("%s: racer #%d 一个滞留行都没扫到（Checked=0）：两条补查没重叠，用例失去意义", label, i)
		}
		if sums[i].Adopted+sums[i].Failed+sums[i].Unresolved+sums[i].Skipped != sums[i].Checked {
			t.Fatalf("%s: racer #%d 分桶不齐（checked=%d）：%+v —— 有一笔补查没有归属", label, i, sums[i].Checked, sums[i])
		}
	}
	// 指标不许翻倍：这一行只能被一个实例「救回」，另一个必须落 Skipped（条件更新影响 0 行）。
	// 库里那一行只有一个终态（下面的 channel_auth_no 取证），计数也必须只有一个写入者。
	saved, skipped := 0, 0
	for i := range sums {
		saved += sums[i].Adopted + sums[i].Failed
		skipped += sums[i].Skipped
	}
	if saved != 1 {
		t.Fatalf("%s: 两实例合计「本轮落定」= %d, want 恰 1（补查撞车被数成两次救回，运维据此判断补偿效果就是错的）：%+v",
			label, saved, sums)
	}
	if skipped != 1 {
		t.Fatalf("%s: 两实例合计 skipped = %d, want 1（败者的那一笔没归属）：%+v", label, skipped, sums)
	}
	_, _, queries := prov.crossSnapshot()
	if queries[a.AuthNo] != 2 {
		t.Fatalf("%s: 渠道 QueryAuth 对本行调用 %d 次, want 2（两个实例都真的查过）", label, queries[a.AuthNo])
	}

	var fin Authorization
	if err := gdb.WithContext(ctx).Where("id = ?", int64(a.ID)).Take(&fin).Error; err != nil {
		t.Fatalf("%s: reload auth: %v", label, err)
	}
	if fin.Status != wantStatus {
		t.Fatalf("%s: 终态 = %q, want %q（failReason=%q）", label, fin.Status, wantStatus, fin.FailReason)
	}
	if fin.ChannelAuthNo != wantChannelAuthNo {
		t.Fatalf("%s: channel_auth_no = %q, want %q —— 状态回填发生了第二次，后到的写入覆盖了先落库的渠道结论",
			label, fin.ChannelAuthNo, wantChannelAuthNo)
	}
	if fin.FailReason != "" {
		t.Fatalf("%s: 已回填 %s 的行上还写着 %q —— 既回填 authorized 又落 failed", label, wantStatus, fin.FailReason)
	}
}

// 交叉路径 4：同一张结算单被两个请求同时建批。
//
// 兜底的是 uk_pay_payout_item_settlement：一条结算单只进一个批次，重复出款在 DDL 层就被拒。
// 两个请求都持同一张结算单行的 FOR UPDATE，所以后到者必然在对方提交之后才写明细 ——
// 唯一索引不看运气：无论是否重叠它都必须拦住第二行明细。失败方要拿到明确的索引冲突错误
// （不是静默成功），且它自己的批次行随事务回滚（库里不留下半截草稿批次）。
func itCrossPayoutUnique(t *testing.T, gdb *gorm.DB, ctx context.Context) {
	const itemIndex = "uk_pay_payout_item_settlement"
	s := NewService(gdb, nopLogger{})
	st := Settlement{
		SubjectID: "it-xpo-subj", PeriodStart: "2026-11-03", PeriodEnd: "2026-11-03",
		GrossMinor: 1000, CommissionMinor: 100, NetMinor: 900,
		Currency: "CNY", Status: SettlementConfirmed, OrderCount: 1,
	}
	if err := gdb.WithContext(ctx).Create(&st).Error; err != nil {
		t.Fatalf("create settlement: %v", err)
	}
	settlementID := int64(st.ID)
	batchesBefore := itCountRows(t, gdb, `SELECT COUNT(*) FROM plg_pay_payout_batch`)

	batches := make([]*PayoutBatch, 2)
	errs := make([]error, 2)
	start := make(chan struct{})
	itWarmPool(t, gdb, 4) // 让两个请求真的在结算单的行锁上排队，而不是在建连上排队
	var wg sync.WaitGroup
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			<-start
			batches[n], errs[n] = s.CreatePayout(ctx, CreatePayoutInput{
				SettlementIDs: []int64{settlementID}, Method: "channel", Note: "it cross payout",
			})
		}(i)
	}
	close(start)
	wg.Wait()

	ok := 0
	var texts []string
	for i := range errs {
		texts = append(texts, fmt.Sprintf("racer#%d: %v", i, errs[i]))
		if errs[i] == nil {
			ok++
			continue
		}
		// 落败方要拿到契约 409（不是驱动原文冒出去的 500），并且原错误仍 Wrap 在里面：
		// 索引名是「谁拦下这笔出款」的唯一取证，两引擎都把它写进错误文本，不受服务端语言影响。
		var re *response.Error
		if !errors.As(errs[i], &re) || re.Key != errPayoutSettlementTaken || re.Status != 409 {
			t.Fatalf("落败方拿到的是 %v, want 409 %s（重复出款要有明确契约码，调用方才能提示「已打过款」）",
				errs[i], errPayoutSettlementTaken)
		}
		if !strings.Contains(strings.ToLower(re.Error()), itemIndex) {
			t.Fatalf("409 里没有 %s —— 拦住这笔的索引被吞了，只剩一句通用文案：%s", itemIndex, re.Error())
		}
	}
	if ok != 1 {
		t.Fatalf("并发建批成功方 = %d, want 恰 1（两张批次吃同一笔钱 = 重复出款）：%s", ok, strings.Join(texts, " | "))
	}
	if n := itCountRows(t, gdb, `SELECT COUNT(*) FROM plg_pay_payout_item WHERE settlement_id = ?`, settlementID); n != 1 {
		t.Fatalf("%s 没拦住：该结算单在 plg_pay_payout_item 里有 %d 行, want 1", itemIndex, n)
	}
	if n := itCountRows(t, gdb, `SELECT COUNT(*) FROM plg_pay_payout_batch`) - batchesBefore; n != 1 {
		t.Fatalf("并发建批落下 %d 行批次, want 恰 1（失败方的批次必须随事务回滚，不留半截草稿）", n)
	}
	var winner *PayoutBatch
	for i := range batches {
		if errs[i] == nil {
			winner = batches[i]
		}
	}
	if n := itCountRows(t, gdb, `SELECT COUNT(*) FROM plg_pay_payout_item WHERE batch_id = ?`, int64(winner.ID)); n != 1 {
		t.Fatalf("胜者批次 %d 的明细 = %d 行, want 1", int64(winner.ID), n)
	}

	// 串行重试同一条断言：并发那一路要看调度，这一路不看——重复出款必须由同一个索引、
	// 同一个契约码拦下，且 409 文本里仍带着索引名。
	_, retryErr := s.CreatePayout(ctx, CreatePayoutInput{
		SettlementIDs: []int64{settlementID}, Method: "channel", Note: "it cross payout retry",
	})
	var re *response.Error
	if !errors.As(retryErr, &re) || re.Key != errPayoutSettlementTaken || re.Status != 409 {
		t.Fatalf("重复建批串行重试拿到的是 %v, want 409 %s", retryErr, errPayoutSettlementTaken)
	}
	if !strings.Contains(strings.ToLower(re.Error()), itemIndex) {
		t.Fatalf("串行重试的 409 里没有 %s：%s", itemIndex, re.Error())
	}
	if n := itCountRows(t, gdb, `SELECT COUNT(*) FROM plg_pay_payout_item WHERE settlement_id = ?`, settlementID); n != 1 {
		t.Fatalf("结算单 %d 重试后有 %d 行明细, want 仍 1", settlementID, n)
	}
}

// 合法序列（此前没闸）：部分请款之后再撤销。Freezable 含 partial_captured，所以撤销是允许的，
// 但它只释放没请款的那部分——已经请款成功的钱是既成资金事实，撤销既不许在本地抹掉
// captured_minor，也不许让那张收款单跟着消失。这条走真库是因为前两条都要看库里落下来的列，
// sqlmock 从不执行 SQL，照不出「UPDATE 少写了一列」。
func itGateVoidAfterPartialCapture(t *testing.T, gdb *gorm.DB, ctx context.Context) {
	const (
		secret  = "it-voidp-secret"
		frozen  = int64(5000)
		capture = int64(2000)
	)
	prov := newITGateProvider("it-voidp", 0)
	prov.freezeViaLedger = true // 渠道台账要真记下这笔授权，部分请款与撤销才有得可动
	s := NewService(gdb, nopLogger{})
	if _, err := s.UpsertChannel(ctx, SaveChannelInput{
		Code: "itvoidp", Name: "部分请款后撤销", Provider: prov.Name(), Enabled: true,
		Config: map[string]string{"secret": secret},
	}); err != nil {
		t.Fatalf("upsert void-partial channel: %v", err)
	}
	a, err := s.CreateAuth(ctx, CreateAuthInput{
		BizType: "it_voidp", BizNo: "VP-1", ChannelCode: "itvoidp",
		AmountMinor: frozen, Currency: "CNY", SubjectID: "it-voidp-subj", PayerID: "it-buyer",
	})
	if err != nil {
		t.Fatalf("CreateAuth: %v", err)
	}
	if a.Status != AuthAuthorized {
		t.Fatalf("预授权状态 = %q, want authorized", a.Status)
	}

	if _, err := s.CaptureAuth(ctx, a.AuthNo, CaptureAuthInput{
		AmountMinor: capture, BizType: "it_voidp_capture", BizNo: "VPC-1", OperatorID: "it",
	}); err != nil {
		t.Fatalf("CaptureAuth(部分请款): %v", err)
	}
	after, err := s.QueryAuth(ctx, a.AuthNo)
	if err != nil {
		t.Fatalf("reload after partial capture: %v", err)
	}
	if after.Status != AuthPartialCaptured || after.CapturedMinor != capture {
		t.Fatalf("部分请款后 = (%q, captured %d), want (partial_captured, %d)", after.Status, after.CapturedMinor, capture)
	}

	v, err := s.VoidAuth(ctx, a.AuthNo)
	if err != nil {
		t.Fatalf("部分请款后的撤销必须被允许（Freezable 含 partial_captured，释放剩余额度）：%v", err)
	}
	if v.Status != AuthVoided || v.VoidedAt == nil {
		t.Fatalf("撤销终态 = (%q, voidedAt=%v), want (voided, 非空)", v.Status, v.VoidedAt)
	}
	if got := itScanOne[int64](t, gdb, `SELECT captured_minor FROM plg_pay_auth WHERE auth_no = ?`, a.AuthNo); got != capture {
		t.Fatalf("撤销后 captured_minor = %d, want %d —— 撤销把已经请款成功的资金事实抹掉了", got, capture)
	}
	if n := itCountRows(t, gdb, `SELECT COUNT(*) FROM plg_pay_order WHERE biz_type = ? AND biz_no = ? AND status = ?`,
		"it_voidp_capture", "VPC-1", StatusSucceeded); n != 1 {
		t.Fatalf("部分请款的收款单 = %d 行(succeeded), want 1 —— 撤销不该倒扣已经收进来的钱", n)
	}
	complete, cancel, _ := prov.crossSnapshot()
	if complete != 1 || cancel != 1 {
		t.Fatalf("渠道动作 complete=%d cancel=%d, want 1/1（请款一次、撤销剩余额度一次）", complete, cancel)
	}
}

// 交叉路径 5：两个 sweeper 同时补查同一批滞留在 initiated 的代付明细，并拿到相反的终局。
//
// 资金语义：出款终局是资金事实，只许第一个写入者落。渠道查询幂等，多打一次不算错；
// 但后到的「失败」结论若允许无守卫写入，会把已经落定的「成功」覆写成失败——钱已经出去了，
// 账上记成没出，运维照着这张表会再打一次款（重复出款）。断言取库里真值：
// 终态仍是第一个结论、失败原因没被写进去、两个 sweeper 合计只数到一个写入者。
func itCrossPayoutSweepRace(t *testing.T, gdb *gorm.DB, ctx context.Context) {
	prov := newITGateProvider("it-sweprice", 0)
	prov.transferPending = true // 发起时只受理、不给终局：明细留在 initiated，交给补查收敛
	prov.transferQueryPlan = func(call int) (*TransferResult, error) {
		switch call {
		case 1:
			time.Sleep(300 * time.Millisecond) // 第一个结论故意晚到，保证两个 sweeper 都先扫到了这一行
			return &TransferResult{Accepted: true, State: "SUCCESS", ChannelBatchNo: "itbt_race"}, nil
		case 2:
			time.Sleep(700 * time.Millisecond)
			return &TransferResult{Accepted: false, State: "FAILED",
				ChannelBatchNo: "itbt_race", Reason: "it: 渠道回终局失败"}, nil
		}
		return nil, nil
	}
	s := NewService(gdb, nopLogger{})
	if _, err := s.UpsertChannel(ctx, SaveChannelInput{
		Code: "itswee", Name: "代付终局补查撞车", Provider: prov.Name(), Enabled: true,
		Config: map[string]string{"secret": "it-sweep-secret"},
	}); err != nil {
		t.Fatalf("upsert payout race channel: %v", err)
	}
	st := Settlement{
		SubjectID: "it-swee-subj", PeriodStart: "2026-11-04", PeriodEnd: "2026-11-04",
		GrossMinor: 1000, CommissionMinor: 100, NetMinor: 900,
		Currency: "CNY", Status: SettlementConfirmed, OrderCount: 1,
	}
	if err := gdb.WithContext(ctx).Create(&st).Error; err != nil {
		t.Fatalf("create settlement: %v", err)
	}
	batch, err := s.CreatePayout(ctx, CreatePayoutInput{
		SettlementIDs: []int64{int64(st.ID)}, Method: "channel", Note: "it race",
	})
	if err != nil {
		t.Fatalf("CreatePayout: %v", err)
	}
	if _, err := s.PayPayoutViaChannel(ctx, int64(batch.ID), "itswee"); err != nil {
		t.Fatalf("PayPayoutViaChannel: %v", err)
	}

	// 前置取证：明细真的停在 initiated，批次真的带上了渠道批次号（否则补查压根不入选）。
	if got := itScanOne[string](t, gdb, `SELECT status FROM plg_pay_payout_item WHERE batch_id = ?`, int64(batch.ID)); got != PayoutItemInitiated {
		t.Fatalf("发起后代付明细 = %q, want %s：前置没建立，两条补查撞不了车", got, PayoutItemInitiated)
	}
	if got := itScanOne[string](t, gdb, `SELECT channel_batch_no FROM plg_pay_payout_batch WHERE id = ?`, int64(batch.ID)); got == "" {
		t.Fatal("批次没有渠道批次号：SweepStuckPayouts 不会入选，用例失去意义")
	}
	stale := time.Now().UTC().Add(-20 * time.Minute)
	if err := gdb.Exec(`UPDATE plg_pay_payout_item SET updated_at = ? WHERE batch_id = ?`, stale, int64(batch.ID)).Error; err != nil {
		t.Fatalf("age payout item: %v", err)
	}

	itWarmPool(t, gdb, 4) // 两个 sweeper 都要在第一个终局落库前扫到这一行
	sums := make([]PayoutSweepSummary, 2)
	errs := make([]error, 2)
	start := make(chan struct{})
	var wg sync.WaitGroup
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			<-start
			sums[n], errs[n] = s.SweepStuckPayouts(ctx, time.Now().UTC())
		}(i)
	}
	close(start)
	wg.Wait()
	for i := range errs {
		if errs[i] != nil {
			t.Fatalf("sweeper #%d: %v", i, errs[i])
		}
	}
	resolved, skipped := 0, 0
	for i := range sums {
		resolved += sums[i].Resolved
		skipped += sums[i].Skipped
	}
	if resolved != 1 || skipped != 1 {
		t.Fatalf("本轮落定 = %d, 败者 = %d, want 1/1（两个 sweeper 各数一遍 = 指标翻倍，也说明覆写没被拦住）：%+v",
			resolved, skipped, sums)
	}
	if got := itScanOne[string](t, gdb, `SELECT status FROM plg_pay_payout_item WHERE batch_id = ?`, int64(batch.ID)); got != PayoutItemSuccess {
		t.Fatalf("代付明细终态 = %q, want %s —— 后到的渠道结论覆写了已经落定的资金事实", got, PayoutItemSuccess)
	}
	if got := itScanOne[string](t, gdb, `SELECT fail_reason FROM plg_pay_payout_item WHERE batch_id = ?`, int64(batch.ID)); got != "" {
		t.Fatalf("成功那笔上写着失败原因 %q —— 同一笔既成功又失败", got)
	}
}
