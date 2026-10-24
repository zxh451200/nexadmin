package ai

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// fakeResolver 模拟 DNS：每次解析按顺序返回下一组地址（用来模拟 rebinding）。
type fakeResolver struct {
	answers [][]string
	calls   atomic.Int32
}

func (f *fakeResolver) LookupIPAddr(_ context.Context, _ string) ([]net.IPAddr, error) {
	i := int(f.calls.Add(1)) - 1
	if i >= len(f.answers) {
		i = len(f.answers) - 1
	}
	var out []net.IPAddr
	for _, s := range f.answers[i] {
		out = append(out, net.IPAddr{IP: net.ParseIP(s)})
	}
	return out, nil
}

func TestValidateBaseURLRejections(t *testing.T) {
	r := &fakeResolver{answers: [][]string{{"93.184.216.34"}}}
	bad := map[string]string{
		"scheme ftp":         "ftp://api.example.com/v1",
		"scheme file":        "file:///etc/passwd",
		"no host":            "http:///v1",
		"userinfo":           "https://user:pw@api.example.com/v1",
		"aws metadata":       "http://169.254.169.254/latest/meta-data",
		"link local":         "http://169.254.10.10/",
		"aws ipv6 metadata":  "http://[fd00:ec2::254]/",
		"ipv6 link local":    "http://[fe80::1]/",
		"alibaba metadata":   "http://100.100.100.200/",
		"mapped metadata":    "http://[::ffff:169.254.169.254]/",
		"nat64 metadata":     "http://[64:ff9b::a9fe:a9fe]/",
		"gcp metadata name":  "http://metadata.google.internal/computeMetadata/v1/",
		"loopback v4":        "http://127.0.0.1:11434/v1",
		"loopback v6":        "http://[::1]:11434/v1",
		"localhost":          "http://localhost:11434/v1",
		"private 10/8":       "http://10.1.2.3/v1",
		"private 172.16/12":  "http://172.20.0.5/v1",
		"private 192.168/16": "http://192.168.1.10/v1",
		"cgnat":              "http://100.64.1.1/v1",
		"unspecified":        "http://0.0.0.0/v1",
		"ula ipv6":           "http://[fd12::1]/v1",
	}
	for name, raw := range bad {
		t.Run(name, func(t *testing.T) {
			if err := ValidateBaseURL(context.Background(), raw, false, r); err == nil {
				t.Fatalf("%s must be rejected", raw)
			}
		})
	}
	// 即使允许私网，链路本地 / 元数据也永远拒绝。
	for _, raw := range []string{"http://169.254.169.254/", "http://[fd00:ec2::254]/", "http://100.100.100.200/", "http://metadata.google.internal/"} {
		if err := ValidateBaseURL(context.Background(), raw, true, r); err == nil {
			t.Fatalf("%s must be rejected even with allowPrivate", raw)
		}
	}
	// 主机名解析到元数据 / 私网地址（含混合应答）。
	for name, answers := range map[string][]string{
		"dns to metadata": {"169.254.169.254"},
		"dns to private":  {"10.0.0.8"},
		"dns mixed":       {"93.184.216.34", "10.0.0.8"},
	} {
		rr := &fakeResolver{answers: [][]string{answers}}
		if err := ValidateBaseURL(context.Background(), "https://llm.example.com/v1", false, rr); err == nil {
			t.Fatalf("%s must be rejected", name)
		}
	}
}

func TestValidateBaseURLAllowed(t *testing.T) {
	r := &fakeResolver{answers: [][]string{{"93.184.216.34"}}}
	if err := ValidateBaseURL(context.Background(), "https://api.example.com/v1", false, r); err != nil {
		t.Fatalf("public host: %v", err)
	}
	for _, raw := range []string{"http://localhost:11434/v1", "http://127.0.0.1:11434/v1", "http://10.1.2.3/v1", "http://192.168.1.10:8000/v1"} {
		if err := ValidateBaseURL(context.Background(), raw, true, r); err != nil {
			t.Fatalf("%s with allowPrivate: %v", raw, err)
		}
	}
}

func chatOK(w http.ResponseWriter, _ *http.Request) {
	_ = json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{
		"message": map[string]any{"content": "hi"}, "finish_reason": "stop"}}})
}

