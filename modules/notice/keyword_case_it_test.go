// 公告关键字筛选大小写折叠的真库守卫。
//
// modules/notice/service.go（管理端列表 List）与 modules/notice/audience.go（我的公告 Mine）
// 的 keyword 条件从 `title LIKE ?` 改成了 `LOWER(title) LIKE LOWER(?)`。
// PG 的 LIKE 大小写敏感、MySQL/MariaDB 的列是 CI 排序，改之前关键字带大写字母时
// PG 部署静默查空——而 sqlmock 只比对语句文本、从不执行 SQL，两边都「通过」，照不出这个差异。
// 本文件在真引擎上跑「同一串文字、不同大小写」的关键字，两条路径（List / Mine）各断一次。
//
// 夹具与 modules/recycle/soft_delete_it_test.go 同形（一次性库 + 全量框架迁移 + 库名留空的 DSN 守卫），
// 只是表与被测服务不同：
//
//	NEXADMIN_IT_MYSQL_DSN='root:pass@tcp(127.0.0.1:3306)/?charset=utf8mb4&parseTime=true&loc=UTC' \
//	NEXADMIN_IT_PG_DSN='host=localhost port=5432 user=postgres password=pass dbname=postgres sslmode=disable' \
//	go test ./modules/notice -run TestITKeyword -count=1 -v
package notice

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/go-sql-driver/mysql"
	gormmysql "gorm.io/driver/mysql"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"nexadmin/core/datascope"
	"nexadmin/core/migrate"
	"nexadmin/core/tenant"
	"nexadmin/pkg/idgen"
)

// itTenant 取一个远离演示数据的租户号，避免和同机其它真库用例串台。
const itTenant = 900011

// itUID 是「我的公告」用例的读者：夹具库里没有用户行，audience() 查不到部门/角色
// 只会回 0 与空集，而用例种的公告都是 scope=all，可见性不依赖它们。
const itUID = idgen.ID(7300000000000910001)

func itCtx() context.Context { return tenant.With(context.Background(), itUID) }

func itDSNMySQL(t *testing.T) (rootDSN, dbDSN, dbname string) {
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
	name := fmt.Sprintf("nexadmin_noticeit_%d", time.Now().UnixNano())
	root := d.FormatDSN()
	d.DBName = name
	return root, d.FormatDSN(), name
}

// itOpenMySQL 建一次性库（库名由时间戳生成，不是请求输入）并跑完 mysql/mariadb 目录的框架迁移。
func itOpenMySQL(t *testing.T, engine string) *gorm.DB {
	t.Helper()
	rootDSN, dbDSN, name := itDSNMySQL(t)
	root, err := sql.Open("mysql", rootDSN)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := root.Exec("CREATE DATABASE " + name + " CHARACTER SET utf8mb4"); err != nil {
		_ = root.Close()
		t.Fatalf("建一次性库失败: %v", err)
	}
	// 清场排在最后：先删库再关连接（同 recycle 的夹具）。
	t.Cleanup(func() {
		if _, err := root.Exec("DROP DATABASE " + name); err != nil {
			t.Errorf("DROP DATABASE %s: %v", name, err)
		}
		_ = root.Close()
	})
	db, err := gorm.Open(gormmysql.Open(dbDSN), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	itApplyMigrations(t, db, engine)
	return db
}

// itOpenPG 同样建一次性库；DSN 里已写 dbname 的按原样替换，没写则追加。
func itOpenPG(t *testing.T) *gorm.DB {
	t.Helper()
	raw := os.Getenv("NEXADMIN_IT_PG_DSN")
	if raw == "" {
		t.Skip("NEXADMIN_IT_PG_DSN not set; skipping real-PostgreSQL integration test")
	}
	name := fmt.Sprintf("nexadmin_noticeit_%d", time.Now().UnixNano())
	root, err := gorm.Open(postgres.Open(raw), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatalf("PG 连不上: %v", err)
	}
	if err := root.Exec("CREATE DATABASE " + name).Error; err != nil {
		t.Fatalf("建一次性库失败: %v", err)
	}
	sqlDB, err := root.DB()
	if err != nil {
		t.Fatal(err)
	}
	dsn := raw
	if i := strings.Index(raw, "dbname="); i >= 0 {
		end := len(raw)
		for j := i + len("dbname="); j < len(raw); j++ {
			if raw[j] == ' ' || raw[j] == ';' {
				end = j
				break
			}
		}
		dsn = raw[:i] + "dbname=" + name + raw[end:]
	} else {
		dsn = raw + " dbname=" + name
	}
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	itApplyMigrations(t, db, "postgres")
	t.Cleanup(func() {
		// 先断开连到一次性库的那一路会话，再 DROP：PG 在有连接时删库直接报 55006。
		if d, derr := db.DB(); derr == nil {
			_ = d.Close()
		}
		if _, err := sqlDB.Exec("DROP DATABASE " + name); err != nil {
			t.Errorf("DROP DATABASE %s: %v", name, err)
		}
		_ = sqlDB.Close()
	})
	return db
}

// itApplyMigrations 按文件名顺序执行 migrations/framework/<engine>/ 下全部 SQL：
// 列的字符集与排序规则跟生产一致，MySQL 侧的 CI 行为才是真的，不是夹具自己造的。
func itApplyMigrations(t *testing.T, db *gorm.DB, engine string) {
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
				t.Fatalf("%s: %v\n语句：%s", filepath.Base(f), err, firstLineOf(stmt))
			}
		}
	}
}

