// 前端错误列表关键字大小写折叠的真库守卫（modules/clienterror/service.go:267）。
//
// 改动前：`(message LIKE ? OR route LIKE ? OR username LIKE ?)`；
// 改动后：三个列各自 LOWER(col) LIKE LOWER(?)。
// PG 的 LIKE 大小写敏感、MySQL/MariaDB 这里的列是 CI 排序，所以带大写字母的关键字
// 在 PG 部署上会静默查出 0 行。本包原先一条用例都没到过这个分支
// （clienterror_test.go 的 memStore 只覆盖 Ingest 的聚合与限流，List 只有 memStore 之外的空实现），
// 所以这里是第一份真执行。
//
// 建库走生产构造器 database.Open：本包的 List 完全不写 tenant_id，租户条件 100% 由
// core/database 的租户插件追加。裸 gorm.Open 夹具下「别的租户的行查不到」会退化成空断言，
// 所以那条反例既是业务断言，也是夹具走对了构造器的证据。
//
// 默认跳过；设 DSN 才执行（MySQL 侧库名必须留空，见 AGENTS.md）：
//
//	NEXADMIN_IT_MYSQL_DSN='root:pass@tcp(127.0.0.1:3306)/?charset=utf8mb4&parseTime=true&loc=UTC' \
//	NEXADMIN_IT_PG_DSN='host=localhost port=5432 user=postgres password=pass dbname=postgres sslmode=disable' \
//	go test ./modules/clienterror -run TestITKeyword -count=1 -v
package clienterror

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

const (
	kwTenant      = 900023
	kwOtherTenant = 900123
)

func kwCtx(tid int64) context.Context { return tenant.With(context.Background(), idgen.ID(tid)) }

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
		Name: fmt.Sprintf("nexadmin_kw_clienterr_%d", time.Now().UnixNano()),
	}
	if err := database.EnsureDatabase(context.Background(), cfg); err != nil {
		t.Fatalf("建一次性库失败: %v", err)
	}
	// Cleanup 后进先出：删库注册在前 → 最后执行，先让后注册的「关连接池」跑掉。
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

func kwPGKV(raw string) map[string]string {
	out := map[string]string{}
	for _, part := range strings.Fields(raw) {
		if k, v, ok := strings.Cut(part, "="); ok {
			out[k] = v
		}
	}
	return out
}

