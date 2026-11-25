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
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"
	"unicode/utf8"

	"gorm.io/gorm"

	"nexadmin/core"
	"nexadmin/core/ai"
	"nexadmin/core/response"
	"nexadmin/pkg/idgen"
)

// 上限：单文档 2MB 纯文本（同步向量化在十几秒内能完成）；单块长度与重叠走检索设置
// （默认 500 / 50 字，见 settings.go 的 defaultChunkSize）。
const (
	maxDocBytes    = 2 << 20
	maxAskRunes    = 500
	minBlockRunes  = 10
	embedBatchSize = 64

	// rrfK 是倒数排名融合的平滑常数（文献常用 60）：两路各排一次名再相加，
	// 避免直接比较「余弦 0.42」与「BM25 8.7」这两种没有共同量纲的分数。
	rrfK = 60
	// keywordCoverGate 是关键词兜底闸：查询词被这一块覆盖到这个比例，才允许在
	// 向量分略低于阈值时仍算命中（型号 / 订单号这类「字面全中、向量不像」的问法）。
	keywordCoverGate = 0.6
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
	queue *ingestQueue // 异步入库队列；handler 在插件未 Start 时也能安全 notify
}

// NewService 创建知识库服务。
func NewService(db *gorm.DB, gw *ai.Gateway, log logger) *Service {
	if log == nil {
		log = nullLog{}
	}
	s := &Service{db: db, gw: gw, log: log, now: func() time.Time { return time.Now().UTC() }, cache: newIndexCache()}
	s.queue = newIngestQueue(s)
	return s
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
	st := s.Settings(ctx)
	blocks := splitBlocks(text, st.ChunkSize, st.ChunkOverlap)
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
		SourceText: sql.NullString{String: text, Valid: true},
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
	return s.embedAllWithProgress(ctx, cfg, inputs, nil)
}

// embedAllWithProgress 与 embedAll 同一条调用链，只是每批完成后回调一次——
// 异步入库用它把「向量化 12/40 块」写进文档 stage，管理端不用对着转圈干等。
func (s *Service) embedAllWithProgress(ctx context.Context, cfg ai.Config, inputs []string, onBatch func(done int)) ([][]float64, error) {
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
		if onBatch != nil {
			onBatch(end)
		}
	}
	return out, nil
}

