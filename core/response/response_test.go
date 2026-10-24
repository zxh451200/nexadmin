package response

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"

	"nexadmin/pkg/idgen"
)

func do(h gin.HandlerFunc) *httptest.ResponseRecorder {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.GET("/", h)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/", nil))
	return w
}

func TestOKShape(t *testing.T) {
	w := do(func(c *gin.Context) { OK(c, gin.H{"id": idgen.ID(1790000000000000001)}) })
	if w.Code != 200 || w.Body.String() != `{"code":0,"message":"ok","data":{"id":"1790000000000000001"}}` {
		t.Fatalf("unexpected: %d %s", w.Code, w.Body.String())
	}
}

func TestPageShapeAndEmptyList(t *testing.T) {
	w := do(func(c *gin.Context) { Page[string](c, nil, 0, 1, 10) })
	var b struct {
		Data struct {
			List     []string `json:"list"`
			Total    int      `json:"total"`
			Page     int      `json:"page"`
			PageSize int      `json:"pageSize"`
		}
	}
	if err := json.Unmarshal(w.Body.Bytes(), &b); err != nil || b.Data.List == nil || b.Data.PageSize != 10 {
		t.Fatalf("unexpected: %s", w.Body.String())
	}
}

func TestNotInitialized(t *testing.T) {
	w := do(func(c *gin.Context) { Fail(c, NotInitialized()) })
	var b Body
	_ = json.Unmarshal(w.Body.Bytes(), &b)
	if w.Code != 503 || b.Code != 503 || b.Reason != "NOT_INITIALIZED" || b.Data != nil {
		t.Fatalf("unexpected: %d %s", w.Code, w.Body.String())
	}
}
