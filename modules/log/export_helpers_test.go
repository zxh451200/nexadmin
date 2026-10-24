package log

import (
	"bytes"
	"fmt"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"

	"nexadmin/core"
	"nexadmin/core/auth"
	"nexadmin/core/cache"
	"nexadmin/core/config"
	"nexadmin/pkg/excel"
	"nexadmin/pkg/idgen"
)

// captureSQL 记录实际执行的 SQL，并按正则匹配（和默认匹配器一致），用来断言“哪些列被查询”。
type captureSQL struct{ got []string }

func (c *captureSQL) match(expected, actual string) error {
	c.got = append(c.got, actual)
	if !regexp.MustCompile(expected).MatchString(actual) {
		return fmt.Errorf("sql %q does not match %q", actual, expected)
	}
	return nil
}

func (c *captureSQL) selects() []string {
	var out []string
	for _, s := range c.got {
		if strings.HasPrefix(s, "SELECT `") {
			out = append(out, s)
		}
	}
	return out
}

// newExportEnv 用真实的 module.Register 挂路由（含 core.Perm 与限流中间件），数据库是 sqlmock。
func newExportEnv(t *testing.T, rl config.RateLimit, maxRows int) (*gin.Engine, sqlmock.Sqlmock, *captureSQL) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	if err := idgen.Init(1); err != nil { // 写导出操作日志需要雪花 ID
		t.Fatal(err)
	}
	capt := &captureSQL{}
	sqlDB, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherFunc(capt.match)))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	db, err := gorm.Open(mysql.New(mysql.Config{Conn: sqlDB, SkipInitializeWithVersion: true}), &gorm.Config{DisableAutomaticPing: true})
	if err != nil {
		t.Fatal(err)
	}
	ch, err := cache.New(config.Cache{Driver: "memory"})
	if err != nil {
		t.Fatal(err)
	}
	cfg := &config.Config{RateLimit: rl}
	cfg.App.Timezone = "UTC"
	cfg.Log.ExportMaxRows = maxRows
	e := gin.New()
	e.Use(func(c *gin.Context) { // 以开放应用主体登录：权限由 Perms 直接决定，数据范围为全部
		auth.SetClaims(c, &auth.Claims{UserID: 9, TenantID: 1, Username: "app:t", Principal: auth.PrincipalApp, Perms: strings.Split(c.GetHeader("X-Test-Perms"), ",")})
	})
	module{}.Register(e.Group("/api/v1"), &core.Deps{Config: cfg, DB: db, Cache: ch, Log: zap.NewNop()})
	return e, mock, capt
}

func doGet(e *gin.Engine, url, perms string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodGet, url, nil)
	req.Header.Set("X-Test-Perms", perms)
	w := httptest.NewRecorder()
	e.ServeHTTP(w, req)
	return w
}

func readXLSX(t *testing.T, w *httptest.ResponseRecorder) [][]string {
	t.Helper()
	b := w.Body.Bytes()
	rows, err := excel.ReadAll(bytes.NewReader(b), int64(len(b)), excel.ReadOptions{})
	if err != nil {
		t.Fatalf("响应不是合法 xlsx: %v (body=%q)", err, truncate(string(b), 120))
	}
	return rows
}

func truncate(s string, n int) string {
	if len(s) > n {
		return s[:n]
	}
	return s
}

// expectExportLog 声明导出动作本身会写一条操作日志。
func expectExportLog(mock sqlmock.Sqlmock) {
	mock.ExpectBegin()
	mock.ExpectExec("INSERT INTO `sys_oper_log`").WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectCommit()
}
