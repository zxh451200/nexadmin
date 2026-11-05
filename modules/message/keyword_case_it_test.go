// 站内消息关键字大小写折叠的真库守卫（modules/message/service.go:49）。
//
// 改动前：`(title LIKE ? OR content LIKE ?)`；改动后两个列各自 LOWER(col) LIKE LOWER(?)。
// PG 的 LIKE 大小写敏感、MySQL/MariaDB 这里的列是 CI 排序，所以带大写字母的关键字
// 在 PG 部署上会静默查出 0 行——消息标题里中英混排是常态（演示数据就有
// "Welcome to NexAdmin"、"SKU-8841" 这类）。
// 本包原先的用例（list_filter_test.go / delete_many_test.go）走的是 sqlmock 或内存实现，
// 只比语句文本、从不执行 SQL，照不出这个差异。
//
// 建库走生产构造器 database.Open（装租户回调 + UTC 时区）：List 只显式限定 user_id，
// 租户条件由插件追加。裸 gorm.Open 夹具下「别的租户的消息查不到」会退化成空断言，
// 所以那条反例同时是业务断言和「夹具走对了构造器」的证据。
//
// 默认跳过；设 DSN 才执行（MySQL 侧库名必须留空，见 AGENTS.md）：
//
//	NEXADMIN_IT_MYSQL_DSN='root:pass@tcp(127.0.0.1:3306)/?charset=utf8mb4&parseTime=true&loc=UTC' \
//	NEXADMIN_IT_PG_DSN='host=localhost port=5432 user=postgres password=pass dbname=postgres sslmode=disable' \
//	go test ./modules/message -run TestITKeyword -count=1 -v
package message

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
	gormpg "gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"nexadmin/core/config"
	"nexadmin/core/database"
	"nexadmin/core/migrate"
	"nexadmin/core/tenant"
	"nexadmin/modules/notice"
	"nexadmin/pkg/idgen"
)

const (
	kwTenant      = 900025
	kwOtherTenant = 900125
)

