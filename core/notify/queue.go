package notify

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"go.uber.org/zap"

	"nexadmin/core/middleware"
	"nexadmin/pkg/idgen"
)

const attempts = 3 // 首次 + 重试 2 次

func (s *Service) worker() {
	defer s.wg.Done()
	for {
		select {
		case j := <-s.queue:
			s.run(j)
		case <-s.stop:
			for { // 退出前把队列里剩下的处理完
				select {
				case j := <-s.queue:
					s.run(j)
				default:
					return
				}
			}
		}
	}
}

func (s *Service) run(j job) {
	var (
		err   error
		spent time.Duration
		ran   bool
	)
	defer func() {
		if r := recover(); r != nil {
			s.log.Error("notify: job panicked", zap.String("job", j.desc), zap.Any("panic", r))
			err = errors.New("internal error: delivery panicked")
		}
		if ran {
			s.finish(j, err, spent)
		}
	}()
	ran = true
	for i := 0; i < attempts; i++ {
		if i > 0 {
			time.Sleep(s.backoff(i))
		}
		ctx, cancel := context.WithTimeout(j.ctx, 30*time.Second)
		t0 := time.Now()
		err = j.fn(ctx)
		spent += time.Since(t0)
		cancel()
		if err == nil {
			return
		}
		if errors.Is(err, ErrSkip) {
			s.log.Info("notify: skipped", zap.String("job", j.desc), zap.Error(err))
			return
		}
	}
	s.log.Error("notify: delivery failed after retries", zap.String("job", j.desc), zap.Int("attempts", attempts), zap.Error(err))
}

// finish 调用 j.done；done 里出的任何问题（含 panic）都只记日志，不影响 worker。
func (s *Service) finish(j job, err error, spent time.Duration) {
	if j.done == nil {
		return
	}
	defer func() {
		if r := recover(); r != nil {
			s.log.Warn("notify: send-log hook panicked", zap.String("job", j.desc), zap.Any("panic", r))
		}
	}()
	j.done(j.ctx, err, spent)
}

// Async 把任务放进队列（失败重试 2 次）。ctx 的取消被剥离但租户等值保留。队列已满或服务已关闭时丢弃并记日志。
func (s *Service) Async(ctx context.Context, desc string, fn func(ctx context.Context) error) {
	s.enqueue(ctx, desc, fn, nil)
}

// enqueue 同 Async，另带结束回调 done（见 job.done）。任务被丢弃时 done 以“丢弃”错误调用一次，让发送日志不漏记。
func (s *Service) enqueue(ctx context.Context, desc string, fn func(ctx context.Context) error, done func(context.Context, error, time.Duration)) {
	if s == nil {
		return
	}
	j := job{ctx: context.WithoutCancel(ctx), desc: desc, fn: fn, done: done}
	if s.closed.Load() {
		s.log.Warn("notify: service closed, dropped", zap.String("job", desc))
		s.finish(j, errors.New("service closed, dropped"), 0)
		return
	}
	select {
	case s.queue <- j:
	default:
		s.log.Warn("notify: queue full, dropped", zap.String("job", desc))
		s.finish(j, errors.New("queue full, dropped"), 0)
	}
}

// Send 发送一条通知。参数有误同步返回错误；其余（渠道不可用、限频、对方没有邮箱 / 手机号、发送失败）都只记日志。
func (s *Service) Send(ctx context.Context, m Msg) error {
	if s == nil {
		return nil
	}
	m.Title = strings.TrimSpace(m.Title)
	if m.Title == "" {
		return errors.New("notify: title is required")
	}
	m.To = dedupIDs(m.To)
	chans := normalizeChannels(m.Channels)
	for _, ch := range chans {
		if ch == ChannelInApp && len(m.To) == 0 {
			continue
		}
		d := &delivery{s: s, ch: ch, m: m}
		s.enqueue(ctx, "send:"+ch+":"+m.Category, d.run, d.finish)
	}
	return nil
}

func dedupIDs(in []idgen.ID) []idgen.ID {
	seen := make(map[idgen.ID]bool, len(in))
	out := make([]idgen.ID, 0, len(in))
	for _, id := range in {
		if id != 0 && !seen[id] {
			seen[id] = true
			out = append(out, id)
		}
	}
	return out
}

func normalizeChannels(in []string) []string {
	if len(in) == 0 {
		return []string{ChannelInApp}
	}
	seen := map[string]bool{}
	out := make([]string, 0, len(in))
	for _, c := range in {
		c = strings.TrimSpace(c)
		if c == "" || seen[c] {
			continue
		}
		seen[c] = true
		out = append(out, c)
	}
	return out
}

func (s *Service) lookup(ch string) Channel {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if c, ok := s.channels[ch]; ok {
		return c
	}
	if strings.HasPrefix(ch, ChannelRobotPrefix) {
		return s.channels[ChannelRobotPrefix]
	}
	return nil
}

