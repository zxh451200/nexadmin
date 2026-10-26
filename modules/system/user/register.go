package user

import (
	"context"
	"regexp"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"nexadmin/core"
	"nexadmin/core/auth"
	"nexadmin/core/event"
	"nexadmin/core/middleware"
	"nexadmin/core/response"
	"nexadmin/core/tenant"
	"nexadmin/pkg/idgen"
)

// 自助注册：POST /auth/register（匿名）。运行时开关是设置页「参数设置」的 user.register_enabled
// （默认关闭，经 core/auth.SetRegisterEnabledSource 注入，后台改完即刻生效；行缺失回退 YAML
// auth.allow_register）。默认角色 auth.register_roles（角色码，在当前租户内解析）。
//
// 防滥用与边界（与框架既有约定同一条路径）：
//   - 图形验证码强制校验（与登录同款：绑定签发 IP、一次性，d.Auth.VerifyCaptcha）；
//   - IP 限流（register_route：每小时 10 次）；
//   - 建号在当前解析到的租户（无域名绑定时为默认租户），受该租户套餐用户数上限约束
//     （ensureUserQuota，与手工创建 / 第三方自动建号同一条路径）；
//   - 用户名规则与向导超管一致（字母开头，3-32 位字母/数字/下划线），租户内唯一（含并发冲突兜底）；
//   - 注册成功发布 user.created 事件（载荷不含密码）。

var registerNameRe = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_]{2,31}$`)

// registerSelf 注册自助注册接口。路由常驻注册，请求时查运行时开关：
// 未开放时与从前"不注册路由"的表现一致（404），/auth/methods 的 registerEnabled 同源同步。
func (m *module) registerSelf(d *core.Deps) {
	byIP := middleware.RateLimit(d.Cache, d.Log, "register_route", middleware.ByIPAndPath(), 10, time.Hour)
	d.Public.POST("/auth/register", byIP, func(c *gin.Context) {
		if !auth.RegisterEnabled(c.Request.Context()) {
			response.Fail(c, response.NotFound("error.not_found"))
			return
		}
		var body struct {
			Username  string `json:"username"`
			Password  string `json:"password"`
			CaptchaID string `json:"captchaId"`
			Captcha   string `json:"captcha"`
		}
		if !response.BindJSON(c, &body) {
			return
		}
		out, err := selfRegister(c.Request.Context(), d, body.Username, body.Password, body.CaptchaID, body.Captcha, c.ClientIP(), c.Request.UserAgent())
		if err != nil {
			response.Fail(c, err)
			return
		}
		// 注册即登录：响应与 /auth/login 完全同形（令牌，或部署了二次验证门时的 {challenge}）。
		response.OK(c, out.Payload())
	})
}

func selfRegister(ctx context.Context, d *core.Deps, username, password, captchaID, captchaAnswer, ip, ua string) (*auth.LoginOutcome, error) {
	// 验证码先校验：无论后续成败都已被一次性消费，脚本无法反复试。
	ok, err := d.Auth.VerifyCaptcha(ctx, captchaID, captchaAnswer, ip)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, bad("验证码错误或已过期")
	}
	username = strings.TrimSpace(username)
	if !registerNameRe.MatchString(username) {
		return nil, bad("账号需以字母开头，3-32 位，仅限字母、数字、下划线")
	}
	if password == "" || len(password) > 72 { // 不设强度规则；bcrypt 只取前 72 字节，故限 72
		return nil, bad("密码长度为 1-72 字节")
	}

	// 注册进当前请求解析到的租户；Public 组没解析到（理论不会）则按默认租户。
	if tid, ok := tenant.From(ctx); !ok || tid == 0 || tenant.Skipped(ctx) {
		ctx = tenant.With(ctx, tenant.DefaultID)
	}
	db := d.DB.WithContext(ctx)

	var n int64
	if err := db.Model(&User{}).Where("username = ?", username).Count(&n).Error; err != nil {
		return nil, err
	}
	if n > 0 {
		return nil, bad("账号已存在")
	}

	hash, err := auth.HashPassword(password)
	if err != nil {
		return nil, err
	}
	u := &User{Username: username, Nickname: username, Password: hash, Gender: "0", Status: statusOn, Remark: "自助注册"}
	err = db.Transaction(func(tx *gorm.DB) error {
		roleIDs, err := registerRoleIDs(ctx, tx, d.Config.Auth.RegisterRoles)
		if err != nil {
			return err
		}
		if err := ensureUserQuota(ctx, tx, 1); err != nil {
			return err
		}
		if err := tx.Create(u).Error; err != nil {
			if isDup(err) { // 并发注册同名：唯一索引兜底
				return bad("账号已存在")
			}
			return err
		}
		for _, rid := range roleIDs {
			if err := tx.Create(&userRole{UserID: u.ID, RoleID: rid}).Error; err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	event.Publish(ctx, event.UserCreated, userEvent(u))
	// 注册即登录：加载角色权限后走与密码登录相同的签发出口（租户校验 + 二次验证门 + 令牌）。
	au, err := buildAuthUser(db, u)
	if err != nil {
		return nil, err
	}
	return d.Auth.FinishLogin(ctx, au, auth.LoginMeta{Method: "password", IP: ip, UserAgent: ua})
}

// registerRoleIDs 在建号事务内按角色码解析默认角色（限当前租户，防跨租户挂角色）。
// 配置了角色码但任一不存在时整体拒绝——配置错误不该静默降权放人进来；
// 未配置则不挂角色：注册后为“待管理员分配权限”状态（能登录，侧边栏为空）。
func registerRoleIDs(ctx context.Context, tx *gorm.DB, codes []string) ([]idgen.ID, error) {
	cleaned := make([]string, 0, len(codes))
	for _, c := range codes {
		if c = strings.TrimSpace(c); c != "" {
			cleaned = append(cleaned, c)
		}
	}
	if len(cleaned) == 0 {
		return nil, nil
	}
	var ids []idgen.ID
	if err := tx.Table("sys_role").Select("id").
		Where("tenant_id = ? AND code IN ?", int64(tenantOf(ctx)), cleaned).Scan(&ids).Error; err != nil {
		return nil, err
	}
	if len(ids) != len(cleaned) {
		return nil, bad("注册默认角色配置无效，请联系管理员")
	}
	return ids, nil
}
