package ai

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// Message is an OpenAI compatible conversation message.
type Message struct {
	Role       string     `json:"role"`
	Content    string     `json:"content"`
	ToolCalls  []ToolCall `json:"tool_calls,omitempty"`
	ToolCallID string     `json:"tool_call_id,omitempty"`
}

type ToolCall struct {
	ID       string       `json:"id"`
	Type     string       `json:"type"`
	Function FunctionCall `json:"function"`
}

type FunctionCall struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

// Tool is supplied by trusted server code, never by model output.
// ReadOnly must be explicitly true before automatic execution is permitted.
type Tool struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	Parameters  json.RawMessage `json:"parameters"`
	ReadOnly    bool            `json:"-"`
}

// ToolExecutor must enforce current identity, tenant and resource permissions,
// honor ctx cancellation, and return JSON without credentials or private errors.
type ToolExecutor interface {
	Execute(ctx context.Context, who Caller, call ToolCall) (json.RawMessage, error)
}

type RunRequest struct {
	Messages    []Message     `json:"messages"`
	Tools       []Tool        `json:"tools,omitempty"`
	MaxRounds   int           `json:"max_rounds,omitempty"`
	Timeout     time.Duration `json:"-"`
	TokenBudget int           `json:"token_budget,omitempty"`
}

type RunResult struct {
	Status   string     `json:"status"`
	Messages []Message  `json:"messages"`
	Pending  []ToolCall `json:"pending,omitempty"`
	Rounds   int        `json:"rounds"`
	Tokens   int        `json:"tokens"`
}

var (
	ErrRounds   = errors.New("ai: maximum rounds reached")
	ErrRepeated = errors.New("ai: repeated tool arguments")
	ErrBudget   = errors.New("ai: token budget exhausted")
	ErrTool     = errors.New("ai: tool execution failed")
)

const (
	DefaultRunRounds   = 6
	MaxRunRounds       = 16
	DefaultRunTokens   = 16384
	MaxRunTokens       = 65536
	DefaultRunTimeout  = 90 * time.Second
	MaxRunBytes        = 256 << 10
	MaxToolResultBytes = 64 << 10
)

