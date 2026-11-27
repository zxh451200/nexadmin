package user

import (
	"context"

	"gorm.io/gorm"

	"nexadmin/core/auth"
	"nexadmin/core/event"
	"nexadmin/modules/internal/password"
	"nexadmin/modules/internal/recbin"
	"nexadmin/pkg/idgen"
)

// remove 软删除用户（进回收站的数据保留在表里）。超级管理员和自己不能删。
func (s *service) remove(ctx context.Context, ids []idgen.ID, me actor) error {
	if len(ids) == 0 {
		return nil
	}
	for _, id := range ids {
		if me.Principal != auth.PrincipalApp && id == me.ID {
			return bad("user.delete_self_forbidden")
		}
	}
	if err := s.ensureInScope(ctx, me, ids...); err != nil {
		return err
	}
	db := s.db.WithContext(ctx)
	var supers int64
	if err := db.Model(&User{}).Where("id IN ? AND is_super = ?", ids, true).Count(&supers).Error; err != nil {
		return err
	}
	if supers > 0 {
		return bad("user.super_delete_forbidden")
	}
	if !me.Super { // 持有内置超管角色的账号只有超管能删
		var held int64
		if err := db.Table("sys_user_role AS ur").
			Joins("JOIN sys_role r ON r.id = ur.role_id AND r.builtin = ?", true).
			Where("ur.user_id IN ?", ids).Count(&held).Error; err != nil {
			return err
		}
		if held > 0 {
			return forbiddenRole("user.super_operate_forbidden")
		}
	}
	userTenants := make(map[idgen.ID]idgen.ID, len(ids))
	err := db.Transaction(func(tx *gorm.DB) error {
		var users []User
		if err := tx.Where("id IN ?", ids).Find(&users).Error; err != nil {
			return err
		}
		for _, u := range users {
			userTenants[u.ID] = u.TenantID
			if err := s.revokeUser(ctx, u.ID, u.TenantID); err != nil {
				return err
			}
		}
		if err := tx.Where("id IN ?", ids).Delete(&User{}).Error; err != nil {
			return err
		}
		for _, u := range users {
			if err := recbin.Add(ctx, tx, recbin.TypeUser, u.ID, u.Nickname, me.ID.String()); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return err
	}
	// 删除即下线；安全版本保证漏登记的旧令牌在软删还原后也不能复活。
	for _, id := range ids {
		event.Publish(ctx, event.UserDeleted, event.UserData{ID: id, TenantID: userTenants[id]})
	}
	return nil
}

// setStatus 启用/停用用户。超级管理员和自己不能停用。
func (s *service) setStatus(ctx context.Context, id idgen.ID, status string, me actor) error {
	if status != statusOn && status != statusOff {
		return bad("user.status_invalid")
	}
	if err := s.ensureInScope(ctx, me, id); err != nil {
		return err
	}
	u, err := s.find(ctx, id)
	if err != nil {
		return err
	}
	if err := s.guardTarget(ctx, me, u); err != nil {
		return err
	}
	if status == statusOff && (u.IsSuper || (me.Principal != auth.PrincipalApp && u.ID == me.ID)) {
		return bad("user.super_self_cannot_disable")
	}
	if status == statusOff {
		// 先吊销再落库；缓存写入失败时停用直接失败，旧凭据不会继续可用。
		if err := s.revokeUser(ctx, id, u.TenantID); err != nil {
			return err
		}
	}
	if err := s.db.WithContext(ctx).Model(&User{}).Where("id = ?", int64(id)).Update("status", status).Error; err != nil {
		return err
	}
	u.Status = status
	event.Publish(ctx, event.UserUpdated, userEvent(u))
	return nil
}

// batchStatus 批量启用/停用：逐个走 setStatus 的完整护栏（数据范围、超管/自己不可停用）。
// 个体失败（不在范围、停用了自己/超管）不阻断其余，结果如实报 updated/skipped；
// 全部失败时把第一个错误抛回，让调用方看到原因。
func (s *service) batchStatus(ctx context.Context, ids []idgen.ID, status string, me actor) (updated, skipped int, err error) {
	if status != statusOn && status != statusOff {
		return 0, 0, bad("user.status_invalid")
	}
	var firstErr error
	for _, id := range ids {
		if e := s.setStatus(ctx, id, status, me); e != nil {
			skipped++
			if firstErr == nil {
				firstErr = e
			}
			continue
		}
		updated++
	}
	if updated == 0 && firstErr != nil {
		return 0, skipped, firstErr
	}
	return updated, skipped, nil
}

// resetPassword 重置密码：策略校验（最小长度 / 混合要求 / 不得等于初始口令）+ bcrypt 上限 72 字节。
// 成功后吊销目标用户全部会话——旧令牌不能再带着旧凭据继续用（与个人中心改密同口径）。
func (s *service) resetPassword(ctx context.Context, id idgen.ID, newPw string, me actor) error {
	if newPw == "" || len(newPw) > 72 {
		return bad("profile.password_length")
	}
	if err := s.ensureInScope(ctx, me, id); err != nil {
		return err
	}
	u, err := s.find(ctx, id)
	if err != nil {
		return err
	}
	if err := s.guardTarget(ctx, me, u); err != nil {
		return err
	}
	if perr := password.Load(ctx, s.db).Validate(newPw, password.DefaultPassword(ctx, s.db)); perr != nil {
		return perr
	}
	hash, err := auth.HashPassword(newPw)
	if err != nil {
		return err
	}
	if err := s.revokeUser(ctx, id, u.TenantID); err != nil {
		return err
	}
	if err := s.db.WithContext(ctx).Model(&User{}).Where("id = ?", int64(id)).Update("password", hash).Error; err != nil {
		return err
	}
	s.mailAccount(ctx, u, "reset")
	return nil
}

// unlock 解除登录锁定（清掉失败计数与锁定标记）。
func (s *service) unlock(ctx context.Context, id idgen.ID, me actor) error {
	if err := s.ensureInScope(ctx, me, id); err != nil {
		return err
	}
	u, err := s.find(ctx, id)
	if err != nil {
		return err
	}
	if err := s.guardTarget(ctx, me, u); err != nil {
		return err
	}
	return clearLock(ctx, s.db, s.cache, u.Username)
}
