package log

// 操作日志异步队列的用例：队满丢弃并告警、优雅关闭排空、关闭后入队不 panic、
// 请求 ctx 取消后仍落库、写失败只告警。断言打在「是否落库 / 丢弃计数 / 是否出过 WARN」上。

import (
	"context"
	"errors"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"go.uber.org/zap/zaptest/observer"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"

	"nexadmin/core/auth"
	"nexadmin/pkg/idgen"
)

const dropWarnMsg = "log: oper log queue full, records dropped"

func newQueueDB(t *testing.T) (*gorm.DB, sqlmock.Sqlmock, func()) {
	t.Helper()
	sqlDB, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	// SkipDefaultTransaction：只关心落库语句，不给每条 INSERT 预置 Begin/Commit 期望。
	db, err := gorm.Open(mysql.New(mysql.Config{Conn: sqlDB, SkipInitializeWithVersion: true}),
		&gorm.Config{DisableAutomaticPing: true, SkipDefaultTransaction: true})
	if err != nil {
		t.Fatal(err)
	}
	return db, mock, func() { _ = sqlDB.Close() }
}

func obsLogger() (*zap.Logger, *observer.ObservedLogs) {
	core, logs := observer.New(zapcore.WarnLevel)
	return zap.New(core), logs
}

// newQueueEnv 起一个带队列的 Service；close 等价模块在 d.OnClose 上登记的清理函数。
func newQueueEnv(t *testing.T) (*Service, sqlmock.Sqlmock, *observer.ObservedLogs, func()) {
	t.Helper()
	db, mock, stopDB := newQueueDB(t)
	log, obs := obsLogger()
	svc := NewService(db, "UTC", log)
	svc.startOperQueue()
	return svc, mock, obs, func() {
		defer stopDB()
		svc.stopOperQueue()
	}
}

func newOper(id int) *OperLog {
	l := &OperLog{Module: "测试", Type: "create", Operator: "alice", Status: StatusSuccess, Method: "POST", URL: "/x"}
	l.TenantID = idgen.ID(1)
	l.ID = idgen.ID(id)
	return l
}

// TestQueueFullDropsAndWarns 队满必须「不阻塞 + 计数 + 可观测告警」，不静默吞掉。
func TestQueueFullDropsAndWarns(t *testing.T) {
	db, _, stopDB := newQueueDB(t)
	defer stopDB()
	log, obs := obsLogger()
	r := newOperRecorder(db, log) // 不起冲刷协程：队列只进不出，才能确定性地填满

	done := make(chan struct{})
	go func() {
		for i := 0; i < operQueueSize; i++ {
			r.enqueue(context.Background(), newOper(i))
		}
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("队满时入队阻塞了请求")
	}

	r.enqueue(context.Background(), newOper(operQueueSize))
	if got := r.Dropped(); got != 1 {
		t.Fatalf("队满应计数丢弃 1 条，实际 %d", got)
	}
	if obs.FilterMessage(dropWarnMsg).Len() == 0 {
		t.Error("队满丢弃没有记 WARN")
	}
	// 告警要带上丢弃数与容量，否则运维无从判断丢了多少。
	found := false
	for _, e := range obs.FilterMessage(dropWarnMsg).All() {
		for _, f := range e.Context {
			if f.Key == "dropped" && f.Integer > 0 {
				found = true
			}
		}
	}
	if !found {
		t.Error("WARN 缺少 dropped 字段")
	}
}

// TestCloseDrainsQueued 优雅关闭要把已入队的残留条目排空落库。
func TestCloseDrainsQueued(t *testing.T) {
	svc, mock, obs, close := newQueueEnv(t)
	defer close()
	r := svc.operQueue()
	if r == nil {
		t.Fatal("队列未启动")
	}
	const n = 20
	for i := 0; i < n; i++ {
		svc.enqueue(context.Background(), newOper(i))
	}
	// 一批一条 INSERT（多行 VALUES），sqlmock 用同一条期望匹配。
	mock.ExpectExec("INSERT INTO `sys_oper_log`").WillReturnResult(sqlmock.NewResult(1, n))
	close()

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("关闭后残留条目没有排空落库: %v", err)
	}
	if got := r.Dropped(); got != 0 {
		t.Errorf("排空不应产生丢弃，实际 %d", got)
	}
	if obs.FilterMessage("log: oper log drain timed out").Len() != 0 {
		t.Error("不应触发排空超时")
	}
}