// Run executes a bounded conversation. Each model round uses the existing
// configuration, limiter, quota and audit. It never executes write tools.
// A confirmation result is a handoff, not an approval or durable session.
// Content is redacted only in the model-bound copy using each round's config.
// Tool arguments and definitions retain their semantics; executors must limit
// sensitive data in those fields and must not treat masks as resource IDs.
func (g *Gateway) Run(ctx context.Context, who Caller, in RunRequest, executor ToolExecutor) (*RunResult, error) {
	if in.MaxRounds == 0 {
		in.MaxRounds = DefaultRunRounds
	}
	if in.Timeout == 0 {
		in.Timeout = DefaultRunTimeout
	}
	if in.TokenBudget == 0 {
		in.TokenBudget = DefaultRunTokens
	}
	if in.MaxRounds < 1 || in.MaxRounds > MaxRunRounds || in.Timeout < 0 || in.Timeout > MaxTimeout ||
		in.TokenBudget < 1 || in.TokenBudget > MaxRunTokens || len(in.Messages) == 0 ||
		len(in.Messages) > 128 || len(in.Tools) > 64 {
		return nil, ErrBadInput
	}
	// Clone caller-owned data; the loop never mutates the input transcript.
	raw, err := json.Marshal(in)
	if err != nil || len(raw) > MaxRunBytes {
		return nil, ErrInputTooLong
	}
	raw, _ = json.Marshal(in.Messages)
	out := &RunResult{Status: "running"}
	_ = json.Unmarshal(raw, &out.Messages)
	// Initial history may contain completed tool exchanges, but never dangling
	// calls or fabricated tool results.
	pending := map[string]bool{}
	allIDs := map[string]bool{}
	for _, m := range out.Messages {
		switch m.Role {
		case "system", "user":
			if len(pending) != 0 || len(m.ToolCalls) != 0 || m.ToolCallID != "" {
				return nil, ErrBadInput
			}
		case "assistant":
			if len(pending) != 0 || m.ToolCallID != "" || len(m.ToolCalls) > 32 {
				return nil, ErrBadInput
			}
			for _, c := range m.ToolCalls {
				if c.ID == "" || allIDs[c.ID] || c.Type != "function" || c.Function.Name == "" {
					return nil, ErrBadInput
				}
				if _, err := canonicalArguments(c.Function.Arguments); err != nil {
					return nil, ErrBadInput
				}
				allIDs[c.ID] = true
				pending[c.ID] = true
			}
		case "tool":
			if !pending[m.ToolCallID] || len(m.ToolCalls) != 0 {
				return nil, ErrBadInput
			}
			delete(pending, m.ToolCallID)
		default:
			return nil, ErrBadInput
		}
	}
	if len(pending) != 0 || out.Messages[len(out.Messages)-1].Role == "assistant" {
		return nil, ErrBadInput
	}
	tools := make(map[string]Tool, len(in.Tools))
	var wireTools []map[string]any
	for _, t := range in.Tools {
		var schema map[string]any
		if t.Name == "" || len(t.Name) > 64 || tools[t.Name].Name != "" ||
			json.Unmarshal(t.Parameters, &schema) != nil || schema == nil || schema["type"] != "object" {
			return nil, ErrBadInput
		}
		tools[t.Name] = t
		wireTools = append(wireTools, map[string]any{"type": "function", "function": map[string]any{
			"name": t.Name, "description": t.Description, "parameters": schema,
		}})
	}
	ctx, cancel := context.WithTimeout(ctx, in.Timeout)
	defer cancel()
	seen := map[string]bool{}
	fail := func(err error) (*RunResult, error) {
		out.Status = "stopped"
		if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, ErrTimeout) {
			out.Status = "timeout"
		} else if errors.Is(err, context.Canceled) {
			out.Status = "cancelled"
		}
		return out, err
	}
	for out.Rounds < in.MaxRounds {
		if err := ctx.Err(); err != nil {
			return fail(err)
		}
		// UTF-8 JSON byte length is a conservative local token estimate,
		// including tool schemas and all retransmitted history.
		envelope := map[string]any{"messages": out.Messages}
		if len(wireTools) != 0 {
			envelope["tools"] = wireTools
		}
		prompt, _ := json.Marshal(envelope)
		if len(prompt) > MaxRunBytes {
			return fail(ErrInputTooLong)
		}
		remaining := in.TokenBudget - out.Tokens - len(prompt)
		if remaining <= 0 {
			return fail(ErrBudget)
		}
		cfg, err := g.begin(ctx, who)
		if err != nil {
			return fail(err)
		}
		modelMessages, err := redactRunMessages(out.Messages, cfg.Redact)
		if err != nil {
			return fail(err)
		}
		envelope["messages"] = modelMessages
		modelPrompt, err := json.Marshal(envelope)
		if err != nil || len(modelPrompt) > MaxRunBytes {
			return fail(ErrInputTooLong)
		}
		// Reserve against the larger representation: redaction can expand a
		// short value into an escaped JSON string or a longer placeholder.
		if len(modelPrompt) > len(prompt) {
			prompt = modelPrompt
			remaining = in.TokenBudget - out.Tokens - len(prompt)
			if remaining <= 0 {
				return fail(ErrBudget)
			}
		}
		limit := cfg.MaxTokens
		if limit > remaining {
			limit = remaining
		}
		envelope["model"], envelope["max_tokens"] = cfg.Model, limit
		envelope["stream"], envelope["temperature"] = false, 0
		start := time.Now()
		message, usage, err := g.Client.conversation(ctx, cfg, envelope)
		out.Rounds++
		if err == nil {
			if message.Role != "assistant" || len(message.ToolCalls) > 32 || message.ToolCallID != "" {
				err = ErrBadOutput
			}
		}
		g.audit(who, "run", cfg.Model, time.Since(start), usage, err)
		if err != nil {
			return fail(err)
		}
		encoded, _ := json.Marshal(message)
		charged := len(prompt) + len(encoded)
		if usage.TotalTokens > charged {
			charged = usage.TotalTokens
		}
		out.Tokens += charged
		out.Messages = append(out.Messages, message)
		if out.Tokens > in.TokenBudget {
			return fail(ErrBudget)
		}
		if len(message.ToolCalls) == 0 {
			out.Status = "completed"
			return out, nil
		}
		// Validate the whole batch and detect writes before any execution.
		for _, call := range message.ToolCalls {
			t, ok := tools[call.Function.Name]
			canonical, err := canonicalArguments(call.Function.Arguments)
			if !ok || call.ID == "" || allIDs[call.ID] || call.Type != "function" || err != nil {
				return fail(ErrBadOutput)
			}
			allIDs[call.ID] = true
			key := call.Function.Name + ":" + string(canonical)
			if seen[key] {
				return fail(ErrRepeated)
			}
			seen[key] = true
			if !t.ReadOnly {
				out.Pending = append(out.Pending, call)
			}
		}
		if len(out.Pending) != 0 {
			out.Status = "awaiting_confirmation"
			return out, nil
		}
		if out.Rounds == in.MaxRounds {
			return fail(ErrRounds)
		}
		if executor == nil {
			return fail(ErrTool)
		}
		for _, call := range message.ToolCalls {
			if err := ctx.Err(); err != nil {
				return fail(err)
			}
			start := time.Now()
			result, err := executor.Execute(ctx, who, call)
			if err != nil {
				err = ErrTool
			} else if len(result) > MaxToolResultBytes || !json.Valid(result) {
				err = ErrBadOutput
			}
			if ctx.Err() != nil {
				err = ctx.Err()
			}
			g.audit(who, "run.tool", cfg.Model, time.Since(start), nil, err)
			if err != nil {
				return fail(err)
			}
			out.Messages = append(out.Messages, Message{Role: "tool", ToolCallID: call.ID, Content: string(result)})
		}
	}
	return fail(ErrRounds)
}