// 两个收件人：kwReader 是断言用的读者，kwOtherReader 同租户下的别人（消息不该串到它这里）。
var (
	kwReader      = idgen.ID(7300000000000925001)
	kwOtherReader = idgen.ID(7300000000000925002)
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
		Name: fmt.Sprintf("nexadmin_kw_message_%d", time.Now().UnixNano()),
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
		Name: fmt.Sprintf("nexadmin_kw_message_%d", time.Now().UnixNano()),
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

// 四条标题（比对用）：A/C 只有大写拉丁、B 中文与拉丁混排、D 不含任何被测子串。
const (
	kwMsgA = "Region-Report Ready"
	kwMsgB = "月度报表Alpha"
	kwMsgC = "Export-Center Done"
	kwMsgD = "Unrelated-Note"
)

// kwSeed 种四条收件人 kwReader 的消息 + 两条反例：
// 同租户别人的消息（own() 必须挡住）、别的租户的消息（租户插件必须挡住）。
// readAt 故意做成 A 未读、B 已读，供「未读 + 关键字」的 AND 断言配对。
func kwSeed(t *testing.T, db *gorm.DB) {
	t.Helper()
	ctx := kwCtx(kwTenant)
	now := time.Now().UTC()
	read := now.Add(-time.Hour)
	rows := []Message{
		{Title: kwMsgA, Content: "Queued-For-Review in Room 3B", Category: "system", UserID: kwReader},
		{Title: kwMsgB, Content: "报表已生成，请核对 REPORT-OK", Category: "todo", UserID: kwReader, ReadAt: &read},
		{Title: kwMsgC, Content: "See the Monthly-Summary tab", Category: "business", UserID: kwReader},
		{Title: kwMsgD, Content: "Nothing to see here", Category: "other", UserID: kwReader},
	}
	for i := range rows {
		m := rows[i]
		if err := db.WithContext(ctx).Create(&m).Error; err != nil {
			t.Fatalf("建消息 %q 失败: %v", m.Title, err)
		}
	}
	// 同租户、别人名下：标题里带同样的大写串，关键字绝不能越 over own() 把它捞出来。
	other := Message{Title: "Region-Report For Someone Else", Content: "not yours", Category: "system", UserID: kwOtherReader}
	if err := db.WithContext(ctx).Create(&other).Error; err != nil {
		t.Fatalf("建别人的消息失败: %v", err)
	}
	// 别的租户、同样挂在 kwReader 名下：租户插件必须挡住（收件人 id 会撞）。
	tenantRow := Message{Title: "Other-Tenant-Region-Report", Content: "not your tenant", Category: "system", UserID: kwReader}
	if err := db.WithContext(kwCtx(kwOtherTenant)).Create(&tenantRow).Error; err != nil {
		t.Fatalf("建别的租户的消息失败: %v", err)
	}
}

// kwSearchTitles 走服务自己的列表路径，返回命中的标题（升序，便于整串比对）。
func kwSearchTitles(t *testing.T, svc *Service, q ListQuery) []string {
	t.Helper()
	q.Page, q.PageSize = 1, 50
	list, total, err := svc.List(kwCtx(kwTenant), kwReader, q)
	if err != nil {
		t.Fatalf("List(%+v): %v", q, err)
	}
	if int64(len(list)) != total {
		t.Fatalf("keyword=%q: total=%d 但本页 %d 行（同一 WHERE 不该分叉）", q.Keyword, total, len(list))
	}
	out := make([]string, 0, len(list))
	for _, v := range list {
		out = append(out, v.Title)
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

// kwProbes 逐列探针：title 与 content 各若干条，keyword 只出现在被测的那一列上。
func kwProbes() []kwProbeSpec {
	return []kwProbeSpec{
		{"region-report", []string{kwMsgA}},     // title 列
		{"export-center", []string{kwMsgC}},     // title 列
		{"unrelated", []string{kwMsgD}},         // title 列（噪声行只能被自己的子串命中）
		{"queued-for-review", []string{kwMsgA}}, // content 列
		{"room 3b", []string{kwMsgA}},           // content 列
		{"report-ok", []string{kwMsgB}},         // content 列
		{"monthly-summary", []string{kwMsgC}},   // content 列
		{"nothing to see", []string{kwMsgD}},    // content 列
	}
}

func runKeywordFoldInvariants(t *testing.T, db *gorm.DB) {
	t.Helper()
	svc := NewService(db, notice.NewService(db), "UTC")
	kwSeed(t, db)
	all := []string{kwMsgA, kwMsgB, kwMsgC, kwMsgD}

	// 0) 无关键字：四条都在（分母；同租户别人与别的租户那两条都不在）。
	kwExpect(t, "无关键字", kwSearchTitles(t, svc, ListQuery{}), all...)

	// 1) 逐列探针：改动前这些关键字在 PG 上一律查空，MySQL/MariaDB 照常命中。
	//    同一串文字再按全大写、混排各查一次。
	for _, probe := range kwProbes() {
		kwExpect(t, probe.keyword, kwSearchTitles(t, svc, ListQuery{Keyword: probe.keyword}), probe.want...)
		kwExpect(t, strings.ToUpper(probe.keyword), kwSearchTitles(t, svc, ListQuery{Keyword: strings.ToUpper(probe.keyword)}), probe.want...)
		kwExpect(t, "混排 "+kwMixedCase(probe.keyword), kwSearchTitles(t, svc, ListQuery{Keyword: kwMixedCase(probe.keyword)}), probe.want...)
	}

	// 2) 中文（CJK）：LOWER() 对中文是无操作，关键字照常命中；
	//    「中文 + 拉丁大小写」在同一条谓词里一起命中，证明折叠没把非拉丁文本挤出结果。
	kwExpect(t, "月度报表", kwSearchTitles(t, svc, ListQuery{Keyword: "月度报表"}), kwMsgB)
	kwExpect(t, "alpha", kwSearchTitles(t, svc, ListQuery{Keyword: "alpha"}), kwMsgB)
	kwExpect(t, "ALPHA", kwSearchTitles(t, svc, ListQuery{Keyword: "ALPHA"}), kwMsgB)
	kwExpect(t, "月度报表alpha", kwSearchTitles(t, svc, ListQuery{Keyword: "月度报表alpha"}), kwMsgB)
	kwExpect(t, "月度报表ALPHA", kwSearchTitles(t, svc, ListQuery{Keyword: "月度报表ALPHA"}), kwMsgB)
	kwExpect(t, "报表已生成", kwSearchTitles(t, svc, ListQuery{Keyword: "报表已生成"}), kwMsgB)

	// 3) 配对反例：折叠后的谓词不是「恒真」。差一字符、字序颠倒、乱码串都要查不到，
	//    否则上面那批正例就是空跑出来的。
	for _, bad := range []string{"region-reportz", "report-region", "报表度月", "生成已报表", "qzxvjk", "zzz-not-here"} {
		kwExpect(t, "反例 "+bad, kwSearchTitles(t, svc, ListQuery{Keyword: bad}))
	}

	// 4) 收件人作用域没被折叠带跑：同租户别人那行的标题里就带 "region-report"，
	//    但它独有的片段对本读者必须恒不可见（own() 的 user_id 条件还在生效）。
	kwExpect(t, "别人的消息（独有片段）", kwSearchTitles(t, svc, ListQuery{Keyword: "someone else"}))
	kwExpect(t, "别人的消息（独有片段，全大写）", kwSearchTitles(t, svc, ListQuery{Keyword: "SOMEONE ELSE"}))
	// 同一个关键字查自己那行：证明上面 0 行不是「整条谓词坏了」，而是收件人条件在挡。
	kwExpect(t, "同关键字查自己那行", kwSearchTitles(t, svc, ListQuery{Keyword: "region-report"}), kwMsgA)

	// 5) 租户隔离没被折叠带跑。
	kwExpect(t, "别的租户的行", kwSearchTitles(t, svc, ListQuery{Keyword: "other-tenant-region-report"}))

	// 6) 与未读 / 分类筛选叠加时折叠仍生效（AND 关系，不互相顶掉）。
	kwExpect(t, "unread+keyword 正例", kwSearchTitles(t, svc, ListQuery{Unread: true, Keyword: "region-report"}), kwMsgA)
	kwExpect(t, "unread+keyword 反例（B 已读）", kwSearchTitles(t, svc, ListQuery{Unread: true, Keyword: "月度报表"}))
	kwExpect(t, "category+keyword 正例", kwSearchTitles(t, svc, ListQuery{Category: "business", Keyword: "monthly-summary"}), kwMsgC)
	kwExpect(t, "category+keyword 反例", kwSearchTitles(t, svc, ListQuery{Category: "system", Keyword: "monthly-summary"}))
	// 分页：一页一条时 total 仍是命中总数，不能因为折叠条件把 count 顶掉。
	if list, total, err := svc.List(kwCtx(kwTenant), kwReader, ListQuery{Keyword: "region-report", Page: 1, PageSize: 1}); err != nil || total != 1 || len(list) != 1 {
		t.Fatalf("分页 size=1 期望 total=1/1 行，实得 total=%d 行数=%d err=%v", total, len(list), err)
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
