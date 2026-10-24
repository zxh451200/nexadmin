package user

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"sync"
	"time"

	"go.uber.org/zap"
	"gorm.io/gorm"

	"nexadmin/core/auth"
	"nexadmin/core/cache"
	"nexadmin/core/response"
	"nexadmin/core/tenant"
	"nexadmin/pkg/idgen"
)

// 登录安全设置：设置页（sys_setting.security）优先，其次 sys_config 参数，最后默认值。
const (
	cfgMaxRetry        = "login.max_retry"    // 连续密码错误几次后锁定
	cfgLockMinutes     = "login.lock_minutes" // 锁定多少分钟
	defaultMaxRetry    = 5
	defaultLockMinutes = 15
)

// ---- 锁定状态：放在 core/cache（按租户前缀），对存在与不存在的用户名一视同仁，避免靠锁定行为枚举账号 ----

func lockKey(ctx context.Context, username string) string {
	return cache.Key(ctx, "login", "lock", strings.ToLower(username))
}

func failKey(ctx context.Context, username string) string {
	return cache.Key(ctx, "login", "fail", strings.ToLower(username))
}

// lockedUntil 返回账号的锁定截止时间；未锁定返回零值。
func lockedUntil(ctx context.Context, c cache.Cache, username string) (time.Time, error) {
	v, err := c.Get(ctx, lockKey(ctx, username))
	if errors.Is(err, cache.ErrNotFound) {
		return time.Time{}, nil
	}
	if err != nil {
		return time.Time{}, err
	}
	until, err := time.Parse(time.RFC3339Nano, v)
	if err != nil {
		// 兼容升级前缓存中的 Unix 秒；损坏的状态不能当作未锁定。
		sec, parseErr := strconv.ParseInt(v, 10, 64)
		if parseErr != nil {
			return time.Time{}, parseErr
		}
		until = time.Unix(sec, 0)
	}
	if until.After(time.Now()) {
		return until, nil
	}
	return time.Time{}, nil
}

// clearLock 清除失败计数与锁定（登录成功、管理员解锁时调用）。
func clearLock(ctx context.Context, c cache.Cache, username string) error {
	return c.Del(ctx, failKey(ctx, username), lockKey(ctx, username))
}

// readConfigInt 读取 sys_config 里的整数参数，缺失、非法或表不存在时返回默认值。
func readConfigInt(db *gorm.DB, key string, def int) int {
	var v string
	if err := db.Table("sys_config").Select("config_value").Where("config_key = ?", key).Limit(1).Scan(&v).Error; err != nil {
		return def
	}
	if n, err := strconv.Atoi(strings.TrimSpace(v)); err == nil && n > 0 {
		return n
	}
	return def
}

// loginPolicy 返回（最大连续错误次数, 锁定时长）。
// 优先读设置页“安全设置”（sys_setting 的 security 分区，管理员在后台改的就是它），
// 其次读 sys_config 的 login.max_retry / login.lock_minutes，都没有则用默认值。
func loginPolicy(db *gorm.DB) (int, time.Duration) {
	maxRetry, lockMin := 0, 0
	var content string
	if err := db.Table("sys_setting").Select("content").Where("section = ?", "security").Limit(1).Scan(&content).Error; err == nil && content != "" {
		var sec struct {
			MaxAttempts json.Number `json:"maxAttempts"`
			LockMinutes json.Number `json:"lockMinutes"`
		}
		dec := json.NewDecoder(strings.NewReader(content))
		dec.UseNumber()
		if dec.Decode(&sec) == nil {
			maxRetry, _ = strconv.Atoi(strings.TrimSpace(sec.MaxAttempts.String()))
			lockMin, _ = strconv.Atoi(strings.TrimSpace(sec.LockMinutes.String()))
		}
	}
	if maxRetry <= 0 {
		maxRetry = readConfigInt(db, cfgMaxRetry, defaultMaxRetry)
	}
	if lockMin <= 0 {
		lockMin = readConfigInt(db, cfgLockMinutes, defaultLockMinutes)
	}
	return maxRetry, time.Duration(lockMin) * time.Minute
}

