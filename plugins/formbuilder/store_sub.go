package formbuilder

import (
	"context"
	"time"

	"gorm.io/gorm"

	"nexadmin/core/database"
	"nexadmin/core/tenant"
	"nexadmin/pkg/idgen"
)

// Publish 原子地发布一个新版本：用"published_version = 期望的上一版本"做乐观并发控制，
// 两个人同时发布时只有一个 RowsAffected == 1；版本表另有 (tenant, form, version) 唯一索引兜底。
// 版本行只会被插入，永远不会被更新。
func (s *gormStore) Publish(ctx context.Context, formID idgen.ID, v *Version, meta PublishMeta) error {
	tid, ok := tenant.From(ctx)
	if !ok || tid == 0 {
		return database.ErrNoTenant
	}
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		now := time.Now().UTC()
		res := tx.Model(&Form{}).Where("tenant_id = ? AND id = ? AND published_version = ?", int64(tid), int64(formID), v.Version-1).
			Updates(map[string]any{
				"status": StatusPublished, "published_version": v.Version, "published_rev": meta.Rev, "published_at": now,
				"name": meta.Name, "description": meta.Description, "allow_repeat": meta.AllowRepeat,
				"deadline_at": meta.DeadlineAt, "max_submissions": meta.MaxSubmissions, "updated_at": now,
			})
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected == 0 {
			return errStoreConflict
		}
		v.TenantID, v.FormID = tid, formID
		if err := tx.Create(v).Error; err != nil {
			if isDuplicateKey(err) {
				return errStoreConflict
			}
			return err
		}
		return nil
	})
}

func (s *gormStore) SetStatus(ctx context.Context, id idgen.ID, from, to string) error {
	q, _, err := s.scope(ctx, &Form{})
	if err != nil {
		return err
	}
	res := q.Where("id = ? AND status = ? AND published_version > 0", int64(id), from).
		Updates(map[string]any{"status": to, "updated_at": time.Now().UTC()})
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return errStoreConflict
	}
	return nil
}

func (s *gormStore) GetVersion(ctx context.Context, formID idgen.ID, version int) (*Version, error) {
	q, _, err := s.scope(ctx, &Version{})
	if err != nil {
		return nil, err
	}
	var v Version
	if err := q.Where("form_id = ? AND version = ?", int64(formID), version).First(&v).Error; err != nil {
		return nil, notFound(err)
	}
	return &v, nil
}

func (s *gormStore) ListVersions(ctx context.Context, formID idgen.ID) ([]Version, error) {
	q, _, err := s.scope(ctx, &Version{})
	if err != nil {
		return nil, err
	}
	var list []Version
	err = q.Where("form_id = ?", int64(formID)).Order("version ASC").Limit(MaxVersions).Find(&list).Error
	return list, err
}

// InsertSubmission 在一个事务里"占名额 + 写提交"：
//
//	UPDATE form SET submission_count = submission_count + 1
//	 WHERE id = ? AND tenant_id = ? AND status = 'published' AND (? <= 0 OR submission_count < ?)
//
// 单条 UPDATE 在三种引擎上都是原子的，并发提交不会超过上限；随后插入提交行，
// 唯一索引 (tenant, form, dedupe_key) 保证防重复/幂等，冲突时整个事务回滚（名额一并归还）。
func (s *gormStore) InsertSubmission(ctx context.Context, sub *Submission, maxCount int) error {
	tid, ok := tenant.From(ctx)
	if !ok || tid == 0 {
		return database.ErrNoTenant
	}
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		res := tx.Model(&Form{}).
			Where("tenant_id = ? AND id = ? AND status = ? AND (? <= 0 OR submission_count < ?)", int64(tid), int64(sub.FormID), StatusPublished, maxCount, maxCount).
			UpdateColumn("submission_count", gorm.Expr("submission_count + 1"))
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected == 0 {
			var n int64
			if err := tx.Model(&Form{}).Where("tenant_id = ? AND id = ? AND status = ?", int64(tid), int64(sub.FormID), StatusPublished).Count(&n).Error; err != nil {
				return err
			}
			if n == 0 {
				return errStoreNotPublished
			}
			return errStoreFull
		}
		sub.TenantID = tid
		if err := tx.Create(sub).Error; err != nil {
			if isDuplicateKey(err) {
				return errStoreDuplicate
			}
			return err
		}
		return nil
	})
	return err
}