func (s *Service) limiter(ch string) *middleware.Limiter {
	base := ch
	if strings.HasPrefix(ch, ChannelRobotPrefix) {
		base = ChannelRobotPrefix
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if l, ok := s.limiters[base]; ok {
		return l
	}
	lim, ok := s.limits[base]
	if !ok {
		lim = limit{60, time.Minute}
	}
	var l *middleware.Limiter
	if s.cache != nil {
		l = middleware.NewLimiter(s.cache, s.log, "notify:"+base, lim.max, lim.window)
	}
	s.limiters[base] = l
	return l
}

// delivery 是一次渠道投递任务的状态。准备阶段（查联系方式、限频）只做一次，重试时只重发失败的收件人；
// 结束时据此为每个收件人记一条发送日志（见 finish）。
type delivery struct {
	s  *Service
	ch string
	m  Msg

	to    []Recipient // 下一次尝试要发的收件人
	ready bool
	all   []Recipient       // 准备阶段确定的全部收件人
	sent  map[idgen.ID]bool // 已经投递成功的收件人

	noContact int // 资料里没有邮箱 / 手机号而被丢弃的人数
	limited   int // 被限频丢弃的人数
}

func (d *delivery) run(ctx context.Context) error {
	s, ch, m := d.s, d.ch, d.m
	impl := s.lookup(ch)
	if impl == nil {
		if ch == ChannelInApp {
			return skip("in-app channel unavailable (message module disabled?), %d message(s) dropped", len(m.To))
		}
		return skip("channel %q is not available", ch)
	}
	if !d.ready {
		to, err := d.prepare(ctx)
		if err != nil {
			return err
		}
		d.to, d.all, d.ready = to, append([]Recipient(nil), to...), true
	}
	isRobot := strings.HasPrefix(ch, ChannelRobotPrefix)
	if len(d.to) == 0 && !isRobot {
		return nil
	}
	attempt := d.to
	err := impl.Deliver(ctx, ch, m, attempt)
	var pe *PartialError
	switch {
	case err == nil:
		d.markSent(attempt, nil)
	case errors.As(err, &pe):
		d.markSent(attempt, pe.Failed)
		d.to = pe.Failed
	}
	return err
}

// markSent 把 attempt 里不在 failed 中的收件人记为已投递。
func (d *delivery) markSent(attempt, failed []Recipient) {
	if d.sent == nil {
		d.sent = map[idgen.ID]bool{}
	}
	bad := make(map[idgen.ID]bool, len(failed))
	for _, f := range failed {
		bad[f.ID] = true
	}
	for _, r := range attempt {
		if !bad[r.ID] {
			d.sent[r.ID] = true
		}
	}
}

// prepare 解析收件人、补全联系方式并按渠道限频。
func (d *delivery) prepare(ctx context.Context) ([]Recipient, error) {
	s, ch, m := d.s, d.ch, d.m
	lim := s.limiter(ch)
	if strings.HasPrefix(ch, ChannelRobotPrefix) {
		if ok, _ := lim.Hit(ctx, ch); !ok {
			return nil, skip("robot %q rate limited", ch)
		}
		return nil, nil
	}
	contacts := map[idgen.ID]Contact{}
	if ch == ChannelEmail || ch == ChannelSMS {
		s.mu.RLock()
		dir := s.dir
		s.mu.RUnlock()
		if dir == nil {
			return nil, skip("no user directory registered")
		}
		var err error
		if contacts, err = dir.Contacts(ctx, m.To); err != nil {
			return nil, err
		}
	}
	out := make([]Recipient, 0, len(m.To))
	for _, id := range m.To {
		c := contacts[id]
		if ch == ChannelEmail && strings.TrimSpace(c.Email) == "" {
			s.log.Info("notify: recipient skipped, no email on profile", zap.Int64("user", int64(id)), zap.String("channel", ch))
			d.noContact++
			continue
		}
		if ch == ChannelSMS && strings.TrimSpace(c.Phone) == "" {
			s.log.Info("notify: recipient skipped, no phone on profile", zap.Int64("user", int64(id)), zap.String("channel", ch))
			d.noContact++
			continue
		}
		if ok, _ := lim.Hit(ctx, ch+":"+id.String()); !ok {
			s.log.Warn("notify: recipient rate limited", zap.Int64("user", int64(id)), zap.String("channel", ch))
			d.limited++
			continue
		}
		out = append(out, Recipient{ID: id, Contact: c})
	}
	return out, nil
}

// finish 在任务结束后记发送日志：被丢弃的收件人按原因各合并成一条 skipped；其余每个收件人一条，
// 成功 / 失败按最后一次尝试判定。耗时是整个任务的耗时，同一任务的多个收件人共享。站内和自定义渠道不记。
func (d *delivery) finish(ctx context.Context, err error, spent time.Duration) {
	s := d.s
	if !s.rec.Active() {
		return
	}
	channel := recChannel(d.ch)
	if channel == "" {
		return
	}
	base := Record{Channel: channel, Scene: SceneNotice, Subject: d.m.Title, Duration: spent}
	if channel == RecordRobot {
		r := base
		r.Recipient, r.Status, r.Err = d.ch, statusOf(err), s.errText(ctx, err)
		s.emit(ctx, r)
		return
	}
	what := "phone"
	if channel == RecordEmail {
		what = "email"
	}
	if d.noContact > 0 {
		r := base
		r.Status, r.Err = StatusSkipped, fmt.Sprintf("%d recipient(s) have no %s on profile", d.noContact, what)
		s.emit(ctx, r)
	}
	if d.limited > 0 {
		r := base
		r.Status, r.Err = StatusSkipped, fmt.Sprintf("%d recipient(s) rate limited", d.limited)
		s.emit(ctx, r)
	}
	if len(d.all) == 0 {
		if err != nil { // 还没走到逐个收件人就结束了（渠道不可用、没有用户目录、查联系方式失败等）
			r := base
			r.Status, r.Err = statusOf(err), s.errText(ctx, err)
			s.emit(ctx, r)
		}
		return
	}
	text := s.errText(ctx, err)
	for _, to := range d.all {
		r := base
		if channel == RecordEmail {
			r.Recipient = strings.TrimSpace(to.Email)
		} else {
			r.Recipient = strings.TrimSpace(to.Phone)
		}
		if err == nil || d.sent[to.ID] {
			r.Status = StatusSuccess
		} else {
			r.Status, r.Err = statusOf(err), text
		}
		s.emit(ctx, r)
	}
}
