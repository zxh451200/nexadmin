package region

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"nexadmin/core"
	"nexadmin/core/auth"
	"nexadmin/core/config"
	coreregion "nexadmin/core/region"
	"nexadmin/pkg/idgen"
)

func newEnv(t *testing.T) (*gin.Engine, sqlmock.Sqlmock) {
	return newEnvWithSource(t, "https://example.com/regions.json", "MIT")
}

func newEnvWithSource(t *testing.T, source, license string) (*gin.Engine, sqlmock.Sqlmock) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	if err := idgen.Init(1); err != nil {
		t.Fatal(err)
	}
	sqlDB, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	db, err := gorm.Open(mysql.New(mysql.Config{Conn: sqlDB, SkipInitializeWithVersion: true}), &gorm.Config{DisableAutomaticPing: true, Logger: logger.Discard})
	if err != nil {
		t.Fatal(err)
	}
	// Register 启动装载：库里没有快照
	mock.ExpectQuery("SELECT \\* FROM `sys_region_snapshot`").WillReturnError(gorm.ErrRecordNotFound)
	e := gin.New()
	e.Use(func(c *gin.Context) {
		auth.SetClaims(c, &auth.Claims{
			UserID: 9, Username: "app:test", Principal: auth.PrincipalApp,
			Perms: strings.Split(c.GetHeader("X-Test-Perms"), ","),
		})
	})
	cfg := &config.Config{}
	cfg.Region.SourceURL = source
	cfg.Region.SourceLicense = license
	module{}.Register(e.Group("/api/v1"), &core.Deps{Config: cfg, DB: db, Log: zap.NewNop()})
	return e, mock
}

func do(e *gin.Engine, method, path, body, perms string) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	r := httptest.NewRequest(method, path, strings.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	if perms != "" {
		r.Header.Set("X-Test-Perms", perms)
	}
	e.ServeHTTP(w, r)
	return w
}

func TestSyncRoutesRequirePerm(t *testing.T) {
	e, mock := newEnv(t)
	for _, tc := range []struct{ method, path string }{
		{http.MethodGet, "/api/v1/regions/status"},
		{http.MethodPost, "/api/v1/regions/sync"},
		{http.MethodPost, "/api/v1/regions/reset"},
	} {
		if w := do(e, tc.method, tc.path, "{}", "settings:list"); w.Code != http.StatusForbidden {
			t.Errorf("%s %s 无 region:sync = %d，期望 403", tc.method, tc.path, w.Code)
		}
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Error(err)
	}
}

