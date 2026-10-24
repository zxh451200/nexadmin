package mcp_test

import (
	"bufio"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"nexadmin/core/mcp"
)

func TestMCPRejectsMissingOrUnauthorizedCredentials(t *testing.T) {
	v := newEnv(t)
	init := rpc(1, "initialize", initParams)
	if r := v.do("POST", "/api/v1/mcp", init); r.Code != 401 {
		t.Errorf("no token = %d, want 401", r.Code)
	}
	if r := v.do("POST", "/api/v1/mcp", init, bearer("nobody")); r.Code != 403 {
		t.Errorf("no mcp:list = %d, want 403", r.Code)
	}
	if r := v.do("POST", "/api/v1/mcp", init, bearer("alice")); r.Code != 200 {
		t.Errorf("authorized = %d %s", r.Code, r.Body)
	}
	// 应用签名（无用户身份）不能建立会话。
	if r := v.do("POST", "/api/v1/mcp", init, bearer("app"), "X-App-Key: k"); r.Code != 403 {
		t.Errorf("app initialize = %d, want 403", r.Code)
	}
	// 应用签名的外层请求不能被复用于 tools/call。
	list := v.do("POST", "/api/v1/mcp", rpc(2, "tools/list", ""), bearer("alice"))
	if !strings.Contains(list.Body, "unread-count") {
		t.Fatalf("tool list: %s", list.Body)
	}
	name := toolName(t, list.Body, "/api/v1/messages/unread-count")
	call := rpc(3, "tools/call", `{"name":"`+name+`","arguments":{}}`)
	text, isErr := toolResult(t, v.do("POST", "/api/v1/mcp", call, bearer("app"), "X-App-Key: k"))
	if !isErr || !strings.Contains(text, "application signatures cannot be reused") {
		t.Errorf("app tools/call = %q (%v)", text, isErr)
	}
}

func toolName(t *testing.T, body, pathPart string) string {
	t.Helper()
	var out struct {
		Result struct {
			Tools []struct{ Name, Description string }
		}
	}
	if err := json.Unmarshal([]byte(body), &out); err != nil {
		t.Fatal(err)
	}
	for _, tl := range out.Result.Tools {
		if strings.Contains(tl.Description, pathPart) {
			return tl.Name
		}
	}
	t.Fatalf("tool for %s not found in %s", pathPart, body)
	return ""
}

func TestMCPOriginValidation(t *testing.T) {
	v := newEnv(t)
	ping := rpc(1, "ping", "")
	cases := map[string]int{
		"":                          200, // 非浏览器客户端不带 Origin
		"http://example.com":        200, // 与 Host 同源
		"https://example.com":       200,
		"http://evil.example.org":   403, // DNS rebinding / 跨站
		"http://example.com.evil.x": 403,
		"null":                      403,
		"http://example.com/path":   403,
		"http://u:p@example.com":    403,
		"ftp://example.com":         403,
	}
	for origin, want := range cases {
		h := []string{bearer("alice")}
		if origin != "" {
			h = append(h, "Origin: "+origin)
		}
		if r := v.do("POST", "/api/v1/mcp", ping, h...); r.Code != want {
			t.Errorf("Origin %q = %d, want %d", origin, r.Code, want)
		}
	}
	if r := v.do("POST", "/api/v1/mcp", ping, bearer("alice"), "MCP-Protocol-Version: 1999-01-01"); r.Code != 400 {
		t.Errorf("unsupported protocol version = %d", r.Code)
	}
}

func TestMCPBodyAndContentTypeLimits(t *testing.T) {
	v := newEnv(t)
	big := `{"jsonrpc":"2.0","id":1,"method":"ping","params":{"pad":"` + strings.Repeat("a", 1<<20) + `"}}`
	if r := v.do("POST", "/api/v1/mcp", big, bearer("alice")); r.Code != http.StatusRequestEntityTooLarge {
		t.Errorf("oversized body = %d, want 413", r.Code)
	}
	if r := v.do("POST", "/api/v1/mcp", rpc(1, "ping", ""), bearer("alice"), "Content-Type: text/plain"); r.Code != http.StatusUnsupportedMediaType {
		t.Errorf("content type = %d, want 415", r.Code)
	}
	// 批量请求不支持。
	if r := v.do("POST", "/api/v1/mcp", `[`+rpc(1, "ping", "")+`]`, bearer("alice")); !strings.Contains(r.Body, "-32600") {
		t.Errorf("batch must be rejected: %s", r.Body)
	}
}

