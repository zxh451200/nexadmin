// 关键字筛选大小写折叠的真库守卫。
//
// modules/recycle/service.go 的 keyword 条件从 `name LIKE ?` 改成了
// `LOWER(name) LIKE LOWER(?)`：PG 的 LIKE 大小写敏感（列没有 CI 排序规则），MySQL/MariaDB
// 的列是 CI 排序，所以改之前——关键字里带任意大写字母时——PG 部署会静默查出 0 行，MySQL 部署照常命中。
// 这个差异 sqlmock 照不出来（它只比对语句文本，从不执行 SQL，两边都「通过」），只有真引擎能证明：
// 本文件在三种引擎上各跑一遍「同一串文字、不同大小写」的关键字，断言能捞到那行。
//
// 夹具（一次性库 + 全量框架迁移）复用 soft_delete_it_test.go 的 itOpenMySQL / itOpenPG /
// itCtx / itTenant，本文件只补断言，不另起一套 harness：
//
//	NEXADMIN_IT_MYSQL_DSN='root:pass@tcp(127.0.0.1:3306)/?charset=utf8mb4&parseTime=true&loc=UTC' \
//	NEXADMIN_IT_PG_DSN='host=localhost port=5432 user=postgres password=pass dbname=postgres sslmode=disable' \
//	go test ./modules/recycle -run TestITKeyword -count=1 -v
//
// 关键字里 % _ \ 的转义口径不在这里重复验证，由 TestListFiltersEscapesKeywordAndPaginates 钉住。
package recycle

import (
	"sort"
	"strings"
	"testing"

	"go.uber.org/zap"
	"gorm.io/gorm"

	"nexadmin/modules/internal/recbin"
	"nexadmin/pkg/idgen"
)

// kwSeedNames 打进回收站的展示名：三条含大写字母（其中一条中文与拉丁混排），
// 一条不含任何被测子串，用来钉住「折叠不是把所有行都捞出来」。
var kwSeedNames = []string{"Region-Report", "Export-Center", "月度报表Alpha", "Unrelated-Note"}

// kwSearch 用关键字走服务自己的列表路径，返回命中的名字（升序，便于整串比对）。
// total 与本页行数必须一致：同一个 WHERE 下不一致说明 count 与 find 的条件分叉了。
func kwSearch(t *testing.T, svc *Service, keyword string) []string {
	t.Helper()
	list, total, err := svc.List(itCtx(), ListFilter{Keyword: keyword, Page: 1, Size: 50})
	if err != nil {
		t.Fatalf("List(keyword=%q): %v", keyword, err)
	}
	if int64(len(list)) != total {
		t.Fatalf("keyword=%q: total=%d 但本页 %d 行（同一 WHERE 不该分叉）", keyword, total, len(list))
	}
	names := make([]string, 0, len(list))
	for _, it := range list {
		names = append(names, it.Name)
	}
	sort.Strings(names)
	return names
}

// kwExpect 断言关键字命中的行集合正好是 want（两侧都排序后整串比对）。
func kwExpect(t *testing.T, svc *Service, keyword string, want ...string) {
	t.Helper()
	want = append([]string{}, want...)
	sort.Strings(want)
	got := kwSearch(t, svc, keyword)
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Errorf("keyword=%q 命中 %v，期望 %v", keyword, got, want)
	}
}

func runKeywordFoldInvariants(t *testing.T, db *gorm.DB) {
	t.Helper()
	svc := NewService(db, zap.NewNop())
	ctx := itCtx()

	for i, name := range kwSeedNames {
		ref := idgen.ID(7300000000000900000 + int64(i))
		if err := recbin.Add(ctx, db, recbin.TypePost, ref, name, "it"); err != nil {
			t.Fatalf("登记回收记录 %q 失败: %v", name, err)
		}
	}

	// 0) 无关键字：四条都在（后面每条断言的分母，也钉住「夹具真落了库」）。
	kwExpect(t, svc, "", kwSeedNames...)

	// 1) 改动前会在 PG 上失败的正例：种的是大写、查的是同一串文字的小写形式。
	kwExpect(t, svc, "region-report", "Region-Report")
	kwExpect(t, svc, "REGION-REPORT", "Region-Report")
	kwExpect(t, svc, "export-center", "Export-Center")
	kwExpect(t, svc, "EXPORT-CENTER", "Export-Center")
	// 混排大小写同样要命中（不是只把首字母小写这一种情况兜住了）。
	kwExpect(t, svc, "eXpOrT-cEnTeR", "Export-Center")

	// 2) 中文（CJK）：LOWER() 对中文是无操作，关键字仍要照常命中；
	//    以及「中文 + 拉丁大小写」同一条谓词里一起命中，证明折叠没把非拉丁文本挤出结果。
	kwExpect(t, svc, "月度报表", "月度报表Alpha")
	kwExpect(t, svc, "alpha", "月度报表Alpha")
	kwExpect(t, svc, "ALPHA", "月度报表Alpha")
	kwExpect(t, svc, "月度报表alpha", "月度报表Alpha")

	// 3) 配对反例：折叠后的谓词不是「恒真」。长度差一字符、字序颠倒、
	//    乱码串都必须查不到——否则上面那批正例是空跑出来的。
	kwExpect(t, svc, "region-reportz")
	kwExpect(t, svc, "repor-region")
	kwExpect(t, svc, "报表度月")
	kwExpect(t, svc, "qzxvjk")
	// 反例对噪声行同样成立：Unrelated-Note 只能被自己的子串命中。
	kwExpect(t, svc, "unrelated", "Unrelated-Note")
	kwExpect(t, svc, "UNRELATED-NOTE", "Unrelated-Note")

	// 4) 关键字与类型筛选叠加时折叠仍生效（type 条件不能把 keyword 条件顶掉）。
	list, total, err := svc.List(ctx, ListFilter{Type: recbin.TypePost, Keyword: "region-report", Page: 1, Size: 10})
	if err != nil {
		t.Fatal(err)
	}
	if total != 1 || len(list) != 1 || list[0].Name != "Region-Report" {
		t.Fatalf("type+keyword 叠加：期望 1 行 Region-Report，实得 total=%d %+v", total, list)
	}
	if _, total2, err := svc.List(ctx, ListFilter{Type: recbin.TypePost, Keyword: "region-reportz", Page: 1, Size: 10}); err != nil || total2 != 0 {
		t.Fatalf("type+keyword 的反例期望 0 行，实得 total=%d err=%v", total2, err)
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