func (s *gormStore) FindByDedupe(ctx context.Context, formID idgen.ID, key string) (*Submission, error) {
	q, _, err := s.scope(ctx, &Submission{})
	if err != nil {
		return nil, err
	}
	var sub Submission
	if err := q.Where("form_id = ? AND dedupe_key = ?", int64(formID), key).First(&sub).Error; err != nil {
		return nil, notFound(err)
	}
	return &sub, nil
}

func (s *gormStore) GetSubmission(ctx context.Context, formID, id idgen.ID) (*Submission, error) {
	q, _, err := s.scope(ctx, &Submission{})
	if err != nil {
		return nil, err
	}
	var sub Submission
	if err := q.Where("form_id = ? AND id = ?", int64(formID), int64(id)).First(&sub).Error; err != nil {
		return nil, notFound(err)
	}
	return &sub, nil
}

// DeleteSubmission 删除一条提交并归还名额（submission_count 表示"当前保留的提交数"）。
func (s *gormStore) DeleteSubmission(ctx context.Context, formID, id idgen.ID) error {
	tid, ok := tenant.From(ctx)
	if !ok || tid == 0 {
		return database.ErrNoTenant
	}
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		res := tx.Where("tenant_id = ? AND form_id = ? AND id = ?", int64(tid), int64(formID), int64(id)).Delete(&Submission{})
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected == 0 {
			return errNotFound
		}
		return tx.Model(&Form{}).Where("tenant_id = ? AND id = ? AND submission_count > 0", int64(tid), int64(formID)).
			UpdateColumn("submission_count", gorm.Expr("submission_count - 1")).Error
	})
}

func (s *gormStore) subScope(ctx context.Context, q SubQuery) (*gorm.DB, error) {
	db, _, err := s.scope(ctx, &Submission{})
	if err != nil {
		return nil, err
	}
	db = db.Where("form_id = ?", int64(q.FormID))
	if q.Version > 0 {
		db = db.Where("version = ?", q.Version)
	}
	if q.Username != "" {
		db = db.Where("username LIKE ?", "%"+likeEscape(q.Username)+"%")
	}
	if q.From != nil {
		db = db.Where("created_at >= ?", *q.From)
	}
	if q.To != nil {
		db = db.Where("created_at <= ?", *q.To)
	}
	return db, nil
}

func (s *gormStore) CountSubmissions(ctx context.Context, q SubQuery) (int64, error) {
	db, err := s.subScope(ctx, q)
	if err != nil {
		return 0, err
	}
	var n int64
	return n, db.Count(&n).Error
}

func (s *gormStore) PageSubmissions(ctx context.Context, q SubQuery) ([]Submission, error) {
	db, err := s.subScope(ctx, q)
	if err != nil {
		return nil, err
	}
	var list []Submission
	err = db.Order("id DESC").Offset((q.Page - 1) * q.PageSize).Limit(q.PageSize).Find(&list).Error
	return list, err
}

func (s *gormStore) ScanSubmissions(ctx context.Context, q SubQuery, before idgen.ID, limit int) ([]Submission, error) {
	db, err := s.subScope(ctx, q)
	if err != nil {
		return nil, err
	}
	if before > 0 {
		db = db.Where("id < ?", int64(before))
	}
	var list []Submission
	err = db.Order("id DESC").Limit(limit).Find(&list).Error
	return list, err
}

func (s *gormStore) SubmittedForms(ctx context.Context, userID idgen.ID, formIDs []idgen.ID) (map[idgen.ID]bool, error) {
	out := map[idgen.ID]bool{}
	if len(formIDs) == 0 {
		return out, nil
	}
	q, _, err := s.scope(ctx, &Submission{})
	if err != nil {
		return nil, err
	}
	var ids []int64
	if err := q.Where("user_id = ? AND form_id IN ?", int64(userID), formIDs).Distinct().Pluck("form_id", &ids).Error; err != nil {
		return nil, err
	}
	for _, id := range ids {
		out[idgen.ID(id)] = true
	}
	return out, nil
}