// Only Content is replaced. The shared ToolCalls slices are read-only here;
// neither caller-owned history nor the result transcript is rewritten.
func redactRunMessages(messages []Message, opts RedactOptions) ([]Message, error) {
	out := append([]Message(nil), messages...)
	if !opts.Any() {
		return out, nil
	}
	for i, m := range out {
		if m.Role != "tool" || !json.Valid([]byte(m.Content)) {
			// Historical tool content may be plain text. New executor results
			// are validated JSON before being added to the transcript.
			out[i].Content = Redact(m.Content, opts)
			continue
		}
		value, err := redactRunJSON(json.RawMessage(m.Content), opts)
		if err != nil {
			return nil, err
		}
		out[i].Content = string(value)
	}
	return out, nil
}

// Preserve object keys, array order, nulls, booleans and unmatched number
// literals. A matching numeric value becomes a JSON string mask only in the
// model view; it is never written back or passed to the executor.
func redactRunJSON(raw json.RawMessage, opts RedactOptions) (json.RawMessage, error) {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 {
		return nil, ErrBadInput
	}
	switch raw[0] {
	case '{':
		var object map[string]json.RawMessage
		if json.Unmarshal(raw, &object) != nil {
			return nil, ErrBadInput
		}
		for key, value := range object {
			masked, err := redactRunJSON(value, opts)
			if err != nil {
				return nil, err
			}
			object[key] = masked
		}
		return json.Marshal(object)
	case '[':
		var array []json.RawMessage
		if json.Unmarshal(raw, &array) != nil {
			return nil, ErrBadInput
		}
		for i, value := range array {
			masked, err := redactRunJSON(value, opts)
			if err != nil {
				return nil, err
			}
			array[i] = masked
		}
		return json.Marshal(array)
	case '"':
		var text string
		if json.Unmarshal(raw, &text) != nil {
			return nil, ErrBadInput
		}
		return json.Marshal(Redact(text, opts))
	default:
		if masked := Redact(string(raw), opts); masked != string(raw) {
			return json.Marshal(masked)
		}
		return raw, nil
	}
}

func (c *Client) conversation(ctx context.Context, cfg Config, payload any) (Message, *Result, error) {
	body, err := json.Marshal(payload)
	if err != nil || len(body) > 512<<10 {
		return Message{}, nil, ErrInputTooLong
	}
	ctx, cancel := context.WithTimeout(ctx, cfg.Timeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, cfg.endpoint(), bytes.NewReader(body))
	if err != nil {
		return Message{}, nil, ErrUpstream
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	if cfg.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+cfg.APIKey)
	}
	client := *c.httpClient(cfg)
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	resp, err := client.Do(req)
	if err != nil {
		return Message{}, nil, classify(ctx, err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxRespBytes+1))
	if err != nil {
		return Message{}, nil, classify(ctx, err)
	}
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return Message{}, nil, fmt.Errorf("%w: status %d", ErrUpstream, resp.StatusCode)
	}
	var data struct {
		Choices []struct {
			Message Message `json:"message"`
			Finish  string  `json:"finish_reason"`
		} `json:"choices"`
		Usage struct {
			Prompt     int `json:"prompt_tokens"`
			Completion int `json:"completion_tokens"`
			Total      int `json:"total_tokens"`
		} `json:"usage"`
	}
	if len(raw) > maxRespBytes || json.Unmarshal(raw, &data) != nil || len(data.Choices) != 1 ||
		data.Usage.Prompt < 0 || data.Usage.Completion < 0 || data.Usage.Total < 0 {
		return Message{}, nil, ErrBadOutput
	}
	ch := data.Choices[0]
	usage := &Result{Content: ch.Message.Content, FinishReason: ch.Finish,
		PromptTokens: data.Usage.Prompt, CompletionTokens: data.Usage.Completion, TotalTokens: data.Usage.Total}
	if usage.TotalTokens < usage.PromptTokens+usage.CompletionTokens {
		usage.TotalTokens = usage.PromptTokens + usage.CompletionTokens
	}
	if ch.Finish != "stop" && ch.Finish != "tool_calls" {
		return Message{}, usage, ErrBadOutput
	}
	if (ch.Finish == "tool_calls") != (len(ch.Message.ToolCalls) != 0) {
		return Message{}, usage, ErrBadOutput
	}
	if len(ch.Message.ToolCalls) == 0 && strings.TrimSpace(ch.Message.Content) == "" {
		return Message{}, usage, ErrBadOutput
	}
	return ch.Message, usage, nil
}

// Decode recursively with UseNumber so nested object ordering is normalized
// without rounding resource IDs carried as JSON numbers.
func canonicalArguments(raw string) ([]byte, error) {
	var args map[string]any
	d := json.NewDecoder(strings.NewReader(raw))
	d.UseNumber()
	if d.Decode(&args) != nil || args == nil {
		return nil, ErrBadOutput
	}
	var trailing any
	if d.Decode(&trailing) != io.EOF {
		return nil, ErrBadOutput
	}
	return json.Marshal(args)
}
