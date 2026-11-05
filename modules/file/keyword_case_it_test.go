// 文件列表关键字大小写折叠的真库守卫（modules/file/service.go:212）。
//
// 改动前：`(name LIKE ? OR uploader LIKE ?)`；改动后两个列各自 LOWER(col) LIKE LOWER(?)。
// PG 的 LIKE 大小写敏感、MySQL/MariaDB 这里的列是 CI 排序，所以带大写字母的关键字
// 在 PG 部署上会静默查出 0 行（文件名几乎都是大写扩展名/驼峰命名，撞上的概率很高）。
// 本包原先的用例（quota/serve/variant/image_probe）没有一条到过 List 的关键字分支。
//
// 建库走生产构造器 database.Open：List 完全不写 tenant_id，租户条件 100% 由
// core/database 的租户插件追加；裸 gorm.Open 夹具下「别的租户的文件查不到」会退化成
// 空断言，所以那条反例同时是业务断言和「夹具走对了构造器」的证据。
//
// 默认跳过；设 DSN 才执行（MySQL 侧库名必须留空，见 AGENTS.md）：
//
//	NEXADMIN_IT_MYSQL_DSN='root:pass@tcp(127.0.0.1:3306)/?charset=utf8mb4&parseTime=true&loc=UTC' \
//	NEXADMIN_IT_PG_DSN='host=localhost port=5432 user=postgres password=pass dbname=postgres sslmode=disable' \
//	go test ./modules/file -run TestITKeyword -count=1 -v
package file

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
	"nexadmin/pkg/idgen"
)

