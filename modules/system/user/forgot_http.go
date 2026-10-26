package user

import (
	"context"
	"crypto/subtle"
	"errors"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"

	"nexadmin/core"
	"nexadmin/core/auth"
	"nexadmin/core/cache"
	"nexadmin/core/middleware"
	"nexadmin/core/notify"
	"nexadmin/core/response"
	"nexadmin/modules/internal/session"
	"nexadmin/modules/internal/smscode"
	"nexadmin/pkg/idgen"
	"nexadmin/pkg/mail"
)

// verify 校验验证码：成功即作废；累计输错 forgotMaxTries 次后作废。没发过 / 过期 / 输错 / 已作废的提示完全相同。
// 每次尝试先原子 Incr 该验证码的尝试计数再比对，所以并发猜测最多只有前 forgotMaxTries 次会被比对；
// 作废用 Del，成功用 GetDel 一次性消费。
func (f *forgotService) verify(ctx context.Context, email, code string) error {
	key := codeKey(ctx, email)
	raw, err := f.cache.Get(ctx, key)
	if errors.Is(err, cache.ErrNotFound) {
		return badCode()
	}
	if err != nil {
		return err
	}
	parts := strings.Split(raw, "|")
	if len(parts) != 4 {
		return badCode()
	}
	exp, _ := strconv.ParseInt(parts[3], 10, 64)
	remain := time.Unix(exp, 0).Sub(f.now())
	tk := triesKey(ctx, email, parts[0])
	if remain <= 0 {
		_ = f.cache.Del(ctx, key) // 计数 key 不删：留给 TTL，避免已通过读取的并发尝试把计数重置为 0
		return badCode()
	}
	n, err := cache.Incr(f.cache, ctx, tk, remain)
	if err != nil {
		return err
	}
	if n > forgotMaxTries {
		_ = f.cache.Del(ctx, key) // 计数 key 不删：留给 TTL，避免已通过读取的并发尝试把计数重置为 0
		return badCode()
	}
	if subtle.ConstantTimeCompare([]byte(parts[1]), []byte(digest(parts[0], email, code))) == 1 {
		// 原子消费：多实例下两个并发的正确校验只有一个成功，另一个按“已作废”处理。
		if _, err := cache.GetDel(f.cache, ctx, key); err != nil {
			if errors.Is(err, cache.ErrNotFound) {
				return badCode()
			}
			return err
		}
		return nil
	}
	if n >= forgotMaxTries {
		_ = f.cache.Del(ctx, key) // 计数 key 不删：留给 TTL，避免已通过读取的并发尝试把计数重置为 0
	}
	return badCode()
}

// reset 校验验证码并设置新密码。target 含 @ 走邮件，否则视为手机号走短信。
func (f *forgotService) reset(ctx context.Context, rawTarget, code, newPassword, ip, lang string) error {
	if strings.Contains(rawTarget, "@") {
		return f.resetEmail(ctx, rawTarget, code, newPassword, ip, lang)
	}
	return f.resetPhone(ctx, rawTarget, code, newPassword, ip, lang)
}

// alertReset 重置成功后的安全告警：有邮箱就发（尽力而为，失败只记日志），没有就跳过。
func (f *forgotService) alertReset(ctx context.Context, email, name, ip, lang, via string) {
	if strings.TrimSpace(email) == "" {
		return
	}
	alert := mail.Data{
		Name: name, Title: "Password reset", Time: f.now().UTC().Format("2006-01-02 15:04:05 UTC"), IP: ip,
		Content: "Your password was reset with a " + via + " verification code. / 您的密码刚刚通过" + via + "验证码重置。",
	}
	if err := f.notify.SendMail(ctx, email, mail.TplSecurityAlert, lang, alert); err != nil {
		f.log.Warn("forgot: cannot queue security alert mail", zap.Error(err))
	}
}

func (f *forgotService) resetEmail(ctx context.Context, rawEmail, code, newPassword, ip, lang string) error {
	email, err := normEmail(rawEmail)
	if err != nil {
		return err
	}
	if newPassword == "" || len(newPassword) > 72 { // 不设强度规则；bcrypt 只取前 72 字节，故限 72
		return bad("密码长度为 1-72 字节")
	}
	if len(code) != 6 {
		return badCode()
	}
	if ok, _ := f.ipRst.Hit(ctx, ip); !ok {
		return tooMany()
	}
	if err := f.verify(ctx, email, code); err != nil {
		return err
	}
	u, err := f.store.byEmail(ctx, email)
	if err != nil {
		return err
	}
	if u == nil {
		return badCode()
	}
	hash, err := auth.HashPassword(newPassword)
	if err != nil {
		return err
	}
	if err := f.store.setPassword(ctx, u.ID, hash); err != nil {
		return err
	}
	_ = clearLock(ctx, f.cache, u.Username) // 重置后顺带解除登录锁定
	f.revokeSessions(ctx, u.ID)             // 与个人中心改密同款：被盗会话随重置一并失效
	_ = f.cache.Del(ctx, cache.Key(ctx, "forgot", "cool", digest(email)))
	f.alertReset(ctx, email, u.Name, ip, lang, "email")
	return nil
}

