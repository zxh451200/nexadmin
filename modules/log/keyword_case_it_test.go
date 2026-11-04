// 日志列表关键字大小写折叠的真库守卫（modules/log/service.go:77）。
//
// 这一处最特殊：keywordCols 是白名单里的动态列名（操作日志 module/operator/url，
// 登录日志 username/ip），折叠写成 "LOWER("+col+") LIKE LOWER(?)"。
// 改动前是 `col LIKE ?`：PG 的 LIKE 大小写敏感、MySQL/MariaDB 这里的列是 CI 排序，
// 关键字带大写字母时只有 PG 部署静默查空。modules/log/export_test.go 那组 sqlmock 用例
// 只比对语句文本（LOWER(...) 出现在 SQL 里就算过），从不执行 SQL，照不出行为差异。
// 本文件在三种真引擎上跑两条独立拼 SQL 的路径（ListOper / ListLogin），
// 并逐列打一次探针，证明白名单里每个列都真的进了折叠后的 OR 条件。
//
// 建库走生产构造器 database.Open（装租户回调 + UTC 时区）：日志表的租户条件全靠那个插件，
// 裸 gorm.Open 夹具下「别的租户的行查不到」会退化成空断言，所以这里把它显式列成一条反例。
//
// 默认跳过；设 DSN 才执行（MySQL 侧库名必须留空，见 AGENTS.md）：
//
//	NEXADMIN_IT_MYSQL_DSN='root:pass@tcp(127.0.0.1:3306)/?charset=utf8mb4&parseTime=true&loc=UTC' \
//	NEXADMIN_IT_PG_DSN='host=localhost port=5432 user=postgres password=pass dbname=postgres sslmode=disable' \
//	go test ./modules/log -run TestITKeyword -count=1 -v
package log

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
	"nexadmin/core/datascope"
	"nexadmin/core/migrate"
	"nexadmin/core/tenant"
	"nexadmin/pkg/idgen"
)

const (
	kwTenant      = 900022
	kwOtherTenant = 900122
)

func kwCtx(tid int64) context.Context { return tenant.With(context.Background(), idgen.ID(tid)) }

// kwAllScope 是超管/全部范围：不裁剪部门，只测折叠本身。
func kwAllScope() *datascope.Scope { return &datascope.Scope{All: true} }

