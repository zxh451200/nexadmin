package notice

import (
	"context"
	"errors"
	"strings"

	"gorm.io/gorm"

	"nexadmin/core/response"
	"nexadmin/core/tenant"
	"nexadmin/modules/internal/recbin"
	"nexadmin/pkg/idgen"
)

// Service 是公告的业务层：管理端 CRUD、发布/撤回、删除入回收站。
type Service struct{ db *gorm.DB }

func NewService(db *gorm.DB) *Service { return &Service{db: db} }

func ids64(ids []idgen.ID) []int64 {
	out := make([]int64, len(ids))
	for i, v := range ids {
		out[i] = int64(v)
	}
	return out
}

// likeArg 转义 LIKE 通配符。
func likeArg(s string) string {
	r := strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`)
	return "%" + r.Replace(s) + "%"
}

func (s *Service) get(ctx context.Context, id idgen.ID) (*Notice, error) {
	var n Notice
	err := s.db.WithContext(ctx).First(&n, "id = ?", int64(id)).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, response.NotFound("error.not_found")
	}
	return &n, err
}

func (s *Service) targetIDs(ctx context.Context, id idgen.ID) []idgen.ID {
	var ts []Target
	s.db.WithContext(ctx).Where("notice_id = ?", int64(id)).Find(&ts)
	out := make([]idgen.ID, 0, len(ts))
	for _, t := range ts {
		out = append(out, t.TargetID)
	}
	return out
}

// List 管理端分页列表：置顶在前，其后按发布时间（草稿按创建时间）倒序。
func (s *Service) List(ctx context.Context, q ListQuery) ([]View, int64, error) {
	base := scopedNotices(s.db.WithContext(ctx).Model(&Notice{}), q.Scope)
	if q.Keyword != "" {
		base = base.Where("title LIKE ?", likeArg(q.Keyword))
	}
	for col, v := range map[string]string{"type": q.Type, "level": q.Level, "status": q.Status} {
		if v != "" {
			base = base.Where(col+" = ?", v)
		}
	}
	base = base.Session(&gorm.Session{})
	var total int64
	if err := base.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var rows []Notice
	err := base.Order("pinned DESC").Order("COALESCE(published_at, created_at) DESC").
		Offset((q.Page - 1) * q.PageSize).Limit(q.PageSize).Find(&rows).Error
	if err != nil {
		return nil, 0, err
	}
	views, err := s.views(ctx, rows)
	return views, total, err
}

// views 批量补齐 targetIds 与已读人数。
func (s *Service) views(ctx context.Context, rows []Notice) ([]View, error) {
	out := make([]View, len(rows))
	if len(rows) == 0 {
		return out, nil
	}
	ids := make([]int64, len(rows))
	for i, r := range rows {
		ids[i] = int64(r.ID)
	}
	var ts []Target
	if err := s.db.WithContext(ctx).Where("notice_id IN ?", ids).Find(&ts).Error; err != nil {
		return nil, err
	}
	targets := map[idgen.ID][]idgen.ID{}
	for _, t := range ts {
		targets[t.NoticeID] = append(targets[t.NoticeID], t.TargetID)
	}
	type cnt struct {
		NoticeID idgen.ID
		N        int64
	}
	var cs []cnt
	err := s.db.WithContext(tenant.Skip(ctx)).Model(&Read{}).Select("notice_id, COUNT(*) AS n").
		Where("notice_id IN ?", ids).Group("notice_id").Scan(&cs).Error
	if err != nil {
		return nil, err
	}
	reads := map[idgen.ID]int64{}
	for _, c := range cs {
		reads[c.NoticeID] = c.N
	}
	for i, n := range rows {
		tids := targets[n.ID]
		if tids == nil {
			tids = []idgen.ID{}
		}
		out[i] = View{ID: n.ID, Title: n.Title, Type: n.Type, Level: n.Level, Status: n.Status, Content: n.Content,
			Scope: n.Scope, TargetIDs: tids, Pinned: n.Pinned, Popup: n.Popup, Platform: n.Platform,
			PublishAt: n.PublishAt, ExpireAt: n.ExpireAt, PublishedAt: n.PublishedAt,
			Attachments: attachmentsJSON(n.Attachments), ReadCount: reads[n.ID], TotalCount: n.TotalCount,
			CreatedAt: n.CreatedAt, UpdatedAt: n.UpdatedAt}
	}
	return out, nil
}

// Get 返回单条公告详情。
func (s *Service) Get(ctx context.Context, id idgen.ID) (*View, error) {
	n, err := s.get(ctx, id)
	if err != nil {
		return nil, err
	}
	vs, err := s.views(ctx, []Notice{*n})
	if err != nil {
		return nil, err
	}
	return &vs[0], nil
}

// Delete 软删除并登记回收站（同一事务）。不存在的 ID 直接忽略。
func (s *Service) Delete(ctx context.Context, ids []idgen.ID, by string) error {
	var rows []Notice
	if err := s.db.WithContext(ctx).Where("id IN ?", ids64(ids)).Find(&rows).Error; err != nil {
		return err
	}
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		for _, n := range rows {
			if err := tx.Delete(&Notice{}, "id = ?", int64(n.ID)).Error; err != nil {
				return err
			}
			if err := recbin.Add(ctx, tx, recbin.TypeNotice, n.ID, n.Title, by); err != nil {
				return err
			}
		}
		return nil
	})
}
