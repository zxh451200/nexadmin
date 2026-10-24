package cachemonitor

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/redis/go-redis/v9"

	"nexadmin/core"
	"nexadmin/core/auth"
	"nexadmin/core/cache"
	"nexadmin/core/config"
)

func TestParseInfo(t *testing.T) {
	raw := "# Server\r\nredis_version:7.2.4\r\nuptime_in_seconds:120\r\n\r\n# Stats\r\nkeyspace_hits:3\r\nbad line without colon\r\nkey_with_colon:a:b\r\n"
	got := parseInfo(raw)
	if got["redis_version"] != "7.2.4" || got["uptime_in_seconds"] != "120" || got["keyspace_hits"] != "3" {
		t.Errorf("字段解析不对: %v", got)
	}
	if got["key_with_colon"] != "a:b" {
		t.Errorf("只按第一个冒号切分: %q", got["key_with_colon"])
	}
	if _, ok := got["# Server"]; ok || len(got) != 4 {
		t.Errorf("注释行和无冒号行应被忽略: %v", got)
	}
}

func TestAtoi(t *testing.T) {
	for in, want := range map[string]int64{"42": 42, " 7 ": 7, "": 0, "abc": 0, "1.5": 0, "-3": -3} {
		if got := atoi(in); got != want {
			t.Errorf("atoi(%q)=%d 期望 %d", in, got, want)
		}
	}
}

func TestTopCommandStats(t *testing.T) {
	info := map[string]string{"redis_version": "7"}
	for i, name := range []string{"get", "set", "del", "hget", "hset", "expire", "ping", "scan"} {
		info["cmdstat_"+name] = "calls=" + strconv.Itoa((i+1)*100) + ",usec=1,usec_per_call=0.1"
	}
	info["cmdstat_broken"] = "usec=5" // 没有 calls
	got := topCommandStats(info)
	if len(got) != topCommands {
		t.Fatalf("应只保留前 %d 个，得到 %d", topCommands, len(got))
	}
	if got[0].Name != "SCAN" || got[0].Count != 800 {
		t.Errorf("应按调用次数降序且命令名大写: %+v", got[0])
	}
	for i := 1; i < len(got); i++ {
		if got[i].Count > got[i-1].Count {
			t.Errorf("未降序: %+v", got)
		}
	}
	if empty := topCommandStats(map[string]string{}); empty == nil || len(empty) != 0 {
		t.Errorf("没有命令统计时应是空数组而非 nil: %#v", empty)
	}
}

func TestPrefixOf(t *testing.T) {
	for _, tc := range []struct{ key, prefix, name string }{
		{"nex:t123:session:abc", "nex:session:*", "session"}, // 去掉租户段，多租户的同类 key 归并
		{"nex:t7:online:s:99", "nex:online:*", "online"},
		{"nex:session:abc", "nex:session:*", "session"}, // 平台级 key 没有租户段
		{"nex:tx:foo", "nex:tx:*", "tx"},                // t 后不是纯数字，不是租户段
		{"nex:t12", "nex:t12:*", "t12"},                 // 只有一段时不能把它吞掉
		{"nex:auth:revoked:j1", "nex:auth:*", "auth"},
	} {
		p, n := prefixOf(tc.key)
		if p != tc.prefix || n != tc.name {
			t.Errorf("prefixOf(%q)=(%q,%q) 期望 (%q,%q)", tc.key, p, n, tc.prefix, tc.name)
		}
	}
}

func TestSnapshotMemoryDriver(t *testing.T) {
	m := &monitor{driver: "memory"}
	s := m.snapshot(context.Background())
	if s.Driver != "memory" || s.Version != "memory" || s.TS == 0 || s.UsedMemoryMB <= 0 {
		t.Errorf("memory 驱动快照不对: %+v", s)
	}
	if s.Commands == nil || s.Prefixes == nil || s.Keys != 0 || s.HitRate != 0 {
		t.Errorf("memory 驱动的数组应为空而非 nil，key 数/命中率为 0: %+v", s)
	}
}

func TestSnapshotRedisUnreachableDegrades(t *testing.T) {
	m := &monitor{driver: "redis", cli: redis.NewClient(&redis.Options{Addr: "127.0.0.1:1"})} // 连接被拒绝
	t.Cleanup(func() { _ = m.cli.Close() })
	s := m.snapshot(context.Background())
	if s.Driver != "valkey" || s.Version != "unavailable" {
		t.Errorf("INFO 失败应降级而不是 panic/报错: %+v", s)
	}
	if s.Commands == nil || s.Prefixes == nil {
		t.Error("降级时数组也必须非 nil")
	}
}

func serve(t *testing.T, cl *auth.Claims) *httptest.ResponseRecorder {
	t.Helper()
	gin.SetMode(gin.TestMode)
	e := gin.New()
	g := e.Group("/api/v1")
	if cl != nil {
		g.Use(func(c *gin.Context) { auth.SetClaims(c, cl) })
	}
	cfg := &config.Config{}
	cfg.Cache = config.Cache{Driver: "memory"}
	module{}.Register(g, &core.Deps{Config: cfg})
	w := httptest.NewRecorder()
	e.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/v1/monitor/cache", nil))
	return w
}

func TestRoutePermission(t *testing.T) {
	if w := serve(t, nil); w.Code != http.StatusUnauthorized {
		t.Errorf("未登录应 401，得到 %d", w.Code)
	}
	if w := serve(t, &auth.Claims{Principal: auth.PrincipalApp}); w.Code != http.StatusForbidden {
		t.Errorf("无 cacheMonitor:list 应 403，得到 %d", w.Code)
	}
	w := serve(t, &auth.Claims{Principal: auth.PrincipalApp, Perms: []string{PermList}})
	var body struct {
		Data Snapshot `json:"data"`
	}
	if w.Code != http.StatusOK || json.Unmarshal(w.Body.Bytes(), &body) != nil || body.Data.Driver != "memory" {
		t.Errorf("有权限应 200 且为 memory 快照: %d %s", w.Code, w.Body.String())
	}
}

var _ = cache.Prefix // prefixOf 依赖 cache.Prefix == "nex:"，该常量变化时上面的用例会提醒更新
