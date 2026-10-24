package user

import (
	"context"
	"errors"
	"strings"
	"time"
	"unicode/utf8"

	"go.uber.org/zap"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"nexadmin/core"
	"nexadmin/core/auth"
	"nexadmin/core/event"
	"nexadmin/core/middleware"
	"nexadmin/core/response"
	"nexadmin/core/tenant"
	"nexadmin/pkg/idgen"
	"nexadmin/pkg/oauth"
)

// 本文件把 core/auth 的登录扩展点接到用户模块：外部身份 -> 本地用户（映射 / 自动创建）、扩展点登录日志。

// errExternalDenied 表示外部身份不被允许落到本地账号（用户名不合规、账号停用 / 超管 / 已被占用、未开启匹配与自动创建）。
// 密码登录里按“凭据错误”处理（计失败、对外统一提示），原因只进日志。
var errExternalDenied = response.Forbidden("auth.login_denied")

var (
	_ auth.ExternalUserResolver = (*module)(nil)
	_ auth.LoginRecorder        = (*module)(nil)
)

// RecordLogin 实现 auth.LoginRecorder：扩展点（二次验证、LoginAs、前置检查拒绝）写登录日志。
func (m *module) RecordLogin(ctx context.Context, ev auth.LoginEvent) {
	if d := m.deps.Load(); d != nil {
		writeLoginLog(ctx, d, ev.Username, ev.Method, ev.IP, ev.UserAgent, ev.Reason)
	}
}

// ResolveExternalUser 实现 auth.ExternalUserResolver：已绑定 -> 登录；否则（插件允许时）绑定同名账号或自动创建。
// 仅自动建号时使用映射角色（有匹配时替代默认角色）；已有用户的本地角色永不变更。
// 不提供持续角色同步；组准入检查由外部认证器每次认证执行。
func (m *module) ResolveExternalUser(ctx context.Context, method string, ext auth.ExternalIdentity) (*auth.User, error) {
	d := m.deps.Load()
	tid, tenantOK := tenant.From(ctx)
	if d == nil || !tenantOK || tid == 0 || tenant.Skipped(ctx) {
		return nil, errExternalDenied
	}
	name := strings.TrimSpace(ext.Username)
	// provider 是写入 sys_user_identity 的绑定键；legacy 是升级前无 source id 的旧键（"ldap" / "cas"），
	// 只用于读取存量绑定并在本次登录时迁移，新绑定一律写 provider。
	provider, legacy := method, ""
	if ext.ProviderKey != "" {
		switch {
		case strings.HasPrefix(ext.ProviderKey, directoryProviderPrefix):
			if !validDirectoryIdentity(ext) {
				return nil, errExternalDenied
			}
			method = ext.ProviderKey
			provider = method
			ext.MatchExisting = false
		default:
			old, ok := legacyProviderOf(ext.ProviderKey)
			if !ok || old != method || len(ext.Subject) > 128 {
				return nil, errExternalDenied
			}
			provider, legacy = ext.ProviderKey, old
		}
	}
	if !usernameRe.MatchString(name) {
		d.Log.Warn("external login refused: invalid username", zap.String("method", method))
		return nil, errExternalDenied
	}
	id := &oauth.Identity{Provider: provider, ExternalID: clip(firstNonEmpty(strings.TrimSpace(ext.Subject), name), 128),
		Name: ext.Nickname, Email: ext.Email, EmailVerified: true, Phone: ext.Phone}
	u, err := identityUser(ctx, d.DB, provider, id.ExternalID)
	if err != nil {
		return nil, err
	}
	if u == nil && legacy != "" {
		if u, err = adoptLegacyBinding(ctx, d.DB, tid, legacy, id); err != nil {
			return nil, err
		}
	}
	if u == nil && ext.MatchExisting {
		if u, err = matchByUsername(ctx, d, name, id, legacy); err != nil {
			return nil, err
		}
	}
	if u == nil && ext.AutoCreate {
		if u, err = createExternal(ctx, d, name, ext.DefaultRole, id, ext.MappedRoleCodes...); err != nil {
			return nil, err
		}
	}
	if u == nil || u.Status != statusOn || u.IsSuper || u.TenantID != tid {
		d.Log.Info("external login refused", zap.String("method", method))
		return nil, errExternalDenied
	}
	db := d.DB.WithContext(ctx)
	if err := db.Model(&User{}).Where("id = ?", int64(u.ID)).UpdateColumn("last_login_at", time.Now().UTC()).Error; err != nil {
		d.Log.Warn("update last_login_at failed", zap.Error(err))
	}
	au, err := buildAuthUser(db, u)
	if err != nil {
		return nil, err
	}
	au.LoginMethod = method
	return au, nil
}

