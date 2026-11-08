package user

import (
	"context"
	"errors"
	"strings"
	"time"

	"go.uber.org/zap"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"nexadmin/core/cache"
	"nexadmin/core/database"
	"nexadmin/core/tenant"
	"nexadmin/pkg/idgen"
)

// loginLock 对应 sys_login_lock：登录锁定的持久层。缓存是快路径，这张表兜住进程重启与
// 多实例（memory 缓存各进程一份）。按 (tenant_id, username) 一行；用户名不存在也照建行，
// 避免靠锁定行为枚举账号。
type loginLock struct {
	database.TenantModel
	Username    string    `gorm:"size:64;not null"`
	LockedUntil time.Time `gorm:"not null"`
}

func (loginLock) TableName() string { return "sys_login_lock" }

// lockScope 与 cache.Key 同口径：context 有租户就按租户，解析不到时落默认租户。
func lockScope(ctx context.Context) idgen.ID {
	if id, ok := tenant.From(ctx); ok {
		return id
	}
	return tenant.DefaultID
}

// persistLock 把锁定幂等写入 DB（同一用户名重复锁定时只刷新截止时间）。
func persistLock(ctx context.Context, db *gorm.DB, username string, until time.Time) error {
	if db == nil {
		return nil
	}
	row := loginLock{Username: strings.ToLower(username), LockedUntil: until.UTC()}
	row.TenantID = lockScope(ctx)
	return db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "tenant_id"}, {Name: "username"}},
		DoUpdates: clause.Assignments(map[string]any{
			"locked_until": row.LockedUntil,
			"updated_at":   time.Now().UTC(),
		}),
	}).Create(&row).Error
}

// loadLock 读未过期的锁定；没有则返回零值。tenant_id 显式带在条件里：
// 管理员端（SkipTenant）下租户插件不加隔离，不能靠它兜底。
func loadLock(ctx context.Context, db *gorm.DB, username string) (time.Time, error) {
	if db == nil {
		return time.Time{}, nil
	}
	var row loginLock
	err := db.WithContext(ctx).
		Where("tenant_id = ? AND username = ? AND locked_until > ?", int64(lockScope(ctx)), strings.ToLower(username), time.Now().UTC()).
		Take(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return time.Time{}, nil
	}
	if err != nil {
		return time.Time{}, err
	}
	return row.LockedUntil.UTC(), nil
}

// dropLock 删除锁定行（登录成功、管理员解锁、重置密码时调用）。
func dropLock(ctx context.Context, db *gorm.DB, username string) error {
	if db == nil {
		return nil
	}
	return db.WithContext(ctx).
		Where("tenant_id = ? AND username = ?", int64(lockScope(ctx)), strings.ToLower(username)).
		Delete(&loginLock{}).Error
}

// sweepExpiredLocks 清掉早已过期的锁定行，避免攻击者用不存在的用户名刷表。
// 每小时最多一次（缓存 SetNX 当闸）；跨租户清理，所以显式跳过租户隔离。
func sweepExpiredLocks(ctx context.Context, db *gorm.DB, c cache.Cache) {
	if db == nil {
		return
	}
	locker, ok := cache.As[cache.Locker](c)
	if !ok {
		return
	}
	fire, err := locker.SetNX(ctx, cache.GlobalKey("login", "lock-sweep"), "1", time.Hour)
	if err != nil || !fire {
		return
	}
	if err := db.WithContext(tenant.Skip(ctx)).
		Where("locked_until < ?", time.Now().Add(-time.Hour).UTC()).
		Delete(&loginLock{}).Error; err != nil {
		zap.L().Warn("login lock sweep failed", zap.Error(err))
	}
}
