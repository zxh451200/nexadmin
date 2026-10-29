package demo

import (
	"context"
	"errors"
	"strings"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"nexadmin/core/response"
	"nexadmin/pkg/idgen"
)

// Service 是 Demo 的业务层：只经 gorm 参数化查询。
// 表有 tenant_id 列：查询 / 更新 / 删除由框架租户 scope 自动追加 tenant_id 条件。
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

var timeLayouts = []string{time.RFC3339Nano, "2006-01-02T15:04:05", "2006-01-02 15:04:05", "2006-01-02T15:04", "2006-01-02 15:04", "2006-01-02"}

// parseTime 解析日期 / 日期时间；空串或 nil 返回 nil。
func parseTime(s *string) (*time.Time, error) {
	if s == nil || strings.TrimSpace(*s) == "" {
		return nil, nil
	}
	v := strings.TrimSpace(*s)
	for _, l := range timeLayouts {
		if t, err := time.Parse(l, v); err == nil {
			return &t, nil
		}
	}
	return nil, &time.ParseError{Value: v, Message: "unsupported time format"}
}

func toView(m *Demo) View {
	return View{
		ID:          m.ID,
		Title:       m.Title,
		Summary:     m.Summary,
		Qty:         m.Qty,
		Price:       m.Price,
		IsHot:       m.IsHot,
		OwnerID:     m.OwnerID,
		PublishedAt: m.PublishedAt,
		Birthday:    m.Birthday,
		Status:      m.Status,
		CreatedAt:   m.CreatedAt,
		UpdatedAt:   m.UpdatedAt,
	}
}

// apply 把请求体写到模型上。
func (f Form) apply(m *Demo) error {
	m.Title = strings.TrimSpace(f.Title)
	if m.Title == "" {
		return response.BadRequest("error.bad_request").WithMsg("title is required")
	}
	m.Summary = f.Summary
	m.Qty = f.Qty
	m.Price = f.Price
	m.IsHot = f.IsHot
	m.OwnerID = f.OwnerID
	tPublishedAt, err := parseTime(f.PublishedAt)
	if err != nil {
		return response.BadRequest("error.bad_request").WithMsg("invalid publishedAt")
	}
	m.PublishedAt = tPublishedAt
	tBirthday, err := parseTime(f.Birthday)
	if err != nil {
		return response.BadRequest("error.bad_request").WithMsg("invalid birthday")
	}
	m.Birthday = tBirthday
	m.Status = strings.TrimSpace(f.Status)
	return nil
}

func (s *Service) get(ctx context.Context, id idgen.ID) (*Demo, error) {
	var m Demo
	err := s.db.WithContext(ctx).First(&m, "id = ?", int64(id)).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, response.NotFound("error.not_found")
	}
	return &m, err
}

// List 分页列表，按 id 倒序（新的在前）。
func (s *Service) List(ctx context.Context, q ListQuery) ([]View, int64, error) {
	base := s.db.WithContext(ctx).Model(&Demo{})
	if q.Title != "" {
		base = base.Where(clause.Like{Column: clause.Column{Name: "title"}, Value: likeArg(q.Title)})
	}
	if q.IsHot != nil {
		base = base.Where(clause.Eq{Column: clause.Column{Name: "is_hot"}, Value: *q.IsHot})
	}
	if q.Status != "" {
		base = base.Where(clause.Like{Column: clause.Column{Name: "status"}, Value: likeArg(q.Status)})
	}
	base = base.Session(&gorm.Session{})
	var total int64
	if err := base.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var rows []Demo
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
	var m Demo
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
	return s.db.WithContext(ctx).Where("id IN ?", ids64(ids)).Delete(&Demo{}).Error
}
