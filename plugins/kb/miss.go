package kb

// 未命中问题池：机器人检索不到时把访客问题攒起来，按热度排序；管理员补一条答案，
// 就把它变成一篇 QA 文档——知识库靠真实提问持续增长，而不是靠人工凭空想 FAQ。
//
// 只有「真正的检索未命中」才记录（见 bot.go）：频控超限、模型调用失败是系统问题，
// 记进来只会污染运营队列。管理端测试提问同样不记录，那是调试动作。

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"unicode/utf8"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"nexadmin/core"
	"nexadmin/core/response"
	"nexadmin/pkg/idgen"
)

// questionTrimSet 归一化时从首尾剥掉的标点：同一句问法带不带问号不该算两条。
const questionTrimSet = " \t\r\n。！？；，、,.!?;:：\"'“”‘’()（）[]【】"

// RecordMiss 记录一次未命中。归一化后按 norm_hash upsert：同一问题只攒一条，
// 热度 AskCount 自增；已忽略的不再重开（尊重管理员决定），已转 QA 的再次未命中
// 则重开为待补（说明补进去的答案没解决问题，得改）。
func (s *Service) RecordMiss(ctx context.Context, question, convNo string) error {
	q := strings.TrimSpace(question)
	if q == "" {
		return nil
	}
	if utf8.RuneCountInString(q) > maxAskRunes {
		q = string([]rune(q)[:maxAskRunes])
	}
	norm := normalizeQuestion(q)
	if norm == "" {
		return nil
	}
	sum := sha256.Sum256([]byte(norm))
	hash := hex.EncodeToString(sum[:])
	id, err := idgen.Next()
	if err != nil {
		return err
	}
	now := s.now()
	row := Miss{
		Question: q, NormHash: hash, AskCount: 1, Status: MissPending,
		LastConvNo: convNo, FirstAt: now, LastAt: now,
	}
	row.ID = id
	return s.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "norm_hash"}},
		DoUpdates: clause.Assignments(map[string]any{
			"ask_count":    gorm.Expr("ask_count + 1"),
			"last_at":      now,
			"last_conv_no": convNo,
			"updated_at":   now,
			"status": gorm.Expr("CASE WHEN status = ? THEN ? ELSE status END",
				MissResolved, MissPending),
			"document_id": gorm.Expr("CASE WHEN status = ? THEN 0 ELSE document_id END", MissResolved),
		}),
	}).Create(&row).Error
}

// normalizeQuestion 折叠空白、统一大小写、剥首尾标点。中间的标点保留——
// "保修三年，能延保吗" 和 "保修三年能延保吗" 是不同问题，不该合并。
func normalizeQuestion(q string) string {
	q = strings.ToLower(strings.TrimSpace(q))
	q = strings.Join(strings.Fields(q), " ")
	return strings.Trim(q, questionTrimSet)
}

// MissQuery 是问题池列表的筛选 / 排序 / 分页参数。
type MissQuery struct {
	Status   string
	Sort     string
	Desc     bool
	Page     int
	PageSize int
}

// ListMisses 分页列出未命中问题。默认按提问次数降序——被问得最多的先补。
func (s *Service) ListMisses(ctx context.Context, q MissQuery) ([]Miss, int64, error) {
	db := s.db.WithContext(ctx).Model(&Miss{})
	switch q.Status {
	case MissPending, MissResolved, MissIgnored:
		db = db.Where("status = ?", q.Status)
	}
	var total int64
	if err := db.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	page, pageSize := q.Page, q.PageSize
	if page < 1 {
		page = 1
	}
	if pageSize < 1 {
		pageSize = 20
	}
	if pageSize > 100 {
		pageSize = 100
	}
	sortField := q.Sort
	if sortField == "" {
		sortField = "ask_count"
	}
	var rows []Miss
	if err := db.Order(core.OrderBy(sortField, q.Desc, map[string]string{
		"question":   "question",
		"status":     "status",
		"askCount":   "ask_count",
		"ask_count":  "ask_count",
		"lastAt":     "last_at",
		"last_at":    "last_at",
	}, "ask_count")).Order("id DESC").
		Offset((page - 1) * pageSize).Limit(pageSize).Find(&rows).Error; err != nil {
		return nil, 0, err
	}
	return rows, total, nil
}

