package user

import (
	"context"

	"gorm.io/gorm"

	"nexadmin/core/auth"
	"nexadmin/core/event"
	"nexadmin/modules/internal/recbin"
	"nexadmin/pkg/idgen"
)

// remove 软删除用户（进回收站的数据保留在表里）。超级管理员和自己不能删。
func (s *service) remove(ctx context.Context, ids []idgen.ID, me actor) error {
	if len(ids) == 0 {
		return nil
	}
	for _, id := range ids {
		if id == me.ID {
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
	err := db.Transaction(func(tx *gorm.DB) error {
		var users []User
		if err := tx.Where("id IN ?", ids).Find(&users).Error; err != nil {
			return err
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
	for _, id := range ids {
		event.Publish(ctx, event.UserDeleted, event.UserData{ID: id, TenantID: tenantOf(ctx)})
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
	if status == statusOff && (u.IsSuper || u.ID == me.ID) {
		return bad("user.super_self_cannot_disable")
	}
	if err := s.db.WithContext(ctx).Model(&User{}).Where("id = ?", int64(id)).Update("status", status).Error; err != nil {
		return err
	}
	u.Status = status
	event.Publish(ctx, event.UserUpdated, userEvent(u))
	return nil
}

// resetPassword 重置密码。不设强度规则，只要求非空；bcrypt 只取前 72 字节，所以上限 72。
func (s *service) resetPassword(ctx context.Context, id idgen.ID, password string, me actor) error {
	if password == "" || len(password) > 72 {
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
	hash, err := auth.HashPassword(password)
	if err != nil {
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
	return clearLock(ctx, s.cache, u.Username)
}
