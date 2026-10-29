package website

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

// postTranslate 用 nil 网关打一次机翻接口：状态码就能区分「请求本身不对」和「AI 没配」。
func postTranslate(t *testing.T, body string) int {
	t.Helper()
	h := &handler{}
	r := gin.New()
	r.POST("/t", h.makeTranslator("post"))
	req := httptest.NewRequest(http.MethodPost, "/t", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w.Code
}

// 参数校验必须排在网关之前：否则所有失败都糊成一个 503，坏请求也照样吃限流次数。
func TestTranslateValidation(t *testing.T) {
	def := DefaultLanguage().Code
	if def == "" {
		t.Fatal("测试环境没有默认语言，断言无从建立")
	}

	if code := postTranslate(t, `{"texts":["标题"],"from":"`+def+`","to":"en-US","format":"text"}`); code != http.StatusServiceUnavailable {
		t.Errorf("合法请求 + 未配置网关 = %d, want 503", code)
	}

	bad := map[string]string{
		"空文本":         `{"texts":[],"from":"` + def + `","to":"en-US"}`,
		"from 不是默认语言": `{"texts":["x"],"from":"en-US","to":"` + def + `"}`,
		"to 是默认语言":    `{"texts":["x"],"from":"` + def + `","to":"` + def + `"}`,
		"to 未配置":      `{"texts":["x"],"from":"` + def + `","to":"ja-JP"}`,
		"不是 JSON":     `texts=1`,
	}
	for name, body := range bad {
		if code := postTranslate(t, body); code != http.StatusBadRequest {
			t.Errorf("%s：应当 400，实得 %d", name, code)
		}
	}
}

// 每个内容实体都要挂上自己的机翻路由，否则前端点了按钮只会拿到 404。
func TestTranslateRoutesCoverEveryEntity(t *testing.T) {
	seen := map[string]string{}
	for _, e := range translateRoutes {
		if e.perm == "" {
			t.Errorf("%s 没有映射权限码", e.entity)
		}
		if _, dup := seen[e.entity]; dup {
			t.Errorf("实体 %s 注册了两次", e.entity)
		}
		seen[e.entity] = e.perm
	}
	for entity := range entityViews {
		if _, ok := seen[entity]; !ok {
			t.Errorf("%s 有翻译表单却没有机翻路由", entity)
		}
	}
}
