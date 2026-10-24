package about

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"runtime"
	"sort"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/gin-gonic/gin"
	"gorm.io/driver/mysql"
	gormpg "gorm.io/driver/postgres"
	"gorm.io/gorm"

	"nexadmin/core"
	"nexadmin/core/auth"
)

func mysqlDB(t *testing.T) (*gorm.DB, sqlmock.Sqlmock) {
	t.Helper()
	sqlDB, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	db, err := gorm.Open(mysql.New(mysql.Config{Conn: sqlDB, SkipInitializeWithVersion: true}), &gorm.Config{DisableAutomaticPing: true})
	if err != nil {
		t.Fatal(err)
	}
	return db, mock
}

func TestDBEngineMySQLFamily(t *testing.T) {
	for _, tc := range []struct{ raw, want string }{
		{"8.4.0", "MySQL 8.4.0"},
		{"8.0.36-0ubuntu0.22.04.1", "MySQL 8.0.36"},
		{"10.11.6-MariaDB-1:10.11.6+maria~ubu2204", "MariaDB 10.11.6"},
		{"10.11.6-mariadb", "MariaDB 10.11.6"},
		{"5.5.5-10.11.6-MariaDB", "MariaDB 10.11.6"}, // 兼容前缀
	} {
		db, mock := mysqlDB(t)
		mock.ExpectQuery("SELECT VERSION\\(\\)").WillReturnRows(sqlmock.NewRows([]string{"v"}).AddRow(tc.raw))
		if got := dbEngine(db); got != tc.want {
			t.Errorf("VERSION()=%q 得到 %q，期望 %q", tc.raw, got, tc.want)
		}
	}
	t.Run("查询失败退回引擎名", func(t *testing.T) {
		db, mock := mysqlDB(t)
		mock.ExpectQuery("SELECT VERSION\\(\\)").WillReturnError(errors.New("down"))
		if got := dbEngine(db); got != "MySQL" {
			t.Errorf("got %q", got)
		}
	})
}

func TestDBEnginePostgres(t *testing.T) {
	open := func(t *testing.T) (*gorm.DB, sqlmock.Sqlmock) {
		sqlDB, mock, err := sqlmock.New()
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = sqlDB.Close() })
		db, err := gorm.Open(gormpg.New(gormpg.Config{Conn: sqlDB}), &gorm.Config{DisableAutomaticPing: true})
		if err != nil {
			t.Fatal(err)
		}
		return db, mock
	}
	db, mock := open(t)
	mock.ExpectQuery("SHOW server_version").WillReturnRows(sqlmock.NewRows([]string{"v"}).AddRow("16.4 (Debian 16.4-1.pgdg120+1)"))
	if got := dbEngine(db); got != "PostgreSQL 16.4" {
		t.Errorf("got %q", got)
	}
	db, mock = open(t)
	mock.ExpectQuery("SHOW server_version").WillReturnError(errors.New("down"))
	if got := dbEngine(db); got != "PostgreSQL" {
		t.Errorf("got %q", got)
	}
}

func TestBuildTime(t *testing.T) {
	old := BuildTime
	t.Cleanup(func() { BuildTime = old })
	BuildTime = "2026-10-01T08:30:00Z"
	if got := buildTime(); got != "2026-10-01T08:30:00Z" {
		t.Errorf("ldflags 注入值应优先: %q", got)
	}
	BuildTime = ""
	if got := buildTime(); got == "" {
		t.Error("没有注入值时也不应为空（VCS 时间或启动时间兜底）")
	}
}

func TestLicenses(t *testing.T) {
	list := licenses()
	if len(list) == 0 {
		t.Skip("测试二进制没有嵌入依赖信息")
	}
	if !sort.SliceIsSorted(list, func(i, j int) bool { return list[i].Name < list[j].Name }) {
		t.Error("应按名称排序")
	}
	seen := map[string]License{}
	for _, l := range list {
		if l.License == "" || l.Name == "" {
			t.Errorf("条目不完整: %+v", l)
		}
		seen[l.Name] = l
	}
	if g, ok := seen["github.com/gin-gonic/gin"]; ok && g.License != "MIT" {
		t.Errorf("已收录依赖应显示许可证: %+v", g)
	}
	for name, l := range seen {
		if _, known := knownLicenses[name]; !known && l.License != "Unknown" {
			t.Errorf("未收录依赖 %s 应显示 Unknown，得到 %q", name, l.License)
		}
	}
}

func serve(t *testing.T, cl *auth.Claims, path string) *httptest.ResponseRecorder {
	t.Helper()
	db, mock := mysqlDB(t)
	mock.ExpectQuery("SELECT VERSION\\(\\)").WillReturnRows(sqlmock.NewRows([]string{"v"}).AddRow("8.4.0"))
	gin.SetMode(gin.TestMode)
	e := gin.New()
	g := e.Group("/api/v1")
	if cl != nil {
		g.Use(func(c *gin.Context) { auth.SetClaims(c, cl) })
	}
	module{}.Register(g, &core.Deps{DB: db})
	w := httptest.NewRecorder()
	e.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/v1"+path, nil))
	return w
}

func TestRoutesRequirePermission(t *testing.T) {
	for _, path := range []string{"/about", "/about/licenses"} {
		if w := serve(t, nil, path); w.Code != http.StatusUnauthorized {
			t.Errorf("%s 未登录应 401，得到 %d", path, w.Code)
		}
		if w := serve(t, &auth.Claims{Principal: auth.PrincipalApp}, path); w.Code != http.StatusForbidden {
			t.Errorf("%s 无 about:view 应 403，得到 %d", path, w.Code)
		}
		if w := serve(t, &auth.Claims{Principal: auth.PrincipalApp, Perms: []string{PermView}}, path); w.Code != http.StatusOK {
			t.Errorf("%s 有 about:view 应 200，得到 %d", path, w.Code)
		}
	}
}

func TestAboutPayload(t *testing.T) {
	w := serve(t, &auth.Claims{Super: true}, "/about")
	var body struct {
		Data Info `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	d := body.Data
	if d.Version != Version || d.DBEngine != "MySQL 8.4.0" || d.GoVersion != runtime.Version() || d.OS != runtime.GOOS+"/"+runtime.GOARCH || d.BuildTime == "" {
		t.Errorf("payload 不对: %+v", d)
	}
}
