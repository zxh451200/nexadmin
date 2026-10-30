package user

import (
	"context"
	"errors"
	"time"

	"go.uber.org/zap"

	"nexadmin/core"
	"nexadmin/core/auth"
	"nexadmin/core/response"
	"nexadmin/modules/internal/smscode"
)

// Login 是 auth.Provider 的入口：按 req.Type 分派到账号密码或手机号验证码，并统一记登录日志（含登录方式）。
func (m *module) Login(ctx context.Context, req auth.LoginRequest) (*auth.User, error) {
	d := m.deps.Load()
	if d == nil {
		return nil, response.Unavailable("auth.provider_missing")
	}
	var (
		u         *auth.User
		err       error
		loginType = loginPassword
		name      = req.Username
	)
	if req.Type == loginPhone {
		loginType, name = loginPhone, maskedPhone(req.Phone)
		u, err = m.phoneLogin(ctx, d, req)
	} else {
		u, err = m.passwordLogin(ctx, req)
	}
	if err != nil {
		var re *response.Error
		reason := reasonCredentials
		if errors.As(err, &re) && re.Status == 423 {
			reason = reasonLocked
		} else if !errors.As(err, &re) {
			reason = reasonOther
		}
		writeLoginLog(ctx, d, name, loginType, req.IP, req.UserAgent, reason)
		return nil, err
	}
	if u.LoginMethod != "" { // 外部校验器（ldap 等）通过：登录日志记实际方式
		loginType = u.LoginMethod
	}
	writeLoginLog(ctx, d, u.Username, loginType, req.IP, req.UserAgent, "")
	return u, nil
}

func maskedPhone(p string) string {
	if n := normalizePhone(p); n != "" {
		return n[:3] + "****" + n[7:]
	}
	return "-"
}

// phoneLogin 手机号 + 短信验证码登录。手机号未绑定、账号不存在/停用/锁定、验证码错误或过期，一律返回同一条提示，
// 且都会先消耗一次验证码校验，外部无法据此判断某手机号是否绑定了账号。
func (m *module) phoneLogin(ctx context.Context, d *core.Deps, req auth.LoginRequest) (*auth.User, error) {
	invalid := response.BadRequest("user.phone_code_invalid")
	cfg := d.Config
	sc := smscode.Shared(cfg, d.Cache, d.Log)
	if !phoneEnabled(cfg.Auth.Methods) || !sc.Enabled() {
		return nil, invalid
	}
	phone := normalizePhone(req.Phone)
	if phone == "" || len(req.Code) != 6 {
		return nil, invalid
	}
	if err := sc.Verify(ctx, phone, req.Code); err != nil {
		var re *response.Error
		if errors.As(err, &re) && re.Status == 400 {
			return nil, invalid
		}
		return nil, err
	}
	var us []User
	if err := d.DB.WithContext(ctx).Where("phone IN ?", []string{phone, "+86" + phone, "86" + phone, "+86 " + phone}).Limit(2).Find(&us).Error; err != nil {
		return nil, err
	}
	if len(us) != 1 || us[0].Status != statusOn {
		return nil, invalid
	}
	u := &us[0]
	if until, err := lockedUntil(ctx, d.Cache, u.Username); err != nil {
		return nil, err
	} else if !until.IsZero() {
		d.Log.Info("phone login rejected: account locked", zap.String("username", u.Username))
		return nil, invalid
	}
	db := d.DB.WithContext(ctx)
	if err := db.Model(&User{}).Where("id = ?", int64(u.ID)).UpdateColumn("last_login_at", time.Now().UTC()).Error; err != nil {
		d.Log.Warn("update last_login_at failed", zap.Error(err))
	}
	return buildAuthUser(db, u)
}

func phoneEnabled(methods []string) bool {
	for _, m := range methods {
		if m == loginPhone {
			return true
		}
	}
	return false
}
