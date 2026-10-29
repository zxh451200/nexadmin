package imports

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

// 模板下载：默认 CSV、可选 XLSX、未知格式 400；两者都以 attachment 返回（浏览器存文件而非内联展示）。
func TestTemplateHandlerFormats(t *testing.T) {
	if err := RegisterSource(testSource("tpl-fmt", func(context.Context, int, map[string]string) error { return nil })); err != nil {
		t.Fatal(err)
	}
	h := &handler{}
	call := func(query string) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequest(http.MethodGet, "/api/v1/plugins/imports/template?"+query, nil)
		h.template(c)
		return w
	}

	csv := call("source=tpl-fmt")
	if csv.Code != http.StatusOK {
		t.Fatalf("default format: status=%d body=%s", csv.Code, csv.Body.String())
	}
	if ct := csv.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/csv") {
		t.Errorf("csv Content-Type=%q", ct)
	}
	if cd := csv.Header().Get("Content-Disposition"); !strings.HasPrefix(cd, "attachment") ||
		!strings.Contains(cd, "import-template-tpl-fmt.csv") {
		t.Errorf("csv Content-Disposition=%q", cd)
	}
	if !strings.HasPrefix(csv.Body.String(), "\uFEFF名称") {
		t.Errorf("csv body=%q", csv.Body.String())
	}

	xlsx := call("source=tpl-fmt&format=XLSX")
	if xlsx.Code != http.StatusOK {
		t.Fatalf("xlsx: status=%d body=%s", xlsx.Code, xlsx.Body.String())
	}
	if body := xlsx.Body.Bytes(); len(body) < 2 || string(body[:2]) != "PK" {
		t.Errorf("xlsx body is not a workbook (%d bytes)", len(body))
	}
	if cd := xlsx.Header().Get("Content-Disposition"); !strings.Contains(cd, "import-template-tpl-fmt.xlsx") {
		t.Errorf("xlsx Content-Disposition=%q", cd)
	}

	if bad := call("source=tpl-fmt&format=pdf"); bad.Code != http.StatusBadRequest {
		t.Errorf("unknown format: status=%d body=%s", bad.Code, bad.Body.String())
	}
}

// 来源元数据的 JSON 键必须小写：前端 ImportColumn 按 key/label/required/example 取值，
// 大写字母会让来源列说明整块渲染成空。
func TestSourceMetaJSONKeys(t *testing.T) {
	if err := RegisterSource(testSource("tpl-keys", func(context.Context, int, map[string]string) error { return nil })); err != nil {
		t.Fatal(err)
	}
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/v1/plugins/imports/sources", nil)
	(&handler{}).sources(c)
	body := w.Body.String()
	for _, want := range []string{`"key":"title"`, `"label":"名称"`, `"required":true`, `"example":"测试商品"`} {
		if !strings.Contains(body, want) {
			t.Errorf("missing %s in %s", want, body)
		}
	}
	for _, unwanted := range []string{`"Key"`, `"Label"`, `"Required"`, `"Example"`} {
		if strings.Contains(body, unwanted) {
			t.Errorf("unexpected PascalCase key %s in %s", unwanted, body)
		}
	}
}
