package core

import (
	"fmt"
	"github.com/gin-gonic/gin"
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"strings"
	"testing"
)

func contractGolden(t *testing.T, name, got string) {
	t.Helper()
	want, err := os.ReadFile("testdata/contracts/" + name + ".golden")
	if err != nil {
		t.Fatal(err)
	}
	if string(want) != got {
		t.Fatalf("contract %s changed; update reviewed golden and framework version\n%s", name, got)
	}
}
func TestContractModule(t *testing.T) {
	typ := reflect.TypeOf((*Module)(nil)).Elem()
	var b strings.Builder
	for i := 0; i < typ.NumMethod(); i++ {
		m := typ.Method(i)
		fmt.Fprintf(&b, "%s %s\n", m.Name, m.Type.String())
	}
	contractGolden(t, "module", b.String())
}
func TestContractRoutes(t *testing.T) {
	gin.SetMode(gin.TestMode)
	e := gin.New()
	api := e.Group(contractAPIPrefix)
	api.GET("/contract", func(c *gin.Context) { c.Status(204) })
	api.Group(contractAdminPrefix).GET("/contract", func(c *gin.Context) { c.Status(204) })
	var b strings.Builder
	for _, route := range e.Routes() {
		w := httptest.NewRecorder()
		e.ServeHTTP(w, httptest.NewRequest(http.MethodGet, route.Path, nil))
		if w.Code != 204 {
			t.Fatal(w.Code)
		}
		fmt.Fprintf(&b, "%s %s\n", route.Method, route.Path)
	}
	contractGolden(t, "routes", b.String())
}
