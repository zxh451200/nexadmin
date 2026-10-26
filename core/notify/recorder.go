package notify

import (
	"context"
	"errors"
	"strings"
	"sync/atomic"
	"time"

	"go.uber.org/zap"

	"nexadmin/pkg/mail"
)

// 发送结果。
const (
	StatusSuccess = "success"
	StatusFail    = "fail"
	StatusSkipped = "skipped"
)

// 发送场景（Record.Scene）。
const (
	SceneNotice     = "notice"      // 通过 Send 投递的普通通知
	SceneVerifyCode = "verify_code" // 验证码（短信登录）
	SceneBindPhone  = "bind_phone"  // 个人中心绑定手机号的验证码
	SceneForgot     = "forgot"      // 忘记密码的短信验证码（验证码 key 按场景隔离，见 modules/internal/smscode）
	SceneAccount    = "account"     // 账号创建 / 重置密码通知
	SceneSecurity   = "security"    // 安全告警
	SceneTest       = "test"        // 设置页「发送测试」
)

// 渠道（Record.Channel）。站内消息不记发送日志。
const (
	RecordSMS   = "sms"
	RecordEmail = "email"
	RecordRobot = "robot"
)

// Record 是一次投递的结果，交给 Recorder 持久化。
//
// 它故意不含正文、验证码和任何密钥：Subject 只放模板名或标题。Recipient 是原始收件地址
// （手机号 / 邮箱 / 机器人渠道名），脱敏由 Recorder 负责，所以 Recorder 实现不得原样落库。
type Record struct {
	Channel   string // RecordSMS / RecordEmail / RecordRobot
	Scene     string // Scene*
	Recipient string // 原始收件地址；机器人为完整渠道名（robot:<名称>）；未知时为空
	Subject   string // 模板名或标题
	Status    string // StatusSuccess / StatusFail / StatusSkipped
	Err       string // 失败 / 跳过原因，已去掉已知密钥；Recorder 仍应自行截断、脱敏
	Duration  time.Duration
}

// Recorder 接收发送结果。实现必须快速返回、不阻塞、不 panic（通常是入队后异步写库）；
// notify 不关心它的成败，也不会因为它影响投递。core 不依赖具体实现，由 sendlog 模块在注册时注入。
type Recorder interface {
	Record(ctx context.Context, r Record)
}

type recorderBox struct{ r Recorder }

// RecorderHolder 持有一个可并发替换的 Recorder。零值可用：没有注入时一切都是空操作。
type RecorderHolder struct{ p atomic.Pointer[recorderBox] }

// Set 注入（或用 nil 移除）Recorder。
func (h *RecorderHolder) Set(r Recorder) {
	if h == nil {
		return
	}
	if r == nil {
		h.p.Store(nil)
		return
	}
	h.p.Store(&recorderBox{r: r})
}

// Active 报告是否注入了 Recorder。
func (h *RecorderHolder) Active() bool { return h != nil && h.p.Load() != nil }

// Emit 把一条记录交给 Recorder；未注入时什么都不做。Recorder 的 panic 被吞掉并记一条 warn，永不影响调用方。
func (h *RecorderHolder) Emit(ctx context.Context, log *zap.Logger, r Record) {
	if h == nil {
		return
	}
	b := h.p.Load()
	if b == nil {
		return
	}
	defer func() {
		if p := recover(); p != nil && log != nil {
			log.Warn("notify: send-log recorder panicked", zap.Any("panic", p))
		}
	}()
	b.r.Record(ctx, r)
}

// SetRecorder 注入发送日志记录器（由 sendlog 模块在注册时调用）；传 nil 移除。没有注入时发送路径不做任何记录。
func (s *Service) SetRecorder(r Recorder) {
	if s == nil {
		return
	}
	s.rec.Set(r)
}

func (s *Service) emit(ctx context.Context, r Record) {
	if s == nil {
		return
	}
	s.rec.Emit(ctx, s.log, r)
}

// statusOf 把投递结果映射成日志状态：nil = 成功，ErrSkip = 跳过，其余 = 失败。
func statusOf(err error) string {
	switch {
	case err == nil:
		return StatusSuccess
	case errors.Is(err, ErrSkip):
		return StatusSkipped
	}
	return StatusFail
}

// recChannel 把完整渠道名映射成日志渠道；站内和自定义渠道返回 ""（不记）。
func recChannel(ch string) string {
	switch {
	case ch == ChannelEmail:
		return RecordEmail
	case ch == ChannelSMS:
		return RecordSMS
	case strings.HasPrefix(ch, ChannelRobotPrefix):
		return RecordRobot
	}
	return ""
}

// mailScene 按邮件模板决定场景。
func mailScene(tpl string) string {
	switch tpl {
	case mail.TplForgotCode:
		return SceneVerifyCode
	case mail.TplAccountCreated:
		return SceneAccount
	case mail.TplSecurityAlert:
		return SceneSecurity
	}
	return SceneNotice
}

// errText 生成写进 Record.Err 的文本：去掉配置里已知的密钥（SMTP 密码、机器人 URL / 密钥、短信密钥）。
// 一般性的脱敏（手机号、邮箱、URL、令牌）与截断由 Recorder 负责。
func (s *Service) errText(ctx context.Context, err error, extra ...string) string {
	if err == nil || !s.rec.Active() {
		return ""
	}
	msg := err.Error()
	for _, sec := range append(s.knownSecrets(ctx), extra...) {
		if len(sec) >= 4 {
			msg = strings.ReplaceAll(msg, sec, "[REDACTED]")
		}
	}
	return msg
}

func (s *Service) knownSecrets(ctx context.Context) []string {
	var out []string
	if s.cfg != nil {
		c := s.cfg.SMS
		for _, v := range []string{c.Webhook.URL, c.Webhook.Secret, c.Aliyun.AccessKeyID, c.Aliyun.AccessKeySecret,
			c.Tencent.SecretID, c.Tencent.SecretKey} {
			out = append(out, v, expand(v))
		}
	}
	st := s.Settings(ctx)
	out = append(out, st.Mail.Password)
	for _, r := range st.Robots {
		out = append(out, r.URL, r.Secret)
	}
	return out
}
