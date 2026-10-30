package log

import (
	"context"
	"time"

	"gorm.io/gorm"

	"nexadmin/core/datascope"
	"nexadmin/pkg/idgen"
)

// 日志表只记录账号（operator / username），没有用户 id 与部门列，
// 所以数据范围通过账号关联 sys_user 判断归属部门：不改表结构、不需要迁移。
// 限制：用户被删除或改名后，其历史日志不再归属任何部门，对受限操作者不可见（超管/全部范围不受影响）。

const (
	operCol  = "operator"
	loginCol = "username"
)

func scoped(tx *gorm.DB, sc *datascope.Scope, col string) *gorm.DB {
	if cond, args, ok := sc.UsernameCond(col); ok {
		return tx.Where(cond, args...)
	}
	return tx
}

func uniqIDs(ids []idgen.ID) []int64 {
	seen := make(map[idgen.ID]bool, len(ids))
	out := make([]int64, 0, len(ids))
	for _, id := range ids {
		if !seen[id] {
			seen[id] = true
			out = append(out, int64(id))
		}
	}
	return out
}

// checkIDs 批量删除前校验：每个 id 都必须存在于范围内，否则整批拒绝，只报数量。
func (s *Service) checkIDs(ctx context.Context, sc *datascope.Scope, model any, col string, ids []idgen.ID) error {
	if !sc.Limited() {
		return nil
	}
	uniq := uniqIDs(ids)
	var n int64
	if err := scoped(s.db.WithContext(ctx).Model(model).Where("id IN ?", uniq), sc, col).Count(&n).Error; err != nil {
		return err
	}
	if denied := len(uniq) - int(n); denied > 0 {
		return datascope.Forbidden(ctx, "datascope.resource.log", denied)
	}
	return nil
}

// CheckOperIDs / CheckLoginIDs 校验批量删除的 id。
func (s *Service) CheckOperIDs(ctx context.Context, sc *datascope.Scope, ids []idgen.ID) error {
	return s.checkIDs(ctx, sc, &OperLog{}, operCol, ids)
}

func (s *Service) CheckLoginIDs(ctx context.Context, sc *datascope.Scope, ids []idgen.ID) error {
	return s.checkIDs(ctx, sc, &LoginLog{}, loginCol, ids)
}

// CheckCleanOper 校验清理：待清理的日志里只要有范围外的，整批拒绝并报数量。
func (s *Service) CheckCleanOper(ctx context.Context, sc *datascope.Scope, days int) error {
	if !sc.Limited() {
		return nil
	}
	cutoff := cleanCutoff(days)
	cond, args, _ := sc.UsernameCond(operCol)
	var n int64
	if err := s.db.WithContext(ctx).Model(&OperLog{}).Where("created_at < ?", cutoff).Where("NOT ("+cond+")", args...).Count(&n).Error; err != nil {
		return err
	}
	if n > 0 {
		return datascope.Forbidden(ctx, "datascope.resource.log", int(n))
	}
	return nil
}

func cleanCutoff(days int) time.Time {
	if days == 0 {
		return time.Now().UTC().Add(time.Hour) // 包含刚写入的
	}
	return time.Now().UTC().AddDate(0, 0, -days)
}
