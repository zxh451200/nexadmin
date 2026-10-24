package response

import (
	"fmt"
	"github.com/gin-gonic/gin"
	"net/http/httptest"
	"nexadmin/pkg/idgen"
	"os"
	"strings"
	"testing"
)

func TestContractResponses(t *testing.T) {
	gin.SetMode(gin.TestMode)
	var b strings.Builder
	cases := []struct {
		name    string
		handler gin.HandlerFunc
	}{
		{"success", func(c *gin.Context) {
			OK(c, struct {
				ID idgen.ID `json:"id"`
			}{ID: 9007199254740993})
		}},
		{"page", func(c *gin.Context) { Page(c, []string(nil), 0, 1, 10) }},
		{"bad_request", func(c *gin.Context) { Fail(c, BadRequest("x").WithMsg("bad")) }},
		{"unauthorized", func(c *gin.Context) { Fail(c, Unauthorized("x").WithMsg("auth")) }},
		{"forbidden", func(c *gin.Context) { Fail(c, Forbidden("x").WithMsg("denied")) }},
		{"not_found", func(c *gin.Context) { Fail(c, NotFound("x").WithMsg("missing")) }},
		{"conflict", func(c *gin.Context) { Fail(c, Conflict("x").WithMsg("conflict")) }},
		{"internal", func(c *gin.Context) { Fail(c, Internal().WithMsg("internal")) }},
		{"unavailable", func(c *gin.Context) { Fail(c, Unavailable("x").WithMsg("unavailable")) }},
		{"locked", func(c *gin.Context) { Fail(c, Locked(30)) }},
		{"reason", func(c *gin.Context) { Fail(c, NotInitialized().WithMsg("setup")) }},
	}
	for _, tc := range cases {
		e := gin.New()
		e.GET("/", tc.handler)
		w := httptest.NewRecorder()
		e.ServeHTTP(w, httptest.NewRequest("GET", "/", nil))
		fmt.Fprintf(&b, "%s %d %s\n", tc.name, w.Code, w.Body.String())
	}
	want, err := os.ReadFile("../testdata/contracts/response.golden")
	if err != nil {
		t.Fatal(err)
	}
	if string(want) != b.String() {
		t.Fatalf("response contract changed; update reviewed golden and version\n%s", b.String())
	}
}
