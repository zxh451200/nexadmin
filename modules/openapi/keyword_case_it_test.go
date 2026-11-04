// 开放平台「应用」与「Webhook」列表关键字大小写折叠的真库守卫
// （modules/openapi/apps.go:131 与 modules/openapi/webhooks.go:70）。
//
// 改动前：`name LIKE ? OR app_key LIKE ?` / `name LIKE ? OR url LIKE ?`；
// 改动后每个列各自 LOWER(col) LIKE LOWER(?)。
// PG 的 LIKE 大小写敏感、MySQL/MariaDB 这里的列是 CI 排序，所以关键字里带大写字母时
// PG 部署会静默查出 0 行——而 app_key / webhook url 天生就是大小写混排的（"NXAPP_…"、
// "https://Hooks.Example/Region-Report"），运维照着列表页粘贴一个名字就搜不到，
// 现场只会表现为「应用明明在，列表里查不到」。
//
// 两个站点都是 HTTP handler，所以这里真起一条 gin 路由跑完整请求（不绕过 handler 直接拼 SQL），
// 顺带把「列表与导出/分页口径一致」钉住：total 必须等于本页行数。
//
// 建库走生产构造器 database.Open：两个 handler 都不写 tenant_id，租户条件 100% 由
// core/database 的租户插件追加。裸 gorm.Open 夹具下「别的租户的应用查不到」会退化成空断言，
// 所以那条反例同时是业务断言和「夹具走对了构造器」的证据。
//
// 默认跳过；设 DSN 才执行（MySQL 侧库名必须留空，见 AGENTS.md）：
//
//	NEXADMIN_IT_MYSQL_DSN='root:pass@tcp(127.0.0.1:3306)/?charset=utf8mb4&parseTime=true&loc=UTC' \
//	NEXADMIN_IT_PG_DSN='host=localhost port=5432 user=postgres password=pass dbname=postgres sslmode=disable' \
//	go test ./modules/openapi -run TestITKeyword -count=1 -v
//
// 说明：这两处关键字没有做 % _ \ 转义（与 service 层那几个站点不同，是既有口径，本次折叠没改它），
// 所以关键字里的下划线在 LIKE 里仍是「任意单字符」通配。本文件不改动这个口径，只在它旁边把
// 大小写折叠证明白：探针取的都是整段唯一的名字（"NXAPP_Reporter_A" 这种），通配只会让同一行
// 内的字符更容易对上，不会让别的行蒙进来；所有反例都不含 % _ \，因此「反例查到 0 行」
// 不是因为通配符意外挡住的。转义口径本身不在这里钉（apps/webhooks 的 handler 没有转义，
// 那是另一个议题）。
package openapi

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/go-sql-driver/mysql"
	gormpg "gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"nexadmin/core/config"
	"nexadmin/core/database"
	"nexadmin/core/migrate"
	coreoa "nexadmin/core/openapi"
	"nexadmin/core/tenant"
	"nexadmin/pkg/idgen"
)

const (
	kwTenant      = 900026
	kwOtherTenant = 900126
)

func kwCtxID() idgen.ID { return idgen.ID(kwTenant) }

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
		Name: fmt.Sprintf("nexadmin_kw_openapi_%d", time.Now().UnixNano()),
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
		Name: fmt.Sprintf("nexadmin_kw_openapi_%d", time.Now().UnixNano()),
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

// 四条应用名（比对用）：A/C 只有大写拉丁、B 中文与拉丁混排、D 不含任何被测子串。
const (
	kwAppA = "Region-Report"
	kwAppB = "月度报表Alpha"
	kwAppC = "Export-Center"
	kwAppD = "Unrelated-Note"

	kwHookA = "Region-Hook"
	kwHookB = "月度报表Hook"
	kwHookC = "Export-Hook"
	kwHookD = "Unrelated-Hook"
)

