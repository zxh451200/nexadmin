// Package kb 是知识库插件：上传 txt/md → 切分 → 向量化（core/ai 的 OpenAI 兼容 embeddings）
// → 余弦 topK 检索。检索不到（低于阈值）一律不编造，由调用方走「礼貌说明 + 转人工」兜底。
// 遵守设计文档 §19：只依赖 core/ 与 pkg/，表前缀 plg_kb_，权限码前缀 kb:，接口路径 /api/v1/plugins/kb/...。
package kb

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"math"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"gorm.io/gorm"

	"nexadmin/core/ai"
	"nexadmin/core/response"
	"nexadmin/pkg/idgen"
)

// 上限：单文档 2MB 纯文本（同步向量化在十几秒内能完成）；单块约 500 字。
const (
	maxDocBytes    = 2 << 20
	blockRunes     = 500
	maxAskRunes    = 500
	minBlockRunes  = 10
	embedBatchSize = 64 // core/ai embeddings 的单批上限
)

// logger 是最小日志面（便于测试注入空实现）。
type logger interface {
	Info(msg string, keysAndValues ...any)
	Warn(msg string, keysAndValues ...any)
	Error(msg string, keysAndValues ...any)
}

// Service 是知识库业务层。gw 为 nil 时（AI 未装配）文档上传直接报不可用。
type Service struct {
	db    *gorm.DB
	gw    *ai.Gateway
	log   logger
	now   func() time.Time
	cache *indexCache
}

// NewService 创建知识库服务。
func NewService(db *gorm.DB, gw *ai.Gateway, log logger) *Service {
	if log == nil {
		log = nullLog{}
	}
	return &Service{db: db, gw: gw, log: log, now: func() time.Time { return time.Now().UTC() }, cache: newIndexCache()}
}

// Ready 报告问答链路是否可用（对话模型与 embedding 都配置完整）。
// IM 机器人据此判断：AI 没配好时保持人工现状（静默跳过），不做兜底转人工刷屏。
func (s *Service) Ready(ctx context.Context) bool {
	if s.gw == nil {
		return false
	}
	cfg, err := s.gw.Load(ctx)
	return err == nil && cfg.Ready() && cfg.EmbedReady()
}

type nullLog struct{}

func (nullLog) Info(string, ...any)  {}
func (nullLog) Warn(string, ...any)  {}
func (nullLog) Error(string, ...any) {}

// ---- 文档入库 ----

// IngestInput 是上传入库的入参（文本已从文件读出）。
type IngestInput struct {
	Title  string
	Source string
	Mime   string
	Size   int64
	Text   string
}

// Ingest 切分并向量化一篇文档；成功后文档与切块同一事务落库。
// embedding 模型未配置时报 kb.embed_unavailable（前端给出设置指引），不留半成品。
func (s *Service) Ingest(ctx context.Context, in IngestInput) (*Document, error) {
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
	text := strings.TrimSpace(in.Text)
	if text == "" {
		return nil, response.BadRequest("kb.document_empty")
	}
	blocks := splitBlocks(text)
	if len(blocks) == 0 {
		return nil, response.BadRequest("kb.document_empty")
	}
	vectors, err := s.embedAll(ctx, cfg, blocks)
	if err != nil {
		return nil, err
	}
	id, err := idgen.Next()
	if err != nil {
		return nil, err
	}
	now := s.now()
	doc := Document{
		Title: strings.TrimSpace(in.Title), SourceName: strings.TrimSpace(in.Source),
		Mime: strings.TrimSpace(in.Mime), Size: in.Size,
		Status: StatusReady, ChunkCount: len(blocks), Model: cfg.EmbedModel,
		Dimension: len(vectors[0]), CreatedAt: now, UpdatedAt: now,
	}
	doc.ID = id
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(&doc).Error; err != nil {
			return err
		}
		rows := make([]Chunk, len(blocks))
		for i, b := range blocks {
			rowID, ierr := idgen.Next()
			if ierr != nil {
				return ierr
			}
			raw, merr := json.Marshal(vectors[i])
			if merr != nil {
				return merr
			}
			rows[i] = Chunk{DocumentID: id, Seq: i + 1, Content: b, Vector: string(raw)}
			rows[i].ID = rowID
			rows[i].CreatedAt, rows[i].UpdatedAt = now, now
		}
		return tx.CreateInBatches(rows, 100).Error
	})
	if err != nil {
		return nil, err
	}
	s.cache.invalidate()
	return &doc, nil
}