func TestStatusReportsBuiltin(t *testing.T) {
	e, _ := newEnv(t)
	w := do(e, http.MethodGet, "/api/v1/regions/status", "", "region:sync")
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d", w.Code)
	}
	var body struct {
		Data Status `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if !body.Data.Builtin || body.Data.Provinces != 34 || body.Data.DefaultSource == "" {
		t.Fatalf("状态不对: %+v", body.Data)
	}
}

func TestSyncWithoutAnySource(t *testing.T) {
	// 配置里没有默认地址、请求也没带、库里也没存过：应直接拒绝而不是空跑
	e, mock := newEnvWithSource(t, "", "")
	mock.ExpectQuery("SELECT `source_url` FROM `sys_region_snapshot`").WillReturnError(gorm.ErrRecordNotFound)
	if w := do(e, http.MethodPost, "/api/v1/regions/sync", "{}", "region:sync"); w.Code != http.StatusBadRequest {
		t.Fatalf("缺少数据源 = %d，期望 400", w.Code)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Error(err)
	}
}

func TestSyncRejectsPlainHTTP(t *testing.T) {
	e, mock := newEnv(t)
	w := do(e, http.MethodPost, "/api/v1/regions/sync", `{"source":"http://example.com/regions.json"}`, "region:sync")
	if w.Code != http.StatusBadRequest {
		t.Fatalf("明文地址 = %d，期望 400", w.Code)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Error(err)
	}
}

func TestSourceClientRejectsPrivateAddress(t *testing.T) {
	// 守卫在拨号阶段：即使地址写成 HTTPS，解析到本机也必须拒绝
	client := newSourceClient()
	if _, err := client.Get("https://127.0.0.1:9/regions.json"); err == nil || !strings.Contains(err.Error(), "内网") {
		t.Fatalf("本机地址应被拒绝，得到 %v", err)
	}
}

func TestSyncAppliesSnapshotAndResetRestores(t *testing.T) {
	t.Cleanup(func() { coreregion.Reset() })

	// 数据源：用当前内置树改一个名字，确保差异统计和生效都能验证
	tree, err := coreregion.Tree(3)
	if err != nil {
		t.Fatal(err)
	}
	tree[0].Name = "北京市同步测试"
	payload, err := json.Marshal(tree)
	if err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(payload)
	}))
	defer ts.Close()
	// 必须在 Register 之前替换：Service 在注册时就构造好了 HTTP 客户端
	withTestClient(t, ts)

	e, mock := newEnv(t)
	mock.ExpectBegin()
	mock.ExpectExec("UPDATE `sys_region_snapshot`").WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectCommit()
	mock.ExpectBegin()
	mock.ExpectExec("INSERT INTO `sys_region_snapshot`").WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectCommit()

	w := do(e, http.MethodPost, "/api/v1/regions/sync", `{"source":"`+ts.URL+`/regions.json"}`, "region:sync")
	if w.Code != http.StatusOK {
		t.Fatalf("sync = %d body=%s", w.Code, w.Body.String())
	}
	// 同步已后台化：轮询状态直到完成，从状态接口取结果（result 嵌在状态里）。
	status := struct {
		Data struct {
			Result *Result `json:"result"`
		} `json:"data"`
	}{}
	if err := json.Unmarshal([]byte(awaitSyncDone(t, e)), &status); err != nil {
		t.Fatal(err)
	}
	if status.Data.Result == nil {
		t.Fatal("后台同步没有返回结果")
	}
	res := *status.Data.Result
	if res.Renamed != 1 || res.Info.Builtin || res.Info.Provinces != 34 {
		t.Fatalf("同步结果不对: %+v", res)
	}
	if name, _ := coreregion.Resolve("110000"); name != "北京市同步测试" {
		t.Fatalf("同步后未生效: %q", name)
	}

	mock.ExpectBegin()
	mock.ExpectExec("DELETE FROM `sys_region_snapshot`").WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()
	if w := do(e, http.MethodPost, "/api/v1/regions/reset", "", "region:sync"); w.Code != http.StatusOK {
		t.Fatalf("reset = %d", w.Code)
	}
	if name, _ := coreregion.Resolve("110000"); name != "北京市" {
		t.Fatalf("恢复内置后名称不对: %q", name)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Error(err)
	}
}

// TestSyncKeepsSpecialRegions 同步一份只有大陆 31 省的数据源时，港澳台不该被抹掉——
// 公开数据源大多不带台湾省。补全发生在落库之前，所以差异统计里不会出现「移除 1 条」，
// 重启装载的也是同一份字节。
func TestSyncKeepsSpecialRegions(t *testing.T) {
	t.Cleanup(func() { coreregion.Reset() })

	tree, err := coreregion.Tree(3)
	if err != nil {
		t.Fatal(err)
	}
	var mainlandOnly []*coreregion.Node
	for _, n := range tree {
		if n.Code[:2] != "71" && n.Code[:2] != "81" && n.Code[:2] != "82" {
			mainlandOnly = append(mainlandOnly, n)
		}
	}
	payload, err := json.Marshal(mainlandOnly)
	if err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(payload)
	}))
	defer ts.Close()
	// 必须在 Register 之前替换：Service 在注册时就构造好了 HTTP 客户端
	withTestClient(t, ts)

	source := ts.URL + "/regions.json"
	e, mock := newEnv(t)
	mock.ExpectBegin()
	mock.ExpectExec("UPDATE `sys_region_snapshot`").WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectCommit()
	mock.ExpectBegin()
	mock.ExpectExec("INSERT INTO `sys_region_snapshot`").WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectCommit()

	w := do(e, http.MethodPost, "/api/v1/regions/sync", `{"source":"`+source+`"}`, "region:sync")
	if w.Code != http.StatusOK {
		t.Fatalf("sync = %d body=%s", w.Code, w.Body.String())
	}
	status := struct {
		Data struct {
			Result *Result `json:"result"`
		} `json:"data"`
	}{}
	if err := json.Unmarshal([]byte(awaitSyncDone(t, e)), &status); err != nil {
		t.Fatal(err)
	}
	if status.Data.Result == nil {
		t.Fatal("后台同步没有返回结果")
	}
	res := *status.Data.Result
	if res.Info.Provinces != 34 {
		t.Errorf("省级 %d，期望 34", res.Info.Provinces)
	}
	if res.Added != 0 || res.Removed != 0 || res.Renamed != 0 {
		t.Errorf("内置补全不该造出差异: %+v", res)
	}
	for code, want := range map[string]string{"710000": "台湾省", "810101": "中西区", "820108": "路氹城"} {
		if name, _ := coreregion.Resolve(code); name != want {
			t.Errorf("同步后 Resolve(%s) = %q，期望 %q", code, name, want)
		}
	}

	// 预览走同一条链路，同样不该报「移除港澳台」（请求带了地址，不会再查库里的来源）
	// 预览仍是同步接口：响应体本身就是结果。
	w = do(e, http.MethodPost, "/api/v1/regions/preview", `{"source":"`+source+`"}`, "region:sync")
	if w.Code != http.StatusOK {
		t.Fatalf("preview = %d body=%s", w.Code, w.Body.String())
	}
	preview := struct {
		Data Result `json:"data"`
	}{}
	if err := json.Unmarshal(w.Body.Bytes(), &preview); err != nil {
		t.Fatal(err)
	}
	if preview.Data.Removed != 0 {
		t.Errorf("预览报出移除 %d 条: %+v", preview.Data.Removed, preview.Data)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Error(err)
	}
}

// withTestClient 把模块里新建的 Service 的 HTTP 客户端换成测试客户端的证书信任链。
// 同步接口在 Register 时就构造了 Service，这里通过包级钩子替换。
func withTestClient(t *testing.T, ts *httptest.Server) {
	t.Helper()
	prev := newSourceClient
	newSourceClient = func() *http.Client { return ts.Client() }
	t.Cleanup(func() { newSourceClient = prev })
}

// awaitSyncDone 轮询后台同步状态直到结束（5 秒兜底），返回最终状态 JSON。
// 同步已后台化：POST /regions/sync 只代表任务受理，结果从这里取；后台报错就地失败。
func awaitSyncDone(t *testing.T, e *gin.Engine) string {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		w := do(e, http.MethodGet, "/api/v1/regions/sync/status", "", "region:sync")
		if w.Code == http.StatusOK {
			var body struct {
				Data struct {
					Running bool    `json:"running"`
					Result  *Result `json:"result"`
					Error   string  `json:"error"`
				}
			}
			if err := json.Unmarshal(w.Body.Bytes(), &body); err == nil && !body.Data.Running {
				if body.Data.Error != "" {
					t.Fatalf("后台同步失败：%s", body.Data.Error)
				}
				return w.Body.String()
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("后台同步 5 秒内未完成")
	return ""
}
