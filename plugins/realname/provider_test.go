package realname

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestNewProviderOnlyWhenEnabled(t *testing.T) {
	cases := []struct {
		name    string
		cfg     *Config
		wantNil bool
		wantErr string
	}{
		{"未配置", nil, true, ""},
		{"开关关闭", &Config{Enabled: false, Provider: ProviderGeneric, Endpoint: "https://x.example.com"}, true, ""},
		{"开关开但没选服务商", &Config{Enabled: true}, true, ""},
		{"https 齐备", enabledConfig("https://x.example.com/match", "k", 10), false, ""},
		{"http 地址", &Config{Enabled: true, Provider: ProviderGeneric, Endpoint: "http://x.example.com"}, false, "realname.endpoint_invalid"},
		{"没填地址", &Config{Enabled: true, Provider: ProviderGeneric}, false, "realname.endpoint_invalid"},
		{"未接入的服务商", &Config{Enabled: true, Provider: "aliyun", Endpoint: "https://x.example.com"}, false, "realname.provider_unknown"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			prov, err := newProvider(tc.cfg, nil)
			if tc.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("want err %q, got %v", tc.wantErr, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected err: %v", err)
			}
			if (prov == nil) != tc.wantNil {
				t.Fatalf("provider nil = %v, want %v", prov == nil, tc.wantNil)
			}
		})
	}
}

// 上游异常一律算「核验未完成」，不能降级成"不一致"：把网络故障写成 mismatch 会误判客户身份。
func TestGenericProviderUpstreamAnomalies(t *testing.T) {
	cases := []struct {
		name   string
		status int
		body   string
	}{
		{"非 200", http.StatusInternalServerError, `{"matched":true}`},
		{"缺 matched", http.StatusOK, `{"message":"配额不足"}`},
		{"非 JSON", http.StatusOK, `<html>gateway error</html>`},
		{"空响应", http.StatusOK, ``},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte(tc.body))
			}))
			defer srv.Close()

			prov, err := newProvider(enabledConfig(srv.URL, "k", 0), srv.Client())
			if err != nil {
				t.Fatal(err)
			}
			if _, err := prov.Verify(context.Background(), VerifyRequest{Name: "张三", Number: validNumber}); err == nil {
				t.Fatal("异常响应应报错，而不是返回一个结论")
			}
		})
	}
}
