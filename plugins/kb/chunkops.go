package kb

// 切块运营：单块启停与编辑。改坏的块可以停用或单独改文案并重算该块向量，
// 不必删掉整篇重传；文档正文指纹用于同内容重传时覆盖，避免重复 dilution topK。

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strings"
	"unicode/utf8"

	"nexadmin/core/response"
	"nexadmin/pkg/idgen"
)

// fingerprint 归一化空白后取 sha256：同一份内容换个空白排版也算同一篇。
func fingerprint(text string) string {
	sum := sha256.Sum256([]byte(strings.Join(strings.Fields(text), " ")))
	return hex.EncodeToString(sum[:])
}

// SetChunkEnabled 启停单块。禁用的块仍留在库里，随时能恢复；索引指纹会因 updated_at 变化而重建。
func (s *Service) SetChunkEnabled(ctx context.Context, docIDStr string, seq int, enabled bool) (*Chunk, error) {
	docID, err := idgen.Parse(docIDStr)
	if err != nil || seq < 1 {
		return nil, response.BadRequest("error.bad_request")
	}
	var chunk Chunk
	if err := s.db.WithContext(ctx).Where("document_id = ? AND seq = ?", docID, seq).Take(&chunk).Error; err != nil {
		return nil, response.NotFound("kb.chunk_not_found")
	}
	if err := s.db.WithContext(ctx).Model(&chunk).Updates(map[string]any{
		"enabled": enabled, "updated_at": s.now(),
	}).Error; err != nil {
		return nil, err
	}
	chunk.Enabled = enabled
	s.cache.invalidate()
	return &chunk, nil
}

// UpdateChunk 编辑单块内容并重算该块向量：其它块与标题都不动。
// 文档用的 embedding 模型必须与当前配置一致，否则单块新向量会和其余块的量纲对不上。
func (s *Service) UpdateChunk(ctx context.Context, docIDStr string, seq int, content string) (*Chunk, error) {
	content = strings.TrimSpace(content)
	if content == "" {
		return nil, response.BadRequest("kb.document_empty")
	}
	if utf8.RuneCountInString(content) > 4000 {
		return nil, response.BadRequest("kb.chunk_too_long")
	}
	docID, err := idgen.Parse(docIDStr)
	if err != nil || seq < 1 {
		return nil, response.BadRequest("error.bad_request")
	}
	var doc Document
	if err := s.db.WithContext(ctx).Take(&doc, int64(docID)).Error; err != nil {
		return nil, response.NotFound("kb.document_not_found")
	}
	if busy := requireSettledDoc(&doc); busy != nil {
		return nil, busy
	}
	if s.gw == nil {
		return nil, response.Unavailable("kb.plugin_inactive")
	}
	cfg, err := s.gw.Load(ctx)
	if err != nil {
		return nil, err
	}
	if !cfg.EmbedReady() {
		return nil, response.Unavailable("kb.embed_unavailable")
	}
	cfg = cfg.Normalize()
	if doc.Model != cfg.EmbedModel {
		return nil, response.Conflict("kb.chunk_model_stale")
	}
	var chunk Chunk
	if err := s.db.WithContext(ctx).Where("document_id = ? AND seq = ?", docID, seq).Take(&chunk).Error; err != nil {
		return nil, response.NotFound("kb.chunk_not_found")
	}
	vectors, err := s.embedAll(ctx, cfg, []string{content})
	if err != nil {
		return nil, err
	}
	raw, err := json.Marshal(vectors[0])
	if err != nil {
		return nil, err
	}
	if err := s.db.WithContext(ctx).Model(&chunk).Updates(map[string]any{
		"content": content, "vector": string(raw), "updated_at": s.now(),
	}).Error; err != nil {
		return nil, err
	}
	chunk.Content, chunk.Vector = content, string(raw)
	s.cache.invalidate()
	return &chunk, nil
}
