package core

import (
	"context"
	"errors"

	"gorm.io/gorm"

	"nexadmin/core/response"
	"nexadmin/pkg/idgen"
)

// Sort 按 ids 的顺序重写带 id/sort 字段的模型。
// scope 用于追加租户、父节点或数据权限条件；传 nil 表示不追加条件。
// ids 必须恰好包含当前 scope 下的全部记录，避免覆盖并发修改。
func Sort(ctx context.Context, db *gorm.DB, model any, ids []idgen.ID, scope func(*gorm.DB) *gorm.DB) error {
	q := db.WithContext(ctx)
	if scope != nil {
		q = scope(q)
	}
	var have []int64
	if err := q.Model(model).Pluck("id", &have).Error; err != nil {
		return err
	}
	if len(ids) != len(have) {
		return response.Conflict("error.conflict").WithMsg("列表已变化，请刷新")
	}
	allowed := make(map[int64]struct{}, len(have))
	for _, id := range have {
		allowed[id] = struct{}{}
	}
	seen := make(map[int64]struct{}, len(ids))
	for _, id := range ids {
		n := int64(id)
		if _, ok := allowed[n]; !ok {
			return response.Conflict("error.conflict").WithMsg("列表已变化，请刷新")
		}
		if _, ok := seen[n]; ok {
			return response.Conflict("error.conflict").WithMsg("列表已变化，请刷新")
		}
		seen[n] = struct{}{}
	}
	if len(seen) != len(have) {
		return response.Conflict("error.conflict").WithMsg("列表已变化，请刷新")
	}
	for i, id := range ids {
		u := q.Model(model).Where("id = ?", int64(id)).Update("sort", (i+1)*10)
		if errors.Is(u.Error, gorm.ErrRecordNotFound) {
			return response.Conflict("error.conflict").WithMsg("列表已变化，请刷新")
		}
		if u.Error != nil {
			return u.Error
		}
	}
	return nil
}
