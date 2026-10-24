package report

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/gin-gonic/gin"

	"nexadmin/core/auth"
	"nexadmin/core/datascope"
	"nexadmin/core/tenant"
)

// 导出接口：下载头、xlsx 内容、行数上限（MaxRows=3 -> 带 X-Report-Truncated）与错误时不写文件。
func TestExportEndpoint(t *testing.T) {
	gin.SetMode(gin.TestMode)
	svc, mock := newTestService(t)
	h := newHandler(svc, nil)
	h.scopeFn = func(*gin.Context, *auth.Claims) (*datascope.Scope, error) { return &datascope.Scope{All: true}, nil }
	h.permFn = func(*gin.Context, string) bool { return true }
	e := gin.New()
	e.Use(func(c *gin.Context) {
		auth.SetClaims(c, &auth.Claims{UserID: 5, TenantID: 7, Super: true})
		c.Request = c.Request.WithContext(tenant.With(c.Request.Context(), 7))
	})
	registerRoutes(e.Group("/api/v1"), h)

	cfg := `{"items":[{"chartId":"11","group":"","span":12}],"filters":[]}`
	mock.ExpectQuery(reportRowSQL).WithArgs(int64(7), int64(3), 1).WillReturnRows(reportRows(cfg))
	expectChartAndDataset(mock, "table", tableChartCfg, `{"columns":["status","attempts"],"filters":[]}`)
	mock.ExpectQuery("SELECT `status` AS `d0` FROM `plg_cron_log` WHERE `tenant_id` = \\? ORDER BY 1 LIMIT \\?").WithArgs(int64(7), 4).
		WillReturnRows(sqlmock.NewRows([]string{"d0"}).AddRow("a").AddRow("b").AddRow("c").AddRow("d"))
	req := httptest.NewRequest(http.MethodPost, "/api/v1/plugins/report/view/reports/3/charts/11/export", strings.NewReader(`{"filters":{}}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	e.ServeHTTP(w, req)
	if w.Code != http.StatusOK || w.Header().Get("Content-Type") != xlsxContentType {
		t.Fatalf("status=%d type=%q body=%s", w.Code, w.Header().Get("Content-Type"), w.Body.String())
	}
	if cd := w.Header().Get("Content-Disposition"); !strings.HasPrefix(cd, "attachment; filename=\"") || !strings.Contains(cd, ".xlsx\"") {
		t.Errorf("Content-Disposition = %q", cd)
	}
	if w.Header().Get("X-Report-Truncated") != "1" || !strings.HasPrefix(w.Body.String(), "PK") {
		t.Errorf("truncated=%q bodyPK=%v", w.Header().Get("X-Report-Truncated"), strings.HasPrefix(w.Body.String(), "PK"))
	}

	// 报表不存在 -> JSON 404，不写出任何文件头。
	mock.ExpectQuery(reportRowSQL).WithArgs(int64(7), int64(3), 1).WillReturnRows(sqlmock.NewRows([]string{"id"}))
	req = httptest.NewRequest(http.MethodPost, "/api/v1/plugins/report/view/reports/3/charts/11/export", strings.NewReader(`{}`))
	req.Header.Set("Content-Type", "application/json")
	w = httptest.NewRecorder()
	e.ServeHTTP(w, req)
	if w.Code != http.StatusNotFound || w.Header().Get("Content-Disposition") != "" {
		t.Errorf("status=%d disposition=%q", w.Code, w.Header().Get("Content-Disposition"))
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}
