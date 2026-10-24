package ai

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"sync"
)

// 分类错误：调用方只看到这些，上游响应体和错误细节不会外泄。
var (
	ErrDisabled     = errors.New("ai: disabled")
	ErrRateLimited  = errors.New("ai: rate limited")
	ErrBadInput     = errors.New("ai: bad input")
	ErrInputTooLong = errors.New("ai: input too long")
	ErrTimeout      = errors.New("ai: upstream timeout")
	ErrUpstream     = errors.New("ai: upstream error")
	ErrBadOutput    = errors.New("ai: unusable model output")
)

const maxRespBytes = 2 << 20 // 上游响应体上限，防止被撑爆内存

// Client 是 OpenAI 兼容 Chat Completions 客户端。零值可用。
type Client struct {
	// HTTP 非空时原样使用（测试注入）；为空时使用带 SSRF 拨号校验的默认客户端。
	HTTP *http.Client
	// Resolver 用于拨号校验的 DNS 解析；为空用系统解析器（测试可替换以模拟 rebinding）。
	Resolver Resolver

	mu      sync.Mutex
	guarded map[bool]*http.Client
}

// Result 是一次补全的结果与用量。
type Result struct {
	Content          string
	ToolCalls        []ToolCall
	FinishReason     string
	PromptTokens     int
	CompletionTokens int
	TotalTokens      int
}

type chatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type chatRequest struct {
	Model       string        `json:"model"`
	Messages    []chatMessage `json:"messages"`
	MaxTokens   int           `json:"max_tokens"`
	Temperature float64       `json:"temperature"`
	Stream      bool          `json:"stream"`
	Tools       []Tool        `json:"tools,omitempty"`
}

type chatResponse struct {
	Choices []struct {
		Message struct {
			Content   string     `json:"content"`
			ToolCalls []ToolCall `json:"tool_calls"`
		} `json:"message"`
		FinishReason string `json:"finish_reason"`
	} `json:"choices"`
	Usage struct {
		PromptTokens     int `json:"prompt_tokens"`
		CompletionTokens int `json:"completion_tokens"`
		TotalTokens      int `json:"total_tokens"`
	} `json:"usage"`
}

func (c *Client) httpClient(cfg Config) *http.Client {
	if c != nil && c.HTTP != nil {
		return c.HTTP
	}
	if c == nil {
		c = &Client{}
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if hc := c.guarded[cfg.AllowPrivate]; hc != nil {
		return hc
	}
	if c.guarded == nil {
		c.guarded = map[bool]*http.Client{}
	}
	hc := &http.Client{
		Transport: guardedTransport(cfg.AllowPrivate, c.Resolver),
		// 不跟随重定向：避免把带 Key 的请求转发到别处。
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
	c.guarded[cfg.AllowPrivate] = hc
	return hc
}

// Chat 发送一轮 system+user 对话。超时取 cfg.Timeout；失败只返回分类错误（带状态码，不带响应体）。
func (c *Client) Chat(ctx context.Context, cfg Config, system, user string, maxTokens int) (*Result, error) {
	return c.complete(ctx, cfg, []Message{{Role: "system", Content: system}, {Role: "user", Content: user}}, nil, maxTokens)
}

func (c *Client) complete(ctx context.Context, cfg Config, messages []Message, tools []Tool, maxTokens int) (*Result, error) {
	cfg = cfg.Normalize()
	if maxTokens <= 0 || maxTokens > cfg.MaxTokens {
		maxTokens = cfg.MaxTokens
	}
	body, err := json.Marshal(struct {
		Model       string    `json:"model"`
		Messages    []Message `json:"messages"`
		Tools       []Tool    `json:"tools,omitempty"`
		MaxTokens   int       `json:"max_tokens"`
		Temperature float64   `json:"temperature"`
		Stream      bool      `json:"stream"`
	}{
		Model:     cfg.Model,
		Messages:  messages,
		Tools:     tools,
		MaxTokens: maxTokens,
	})
	if err != nil {
		return nil, fmt.Errorf("%w: encode", ErrUpstream)
	}
	ctx, cancel := context.WithTimeout(ctx, cfg.Timeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, cfg.endpoint(), bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("%w: bad request", ErrUpstream)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	if cfg.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+cfg.APIKey)
	}
	resp, err := c.httpClient(cfg).Do(req)
	if err != nil {
		return nil, classify(ctx, err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxRespBytes))
	if err != nil {
		return nil, classify(ctx, err)
	}
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return nil, fmt.Errorf("%w: status %d", ErrUpstream, resp.StatusCode) // 不带响应体
	}
	var out chatResponse
	if json.Unmarshal(raw, &out) != nil || len(out.Choices) == 0 {
		return nil, fmt.Errorf("%w: malformed response", ErrUpstream)
	}
	ch := out.Choices[0]
	total := out.Usage.TotalTokens
	if total == 0 {
		total = out.Usage.PromptTokens + out.Usage.CompletionTokens
	}
	return &Result{
		Content: ch.Message.Content, FinishReason: ch.FinishReason,
		ToolCalls:    ch.Message.ToolCalls,
		PromptTokens: out.Usage.PromptTokens, CompletionTokens: out.Usage.CompletionTokens, TotalTokens: total,
	}, nil
}

// classify 把传输层错误归类；*url.Error 会带 URL，所以不拼接原错误。
func classify(ctx context.Context, err error) error {
	var ne net.Error
	if errors.Is(ctx.Err(), context.DeadlineExceeded) || errors.Is(err, context.DeadlineExceeded) || (errors.As(err, &ne) && ne.Timeout()) {
		return ErrTimeout
	}
	return fmt.Errorf("%w: transport", ErrUpstream)
}