// TestEnqueueAfterClose 关闭后再入队不得 panic（Deps.OnClose 为 nil 或重复调用同理）。
func TestEnqueueAfterClose(t *testing.T) {
	svc, _, _, close := newQueueEnv(t)
	defer close()
	r := svc.operQueue()
	close()

	defer func() {
		if p := recover(); p != nil {
			t.Fatalf("关闭后入队 panic: %v", p)
		}
	}()
	svc.enqueue(context.Background(), newOper(1))
	if r.Dropped() == 0 {
		t.Error("关闭后入队应计数丢弃")
	}
	// 幂等：重复关闭不 panic、不重复排空。
	svc.stopOperQueue()
}

// TestMiddlewareWritesAfterRequestCtxDone 请求 ctx 被取消后，已入队的日志仍要落库。
func TestMiddlewareWritesAfterRequestCtxDone(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db, mock, stopDB := newQueueDB(t)
	defer stopDB()
	log, obs := obsLogger()
	svc := NewService(db, "UTC", log)
	svc.startOperQueue()
	defer svc.stopOperQueue()

	reqCtx, cancel := context.WithCancel(context.Background())
	defer cancel()
	e := gin.New()
	e.POST("/x", svc.Middleware("测试模块", "create"), func(c *gin.Context) {
		auth.SetClaims(c, &auth.Claims{Username: "alice", TenantID: idgen.ID(1)})
		c.Status(204)
	})
	req := httptest.NewRequest("POST", "/x", nil).WithContext(reqCtx)
	w := httptest.NewRecorder()
	e.ServeHTTP(w, req)
	if w.Code != 204 {
		t.Fatalf("响应码 = %d，业务响应不应受日志影响", w.Code)
	}

	mock.ExpectExec("INSERT INTO `sys_oper_log`").WillReturnResult(sqlmock.NewResult(1, 1))
	cancel() // 排队期间请求结束/客户端断连
	svc.stopOperQueue()

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("请求 ctx 取消后应仍落库: %v", err)
	}
	if obs.FilterMessage("log: write oper log failed").Len() != 0 {
		t.Error("正常写入不应告警")
	}
}

// TestWriteFailureOnlyWarns 落库失败不影响业务响应，只出 WARN 并计数。
func TestWriteFailureOnlyWarns(t *testing.T) {
	svc, mock, obs, close := newQueueEnv(t)
	defer close()
	r := svc.operQueue()

	svc.enqueue(context.Background(), newOper(1))
	mock.ExpectExec("INSERT INTO `sys_oper_log`").WillReturnError(errors.New("db is down"))
	close()

	if obs.FilterMessage("log: write oper log failed").Len() == 0 {
		t.Error("写失败没有出 WARN")
	}
	if r.Dropped() == 0 {
		t.Error("写失败应计入丢弃计数")
	}
}

// TestQueueFlushLoopStops 冲刷协程要随队列 ctx 取消退出，不能只靠进程退出兜底。
func TestQueueFlushLoopStops(t *testing.T) {
	db, _, stopDB := newQueueDB(t)
	defer stopDB()
	log, _ := obsLogger()
	svc := NewService(db, "UTC", log)
	svc.startOperQueue()
	r := svc.operQueue()
	if r == nil {
		t.Fatal("队列未启动")
	}
	if svc.cancelRun == nil {
		t.Fatal("队列应持有 ctx 取消函数")
	}
	svc.cancelRun()
	select {
	case <-r.Done():
	case <-time.After(2 * time.Second):
		t.Fatal("冲刷协程没有随 ctx 取消退出")
	}
	svc.stopOperQueue()
}

func waitUntil(timeout time.Duration, cond func() bool) bool {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if cond() {
			return true
		}
		time.Sleep(2 * time.Millisecond)
	}
	return cond()
}