func firstLineOf(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}

// kwSeedTitles 四条公告标题：三条含大写字母（其中一条中文与拉丁混排），
// 一条不含任何被测子串，用来钉住「折叠不是把所有行都捞出来」。
var kwSeedTitles = []string{"Region-Report", "Export-Center", "月度报表Alpha", "Unrelated-Note"}

// kwSeedNotices 走模型自己的落库路径插公告（tenant_id 由 TenantModel.BeforeCreate 按 ctx 填），
// 全部已发布 + 面向全员，管理端列表与「我的公告」两条路径都看得到。
func kwSeedNotices(t *testing.T, db *gorm.DB) {
	t.Helper()
	now := time.Now().UTC()
	for _, title := range kwSeedTitles {
		n := &Notice{
			Title: title, Type: "notice", Level: "normal", Status: StatusPublished,
			Content: "<p>keyword fold fixture</p>", Scope: ScopeAll,
			PublishedAt: &now, CreatedBy: itUID,
		}
		if err := db.WithContext(itCtx()).Create(n).Error; err != nil {
			t.Fatalf("建公告 %q 失败: %v", title, err)
		}
	}
	// 一条草稿：管理端列表（不限状态）应当看得到它，「我的公告」看不到——
	// 它同时是「关键字命中随行可见性一起走」的反向配平，避免可见性坏了却把行数凑对。
	draft := &Notice{Title: "Drafted-Omega", Type: "notice", Level: "normal", Status: StatusDraft,
		Content: "<p>draft</p>", Scope: ScopeAll, CreatedBy: itUID}
	if err := db.WithContext(itCtx()).Create(draft).Error; err != nil {
		t.Fatalf("建草稿公告失败: %v", err)
	}
}

// kwAdminTitles 走管理端 List（service.go 的 keyword 分支）。
func kwAdminTitles(t *testing.T, svc *Service, keyword string) []string {
	t.Helper()
	list, total, err := svc.List(itCtx(), ListQuery{Keyword: keyword, Page: 1, PageSize: 50, Scope: &datascope.Scope{All: true}})
	if err != nil {
		t.Fatalf("List(keyword=%q): %v", keyword, err)
	}
	if int64(len(list)) != total {
		t.Fatalf("keyword=%q: total=%d 但本页 %d 行（同一 WHERE 不该分叉）", keyword, total, len(list))
	}
	out := make([]string, 0, len(list))
	for _, v := range list {
		out = append(out, v.Title)
	}
	sort.Strings(out)
	return out
}

// kwMyTitles 走读者侧 Mine（audience.go 的 keyword 分支，另一条独立拼 SQL 的路径）。
func kwMyTitles(t *testing.T, svc *Service, keyword string) []string {
	t.Helper()
	list, total, err := svc.Mine(itCtx(), itUID, keyword, "", false, 1, 50)
	if err != nil {
		t.Fatalf("Mine(keyword=%q): %v", keyword, err)
	}
	if int64(len(list)) != total {
		t.Fatalf("Mine keyword=%q: total=%d 但本页 %d 行", keyword, total, len(list))
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
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Errorf("%s 命中 %v，期望 %v", name, got, want)
	}
}

