package plugin

import (
	"context"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"nexadmin/core"
)

// 框架关闭钩子（程序内 App.closeAll）必须取消插件 ctx；此前 m.stop 全程没被调用。
func TestAttachRegistersCloseHookCancelsCtx(t *testing.T) {
	m := newManager(nil, Options{Root: t.TempDir()})
	t.Cleanup(m.stop)
	var closer func()
	m.Attach(&core.Deps{OnClose: func(f func()) { closer = f }})
	if closer == nil {
		t.Fatal("Attach 未把关闭钩子登记进 deps.OnClose")
	}
	if m.ctx.Err() != nil {
		t.Fatal("关闭前 m.ctx 不应已取消")
	}
	closer()
	if m.ctx.Err() != context.Canceled {
		t.Fatalf("App.closeAll 必须取消插件 ctx，得到 %v", m.ctx.Err())
	}
}

// m.stop 必须幂等：OnClose 与 SIGTERM 两条链路都指向它，重复调用不能 panic。
func TestStopIsIdempotent(t *testing.T) {
	m := newManager(nil, Options{Root: t.TempDir()})
	m.stop()
	m.stop()
	if m.ctx.Err() != context.Canceled {
		t.Fatal("stop 两次后 ctx 应已取消")
	}
}

// blockingSrc 在 probe（一次文件元数据 IO）处阻塞，用来观察 admit 是否在 m.mu 之外做 IO。
type blockingSrc struct {
	gate   chan struct{}
	probes atomic.Int64
}

func (s *blockingSrc) probe() (string, int64, error) {
	s.probes.Add(1)
	<-s.gate // 模拟打文件系统耗时
	return "", 0, nil
}

func (s *blockingSrc) read() ([]byte, error) { return nil, nil }

// admit 每个插件请求都会走；文件 IO 必须在锁外，否则会把所有请求串行化并打文件系统。
func TestAdmitDoesFileIOOutsideLock(t *testing.T) {
	gin.SetMode(gin.TestMode)
	m := newManager([]Plugin{{ID: "cron", Edition: Pro, Module: fakeModule{}}},
		Options{Root: t.TempDir(), Now: func() time.Time { return now }})
	t.Cleanup(m.stop)
	src := &blockingSrc{gate: make(chan struct{})}
	m.revSrc = src // 覆盖真实文件系统

	e := m.entries["cron"]
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest("GET", "/x", nil)

	done := make(chan bool, 1)
	go func() { done <- m.admit(c, e) }()

	// 让 goroutine 进入 probe 阻塞。
	time.Sleep(50 * time.Millisecond)
	if src.probes.Load() == 0 {
		close(src.gate)
		t.Fatal("admit 尚未开始文件探测")
	}
	if !m.mu.TryLock() {
		close(src.gate)
		t.Fatal("admit 在 m.mu 临界区内做文件 IO（应移出锁外）")
	}
	m.mu.Unlock()

	close(src.gate)
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("admit 未退出（可能死锁）")
	}
	if src.probes.Load() == 0 {
		t.Fatal("probe 未被调用")
	}
}

// 并发 admit 时，文件探测调用计数应反映每个请求都独立探测（锁外），且都不阻塞彼此进入临界区。
func TestConcurrentAdmitsProbeWithoutSerializingOnIO(t *testing.T) {
	gin.SetMode(gin.TestMode)
	m := newManager([]Plugin{{ID: "cron", Edition: Pro, Module: fakeModule{}}},
		Options{Root: t.TempDir(), Now: func() time.Time { return now }})
	t.Cleanup(m.stop)
	src := &blockingSrc{gate: make(chan struct{})}
	m.revSrc = src

	e := m.entries["cron"]
	const n = 8
	started := make(chan struct{}, n)
	results := make(chan bool, n)
	for i := 0; i < n; i++ {
		go func() {
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest("GET", "/x", nil)
			started <- struct{}{}
			results <- m.admit(c, e)
		}()
	}
	for i := 0; i < n; i++ {
		<-started
	}
	// 至少一次 probe 期间，锁必须可被外部获取（说明 IO 不在锁内）。
	time.Sleep(50 * time.Millisecond)
	locked := m.mu.TryLock()
	if locked {
		m.mu.Unlock()
	}
	close(src.gate)
	if !locked {
		t.Fatal("并发 admit 持有全局锁做 IO")
	}
	for i := 0; i < n; i++ {
		select {
		case <-results:
		case <-time.After(3 * time.Second):
			t.Fatal("并发 admit 未全部退出")
		}
	}
	if got := src.probes.Load(); got < n {
		t.Fatalf("每个 admit 都应做一次锁外 probe，得到 %d 次", got)
	}
}