// recordFailure 记一次密码错误；达到上限则锁定并返回锁定秒数，否则返回 0。
func recordFailure(ctx context.Context, db *gorm.DB, c cache.Cache, username string) (int, error) {
	if until, err := lockedUntil(ctx, c, username); err != nil {
		return 0, err
	} else if !until.IsZero() {
		return lockSeconds(until), nil
	}
	ctr, hasCounter := cache.As[cache.Counter](c)
	locker, hasLocker := cache.As[cache.Locker](c)
	if !hasCounter || !hasLocker {
		return 0, errors.New("login security: cache requires atomic Counter and Locker")
	}
	maxRetry, lockFor := loginPolicy(db)
	n, _, err := ctr.Incr(ctx, failKey(ctx, username), lockFor)
	if err != nil {
		return 0, err
	}
	if n >= int64(maxRetry) {
		until := time.Now().Add(lockFor)
		// 仅首次达到阈值时建立锁定；保留失败计数直到其固定窗口过期，
		// 否则已经通过登录前置检查的并发请求会重新从 1 开始。
		created, err := locker.SetNX(ctx, lockKey(ctx, username), until.UTC().Format(time.RFC3339Nano), time.Until(until))
		if err != nil {
			return 0, err
		}
		if created {
			return lockSeconds(until), nil
		}
		// 返回首次锁定的剩余时间，不让后续失败延长锁定。
		until, err = lockedUntil(ctx, c, username)
		if err != nil {
			return 0, err
		}
		if until.IsZero() {
			return 0, errors.New("login security: lock expired or cleared while recording failure")
		}
		return lockSeconds(until), nil
	}
	return 0, nil
}

func lockSeconds(until time.Time) int {
	remaining := time.Until(until)
	if remaining <= 0 {
		return 1 // 本次请求已经触发锁定，不能返回“未锁定”。
	}
	return int((remaining + time.Second - 1) / time.Second)
}

// ---- auth.Provider ----

var (
	dummyOnce sync.Once
	dummyHash string
)

// burn 在用户不存在时也做一次 bcrypt 比较，使响应耗时与“密码错误”接近，防止靠耗时枚举账号。
func burn(pw string) {
	dummyOnce.Do(func() { dummyHash, _ = auth.HashPassword("nexadmin-dummy-password") })
	auth.CheckPassword(dummyHash, pw)
}

// passwordLogin 校验账号密码（含锁定），返回用户及其角色、权限。真实失败原因只写日志，对外统一“用户名或密码错误”。
func (m *module) passwordLogin(ctx context.Context, req auth.LoginRequest) (*auth.User, error) {
	d := m.deps.Load()
	if d == nil {
		return nil, response.Unavailable("auth.provider_missing")
	}
	invalid := response.BadRequest("auth.invalid_credentials")
	username := strings.TrimSpace(req.Username)
	if (req.Type != "" && req.Type != "password") || username == "" || req.Password == "" {
		return nil, invalid // 手机号登录依赖短信服务，本模块只处理账号密码
	}
	db := d.DB.WithContext(ctx)

	until, err := lockedUntil(ctx, d.Cache, username)
	if err != nil {
		return nil, err
	}
	if !until.IsZero() {
		return nil, response.Locked(lockSeconds(until))
	}

	// username 的唯一约束是 (tenant_id, username)，跨租户允许同名；登录按密码在候选中消歧，
	// 谁匹配就登录谁（停用的候选跳过）。候选为空或全部不匹配走与单用户相同的失败路径。
	var cands []User
	err = db.Where("username = ?", username).Order("id ASC").Find(&cands).Error
	if err != nil {
		return nil, err
	}
	var u *User
	for i := range cands {
		if !auth.CheckPassword(cands[i].Password, req.Password) {
			continue
		}
		if cands[i].Status != statusOn {
			d.Log.Info("login rejected: user disabled", zap.String("username", username), zap.String("ip", req.IP))
			continue
		}
		u = &cands[i]
		break
	}
	if u == nil && len(cands) == 0 {
		burn(req.Password) // 没有任何候选时补一次 bcrypt 比较，保持与“密码错误”耗时接近
	}
	found := u != nil
	if !found {
		// 本地账号密码失败（含用户不存在 / 停用）：按序尝试外部校验器（LDAP 等）。未注册时 handled 恒为 false，行为不变。
		// 存在与不存在的用户名走完全相同的路径，失败计数与锁定逻辑不变。
		if au, handled, err := m.externalLogin(ctx, d, req, username); handled {
			return au, err
		}
		secs, err := recordFailure(ctx, db, d.Cache, username)
		if err != nil {
			return nil, err
		}
		if secs > 0 {
			d.Log.Warn("account locked after repeated failures", zap.String("username", username), zap.String("ip", req.IP))
			return nil, response.Locked(secs)
		}
		return nil, invalid
	}

	if err := clearLock(ctx, d.Cache, username); err != nil {
		return nil, err
	}
	if err := db.Model(&User{}).Where("id = ?", int64(u.ID)).UpdateColumn("last_login_at", time.Now().UTC()).Error; err != nil {
		d.Log.Warn("update last_login_at failed", zap.Error(err))
	}
	return buildAuthUser(db, u)
}

