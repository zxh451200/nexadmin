package user

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"math/big"
	"strings"
	"time"

	"go.uber.org/zap"
	"gorm.io/gorm"

	"nexadmin/core/cache"
	"nexadmin/core/middleware"
	"nexadmin/core/notify"
	"nexadmin/core/response"
	"nexadmin/modules/internal/smscode"
	"nexadmin/pkg/idgen"
	"nexadmin/pkg/mail"
)

// 忘记密码（邮箱或手机号验证码）规则：6 位、邮件 10 分钟 / 短信 5 分钟有效、加盐哈希存 core/cache；
// 同目标 60 秒一次、每小时有上限，同 IP 限流；连续输错 5 次作废，校验成功即作废（一次性）；
// 账号不存在 / 停用时对外表现与成功发送完全一致（防枚举）。短信场景的冷却与每日 / IP 限额
// 由 modules/internal/smscode 统一执行，并与手机号登录、绑定手机号共享同一手机号的额度。
const (
	forgotTTL      = 10 * time.Minute
	forgotCooldown = 60 * time.Second
	forgotMaxTries = 5
)

// forgotStore 是忘记密码需要的用户数据访问（测试里用假实现）。
type forgotStore interface {
	// byEmail 返回启用状态且邮箱唯一匹配的账号；找不到、停用或邮箱重复返回 (nil, nil)。
	byEmail(ctx context.Context, email string) (*forgotUser, error)
	// byPhone 返回启用状态且手机号唯一匹配的账号（含 +86/86 前缀写法）；找不到、停用或重复返回 (nil, nil)。
	byPhone(ctx context.Context, phone string) (*forgotUser, error)
	setPassword(ctx context.Context, id idgen.ID, hash string) error
}

type forgotUser struct {
	ID       idgen.ID
	Username string
	Name     string
	Email    string // 有邮箱时重置成功发安全告警邮件；没有就跳过
}

// uniqueForgotUser 列表恰好一条且启用才返回；停用 / 多条 / 空（防枚举：多条视为不存在）都返回 nil。
func uniqueForgotUser(us []User) *forgotUser {
	if len(us) != 1 || us[0].Status != statusOn {
		return nil
	}
	u := us[0]
	name := u.Nickname
	if name == "" {
		name = u.Username
	}
	return &forgotUser{ID: u.ID, Username: u.Username, Name: name, Email: u.Email}
}

type gormForgotStore struct{ db *gorm.DB }

func (g gormForgotStore) byEmail(ctx context.Context, email string) (*forgotUser, error) {
	var us []User
	if err := g.db.WithContext(ctx).Where("LOWER(email) = ?", email).Limit(2).Find(&us).Error; err != nil {
		return nil, err
	}
	return uniqueForgotUser(us), nil
}

func (g gormForgotStore) byPhone(ctx context.Context, phone string) (*forgotUser, error) {
	var us []User
	if err := g.db.WithContext(ctx).Where("phone IN ?", []string{phone, "+86" + phone, "86" + phone, "+86 " + phone}).Limit(2).Find(&us).Error; err != nil {
		return nil, err
	}
	return uniqueForgotUser(us), nil
}

func (g gormForgotStore) setPassword(ctx context.Context, id idgen.ID, hash string) error {
	return g.db.WithContext(ctx).Model(&User{}).Where("id = ?", int64(id)).Update("password", hash).Error
}

// smsSender 是忘记密码用到的短信验证码能力；生产环境是 *smscode.Service，测试用假实现。
type smsSender interface {
	Enabled() bool
	Send(ctx context.Context, phone, ip string, deliver bool) error
	Verify(ctx context.Context, phone, code string) error
}

type forgotService struct {
	store  forgotStore
	sms    smsSender
	cache  cache.Cache
	notify *notify.Service
	log    *zap.Logger
	ipSend *middleware.Limiter
	ipRst  *middleware.Limiter
	acct   *middleware.Limiter
	now    func() time.Time
}

func newForgotService(st forgotStore, c cache.Cache, n *notify.Service, sms smsSender, log *zap.Logger) *forgotService {
	return &forgotService{
		store: st, sms: sms, cache: c, notify: n, log: log, now: time.Now,
		ipSend: middleware.NewLimiter(c, log, "forgot_send_ip", 10, time.Hour),
		ipRst:  middleware.NewLimiter(c, log, "forgot_reset_ip", 30, time.Hour),
		acct:   middleware.NewLimiter(c, log, "forgot_acct", 5, time.Hour),
	}
}

func tooMany() error {
	return response.New(429, "error.bad_request").WithMsg("操作过于频繁，请稍后再试")
}