// 绑定键前缀：ldap-<id> 目录作用域（稳定 GUID 主体）；ldapu-<id> / cas-<id> 为按源隔离的登录名主体。
const (
	directoryProviderPrefix = "ldap-"
	ldapUserProviderPrefix  = "ldapu-"
	casProviderPrefix       = "cas-"
)

// legacyProviderOf 校验 ldapu-<digits> / cas-<digits>，返回升级前的旧绑定键。
func legacyProviderOf(key string) (string, bool) {
	for prefix, legacy := range map[string]string{ldapUserProviderPrefix: "ldap", casProviderPrefix: "cas"} {
		if rest, ok := strings.CutPrefix(key, prefix); ok && len(key) <= 32 && rest != "" && strings.Trim(rest, "0123456789") == "" {
			return legacy, true
		}
	}
	return "", false
}

// adoptLegacyBinding 找到存量（无 source id）绑定并迁移到按源隔离的键，已绑定用户因此不会突然登不上。
// 旧键没有记录来源，迁移是“先到先得”：之后该绑定只属于迁移它的那个源。
func adoptLegacyBinding(ctx context.Context, db *gorm.DB, tid idgen.ID, legacy string, id *oauth.Identity) (*User, error) {
	u, err := identityUser(ctx, db, legacy, id.ExternalID)
	if err != nil || u == nil {
		return nil, err
	}
	if u.TenantID != tid {
		return nil, errExternalDenied
	}
	err = db.WithContext(ctx).Model(&identityRow{}).
		Where("user_id = ? AND provider = ? AND external_id = ?", int64(u.ID), legacy, id.ExternalID).
		Update("provider", id.Provider).Error
	if err != nil {
		if isDup(err) {
			return nil, errExternalDenied
		}
		return nil, err
	}
	return u, nil
}

func validDirectoryIdentity(ext auth.ExternalIdentity) bool {
	return strings.HasPrefix(ext.ProviderKey, directoryProviderPrefix) &&
		len(ext.ProviderKey) <= 32 && len(ext.ProviderKey) > 5 &&
		strings.Trim(ext.ProviderKey[5:], "0123456789") == "" &&
		len(ext.Subject) > 0 && len(ext.Subject) <= 128 &&
		ext.Subject == strings.TrimSpace(ext.Subject)
}

// SyncExternalUser never adopts an existing username. createExternal includes
// quota checks, initial roles and the unique identity binding in one transaction.
func (m *module) SyncExternalUser(ctx context.Context, ext auth.ExternalIdentity) (bool, error) {
	d := m.deps.Load()
	tid, ok := tenant.From(ctx)
	if d == nil || !ok || tid == 0 || tenant.Skipped(ctx) ||
		!validDirectoryIdentity(ext) || !usernameRe.MatchString(ext.Username) {
		return false, errExternalDenied
	}
	existing, err := identityUser(ctx, d.DB, ext.ProviderKey, ext.Subject)
	if err != nil {
		return false, err
	}
	if existing == nil {
		_, err := createExternal(ctx, d, ext.Username, ext.DefaultRole, &oauth.Identity{
			Provider: ext.ProviderKey, ExternalID: ext.Subject, Name: ext.Nickname,
			Email: ext.Email, Phone: ext.Phone,
		}, ext.MappedRoleCodes...)
		return err == nil, err
	}
	err = d.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		u, err := identityUser(ctx, tx, ext.ProviderKey, ext.Subject)
		if err != nil {
			return err
		}
		if u == nil {
			return errExternalDenied
		}
		if u.TenantID != tid || u.IsSuper {
			return errExternalDenied
		}
		// 后来被本地提升为内置角色的账号不再接受目录资料同步（否则目录侧可改它的邮箱 / 手机）。
		if protected, err := isProtectedUser(ctx, tx, u); err != nil {
			return err
		} else if protected {
			return nil
		}
		// Update only directory profile columns; status, roles, posts, department
		// and credentials are local policy and remain untouched.
		email := strings.ToLower(strings.TrimSpace(ext.Email))
		if email != "" && (len(email) > 100 || !emailRe.MatchString(email)) {
			return errExternalDenied
		}
		phone := normalizePhone(ext.Phone)
		if strings.TrimSpace(ext.Phone) != "" && phone == "" {
			return errExternalDenied
		}
		var n int64
		if err := tx.Model(&User{}).Where("tenant_id = ? AND id <> ? AND (username = ? OR (? <> '' AND phone = ?))",
			tid, u.ID, ext.Username, phone, phone).Count(&n).Error; err != nil {
			return err
		}
		if n != 0 {
			return errExternalDenied
		}
		return tx.Model(&User{}).Where("tenant_id = ? AND id = ? AND is_super = ?", tid, u.ID, false).
			Updates(map[string]any{"username": ext.Username, "nickname": firstNonEmpty(clip(ext.Nickname, 64), ext.Username),
				"email": email, "phone": phone}).Error
	})
	return false, err
}

