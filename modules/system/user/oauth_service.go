package user

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"regexp"
	"strings"

	"go.uber.org/zap"
	"gorm.io/gorm"

	"nexadmin/core"
	"nexadmin/core/auth"
	"nexadmin/core/config"
	"nexadmin/core/event"
	"nexadmin/core/response"
	"nexadmin/core/tenant"
	"nexadmin/modules/internal/oauthflow"
	"nexadmin/pkg/idgen"
	"nexadmin/pkg/oauth"
)

func isDup(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, gorm.ErrDuplicatedKey) {
		return true
	}
	s := strings.ToLower(err.Error())
	return strings.Contains(s, "duplicate") || strings.Contains(s, "unique constraint")
}

// identityUser 按（provider, external_id）找已绑定的本地用户；没有返回 (nil, nil)。
func identityUser(ctx context.Context, db *gorm.DB, provider, externalID string) (*User, error) {
	var row identityRow
	err := db.WithContext(ctx).Where("provider = ? AND external_id = ?", provider, externalID).First(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var u User
	err = db.WithContext(ctx).First(&u, "id = ?", int64(row.UserID)).Error
	if errors.Is(err, gorm.ErrRecordNotFound) { // 用户已被删除：绑定记录作废，按未绑定处理
		return nil, nil
	}
	return &u, err
}

func hasIdentity(ctx context.Context, db *gorm.DB, uid idgen.ID, provider string) (bool, error) {
	var n int64
	err := db.WithContext(ctx).Model(&identityRow{}).Where("user_id = ? AND provider = ?", int64(uid), provider).Count(&n).Error
	return n > 0, err
}

// findMatch 按 match_by 找唯一的本地账号；没有或不唯一（有歧义）都返回 nil —— 宁可让用户走“绑定已有账号”也不猜。
func findMatch(ctx context.Context, db *gorm.DB, by string, id *oauth.Identity) (*User, error) {
	col, val, ok := matchTarget(by, id)
	if !ok {
		return nil, nil
	}
	tx := db.WithContext(ctx)
	switch col {
	case "phone": // 库里可能存了 +86 / 86 前缀的写法
		tx = tx.Where("phone IN ?", []string{val, "+86" + val, "86" + val, "+86 " + val})
	case "email":
		tx = tx.Where("LOWER(email) = ?", val)
	default:
		tx = tx.Where("username = ?", val)
	}
	var us []User
	if err := tx.Limit(2).Find(&us).Error; err != nil {
		return nil, err
	}
	if len(us) != 1 {
		return nil, nil
	}
	return &us[0], nil
}

// linkIdentity 写入绑定记录。同一第三方身份已属于别人、或该用户在此 provider 已有绑定，返回 errOAuthConflict。
func linkIdentity(ctx context.Context, db *gorm.DB, uid idgen.ID, id *oauth.Identity) error {
	row := &identityRow{UserID: uid, Provider: id.Provider, ExternalID: id.ExternalID, ExternalName: clip(id.Name, 128)}
	if err := db.WithContext(ctx).Create(row).Error; err != nil {
		if isDup(err) {
			return errOAuthConflict
		}
		return err
	}
	return nil
}

func clip(s string, n int) string {
	if r := []rune(s); len(r) > n {
		return string(r[:n])
	}
	return s
}

var badNameChars = regexp.MustCompile(`[^A-Za-z0-9_.-]`)

// newUsername 取邮箱前缀（或 provider 名）加随机后缀，保证不与现有用户名冲突。
func newUsername(ctx context.Context, db *gorm.DB, id *oauth.Identity) (string, error) {
	base := badNameChars.ReplaceAllString(strings.SplitN(id.Email, "@", 2)[0], "")
	if len(base) < 2 || id.Email == "" || !id.EmailVerified {
		base = id.Provider
	}
	base = clip(base, 40)
	for i := 0; i < 8; i++ {
		b := make([]byte, 3)
		if _, err := rand.Read(b); err != nil {
			return "", err
		}
		name := base + "_" + hex.EncodeToString(b)
		var n int64
		if err := db.WithContext(ctx).Model(&User{}).Where("username = ?", name).Count(&n).Error; err != nil {
			return "", err
		}
		if n == 0 {
			return name, nil
		}
	}
	return "", errors.New("oauth: cannot allocate username")
}

// autoCreate 为匹配不上的第三方身份建号：无密码（不可密码登录），受租户用户数上限约束。
// 两条通道各有角色来源——快捷注册（register）按 auth.register_roles，与账号密码注册完全一致；
// 登录侧自动建号按 provider 的 default_role（默认 guest）。内置角色永不自动赋予；
// 找不到该角色则不赋角色（最小权限）并记警告。
func autoCreate(ctx context.Context, d *core.Deps, conf config.ProviderConf, id *oauth.Identity, register bool) (*User, error) {
	db := d.DB.WithContext(ctx)
	name, err := newUsername(ctx, db, id)
	if err != nil {
		return nil, err
	}
	u := &User{Username: name, Nickname: firstNonEmpty(clip(id.Name, 64), name), Avatar: clip(id.Avatar, 500), Gender: "0", Status: statusOn}
	if id.EmailVerified {
		u.Email = clip(strings.ToLower(id.Email), 100)
	}
	if p := normalizePhone(id.Phone); p != "" {
		var n int64
		if err := db.Model(&User{}).Where("phone = ?", p).Count(&n).Error; err == nil && n == 0 {
			u.Phone = p
		}
	}
	err = db.Transaction(func(tx *gorm.DB) error {
		var roleIDs []idgen.ID
		if register {
			var err error
			if roleIDs, err = registerRoleIDs(ctx, tx, d.Config.Auth.RegisterRoles); err != nil {
				return err
			}
		} else {
			rid, err := defaultRoleID(ctx, d, conf.DefaultRole)
			if err != nil {
				return err
			}
			if rid != 0 {
				roleIDs = []idgen.ID{rid}
			}
		}
		if err := ensureUserQuota(ctx, tx, 1); err != nil { // 自动建号同样受套餐用户数限制
			return err
		}
		if err := tx.Create(u).Error; err != nil {
			return err
		}
		for _, rid := range roleIDs {
			if err := tx.Create(&userRole{UserID: u.ID, RoleID: rid}).Error; err != nil {
				return err
			}
		}
		return linkIdentity(ctx, tx, u.ID, id)
	})
	if err != nil {
		return nil, err
	}
	if register { // 快捷注册也是注册：与 /auth/register 一样发事件（载荷不含密码）
		event.Publish(ctx, event.UserCreated, userEvent(u))
	}
	return u, nil
}

// defaultRoleID 查当前租户里编码为 code（空 = guest）的启用、非内置角色；找不到返回 0（用户不赋角色，最小权限）并记警告。
func defaultRoleID(ctx context.Context, d *core.Deps, code string) (idgen.ID, error) {
	if code == "" {
		code = "guest"
	}
	var roleID idgen.ID
	err := d.DB.WithContext(ctx).Table("sys_role").Select("id").Where("tenant_id = ? AND code = ? AND status = ? AND builtin = ?", int64(tenantOf(ctx)), code, statusOn, false).Limit(1).Scan(&roleID).Error
	if err != nil {
		return 0, err
	}
	if roleID == 0 {
		d.Log.Warn("auto_create: default role not found or builtin, user created without role", zap.String("role", code))
	}
	return roleID, nil
}

func firstNonEmpty(ss ...string) string {
	for _, s := range ss {
		if s != "" {
			return s
		}
	}
	return ""
}

// resolveLogin 把第三方身份落到本地账号：已绑定 → 登录；首次 → 按 match_by 匹配；再不行建号
// （provider 的 auto_create，或注册页发起的快捷注册）；都不允许才返回 NeedBind + bindToken。
// 任何“不允许”都返回统一的 errOAuthFailed，并带内部原因给日志。
func (m *module) resolveLogin(ctx context.Context, d *core.Deps, flow *oauthflow.Flow, conf config.ProviderConf, id *oauth.Identity, purpose string) (*loginOutcome, string, error) {
	if !domainAllowed(conf.AllowedEmailDomains, id) {
		return nil, "blocked", errOAuthFailed()
	}
	if u, err := identityUser(ctx, d.DB, id.Provider, id.ExternalID); err != nil {
		return nil, "other", err
	} else if u != nil {
		if u.Status != statusOn {
			return nil, "disabled", errOAuthFailed()
		}
		return &loginOutcome{User: u}, "", nil
	}
	if conf.MatchBy != "" {
		u, err := findMatch(ctx, d.DB, conf.MatchBy, id)
		if err != nil {
			return nil, "other", err
		}
		if u != nil {
			if u.Status != statusOn {
				return nil, "disabled", errOAuthFailed()
			}
			// 该账号在此 provider 已绑了另一个第三方身份：不自动覆盖，走手动绑定确认
			if has, err := hasIdentity(ctx, d.DB, u.ID, id.Provider); err != nil {
				return nil, "other", err
			} else if !has {
				if err := linkIdentity(ctx, d.DB, u.ID, id); err != nil {
					return nil, "other", errOAuthFailed()
				}
				return &loginOutcome{User: u}, "", nil
			}
		}
	}
	// 授权期间管理员关掉自助注册：state 里的 register 意图作废，退回登录语义（不建号），
	// 而不是把已经授权完的用户挡在门外。
	register := purpose == oauthflow.PurposeRegister && auth.RegisterEnabled(ctx)
	if conf.AutoCreate || register {
		u, err := autoCreate(ctx, d, conf, id, register)
		if err != nil {
			d.Log.Error("oauth account creation failed", zap.String("provider", id.Provider), zap.Bool("register", register), zap.Error(err))
			var re *response.Error
			if errors.As(err, &re) && re.Reason == tenant.ReasonQuotaUsers { // 套餐用户数已满：给出明确原因
				return nil, "other", err
			}
			return nil, "other", errOAuthFailed()
		}
		return &loginOutcome{User: u}, "", nil
	}
	tok, err := flow.NewBindToken(ctx, id)
	if err != nil {
		return nil, "other", err
	}
	return &loginOutcome{NeedBind: true, BindToken: tok}, "", nil
}
