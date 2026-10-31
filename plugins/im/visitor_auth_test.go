package im

import (
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestCheckVisitorRejectsEmptyStoredToken(t *testing.T) {
	gin.SetMode(gin.TestMode)
	h := &handler{}
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest("GET", "/x", nil)
	if h.checkVisitor(c, &Conversation{}) {
		t.Fatal("empty stored visitor token must not authorize an anonymous request")
	}
	if w.Code != 401 {
		t.Fatalf("status = %d, want 401", w.Code)
	}
}

func TestCheckVisitorRequiresMatchingToken(t *testing.T) {
	gin.SetMode(gin.TestMode)
	h := &handler{}
	conv := &Conversation{VisitorToken: "tok-123"}

	cases := []struct {
		name   string
		header string
		query  string
		want   bool
	}{
		{name: "header ok", header: "tok-123", want: true},
		{name: "query ok", query: "?visitorToken=tok-123", want: true},
		{name: "missing", want: false},
		{name: "wrong", header: "tok-999", want: false},
		{name: "empty query", query: "?visitorToken=", want: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(w)
			c.Request = httptest.NewRequest("GET", "/x"+tc.query, nil)
			if tc.header != "" {
				c.Request.Header.Set("X-Visitor-Token", tc.header)
			}
			if got := h.checkVisitor(c, conv); got != tc.want {
				t.Fatalf("checkVisitor() = %v, want %v (status %d)", got, tc.want, w.Code)
			}
			if !tc.want && w.Code != 401 {
				t.Fatalf("status = %d, want 401", w.Code)
			}
		})
	}
}
