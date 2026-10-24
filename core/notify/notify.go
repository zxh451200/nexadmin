// Package notify 是统一发送入口（设计文档 §12「业务模块怎么发消息」）：
//
//	d.Notify.Send(ctx, notify.Msg{To: userIDs, Category: "order", Title: "订单已发货", Link: "/orders/123"})
//
// 默认走站内（inapp），渠道可选 email / sms / robot:<name>。站内渠道由 modules/message 在启动时注册（本包不 import 它），
// 消息模块被停用时站内渠道是空操作并记一条日志。发送异步：进内存队列，worker 失败重试 2 次，只记日志，不阻塞请求。
// 每用户每渠道有限频（复用 core/middleware 的 Limiter，计数在 core/cache）。
package notify

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"go.uber.org/zap"

	"nexadmin/core/cache"
	"nexadmin/core/config"
	"nexadmin/core/middleware"
	"nexadmin/pkg/idgen"
	"nexadmin/pkg/mail"
	"nexadmin/pkg/robot"
)

// 渠道名。
const (
	ChannelInApp       = "inapp"
	ChannelEmail       = "email"
	ChannelSMS         = "sms"
	ChannelRobotPrefix = "robot:" // robot:<配置里的机器人名称>
)

// Msg 是一条通知。To 是收件用户；Channels 为空时只发站内。robot 渠道是群播，不看 To。
type Msg struct {
	To       []idgen.ID
	Category string
	Title    string
	Content  string
	Link     string
	Channels []string
}

// Contact 是用户资料里与通知有关的部分。Lang 为空时用站点默认语言。
type Contact struct{ Name, Email, Phone, Lang string }

// Recipient 是一个收件人及其联系方式（站内渠道只用 ID）。
type Recipient struct {
	ID idgen.ID
	Contact
}

// Channel 是一个渠道的实现。channel 是完整渠道名（robot 渠道含机器人名）。
type Channel interface {
	Deliver(ctx context.Context, channel string, m Msg, to []Recipient) error
}

// ChannelFunc 让普通函数实现 Channel。
type ChannelFunc func(ctx context.Context, channel string, m Msg, to []Recipient) error

func (f ChannelFunc) Deliver(ctx context.Context, channel string, m Msg, to []Recipient) error {
	return f(ctx, channel, m, to)
}

// Directory 按用户 ID 查联系方式（由用户模块提供）。缺失的 ID 不出现在结果里。
type Directory interface {
	Contacts(ctx context.Context, ids []idgen.ID) (map[idgen.ID]Contact, error)
}

// PartialError 表示部分收件人失败：重试时只重发 Failed。
type PartialError struct {
	Failed []Recipient
	Err    error
}

func (e *PartialError) Error() string {
	return fmt.Sprintf("notify: %d recipient(s) failed: %v", len(e.Failed), e.Err)
}
func (e *PartialError) Unwrap() error { return e.Err }

// ErrSkip 表示这次发送应当跳过（渠道未配置等），不重试，只记一条信息日志。
var ErrSkip = errors.New("notify: skipped")

func skip(format string, a ...any) error {
	return fmt.Errorf("%w: %s", ErrSkip, fmt.Sprintf(format, a...))
}

// ErrMailNotConfigured 邮件（SMTP）尚未配置。
var ErrMailNotConfigured = errors.New("notify: mail is not configured")

type job struct {
	ctx  context.Context
	desc string
	fn   func(ctx context.Context) error
	// done 在任务结束（成功、跳过、重试耗尽或 panic）后调用一次，err 是最后一次的结果，spent 是各次尝试的耗时之和（不含退避）。
	// 发送日志靠它落记录；为 nil 表示这个任务不记。
	done func(ctx context.Context, err error, spent time.Duration)
}

type limit struct {
	max    int
	window time.Duration
}

// 默认限频（每用户每渠道；robot 按机器人名）。
var defaultLimits = map[string]limit{
	ChannelInApp:       {120, time.Minute},
	ChannelEmail:       {10, time.Hour},
	ChannelSMS:         {5, time.Hour},
	ChannelRobotPrefix: {30, time.Minute},
}

// Service 是通知服务。零值不可用，请用 New；nil *Service 的所有方法都是安全的空操作。
type Service struct {
	log   *zap.Logger
	cfg   *config.Config
	cache cache.Cache

	rec RecorderHolder // 发送日志记录器；没有注入时是空操作

	mu       sync.RWMutex
	channels map[string]Channel
	dir      Directory
	settings SettingsFunc
	limiters map[string]*middleware.Limiter
	limits   map[string]limit

	queue   chan job
	stop    chan struct{}
	wg      sync.WaitGroup
	closed  atomic.Bool
	backoff func(attempt int) time.Duration

	// 测试缝：替换真实的 SMTP / 机器人 / 短信发送。
	mailer func(MailSettings) (MailSender, error)
	robots func(RobotSettings) (RobotSender, error)
}

type MailSender interface {
	Send(ctx context.Context, m mail.Message) error
}

type RobotSender interface {
	Send(ctx context.Context, title, content string) error
}

var (
	_ MailSender  = (*mail.Client)(nil)
	_ RobotSender = (*robot.Client)(nil)
)

// New 创建服务并启动 worker。cfg / cache 可为 nil（限频与短信随之不可用）。
func New(cfg *config.Config, c cache.Cache, log *zap.Logger) *Service {
	if log == nil {
		log = zap.NewNop()
	}
	s := &Service{
		log: log, cfg: cfg, cache: c,
		channels: map[string]Channel{}, limiters: map[string]*middleware.Limiter{},
		limits: map[string]limit{},
		queue:  make(chan job, 1024), stop: make(chan struct{}),
		backoff: func(attempt int) time.Duration { return time.Duration(attempt) * time.Second },
	}
	for k, v := range defaultLimits {
		s.limits[k] = v
	}
	s.channels[ChannelEmail] = ChannelFunc(s.deliverEmail)
	s.channels[ChannelSMS] = ChannelFunc(s.deliverSMS)
	s.channels[ChannelRobotPrefix] = ChannelFunc(s.deliverRobot)
	for i := 0; i < 4; i++ {
		s.wg.Add(1)
		go s.worker()
	}
	return s
}

// Register 注册（或替换）一个渠道。站内渠道由 modules/message 注册：Register(notify.ChannelInApp, ch)。
func (s *Service) Register(name string, ch Channel) {
	if s == nil || ch == nil {
		return
	}
	s.mu.Lock()
	s.channels[name] = ch
	s.mu.Unlock()
}

// SetDirectory 注入联系方式查询（用户模块提供）。
func (s *Service) SetDirectory(d Directory) {
	if s == nil {
		return
	}
	s.mu.Lock()
	s.dir = d
	s.mu.Unlock()
}

// Close 停止接收新任务并处理完队列里已有的任务。
func (s *Service) Close() {
	if s == nil || !s.closed.CompareAndSwap(false, true) {
		return
	}
	close(s.stop)
	s.wg.Wait()
}