// ResolveMiss 把一条问题 + 管理员补的答案写成 QA 文档，并把问题项标为已解决。
// 复用 Ingest 的切分 / 向量化 / 索引失效链路，不另起一套写入逻辑。
// 文档先建、状态后改：两步没法放进一个事务（Ingest 内部要调 embedding 且有自身事务），
// 中途失败最多留下一条「已解决但实际未重开」的脏状态，下次同问题会重开并再补一篇，
// 不会把答案静默丢掉。
func (s *Service) ResolveMiss(ctx context.Context, id, answer string) (*Document, error) {
	missID, err := idgen.Parse(id)
	if err != nil {
		return nil, response.BadRequest("error.bad_request")
	}
	answer = strings.TrimSpace(answer)
	if answer == "" || utf8.RuneCountInString(answer) > 4000 {
		return nil, response.BadRequest("error.bad_request")
	}
	var miss Miss
	if err := s.db.WithContext(ctx).Take(&miss, int64(missID)).Error; err != nil {
		return nil, response.NotFound("error.not_found")
	}
	q := strings.TrimSpace(miss.Question)
	qa := "问：" + q + "\n答：" + answer
	title := q
	if utf8.RuneCountInString(title) > 200 {
		title = string([]rune(title)[:200])
	}
	doc, err := s.Ingest(ctx, IngestInput{
		Title: title, Source: "未命中问题转QA.txt", Mime: "text/plain", Size: int64(len(qa)), Text: qa,
	})
	if err != nil {
		return nil, err
	}
	if err := s.db.WithContext(ctx).Model(&Miss{}).Where("id = ?", int64(missID)).Updates(map[string]any{
		"status": MissResolved, "document_id": doc.ID, "updated_at": s.now(),
	}).Error; err != nil && s.log != nil {
		s.log.Warn("kb: mark miss resolved failed", "miss", int64(missID), "err", err.Error())
	}
	return doc, nil
}

// IgnoreMisses 批量忽略待补问题，返回实际改动条数（已忽略的不再重复计）。
func (s *Service) IgnoreMisses(ctx context.Context, ids []string) (int64, error) {
	if len(ids) == 0 {
		return 0, nil
	}
	if len(ids) > 100 {
		ids = ids[:100]
	}
	ids64 := make([]int64, 0, len(ids))
	for _, raw := range ids {
		if v, err := idgen.Parse(raw); err == nil {
			ids64 = append(ids64, int64(v))
		}
	}
	if len(ids64) == 0 {
		return 0, nil
	}
	res := s.db.WithContext(ctx).Model(&Miss{}).
		Where("id IN ?", ids64).Where("status = ?", MissPending).
		Updates(map[string]any{"status": MissIgnored, "updated_at": s.now()})
	return res.RowsAffected, res.Error
}

// ReopenMiss 把已忽略 / 已解决的问题重新放回待补队列。
func (s *Service) ReopenMiss(ctx context.Context, id string) (*Miss, error) {
	missID, err := idgen.Parse(id)
	if err != nil {
		return nil, response.BadRequest("error.bad_request")
	}
	res := s.db.WithContext(ctx).Model(&Miss{}).
		Where("id = ?", int64(missID)).Where("status <> ?", MissPending).
		Updates(map[string]any{"status": MissPending, "document_id": 0, "updated_at": s.now()})
	if res.Error != nil {
		return nil, res.Error
	}
	if res.RowsAffected == 0 {
		return nil, response.NotFound("error.not_found")
	}
	var miss Miss
	if err := s.db.WithContext(ctx).Take(&miss, int64(missID)).Error; err != nil {
		return nil, err
	}
	return &miss, nil
}

// DeleteMiss 删除问题项；已转成 QA 的文档是独立文档，不跟着删（在文档列表里自行管理）。
func (s *Service) DeleteMiss(ctx context.Context, id string) error {
	missID, err := idgen.Parse(id)
	if err != nil {
		return response.BadRequest("error.bad_request")
	}
	res := s.db.WithContext(ctx).Delete(&Miss{}, int64(missID))
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return response.NotFound("error.not_found")
	}
	return nil
}

// pendingMisses 给全库统计条用：待补问题是「知识库还缺什么」的直接读数。
func (s *Service) pendingMisses(ctx context.Context) (int64, error) {
	var n int64
	err := s.db.WithContext(ctx).Model(&Miss{}).Where("status = ?", MissPending).Count(&n).Error
	return n, err
}