// DeactivateMissingExternal 停用“由目录源 provider 绑定过、但本次同步已不在目录里”的本地账号。
// keep 是本次同步看到的全部 subject。只动由该目录绑定过的账号（不碰本地账号），跳过超管与内置角色持有者；
// 已停用的不重复处理。防误停：目录返回数不到已绑定数的一半时整体拒绝（目录故障 / 过滤器写错的典型表现）。
func (m *module) DeactivateMissingExternal(ctx context.Context, provider string, keep map[string]bool) (int, error) {
	d := m.deps.Load()
	tid, ok := tenant.From(ctx)
	if d == nil || !ok || tid == 0 || tenant.Skipped(ctx) ||
		!strings.HasPrefix(provider, directoryProviderPrefix) || len(provider) <= len(directoryProviderPrefix) ||
		strings.Trim(provider[len(directoryProviderPrefix):], "0123456789") != "" {
		return 0, errExternalDenied
	}
	var rows []identityRow
	if err := d.DB.WithContext(ctx).Where("tenant_id = ? AND provider = ?", int64(tid), provider).Find(&rows).Error; err != nil {
		return 0, err
	}
	if len(rows) > 0 && len(keep)*2 < len(rows) {
		return 0, errors.New("deactivation aborted: directory returned fewer than half of the bound accounts")
	}
	n := 0
	for _, r := range rows {
		if keep[r.ExternalID] {
			continue
		}
		var u User
		err := d.DB.WithContext(ctx).Where("tenant_id = ? AND id = ?", int64(tid), int64(r.UserID)).First(&u).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			continue
		}
		if err != nil {
			return n, err
		}
		if u.Status != statusOn {
			continue
		}
		if protected, err := isProtectedUser(ctx, d.DB, &u); err != nil {
			return n, err
		} else if protected {
			continue
		}
		if err := d.DB.WithContext(ctx).Model(&User{}).Where("tenant_id = ? AND id = ? AND is_super = ?", int64(tid), int64(u.ID), false).
			Update("status", statusOff).Error; err != nil {
			return n, err
		}
		u.Status = statusOff
		event.Publish(ctx, event.UserUpdated, userEvent(&u))
		n++
	}
	return n, nil
}

// matchByUsername 把外部身份绑到同名的已有账号：超管和持有内置角色的账号永不匹配；该账号在此方式下已有别的绑定也不覆盖。
func matchByUsername(ctx context.Context, d *core.Deps, name string, id *oauth.Identity, legacy string) (*User, error) {
	var u User
	err := d.DB.WithContext(ctx).Where("username = ?", name).First(&u).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if u.Status != statusOn || u.TenantID != tenantOf(ctx) {
		return nil, errExternalDenied
	}
	// 超管与持有内置角色（租户管理员等）的账号永不被外部目录同名绑定：目录管理员不应因此获得本地管理权。
	if protected, err := isProtectedUser(ctx, d.DB, &u); err != nil {
		return nil, err
	} else if protected {
		return nil, errExternalDenied
	}
	for _, p := range []string{id.Provider, legacy} {
		if p == "" {
			continue
		}
		if has, err := hasIdentity(ctx, d.DB, u.ID, p); err != nil {
			return nil, err
		} else if has {
			return nil, errExternalDenied
		}
	}
	if err := linkIdentity(ctx, d.DB, u.ID, id); err != nil {
		if errors.Is(err, errOAuthConflict) {
			return nil, errExternalDenied
		}
		return nil, err
	}
	return &u, nil
}

