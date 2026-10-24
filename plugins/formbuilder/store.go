package formbuilder

import (
	"context"
	"errors"
	"strings"
	"time"

	"gorm.io/gorm"

	"nexadmin/core/database"
	"nexadmin/core/tenant"
	"nexadmin/pkg/idgen"
)

// Store 是持久化接口。每个实现都必须按当前租户隔离数据；gormStore 在每条语句里显式带 tenant_id，
// 不依赖 core/database 的租户 scope（那是兜底，单测里也无法依赖），测试里的内存实现遵循同样的语义。
type Store interface {
	CreateForm(ctx context.Context, f *Form) error
	GetForm(ctx context.Context, id idgen.ID) (*Form, error)
	ListForms(ctx context.Context, q FormQuery) ([]Form, int64, error)
	ListPublished(ctx context.Context, limit int) ([]Form, error)
	SaveDraft(ctx context.Context, id idgen.ID, name, desc, def string) error
	SaveAudience(ctx context.Context, id idgen.ID, audience string) error
	DeleteForm(ctx context.Context, id idgen.ID) error
	Publish(ctx context.Context, formID idgen.ID, v *Version, meta PublishMeta) error
	SetStatus(ctx context.Context, id idgen.ID, from, to string) error
	GetVersion(ctx context.Context, formID idgen.ID, version int) (*Version, error)
	ListVersions(ctx context.Context, formID idgen.ID) ([]Version, error)
	InsertSubmission(ctx context.Context, s *Submission, maxCount int) error
	FindByDedupe(ctx context.Context, formID idgen.ID, key string) (*Submission, error)
	GetSubmission(ctx context.Context, formID, id idgen.ID) (*Submission, error)
	DeleteSubmission(ctx context.Context, formID, id idgen.ID) error
	CountSubmissions(ctx context.Context, q SubQuery) (int64, error)
	PageSubmissions(ctx context.Context, q SubQuery) ([]Submission, error)
	// ScanSubmissions 按 id 倒序取一批（keyset：id < before；before = 0 表示从最新开始），不分页。
	ScanSubmissions(ctx context.Context, q SubQuery, before idgen.ID, limit int) ([]Submission, error)
	SubmittedForms(ctx context.Context, userID idgen.ID, formIDs []idgen.ID) (map[idgen.ID]bool, error)
}

type gormStore struct{ db *gorm.DB }

// NewStore 返回基于 GORM 的实现。
func NewStore(db *gorm.DB) Store { return &gormStore{db: db} }

// scope 返回已带租户条件的查询链。context 里没有租户时拒绝（宁可报错也不放行）。
func (s *gormStore) scope(ctx context.Context, model any) (*gorm.DB, idgen.ID, error) {
	tid, ok := tenant.From(ctx)
	if !ok || tid == 0 {
		return nil, 0, database.ErrNoTenant
	}
	return s.db.WithContext(ctx).Model(model).Where("tenant_id = ?", int64(tid)), tid, nil
}

func notFound(err error) error {
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return errNotFound
	}
	return err
}

func likeEscape(s string) string {
	return strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(s)
}

func (s *gormStore) CreateForm(ctx context.Context, f *Form) error {
	tid, ok := tenant.From(ctx)
	if !ok || tid == 0 {
		return database.ErrNoTenant
	}
	f.TenantID = tid
	return s.db.WithContext(ctx).Create(f).Error
}

func (s *gormStore) GetForm(ctx context.Context, id idgen.ID) (*Form, error) {
	q, _, err := s.scope(ctx, &Form{})
	if err != nil {
		return nil, err
	}
	var f Form
	if err := q.Where("id = ?", int64(id)).First(&f).Error; err != nil {
		return nil, notFound(err)
	}
	return &f, nil
}

func (s *gormStore) ListForms(ctx context.Context, fq FormQuery) ([]Form, int64, error) {
	q, _, err := s.scope(ctx, &Form{})
	if err != nil {
		return nil, 0, err
	}
	if fq.Keyword != "" {
		q = q.Where("name LIKE ?", "%"+likeEscape(fq.Keyword)+"%")
	}
	if fq.Status != "" {
		q = q.Where("status = ?", fq.Status)
	}
	var total int64
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var list []Form
	err = q.Order("id DESC").Offset((fq.Page - 1) * fq.PageSize).Limit(fq.PageSize).Find(&list).Error
	return list, total, err
}

func (s *gormStore) ListPublished(ctx context.Context, limit int) ([]Form, error) {
	q, _, err := s.scope(ctx, &Form{})
	if err != nil {
		return nil, err
	}
	var list []Form
	err = q.Where("status = ? AND published_version > 0", StatusPublished).Order("id DESC").Limit(limit).Find(&list).Error
	return list, err
}

func (s *gormStore) SaveDraft(ctx context.Context, id idgen.ID, name, desc, def string) error {
	q, _, err := s.scope(ctx, &Form{})
	if err != nil {
		return err
	}
	res := q.Where("id = ?", int64(id)).Updates(map[string]any{
		"name": name, "description": desc, "draft_def": def,
		"draft_rev": gorm.Expr("draft_rev + 1"), "updated_at": time.Now().UTC(),
	})
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return errNotFound
	}
	return nil
}

func (s *gormStore) SaveAudience(ctx context.Context, id idgen.ID, audience string) error {
	q, _, err := s.scope(ctx, &Form{})
	if err != nil {
		return err
	}
	res := q.Where("id = ?", int64(id)).Updates(map[string]any{"audience": audience, "updated_at": time.Now().UTC()})
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return errNotFound
	}
	return nil
}

// DeleteForm 在一个事务里删除表单、全部版本与提交，三条语句都带租户条件。
func (s *gormStore) DeleteForm(ctx context.Context, id idgen.ID) error {
	tid, ok := tenant.From(ctx)
	if !ok || tid == 0 {
		return database.ErrNoTenant
	}
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		for _, m := range []any{&Submission{}, &Version{}} {
			if err := tx.Where("tenant_id = ? AND form_id = ?", int64(tid), int64(id)).Delete(m).Error; err != nil {
				return err
			}
		}
		res := tx.Where("tenant_id = ? AND id = ?", int64(tid), int64(id)).Delete(&Form{})
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected == 0 {
			return errNotFound
		}
		return nil
	})
}
