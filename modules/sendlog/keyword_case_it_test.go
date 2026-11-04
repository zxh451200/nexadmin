// 发送日志「收件人关键字」大小写折叠的真库守卫（modules/sendlog/service.go:196）。
//
// 改动：`recipient LIKE ?` → `LOWER(recipient) LIKE LOWER(?)`。
// PG 的 LIKE 大小写敏感（列没有 CI 排序规则），MySQL/MariaDB 这里的列是 CI 排序，
// 所以改之前「关键字里带大写字母」在 PG 部署上会静默查出 0 行，MySQL 部署照常命中。
// 这个差异 sqlmock 照不出来：modules/sendlog/service_test.go 只比对语句文本、从不执行 SQL，
// 两边都「通过」。本文件在三种真引擎上各跑一遍，断言同一串文字的大小写变体都能捞到那行。
//
// 与 recycle/notice 的两处夹具不同，这里建库走生产构造器 database.Open（装租户回调 + UTC 时区）：
// 本模块的 List 自己显式带 tenant_id，租户条件不依赖插件，所以额外用「别的租户的行查不到」
// 这条反例把插件真的在跑钉住——裸 gorm.Open 的夹具下这条断言会退化成空断言。
//
// 默认跳过；设 DSN 才执行（MySQL 侧库名必须留空，见 AGENTS.md）：
//
//	NEXADMIN_IT_MYSQL_DSN='root:pass@tcp(127.0.0.1:3306)/?charset=utf8mb4&parseTime=true&loc=UTC' \
//	NEXADMIN_IT_PG_DSN='host=localhost port=5432 user=postgres password=pass dbname=postgres sslmode=disable' \
//	go test ./modules/sendlog -run TestITKeyword -count=1 -v
//
// 关键字里 % _ \ 的转义口径不在这里重复验证（由 sqlmock 那组用例钉住语句形状）。
package sendlog

import (
	"context"
	"database/sql"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/go-sql-driver/mysql"
	"go.uber.org/zap"
	gormpg "gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"nexadmin/core/config"
	"nexadmin/core/database"
	"nexadmin/core/migrate"
	"nexadmin/core/tenant"
	"nexadmin/pkg/idgen"
)

// kwTenant 是本次断言的租户；kwOtherTenant 只用来种「不该被查到的别人家的行」。
// 号段避开 recycle(900007)/notice(900011) 等同机真库用例。
const (
	kwTenant      = 900021
	kwOtherTenant = 900121
)

func kwCtx(tid int64) context.Context { return tenant.With(context.Background(), idgen.ID(tid)) }

// kwOpenMySQL 用生产构造器连一次性库（库名由时间戳生成，不是请求输入），并跑完 mysql/mariadb 目录的框架迁移。
// 迁移里的 DDL 不写 COLLATE，列的排序规则继承库级 utf8mb4_general_ci —— MySQL 侧的 CI 行为才是生产真的行为。
func kwOpenMySQL(t *testing.T, engine string) *gorm.DB {
	t.Helper()
	raw := os.Getenv("NEXADMIN_IT_MYSQL_DSN")
	if raw == "" {
		t.Skip("NEXADMIN_IT_MYSQL_DSN not set; skipping real-MySQL integration test")
	}
	d, err := mysql.ParseDSN(raw)
	if err != nil {
		t.Fatal(err)
	}
	if d.DBName != "" {
		t.Fatalf("MySQL DSN 的库名必须留空（夹具自建一次性库），实得 %q", d.DBName)
	}
	host, portStr, err := net.SplitHostPort(d.Addr)
	if err != nil {
		t.Fatal(err)
	}
	port, err := strconv.Atoi(portStr)
	if err != nil {
		t.Fatal(err)
	}
	cfg := config.Database{
		Type: engine, Host: host, Port: port, User: d.User, Password: d.Passwd,
		Name: fmt.Sprintf("nexadmin_kw_sendlog_%d", time.Now().UnixNano()),
	}
	if err := database.EnsureDatabase(context.Background(), cfg); err != nil {
		t.Fatalf("建一次性库失败: %v", err)
	}
	// 清场顺序：Cleanup 是后进先出，下面这个「删库」注册在前 → 最后执行；
	// 后注册的「关连接池」先执行。反过来的话 MySQL 不会报错，但库会留在服务器上。
	t.Cleanup(func() {
		root, err := sql.Open("mysql", d.FormatDSN())
		if err != nil {
			t.Errorf("连回服务器删库失败，一次性库 %s 残留: %v", cfg.Name, err)
			return
		}
		defer func() { _ = root.Close() }()
		if _, err := root.Exec("DROP DATABASE " + cfg.Name); err != nil {
			t.Errorf("DROP DATABASE %s: %v", cfg.Name, err)
		}
	})
	db, err := database.Open(cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close(db) })
	kwApplyMigrations(t, db, engine)
	return db
}