// VerifyPassword is reusable by sensitive action confirmations.
func (m *module) VerifyPassword(ctx context.Context, uid idgen.ID, password string) error {
	d := m.deps.Load()
	if d == nil {
		return response.Unavailable("auth.provider_missing")
	}
	db := d.DB.WithContext(ctx)
	var u User
	if err := db.First(&u, "id = ?", int64(uid)).Error; err != nil {
		return response.Unauthorized("auth.invalid_credentials")
	}
	until, err := lockedUntil(ctx, d.Cache, u.Username)
	if err != nil {
		return err
	}
	if !until.IsZero() {
		return response.Locked(lockSeconds(until))
	}
	if password == "" || len(password) > 72 || u.Password == "" ||
		u.Status != statusOn || !auth.CheckPassword(u.Password, password) {
		seconds, err := recordFailure(ctx, db, d.Cache, u.Username)
		if err != nil {
			return err
		}
		if seconds > 0 {
			return response.Locked(seconds)
		}
		return response.Unauthorized("auth.invalid_credentials")
	}
	return clearLock(ctx, d.Cache, u.Username)
}

// Profile 按用户 ID 读取最新信息与权限；用户不存在、已删除或已停用都按未登录处理，令牌随之失效。
func (m *module) Profile(ctx context.Context, userID idgen.ID) (*auth.User, error) {
	d := m.deps.Load()
	if d == nil {
		return nil, response.Unavailable("auth.provider_missing")
	}
	db := d.DB.WithContext(ctx)
	var u User
	if err := db.First(&u, "id = ?", int64(userID)).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, response.Unauthorized("error.unauthorized")
		}
		return nil, err
	}
	if u.Status != statusOn {
		return nil, response.Unauthorized("error.unauthorized")
	}
	return buildAuthUser(db, &u)
}

// buildAuthUser 组装角色编码与权限码。超管或持有内置超管角色 -> "*"；
// 其余用户 = 所属启用角色 -> 已分配的启用菜单/按钮上的权限码。
func buildAuthUser(db *gorm.DB, u *User) (*auth.User, error) {
	var roles []struct {
		Code    string
		Builtin bool
	}
	err := db.Table("sys_user_role AS ur").Select("r.code AS code, r.builtin AS builtin").
		Joins("JOIN sys_role r ON r.id = ur.role_id").
		Where("ur.user_id = ? AND r.status = ?", int64(u.ID), statusOn).Order("r.sort ASC, r.id ASC").Scan(&roles).Error
	if err != nil {
		return nil, err
	}
	out := &auth.User{
		ID: u.ID, Username: u.Username, Nickname: u.Nickname, Avatar: u.Avatar, TenantID: u.TenantID,
		Roles: []string{}, Perms: []string{}, Super: u.IsSuper,
	}
	all := u.IsSuper
	for _, r := range roles {
		out.Roles = append(out.Roles, r.Code)
		all = all || r.Builtin
	}
	if all {
		out.Perms = []string{"*"}
		return restrictByPackage(db, u, out)
	}
	err = db.Table("sys_user_role AS ur").Select("DISTINCT m.perm").
		Joins("JOIN sys_role r ON r.id = ur.role_id AND r.status = ?", statusOn).
		Joins("JOIN sys_role_menu rm ON rm.role_id = r.id").
		Joins("JOIN sys_menu m ON m.id = rm.menu_id AND m.status = ?", statusOn).
		Where("ur.user_id = ? AND m.perm <> ''", int64(u.ID)).Scan(&out.Perms).Error
	if out.Perms == nil {
		out.Perms = []string{}
	}
	if err != nil {
		return out, err
	}
	return restrictByPackage(db, u, out)
}

// restrictByPackage 把租户用户的权限收窄为“角色权限 ∩ 套餐菜单”。
// 边界：超管不受限；租户不受管（如默认租户）或套餐没勾选菜单时不限制；
// 平台级权限（工作台/个人中心/关于/消息/导出中心）和无权限码的菜单不受套餐限制；
// 持有 "*" 的内置管理员角色会被展开为套餐允许的全部权限码。
// /menus/mine 按用户权限码过滤菜单，因此自动得到同样的交集。
func restrictByPackage(db *gorm.DB, u *User, out *auth.User) (*auth.User, error) {
	if u.IsSuper || u.TenantID == 0 {
		return out, nil
	}
	ctx := db.Statement.Context
	if ctx == nil {
		ctx = context.Background()
	}
	info, err := tenant.Lookup(ctx, u.TenantID)
	if err != nil {
		return nil, err
	}
	if info == nil || len(info.MenuIDs) == 0 {
		return out, nil
	}
	var rows []struct {
		ID       idgen.ID
		ParentID idgen.ID
		Type     string
		Perm     string
	}
	if err := db.Session(&gorm.Session{NewDB: true}).WithContext(ctx).Table("sys_menu").
		Select("id, parent_id, type, perm").Where("status = ?", statusOn).Scan(&rows).Error; err != nil {
		return nil, err
	}
	nodes := make([]tenant.MenuNode, len(rows))
	for i, r := range rows {
		nodes[i] = tenant.MenuNode{ID: r.ID, ParentID: r.ParentID, Type: r.Type, Perm: r.Perm}
	}
	out.Perms = tenant.IntersectPerms(out.Perms, tenant.AllowedPerms(info.MenuIDs, nodes))
	return out, nil
}
