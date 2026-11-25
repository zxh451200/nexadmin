package kb

// 异步入库队列：上传请求只抽取文本、建 pending 文档并唤醒 worker，切块/向量化在后台做。
//
// 设计要点：
//   - DB 状态是唯一真相来源，进程内 channel 只是唤醒信号。重启、队列满、多实例都不丢任务。
//   - 抢占用 UPDATE ... WHERE status='pending' 的乐观锁，影响行数为 1 的实例才处理。
//   - 上次进程崩溃遗留的 processing 超过 10 分钟回收重跑；不会抢走别的实例正在跑的任务。
//   - worker 固定 2 个，避免批量上传把 embedding 网关打爆。

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"gorm.io/gorm"

	"nexadmin/core/response"
	"nexadmin/pkg/idgen"
)

const (
	ingestWorkers       = 2
	ingestPollInterval  = 2 * time.Second
	staleIngestAfter    = 10 * time.Minute
	errTextNoEmbed      = "Embedding 模型未配置"
	errTextEmbedFailed  = "向量化服务调用失败，可点重试"
	errTextWriteFailed  = "写入知识库失败，可点重试"
	errTextInactive     = "知识库插件未启用"
	errTextGeneric      = "入库失败，可点重试"
	errTextEmptySource  = "文档没有可用内容"
	errTextEmptyChunks  = "切分后没有可用知识块"
)

type ingestQueue struct {
	svc  *Service
	wake chan struct{}
}

func newIngestQueue(svc *Service) *ingestQueue {
	return &ingestQueue{svc: svc, wake: make(chan struct{}, 1)}
}

// notify 唤醒空闲 worker；队列满时丢弃信号也无妨——worker 每 2 秒还会扫库。
func (q *ingestQueue) notify() {
	select {
	case q.wake <- struct{}{}:
	default:
	}
}

// start 在插件 Start 里调用一次：先回收崩溃遗留，再拉起固定数量 worker。
func (q *ingestQueue) start(ctx context.Context) {
	q.reclaim(ctx)
	for i := 0; i < ingestWorkers; i++ {
		go q.worker(ctx)
	}
}

func (q *ingestQueue) worker(ctx context.Context) {
	ticker := time.NewTicker(ingestPollInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-q.wake:
		case <-ticker.C:
		}
		q.reclaim(ctx)
		for ctx.Err() == nil {
			id, ok := q.claimOne(ctx)
			if !ok {
				break
			}
			q.process(ctx, id)
		}
	}
}

// reclaim 把卡死超过 10 分钟的 processing 放回 pending。stage 更新会刷新 updated_at，
// 所以正常处理中的任务不会被误回收；只在极长时间无进展时才视为上次进程崩溃。
func (q *ingestQueue) reclaim(ctx context.Context) {
	cutoff := q.svc.now().Add(-staleIngestAfter)
	res := q.svc.db.WithContext(ctx).Model(&Document{}).
		Where("status = ?", StatusProcessing).Where("updated_at < ?", cutoff).
		Updates(map[string]any{
			"status": StatusPending, "stage": "重新排队", "error": "", "updated_at": q.svc.now(),
		})
	if res.Error == nil && res.RowsAffected > 0 && q.svc.log != nil {
		q.svc.log.Warn("kb: reclaimed stale ingest tasks", "count", res.RowsAffected)
	}
}

// claimOne 按先来先服务抢占一条 pending，乐观锁保证多 worker / 多实例不重复。
func (q *ingestQueue) claimOne(ctx context.Context) (idgen.ID, bool) {
	var doc Document
	if err := q.svc.db.WithContext(ctx).Where("status = ?", StatusPending).
		Order("created_at ASC, id ASC").Limit(1).Take(&doc).Error; err != nil {
		return 0, false
	}
	res := q.svc.db.WithContext(ctx).Model(&Document{}).
		Where("id = ?", int64(doc.ID)).Where("status = ?", StatusPending).
		Updates(map[string]any{
			"status": StatusProcessing, "stage": "准备中", "updated_at": q.svc.now(),
		})
	if res.Error != nil || res.RowsAffected == 0 {
		return 0, false // 被别的 worker 抢走，下一轮再看
	}
	return doc.ID, true
}