// embedAll 按单批 ≤64 条调 embeddings；失败包装成用户可读的错误 key（模型配置错 / 调用失败）。
func (s *Service) embedAll(ctx context.Context, cfg ai.Config, inputs []string) ([][]float64, error) {
	out := make([][]float64, 0, len(inputs))
	for start := 0; start < len(inputs); start += embedBatchSize {
		end := min(start+embedBatchSize, len(inputs))
		batch, err := s.gw.Client.Embeddings(ctx, cfg, inputs[start:end])
		if err != nil {
			if s.log != nil {
				s.log.Warn("kb: embeddings call failed", "err", err.Error())
			}
			switch {
			case errors.Is(err, ai.ErrDisabled):
				return nil, response.Unavailable("kb.embed_unavailable")
			default:
				return nil, response.New(502, "kb.embed_failed")
			}
		}
		out = append(out, batch...)
	}
	return out, nil
}

// splitBlocks 把整篇文本切成检索块：空行分段，每段独立成块（FAQ / 主题段落天然自包含，
// 混进同一块会互相稀释相似度）；超长段落硬切，过短碎块并入上一块。
func splitBlocks(text string) []string {
	text = strings.ReplaceAll(text, "\r\n", "\n")
	var blocks []string
	for _, p := range strings.Split(text, "\n\n") {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		// 超长段落硬切，尽量在句读处断开，避免把一句话截成两半。
		for utf8.RuneCountInString(p) > blockRunes {
			runes := []rune(p)
			cut := blockRunes
			for i := blockRunes - 1; i > blockRunes/2; i-- {
				if strings.ContainsRune("。！？；\n.!?;", runes[i]) {
					cut = i + 1
					break
				}
			}
			blocks = append(blocks, strings.TrimSpace(string(runes[:cut])))
			p = strings.TrimSpace(string(runes[cut:]))
		}
		if p == "" {
			continue
		}
		if utf8.RuneCountInString(p) < minBlockRunes && len(blocks) > 0 {
			blocks[len(blocks)-1] += "\n" + p
			continue
		}
		blocks = append(blocks, p)
	}
	return blocks
}

// ---- 检索 ----

// Hit 是一条检索命中。
type Hit struct {
	DocumentID string  `json:"documentId"`
	Title      string  `json:"title"`
	Excerpt    string  `json:"excerpt"`
	Score      float64 `json:"score"`
}

// indexedChunk 是内存索引的一行。
type indexedChunk struct {
	docID   string
	title   string
	content string
	vector  []float64
}

// indexCache 是带指纹失效的进程内向量索引：指纹 = 当前 embedding 模型 + 可检索切块的
// 数量与最新更新时间。文档增删/重嵌都会改动指纹，下一次检索自动重建。
type indexCache struct {
	mu      sync.RWMutex
	finger  string
	chunks  []indexedChunk
	loading bool // 简单防抖：重建期间其它请求直接走旧索引（可能短暂偏旧，可接受）
}

func newIndexCache() *indexCache { return &indexCache{} }

func (c *indexCache) invalidate() {
	c.mu.Lock()
	c.finger = ""
	c.mu.Unlock()
}

