package openapi

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"go.uber.org/zap"

	"nexadmin/core/secret"
)

const (
	sendTimeout     = 5 * time.Second
	maxResponseBody = 2048
	maxAttempts     = 5
	leaseDuration   = 2 * time.Minute
	pollInterval    = 5 * time.Second
	claimBatch      = 50
	workerParallel  = 8
	retention       = 30 * 24 * time.Hour
)

// backoffs[i] 是第 i+1 次失败后到下一次重试的等待：1m、5m、30m、2h；第 5 次失败即放弃。
var backoffs = []time.Duration{time.Minute, 5 * time.Minute, 30 * time.Minute, 2 * time.Hour}

// nextBackoff 返回已尝试 attempts 次（>=1）且仍失败时的下次重试等待；ok=false 表示不再重试。
func nextBackoff(attempts int) (time.Duration, bool) {
	if attempts < 1 || attempts >= maxAttempts || attempts > len(backoffs) {
		return 0, false
	}
	return backoffs[attempts-1], true
}

// signPayload 返回 X-Nex-Signature：hex(HMAC-SHA256(secret, timestamp + "." + body))。
func signPayload(secretPlain, timestamp string, body []byte) string {
	mac := hmac.New(sha256.New, []byte(secretPlain))
	mac.Write([]byte(timestamp + "."))
	mac.Write(body)
	return hex.EncodeToString(mac.Sum(nil))
}

type sendResult struct {
	Status   int
	Body     string
	Err      string
	Duration time.Duration
}

func (r sendResult) ok() bool { return r.Err == "" && r.Status >= 200 && r.Status < 300 }

// sender 负责单次 HTTP 投递。guard=false 仅用于单测（允许连回环地址的接收端）。
type sender struct {
	client *http.Client
	lookup lookupFunc
	guard  bool
	now    func() time.Time
}

func newSender() *sender {
	return &sender{client: newClient(sendTimeout, safeDialControl), lookup: defaultLookup, guard: true, now: time.Now}
}

func (s *sender) send(ctx context.Context, rawURL, secretPlain, event, deliveryID string, body []byte) (res sendResult) {
	start := time.Now()
	defer func() { res.Duration = time.Since(start) }()
	if s.guard {
		if _, err := validateURL(ctx, rawURL, s.lookup); err != nil {
			res.Err = clip(err.Error(), 480)
			return res
		}
	}
	ctx, cancel := context.WithTimeout(ctx, sendTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, rawURL, strings.NewReader(string(body)))
	if err != nil {
		res.Err = clip(err.Error(), 480)
		return res
	}
	ts := strconv.FormatInt(s.now().Unix(), 10)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "NexAdmin-Webhook/1.0")
	req.Header.Set("X-Nex-Event", event)
	req.Header.Set("X-Nex-Delivery", deliveryID)
	req.Header.Set("X-Nex-Timestamp", ts)
	req.Header.Set("X-Nex-Signature", signPayload(secretPlain, ts, body))
	resp, err := s.client.Do(req)
	if err != nil {
		res.Err = clip(unwrapURLErr(err), 480)
		return res
	}
	defer resp.Body.Close()
	res.Status = resp.StatusCode
	buf, _ := io.ReadAll(io.LimitReader(resp.Body, maxResponseBody))
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 64<<10)) // 尽量读完以复用连接，但不无限读
	res.Body = strings.ToValidUTF8(string(buf), "")
	if !res.ok() {
		res.Err = "unexpected http status " + strconv.Itoa(resp.StatusCode)
	}
	return res
}

func unwrapURLErr(err error) string {
	var ue interface{ Unwrap() error }
	if errors.As(err, &ue) && ue.Unwrap() != nil {
		return ue.Unwrap().Error()
	}
	return err.Error()
}

func clip(s string, n int) string {
	if len(s) <= n {
		return s
	}
	s = s[:n]
	for !utf8.ValidString(s) && len(s) > 0 {
		s = s[:len(s)-1]
	}
	return s
}

