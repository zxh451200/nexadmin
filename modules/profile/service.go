package profile

import (
	"context"
	"errors"
	"time"

	"gorm.io/gorm"

	"nexadmin/core/auth"
	"nexadmin/core/config"
	"nexadmin/core/i18n"
	"nexadmin/core/response"
	"nexadmin/pkg/idgen"
)

// Service 是个人中心的业务层，所有方法只操作传入的 uid 自己的数据。
type Service struct {
	db  *gorm.DB
	cfg *config.Config
}

func badReq(msg string) error { return response.BadRequest("error.bad_request").WithMsg(msg) }

func (s *Service) me(ctx context.Context, uid idgen.ID) (*userRow, error) {
	var u userRow
	err := s.db.WithContext(ctx).Where("id = ? AND deleted_at IS NULL", int64(uid)).First(&u).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, response.NotFound("error.not_found")
	}
	return &u, err
}

// Get 读取个人资料。
func (s *Service) Get(ctx context.Context, uid idgen.ID) (*Profile, error) {
	u, err := s.me(ctx, uid)
	if err != nil {
		return nil, err
	}
	return toProfile(u), nil
}

// Update 修改个人资料（昵称、头像、性别、邮箱、地区、简介）。
func (s *Service) Update(ctx context.Context, uid idgen.ID, in ProfileInput) (*Profile, error) {
	if _, err := s.me(ctx, uid); err != nil {
		return nil, err
	}
	if in.Gender == "" {
		in.Gender = "0"
	}
	err := s.db.WithContext(ctx).Model(&userRow{}).Where("id = ?", int64(uid)).Updates(map[string]any{
		"nickname": in.Nickname, "avatar": in.Avatar, "gender": in.Gender, "email": in.Email,
		"province": in.Province, "city": in.City, "district": in.District, "remark": in.Bio,
		"updated_at": time.Now().UTC(),
	}).Error
	if err != nil {
		return nil, err
	}
	return s.Get(ctx, uid)
}

// 不设密码强度规则：新密码只要求非空；bcrypt 只取前 72 字节，所以上限 72 字节（超出会被截断，必须拒绝）。
func validNewPassword(pw string) bool { return pw != "" && len(pw) <= 72 }

// ChangePassword 校验旧密码后改密。旧密码错误与新密码不合规返回 400。
func (s *Service) ChangePassword(ctx context.Context, uid idgen.ID, oldPw, newPw string) error {
	u, err := s.me(ctx, uid)
	if err != nil {
		return err
	}
	if u.Password == "" || !auth.CheckPassword(u.Password, oldPw) {
		return badReq("旧密码不正确")
	}
	if oldPw == newPw {
		return badReq("新密码不能与旧密码相同")
	}
	if !validNewPassword(newPw) {
		return badReq("密码长度为 1-72 字节")
	}
	hash, err := auth.HashPassword(newPw)
	if err != nil {
		return err
	}
	return s.db.WithContext(ctx).Model(&userRow{}).Where("id = ?", int64(uid)).
		Updates(map[string]any{"password": hash, "updated_at": time.Now().UTC()}).Error
}

// providerKeys 是可展示的第三方账号，顺序固定（与 auth.KnownProviders 一致）。
var providerKeys = auth.KnownProviders

// identityModel 映射 sys_user_identity（由 user 模块的迁移建表）。带 TenantID，查询会自动限定当前租户。
type identityModel struct {
	ID           idgen.ID `gorm:"primaryKey;autoIncrement:false"`
	TenantID     idgen.ID
	UserID       idgen.ID
	Provider     string
	ExternalID   string
	ExternalName string
	CreatedAt    time.Time
}

func (identityModel) TableName() string { return "sys_user_identity" }

func (s *Service) identities(ctx context.Context, uid idgen.ID) ([]identityModel, error) {
	var ids []identityModel
	err := s.db.WithContext(ctx).Where("user_id = ?", int64(uid)).Find(&ids).Error
	return ids, err
}