// Retrieve 以问题向量做余弦 topK；低于阈值的命中一律丢弃——丢弃后为空就由调用方兜底转人工，
// 这是「不瞎回答」的第一道闸。
func (s *Service) Retrieve(ctx context.Context, query string) ([]Hit, error) {
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
	st := s.Settings(ctx)
	vecs, err := s.gw.Client.Embeddings(ctx, cfg, []string{strings.TrimSpace(query)})
	if err != nil {
		if s.log != nil {
			s.log.Warn("kb: query embedding failed", "err", err.Error())
		}
		switch {
		case errors.Is(err, ai.ErrDisabled):
			return nil, response.Unavailable("kb.embed_unavailable")
		default:
			return nil, response.New(502, "kb.embed_failed")
		}
	}
	chunks, err := s.chunks(ctx, cfg.EmbedModel)
	if err != nil {
		return nil, err
	}
	q := vecs[0]
	type scored struct {
		chunk *indexedChunk
		score float64
	}
	top := make([]scored, 0, st.TopK)
	worst := func() float64 {
		if len(top) < st.TopK {
			return math.SmallestNonzeroFloat64
		}
		return top[len(top)-1].score
	}
	for i := range chunks {
		sc := cosine(q, chunks[i].vector)
		if sc < st.Threshold || sc <= worst() {
			continue
		}
		top = append(top, scored{&chunks[i], sc})
		for j := len(top) - 1; j > 0 && top[j].score > top[j-1].score; j-- {
			top[j], top[j-1] = top[j-1], top[j]
		}
		if len(top) > st.TopK {
			top = top[:st.TopK]
		}
	}
	hits := make([]Hit, 0, len(top))
	for _, t := range top {
		hits = append(hits, Hit{
			DocumentID: t.chunk.docID, Title: t.chunk.title,
			Excerpt: truncateRunes(t.chunk.content, 160), Score: roundScore(t.score),
		})
	}
	return hits, nil
}

// chunks 返回可检索切块（模型一致 + 文档 ready），带指纹缓存的懒加载。
func (s *Service) chunks(ctx context.Context, model string) ([]indexedChunk, error) {
	var count int64
	if err := s.db.WithContext(ctx).Model(&Chunk{}).Count(&count).Error; err != nil {
		return nil, err
	}
	var last sql.NullTime
	if err := s.db.WithContext(ctx).Model(&Chunk{}).
		Select("MAX(updated_at)").Row().Scan(&last); err != nil {
		return nil, err
	}
	lastAt := int64(0)
	if last.Valid {
		lastAt = last.Time.UnixMilli()
	}
	finger := model + ":" + strconv.FormatInt(count, 10) + ":" + strconv.FormatInt(lastAt, 10)
	s.cache.mu.RLock()
	ok := s.cache.finger == finger && len(s.cache.chunks) > 0
	var chunks []indexedChunk
	if ok {
		chunks = s.cache.chunks
	}
	s.cache.mu.RUnlock()
	if ok {
		return chunks, nil
	}
	s.cache.mu.Lock()
	defer s.cache.mu.Unlock()
	if s.cache.finger == finger && len(s.cache.chunks) > 0 { // 双检：并发重建只做一次
		return s.cache.chunks, nil
	}
	var rows []struct {
		DocID      string `gorm:"column:doc_id"`
		Title      string `gorm:"column:title"`
		Content    string `gorm:"column:content"`
		VectorJSON string `gorm:"column:vector"`
	}
	err := s.db.WithContext(ctx).Table("plg_kb_chunk AS c").
		Select("c.document_id AS doc_id, d.title AS title, c.content AS content, c.vector AS vector").
		Joins("JOIN plg_kb_document d ON d.id = c.document_id AND d.status = ? AND d.model = ?", StatusReady, model).
		Order("c.document_id, c.seq").Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	chunks = make([]indexedChunk, 0, len(rows))
	for _, r := range rows {
		var vec []float64
		if json.Unmarshal([]byte(r.VectorJSON), &vec) != nil || len(vec) == 0 {
			continue // 坏行跳过，不让一条脏数据废掉整个索引
		}
		chunks = append(chunks, indexedChunk{docID: r.DocID, title: r.Title, content: r.Content, vector: vec})
	}
	s.cache.finger, s.cache.chunks = finger, chunks
	if s.log != nil {
		s.log.Info("kb: index rebuilt", "chunks", len(chunks), "model", model)
	}
	return chunks, nil
}