// createExternal 自动建号：沿用 ensureUserQuota 与事务内建号 + 赋角色 + 绑定；用户名已存在（含并发）按拒绝处理。
func createExternal(ctx context.Context, d *core.Deps, name, role string, id *oauth.Identity, mappedCodes ...string) (*User, error) {
	tid, ok := tenant.From(ctx)
	if !ok || tid == 0 || tenant.Skipped(ctx) {
		return nil, errExternalDenied
	}
	db := d.DB.WithContext(ctx)
	var n int64
	if err := db.Model(&User{}).Where("username = ?", name).Count(&n).Error; err != nil {
		return nil, err
	}
	if n > 0 {
		return nil, errExternalDenied
	}
	u := &User{Username: name, Nickname: firstNonEmpty(clip(strings.TrimSpace(id.Name), 64), name), Gender: "0", Status: statusOn}
	if e := strings.ToLower(strings.TrimSpace(id.Email)); e != "" && len(e) <= 100 && emailRe.MatchString(e) {
		u.Email = e
	}
	if p := normalizePhone(id.Phone); p != "" {
		var c int64
		if err := db.Model(&User{}).Where("phone = ?", p).Count(&c).Error; err == nil && c == 0 {
			u.Phone = p
		}
	}
	err := db.Transaction(func(tx *gorm.DB) error {
		roleIDs, err := externalRoleIDs(ctx, tx, role, mappedCodes)
		if err != nil {
			return err
		}
		if err := ensureUserQuota(ctx, tx, 1); err != nil { // 受套餐用户数限制，和手工创建 / 第三方自动建号同一条路径
			return err
		}
		if err := tx.Create(u).Error; err != nil {
			return err
		}
		for _, roleID := range roleIDs {
			if err := tx.Create(&userRole{UserID: u.ID, RoleID: roleID}).Error; err != nil {
				return err
			}
		}
		return linkIdentity(ctx, tx, u.ID, id)
	})
	if err != nil {
		if isDup(err) || errors.Is(err, errOAuthConflict) {
			return nil, errExternalDenied
		}
		return nil, err
	}
	return u, nil
}

// externalRoleIDs 在建号事务中重新校验并锁定角色。显式配置无效时整体拒绝，
// 只有未配置默认角色且 guest 不存在时保持历史兼容：允许无角色建号。
func externalRoleIDs(ctx context.Context, tx *gorm.DB, defaultCode string, mapped []string) ([]idgen.ID, error) {
	codes := mapped
	optionalGuest := len(codes) == 0 && defaultCode == ""
	if len(codes) == 0 {
		codes = []string{firstNonEmpty(defaultCode, "guest")}
	}
	if len(codes) > 1000 {
		return nil, errExternalDenied
	}
	seen := map[string]bool{}
	var ids []idgen.ID
	for _, code := range codes {
		if code == "" || len(code) > 64 || code != strings.TrimSpace(code) || !utf8.ValidString(code) ||
			strings.EqualFold(code, "super") || strings.ContainsFunc(code, func(r rune) bool { return r < 0x20 || r == 0x7f }) {
			return nil, errExternalDenied
		}
		if seen[code] {
			continue
		}
		seen[code] = true
		var rows []struct {
			ID   idgen.ID
			Code string
		}
		err := tx.Table("sys_role").Clauses(clause.Locking{Strength: "UPDATE"}).
			Select("id, code").
			Where("tenant_id = ? AND code = ? AND status = ? AND builtin = ?", int64(tenantOf(ctx)), code, statusOn, false).
			Limit(2).Find(&rows).Error
		if err != nil {
			return nil, err
		}
		if optionalGuest && len(rows) == 0 {
			continue
		}
		// 防止大小写不敏感数据库排序规则把其他编码当成精确命中。
		if len(rows) != 1 || rows[0].ID == 0 || rows[0].Code != code {
			return nil, errExternalDenied
		}
		ids = append(ids, rows[0].ID)
	}
	return ids, nil
}

// externalLogin 在本地凭据失败后尝试外部校验器；handled=false 表示没有校验器通过，调用方继续原有的失败计数。
// 通过后清除本地失败计数 / 锁定（与本地登录成功一致）。外部身份被拒绝（如账号停用）按未通过处理。
func (m *module) externalLogin(ctx context.Context, d *core.Deps, req auth.LoginRequest, username string) (au *auth.User, handled bool, err error) {
	method, ext := d.Auth.TryExternal(ctx, username, req.Password, func(method string, err error) {
		d.Log.Warn("external authenticator error", zap.String("method", method), zap.Error(err))
	})
	if ext == nil {
		return nil, false, nil
	}
	if strings.TrimSpace(ext.Username) == "" {
		cp := *ext
		cp.Username = username
		ext = &cp
	}
	au, err = m.ResolveExternalUser(ctx, method, *ext)
	if errors.Is(err, errExternalDenied) {
		return nil, false, nil
	}
	if err != nil {
		return nil, true, err
	}
	if err := clearLock(ctx, d.Cache, username); err != nil {
		return nil, true, err
	}
	return au, true, nil
}

// registerLoginVerify 挂载 POST /auth/login/verify（匿名，但必须持有绑定 IP 的一次性挑战；与登录同一档限流）。
func (m *module) registerLoginVerify(d *core.Deps) {
	h := &auth.Handler{Svc: d.Auth, Cfg: d.Config}
	lim := middleware.RateLimitRule(d.Cache, d.Log, d.Config.RateLimit, "login", middleware.ByIPAndPath())
	d.Public.POST("/auth/login/verify", lim, h.LoginVerify)
}