// process 完成一篇文档的切块、向量化和落库；任何失败都落回 failed + 可读原因，
// 不把文档卡在 processing（worker panic 由 framework 的 recover 之外再兜一层）。
func (q *ingestQueue) process(ctx context.Context, id idgen.ID) {
	svc := q.svc
	func() {
		defer func() {
			if r := recover(); r != nil {
				if svc.log != nil {
					svc.log.Error("kb: async ingest panic", "doc", int64(id), "panic", r)
				}
				svc.finishIngest(ctx, id, StatusFailed, "", errTextGeneric, 0, "", 0)
			}
		}()
		q.processOne(ctx, id)
	}()
}

func (q *ingestQueue) processOne(ctx context.Context, id idgen.ID) {
	svc := q.svc
	var doc Document
	if err := svc.db.WithContext(ctx).Take(&doc, int64(id)).Error; err != nil {
		return
	}
	fail := func(text string) {
		svc.finishIngest(ctx, id, StatusFailed, "", text, 0, "", 0)
	}
	if svc.gw == nil {
		fail(errTextInactive)
		return
	}
	cfg, err := svc.gw.Load(ctx)
	if err != nil || !cfg.EmbedReady() {
		fail(errTextNoEmbed)
		return
	}
	cfg = cfg.Normalize()
	text := ""
	if doc.SourceText.Valid {
		text = strings.TrimSpace(doc.SourceText.String)
	}
	if text == "" {
		fail(errTextEmptySource)
		return
	}
	svc.setStage(ctx, id, "切分中")
	st := svc.Settings(ctx)
	blocks := splitBlocks(text, st.ChunkSize, st.ChunkOverlap)
	if len(blocks) == 0 {
		fail(errTextEmptyChunks)
		return
	}
	svc.setStage(ctx, id, fmt.Sprintf("向量化 0/%d 块", len(blocks)))
	vectors, err := svc.embedAllWithProgress(ctx, cfg, blocks, func(done int) {
		svc.setStage(ctx, id, fmt.Sprintf("向量化 %d/%d 块", done, len(blocks)))
	})
	if err != nil {
		fail(ingestErrorText(err))
		return
	}
	svc.setStage(ctx, id, "写入中")
	now := svc.now()
	rows := make([]Chunk, len(blocks))
	for i, b := range blocks {
		rowID, ierr := idgen.Next()
		if ierr != nil {
			fail(errTextWriteFailed)
			return
		}
		raw, merr := json.Marshal(vectors[i])
		if merr != nil {
			fail(errTextWriteFailed)
			return
		}
		rows[i] = Chunk{DocumentID: id, Seq: i + 1, Content: b, Vector: string(raw)}
		rows[i].ID = rowID
		rows[i].CreatedAt, rows[i].UpdatedAt = now, now
	}
	err = svc.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("document_id = ?", id).Delete(&Chunk{}).Error; err != nil {
			return err
		}
		if err := tx.CreateInBatches(rows, 100).Error; err != nil {
			return err
		}
		return tx.Model(&Document{}).Where("id = ?", int64(id)).Updates(map[string]any{
			"status": StatusReady, "stage": "", "error": "", "chunk_count": len(blocks),
			"model": cfg.EmbedModel, "dimension": len(vectors[0]), "updated_at": now,
		}).Error
	})
	if err != nil {
		fail(errTextWriteFailed)
		return
	}
	svc.cache.invalidate()
}

// ingestErrorText 把后台失败原因收成一句可展示的短句（后台没有请求语言上下文），
// 前端列表原样展示；不泄露底层调用细节。
func ingestErrorText(err error) string {
	var re *response.Error
	if errors.As(err, &re) {
		switch re.Key {
		case "kb.embed_unavailable":
			return errTextNoEmbed
		case "kb.embed_failed":
			return errTextEmbedFailed
		case "kb.plugin_inactive":
			return errTextInactive
		}
		if re.Msg != "" {
			return re.Msg
		}
	}
	return errTextGeneric
}