func cosine(a, b []float64) float64 {
	if len(a) != len(b) || len(a) == 0 {
		return 0
	}
	var dot, na, nb float64
	for i := range a {
		dot += a[i] * b[i]
		na += a[i] * a[i]
		nb += b[i] * b[i]
	}
	if na == 0 || nb == 0 {
		return 0
	}
	return dot / (math.Sqrt(na) * math.Sqrt(nb))
}

func roundScore(v float64) float64 { return math.Round(v*1000) / 1000 }

// ---- 管理 ----

// DocumentQuery 目前只有全量列表（知识库文档量级小），status 预留。
type DocumentQuery struct {
	Status string
}

// ListDocuments 列出文档（新在前）。
func (s *Service) ListDocuments(ctx context.Context, q DocumentQuery) ([]Document, error) {
	db := s.db.WithContext(ctx).Model(&Document{})
	if q.Status != "" {
		db = db.Where("status = ?", q.Status)
	}
	var rows []Document
	if err := db.Order("id DESC").Limit(500).Find(&rows).Error; err != nil {
		return nil, err
	}
	return rows, nil
}

// DeleteDocument 删除文档并级联删切块。
func (s *Service) DeleteDocument(ctx context.Context, id string) error {
	docID, err := idgen.Parse(id)
	if err != nil {
		return response.BadRequest("error.bad_request")
	}
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("document_id = ?", docID).Delete(&Chunk{}).Error; err != nil {
			return err
		}
		return tx.Delete(&Document{}, int64(docID)).Error
	})
	if err == nil {
		s.cache.invalidate()
	}
	return err
}

// Reembed 用当前配置的 embedding 模型重跑一篇文档（模型切换后的旧文档补救）。
func (s *Service) Reembed(ctx context.Context, id string) (*Document, error) {
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
	docID, err := idgen.Parse(id)
	if err != nil {
		return nil, response.BadRequest("error.bad_request")
	}
	var doc Document
	if err := s.db.WithContext(ctx).Take(&doc, int64(docID)).Error; err != nil {
		return nil, response.NotFound("kb.document_not_found")
	}
	var chunks []Chunk
	if err := s.db.WithContext(ctx).Where("document_id = ?", docID).Order("seq").Find(&chunks).Error; err != nil {
		return nil, err
	}
	if len(chunks) == 0 {
		return nil, response.BadRequest("kb.document_empty")
	}
	blocks := make([]string, len(chunks))
	for i, c := range chunks {
		blocks[i] = c.Content
	}
	vectors, err := s.embedAll(ctx, cfg, blocks)
	if err != nil {
		return nil, err
	}
	now := s.now()
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		for i := range chunks {
			raw, merr := json.Marshal(vectors[i])
			if merr != nil {
				return merr
			}
			if err := tx.Model(&Chunk{}).Where("id = ?", int64(chunks[i].ID)).
				Updates(map[string]any{"vector": string(raw), "updated_at": now}).Error; err != nil {
				return err
			}
		}
		return tx.Model(&Document{}).Where("id = ?", int64(docID)).Updates(map[string]any{
			"status": StatusReady, "model": cfg.EmbedModel, "dimension": len(vectors[0]),
			"error": "", "updated_at": now,
		}).Error
	})
	if err != nil {
		return nil, err
	}
	s.cache.invalidate()
	return s.reload(ctx, docID)
}

func (s *Service) reload(ctx context.Context, id idgen.ID) (*Document, error) {
	var doc Document
	if err := s.db.WithContext(ctx).Take(&doc, int64(id)).Error; err != nil {
		return nil, err
	}
	return &doc, nil
}

func truncateRunes(s string, n int) string {
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	return string([]rune(s)[:n]) + "…"
}