// kwOpenMySQL 用生产构造器连一次性库（库名由时间戳生成，不是请求输入），并跑完 mysql/mariadb 目录的框架迁移。
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
		Name: fmt.Sprintf("nexadmin_kw_log_%d", time.Now().UnixNano()),
	}
	if err := database.EnsureDatabase(context.Background(), cfg); err != nil {
		t.Fatalf("建一次性库失败: %v", err)
	}
	// Cleanup 后进先出：这个「删库」注册在前 → 最后执行，先让后注册的「关连接池」跑掉。
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
		Name: fmt.Sprintf("nexadmin_kw_log_%d", time.Now().UnixNano()),
	}
	if err := database.EnsureDatabase(context.Background(), cfg); err != nil {
		t.Fatalf("建一次性库失败: %v", err)
	}
	t.Cleanup(func() {
		// 这条管理会话连的是原始 DSN 里的库（不是被测库），只用来 DROP。
		admin, err := gorm.Open(gormpg.Open(raw), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
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

// kwOperRows 每行只在一条被测列上带一个可识别的大写串，
// 这样一个关键字就是一个列的探针（module / operator / url 各一次）。
var kwOperRows = []OperLog{
	{Module: "Alpha-Module", Type: "create", Operator: "Operator-One", Method: "POST", URL: "/api/v1/alpha/one", Status: StatusSuccess},
	{Module: "Beta-Module", Type: "export", Operator: "Region-Report", Method: "GET", URL: "/api/v1/beta/two", Status: StatusFailed},
	{Module: "月度报表Gamma", Type: "query", Operator: "Operator-Three", Method: "GET", URL: "/api/v1/Gamma/Alpha-Report", Status: StatusSuccess},
	{Module: "Unrelated-Delta", Type: "other", Operator: "Operator-Four", Method: "GET", URL: "/api/v1/delta/four", Status: StatusSuccess},
}

// kwLoginRows 同理：username 与 ip 各一条只在该列上可识别的行。
// IP 里放全大写的十六进制段，是这条折叠最真实的用法（关键字多半是小写粘进来的）。
var kwLoginRows = []LoginLog{
	{Username: "Login-Alpha", LoginType: "password", IP: "203.0.113.10", Status: StatusSuccess},
	{Username: "Region-Report", LoginType: "password", IP: "FE80::ABCD:1", Status: StatusFailed},
	{Username: "月度报表User", LoginType: "sso", IP: "2001:DB8::FF00", Status: StatusSuccess},
	{Username: "Unrelated-Note", LoginType: "password", IP: "192.0.2.1", Status: StatusSuccess},
}

func kwSeed(t *testing.T, db *gorm.DB) {
	t.Helper()
	ctx := kwCtx(kwTenant)
	for i := range kwOperRows {
		row := kwOperRows[i]
		if err := db.WithContext(ctx).Create(&row).Error; err != nil {
			t.Fatalf("建操作日志 %q 失败: %v", row.Module, err)
		}
	}
	for i := range kwLoginRows {
		row := kwLoginRows[i]
		if err := db.WithContext(ctx).Create(&row).Error; err != nil {
			t.Fatalf("建登录日志 %q 失败: %v", row.Username, err)
		}
	}
	// 别的租户各一行：本租户的关键字绝不该把它们捞出来（租户插件真的在跑的证明）。
	if err := db.WithContext(kwCtx(kwOtherTenant)).Create(&OperLog{
		Module: "Other-Tenant-Module", Type: "create", Operator: "Other-One", Method: "POST",
		URL: "/api/v1/other", Status: StatusSuccess}).Error; err != nil {
		t.Fatalf("建别的租户的操作日志失败: %v", err)
	}
	if err := db.WithContext(kwCtx(kwOtherTenant)).Create(&LoginLog{
		Username: "Other-Tenant-Login", LoginType: "password", IP: "203.0.113.99", Status: StatusSuccess}).Error; err != nil {
		t.Fatalf("建别的租户的登录日志失败: %v", err)
	}
}

func kwSorted(in []string) []string {
	out := append([]string{}, in...)
	sort.Strings(out)
	return out
}

func kwExpect(t *testing.T, name string, got []string, want ...string) {
	t.Helper()
	if strings.Join(kwSorted(got), "|") != strings.Join(kwSorted(want), "|") {
		t.Errorf("%s 命中 %v，期望 %v", name, kwSorted(got), kwSorted(want))
	}
}

// kwSearchOper 走服务自己的操作日志列表路径；total 与本页行数必须一致，
// 否则说明 count 与 find 的 WHERE 分叉了（折叠条件只加在其中一条上也会在这里露出来）。
func kwSearchOper(t *testing.T, svc *Service, q Query) []string {
	t.Helper()
	q.Page, q.PageSize = 1, 50
	q.Scope = kwAllScope()
	list, total, err := svc.ListOper(kwCtx(kwTenant), q)
	if err != nil {
		t.Fatalf("ListOper(%+v): %v", q, err)
	}
	if int64(len(list)) != total {
		t.Fatalf("ListOper keyword=%q: total=%d 但本页 %d 行", q.Keyword, total, len(list))
	}
	out := make([]string, 0, len(list))
	for _, v := range list {
		out = append(out, v.Module)
	}
	return out
}

func kwSearchLogin(t *testing.T, svc *Service, q Query) []string {
	t.Helper()
	q.Page, q.PageSize = 1, 50
	q.Scope = kwAllScope()
	list, total, err := svc.ListLogin(kwCtx(kwTenant), q)
	if err != nil {
		t.Fatalf("ListLogin(%+v): %v", q, err)
	}
	if int64(len(list)) != total {
		t.Fatalf("ListLogin keyword=%q: total=%d 但本页 %d 行", q.Keyword, total, len(list))
	}
	out := make([]string, 0, len(list))
	for _, v := range list {
		out = append(out, v.Username)
	}
	return out
}

func runKeywordFoldInvariants(t *testing.T, db *gorm.DB) {
	t.Helper()
	svc := NewService(db, "UTC", zap.NewNop())
	kwSeed(t, db)

	operAll := []string{"Alpha-Module", "Beta-Module", "月度报表Gamma", "Unrelated-Delta"}
	loginAll := []string{"Login-Alpha", "Region-Report", "月度报表User", "Unrelated-Note"}

	// 两条路径共用同一组断言形状：一条是 module/operator/url，另一条是 username/ip。
	type kwPath struct {
		name  string
		all   []string
		run   func(Query) []string
		bad   []string // 配对反例
		other string   // 别的租户那行的关键字，必须查不到
	}
	paths := []kwPath{
		{"ListOper", operAll, func(q Query) []string { return kwSearchOper(t, svc, q) },
			[]string{"alpha-modulez", "module-alpha", "beta-moduleb", "报表度月", "qzxvjk", "zzz-not-here"},
			"other-tenant-module"},
		{"ListLogin", loginAll, func(q Query) []string { return kwSearchLogin(t, svc, q) },
			[]string{"login-alphaz", "alpha-login", "region-reportz", "报表度月", "qzxvjk", "zzz-not-here"},
			"other-tenant-login"},
	}
	for _, p := range paths {
		// 0) 无关键字：分母（钉住夹具真落了库、租户条件正常）。
		kwExpect(t, p.name+" 无关键字", p.run(Query{}), p.all...)

		// 1/2) 逐列探针：白名单里每个列都真进了折叠后的 OR 条件。
		//      种的是大写，查的是同一串文字的小写 / 全大写 / 混排三种形式——
		//      改动前带大写形式以外的两种在 PG 上直接查空。
		for _, probe := range kwProbes(p.name) {
			kwExpect(t, p.name+" "+probe.keyword, p.run(Query{Keyword: probe.keyword}), probe.want...)
			kwExpect(t, p.name+" "+strings.ToUpper(probe.keyword), p.run(Query{Keyword: strings.ToUpper(probe.keyword)}), probe.want...)
			kwExpect(t, p.name+" 混排 "+kwMixedCase(probe.keyword), p.run(Query{Keyword: kwMixedCase(probe.keyword)}), probe.want...)
		}

		// 3) 中文（CJK）：LOWER() 对中文是无操作，关键字照常命中；
		//    「中文 + 拉丁大小写」在同一条谓词里一起命中，证明折叠没把非拉丁文本挤出结果。
		for _, cjk := range kwCJKProbes(p.name) {
			kwExpect(t, p.name+" "+cjk.keyword, p.run(Query{Keyword: cjk.keyword}), cjk.want...)
		}

		// 4) 配对反例：折叠后的谓词不是「恒真」。差一字符、字序颠倒、乱码串都要查不到，
		//    否则上面那批正例就是空跑出来的。
		for _, bad := range p.bad {
			kwExpect(t, p.name+" 反例 "+bad, p.run(Query{Keyword: bad}))
		}

		// 5) 租户隔离没被折叠带跑。
		kwExpect(t, p.name+" 别的租户的行", p.run(Query{Keyword: p.other}))
	}

	// 6) 与其余筛选条件叠加时折叠仍生效（这些条件是 AND，不互相顶掉）。
	kwExpect(t, "ListOper type+keyword 正例", kwSearchOper(t, svc, Query{Keyword: "region-report", Type: "export"}), "Beta-Module")
	kwExpect(t, "ListOper type+keyword 反例", kwSearchOper(t, svc, Query{Keyword: "region-report", Type: "delete"}))
	kwExpect(t, "ListOper status+keyword", kwSearchOper(t, svc, Query{Keyword: "alpha-report", Status: StatusSuccess}), "月度报表Gamma")
	kwExpect(t, "ListOper status+keyword 反例", kwSearchOper(t, svc, Query{Keyword: "alpha-report", Status: StatusFailed}))
	kwExpect(t, "ListLogin loginType+keyword", kwSearchLogin(t, svc, Query{Keyword: "月度报表", LoginType: "sso"}), "月度报表User")
	kwExpect(t, "ListLogin loginType+keyword 反例", kwSearchLogin(t, svc, Query{Keyword: "月度报表", LoginType: "password"}))
	kwExpect(t, "ListLogin status+keyword", kwSearchLogin(t, svc, Query{Keyword: "fe80::abcd:1", Status: StatusFailed}), "Region-Report")
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

// kwProbes 是每条路径的逐列探针：keyword 只在该列（而非其它被测列）上出现，
// 期望行因此唯一确定；同一探针换成大写、混排形式也必须命中。
func kwProbes(pathName string) []kwProbeSpec {
	if pathName == "ListLogin" {
		return []kwProbeSpec{
			{"login-alpha", []string{"Login-Alpha"}},     // username 列
			{"region-report", []string{"Region-Report"}}, // username 列
			{"fe80::abcd:1", []string{"Region-Report"}},  // ip 列（库里是全大写十六进制）
			{"db8::ff00", []string{"月度报表User"}},          // ip 列
			{"unrelated", []string{"Unrelated-Note"}},    // username 列
		}
	}
	return []kwProbeSpec{
		{"alpha-module", []string{"Alpha-Module"}},  // module 列
		{"beta-module", []string{"Beta-Module"}},    // module 列
		{"region-report", []string{"Beta-Module"}},  // operator 列
		{"operator-three", []string{"月度报表Gamma"}},   // operator 列
		{"alpha-report", []string{"月度报表Gamma"}},     // url 列（库里是 /Gamma/Alpha-Report）
		{"delta/four", []string{"Unrelated-Delta"}}, // url 列
	}
}

type kwProbeSpec struct {
	keyword string
	want    []string
}

// kwCJKProbes 中文关键字：含纯中文、中文+拉丁小写、中文+拉丁大写三种。
func kwCJKProbes(pathName string) []kwProbeSpec {
	if pathName == "ListLogin" {
		return []kwProbeSpec{
			{"月度报表", []string{"月度报表User"}},
			{"月度报表user", []string{"月度报表User"}},
			{"月度报表USER", []string{"月度报表User"}},
		}
	}
	return []kwProbeSpec{
		{"月度报表", []string{"月度报表Gamma"}},
		{"月度报表gamma", []string{"月度报表Gamma"}},
		{"月度报表GAMMA", []string{"月度报表Gamma"}},
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
