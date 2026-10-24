package apistat

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
)

// steady 返回一个“已运行足够久”的统计器：窗口按完整 30 秒计，QPS = 请求数 / 30，结果可预期。
func steady() *Collector {
	c := New()
	c.started = time.Now().Add(-time.Hour)
	return c
}

func find(s Snapshot, method, path string) (Stat, bool) {
	for _, a := range s.APIs {
		if a.Method == method && a.Path == path {
			return a, true
		}
	}
	return Stat{}, false
}

func TestRecordOnlyCountsCommonMethods(t *testing.T) {
	c := steady()
	for _, m := range []string{"OPTIONS", "HEAD", "PATCH", ""} {
		c.Record(m, "/x", 200, time.Millisecond)
	}
	if got := c.Snapshot(); len(got.APIs) != 0 || got.QPS != 0 {
		t.Fatalf("非 GET/POST/PUT/DELETE 不应计入: %+v", got)
	}
	c.Record("GET", "/x", 200, time.Millisecond)
	if got := c.Snapshot(); len(got.APIs) != 1 {
		t.Fatalf("GET 应计入: %+v", got)
	}
}

func TestSnapshotAggregates(t *testing.T) {
	c := steady()
	// /a：30 次，其中 3 次错误（399 不算错，400 / 500 算错），每次 10ms
	for i := 0; i < 27; i++ {
		c.Record("GET", "/a", 200, 10*time.Millisecond)
	}
	c.Record("GET", "/a", 399, 10*time.Millisecond)
	c.Record("GET", "/a", 400, 10*time.Millisecond)
	c.Record("GET", "/a", 500, 10*time.Millisecond)
	// /b：10 次，全错，每次 40ms
	for i := 0; i < 10; i++ {
		c.Record("POST", "/b", 503, 40*time.Millisecond)
	}
	s := c.Snapshot()

	a, ok := find(s, "GET", "/a")
	if !ok {
		t.Fatal("缺少 GET /a")
	}
	// 30 次中错误 2 次（400、500）：6.67%
	if a.QPS != 1 || a.AvgMs != 10 || a.ErrorRate != 6.67 {
		t.Errorf("GET /a 统计不对: %+v", a)
	}
	b, _ := find(s, "POST", "/b")
	if b.ErrorRate != 100 || b.AvgMs != 40 {
		t.Errorf("POST /b 统计不对: %+v", b)
	}
	// 全局：40 次 / 30 秒 = 1.3；平均耗时 (30*10+10*40)/40 = 17.5；错误 12/40 = 30%
	if s.QPS != 1.3 || s.AvgMs != 17.5 || s.ErrorRate != 30 {
		t.Errorf("全局汇总不对: %+v", s)
	}
	// 按 QPS 降序
	if len(s.APIs) != 2 || s.APIs[0].Path != "/a" {
		t.Errorf("应按 QPS 降序: %+v", s.APIs)
	}
}

func TestSameRouteDifferentMethodsAreSeparate(t *testing.T) {
	c := steady()
	c.Record("GET", "/u", 200, time.Millisecond)
	c.Record("DELETE", "/u", 200, time.Millisecond)
	if got := len(c.Snapshot().APIs); got != 2 {
		t.Fatalf("方法不同应分开统计，得到 %d 项", got)
	}
}

func TestSnapshotIgnoresBucketsOutsideWindow(t *testing.T) {
	c := steady()
	c.Record("GET", "/old", 500, time.Millisecond)
	// 把唯一的桶挪到窗口之外（31 秒前）：不应再出现，也不影响全局
	for i := range c.routes["GET /old"].ring {
		if c.routes["GET /old"].ring[i].n > 0 {
			c.routes["GET /old"].ring[i].sec = time.Now().Unix() - window - 1
		}
	}
	c.Record("GET", "/new", 200, time.Millisecond)
	s := c.Snapshot()
	if _, ok := find(s, "GET", "/old"); ok {
		t.Error("窗口外的数据不应出现在快照里")
	}
	if s.ErrorRate != 0 {
		t.Errorf("窗口外的错误不应计入全局错误率: %v", s.ErrorRate)
	}
}

func TestRingBucketResetOnReuse(t *testing.T) {
	c := steady()
	c.Record("GET", "/r", 500, time.Millisecond)
	r := c.routes["GET /r"]
	// 把已有桶标记为“同槽位的旧秒”，再记录一次：旧数据必须被清掉而不是累加
	now := time.Now().Unix()
	for i := range r.ring {
		if r.ring[i].n > 0 {
			r.ring[i].sec = now - window // 同一个槽位 (sec % window 相同)，但已是旧的一轮
		}
	}
	c.Record("GET", "/r", 200, time.Millisecond)
	st, ok := find(c.Snapshot(), "GET", "/r")
	if !ok || st.ErrorRate != 0 {
		t.Fatalf("旧一轮的错误不应残留: %+v ok=%v", st, ok)
	}
}

func TestDaysSummaryAndPrune(t *testing.T) {
	c := steady()
	c.days["2000-01-01"] = &day{requests: 9}
	c.Record("GET", "/d", 200, time.Millisecond)
	c.Record("GET", "/d", 500, time.Millisecond)
	if _, still := c.days["2000-01-01"]; still {
		t.Error("超过保留天数的日汇总应在新增一天时被清理")
	}
	days := c.Days(3)
	if len(days) != 3 {
		t.Fatalf("Days(3) 应返回 3 项，得到 %d", len(days))
	}
	if !days[0].Date.Before(days[1].Date) || !days[1].Date.Before(days[2].Date) {
		t.Error("应按时间升序")
	}
	if days[0].Requests != 0 || days[1].Requests != 0 {
		t.Error("没有数据的日子应为 0")
	}
	if days[2].Requests != 2 || days[2].Errors != 1 {
		t.Errorf("今天应为 2 次请求 1 次错误: %+v", days[2])
	}
}

func TestMiddlewareUsesRouteTemplateAndSkipsUnmatched(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c := steady()
	r := gin.New()
	r.Use(Middleware(c))
	r.GET("/api/v1/users/:id", func(ctx *gin.Context) { ctx.Status(http.StatusInternalServerError) })

	for _, p := range []string{"/api/v1/users/1", "/api/v1/users/2", "/api/v1/nope/1", "/api/v1/nope/2"} {
		r.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, p, nil))
	}
	s := c.Snapshot()
	if len(s.APIs) != 1 {
		t.Fatalf("不同 id 应归并为一个路由模板，且未匹配路由（404）不记录，避免 key 无限增长: %+v", s.APIs)
	}
	st, ok := find(s, "GET", "/users/:id")
	if !ok {
		t.Fatalf("路径应为去掉 /api/v1 前缀的模板: %+v", s.APIs)
	}
	if st.ErrorRate != 100 {
		t.Errorf("500 应计为错误: %+v", st)
	}
}

func TestInstallOncePerGroup(t *testing.T) {
	gin.SetMode(gin.TestMode)
	e := gin.New()
	g := e.Group("/api/v1")
	Install(g, g) // 同一个组传两次、再装一次，也只能挂一次中间件
	Install(g)
	g.GET("/install-once", func(ctx *gin.Context) { ctx.Status(http.StatusOK) })
	e.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/api/v1/install-once", nil))

	var n int64
	Default.mu.Lock()
	if r := Default.routes["GET /install-once"]; r != nil {
		for _, b := range r.ring {
			n += b.n
		}
	}
	Default.mu.Unlock()
	if n != 1 {
		t.Fatalf("同一个组只应统计一次，得到 %d", n)
	}
}
