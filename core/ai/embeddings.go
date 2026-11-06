package ai

// Embeddings：OpenAI 兼容的向量化接口（POST {base}/embeddings）。
// 与对话共用 BaseURL / APIKey / SSRF 防护，模型名取 cfg.EmbedModel（设置页 AI 分区的
// 「Embedding 模型」字段，空 = 未配置向量化）。知识库 / 语义检索等特性从这里取向量。

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

type embeddingsRequest struct {
	Model string   `json:"model"`
	Input []string `json:"input"`
}

type embeddingsResponse struct {
	Data []struct {
		Embedding []float64 `json:"embedding"`
		Index     int       `json:"index"`
	} `json:"data"`
	Usage struct {
		PromptTokens int `json:"prompt_tokens"`
		TotalTokens  int `json:"total_tokens"`
	} `json:"usage"`
}

// EmbedReadyFor 报告该配置能否做向量化（未配置 EmbedModel 时调用方应直接跳过，不要报错——
// 向量化是可选能力，不是每个部署都配了向量模型）。
func EmbedReadyFor(cfg Config) bool {
	return cfg.Normalize().EmbedReady()
}

// Embeddings 批量向量化：返回与 inputs 等长且按 index 对齐的向量。
// 输入条数与单条长度受 cfg.MaxTokens 约束之外，这里再设单批上限 64 条（超过由调用方分批）。
func (c *Client) Embeddings(ctx context.Context, cfg Config, inputs []string) ([][]float64, error) {
	cfg = cfg.Normalize()
	if len(inputs) == 0 {
		return nil, fmt.Errorf("%w: empty input", ErrBadInput)
	}
	if len(inputs) > 64 {
		return nil, fmt.Errorf("%w: too many inputs (max 64 per batch)", ErrBadInput)
	}
	if !cfg.EmbedReady() {
		return nil, ErrDisabled
	}
	body, err := json.Marshal(embeddingsRequest{Model: cfg.EmbedModel, Input: inputs})
	if err != nil {
		return nil, fmt.Errorf("%w: encode", ErrUpstream)
	}
	ctx, cancel := context.WithTimeout(ctx, cfg.Timeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, cfg.endpoint("embeddings"), bytes.NewReader(body))
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
		return nil, fmt.Errorf("%w: status %d", ErrUpstream, resp.StatusCode)
	}
	var out embeddingsResponse
	if json.Unmarshal(raw, &out) != nil || len(out.Data) != len(inputs) {
		return nil, fmt.Errorf("%w: malformed response", ErrUpstream)
	}
	// 按 index 对齐：上游不保证 data 顺序与输入一致
	vectors := make([][]float64, len(inputs))
	for _, d := range out.Data {
		if d.Index < 0 || d.Index >= len(vectors) {
			return nil, fmt.Errorf("%w: index out of range", ErrUpstream)
		}
		vectors[d.Index] = d.Embedding
	}
	for i, v := range vectors {
		if len(v) == 0 {
			return nil, fmt.Errorf("%w: empty vector at %d", ErrBadOutput, i)
		}
	}
	return vectors, nil
}
