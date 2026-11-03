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
	// removeMany 一条事务删掉点名的行，返回实际删除条数（越权 / 不存在的 id 删不到，不计数）。
	removeMany(ctx context.Context, tid idgen.ID, ids []idgen.ID) (int64, error)
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

// removeMany 与 remove 同一套显式租户条件，只是把 id 换成点名的集合；整批包在一条事务里，
// 失败就整批回滚，不会留下「选中 5 条删掉 3 条」的中间状态。
func (g gormStore) removeMany(ctx context.Context, tid idgen.ID, ids []idgen.ID) (int64, error) {
	list := make([]int64, 0, len(ids))
	for _, id := range ids {
		list = append(list, int64(id))
	}
	var n int64
	err := g.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		res := tx.Where("tenant_id = ? AND id IN ?", int64(tid), list).Delete(&Source{})
		if res.Error != nil {
			return res.Error
		}
		n = res.RowsAffected
		return nil
	})
	return n, err
}
