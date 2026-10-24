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
	"nexadmin/pkg/idgen"
	"nexadmin/pkg/mail"
)

// 忘记密码（邮箱验证码）规则：6 位、10 分钟有效、加盐哈希存 core/cache；同邮箱 60 秒一次、每小时有上限，
// 同 IP 限流；连续输错 5 次作废，校验成功即作废（一次性）；邮箱不存在 / 账号停用时对外表现与成功发送完全一致（防枚举）。
const (
	forgotTTL      = 10 * time.Minute
	forgotCooldown = 60 * time.Second
	forgotMaxTries = 5
)

// forgotStore 是忘记密码需要的用户数据访问（测试里用假实现）。
type forgotStore interface {
	// byEmail 返回启用状态且邮箱唯一匹配的账号；找不到、停用或邮箱重复返回 (nil, nil)。
	byEmail(ctx context.Context, email string) (*forgotUser, error)
	setPassword(ctx context.Context, id idgen.ID, hash string) error
}

type forgotUser struct {
	ID       idgen.ID
	Username string
	Name     string
}

type gormForgotStore struct{ db *gorm.DB }

func (g gormForgotStore) byEmail(ctx context.Context, email string) (*forgotUser, error) {
	var us []User
	if err := g.db.WithContext(ctx).Where("LOWER(email) = ?", email).Limit(2).Find(&us).Error; err != nil {
		return nil, err
	}
	if len(us) != 1 || us[0].Status != statusOn {
		return nil, nil
	}
	u := us[0]
	name := u.Nickname
	if name == "" {
		name = u.Username
	}
	return &forgotUser{ID: u.ID, Username: u.Username, Name: name}, nil
}

func (g gormForgotStore) setPassword(ctx context.Context, id idgen.ID, hash string) error {
	return g.db.WithContext(ctx).Model(&User{}).Where("id = ?", int64(id)).Update("password", hash).Error
}

type forgotService struct {
	store  forgotStore
	cache  cache.Cache
	notify *notify.Service
	log    *zap.Logger
	ipSend *middleware.Limiter
	ipRst  *middleware.Limiter
	acct   *middleware.Limiter
	now    func() time.Time
}

func newForgotService(st forgotStore, c cache.Cache, n *notify.Service, log *zap.Logger) *forgotService {
	return &forgotService{
		store: st, cache: c, notify: n, log: log, now: time.Now,
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

// sendCode 发送验证码。对外只有三种结果：邮件未配置（503）、被限流（429）、成功（含“账号不存在”）。
func (f *forgotService) sendCode(ctx context.Context, rawEmail, ip, lang string) error {
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