// runKeywordFoldInvariants 是引擎无关的一组断言，MySQL / MariaDB / PG 各跑一遍。
func runKeywordFoldInvariants(t *testing.T, db *gorm.DB) {
	t.Helper()
	svc := NewService(db)
	kwSeedNotices(t, db)
	all := append(append([]string{}, kwSeedTitles...), "Drafted-Omega")
	sort.Strings(all)

	// 两条路径共用同一组关键字断言：folded 站点是两个（List 与 Mine），各断一次。
	for _, path := range []struct {
		name string
		run  func(*testing.T, string) []string
	}{{"List", func(t *testing.T, k string) []string { return kwAdminTitles(t, svc, k) }},
		{"Mine", func(t *testing.T, k string) []string { return kwMyTitles(t, svc, k) }}} {
		// Mine 只给已发布的，所以期望集合按路径分开算。
		wantAll := all
		if path.name == "Mine" {
			wantAll = append([]string{}, kwSeedTitles...)
			sort.Strings(wantAll)
		}
		// 0) 无关键字：分母（钉住夹具真落了库、可见性正常）。
		kwExpect(t, path.name+" 无关键字", path.run(t, ""), wantAll...)

		// 1) 改动前会在 PG 上失败的正例：种的是大写、查的是同一串文字的小写形式。
		kwExpect(t, path.name+" region-report", path.run(t, "region-report"), "Region-Report")
		kwExpect(t, path.name+" REGION-REPORT", path.run(t, "REGION-REPORT"), "Region-Report")
		kwExpect(t, path.name+" export-center", path.run(t, "export-center"), "Export-Center")
		kwExpect(t, path.name+" EXPORT-CENTER", path.run(t, "EXPORT-CENTER"), "Export-Center")
		// 混排大小写同样要命中（不是只兜住「全小写」这一种输入）。
		kwExpect(t, path.name+" eXpOrT-cEnTeR", path.run(t, "eXpOrT-cEnTeR"), "Export-Center")

		// 2) 中文（CJK）：LOWER() 对中文是无操作，关键字照常命中；
		//    「中文 + 拉丁大小写」在同一条谓词里一起命中，证明折叠没把非拉丁文本挤出结果。
		kwExpect(t, path.name+" 月度报表", path.run(t, "月度报表"), "月度报表Alpha")
		kwExpect(t, path.name+" alpha", path.run(t, "alpha"), "月度报表Alpha")
		kwExpect(t, path.name+" ALPHA", path.run(t, "ALPHA"), "月度报表Alpha")
		kwExpect(t, path.name+" 月度报表alpha", path.run(t, "月度报表alpha"), "月度报表Alpha")

		// 3) 配对反例：折叠后的谓词不是「恒真」。差一字符、字序颠倒、乱码串都要查不到，
		//    否则上面那批正例就是空跑出来的。
		kwExpect(t, path.name+" region-reportz", path.run(t, "region-reportz"))
		kwExpect(t, path.name+" repor-region", path.run(t, "repor-region"))
		kwExpect(t, path.name+" 报表度月", path.run(t, "报表度月"))
		kwExpect(t, path.name+" qzxvjk", path.run(t, "qzxvjk"))
	}

	// 4) 可见性没被折叠带跑：草稿只出现在管理端列表，读者侧查不到它。
	kwExpect(t, "List drafted-omega", kwAdminTitles(t, svc, "drafted-omega"), "Drafted-Omega")
	kwExpect(t, "Mine drafted-omega", kwMyTitles(t, svc, "drafted-omega"))
	// 与关键字叠加的类型筛选仍生效（两个条件是 AND 关系，不是互相顶掉）。
	list, total, err := svc.List(itCtx(), ListQuery{Keyword: "region-report", Type: "notice", Page: 1, PageSize: 10, Scope: &datascope.Scope{All: true}})
	if err != nil || total != 1 || len(list) != 1 || list[0].Title != "Region-Report" {
		t.Fatalf("type+keyword 叠加：期望 1 行 Region-Report，实得 total=%d %+v err=%v", total, list, err)
	}
	if _, total2, err := svc.List(itCtx(), ListQuery{Keyword: "region-reportz", Type: "notice", Page: 1, PageSize: 10, Scope: &datascope.Scope{All: true}}); err != nil || total2 != 0 {
		t.Fatalf("type+keyword 反例期望 0 行，实得 total=%d err=%v", total2, err)
	}
}

func TestITMySQLKeywordFoldCase(t *testing.T) {
	if err := idgen.Init(31); err != nil {
		t.Fatal(err)
	}
	runKeywordFoldInvariants(t, itOpenMySQL(t, "mysql"))
}

func TestITMariaDBKeywordFoldCase(t *testing.T) {
	if err := idgen.Init(31); err != nil {
		t.Fatal(err)
	}
	runKeywordFoldInvariants(t, itOpenMySQL(t, "mariadb"))
}

func TestITPostgresKeywordFoldCase(t *testing.T) {
	if err := idgen.Init(31); err != nil {
		t.Fatal(err)
	}
	runKeywordFoldInvariants(t, itOpenPG(t))
}