// kwPGAdminDSN 补齐库名：pgx 的 keyword DSN 没有空值写法，`dbname=` 会把下一个 token 当成库名，
// 于是只用来 DROP 的那条管理会话连不上、一次性库全留在服务器上。被测库名由 config.Database.Name 决定，
// 这里的库名只是「先连上服务器」的落点。
func kwPGAdminDSN(raw string) string {
	kv := kwPGKV(raw)
	name := kv["dbname"]
	if name == "" {
		name = "postgres"
	}
	parts := make([]string, 0, len(kv)+1)
	for k, v := range kv {
		if k != "dbname" {
			parts = append(parts, k+"="+v)
		}
	}
	return strings.Join(append(parts, "dbname="+name), " ")
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
		Name: fmt.Sprintf("nexadmin_kw_clienterr_%d", time.Now().UnixNano()),
	}
	if err := database.EnsureDatabase(context.Background(), cfg); err != nil {
		t.Fatalf("建一次性库失败: %v", err)
	}
	t.Cleanup(func() {
		// 这条管理会话连的是原始 DSN 里的库（不是被测库），只用来 DROP。
		admin, err := gorm.Open(gormpg.Open(kwPGAdminDSN(raw)), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
		if err != nil {
			t.Errorf("连 PG 管理会话失败，一次性库 %s 残留: %v", cfg.Name, err)
			return
		}
		defer func() {
			if dd, derr := admin.DB(); derr == nil {
				_ = dd.Close()
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

func kwFirstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}

// kwRows 三行被测 + 一行噪声：每行只在一个被测列（message / route / username）上带
// 一个可识别的大写串，这样一个关键字就是一列的探针；期望集合因此唯一确定。
// A 行同时是中文（CJK）行，「月度报表Alpha」必须是连写的，才测得出
// 「同一条谓词里中文片段 + 拉丁大小写片段」一起命中。
var kwRows = []ClientError{
	{Message: "TypeError: 月度报表Alpha加载失败", Route: "/Dashboard/Overview", Username: "Alice-Zhang", Fingerprint: "kwfp-a"},
	{Message: "Network Error: Timeout On Request", Route: "/report/Monthly", Username: "Region-Runner", Fingerprint: "kwfp-b"},
	{Message: "Render Failed: Nothing To See", Route: "/ops/Carad", Username: "Carol-Wang", Fingerprint: "kwfp-c"},
	{Message: "Unrelated Noise", Route: "/zzz/none", Username: "Dave-Reporter", Fingerprint: "kwfp-d"},
}

func kwSeed(t *testing.T, db *gorm.DB) {
	t.Helper()
	for i := range kwRows {
		row := kwRows[i]
		row.HitCount = 1
		row.LastSeenAt = time.Now().UTC()
		if err := db.WithContext(kwCtx(kwTenant)).Create(&row).Error; err != nil {
			t.Fatalf("建前端错误 %q 失败: %v", row.Message, err)
		}
	}
	// 别的租户一行，message 里带一个本租户也会用到的词：本租户任何关键字都不该把它捞出来。
	other := ClientError{Message: "Other-Tenant-Secret Leak", Route: "/other", Username: "Eve",
		Fingerprint: "kwfp-other", HitCount: 1, LastSeenAt: time.Now().UTC()}
	if err := db.WithContext(kwCtx(kwOtherTenant)).Create(&other).Error; err != nil {
		t.Fatalf("建别的租户的前端错误失败: %v", err)
	}
}

// kwMessages 走服务自己的列表路径，返回命中的 message（升序，便于整串比对）。
func kwMessages(t *testing.T, svc *Service, keyword string) []string {
	t.Helper()
	list, total, err := svc.List(kwCtx(kwTenant), Query{Keyword: keyword, Page: 1, PageSize: 50})
	if err != nil {
		t.Fatalf("List(keyword=%q): %v", keyword, err)
	}
	if int64(len(list)) != total {
		t.Fatalf("keyword=%q: total=%d 但本页 %d 行（同一 WHERE 不该分叉）", keyword, total, len(list))
	}
	out := make([]string, 0, len(list))
	for _, r := range list {
		out = append(out, r.Message)
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

// kwMixedCase 把 ASCII 字母按「偶数位大写、奇数位小写」交错，钉住折叠不是只兜住
// 「关键字全小写」或「全大写」这两种极端输入；数字、标点与非 ASCII 字节原样保留。
func kwMixedCase(s string) string {
	out := []byte(s)
	for i := range out {
		switch {
		case out[i] >= 'a' && out[i] <= 'z' && i%2 == 0:
			out[i] -= 32
		case out[i] >= 'A' && out[i] <= 'Z' && i%2 == 1:
			out[i] += 32
		}
	}
	return string(out)
}

type kwProbeSpec struct {
	keyword string
	want    []string
}

// kwProbes 逐列探针：keyword 只在该列（而非其它两个被测列）上出现，期望行因此唯一确定。
// 每条都按「种的大写 / 查的小写」写法准备——改动前这些关键字在 PG 上一律查空，
// 而 MySQL/MariaDB 照常命中；换成全大写、混排形式也必须命中，钉住折叠不是只兜某一种输入。
func kwProbes() []kwProbeSpec {
	return []kwProbeSpec{
		{"typeerror", []string{kwRowA}},           // message 列
		{"timeout on request", []string{kwRowB}},  // message 列
		{"render failed", []string{kwRowC}},       // message 列
		{"nothing to see", []string{kwRowC}},      // message 列
		{"/dashboard/overview", []string{kwRowA}}, // route 列
		{"/report/monthly", []string{kwRowB}},     // route 列
		{"/ops/carad", []string{kwRowC}},          // route 列
		{"alice-zhang", []string{kwRowA}},         // username 列
		{"region-runner", []string{kwRowB}},       // username 列
		{"carol-wang", []string{kwRowC}},          // username 列
		{"unrelated noise", []string{kwRowD}},     // 噪声行：只能被自己的子串命中
		{"dave-reporter", []string{kwRowD}},       // username 列（噪声行的另一列）
	}
}

// 四行的 message 原文（比对用），与 kwRows 一一对应。
const (
	kwRowA = "TypeError: 月度报表Alpha加载失败"
	kwRowB = "Network Error: Timeout On Request"
	kwRowC = "Render Failed: Nothing To See"
	kwRowD = "Unrelated Noise"
)

func runKeywordFoldInvariants(t *testing.T, db *gorm.DB) {
	t.Helper()
	svc := NewService(db, zap.NewNop())
	kwSeed(t, db)
	all := make([]string, 0, len(kwRows))
	for _, r := range kwRows {
		all = append(all, r.Message)
	}

	// 0) 无关键字：四行都在（每条断言的分母，也钉住「夹具真落了库、租户条件正常」）。
	kwExpect(t, "无关键字", kwMessages(t, svc, ""), all...)

	// 1) 改动前会在 PG 上失败的正例，且逐列各打一次探针。
	for _, probe := range kwProbes() {
		kwExpect(t, probe.keyword, kwMessages(t, svc, probe.keyword), probe.want...)
		// 同一串文字换成全大写、混排形式也必须命中（不是只兜住「全小写」这一种输入）。
		kwExpect(t, strings.ToUpper(probe.keyword), kwMessages(t, svc, strings.ToUpper(probe.keyword)), probe.want...)
		kwExpect(t, "混排 "+kwMixedCase(probe.keyword), kwMessages(t, svc, kwMixedCase(probe.keyword)), probe.want...)
	}

	// 2) 中文（CJK）：LOWER() 对中文是无操作，关键字照常命中；
	//    「中文 + 拉丁大小写」在同一条谓词里一起命中，证明折叠没把非拉丁文本挤出结果。
	kwExpect(t, "月度报表", kwMessages(t, svc, "月度报表"), kwRowA)
	kwExpect(t, "加载失败", kwMessages(t, svc, "加载失败"), kwRowA)
	kwExpect(t, "alpha", kwMessages(t, svc, "alpha"), kwRowA)
	kwExpect(t, "ALPHA", kwMessages(t, svc, "ALPHA"), kwRowA)
	kwExpect(t, "月度报表alpha", kwMessages(t, svc, "月度报表alpha"), kwRowA)
	kwExpect(t, "月度报表ALPHA", kwMessages(t, svc, "月度报表ALPHA"), kwRowA)

	// 3) 配对反例：折叠后的谓词不是「恒真」。差一字符、字序颠倒、乱码串都要查不到，
	//    否则上面那批正例就是空跑出来的。
	for _, bad := range []string{"typeerrorz", "request timeout on", "reporter-dave", "报表度月", "失败载加", "qzxvjk", "zzz-not-here"} {
		kwExpect(t, "反例 "+bad, kwMessages(t, svc, bad))
	}

	// 4) 租户隔离没被折叠带跑：别的租户那行的关键字在本租户下 0 行。
	kwExpect(t, "别的租户的行", kwMessages(t, svc, "other-tenant-secret"))
	kwExpect(t, "别的租户的行（全大写）", kwMessages(t, svc, "OTHER-TENANT-SECRET"))

	// 5) 分页与关键字叠加：一页一条时总数仍是总数，不能因为折叠条件把 count 顶掉。
	if _, total, err := svc.List(kwCtx(kwTenant), Query{Keyword: "typeerror", Page: 1, PageSize: 1}); err != nil || total != 1 {
		t.Fatalf("分页 size=1 期望 total=1，实得 total=%d err=%v", total, err)
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