// kwSeed 走模型自己的落库路径（tenant_id 由 TenantModel.BeforeCreate 按 ctx 填）：
// 四条应用 + 四条 Webhook，另外两条反例行（别的租户各一条）。
func kwSeed(t *testing.T, db *gorm.DB) {
	t.Helper()
	ctx := tenant.With(context.Background(), kwCtxID())
	apps := []coreoa.App{
		{Name: kwAppA, AppKey: "NXAPP-Reporter-A", SecretEnc: "kw-placeholder"},
		{Name: kwAppB, AppKey: "nxapp-beta-cjk", SecretEnc: "kw-placeholder"},
		{Name: kwAppC, AppKey: "NXAPP-Gamma-Two", SecretEnc: "kw-placeholder"},
		{Name: kwAppD, AppKey: "nxapp-delta-none", SecretEnc: "kw-placeholder"},
	}
	for i := range apps {
		a := apps[i]
		a.Enabled = true
		if err := db.WithContext(ctx).Create(&a).Error; err != nil {
			t.Fatalf("建开放应用 %q 失败: %v", a.Name, err)
		}
	}
	hooks := []Webhook{
		{Name: kwHookA, URL: "https://hooks.example/Region-Report", SecretEnc: "kw-placeholder"},
		{Name: kwHookB, URL: "https://hooks.example/Alpha-CJK", SecretEnc: "kw-placeholder"},
		{Name: kwHookC, URL: "https://hooks.example/Export-Task", SecretEnc: "kw-placeholder"},
		{Name: kwHookD, URL: "https://hooks.example/none", SecretEnc: "kw-placeholder"},
	}
	for i := range hooks {
		w := hooks[i]
		w.Enabled = true
		w.Events = `["user.created"]`
		if err := db.WithContext(ctx).Create(&w).Error; err != nil {
			t.Fatalf("建 Webhook %q 失败: %v", w.Name, err)
		}
	}
	// 别的租户各一条：名字里带本租户一模一样的可搜串，租户条件必须把它们挡在外面。
	if err := db.WithContext(tenant.With(context.Background(), idgen.ID(kwOtherTenant))).Create(&coreoa.App{
		Name: "Other-Tenant-Region-Report", AppKey: "NXAPP-Other-Tenant", SecretEnc: "kw", Enabled: true}).Error; err != nil {
		t.Fatalf("建别的租户的开放应用失败: %v", err)
	}
	if err := db.WithContext(tenant.With(context.Background(), idgen.ID(kwOtherTenant))).Create(&Webhook{
		Name: "Other-Tenant-Region-Hook", URL: "https://other.example/Region-Report", SecretEnc: "kw", Enabled: true,
		Events: `["user.created"]`}).Error; err != nil {
		t.Fatalf("建别的租户的 Webhook 失败: %v", err)
	}
}

// kwRouter 只挂被测的两条列表路由 + 租户中间件（生产里租户由 Tenant 中间件写入 ctx）。
func kwRouter(h *handler) *gin.Engine {
	gin.SetMode(gin.TestMode)
	e := gin.New()
	e.Use(func(c *gin.Context) {
		c.Request = c.Request.WithContext(tenant.With(c.Request.Context(), kwCtxID()))
	})
	e.GET("/openapi/apps", h.appList)
	e.GET("/openapi/webhooks", h.hookList)
	return e
}

