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

// reset 校验验证码并设置新密码。
func (f *forgotService) reset(ctx context.Context, rawEmail, code, newPassword, ip, lang string) error {
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
	_ = f.cache.Del(ctx, cache.Key(ctx, "forgot", "cool", digest(email)))
	alert := mail.Data{
		Name: u.Name, Title: "Password reset", Time: f.now().UTC().Format("2006-01-02 15:04:05 UTC"), IP: ip,
		Content: "Your password was reset with an email verification code. / 您的密码刚刚通过邮箱验证码重置。",
	}
	if err := f.notify.SendMail(ctx, email, mail.TplSecurityAlert, lang, alert); err != nil {
		f.log.Warn("forgot: cannot queue security alert mail", zap.Error(err))
	}
	return nil
}

func reqLang(c *gin.Context) string {
	l := c.GetHeader("Accept-Language")
	if i := strings.IndexAny(l, ",;"); i >= 0 {
		l = l[:i]
	}
	return strings.TrimSpace(l)
}

// registerForgot 注册匿名接口 POST /auth/forgot/send-code 与 /auth/forgot/reset。
func (m *module) registerForgot(d *core.Deps) {
	f := newForgotService(gormForgotStore{db: d.DB}, d.Cache, d.Notify, d.Log)
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
