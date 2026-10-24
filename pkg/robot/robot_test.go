package robot

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// 向量由独立的 Python hmac/hashlib/base64 按各家文档描述的算法计算（本环境无外网，未直接从官方页面摘录）：
//
//	dingtalk: ts=1577262236757 secret="this is secret"  → HMAC-SHA256(key=secret, msg=ts+"\n"+secret)
//	feishu:   ts=1599360473    secret="demo"            → HMAC-SHA256(key=ts+"\n"+secret, msg="")
func TestSignVectors(t *testing.T) {
	if got := dingtalkSign(1577262236757, "this is secret"); got != "hmPWwU+7lVdm3ZZz0r9tSfx0L4Q26jWOZr9+Gs6EZQM=" {
		t.Fatalf("dingtalk sign = %s", got)
	}
	if got := feishuSign(1599360473, "demo"); got != "l1N0gAcBjdwBvGm1xMjOF0XSyaLRpR7tuO5dHfhAYc8=" {
		t.Fatalf("feishu sign = %s", got)
	}
}

func testClient(t *testing.T, typ, secret string, md bool, h http.HandlerFunc) (*Client, *httptest.Server) {
	t.Helper()
	srv := httptest.NewTLSServer(h)
	t.Cleanup(srv.Close)
	c, err := New(Config{
		Type: typ, URL: srv.URL + "/send?access_token=tok", Secret: secret, Markdown: md, Timeout: 3 * time.Second,
		allowLoopback: true, tlsConfig: &tls.Config{InsecureSkipVerify: true}, //nolint:gosec // 测试自签证书
		now: func() time.Time { return time.UnixMilli(1577262236757) },
	})
	if err != nil {
		t.Fatal(err)
	}
	return c, srv
}

func TestSendPayloads(t *testing.T) {
	var gotURL string
	var gotBody map[string]any
	capture := func(reply string) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			gotURL = r.URL.String()
			b, _ := io.ReadAll(r.Body)
			gotBody = nil
			_ = json.Unmarshal(b, &gotBody)
			_, _ = io.WriteString(w, reply)
		}
	}
	ctx := context.Background()

	c, _ := testClient(t, DingTalk, "this is secret", false, capture(`{"errcode":0,"errmsg":"ok"}`))
	if err := c.Send(ctx, "T", "body"); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(gotURL, "timestamp=1577262236757") || !strings.Contains(gotURL, "access_token=tok") ||
		!strings.Contains(gotURL, "sign=hmPWwU%2B7lVdm3ZZz0r9tSfx0L4Q26jWOZr9%2BGs6EZQM%3D") {
		t.Fatalf("dingtalk url = %s", gotURL)
	}
	if gotBody["msgtype"] != "text" || gotBody["text"].(map[string]any)["content"] != "T\nbody" {
		t.Fatalf("dingtalk body = %v", gotBody)
	}
	c, _ = testClient(t, DingTalk, "", true, capture(`{"errcode":0}`))
	if err := c.Send(ctx, "T", "**b**"); err != nil || gotBody["msgtype"] != "markdown" || strings.Contains(gotURL, "sign=") {
		t.Fatalf("dingtalk markdown: %v %v %s", err, gotBody, gotURL)
	}

	c, _ = testClient(t, Feishu, "demo", false, capture(`{"code":0,"msg":"success"}`))
	if err := c.Send(ctx, "", "hello"); err != nil {
		t.Fatal(err)
	}
	if gotBody["msg_type"] != "text" || gotBody["timestamp"] != "1577262236" || gotBody["sign"] != feishuSign(1577262236, "demo") ||
		gotBody["content"].(map[string]any)["text"] != "hello" {
		t.Fatalf("feishu body = %v", gotBody)
	}
	c, _ = testClient(t, Feishu, "", true, capture(`{"StatusCode":0,"code":0}`))
	if err := c.Send(ctx, "T", "md"); err != nil || gotBody["msg_type"] != "interactive" {
		t.Fatalf("feishu markdown: %v %v", err, gotBody)
	}

	c, _ = testClient(t, WeCom, "ignored", true, capture(`{"errcode":0,"errmsg":"ok"}`))
	if err := c.Send(ctx, "T", "md"); err != nil || gotBody["msgtype"] != "markdown" || gotBody["sign"] != nil {
		t.Fatalf("wecom: %v %v", err, gotBody)
	}
	c, _ = testClient(t, WeCom, "", false, capture(`{"errcode":93000,"errmsg":"invalid webhook url"}`))
	if err := c.Send(ctx, "T", "x"); err == nil || !strings.Contains(err.Error(), "93000") {
		t.Fatalf("want api error, got %v", err)
	}
}