// 默认拒绝回环：即使配置里写了 127.0.0.1，实际连接也被拨号校验拦住，请求根本到不了上游。
func TestClientBlocksLoopbackByDefault(t *testing.T) {
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { hits.Add(1); chatOK(w, r) }))
	defer srv.Close()
	c := &Client{}
	cfg := Config{Enabled: true, BaseURL: srv.URL, APIKey: "k", Model: "m", Timeout: 2 * time.Second}
	if _, err := c.Chat(context.Background(), cfg, "s", "u", 16); !errors.Is(err, ErrUpstream) {
		t.Fatalf("err = %v, want ErrUpstream", err)
	}
	if hits.Load() != 0 {
		t.Fatal("blocked address must never be contacted")
	}
	cfg.AllowPrivate = true
	if _, err := c.Chat(context.Background(), cfg, "s", "u", 16); err != nil {
		t.Fatalf("allowPrivate should pass: %v", err)
	}
	if hits.Load() != 1 {
		t.Fatalf("hits = %d", hits.Load())
	}
}

// DNS rebinding：校验时解析到公网地址，连接时（第二次解析）解析到回环 => 连接被拒绝。
func TestClientBlocksDNSRebinding(t *testing.T) {
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { hits.Add(1); chatOK(w, r) }))
	defer srv.Close()
	u, _ := url.Parse(srv.URL)
	_, port, _ := net.SplitHostPort(u.Host)
	// 保存时（ValidateBaseURL）看到公网地址；之后每次连接都解析成回环。
	r := &fakeResolver{answers: [][]string{{"93.184.216.34"}, {"127.0.0.1"}}}
	base := "http://rebind.example.com:" + port + "/v1"
	if err := ValidateBaseURL(context.Background(), base, false, r); err != nil {
		t.Fatalf("validate at save time: %v", err)
	}
	c := &Client{Resolver: r}
	cfg := Config{Enabled: true, BaseURL: base, APIKey: "k", Model: "m", Timeout: 2 * time.Second}
	if _, err := c.Chat(context.Background(), cfg, "s", "u", 16); err == nil {
		t.Fatal("rebinding to loopback must be blocked at connect time")
	}
	if hits.Load() != 0 {
		t.Fatal("rebinding target must not be contacted")
	}
	// 直接断言拨号层的拒绝原因（Client 对外只给分类错误），并用对照组证明「主机名 -> 回环」本身是能连通的。
	r2 := &fakeResolver{answers: [][]string{{"127.0.0.1"}}}
	_, err := guardedTransport(false, r2).DialContext(context.Background(), "tcp", "rebind.example.com:"+port)
	if !errors.Is(err, ErrBlockedAddress) {
		t.Fatalf("dial err = %v, want ErrBlockedAddress", err)
	}
	conn, err := guardedTransport(true, r2).DialContext(context.Background(), "tcp", "rebind.example.com:"+port)
	if err != nil {
		t.Fatalf("control (allowPrivate) should connect: %v", err)
	}
	_ = conn.Close()
}

// 重定向保持禁用：上游 302 到别处时不跟随，也不会把 Key 发过去。
func TestClientDoesNotFollowRedirects(t *testing.T) {
	var leaked atomic.Int32
	other := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.Header.Get("Authorization"), "SECRET") {
			leaked.Add(1)
		}
		chatOK(w, r)
	}))
	defer other.Close()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, other.URL+"/steal", http.StatusFound)
	}))
	defer srv.Close()
	c := &Client{}
	cfg := Config{Enabled: true, AllowPrivate: true, BaseURL: srv.URL, APIKey: "SECRET", Model: "m", Timeout: 2 * time.Second}
	if _, err := c.Chat(context.Background(), cfg, "s", "u", 16); err == nil {
		t.Fatal("redirect response must be an error")
	}
	if leaked.Load() != 0 {
		t.Fatal("key leaked through redirect")
	}
}

// 改了 base_url 但设置页没有重新给 Key 时，运行时不得把 config.yaml 的旧 Key 发往新地址。
func TestFromSettingsDropsFallbackKeyForNewBaseURL(t *testing.T) {
	fb := Config{BaseURL: "https://old.example.com/v1", APIKey: "OLD-KEY", Model: "m"}
	same := FromSettings(map[string]any{"baseUrl": "https://old.example.com/v1"}, fb)
	if same.APIKey != "OLD-KEY" {
		t.Fatalf("same url keeps fallback key, got %q", same.APIKey)
	}
	moved := FromSettings(map[string]any{"baseUrl": "https://new.example.com/v1"}, fb)
	if moved.APIKey != "" {
		t.Fatalf("new url must not inherit the fallback key, got %q", moved.APIKey)
	}
	withKey := FromSettings(map[string]any{"baseUrl": "https://new.example.com/v1", "apiKey": "NEW"}, fb)
	if withKey.APIKey != "NEW" {
		t.Fatalf("explicit key must win, got %q", withKey.APIKey)
	}
}