// HasIdentity 当前用户是否已绑定该 provider。
func (s *Service) HasIdentity(ctx context.Context, uid idgen.ID, key string) (bool, error) {
	var n int64
	err := s.db.WithContext(ctx).Model(&identityModel{}).Where("user_id = ? AND provider = ?", int64(uid), key).Count(&n).Error
	return n > 0, err
}

// PhoneBound 该手机号是否绑定了一个启用的账号（供匿名发验证码时决定是否真发短信，不对外暴露结果）。
func (s *Service) PhoneBound(ctx context.Context, phone string) (bool, error) {
	var n int64
	err := s.db.WithContext(ctx).Model(&userRow{}).
		Where("phone IN ? AND status = ? AND deleted_at IS NULL", []string{phone, "+86" + phone, "86" + phone, "+86 " + phone}, "1").Count(&n).Error
	return n > 0, err
}

// Bindings 手机号（打码）与第三方账号绑定情况。只列出已配置且可用的第三方（缺 client_id 等必填项的不列）。
func (s *Service) Bindings(ctx context.Context, uid idgen.ID) (*Bindings, error) {
	u, err := s.me(ctx, uid)
	if err != nil {
		return nil, err
	}
	ids, err := s.identities(ctx, uid)
	if err != nil {
		return nil, err
	}
	bound := map[string]identityModel{}
	for _, r := range ids {
		bound[r.Provider] = r
	}
	out := &Bindings{Phone: maskPhone(u.Phone), Providers: []ProviderBinding{}}
	for _, k := range providerKeys {
		pc, configured := s.cfg.Auth.ProviderConf(k)
		if !configured {
			continue
		}
		name := i18n.T(ctx, "auth.provider."+k)
		if k == "oidc" && pc.Name != "" {
			name = pc.Name
		} else if name == "" || name == "auth.provider."+k {
			name = auth.ProviderDisplay(k)
		}
		p := ProviderBinding{Key: k, Name: name}
		if r, ok := bound[k]; ok {
			t := r.CreatedAt
			p.Bound, p.ExternalName, p.BoundAt = true, r.ExternalName, &t
		}
		out.Providers = append(out.Providers, p)
	}
	return out, nil
}

// SetPhone 写入（或清空）手机号。调用前必须已校验验证码。手机号不能被同租户其他用户占用。
func (s *Service) SetPhone(ctx context.Context, uid idgen.ID, phone string) error {
	if phone != "" {
		var n int64
		err := s.db.WithContext(ctx).Model(&userRow{}).
			Where("phone = ? AND id <> ? AND deleted_at IS NULL", phone, int64(uid)).Count(&n).Error
		if err != nil {
			return err
		}
		if n > 0 {
			return response.Conflict("error.bad_request").WithMsg("该手机号已被其他账号绑定")
		}
	}
	return s.db.WithContext(ctx).Model(&userRow{}).Where("id = ?", int64(uid)).
		Updates(map[string]any{"phone": phone, "updated_at": time.Now().UTC()}).Error
}

// canUnbind 解绑保底：解绑后至少还要剩一种可登录方式 —— 有密码，或还有其他第三方绑定。
func canUnbind(hasPassword bool, otherBindings int) bool { return hasPassword || otherBindings > 0 }

// UnbindProvider 解绑第三方账号。没绑过则幂等返回；解绑后若会失去全部可登录方式（无密码且无其他绑定）则拒绝。
func (s *Service) UnbindProvider(ctx context.Context, uid idgen.ID, key string) error {
	if !auth.KnownProvider(key) {
		return response.NotFound("error.not_found")
	}
	u, err := s.me(ctx, uid)
	if err != nil {
		return err
	}
	ids, err := s.identities(ctx, uid)
	if err != nil {
		return err
	}
	bound, others := false, 0
	for _, r := range ids {
		if r.Provider == key {
			bound = true
		} else {
			others++
		}
	}
	if !bound {
		return nil
	}
	if !canUnbind(u.Password != "", others) {
		return badReq("这是你唯一的登录方式，请先设置密码或绑定其他账号后再解绑")
	}
	return s.db.WithContext(ctx).Where("user_id = ? AND provider = ?", int64(uid), key).Delete(&identityModel{}).Error
}
