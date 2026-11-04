// SSO 接入应用列表关键字大小写折叠的真库守卫（modules/sso/admin_http.go:91）。
//
// 折叠点在 appsQuery 这一处，但它有两个出口：GET /sso/apps（分页列表）与
// GET /sso/apps/export（导出口径，注释里就写着「与 list 完全一致」）。
// 改动前是 `name LIKE ? OR client_id LIKE ?`：PG 的 LIKE 大小写敏感、MySQL/MariaDB 这里的
// 列是 CI 排序，所以关键字带大写字母时只有 PG 部署静默查空。
// 本包原先的用例（apps_batch_delete_test.go / sso_flow_test.go）都用 sqlmock 或内存 store，
// 只比语句文本、从不执行 SQL，照不出这个差异；sso 的 client_id 天生就是混合大小写串，
// 撞上来的概率比用户名还高。
//
// 建库走生产构造器 database.Open：appsQuery 完全不写 tenant_id，租户条件 100% 由
// core/database 的租户插件追加。裸 gorm.Open 夹具下「别的租户的应用查不到」会退化成空断言，
// 所以那条反例同时是业务断言和「夹具走对了构造器」的证据。
//
// 默认跳过；设 DSN 才执行（MySQL 侧库名必须留空，见 AGENTS.md）：
//
//	NEXADMIN_IT_MYSQL_DSN='root:pass@tcp(127.0.0.1:3306)/?charset=utf8mb4&parseTime=true&loc=UTC' \
//	NEXADMIN_IT_PG_DSN='host=localhost port=5432 user=postgres password=pass dbname=postgres sslmode=disable' \
//	go test ./modules/sso -run TestITKeyword -count=1 -v
//
// 说明：这里的关键字同样没做 % _ \ 转义（既有口径，本次折叠没改），所以夹具与探针一律用连字符，
// 保证关键字里没有 LIKE 通配符；反例因此不可能是「被通配符意外挡住」而查空的。
package sso

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
	"nexadmin/core/tenant"
	"nexadmin/pkg/idgen"
)

const (
	kwTenant      = 900027
	kwOtherTenant = 900127
)

// 四条应用名（比对用）：A/C 只有大写拉丁、B 中文与拉丁混排、D 不含任何被测子串。
const (
	kwNameA = "Region-Client"
	kwNameB = "月度报表Alpha"
	kwNameC = "Export-Center"
	kwNameD = "Unrelated-Note"
)

func kwCtx() context.Context { return tenant.With(context.Background(), idgen.ID(kwTenant)) }

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
		Name: fmt.Sprintf("nexadmin_kw_sso_%d", time.Now().UnixNano()),
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
		Name: fmt.Sprintf("nexadmin_kw_sso_%d", time.Now().UnixNano()),
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

// kwSeed 走模型自己的落库路径插四行（tenant_id 由 TenantModel.BeforeCreate 按 ctx 填），
// name 与 client_id 各留一个「只在该列上可识别」的大写串；再补一行别的租户的。
func kwSeed(t *testing.T, db *gorm.DB) {
	t.Helper()
	rows := []App{
		{Name: kwNameA, ClientID: "SSO-Reporter-A", SecretHash: "kw-placeholder"},
		{Name: kwNameB, ClientID: "sso-beta-cjk", SecretHash: "kw-placeholder"},
		{Name: kwNameC, ClientID: "SSO-Gamma-Two", SecretHash: "kw-placeholder"},
		{Name: kwNameD, ClientID: "sso-delta-none", SecretHash: "kw-placeholder"},
	}
	for i := range rows {
		a := rows[i]
		a.Enabled = true
		a.RedirectURIs = `["https://app.example/cb"]`
		if err := db.WithContext(kwCtx()).Create(&a).Error; err != nil {
			t.Fatalf("建接入应用 %q 失败: %v", a.Name, err)
		}
	}
	// 别的租户一行：client_id 全局唯一（uk_sys_sso_app_client），名字里带本租户一模一样的可搜串，
	// 租户条件必须把它挡在外面。
	other := App{Name: "Other-Tenant-" + kwNameA, ClientID: "SSO-Other-Tenant", SecretHash: "kw", Enabled: true,
		RedirectURIs: `["https://other.example/cb"]`}
	if err := db.WithContext(tenant.With(context.Background(), idgen.ID(kwOtherTenant))).Create(&other).Error; err != nil {
		t.Fatalf("建别的租户的接入应用失败: %v", err)
	}
}

// kwRouter 只挂被测的两条读路由 + 租户中间件（生产里租户由 Tenant 中间件写入 ctx）。
func kwRouter(h *handler) *gin.Engine {
	gin.SetMode(gin.TestMode)
	e := gin.New()
	e.Use(func(c *gin.Context) {
		c.Request = c.Request.WithContext(kwCtx())
	})
	e.GET("/sso/apps", h.list)
	e.GET("/sso/apps/export", h.exportApps)
	return e
}