func (f *forgotService) resetPhone(ctx context.Context, rawPhone, code, newPassword, ip, lang string) error {
	phone := normalizePhone(rawPhone)
	if phone == "" {
		return bad("手机号格式不正确")
	}
	if newPassword == "" || len(newPassword) > 72 {
		return bad("密码长度为 1-72 字节")
	}
	if len(code) != 6 {
		return badCode()
	}
	if f.sms == nil || !f.sms.Enabled() {
		return response.Unavailable("auth.provider_missing").WithMsg("短信服务未启用")
	}
	if ok, _ := f.ipRst.Hit(ctx, ip); !ok {
		return tooMany()
	}
	if err := f.sms.Verify(smscode.WithScene(ctx, notify.SceneForgot), phone, code); err != nil {
		return err
	}
	u, err := f.store.byPhone(ctx, phone)
	if err != nil {
		return err
	}
	if u == nil {
		return badCode()
	}
	hash, err := auth.HashPassword(newPassword)
	if err != nil {
		return err
	}
	if err := f.store.setPassword(ctx, u.ID, hash); err != nil {
		return err
	}
	_ = clearLock(ctx, f.cache, u.Username)
	f.revokeSessions(ctx, u.ID)
	f.alertReset(ctx, u.Email, u.Name, ip, lang, "短信")
	return nil
}

// revokeSessions 吊销该账号的全部会话（密码重置后旧会话不能再继续用）。
// 找回密码时操作者本身没有登录态，无需保留任何会话，全部踢掉。
func (f *forgotService) revokeSessions(ctx context.Context, uid idgen.ID) {
	if f.sessions == nil {
		return
	}
	if err := f.sessions.KickUser(ctx, uid, ""); err != nil {
		f.log.Warn("forgot: cannot revoke sessions after reset", zap.Int64("user_id", int64(uid)), zap.Error(err))
	}
}

func reqLang(c *gin.Context) string {
	l := c.GetHeader("Accept-Language")
	if i := strings.IndexAny(l, ",;"); i >= 0 {
		l = l[:i]
	}
	return strings.TrimSpace(l)
}

// registerForgot 注册匿名接口 POST /auth/forgot/send-code 与 /auth/forgot/reset。
// target 含 @ 走邮件验证码，否则走手机号短信验证码（共用 modules/internal/smscode 的额度与场景隔离）。
func (m *module) registerForgot(d *core.Deps) {
	f := newForgotService(gormForgotStore{db: d.DB}, d.Cache, d.Notify, smscode.Shared(d.Config, d.Cache, d.Log), d.Log)
	f.sessions = session.Shared(d.Cache, d.Config.Auth.RefreshTTL) // 重置成功后吊销该账号全部会话
	byIP := middleware.RateLimit(d.Cache, d.Log, "forgot_route", middleware.ByIPAndPath(), 60, time.Hour)
	d.Public.POST("/auth/forgot/send-code", byIP, func(c *gin.Context) {
		var body struct {
			Target string `json:"target"`
		}
		if !response.BindJSON(c, &body) {
			return
		}
		if err := f.sendCode(c.Request.Context(), body.Target, c.ClientIP(), reqLang(c)); err != nil {
			response.Fail(c, err)
			return
		}
		response.OK(c, gin.H{"cooldown": int(forgotCooldown.Seconds())})
	})
	d.Public.POST("/auth/forgot/reset", byIP, func(c *gin.Context) {
		var body struct {
			Target      string `json:"target"`
			Code        string `json:"code"`
			NewPassword string `json:"newPassword"`
		}
		if !response.BindJSON(c, &body) {
			return
		}
		if err := f.reset(c.Request.Context(), body.Target, body.Code, body.NewPassword, c.ClientIP(), reqLang(c)); err != nil {
			response.Fail(c, err)
			return
		}
		response.OK(c, nil)
	})
}

// directory 实现 notify.Directory：按用户 ID 查邮箱 / 手机号 / 称呼。
type directory struct{ svc *service }

func (d directory) Contacts(ctx context.Context, ids []idgen.ID) (map[idgen.ID]notify.Contact, error) {
	out := map[idgen.ID]notify.Contact{}
	if len(ids) == 0 {
		return out, nil
	}
	var us []User
	if err := d.svc.db.WithContext(ctx).Select("id", "username", "nickname", "email", "phone").Where("id IN ?", ids).Find(&us).Error; err != nil {
		return nil, err
	}
	for _, u := range us {
		name := u.Nickname
		if name == "" {
			name = u.Username
		}
		out[u.ID] = notify.Contact{Name: name, Email: u.Email, Phone: u.Phone}
	}
	return out, nil
}

// mailAccount 在设置页开启「账号通知邮件」且用户有邮箱时，异步发账号创建 / 重置密码通知。reason: created | reset。
// 邮件内容不含密码。失败只记日志，不影响主流程。
func (s *service) mailAccount(ctx context.Context, u *User, reason string) {
	if s.notify == nil || u == nil || strings.TrimSpace(u.Email) == "" || !s.notify.Settings(ctx).AccountMail {
		return
	}
	name := u.Nickname
	if name == "" {
		name = u.Username
	}
	if err := s.notify.SendMail(ctx, u.Email, mail.TplAccountCreated, "", mail.Data{Name: name, Username: u.Username, Reason: reason}); err != nil && !errors.Is(err, notify.ErrMailNotConfigured) {
		zap.L().Warn("user: cannot queue account mail", zap.Error(err))
	}
}
