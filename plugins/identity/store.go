package identity

import (
	"context"
	"errors"

	"gorm.io/gorm"

	"nexadmin/pkg/idgen"
)

var errNotFound = errors.New("identity: source not found")

// store 是身份源的持久化接口。所有方法都显式带租户条件（不依赖全局租户 scope 作为唯一防线）。
type store interface {
	list(ctx context.Context, tid idgen.ID) ([]Source, error)
	get(ctx context.Context, tid, id idgen.ID) (*Source, error)
	listEnabled(ctx context.Context, tid idgen.ID, typ string) ([]Source, error)
	count(ctx context.Context, tid idgen.ID) (int64, error)
	create(ctx context.Context, s *Source) error
	save(ctx context.Context, s *Source) error
	remove(ctx context.Context, tid, id idgen.ID) error
}

type gormStore struct{ db *gorm.DB }

func (g gormStore) list(ctx context.Context, tid idgen.ID) ([]Source, error) {
	var rows []Source
	err := g.db.WithContext(ctx).Where("tenant_id = ?", tid).Order("created_at ASC, id ASC").Find(&rows).Error
	return rows, err
}

func (g gormStore) get(ctx context.Context, tid, id idgen.ID) (*Source, error) {
	var s Source
	err := g.db.WithContext(ctx).Where("tenant_id = ? AND id = ?", tid, id).First(&s).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, errNotFound
	}
	if err != nil {
		return nil, err
	}
	return &s, nil
}

func (g gormStore) listEnabled(ctx context.Context, tid idgen.ID, typ string) ([]Source, error) {
	var rows []Source
	err := g.db.WithContext(ctx).Where("tenant_id = ? AND type = ? AND enabled = ?", tid, typ, true).
		Order("created_at ASC, id ASC").Find(&rows).Error
	return rows, err
}

func (g gormStore) count(ctx context.Context, tid idgen.ID) (int64, error) {
	var n int64
	err := g.db.WithContext(ctx).Model(&Source{}).Where("tenant_id = ?", tid).Count(&n).Error
	return n, err
}

func (g gormStore) create(ctx context.Context, s *Source) error {
	return g.db.WithContext(ctx).Create(s).Error
}

// save 只更新可变列，并带租户条件。
func (g gormStore) save(ctx context.Context, s *Source) error {
	return g.db.WithContext(ctx).Model(&Source{}).Where("tenant_id = ? AND id = ?", s.TenantID, s.ID).
		Updates(map[string]any{"name": s.Name, "enabled": s.Enabled, "config": s.Config}).Error
}

func (g gormStore) remove(ctx context.Context, tid, id idgen.ID) error {
	return g.db.WithContext(ctx).Where("tenant_id = ? AND id = ?", tid, id).Delete(&Source{}).Error
}