// kwPGKV 把 key=value 形式的 libpq DSN 拆成 map（用例环境里不含带空格/引号的值）。
func kwPGKV(raw string) map[string]string {
	out := map[string]string{}
	for _, part := range strings.Fields(raw) {
		if k, v, ok := strings.Cut(part, "="); ok {
			out[k] = v
		}
	}
	return out
}

func kwOpenPG(t *testing.T) *gorm.DB {
	t.Helper()
	raw := os.Getenv("NEXADMIN_IT_PG_DSN")
	if raw == "" {
		t.Skip("NEXADMIN_IT_PG_DSN not set; skipping real-PostgreSQL integration test")
	}
	kv := kwPGKV(raw)
	port, err := strconv.Atoi(kv["port"])
	if kv["host"] == "" || kv["user"] == "" || err != nil {
		t.Fatalf("NEXADMIN_IT_PG_DSN 需要 host/port/user（password 可为空）：解析得到 %+v，port 转换 err=%v", kv, err)
	}
	cfg := config.Database{
		Type: "postgres", Host: kv["host"], Port: port, User: kv["user"], Password: kv["password"],
		Name: fmt.Sprintf("nexadmin_kw_sendlog_%d", time.Now().UnixNano()),
	}
	if err := database.EnsureDatabase(context.Background(), cfg); err != nil {
		t.Fatalf("建一次性库失败: %v", err)
	}
	t.Cleanup(func() {
		// PG 删库时不能有任何连到该库的会话：生产池已由后注册的 Cleanup 关掉。
		// 这条管理会话连的是原始 DSN 里的库（不是被测库），只用来 DROP。
		admin, err := gorm.Open(gormpg.Open(raw), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
		if err != nil {
			t.Errorf("连 PG 管理会话失败，一次性库 %s 残留: %v", cfg.Name, err)
			return
		}
		defer func() {
			if d, derr := admin.DB(); derr == nil {
				_ = d.Close()
			}
		}()
		if err := admin.Exec(`DROP DATABASE IF EXISTS "` + cfg.Name + `"`).Error; err != nil {
			t.Errorf("DROP DATABASE %s: %v", cfg.Name, err)
		}
	})
	db, err := database.Open(cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close(db) })
	kwApplyMigrations(t, db, "postgres")
	return db
}

// kwApplyMigrations 按文件名顺序执行 migrations/framework/<engine>/ 下全部 SQL：
// 列的字符集与排序规则跟生产一致，不是夹具自己造的。
func kwApplyMigrations(t *testing.T, db *gorm.DB, engine string) {
	t.Helper()
	dir := filepath.Join("..", "..", "migrations", "framework", engine)
	files, err := filepath.Glob(filepath.Join(dir, "*.sql"))
	if err != nil || len(files) == 0 {
		t.Fatalf("没读到 %s 下的迁移文件（%d 个）", dir, len(files))
	}
	sort.Strings(files)
	for _, f := range files {
		b, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		for _, stmt := range migrate.Split(string(b)) {
			if err := db.Exec(stmt).Error; err != nil {
				t.Fatalf("%s: %v\n语句：%s", filepath.Base(f), err, kwFirstLine(stmt))
			}
		}
	}
}

