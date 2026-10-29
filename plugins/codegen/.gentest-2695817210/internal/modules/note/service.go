package note

import (
	"context"
	"errors"
	"strings"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"nexadmin/core/response"
	"nexadmin/pkg/idgen"
)

// Service 是 Note 的业务层：只经 gorm 参数化查询。
// 表没有 tenant_id 列，不做租户隔离。
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

// likeArg 转义 LIKE 通配符。
func likeArg(s string) string {
	r := strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`)
	return "%" + r.Replace(s) + "%"
}

func toView(m *Note) View {
	return View{
		ID:   m.ID,
		Name: m.Name,
	}
}

// apply 把请求体写到模型上。
func (f Form) apply(m *Note) error {
	m.Name = strings.TrimSpace(f.Name)
	if m.Name == "" {
		return response.BadRequest("error.bad_request").WithMsg("name is required")
	}
	return nil
}

func (s *Service) get(ctx context.Context, id idgen.ID) (*Note, error) {
	var m Note
	err := s.db.WithContext(ctx).First(&m, "id = ?", int64(id)).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, response.NotFound("error.not_found")
	}
	return &m, err
}

// List 分页列表，按 id 倒序（新的在前）。
func (s *Service) List(ctx context.Context, q ListQuery) ([]View, int64, error) {
	base := s.db.WithContext(ctx).Model(&Note{})
	if q.Name != "" {
		base = base.Where(clause.Like{Column: clause.Column{Name: "name"}, Value: likeArg(q.Name)})
	}
	base = base.Session(&gorm.Session{})
	var total int64
	if err := base.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var rows []Note
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
	var m Note
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
	return s.db.WithContext(ctx).Where("id IN ?", ids64(ids)).Delete(&Note{}).Error
}
