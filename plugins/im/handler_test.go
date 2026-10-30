package im

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"

	"nexadmin/core/response"
)

// 服务层出错时会话指针为 nil：响应必须走错误分支，不能先解引用。
// 把 respondConv 改回 respond(c, convView(conv), err) 会让本用例 panic 而不是拿到 409。
func TestRespondConvNilOnConflict(t *testing.T) {
	gin.SetMode(gin.TestMode)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/v1/plugins/im/visitor/conversations/IM1/rating", nil)

	respondConv(c, nil, response.Conflict("im.rating_exists"))

	if rec.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409", rec.Code)
	}
	var body struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("bad body %q: %v", rec.Body.String(), err)
	}
	if body.Code == 500 || body.Message == "" {
		t.Fatalf("expected a business error, got %+v", body)
	}
}

func TestRespondConvWritesView(t *testing.T) {
	gin.SetMode(gin.TestMode)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/v1/plugins/im/conversations/IM1", nil)

	respondConv(c, &Conversation{ConvNo: "IM1", Status: StatusClosed}, nil)

	var body struct {
		Code int `json:"code"`
		Data struct {
			ConvNo string `json:"convNo"`
			Status string `json:"status"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("bad body %q: %v", rec.Body.String(), err)
	}
	if body.Code != 0 || body.Data.ConvNo != "IM1" || body.Data.Status != StatusClosed {
		t.Fatalf("unexpected payload %+v", body)
	}
}