// store 是 worker / dispatcher 依赖的持久化接口（单测用内存实现）。所有方法跨租户。
type store interface {
	ActiveHooks(ctx context.Context, tenantID int64) ([]Webhook, error)
	Enqueue(ctx context.Context, ds []Delivery) error
	ClaimDue(ctx context.Context, now time.Time, lease time.Duration, limit int) ([]Delivery, error)
	Webhook(ctx context.Context, id int64) (*Webhook, error) // 不存在返回 nil, nil
	Finish(ctx context.Context, d *Delivery) error
	Cleanup(ctx context.Context, before time.Time) (int64, error)
}

type worker struct {
	st     store
	send   *sender
	log    *zap.Logger
	now    func() time.Time
	wakeCh chan struct{}
}

func newWorker(st store, s *sender, log *zap.Logger) *worker {
	if log == nil {
		log = zap.NewNop()
	}
	return &worker{st: st, send: s, log: log, now: time.Now, wakeCh: make(chan struct{}, 1)}
}

func (w *worker) wake() {
	select {
	case w.wakeCh <- struct{}{}:
	default:
	}
}

// run 轮询到期投递；每小时清理一次过期记录。ctx 取消后返回。
func (w *worker) run(ctx context.Context) {
	t := time.NewTicker(pollInterval)
	defer t.Stop()
	lastClean := time.Time{}
	for {
		w.tick(ctx)
		if w.now().Sub(lastClean) >= time.Hour {
			lastClean = w.now()
			if n, err := w.st.Cleanup(ctx, w.now().UTC().Add(-retention)); err != nil {
				w.log.Warn("openapi: cleanup deliveries failed", zap.Error(err))
			} else if n > 0 {
				w.log.Info("openapi: expired deliveries removed", zap.Int64("count", n))
			}
		}
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		case <-w.wakeCh:
		}
	}
}

// tick 认领并处理一批到期投递，返回处理条数。
func (w *worker) tick(ctx context.Context) int {
	ds, err := w.st.ClaimDue(ctx, w.now().UTC(), leaseDuration, claimBatch)
	if err != nil {
		w.log.Warn("openapi: claim deliveries failed", zap.Error(err))
		return 0
	}
	var wg sync.WaitGroup
	sem := make(chan struct{}, workerParallel)
	for i := range ds {
		wg.Add(1)
		sem <- struct{}{}
		go func(d Delivery) {
			defer wg.Done()
			defer func() { <-sem }()
			w.handle(ctx, &d)
		}(ds[i])
	}
	wg.Wait()
	return len(ds)
}

// handle 做一次投递尝试并落库结果：2xx 成功；否则按退避重试，用尽后 failed。
func (w *worker) handle(ctx context.Context, d *Delivery) {
	now := w.now().UTC()
	hook, err := w.st.Webhook(ctx, int64(d.WebhookID))
	var plain string
	if err == nil && hook != nil && hook.Enabled {
		plain, err = secret.Decrypt(hook.SecretEnc)
	}
	switch {
	case err != nil:
		w.log.Warn("openapi: load webhook failed", zap.Error(err), zap.Int64("delivery", int64(d.ID)))
		d.Status, d.ErrorMsg = StatusFailed, "internal error loading webhook"
	case hook == nil || !hook.Enabled:
		d.Status, d.ErrorMsg = StatusFailed, "webhook disabled or deleted"
	default:
		res := w.send.send(ctx, hook.URL, plain, d.Event, d.ID.String(), []byte(d.Payload))
		d.Attempts++
		d.ResponseStatus, d.ResponseBody, d.DurationMs = res.Status, res.Body, int(res.Duration/time.Millisecond)
		d.ErrorMsg = res.Err
		if res.ok() {
			d.Status, d.DeliveredAt = StatusSuccess, &now
		} else if delay, retry := nextBackoff(d.Attempts); retry {
			d.Status, d.NextAttemptAt = StatusPending, now.Add(delay)
		} else {
			d.Status = StatusFailed
		}
	}
	if err := w.st.Finish(ctx, d); err != nil {
		w.log.Error("openapi: save delivery result failed", zap.Error(err), zap.Int64("delivery", int64(d.ID)))
	}
}
