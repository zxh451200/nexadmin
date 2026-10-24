package post

import (
	"context"
	"errors"
	"strings"

	"gorm.io/gorm"

	"nexadmin/core"
	"nexadmin/core/response"
	"nexadmin/pkg/idgen"
)

// Service 封装岗位的数据访问与业务规则。
type Service struct{ db *gorm.DB }

// NewService 创建服务。
func NewService(db *gorm.DB) *Service { return &Service{db: db} }

func errDupCode() error {
	return response.BadRequest("error.bad_request").WithMsg("岗位编码已存在")
}

func statusOr(s string) string {
	if s == "" {
		return "1"
	}
	return s
}

func nz(p *idgen.ID) *idgen.ID {
	if p == nil || *p == 0 {
		return nil
	}
	return p
}

func get(tx *gorm.DB, id idgen.ID) (*Post, error) {
	var p Post
	if err := tx.Where("id = ?", int64(id)).First(&p).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, response.NotFound("error.not_found")
		}
		return nil, err
	}
	return &p, nil
}

// codeTaken 判断同租户内编码是否已被其他岗位占用（租户条件由 scope 自动追加）。
func codeTaken(tx *gorm.DB, code string, exclude idgen.ID) (bool, error) {
	var n int64
	err := tx.Model(&Post{}).Where("code = ? AND id <> ?", code, int64(exclude)).Count(&n).Error
	return n > 0, err
}

// List 返回全部岗位（不分页），keyword 匹配名称或编码。
func (s *Service) List(ctx context.Context, keyword string) ([]Post, error) {
	q := s.db.WithContext(ctx).Order("sort, id")
	if kw := strings.ToLower(strings.TrimSpace(keyword)); kw != "" {
		like := "%" + strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(kw) + "%"
		q = q.Where("LOWER(name) LIKE ? OR LOWER(code) LIKE ?", like, like)
	}
	list := []Post{}
	return list, q.Find(&list).Error
}

// Options 返回启用岗位的 id+name，供用户表单下拉。
func (s *Service) Options(ctx context.Context) ([]Option, error) {
	var rows []Post
	if err := s.db.WithContext(ctx).Where("status = ?", "1").Order("sort, id").Find(&rows).Error; err != nil {
		return nil, err
	}
	out := make([]Option, 0, len(rows))
	for _, r := range rows {
		out = append(out, Option{ID: r.ID, Name: r.Name})
	}
	return out, nil
}

// Get 读取单个岗位。
func (s *Service) Get(ctx context.Context, id idgen.ID) (*Post, error) {
	return get(s.db.WithContext(ctx), id)
}

// Create 新增岗位。
func (s *Service) Create(ctx context.Context, in Input) (*Post, error) {
	db := s.db.WithContext(ctx)
	if taken, err := codeTaken(db, in.Code, 0); err != nil {
		return nil, err
	} else if taken {
		return nil, errDupCode()
	}
	p := &Post{Name: in.Name, Code: in.Code, DeptID: nz(in.DeptID), Sort: in.Sort, Status: statusOr(in.Status), Remark: in.Remark}
	if err := db.Create(p).Error; err != nil {
		return nil, err
	}
	return p, nil
}

// Update 修改岗位。
func (s *Service) Update(ctx context.Context, id idgen.ID, in Input) (*Post, error) {
	db := s.db.WithContext(ctx)
	if _, err := get(db, id); err != nil {
		return nil, err
	}
	if taken, err := codeTaken(db, in.Code, id); err != nil {
		return nil, err
	} else if taken {
		return nil, errDupCode()
	}
	err := db.Model(&Post{}).Where("id = ?", int64(id)).Updates(map[string]any{
		"name": in.Name, "code": in.Code, "dept_id": nz(in.DeptID), "sort": in.Sort,
		"status": statusOr(in.Status), "remark": in.Remark,
	}).Error
	if err != nil {
		return nil, err
	}
	return get(db, id)
}

// Delete 删除岗位。
func (s *Service) Delete(ctx context.Context, id idgen.ID) error {
	db := s.db.WithContext(ctx)
	if _, err := get(db, id); err != nil {
		return err
	}
	// 仍有（未删除的）用户关联该岗位时拒绝；sys_user_post 无租户列，岗位已在上面按租户校验过归属。
	var n int64
	err := db.Table("sys_user_post AS up").
		Joins("JOIN sys_user u ON u.id = up.user_id AND u.deleted_at IS NULL").
		Where("up.post_id = ?", int64(id)).Count(&n).Error
	if err != nil {
		return err
	}
	if n > 0 {
		return response.Conflict("post.in_use", n)
	}
	return db.Where("id = ?", int64(id)).Delete(&Post{}).Error
}

// Sort 调整顺序：ids 必须恰好是当前租户的全部岗位，否则 409。
func (s *Service) Sort(ctx context.Context, ids []idgen.ID) error {
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		return core.Sort(ctx, tx, &Post{}, ids, nil)
	})
}