// slowloris：慢速（卡住）的请求体不能占用 64 个并发槽；超时后连接得到 408。
func TestMCPSlowBodyDoesNotHoldConcurrencySlots(t *testing.T) {
	defer mcp.SetBodyReadTimeout(400 * time.Millisecond)()
	v := newEnv(t)
	ts := httptest.NewServer(v.e)
	defer ts.Close()
	u, _ := url.Parse(ts.URL)

	const stalled = 70 // 超过每实例 64 个并发槽
	var wg sync.WaitGroup
	statuses := make(chan string, stalled)
	conns := make([]net.Conn, 0, stalled)
	for i := 0; i < stalled; i++ {
		c, err := net.Dial("tcp", u.Host)
		if err != nil {
			t.Fatal(err)
		}
		conns = append(conns, c)
		// 声明 1000 字节 body 但一个字节都不发。
		_, _ = c.Write([]byte("POST /api/v1/mcp HTTP/1.1\r\nHost: " + u.Host + "\r\nAuthorization: Bearer alice\r\n" +
			"Content-Type: application/json\r\nContent-Length: 1000\r\n\r\n"))
		wg.Add(1)
		go func(c net.Conn) {
			defer wg.Done()
			_ = c.SetReadDeadline(time.Now().Add(5 * time.Second))
			line, _ := bufio.NewReader(c).ReadString('\n')
			statuses <- strings.TrimSpace(line)
		}(c)
	}
	defer func() {
		for _, c := range conns {
			_ = c.Close()
		}
	}()
	time.Sleep(100 * time.Millisecond) // 让卡住的请求都进入读 body 阶段
	req, _ := http.NewRequest("POST", ts.URL+"/api/v1/mcp", strings.NewReader(rpc(1, "ping", "")))
	req.Header.Set("Authorization", "Bearer alice")
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("normal request while %d bodies are stalled = %d, want 200 (slots must not be held)", stalled, resp.StatusCode)
	}
	wg.Wait()
	close(statuses)
	for s := range statuses {
		if !strings.Contains(s, "408") {
			t.Fatalf("stalled request got %q, want 408", s)
		}
	}
}

func TestMCPSessionBindingAndLifecycle(t *testing.T) {
	v := newEnv(t)
	r := v.do("POST", "/api/v1/mcp", rpc(1, "initialize", initParams), bearer("alice"))
	sid := r.Header.Get("Mcp-Session-Id")
	if r.Code != 200 || len(sid) != 64 {
		t.Fatalf("initialize: %d %q %s", r.Code, sid, r.Body)
	}
	ping := rpc(2, "ping", "")
	if r := v.do("POST", "/api/v1/mcp", ping, bearer("alice"), "Mcp-Session-Id: "+sid); r.Code != 200 {
		t.Errorf("owner with session = %d %s", r.Code, r.Body)
	}
	// 其他用户、其他租户、同一用户的另一份凭据都不能使用这个会话（不泄露存在性：统一 404）。
	for _, tok := range []string{"bob", "carol", "alice2"} {
		if r := v.do("POST", "/api/v1/mcp", ping, bearer(tok), "Mcp-Session-Id: "+sid); r.Code != 404 {
			t.Errorf("%s using alice's session = %d, want 404", tok, r.Code)
		}
	}
	// 格式非法的会话 ID。
	for _, bad := range []string{"short", strings.Repeat("z", 64), sid + "00"} {
		if r := v.do("POST", "/api/v1/mcp", ping, bearer("alice"), "Mcp-Session-Id: "+bad); r.Code != 404 {
			t.Errorf("bad session id %q = %d, want 404", bad, r.Code)
		}
	}
	// 他人不能删除；本人删除后会话失效。
	if r := v.do("DELETE", "/api/v1/mcp", "", bearer("bob"), "Mcp-Session-Id: "+sid); r.Code != 404 {
		t.Errorf("bob delete = %d, want 404", r.Code)
	}
	if r := v.do("DELETE", "/api/v1/mcp", "", bearer("alice"), "Mcp-Session-Id: "+sid); r.Code != 204 {
		t.Errorf("owner delete = %d, want 204", r.Code)
	}
	if r := v.do("POST", "/api/v1/mcp", ping, bearer("alice"), "Mcp-Session-Id: "+sid); r.Code != 404 {
		t.Errorf("deleted session = %d, want 404", r.Code)
	}
}