func badCode() error {
	return response.BadRequest("error.bad_request").WithMsg("验证码错误或已过期")
}

func digest(parts ...string) string {
	h := sha256.Sum256([]byte(strings.Join(parts, ":")))
	return hex.EncodeToString(h[:])
}

func codeKey(ctx context.Context, email string) string {
	return cache.Key(ctx, "forgot", "code", digest(email))
}

// triesKey 是某一条验证码的输错计数 key；带上盐，重新发送的新验证码天然从 0 开始计数。
func triesKey(ctx context.Context, email, salt string) string {
	return cache.Key(ctx, "forgot", "tries", digest(email), salt)
}

func normEmail(raw string) (string, error) {
	a, err := mail.ParseAddress(raw)
	if err != nil {
		return "", response.BadRequest("error.bad_request").WithMsg("邮箱格式不正确")
	}
	return strings.ToLower(a), nil
}

// sendCode 发送验证码。target 含 @ 走邮件，否则视为手机号走短信。
// 对外只有三种结果：渠道未配置（503）、被限流（429）、成功（含“账号不存在”）。
func (f *forgotService) sendCode(ctx context.Context, rawTarget, ip, lang string) error {
	if strings.Contains(rawTarget, "@") {
		return f.sendEmailCode(ctx, rawTarget, ip, lang)
	}
	return f.sendPhoneCode(ctx, rawTarget, ip)
}

// sendPhoneCode 手机号短信验证码。冷却 / 每日 / IP 限额由 smscode 统一控制（与其他短信场景共享额度）；
// 手机号未绑定账号时同样计数、响应一致但不真正发送（防枚举，见 smscode.Send 的 deliver 语义）。
func (f *forgotService) sendPhoneCode(ctx context.Context, rawPhone, ip string) error {
	phone := normalizePhone(rawPhone)
	if phone == "" {
		return response.BadRequest("error.bad_request").WithMsg("手机号格式不正确")
	}
	if f.sms == nil || !f.sms.Enabled() {
		return response.Unavailable("auth.provider_missing").WithMsg("短信服务未启用")
	}
	u, err := f.store.byPhone(ctx, phone)
	if err != nil {
		return err
	}
	return f.sms.Send(smscode.WithScene(ctx, notify.SceneForgot), phone, ip, u != nil)
}

func (f *forgotService) sendEmailCode(ctx context.Context, rawEmail, ip, lang string) error {
	email, err := normEmail(rawEmail)
	if err != nil {
		return err
	}
	if !f.notify.MailConfigured(ctx) {
		return response.Unavailable("auth.provider_missing").WithMsg("邮件服务未启用")
	}
	if ok, _ := f.ipSend.Hit(ctx, ip); !ok {
		return tooMany()
	}
	eh := digest(email)
	coolKey := cache.Key(ctx, "forgot", "cool", eh)
	// 冷却原子化：Incr（首次创建时 TTL=forgotCooldown），结果 ==1 才放行；中途失败要释放，避免用户被锁却没收到邮件。
	if n, err := cache.Incr(f.cache, ctx, coolKey, forgotCooldown); err != nil {
		return err
	} else if n != 1 {
		return tooMany()
	}
	release := func() { _ = f.cache.Del(ctx, coolKey) }
	if ok, _ := f.acct.Hit(ctx, eh); !ok {
		release()
		return tooMany()
	}
	u, err := f.store.byEmail(ctx, email)
	if err != nil {
		release()
		return err
	}
	if u == nil {
		return nil // 与成功发送同样的响应；不生成验证码、不发邮件
	}
	n, err := rand.Int(rand.Reader, big.NewInt(1000000))
	if err != nil {
		release()
		return err
	}
	code := fmt.Sprintf("%06d", n.Int64())
	salt := make([]byte, 8)
	if _, err := rand.Read(salt); err != nil {
		release()
		return err
	}
	sl := hex.EncodeToString(salt)
	val := fmt.Sprintf("%s|%s|0|%d", sl, digest(sl, email, code), f.now().Add(forgotTTL).Unix())
	if err := f.cache.Set(ctx, codeKey(ctx, email), val, forgotTTL); err != nil {
		release()
		return err
	}
	// 异步发送：不因邮件服务快慢泄漏账号是否存在；发送失败只记日志。
	if err := f.notify.SendMail(ctx, email, mail.TplForgotCode, lang, mail.Data{Name: u.Name, Code: code, Minutes: int(forgotTTL.Minutes())}); err != nil {
		_ = f.cache.Del(ctx, codeKey(ctx, email))
		release()
		f.log.Warn("forgot: cannot queue code mail", zap.Error(err))
	}
	return nil
}
