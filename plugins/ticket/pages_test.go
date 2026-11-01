package ticket

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/gin-gonic/gin"
)

// formPage 要读启用中的字段定义才能渲染附加字段，页面测试就给它一个会回答这条查询的服务。
func pageHandler(t *testing.T, renders int) *handler {
	t.Helper()
	db, m := mockDB(t)
	for i := 0; i < renders; i++ {
		m.ExpectQuery("SELECT \\* FROM `plg_ticket_field` WHERE enabled = \\?").
			WithArgs(true).
			WillReturnRows(sqlmock.NewRows([]string{
				"id", "code", "label", "type", "options", "required", "enabled", "sort", "group_id",
				"created_at", "updated_at",
			}).AddRow(int64(1), "order_channel", "下单渠道", "select", "App,门店", true, true, 0, int64(0), testNow, testNow))
	}
	return &handler{svc: NewService(db, nopLogger{})}
}

func servePage(t *testing.T, h gin.HandlerFunc, target string) string {
	t.Helper()
	r := gin.New()
	r.GET("/ticket/new", h)
	r.GET("/ticket/view", h)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, target, nil))
	if w.Code != http.StatusOK {
		t.Fatalf("status %d", w.Code)
	}
	return w.Body.String()
}

func TestFormPageRendersBilingual(t *testing.T) {
	h := pageHandler(t, 2)
	zh := servePage(t, h.formPage, "/ticket/new")
	if !strings.Contains(zh, "提交工单") || !strings.Contains(zh, "api/v1/plugins/ticket/visitor") {
		t.Fatal("zh form page missing expected content")
	}
	// 附加字段要真的进页面：定义以 JSON 注入，脚本按类型渲染并收进 customValues。
	for _, want := range []string{"order_channel", "下单渠道", "collectFields", `"未填"`} {
		if !strings.Contains(zh, want) {
			t.Fatalf("zh form page missing %q", want)
		}
	}
	en := servePage(t, h.formPage, "/ticket/new?lang=en")
	if !strings.Contains(en, "Submit a ticket") || !strings.Contains(en, "No sign-in required") {
		t.Fatal("en form page missing expected content")
	}
	if !strings.Contains(en, `"Not filled"`) {
		t.Fatal("en form page missing the custom-field blank option label")
	}
}

func TestViewPageEscapesPrefill(t *testing.T) {
	h := &handler{}
	body := servePage(t, h.viewPage, "/ticket/view?no=%3Cscript%3Ealert(1)%3C/script%3E")
	if strings.Contains(body, "<script>alert(1)</script>") {
		t.Fatal("prefill must be HTML-escaped")
	}
	if !strings.Contains(body, "&lt;script&gt;") {
		t.Fatal("escaped prefill missing")
	}
	if !strings.Contains(body, "服务评价") || !strings.Contains(body, "/rating") {
		t.Fatal("view page should render the CSAT block and rating endpoint")
	}
	if !strings.Contains(body, "附件") || !strings.Contains(body, "/attachments/") {
		t.Fatal("view page should render the attachments block and download/upload endpoints")
	}
	// 客户看得到处理进度（提交与状态变化），也拿得到评价接口；两者都靠同一个公开响应。
	if !strings.Contains(body, "处理进度") || !strings.Contains(body, "data.data.activities") {
		t.Fatal("view page should render the customer-visible activity trail")
	}
	// 客户自己上传的附件要有删除入口（服务端同样判定），带确认话术与失败提示。
	if !strings.Contains(body, "\"attDel\":\"删除\"") || !strings.Contains(body, "method: 'DELETE'") {
		t.Fatal("view page should offer the visitor attachment delete action")
	}
	if !strings.Contains(body, "uploaderType === 'reporter'") {
		t.Fatal("visitor delete button must only render for the reporter's own attachments")
	}
	prog := servePage(t, h.viewPage, "/ticket/view?lang=en")
	if !strings.Contains(prog, "Progress") || !strings.Contains(prog, "\"timelineStat\":\"Status changed to\"") {
		t.Fatalf("en progress strings missing: %s", prog)
	}
	if strings.Contains(prog, "提交工单") {
		t.Fatal("英文页夹带中文进度文案")
	}
}
