package report

import (
	"archive/zip"
	"bytes"
	"io"
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
	req := httptest.NewRequest(http.MethodPost, "/api/v1/plugins/report/view/reports/3/charts/11/export", strings.NewReader(`{"filters":{},"name":"当班看板","headers":["状态 Status"]}`))
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
	// 前端给的表名/表头要真的落到文件里：内置图表的名字在库里是 i18n 键，服务端词典翻不出来
	if wb := zipPart(t, w.Body.Bytes(), "xl/workbook.xml"); !strings.Contains(wb, `name="当班看板"`) {
		t.Errorf("sheet 名没采用前端给的表名: %s", wb)
	}
	if sst := zipPart(t, w.Body.Bytes(), "xl/sharedStrings.xml"); !strings.Contains(sst, "状态 Status") {
		t.Errorf("表头没采用前端给的标题: %s", sst)
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

// zipPart 读出 xlsx（zip）里的一个部件内容，用于断言表名/表头真的写进了文件。
func zipPart(t *testing.T, blob []byte, name string) string {
	t.Helper()
	zr, err := zip.NewReader(bytes.NewReader(blob), int64(len(blob)))
	if err != nil {
		t.Fatalf("read xlsx zip: %v", err)
	}
	for _, f := range zr.File {
		if f.Name != name {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			t.Fatalf("open %s: %v", name, err)
		}
		defer rc.Close()
		data, err := io.ReadAll(rc)
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		return string(data)
	}
	t.Fatalf("%s 不在 xlsx 里", name)
	return ""
}

// 表头只在数量对得上时采用；单条为空或含控制字符时退回/清洗，绝不把键名写进文件。
func TestPickHeadersAndSheetName(t *testing.T) {
	res := &Result{Columns: []ResultCol{{Key: "d0", Label: "状态", Kind: "dim"}, {Key: "m0", Label: "plugin_report.builtin.measure.count", Kind: "measure"}}}
	if got := pickHeaders([]string{"只有一个"}, res); got != nil {
		t.Errorf("数量不符时应整体退回元数据文案，得到 %+v", got)
	}
	got := pickHeaders([]string{"  ", "次数\t数"}, res)
	if got[0] != "状态" {
		t.Errorf("空表头应退回元数据: %+v", got)
	}
	if got[1] != "次数数" {
		t.Errorf("控制字符未清除: %q", got[1])
	}
	if s := cleanSheetName("值班/大屏[1]:*?\\"); s != "值班大屏1" {
		t.Errorf("sheet 名未清洗: %q", s)
	}
	if s := cleanSheetName(strings.Repeat("表", 40)); len([]rune(s)) != 31 {
		t.Errorf("sheet 名未截到 31 字符: %d", len([]rune(s)))
	}
}
