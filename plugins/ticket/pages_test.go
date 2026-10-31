package ticket

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

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
	h := &handler{}
	zh := servePage(t, h.formPage, "/ticket/new")
	if !strings.Contains(zh, "提交工单") || !strings.Contains(zh, "api/v1/plugins/ticket/visitor") {
		t.Fatal("zh form page missing expected content")
	}
	en := servePage(t, h.formPage, "/ticket/new?lang=en")
	if !strings.Contains(en, "Submit a ticket") || !strings.Contains(en, "No sign-in required") {
		t.Fatal("en form page missing expected content")
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
	prog := servePage(t, h.viewPage, "/ticket/view?lang=en")
	if !strings.Contains(prog, "Progress") || !strings.Contains(prog, "\"timelineStat\":\"Status changed to\"") {
		t.Fatalf("en progress strings missing: %s", prog)
	}
	if strings.Contains(prog, "提交工单") {
		t.Fatal("英文页夹带中文进度文案")
	}
}
