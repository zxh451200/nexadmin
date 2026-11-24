package devicehub

// 采集/指令日志：回答「这台设备最近到底发生了什么」——离线时最想知道的那件事。
//
// 刻意只放内存环形缓冲（每台设备 200 条，进程重启即清）：
//   - 它是现场排障的临时证据，不是审计。审计已经有两条更合适的出口：指令/配置改动走 oper_log，
//     告警流水走 alarm_event 落库。
//   - 落表要额外背保留期、清理任务与每秒数条的写放大（200ms 轮询的设备一天就是几十万行），
//     而真正被读的永远是最近几分钟。
//
// 同一条错误连续出现时合并计数（Repeat），而不是刷满整个环：设备掉线时每个周期都会读失败，
// 不去重的话 200 条缓冲几秒钟就只剩同一行错误，反而看不到掉线之前发生了什么。

import (
	"context"
	"sync"
	"time"

	"nexadmin/pkg/idgen"
)

// logRingSize 是每台设备保留的日志条数。按 5s 轮询估算大约覆盖最近十几分钟，
// 足够回答「刚才断的那一刻报了什么」，又不至于让常驻内存随设备数失控（200 条 ≈ 40KB/台）。
const logRingSize = 200

// 同一条日志在这个窗口内重复出现时合并计数。
const logRepeatWindow = 10 * time.Second

// 成功轮询的摘要日志最小间隔：没有它，正常设备会把缓冲刷成一片「轮询成功」，淹没真正的异常。
const logSummaryInterval = 60 * time.Second

// 设备级错误落库的最小间隔：错误文案不变时不重复写库，避免掉线设备每 200ms 写一次 device 表。
const deviceErrorWriteInterval = 30 * time.Second

// LogEntry 是回给后台的一行日志。
type LogEntry struct {
	TS      time.Time `json:"ts"`
	Level   string    `json:"level"`            // info | warn | error
	Kind    string    `json:"kind"`             // connect | poll | read | write | status
	Target  string    `json:"target,omitempty"` // 测点地址；状态类日志留空
	Message string    `json:"message"`
	CostMs  int64     `json:"costMs,omitempty"`
	// Repeat 是这条日志（同 kind/target/message）在合并窗口内重复出现的次数（含首次，1 表示没重复）。
	Repeat int `json:"repeat"`
}

type logRing struct {
	mu         sync.Mutex
	buf        []LogEntry
	lastLog    time.Time // 上一条成功类摘要的时间，用于 logSummaryInterval 限流
	lastErrMsg string
	lastErrAt  time.Time
}

// appendLog 追加一条日志；与上一条完全相同的（kind/target/message）在窗口内合并成重复计数。
func (s *Service) appendLog(deviceID idgen.ID, e LogEntry) {
	if deviceID == 0 || e.Message == "" {
		return
	}
	if e.TS.IsZero() {
		e.TS = time.Now().UTC()
	}
	e.Repeat = 1
	r := s.logRing(deviceID)
	r.mu.Lock()
	defer r.mu.Unlock()
	if n := len(r.buf); n > 0 {
		last := &r.buf[n-1]
		if last.Kind == e.Kind && last.Target == e.Target && last.Message == e.Message &&
			e.TS.Sub(last.TS) < logRepeatWindow {
			last.TS = e.TS
			last.Repeat++
			if e.CostMs > 0 {
				last.CostMs = e.CostMs
			}
			return
		}
	}
	if len(r.buf) >= logRingSize {
		copy(r.buf, r.buf[1:])
		r.buf = r.buf[:len(r.buf)-1]
	}
	r.buf = append(r.buf, e)
}

// shouldLogSummary 判断成功类日志是否到了可以再记的间隔（滑动写 r.lastLog）。
func (s *Service) shouldLogSummary(deviceID idgen.ID, now time.Time) bool {
	r := s.logRing(deviceID)
	r.mu.Lock()
	defer r.mu.Unlock()
	if !r.lastLog.IsZero() && now.Sub(r.lastLog) < logSummaryInterval {
		return false
	}
	r.lastLog = now
	return true
}

// Logs 取某设备最近 limit 条日志（新的在前）。
func (s *Service) Logs(deviceID idgen.ID, limit int) []LogEntry {
	if limit <= 0 || limit > logRingSize {
		limit = logRingSize
	}
	r := s.logRing(deviceID)
	r.mu.Lock()
	defer r.mu.Unlock()
	n := len(r.buf)
	if n > limit {
		n = limit
	}
	out := make([]LogEntry, 0, n)
	for i := len(r.buf) - 1; i >= 0 && len(out) < n; i-- {
		out = append(out, r.buf[i])
	}
	return out
}

// dropLogs 在设备删除/停用时释放缓冲，避免内存按历史设备数累积。
func (s *Service) dropLogs(deviceID idgen.ID) {
	s.logMu.Lock()
	delete(s.logs, deviceID)
	s.logMu.Unlock()
}

func (s *Service) logRing(deviceID idgen.ID) *logRing {
	s.logMu.Lock()
	defer s.logMu.Unlock()
	if s.logs == nil {
		s.logs = map[idgen.ID]*logRing{}
	}
	r := s.logs[deviceID]
	if r == nil {
		r = &logRing{buf: make([]LogEntry, 0, logRingSize)}
		s.logs[deviceID] = r
	}
	return r
}

// markDeviceError 把「最后一次采集错误」落到 device 行：离线设备重启后也能看到原因，
// 设备列表不必再点进详情。文案不变时不重复写库（deviceErrorWriteInterval 内只写一次）。
func (s *Service) markDeviceError(ctx context.Context, d *Device, msg string) {
	if d == nil || msg == "" {
		return
	}
	now := time.Now().UTC()
	r := s.logRing(d.ID)
	r.mu.Lock()
	if r.lastErrMsg == msg && now.Sub(r.lastErrAt) < deviceErrorWriteInterval {
		r.mu.Unlock()
		return
	}
	r.lastErrMsg, r.lastErrAt = msg, now
	r.mu.Unlock()

	d.LastError = msg
	d.LastErrorAt = &now
	if err := s.db.WithContext(ctx).Model(&Device{}).Where("id = ?", int64(d.ID)).
		Updates(map[string]any{"last_error": msg, "last_error_at": now}).Error; err != nil {
		s.logWarn("devicehub: update last_error failed", "err", err.Error())
	}
}

// clearDeviceError 在采集恢复后清掉 last_error（没有错误时这一行不该保留历史噪音）。
func (s *Service) clearDeviceError(ctx context.Context, d *Device) {
	if d == nil || d.LastError == "" {
		return
	}
	d.LastError = ""
	d.LastErrorAt = nil
	if err := s.db.WithContext(ctx).Model(&Device{}).Where("id = ?", int64(d.ID)).
		Updates(map[string]any{"last_error": "", "last_error_at": nil}).Error; err != nil {
		s.logWarn("devicehub: clear last_error failed", "err", err.Error())
	}
}
