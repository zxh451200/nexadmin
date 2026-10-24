package region

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/gin-gonic/gin"
)

type node struct {
	Code     string `json:"code"`
	Name     string `json:"name"`
	Children []node `json:"children"`
}

type resp struct {
	Data json.RawMessage `json:"data"`
}

func get(t *testing.T, path string) (int, []node) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	r := gin.New()
	module{}.Register(r.Group("/api/v1"), nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/v1"+path, nil))
	if w.Code != http.StatusOK {
		return w.Code, nil
	}
	var body resp
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	var list []node
	if err := json.Unmarshal(body.Data, &list); err != nil {
		t.Fatalf("data 应是数组: %s", body.Data)
	}
	return w.Code, list
}

func depth(list []node) int {
	d := 0
	for _, n := range list {
		if c := 1 + depth(n.Children); c > d {
			d = c
		}
	}
	return d
}

func TestTreeLevelParam(t *testing.T) {
	for _, tc := range []struct {
		query string
		depth int
	}{
		{"?level=1", 1}, {"?level=2", 2}, {"?level=3", 3},
		{"", 3}, {"?level=0", 3}, {"?level=-1", 3}, {"?level=99", 3}, {"?level=abc", 3}, // 非法值一律回落到 3 级
	} {
		code, list := get(t, "/regions/tree"+tc.query)
		if code != http.StatusOK || len(list) == 0 {
			t.Fatalf("%q: code=%d len=%d", tc.query, code, len(list))
		}
		if got := depth(list); got != tc.depth {
			t.Errorf("%q: 树深度 %d，期望 %d", tc.query, got, tc.depth)
		}
	}
}

func TestChildrenCascade(t *testing.T) {
	code, provinces := get(t, "/regions")
	if code != http.StatusOK || len(provinces) == 0 {
		t.Fatalf("不带 parent 应返回省级列表: code=%d", code)
	}
	if depth(provinces) != 1 {
		t.Error("省级列表不应带下级")
	}

	// 逐级向下：每一级只返回直接下级，且下级代码以上级前缀开头的行政区划规则成立
	parent := provinces[0]
	code, cities := get(t, "/regions?parent="+parent.Code)
	if code != http.StatusOK || len(cities) == 0 {
		t.Fatalf("省 %s 应有下级: code=%d", parent.Code, code)
	}
	if depth(cities) != 1 {
		t.Error("按需取下级只返回直接下级")
	}
	for _, c := range cities {
		if c.Code[:2] != parent.Code[:2] {
			t.Errorf("市 %s 不属于省 %s", c.Code, parent.Code)
		}
	}
}

func TestChildrenUnknownParent(t *testing.T) {
	for _, p := range []string{"999999", "abc", "11", "110000' OR '1'='1"} {
		if code, _ := get(t, "/regions?parent="+url.QueryEscape(p)); code != http.StatusNotFound {
			t.Errorf("parent=%q 应 404，得到 %d", p, code)
		}
	}
}