// kwFirstLine 只取语句首行，报错时不把整份 SQL 糊在输出里。
func kwFirstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}

// kwSeedRecipients 四条收件人：三条含大写字母（其中一条中文与拉丁混排），
// 一条不含任何被测子串，用来钉住「折叠不是把所有行都捞出来」。
// 每行换不同的本地段，保证「一个关键字只该命中一条」。
var kwSeedRecipients = []string{
	"Region-Report@corp.example",
	"Export-Center@corp.example",
	"月度报表Alpha@corp.example",
	"Unrelated-Note@corp.example",
}

func kwSeed(t *testing.T, db *gorm.DB) {
	t.Helper()
	for i, recipient := range kwSeedRecipients {
		row := &SendLog{
			Channel: "email", Scene: "notice", Recipient: recipient,
			Subject: "Keyword fold fixture", Status: "success", DurationMs: 10 + i,
		}
		if err := db.WithContext(kwCtx(kwTenant)).Create(row).Error; err != nil {
			t.Fatalf("建发送日志 %q 失败: %v", recipient, err)
		}
	}
	// 别的租户的一条同名收件人：本租户的关键字绝不能把它捞出来（租户插件真的在跑）。
	other := &SendLog{Channel: "email", Scene: "notice", Recipient: "Other-Tenant-Region@corp.example",
		Subject: "other tenant", Status: "success"}
	if err := db.WithContext(kwCtx(kwOtherTenant)).Create(other).Error; err != nil {
		t.Fatalf("建别的租户的发送日志失败: %v", err)
	}
}

// kwSearch 用关键字走服务自己的列表路径，返回命中的收件人（升序，便于整串比对）。
// total 与本页行数必须一致：同一个 WHERE 下不一致说明 count 与 find 的条件分叉了。
func kwSearch(t *testing.T, svc *Service, keyword string) []string {
	t.Helper()
	list, total, err := svc.List(kwCtx(kwTenant), Query{Recipient: keyword, Page: 1, PageSize: 50})
	if err != nil {
		t.Fatalf("List(recipient=%q): %v", keyword, err)
	}
	if int64(len(list)) != total {
		t.Fatalf("recipient=%q: total=%d 但本页 %d 行（同一 WHERE 不该分叉）", keyword, total, len(list))
	}
	out := make([]string, 0, len(list))
	for _, v := range list {
		out = append(out, v.Recipient)
	}
	sort.Strings(out)
	return out
}

func kwExpect(t *testing.T, name string, got []string, want ...string) {
	t.Helper()
	want = append([]string{}, want...)
	sort.Strings(want)
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Errorf("%s 命中 %v，期望 %v", name, got, want)
	}
}