// Enqueue 接收一篇已抽取的文本：只建 pending 文档并唤醒 worker，不在这里做向量化。
// 上传请求因此与 embedding 耗时解耦——批量传 PDF 是「投递即返回」，后台慢慢跑。
func (s *Service) Enqueue(ctx context.Context, in IngestInput) (*Document, error) {
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
	text := strings.TrimSpace(in.Text)
	if text == "" {
		return nil, response.BadRequest("kb.document_empty")
	}
	now := s.now()
	fp := fingerprint(text)
	model := cfg.Normalize().EmbedModel
	meta := map[string]any{
		"title": strings.TrimSpace(in.Title), "source_name": strings.TrimSpace(in.Source),
		"mime": strings.TrimSpace(in.Mime), "size": in.Size,
		"source_text": text, "fingerprint": fp, "model": model,
		"status": StatusPending, "stage": "排队中", "error": "", "chunk_count": 0,
		"updated_at": now,
	}

	// 同内容重传：覆盖原文档而不是再建一篇。重复文档会占满 topK，把别的资料挤掉。
	var existing Document
	if err := s.db.WithContext(ctx).Where("fingerprint = ? AND fingerprint <> ''", fp).
		Order("id DESC").Take(&existing).Error; err == nil {
		err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
			if err := tx.Where("document_id = ?", existing.ID).Delete(&Chunk{}).Error; err != nil {
				return err
			}
			return tx.Model(&Document{}).Where("id = ?", int64(existing.ID)).Updates(meta).Error
		})
		if err != nil {
			return nil, err
		}
		s.cache.invalidate()
		s.queue.notify()
		return s.reload(ctx, existing.ID)
	}

	id, err := idgen.Next()
	if err != nil {
		return nil, err
	}
	doc := Document{
		Title: strings.TrimSpace(in.Title), SourceName: strings.TrimSpace(in.Source),
		Mime: strings.TrimSpace(in.Mime), Size: in.Size,
		Status: StatusPending, Stage: "排队中", ChunkCount: 0,
		Model: model, Fingerprint: fp,
		SourceText: sql.NullString{String: text, Valid: true},
		CreatedAt: now, UpdatedAt: now,
	}
	doc.ID = id
	if err := s.db.WithContext(ctx).Create(&doc).Error; err != nil {
		return nil, err
	}
	s.queue.notify()
	return &doc, nil
}

// RetryIngest 把失败文档重新排队：原文已在 source_text 里，不用让用户重传文件。
// 只接受 failed——卡住的 processing 由 reclaim 超时回收，避免和正在跑的 worker 撞车。
func (s *Service) RetryIngest(ctx context.Context, id string) (*Document, error) {
	docID, err := idgen.Parse(id)
	if err != nil {
		return nil, response.BadRequest("error.bad_request")
	}
	res := s.db.WithContext(ctx).Model(&Document{}).
		Where("id = ?", int64(docID)).Where("status = ?", StatusFailed).
		Updates(map[string]any{
			"status": StatusPending, "stage": "重新排队", "error": "", "updated_at": s.now(),
		})
	if res.Error != nil {
		return nil, res.Error
	}
	if res.RowsAffected == 0 {
		return nil, response.NotFound("kb.document_not_found")
	}
	s.queue.notify()
	return s.reload(ctx, docID)
}

// setStage / finishIngest 属于 Service，放在这里是因为只有异步入库用它们。
func (s *Service) setStage(ctx context.Context, id idgen.ID, stage string) {
	_ = s.db.WithContext(ctx).Model(&Document{}).Where("id = ?", int64(id)).
		Updates(map[string]any{"stage": stage, "updated_at": s.now()}).Error
}

func (s *Service) finishIngest(ctx context.Context, id idgen.ID, status, stage, errText string, chunks int, model string, dim int) {
	updates := map[string]any{
		"status": status, "stage": stage, "error": errText, "updated_at": s.now(),
	}
	if chunks >= 0 {
		updates["chunk_count"] = chunks
	}
	if model != "" {
		updates["model"] = model
		updates["dimension"] = dim
	}
	if err := s.db.WithContext(ctx).Model(&Document{}).Where("id = ?", int64(id)).Updates(updates).Error; err != nil && s.log != nil {
		s.log.Warn("kb: finish ingest failed", "doc", int64(id), "err", err.Error())
	}
}