// kwGet 真发一次请求，返回 (升序的名字, data.total)。
// 名字列由调用方指定：应用列表取 name，Webhook 列表取 name+url 里的可搜两列之一。
func kwGet(t *testing.T, e *gin.Engine, path, keyword string) (names []string, total int64) {
	t.Helper()
	target := path + "?page=1&pageSize=50"
	if keyword != "" {
		target += "&keyword=" + url.QueryEscape(keyword)
	}
	w := httptest.NewRecorder()
	e.ServeHTTP(w, httptest.NewRequest(http.MethodGet, target, nil))
	if w.Code != http.StatusOK {
		t.Fatalf("GET %s?keyword=%q => %d %s", path, keyword, w.Code, w.Body.String())
	}
	var body struct {
		Code int `json:"code"`
		Data struct {
			List  []json.RawMessage `json:"list"`
			Total int64             `json:"total"`
		} `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("解响应失败: %v body=%s", err, w.Body.String())
	}
	if body.Code != 0 {
		t.Fatalf("code=%d body=%s", body.Code, w.Body.String())
	}
	names = make([]string, 0, len(body.Data.List))
	for _, raw := range body.Data.List {
		var row struct {
			Name string `json:"name"`
		}
		if err := json.Unmarshal(raw, &row); err != nil {
			t.Fatal(err)
		}
		names = append(names, row.Name)
	}
	sort.Strings(names)
	return names, body.Data.Total
}

// kwExpectList 断言命中集合正好是 want，并顺带钉住 total == 本页行数
// （count 与 find 分叉是这类折叠最常见的翻车方式：条件只加在其中一条上）。
func kwExpectList(t *testing.T, label string, e *gin.Engine, path, keyword string, want ...string) {
	t.Helper()
	got, total := kwGet(t, e, path, keyword)
	want = append([]string{}, want...)
	sort.Strings(want)
	if total != int64(len(got)) {
		t.Errorf("%s keyword=%q: total=%d 但本页 %d 行（同一 WHERE 不该分叉）", label, keyword, total, len(got))
	}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Errorf("%s keyword=%q 命中 %v，期望 %v", label, keyword, got, want)
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

// kwAppProbes / kwHookProbes：name 列与第二列（app_key / url）各若干条，
// keyword 只出现在被测的那一列上，期望行因此唯一确定。
func kwAppProbes() []kwProbeSpec {
	return []kwProbeSpec{
		{"region-report", []string{kwAppA}},    // name 列
		{"export-center", []string{kwAppC}},    // name 列
		{"unrelated", []string{kwAppD}},        // name 列（噪声行只能被自己的子串命中）
		{"nxapp-reporter-a", []string{kwAppA}}, // app_key 列（库里是 "NXAPP-…"）
		{"nxapp-gamma-two", []string{kwAppC}},  // app_key 列
		{"nxapp-beta-cjk", []string{kwAppB}},   // app_key 列（库里已经是小写：对照组）
	}
}

func kwHookProbes() []kwProbeSpec {
	return []kwProbeSpec{
		{"region-hook", []string{kwHookA}},                       // name 列
		{"export-hook", []string{kwHookC}},                       // name 列
		{"unrelated", []string{kwHookD}},                         // name 列（噪声行只能被自己的子串命中）
		{"region-report", []string{kwHookA}},                     // url 列（库里是 "/Region-Report"）
		{"alpha-cjk", []string{kwHookB}},                         // url 列（库里是 "/Alpha-CJK"）
		{"export-task", []string{kwHookC}},                       // url 列
		{"https://hooks.example/export-task", []string{kwHookC}}, // url 列：整串
	}
}

func runKeywordFoldInvariants(t *testing.T, db *gorm.DB) {
	t.Helper()
	h := &handler{db: db}
	kwSeed(t, db)
	e := kwRouter(h)

	apps := []string{kwAppA, kwAppB, kwAppC, kwAppD}
	hooks := []string{kwHookA, kwHookB, kwHookC, kwHookD}

	for _, p := range []struct {
		name      string
		path      string
		all       []string
		cjk       string // 中文与拉丁混排那行的名字
		cjkJoined string // 「中文片段 + 拉丁片段」连在一起的关键字，同一条谓词里必须一起命中
		probes    []kwProbeSpec
		other     string // 别的租户那行的关键字，必须查不到
	}{
		{"apps", "/openapi/apps", apps, kwAppB, "月度报表alpha", kwAppProbes(), "other-tenant-region-report"},
		{"webhooks", "/openapi/webhooks", hooks, kwHookB, "月度报表hook", kwHookProbes(), "other-tenant-region-hook"},
	} {
		// 0) 无关键字：四条都在（分母；别的租户那两条不在，钉住夹具与租户条件）。
		kwExpectList(t, p.name, e, p.path, "", p.all...)

		// 1) 逐列探针：改动前这些关键字在 PG 上一律查空，MySQL/MariaDB 照常命中。
		//    同一串文字再按全大写、混排各查一次。
		for _, probe := range p.probes {
			kwExpectList(t, p.name+" "+probe.keyword, e, p.path, probe.keyword, probe.want...)
			kwExpectList(t, p.name+" "+strings.ToUpper(probe.keyword), e, p.path, strings.ToUpper(probe.keyword), probe.want...)
			kwExpectList(t, p.name+" 混排 "+kwMixedCase(probe.keyword), e, p.path, kwMixedCase(probe.keyword), probe.want...)
		}

		// 2) 中文（CJK）：LOWER() 对中文是无操作，关键字照常命中；
		//    「中文 + 拉丁大小写」在同一条谓词里一起命中，证明折叠没把非拉丁文本挤出结果。
		kwExpectList(t, p.name+" 月度报表", e, p.path, "月度报表", p.cjk)
		kwExpectList(t, p.name+" alpha", e, p.path, "alpha", p.cjk)
		kwExpectList(t, p.name+" ALPHA", e, p.path, "ALPHA", p.cjk)
		kwExpectList(t, p.name+" 中文+拉丁", e, p.path, p.cjkJoined, p.cjk)
		kwExpectList(t, p.name+" 中文+拉丁（全大写）", e, p.path, strings.ToUpper(p.cjkJoined), p.cjk)

		// 3) 配对反例：折叠后的谓词不是「恒真」。差一字符、字序颠倒、乱码串都要查不到，
		//    否则上面那批正例就是空跑出来的。
		for _, bad := range []string{"region-reportz", "report-region", "报表度月", "qzxvjk", "zzz-not-here"} {
			kwExpectList(t, p.name+" 反例 "+bad, e, p.path, bad)
		}

		// 4) 租户隔离没被折叠带跑。
		kwExpectList(t, p.name+" 别的租户的行", e, p.path, p.other)
		kwExpectList(t, p.name+" 别的租户的行（全大写）", e, p.path, strings.ToUpper(p.other))
	}

	// 5) 空 keyword 与只含空格的 keyword 都不该加条件（TrimSpace 分支），
	//    且「全部」的口径与无关键字一致——这条也顺带钉住两列的 OR 没有变成 AND。
	kwExpectList(t, "apps 纯空格关键字", e, "/openapi/apps", "   ", kwAppA, kwAppB, kwAppC, kwAppD)
	kwExpectList(t, "webhooks 纯空格关键字", e, "/openapi/webhooks", "   ", kwHookA, kwHookB, kwHookC, kwHookD)

	// 6) 分页与关键字叠加：pageSize=1 时 total 仍是命中总数（count 与 find 同一套条件）。
	got, total := kwGet(t, e, "/openapi/apps", "region-report")
	if total != 1 || len(got) != 1 {
		t.Fatalf("apps keyword=region-report 期望 1 行/total=1，实得 %d 行 total=%d", len(got), total)
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
