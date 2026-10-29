package tnote

import (
	"context"
	"errors"

	"gorm.io/gorm"

	"nexadmin/core/response"
	tn "nexadmin/core/tenant"
	"nexadmin/pkg/idgen"
)

// Service 是 Tnote 的业务层：只经 gorm 参数化查询。
// 表有 tenant_id 列：查询 / 更新 / 删除由框架租户 scope 自动追加 tenant_id 条件，创建时在 Save 里显式填入当前租户。
type Service struct{ db *gorm.DB }

// NewService 创建服务。
func NewService(db *gorm.DB) *Service { return &Service{db: db} }

func ids64(ids []idgen.ID) []int64 {
	out := make([]int64, len(ids))
	for i, v := range ids {
		out[i] = int64(v)
	}
	return out
}

func toView(m *Tnote) View {
	return View{
		ID:      m.ID,
		Content: m.Content,
		Level:   m.Level,
	}
}

// apply 把请求体写到模型上。
func (f Form) apply(m *Tnote) error {
	m.Content = f.Content
	if m.Content == "" {
		return response.BadRequest("error.bad_request").WithMsg("content is required")
	}
	m.Level = f.Level
	return nil
}

func (s *Service) get(ctx context.Context, id idgen.ID) (*Tnote, error) {
	var m Tnote
	err := s.db.WithContext(ctx).First(&m, "id = ?", int64(id)).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, response.NotFound("error.not_found")
	}
	return &m, err
}

// List 分页列表，按 id 倒序（新的在前）。
func (s *Service) List(ctx context.Context, q ListQuery) ([]View, int64, error) {
	base := s.db.WithContext(ctx).Model(&Tnote{})
	base = base.Session(&gorm.Session{})
	var total int64
	if err := base.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var rows []Tnote
	err := base.Order("id DESC").Offset((q.Page - 1) * q.PageSize).Limit(q.PageSize).Find(&rows).Error
	if err != nil {
		return nil, 0, err
	}
	out := make([]View, len(rows))
	for i := range rows {
		out[i] = toView(&rows[i])
	}
	return out, total, nil
}

// Get 返回详情。
func (s *Service) Get(ctx context.Context, id idgen.ID) (*View, error) {
	m, err := s.get(ctx, id)
	if err != nil {
		return nil, err
	}
	v := toView(m)
	return &v, nil
}

// Save 新增（id 为 0）或修改。
func (s *Service) Save(ctx context.Context, id idgen.ID, f Form) (*View, error) {
	var m Tnote
	if id != 0 {
		cur, err := s.get(ctx, id)
		if err != nil {
			return nil, err
		}
		m = *cur
	}
	if err := f.apply(&m); err != nil {
		return nil, err
	}
	db := s.db.WithContext(ctx)
	if id == 0 {
		nid, err := idgen.Next()
		if err != nil {
			return nil, err
		}
		m.ID = nid
		tid, ok := tn.From(ctx)
		if !ok {
			return nil, response.BadRequest("error.bad_request").WithMsg("missing tenant")
		}
		m.TenantID = tid
		if err := db.Create(&m).Error; err != nil {
			return nil, err
		}
	} else if err := db.Save(&m).Error; err != nil {
		return nil, err
	}
	v := toView(&m)
	return &v, nil
}

// Delete 删除（不存在的 ID 忽略）。
func (s *Service) Delete(ctx context.Context, ids []idgen.ID) error {
	return s.db.WithContext(ctx).Where("id IN ?", ids64(ids)).Delete(&Tnote{}).Error
}