// kwNames 真发一次请求。list 走 data.{list,total}，export 走 data 是裸数组——
// 两个出口共用 appsQuery 的同一套条件，所以这里用同一个函数覆盖两种形状。
func kwNames(t *testing.T, e *gin.Engine, path, keyword string) (names []string, total int64) {
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
	var probe struct {
		Code int             `json:"code"`
		Data json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &probe); err != nil {
		t.Fatalf("解响应失败: %v body=%s", err, w.Body.String())
	}
	if probe.Code != 0 {
		t.Fatalf("code=%d body=%s", probe.Code, w.Body.String())
	}
	trimmed := strings.TrimLeft(string(probe.Data), " \n\t")
	if strings.HasPrefix(trimmed, "[") {
		var arr []appDTO
		if err := json.Unmarshal(probe.Data, &arr); err != nil {
			t.Fatal(err)
		}
		for _, a := range arr {
			names = append(names, a.Name)
		}
		total = int64(len(arr))
	} else {
		var page struct {
			List  []appDTO `json:"list"`
			Total int64    `json:"total"`
		}
		if err := json.Unmarshal(probe.Data, &page); err != nil {
			t.Fatal(err)
		}
		for _, a := range page.List {
			names = append(names, a.Name)
		}
		total = page.Total
	}
	sort.Strings(names)
	return names, total
}

// kwExpectNames 断言命中集合正好是 want；list 出口额外钉住 total == 本页行数
// （count 与 find 分叉是这类折叠最常见的翻车方式：条件只加在其中一条上）。
func kwExpectNames(t *testing.T, label string, e *gin.Engine, path, keyword string, want ...string) {
	t.Helper()
	got, total := kwNames(t, e, path, keyword)
	want = append([]string{}, want...)
	sort.Strings(want)
	if path == "/sso/apps" && total != int64(len(got)) {
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

// kwProbes 逐列探针：name 与 client_id 各若干条，keyword 只出现在被测的那一列上，
// 期望行因此唯一确定。
func kwProbes() []kwProbeSpec {
	return []kwProbeSpec{
		{"region-client", []string{kwNameA}},  // name 列
		{"export-center", []string{kwNameC}},  // name 列
		{"unrelated", []string{kwNameD}},      // name 列（噪声行只能被自己的子串命中）
		{"sso-reporter-a", []string{kwNameA}}, // client_id 列（库里是 "SSO-…"）
		{"sso-gamma-two", []string{kwNameC}},  // client_id 列
		{"sso-beta-cjk", []string{kwNameB}},   // client_id 列（库里已经是小写：对照组）
	}
}

func runKeywordFoldInvariants(t *testing.T, db *gorm.DB) {
	t.Helper()
	h := &handler{db: db}
	kwSeed(t, db)
	e := kwRouter(h)
	all := []string{kwNameA, kwNameB, kwNameC, kwNameD}

	// 两个出口跑同一组断言：折叠点只有一处（appsQuery），但它同时服务列表与导出。
	for _, path := range []string{"/sso/apps", "/sso/apps/export"} {
		// 0) 无关键字：四条都在（分母；别的租户那行不在，钉住夹具与租户条件）。
		kwExpectNames(t, path, e, path, "", all...)

		// 1) 逐列探针：改动前这些关键字在 PG 上一律查空，MySQL/MariaDB 照常命中。
		//    同一串文字再按全大写、混排各查一次。
		for _, probe := range kwProbes() {
			kwExpectNames(t, path+" "+probe.keyword, e, path, probe.keyword, probe.want...)
			kwExpectNames(t, path+" "+strings.ToUpper(probe.keyword), e, path, strings.ToUpper(probe.keyword), probe.want...)
			kwExpectNames(t, path+" 混排 "+kwMixedCase(probe.keyword), e, path, kwMixedCase(probe.keyword), probe.want...)
		}

		// 2) 中文（CJK）：LOWER() 对中文是无操作，关键字照常命中；
		//    「中文 + 拉丁大小写」在同一条谓词里一起命中，证明折叠没把非拉丁文本挤出结果。
		kwExpectNames(t, path+" 月度报表", e, path, "月度报表", kwNameB)
		kwExpectNames(t, path+" alpha", e, path, "alpha", kwNameB)
		kwExpectNames(t, path+" ALPHA", e, path, "ALPHA", kwNameB)
		kwExpectNames(t, path+" 月度报表alpha", e, path, "月度报表alpha", kwNameB)
		kwExpectNames(t, path+" 月度报表ALPHA", e, path, "月度报表ALPHA", kwNameB)

		// 3) 配对反例：折叠后的谓词不是「恒真」。差一字符、字序颠倒、乱码串都要查不到，
		//    否则上面那批正例就是空跑出来的。
		for _, bad := range []string{"region-clientz", "client-region", "报表度月", "qzxvjk", "zzz-not-here"} {
			kwExpectNames(t, path+" 反例 "+bad, e, path, bad)
		}

		// 4) 租户隔离没被折叠带跑。
		kwExpectNames(t, path+" 别的租户的行", e, path, "other-tenant-region-client")
		kwExpectNames(t, path+" 别的租户的行（全大写）", e, path, "OTHER-TENANT-REGION-CLIENT")
	}

	// 5) 只含空空格的关键字不加条件（TrimSpace 分支），口径与无关键字一致。
	kwExpectNames(t, "list 纯空格关键字", e, "/sso/apps", "   ", all...)
	kwExpectNames(t, "export 纯空格关键字", e, "/sso/apps/export", "   ", all...)
	// 6) 列表与导出对同一个关键字必须给出同一集合（两条出口共用 appsQuery 的承诺）。
	listNames, _ := kwNames(t, e, "/sso/apps", "sso-reporter-a")
	exportNames, _ := kwNames(t, e, "/sso/apps/export", "sso-reporter-a")
	if strings.Join(listNames, "|") != strings.Join(exportNames, "|") {
		t.Errorf("同一关键字下列表 %v 与导出 %v 口径分叉", listNames, exportNames)
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