// splitBlocks 把整篇文本切成检索块：空行分段，每段独立成块（FAQ / 主题段落天然自包含，
// 混进同一块会互相稀释相似度）；超过 size 的段落硬切，size 以内过长会按句读断开，
// 相邻块再回退 overlap 个字符做重叠——被切断的那句话在前后两块里都能读到。
// 过短的碎块并入上一块。
func splitBlocks(text string, size, overlap int) []string {
	if size < minChunkSize || size > maxChunkSize {
		size = defaultChunkSize
	}
	if overlap < 0 || overlap > size/2 {
		overlap = min(max(overlap, 0), size/2)
	}
	text = strings.ReplaceAll(text, "\r\n", "\n")
	var blocks []string
	for _, p := range strings.Split(text, "\n\n") {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		// 超长段落硬切，尽量在句读处断开，避免把一句话截成两半。
		for utf8.RuneCountInString(p) > size {
			runes := []rune(p)
			cut := size
			for i := size - 1; i > size/2; i-- {
				if strings.ContainsRune("。！？；\n.!?;", runes[i]) {
					cut = i + 1
					break
				}
			}
			blocks = append(blocks, strings.TrimSpace(string(runes[:cut])))
			// 下一块从 cut-overlap 起（至少前进一个字，保证循环收敛）。
			back := min(overlap, cut-1)
			p = strings.TrimSpace(string(runes[cut-back:]))
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

// joinChunks 把切块按顺序拼回全文，供没有原文的老文档重新切分用：
// 只按 overlap 精确剥掉块首与上一块结尾重复的那段——不多剥一个字。
func joinChunks(blocks []string, overlap int) string {
	var b strings.Builder
	prev := ""
	for i, c := range blocks {
		if i > 0 {
			c = stripOverlap(prev, c, overlap)
			b.WriteString("\n\n")
		}
		b.WriteString(c)
		prev = c
	}
	return strings.TrimSpace(b.String())
}

// stripOverlap 在 cur 开头确实是 prev 结尾（恰好 overlap 个字）时剥掉这段，否则原样返回。
// 不做「最长匹配」：周期性文本下最长匹配会把正文当成重叠剥掉（实测 80 句被剥成 56 句）。
func stripOverlap(prev, cur string, overlap int) string {
	if overlap <= 0 {
		return cur
	}
	pr, cr := []rune(prev), []rune(cur)
	if overlap > len(pr) || overlap > len(cr) {
		return cur
	}
	if string(pr[len(pr)-overlap:]) == string(cr[:overlap]) {
		return strings.TrimSpace(string(cr[overlap:]))
	}
	return cur
}

// ---- 检索 ----

// Hit 是一条检索命中。
type Hit struct {
	DocumentID string  `json:"documentId"`
	Title      string  `json:"title"`
	Excerpt    string  `json:"excerpt"`
	// Score 是向量余弦相似度（阈值调参看的就是它）。
	Score float64 `json:"score"`
	// Match 说明这一条凭什么命中：keyword = 字面关键词兜底（向量分低于阈值，但查询词
	// 几乎全中——型号 / 订单号这类「字面精确、向量不像」的问法）；空 = 向量过阈值。
	Match string `json:"match,omitempty"`
}

// indexedChunk 是内存索引的一行：向量 + 关键词统计（BM25 用）。
type indexedChunk struct {
	docID   string
	title   string
	content string
	vector  []float64
	terms   map[string]int // 词 → 词频（中文按二元组切，拉丁字母/数字按词）
	length  int            // 词总数，BM25 的长度归一化用
}

// indexSnapshot 是一次索引快照：切块 + 全库词频统计（df 与平均块长）。
type indexSnapshot struct {
	chunks []indexedChunk
	df     map[string]int
	avgLen float64
}

// indexCache 是带指纹失效的进程内索引：指纹 = 当前 embedding 模型 + 可检索切块的
// 数量与最新更新时间。文档增删/重嵌都会改动指纹，下一次检索自动重建。
type indexCache struct {
	mu      sync.RWMutex
	finger  string
	snap    *indexSnapshot
	loading bool // 简单防抖：重建期间其它请求直接走旧索引（可能短暂偏旧，可接受）
}

func newIndexCache() *indexCache { return &indexCache{} }

func (c *indexCache) invalidate() {
	c.mu.Lock()
	c.finger = ""
	c.mu.Unlock()
}

// queryVector 把问题文本向量化；失败统一映射成用户可读的错误 key（模型没配好 / 调用失败）。
func (s *Service) queryVector(ctx context.Context, cfg ai.Config, query string) ([]float64, error) {
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
	return vecs[0], nil
}

// Retrieve 做召回：向量余弦 + 关键词 BM25 两路融合，低于阈值的命中一律丢弃——
// 丢弃后为空就由调用方兜底转人工，这是「不瞎回答」的第一道闸。
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
	q, err := s.queryVector(ctx, cfg, query)
	if err != nil {
		return nil, err
	}
	snap, err := s.index(ctx, cfg.EmbedModel)
	if err != nil {
		return nil, err
	}
	return selectTop(q, snap, query, st.Threshold, st.TopK, st.Mode == ModeHybrid), nil
}

// candidate 是融合排序前的一条候选。
type candidate struct {
	chunk *indexedChunk
	cos   float64
	kw    float64
	cover float64 // 查询词被这一块覆盖的比例
	rankV int
	rankK int
}

// selectTop 是检索的公共选择器：闸门过滤 → 排序 → 截断 topK。
// Retrieve 用它做过阈值检索；召回调试（Candidates）传 threshold=0 看最接近的候选。
// hybrid=true 时按 RRF（倒数排名融合）排序：向量与 BM25 各排一次名再相加，避免比较
// 两种没有共同量纲的分数；同时给「字面几乎全中、向量略低于阈值」的块一条兜底通路。
func selectTop(q []float64, snap *indexSnapshot, query string, threshold float64, topK int, hybrid bool) []Hit {
	qTerms := tokenize(query)
	hybrid = hybrid && len(qTerms) > 0
	gated := make([]*candidate, 0, min(len(snap.chunks), 512))
	for i := range snap.chunks {
		c := &snap.chunks[i]
		cos := cosine(q, c.vector)
		var kw, cover float64
		if hybrid {
			kw, cover = bm25(qTerms, c, snap)
		}
		// 闸门：向量过阈值正常放行；关键词兜底只放松到阈值的六成，且要求查询词覆盖
		// 到 keywordCoverGate——「放松」不等于「放水」，凭常识作答依然不可能。
		if cos < threshold && !(hybrid && cover >= keywordCoverGate && cos >= threshold*0.6) {
			continue
		}
		gated = append(gated, &candidate{chunk: c, cos: cos, kw: kw, cover: cover})
	}
	if len(gated) == 0 {
		return []Hit{}
	}
	if hybrid {
		byVec := make([]*candidate, len(gated))
		copy(byVec, gated)
		sort.SliceStable(byVec, func(i, j int) bool { return byVec[i].cos > byVec[j].cos })
		for i, c := range byVec {
			c.rankV = i
		}
		byKw := make([]*candidate, len(gated))
		copy(byKw, gated)
		sort.SliceStable(byKw, func(i, j int) bool { return byKw[i].kw > byKw[j].kw })
		for i, c := range byKw {
			c.rankK = i
		}
		sort.SliceStable(gated, func(i, j int) bool {
			return 1/float64(rrfK+gated[i].rankV)+1/float64(rrfK+gated[i].rankK) >
				1/float64(rrfK+gated[j].rankV)+1/float64(rrfK+gated[j].rankK)
		})
	} else {
		sort.SliceStable(gated, func(i, j int) bool { return gated[i].cos > gated[j].cos })
	}
	if len(gated) > topK {
		gated = gated[:topK]
	}
	hits := make([]Hit, 0, len(gated))
	for _, c := range gated {
		match := ""
		if c.cos < threshold && c.cover >= keywordCoverGate {
			match = "keyword"
		}
		hits = append(hits, Hit{
			DocumentID: c.chunk.docID, Title: c.chunk.title,
			Excerpt: truncateRunes(c.chunk.content, 160), Score: roundScore(c.cos), Match: match,
		})
	}
	return hits
}

// bm25 计算一块对查询的 BM25 分与查询词覆盖率。df 来自全库快照，长度做归一化。
func bm25(qTerms map[string]int, c *indexedChunk, snap *indexSnapshot) (score, cover float64) {
	const k1, b = 1.2, 0.75
	n := float64(len(snap.chunks))
	avg := math.Max(snap.avgLen, 1)
	matched := 0
	for t, qtf := range qTerms {
		tf := c.terms[t]
		if tf == 0 {
			continue
		}
		matched++
		dfi := math.Max(float64(snap.df[t]), 1)
		idf := math.Log(1 + (n-dfi+0.5)/(dfi+0.5))
		norm := k1 * (1 - b + b*float64(c.length)/avg)
		score += idf * (float64(tf) * (k1 + 1)) / (float64(tf) + norm) * float64(qtf)
	}
	return score, float64(matched) / float64(len(qTerms))
}

// tokenize 把文本切成检索词：中文按相邻二元组（无需词典，对中文短问句足够），
// 拉丁字母与数字按整段保留（型号 / 订单号 / 错误码靠它精确命中）。
func tokenize(s string) map[string]int {
	out := make(map[string]int, 32)
	var word, cjk []rune
	flushWord := func() {
		if len(word) > 0 {
			out[string(word)]++
			word = word[:0]
		}
	}
	flushCJK := func() {
		switch len(cjk) {
		case 0:
		case 1:
			out[string(cjk)]++
		default:
			for i := 0; i+1 < len(cjk); i++ {
				out[string(cjk[i:i+2])]++
			}
		}
		cjk = cjk[:0]
	}
	for _, r := range strings.ToLower(s) {
		switch {
		case isCJK(r):
			flushWord()
			cjk = append(cjk, r)
		case unicode.IsLetter(r) || unicode.IsDigit(r):
			flushCJK()
			word = append(word, r)
		default:
			flushWord()
			flushCJK()
		}
	}
	flushWord()
	flushCJK()
	return out
}

// isCJK 覆盖中日韩表意文字与假名/谚文（二元组切分对它们同样成立）。
func isCJK(r rune) bool {
	switch {
	case r >= 0x3400 && r <= 0x4DBF, r >= 0x4E00 && r <= 0x9FFF, r >= 0xF900 && r <= 0xFAFF:
		return true
	case r >= 0x3040 && r <= 0x30FF, r >= 0xAC00 && r <= 0xD7AF:
		return true
	}
	return false
}

// Candidates 返回不设阈值的 topK 命中——召回调试用：阈值调高了检索不到时，
// 看看「最接近的候选差多少分」再决定调多少，不用盲调。只给管理端测试提问；
// IM 机器人不调它（handoff 兜底不需要候选，省一次 query embedding）。
func (s *Service) Candidates(ctx context.Context, query string) ([]Hit, error) {
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
	q, err := s.queryVector(ctx, cfg, query)
	if err != nil {
		return nil, err
	}
	snap, err := s.index(ctx, cfg.EmbedModel)
	if err != nil {
		return nil, err
	}
	return selectTop(q, snap, query, 0, st.TopK, st.Mode == ModeHybrid), nil
}

// index 返回可检索索引（模型一致 + 文档 ready），带指纹缓存的懒加载。
func (s *Service) index(ctx context.Context, model string) (*indexSnapshot, error) {
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
	ok := s.cache.finger == finger && s.cache.snap != nil && len(s.cache.snap.chunks) > 0
	loading := s.cache.loading
	var snap *indexSnapshot
	if ok {
		snap = s.cache.snap
	}
	s.cache.mu.RUnlock()
	if ok {
		return snap, nil
	}
	if loading && s.cache.snap != nil && len(s.cache.snap.chunks) > 0 {
		// 重建单飞：别的 goroutine 正在查库建索引，先拿旧索引顶上（文档刚变过可能
		// 短暂偏旧），新指纹由下一次请求再换——而不是排着队等全表 Join 跑完。
		s.cache.mu.RLock()
		stale := s.cache.snap
		s.cache.mu.RUnlock()
		return stale, nil
	}
	s.cache.mu.Lock()
	if s.cache.finger == finger && s.cache.snap != nil && len(s.cache.snap.chunks) > 0 { // 双检：并发重建只做一次
		snap = s.cache.snap
		s.cache.mu.Unlock()
		return snap, nil
	}
	s.cache.loading = true
	s.cache.mu.Unlock()

	// 重建在锁外做：全表 Join + 向量 JSON 解析 + 分词是大活，持锁会让期间所有检索
	// （含机器人应答）排队。
	var rows []struct {
		DocID      string `gorm:"column:doc_id"`
		Title      string `gorm:"column:title"`
		Content    string `gorm:"column:content"`
		VectorJSON string `gorm:"column:vector"`
	}
	err := s.db.WithContext(ctx).Table("plg_kb_chunk AS c").
		Select("c.document_id AS doc_id, d.title AS title, c.content AS content, c.vector AS vector").
		Joins("JOIN plg_kb_document d ON d.id = c.document_id AND d.status = ? AND d.model = ?", StatusReady, model).
		Where("c.enabled = ?", true).
		Order("c.document_id, c.seq").Scan(&rows).Error
	built := &indexSnapshot{chunks: make([]indexedChunk, 0, len(rows)), df: make(map[string]int, 1024)}
	terms, totalLen := 0, 0
	if err == nil {
		for _, r := range rows {
			var vec []float64
			if json.Unmarshal([]byte(r.VectorJSON), &vec) != nil || len(vec) == 0 {
				continue // 坏行跳过，不让一条脏数据废掉整个索引
			}
			tk := tokenize(r.Content)
			length := 0
			for t, n := range tk {
				length += n
				built.df[t]++
			}
			terms++
			totalLen += length
			built.chunks = append(built.chunks, indexedChunk{
				docID: r.DocID, title: r.Title, content: r.Content, vector: vec, terms: tk, length: length,
			})
		}
	}
	if terms > 0 {
		built.avgLen = float64(totalLen) / float64(terms)
	}

	s.cache.mu.Lock()
	s.cache.loading = false
	if err != nil {
		s.cache.mu.Unlock()
		return nil, err
	}
	if s.cache.finger == finger && s.cache.snap != nil && len(s.cache.snap.chunks) > 0 {
		snap = s.cache.snap // 并发竞争：别的 goroutine 已换上同指纹新结果
	} else {
		s.cache.finger, s.cache.snap = finger, built
		snap = built
	}
	s.cache.mu.Unlock()
	if s.log != nil {
		s.log.Info("kb: index rebuilt", "chunks", len(built.chunks), "terms", len(built.df), "model", model)
	}
	return snap, nil
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

// DocumentQuery 是文档列表的查询：状态筛选 + 表头排序 + 分页。
// StaleModel 非空时按「需要重新向量化」筛选（status=failed，或向量模型与当前配置不一致）——
// 这个派生态原先由前端在整表上过滤，分页后必须落到 SQL，否则只在当前页里筛。
type DocumentQuery struct {
	Status     string
	// Queued 筛选排队+处理中的文档（异步入库中间态，不是一个真实 status 值）。
	Queued     bool
	StaleModel string
	Sort       string
	Desc       bool
	Page       int
	PageSize   int
}

// Stats 是后台统计条的数据。按全库聚合，不受列表分页影响。
type Stats struct {
	Docs   int64 `json:"docs"`
	Chunks int64 `json:"chunks"`
	Ready  int64 `json:"ready"`
	Failed int64 `json:"failed"`
	// Queued 是排队 + 处理中的文档数：异步入库后这几个状态不该算作失败。
	Queued int64 `json:"queued"`
	Size   int64 `json:"size"`
	// PendingMiss 是未命中问题池里待补的条数——「知识库还缺什么」的直接读数。
	PendingMiss int64 `json:"pendingMiss"`
}

// ListDocuments 分页列出文档（默认新在前）。返回当页数据与总条数。
func (s *Service) ListDocuments(ctx context.Context, q DocumentQuery) ([]Document, int64, error) {
	// scope 每次重建条件：Count 与 Find 各用一份，避免 Count 改写语句影响后续查询。
	scope := func() *gorm.DB {
		db := s.db.WithContext(ctx).Model(&Document{})
		switch {
		case q.Queued:
			return db.Where("status IN ?", []string{StatusPending, StatusProcessing})
		case q.StaleModel != "":
			// 排队/处理中的文档正在走正常流程，不算「需重新向量化」，否则批量重嵌会和 worker 撞车。
			return db.Where("status = ? OR (status = ? AND model <> '' AND model <> ?)",
				StatusFailed, StatusReady, q.StaleModel)
		case q.Status != "":
			return db.Where("status = ?", q.Status)
		}
		return db
	}
	var total int64
	if err := scope().Count(&total).Error; err != nil {
		return nil, 0, err
	}
	page, pageSize := q.Page, q.PageSize
	if page < 1 {
		page = 1
	}
	if pageSize < 1 {
		pageSize = 20
	}
	var rows []Document
	// 排序白名单：标题/状态/大小/时间，白名单外回退 created_at。
	// 补 id 作次序键：同一毫秒上传的多篇文档在分页间不会重复或漏行。
	if err := scope().Order(core.OrderBy(q.Sort, q.Desc, map[string]string{
		"title":      "title",
		"status":     "status",
		"size":       "size",
		"created_at": "created_at",
	}, "created_at")).
		Order("id DESC").
		Offset((page - 1) * pageSize).Limit(pageSize).
		Find(&rows).Error; err != nil {
		return nil, 0, err
	}
	return rows, total, nil
}

// Stats 汇总文档数 / 知识块数 / 未就绪数 / 占用体积（全库聚合，供页头统计条）。
func (s *Service) Stats(ctx context.Context) (*Stats, error) {
	var row struct {
		Docs   int64 `gorm:"column:docs"`
		Chunks int64 `gorm:"column:chunks"`
		Ready  int64 `gorm:"column:ready"`
		Failed int64 `gorm:"column:failed"`
		Queued int64 `gorm:"column:queued"`
		Size   int64 `gorm:"column:size"`
	}
	err := s.db.WithContext(ctx).Model(&Document{}).
		Select("COUNT(*) AS docs, COALESCE(SUM(chunk_count),0) AS chunks, "+
			"COALESCE(SUM(CASE WHEN status = ? THEN 1 ELSE 0 END),0) AS ready, "+
			"COALESCE(SUM(CASE WHEN status = ? THEN 1 ELSE 0 END),0) AS failed, "+
			"COALESCE(SUM(CASE WHEN status IN ? THEN 1 ELSE 0 END),0) AS queued, "+
			"COALESCE(SUM(size),0) AS size",
			StatusReady, StatusFailed, []string{StatusPending, StatusProcessing}).
		Scan(&row).Error
	if err != nil {
		return nil, err
	}
	pending, err := s.pendingMisses(ctx)
	if err != nil {
		return nil, err
	}
	return &Stats{
		Docs: row.Docs, Chunks: row.Chunks, Ready: row.Ready, Failed: row.Failed,
		Queued: row.Queued, Size: row.Size, PendingMiss: pending,
	}, nil
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

// ListChunks 返回一篇文档的全部知识块（按切分顺序）；向量字段不外发。
// 给前端的块预览用：排查「AI 为什么这么答」时直接看库里实际参与检索的文本。
func (s *Service) ListChunks(ctx context.Context, id string) (*Document, []Chunk, error) {
	docID, err := idgen.Parse(id)
	if err != nil {
		return nil, nil, response.BadRequest("error.bad_request")
	}
	var doc Document
	if err := s.db.WithContext(ctx).Take(&doc, int64(docID)).Error; err != nil {
		return nil, nil, response.NotFound("kb.document_not_found")
	}
	var chunks []Chunk
	if err := s.db.WithContext(ctx).Where("document_id = ?", docID).Order("seq").
		Limit(500).Find(&chunks).Error; err != nil {
		return nil, nil, err
	}
	return &doc, chunks, nil
}

// RenameDocument 只改展示标题，不动切块与向量（无需重新向量化）。
func (s *Service) RenameDocument(ctx context.Context, id, title string) (*Document, error) {
	docID, err := idgen.Parse(id)
	if err != nil {
		return nil, response.BadRequest("error.bad_request")
	}
	title = strings.TrimSpace(title)
	if title == "" {
		return nil, response.BadRequest("kb.title_required")
	}
	if utf8.RuneCountInString(title) > 200 {
		title = string([]rune(title)[:200])
	}
	res := s.db.WithContext(ctx).Model(&Document{}).Where("id = ?", int64(docID)).
		Updates(map[string]any{"title": title, "updated_at": s.now()})
	if res.Error != nil {
		return nil, res.Error
	}
	if res.RowsAffected == 0 {
		return nil, response.NotFound("kb.document_not_found")
	}
	return s.reload(ctx, docID)
}

// Reembed 用当前配置的 embedding 模型重跑一篇文档（模型切换后的旧文档补救）。
// requireSettledDoc 拒绝在异步入库尚未结束时做重嵌/重切：worker 正持有这篇文档，
// 并发改写切块会产生半套向量。排队/处理中请等状态落定再操作。
func requireSettledDoc(doc *Document) *response.Error {
	switch doc.Status {
	case StatusPending, StatusProcessing:
		return response.Conflict("kb.document_busy")
	}
	return nil
}

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
	if busy := requireSettledDoc(&doc); busy != nil {
		return nil, busy
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

// docText 取一篇文档的全文与它的行：优先用入库时存的原文（003 起）；
// 老文档没有原文，就把切块拼回去——重叠只按当前设置“精确试剥”，匹配不上宁可留重复。
func (s *Service) docText(ctx context.Context, docID idgen.ID) (*Document, string, error) {
	var doc Document
	if err := s.db.WithContext(ctx).Take(&doc, int64(docID)).Error; err != nil {
		return nil, "", response.NotFound("kb.document_not_found")
	}
	if doc.SourceText.Valid && strings.TrimSpace(doc.SourceText.String) != "" {
		return &doc, strings.TrimSpace(doc.SourceText.String), nil
	}
	var chunks []Chunk
	if err := s.db.WithContext(ctx).Where("document_id = ?", docID).Order("seq").Find(&chunks).Error; err != nil {
		return nil, "", err
	}
	texts := make([]string, len(chunks))
	for i, c := range chunks {
		texts[i] = c.Content
	}
	return &doc, joinChunks(texts, s.Settings(ctx).ChunkOverlap), nil
}

// RechunkPreview 用当前分块参数把这篇文档重切一遍，只返回切出来的块、不落库：
// 「改分块参数」先在眼前看一眼切成长什么样，再决定要不要真的重切。
func (s *Service) RechunkPreview(ctx context.Context, id string) (*Document, []string, error) {
	docID, err := idgen.Parse(id)
	if err != nil {
		return nil, nil, response.BadRequest("error.bad_request")
	}
	doc, text, err := s.docText(ctx, docID)
	if err != nil {
		return nil, nil, err
	}
	if busy := requireSettledDoc(doc); busy != nil {
		return nil, nil, busy
	}
	if strings.TrimSpace(text) == "" {
		return nil, nil, response.BadRequest("kb.document_empty")
	}
	st := s.Settings(ctx)
	blocks := splitBlocks(text, st.ChunkSize, st.ChunkOverlap)
	if len(blocks) == 0 {
		return nil, nil, response.BadRequest("kb.document_empty")
	}
	return doc, blocks, nil
}

// Rechunk 按当前分块参数重切并重新向量化，替换该文档的全部切块。
// 与 Reembed 的分工：Reembed 只重算向量（切块不变），Rechunk 连切块一起重来——
// 换了分块参数、或一篇文档切得不合适时用它，顺带把模型也切到当前配置。
func (s *Service) Rechunk(ctx context.Context, id string) (*Document, error) {
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
	doc, text, err := s.docText(ctx, docID)
	if err != nil {
		return nil, err
	}
	if busy := requireSettledDoc(doc); busy != nil {
		return nil, busy
	}
	if strings.TrimSpace(text) == "" {
		return nil, response.BadRequest("kb.document_empty")
	}
	st := s.Settings(ctx)
	blocks := splitBlocks(text, st.ChunkSize, st.ChunkOverlap)
	if len(blocks) == 0 {
		return nil, response.BadRequest("kb.document_empty")
	}
	vectors, err := s.embedAll(ctx, cfg, blocks)
	if err != nil {
		return nil, err
	}
	now := s.now()
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("document_id = ?", docID).Delete(&Chunk{}).Error; err != nil {
			return err
		}
		rows := make([]Chunk, len(blocks))
		for i, b := range blocks {
			raw, merr := json.Marshal(vectors[i])
			if merr != nil {
				return merr
			}
			rowID, ierr := idgen.Next()
			if ierr != nil {
				return ierr
			}
			rows[i] = Chunk{DocumentID: docID, Seq: i + 1, Content: b, Vector: string(raw)}
			rows[i].ID = rowID
			rows[i].CreatedAt, rows[i].UpdatedAt = now, now
		}
		if err := tx.CreateInBatches(rows, 100).Error; err != nil {
			return err
		}
		return tx.Model(&Document{}).Where("id = ?", int64(docID)).Updates(map[string]any{
			"chunk_count": len(blocks), "model": cfg.EmbedModel, "dimension": len(vectors[0]),
			"status": StatusReady, "error": "", "updated_at": now,
			// 老文档重切时把原文补上：以后再重切就不用拼切块猜重叠了。
			"source_text": text,
		}).Error
	})
	if err != nil {
		return nil, err
	}
	s.cache.invalidate()
	return s.reload(ctx, docID)
}

func truncateRunes(s string, n int) string {
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	return string([]rune(s)[:n]) + "…"
}