func runKeywordFoldInvariants(t *testing.T, db *gorm.DB) {
	t.Helper()
	svc := NewService(db, "UTC", zap.NewNop())
	t.Cleanup(svc.Close)
	kwSeed(t, db)

	// 0) 无关键字：四条都在（每条断言的分母，也钉住「夹具真落了库」）。
	kwExpect(t, "无关键字", kwSearch(t, svc, ""), kwSeedRecipients...)

	// 1) 改动前会在 PG 上失败的正例：种的是大写、查的是同一串文字的小写形式。
	kwExpect(t, "region-report", kwSearch(t, svc, "region-report"), "Region-Report@corp.example")
	kwExpect(t, "REGION-REPORT", kwSearch(t, svc, "REGION-REPORT"), "Region-Report@corp.example")
	kwExpect(t, "export-center", kwSearch(t, svc, "export-center"), "Export-Center@corp.example")
	kwExpect(t, "EXPORT-CENTER", kwSearch(t, svc, "EXPORT-CENTER"), "Export-Center@corp.example")
	// 混排大小写同样要命中（不是只兜住「全小写」这一种输入）。
	kwExpect(t, "eXpOrT-cEnTeR", kwSearch(t, svc, "eXpOrT-cEnTeR"), "Export-Center@corp.example")

	// 2) 中文（CJK）：LOWER() 对中文是无操作，关键字照常命中；
	//    「中文 + 拉丁大小写」在同一条谓词里一起命中，证明折叠没把非拉丁文本挤出结果。
	kwExpect(t, "月度报表", kwSearch(t, svc, "月度报表"), "月度报表Alpha@corp.example")
	kwExpect(t, "alpha", kwSearch(t, svc, "alpha"), "月度报表Alpha@corp.example")
	kwExpect(t, "ALPHA", kwSearch(t, svc, "ALPHA"), "月度报表Alpha@corp.example")
	kwExpect(t, "月度报表alpha", kwSearch(t, svc, "月度报表alpha"), "月度报表Alpha@corp.example")

	// 3) 配对反例：折叠后的谓词不是「恒真」。差一字符、字序颠倒、乱码串都要查不到，
	//    否则上面那批正例就是空跑出来的。
	kwExpect(t, "region-reportz", kwSearch(t, svc, "region-reportz"))
	kwExpect(t, "report-region", kwSearch(t, svc, "report-region"))
	kwExpect(t, "报表度月", kwSearch(t, svc, "报表度月"))
	kwExpect(t, "qzxvjk", kwSearch(t, svc, "qzxvjk"))
	// 噪声行只能被自己的子串命中。
	kwExpect(t, "unrelated", kwSearch(t, svc, "unrelated"), "Unrelated-Note@corp.example")

	// 4) 租户隔离没被折叠带跑：别的租户那行在本租户下任何关键字都查不到。
	kwExpect(t, "别的租户的行", kwSearch(t, svc, "other-tenant-region"))

	// 5) 与渠道/状态筛选叠加时折叠仍生效（这些条件是 AND，不互相顶掉）。
	list, total, err := svc.List(kwCtx(kwTenant), Query{Channel: "email", Status: "success", Recipient: "region-report", Page: 1, PageSize: 10})
	if err != nil || total != 1 || len(list) != 1 || list[0].Recipient != "Region-Report@corp.example" {
		t.Fatalf("channel+status+关键字 叠加：期望 1 行 Region-Report，实得 total=%d %+v err=%v", total, list, err)
	}
	if _, total2, err := svc.List(kwCtx(kwTenant), Query{Channel: "email", Recipient: "region-reportz", Page: 1, PageSize: 10}); err != nil || total2 != 0 {
		t.Fatalf("channel+关键字 反例期望 0 行，实得 total=%d err=%v", total2, err)
	}
	// 渠道条件真在生效：换成 sms 就一条都不该有。
	if _, total3, err := svc.List(kwCtx(kwTenant), Query{Channel: "sms", Recipient: "region-report", Page: 1, PageSize: 10}); err != nil || total3 != 0 {
		t.Fatalf("channel=sms 期望 0 行（夹具全是 email），实得 total=%d err=%v", total3, err)
	}
}

func TestITMySQLKeywordFoldCase(t *testing.T) {
	if err := idgen.Init(44); err != nil {
		t.Fatal(err)
	}
	runKeywordFoldInvariants(t, kwOpenMySQL(t, "mysql"))
}

func TestITMariaDBKeywordFoldCase(t *testing.T) {
	if err := idgen.Init(44); err != nil {
		t.Fatal(err)
	}
	runKeywordFoldInvariants(t, kwOpenMySQL(t, "mariadb"))
}

func TestITPostgresKeywordFoldCase(t *testing.T) {
	if err := idgen.Init(44); err != nil {
		t.Fatal(err)
	}
	runKeywordFoldInvariants(t, kwOpenPG(t))
}
