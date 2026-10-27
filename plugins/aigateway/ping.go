package aigateway

import (
	"context"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"nexadmin/pkg/idgen"
)

// 连通性测试：对 OpenAI 兼容端点发一次 GET {base}/v1/models（最便宜的鉴权探测），
// 任何 2xx 视为可达。只验证"配置可用"，不产生 token 消耗。
func (s *Service) Ping(ctx context.Context, id idgen.ID) (*PingResult, error) {
	m, err := s.get(ctx, id)
	if err != nil {
		return nil, err
	}
	if m.BaseURL == "" {
		return &PingResult{OK: false, Detail: "base url is empty"}, nil
	}
	u, err := validateURL(ctx, m.BaseURL, func(ctx context.Context, host string) ([]net.IP, error) {
		return net.DefaultResolver.LookupIP(ctx, "ip", host)
	})
	if err != nil {
		return &PingResult{OK: false, Detail: err.Error()}, nil
	}
	key, _ := secretDecrypt(m.APIKeyEnc)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, probeEndpoint(u), nil)
	if err != nil {
		return &PingResult{OK: false, Detail: err.Error()}, nil
	}
	if key != "" {
		req.Header.Set("Authorization", "Bearer "+key)
	}
	start := time.Now()
	client := newSafeClient(10 * time.Second)
	resp, err := client.Do(req)
	latency := time.Since(start).Milliseconds()
	if err != nil {
		return &PingResult{OK: false, LatencyMs: latency, Detail: err.Error()}, nil
	}
	defer func() { _ = resp.Body.Close() }()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return &PingResult{OK: false, Status: resp.StatusCode, LatencyMs: latency,
			Detail: http.StatusText(resp.StatusCode)}, nil
	}
	return &PingResult{OK: true, Status: resp.StatusCode, LatencyMs: latency}, nil
}

// probeEndpoint 拼探测地址：OpenAI 兼容（含 vLLM/Ollama 的 OpenAI 模式）统一
// /v1/models；base 已以 /v1 结尾时只接 /models。
func probeEndpoint(u *url.URL) string {
	base := strings.TrimRight(u.String(), "/")
	if strings.HasSuffix(u.Path, "/v1") {
		return base + "/models"
	}
	return base + "/v1/models"
}