func TestRetryOnce(t *testing.T) {
	var n atomic.Int32
	c, _ := testClient(t, WeCom, "", false, func(w http.ResponseWriter, r *http.Request) {
		if n.Add(1) == 1 {
			w.WriteHeader(http.StatusBadGateway)
			return
		}
		_, _ = io.WriteString(w, `{"errcode":0}`)
	})
	if err := c.Send(context.Background(), "", "x"); err != nil || n.Load() != 2 {
		t.Fatalf("err=%v attempts=%d", err, n.Load())
	}
	n.Store(0)
	c, _ = testClient(t, WeCom, "", false, func(w http.ResponseWriter, r *http.Request) {
		n.Add(1)
		w.WriteHeader(http.StatusInternalServerError)
	})
	if err := c.Send(context.Background(), "", "x"); err == nil || n.Load() != 2 {
		t.Fatalf("want failure after exactly one retry, err=%v attempts=%d", err, n.Load())
	}
	n.Store(0) // 业务错误码不重试
	c, _ = testClient(t, WeCom, "", false, func(w http.ResponseWriter, r *http.Request) {
		n.Add(1)
		_, _ = io.WriteString(w, `{"errcode":40001}`)
	})
	if err := c.Send(context.Background(), "", "x"); err == nil || n.Load() != 1 {
		t.Fatalf("api errors must not be retried, err=%v attempts=%d", err, n.Load())
	}
}

func TestNoRedirect(t *testing.T) {
	var hits atomic.Int32
	c, _ := testClient(t, WeCom, "", false, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/internal" {
			hits.Add(1)
			_, _ = io.WriteString(w, `{"errcode":0}`)
			return
		}
		http.Redirect(w, r, "/internal", http.StatusFound)
	})
	if err := c.Send(context.Background(), "", "x"); err == nil {
		t.Fatal("redirect must be treated as failure")
	}
	if hits.Load() != 0 {
		t.Fatal("redirect target was requested")
	}
}

func TestSSRFRejected(t *testing.T) {
	for _, u := range []string{
		"http://oapi.dingtalk.com/robot/send", "ftp://example.com/x", "https://user:pw@example.com/x", "https:///x",
		"https://127.0.0.1/x", "https://10.1.2.3/x", "https://172.16.0.1/x", "https://192.168.1.1/x",
		"https://169.254.169.254/latest/meta-data", "https://0.0.0.0/x", "https://100.64.0.1/x",
		"https://[::1]/x", "https://[::ffff:127.0.0.1]/x", "https://[fe80::1]/x", "https://[fd00::1]/x", "https://[64:ff9b::7f00:1]/x",
	} {
		if _, err := New(Config{Type: DingTalk, URL: u}); err == nil {
			t.Errorf("%s should be rejected", u)
		}
	}
	if _, err := New(Config{Type: "slack", URL: "https://example.com/x"}); err == nil {
		t.Error("unknown type should be rejected")
	}
	if _, err := New(Config{Type: WeCom, URL: "https://qyapi.weixin.qq.com/cgi-bin/webhook/send?key=k"}); err != nil {
		t.Errorf("public https url should be accepted: %v", err)
	}
	// 域名解析到回环地址：静态检查放行，拨号时被拦截（防 DNS 指向内网 / 重绑定）。
	c, err := New(Config{Type: WeCom, URL: "https://localhost:9/x", Timeout: 2 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	if err := c.Send(context.Background(), "", "x"); !errors.Is(err, ErrBlockedAddr) {
		t.Fatalf("want ErrBlockedAddr at dial time, got %v", err)
	}
}

func TestBlockedTable(t *testing.T) {
	for ip, want := range map[string]bool{
		"8.8.8.8": false, "1.1.1.1": false, "2606:4700:4700::1111": false,
		"127.0.0.1": true, "10.0.0.1": true, "::ffff:10.0.0.1": true, "224.0.0.1": true, "240.0.0.1": true, "::": true,
	} {
		if got := blocked(netip.MustParseAddr(ip)); got != want {
			t.Errorf("blocked(%s) = %v, want %v", ip, got, want)
		}
	}
}