const (
	kwTenant      = 900024
	kwOtherTenant = 900124
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
		Name: fmt.Sprintf("nexadmin_kw_file_%d", time.Now().UnixNano()),
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
		Name: fmt.Sprintf("nexadmin_kw_file_%d", time.Now().UnixNano()),
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

// 四个被测文件名：A/C 只有大写拉丁、B 是中文与拉丁混排、D 不含任何被测子串（钉住「不是全捞」）。
const (
	kwFileA = "Region-Report.pdf"
	kwFileB = "月度报表Alpha.xlsx"
	kwFileC = "Export-Center.png"
	kwFileD = "Unrelated-Note.txt"
	// 派生副本（WebP）：List 的 variant_of = 0 条件必须继续把它挡在列表外。
	kwFileVariant = "Region-Report-derivative.webp"
)

// kwSeedFiles 种四行主文件（name / uploader 两列各自带一个大写串），
// 外加「同一名字的原文件派生副本」和「别的租户的一行」两条反例。
func kwSeedFiles(t *testing.T, db *gorm.DB) idgen.ID {
	t.Helper()
	ctx := kwCtx(kwTenant)
	rows := []File{
		{Name: kwFileA, Uploader: "Alice-Zhang", Category: "doc", Visibility: "private"},
		{Name: kwFileB, Uploader: "Bob-Li", Category: "doc", Visibility: "private"},
		{Name: kwFileC, Uploader: "Carol-Wang", Category: "image", Visibility: "private"},
		{Name: kwFileD, Uploader: "Dave", Category: "other", Visibility: "private"},
	}
	var firstID idgen.ID
	for i := range rows {
		f := rows[i]
		f.URL = "https://cdn.example.invalid/" + strconv.Itoa(i) // StoragePath 留空：走外链，不碰本地存储
		f.MimeType = "application/octet-stream"
		if err := db.WithContext(ctx).Create(&f).Error; err != nil {
			t.Fatalf("建文件 %q 失败: %v", f.Name, err)
		}
		if i == 0 {
			firstID = f.ID
		}
	}
	// 派生副本：名字同样含 "Region-Report"，但它不该出现在列表里（variant_of 非 0）。
	v := File{Name: kwFileVariant, Uploader: "Alice-Zhang", Category: "image", Visibility: "private",
		URL: "https://cdn.example.invalid/variant", MimeType: "image/webp", VariantOf: firstID}
	if err := db.WithContext(ctx).Create(&v).Error; err != nil {
		t.Fatalf("建派生副本 %q 失败: %v", v.Name, err)
	}
	// 别的租户的一行：本租户任何关键字都不该把它捞出来。
	other := File{Name: "Other-Tenant-Region-Report.pdf", Uploader: "Eve", Category: "doc",
		Visibility: "private", URL: "https://cdn.example.invalid/other", MimeType: "application/octet-stream"}
	if err := db.WithContext(kwCtx(kwOtherTenant)).Create(&other).Error; err != nil {
		t.Fatalf("建别的租户的文件失败: %v", err)
	}
	return firstID
}

// kwSearchNames 走服务自己的列表路径，返回命中的文件名（升序，便于整串比对）。
func kwSearchNames(t *testing.T, svc *Service, q listQuery) []string {
	t.Helper()
	list, total, err := svc.List(kwCtx(kwTenant), q, 1, 50)
	if err != nil {
		t.Fatalf("List(%+v): %v", q, err)
	}
	if int64(len(list)) != total {
		t.Fatalf("keyword=%q: total=%d 但本页 %d 行（同一 WHERE 不该分叉）", q.Keyword, total, len(list))
	}
	out := make([]string, 0, len(list))
	for _, it := range list {
		out = append(out, it.Name)
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

// kwProbes 逐列探针：name 与 uploader 各若干条，keyword 只出现在被测的那一列上，
// 期望行因此唯一确定。
func kwProbes() []kwProbeSpec {
	return []kwProbeSpec{
		{"region-report", []string{kwFileA}}, // name 列
		{"export-center", []string{kwFileC}}, // name 列
		{"unrelated", []string{kwFileD}},     // name 列（噪声行只能被自己的子串命中）
		{"alice-zhang", []string{kwFileA}},   // uploader 列
		{"bob-li", []string{kwFileB}},        // uploader 列
		{"carol-wang", []string{kwFileC}},    // uploader 列
		{"dave", []string{kwFileD}},          // uploader 列（name 里没有这串字）
	}
}

func runKeywordFoldInvariants(t *testing.T, db *gorm.DB) {
	t.Helper()
	st, err := newStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	svc := NewService(db, st, func(context.Context) Limits { return Limits{} })
	kwSeedFiles(t, db)
	all := []string{kwFileA, kwFileB, kwFileC, kwFileD}

	// 0) 无关键字：四条都在，派生副本不在（分母 + 夹具真落了库）。
	kwExpect(t, "无关键字", kwSearchNames(t, svc, listQuery{}), all...)

	// 1) 逐列探针：改动前这些关键字在 PG 上一律查空，MySQL/MariaDB 照常命中。
	//    同一串文字再按全大写、混排各查一次。
	for _, probe := range kwProbes() {
		if probe.keyword == "dave" {
			continue // 见 kwProbes 注释：这条只用来打 uploader 的另一半，不参与大小写三变体
		}
		kwExpect(t, probe.keyword, kwSearchNames(t, svc, listQuery{Keyword: probe.keyword}), probe.want...)
		kwExpect(t, strings.ToUpper(probe.keyword), kwSearchNames(t, svc, listQuery{Keyword: strings.ToUpper(probe.keyword)}), probe.want...)
		kwExpect(t, "混排 "+kwMixedCase(probe.keyword), kwSearchNames(t, svc, listQuery{Keyword: kwMixedCase(probe.keyword)}), probe.want...)
	}
	// 2) 中文（CJK）：LOWER() 对中文是无操作，关键字照常命中；
	//    「中文 + 拉丁大小写」在同一条谓词里一起命中，证明折叠没把非拉丁文本挤出结果。
	kwExpect(t, "月度报表", kwSearchNames(t, svc, listQuery{Keyword: "月度报表"}), kwFileB)
	kwExpect(t, "alpha", kwSearchNames(t, svc, listQuery{Keyword: "alpha"}), kwFileB)
	kwExpect(t, "ALPHA", kwSearchNames(t, svc, listQuery{Keyword: "ALPHA"}), kwFileB)
	kwExpect(t, "月度报表alpha", kwSearchNames(t, svc, listQuery{Keyword: "月度报表alpha"}), kwFileB)
	kwExpect(t, "月度报表ALPHA", kwSearchNames(t, svc, listQuery{Keyword: "月度报表ALPHA"}), kwFileB)

	// 3) 配对反例：折叠后的谓词不是「恒真」。差一字符、字序颠倒、乱码串都要查不到，
	//    否则上面那批正例就是空跑出来的。
	for _, bad := range []string{"region-reportz", "report-region", "报表度月", "qzxvjk", "zzz-not-here", ".pdf-z"} {
		kwExpect(t, "反例 "+bad, kwSearchNames(t, svc, listQuery{Keyword: bad}))
	}

	// 4) 派生副本没被折叠捞回列表：关键字命中 A 时，同名的 webp 变体仍必须被 variant_of = 0 挡住。
	kwExpect(t, "关键字不越 over variant_of", kwSearchNames(t, svc, listQuery{Keyword: "region-report"}), kwFileA)
	kwExpect(t, "全大写同样不越 over variant_of", kwSearchNames(t, svc, listQuery{Keyword: "REGION-REPORT-DERIVATIVE"}))

	// 5) 租户隔离没被折叠带跑。
	kwExpect(t, "别的租户的行", kwSearchNames(t, svc, listQuery{Keyword: "other-tenant-region-report"}))

	// 6) 与分类筛选叠加时折叠仍生效（分类与关键字是 AND，不互相顶掉）。
	kwExpect(t, "category+keyword 正例", kwSearchNames(t, svc, listQuery{Keyword: "region-report", Category: "doc"}), kwFileA)
	kwExpect(t, "category+keyword 反例", kwSearchNames(t, svc, listQuery{Keyword: "region-report", Category: "image"}))
	// 未分组筛选：夹具都没分组，所以 group=none 时四条都在。
	kwExpect(t, "groupId=none 无关键字", kwSearchNames(t, svc, listQuery{GroupID: "none"}), all...)
	kwExpect(t, "groupId=none + 关键字", kwSearchNames(t, svc, listQuery{GroupID: "none", Keyword: "region-report"}), kwFileA)
	// 按名字排序也走同一条 WHERE：换排序不能改变命中集合。
	kwExpect(t, "sort=name + 关键字", kwSearchNames(t, svc, listQuery{Keyword: "region-report", Sort: "name"}), kwFileA)
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
