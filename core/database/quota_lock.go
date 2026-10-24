package database

import (
	"context"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"nexadmin/core/tenant"
)

// LockTenantRow 在事务 tx 内对 ctx 所在租户的 sys_tenant 行执行 SELECT ... FOR UPDATE。
//
// 用途：套餐额度（用户数 / 角色数）是“先数后插”——两个并发事务各自数到 max-1 再各插一条就会超额。
// 先对租户行加锁，同一租户的额度检查 + 插入被串行化，第二个事务要等第一个提交后再数，就能看到最新数量。
// 锁随事务提交/回滚释放；必须在 tx 事务里调用并先于计数。
//
// 租户不在 ctx 里（平台级操作）时不做任何事。调用方应先判断该租户有无额度限制（0 = 不限），
// 无限制就不要调用，避免无谓的加锁与往返。
func LockTenantRow(ctx context.Context, tx *gorm.DB) error {
	id, ok := tenant.From(ctx)
	if !ok || tenant.Skipped(ctx) {
		return nil
	}
	var locked int64
	return tx.WithContext(ctx).Table("sys_tenant").Clauses(clause.Locking{Strength: "UPDATE"}).
		Select("id").Where("id = ?", int64(id)).Limit(1).Scan(&locked).Error
}
