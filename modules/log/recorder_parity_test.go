package log

// 与修复前「每请求直写」的语义对照：同一个 Service 分别走旧路径（WriteOper 直写）与新路径
// （中间件 → 队列 → 冲刷），断言两种情况写入的列与值一一对应，最后一条日志没有因为改异步而被改写；
// 并断言跨租户的一批日志在队列路径下合并成一条 INSERT、每条仍带回自己的 tenant_id
// （若被拆成两次写入或漏掉租户，sqlmock 会因期望不满足而报错）。

import (
	"context"
	"database/sql/driver"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"gorm.io/gorm"

	"nexadmin/pkg/idgen"
)

type recArg struct {
	mu  *sync.Mutex
	out *[]driver.Value
}

func (a recArg) Match(v driver.Value) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	*a.out = append(*a.out, v)
	return true
}

// operLogColumns 取 GORM 为 OperLog 生成的 INSERT 列顺序，用来算一行多少实参、租户在第几列。
func operLogColumns(t *testing.T, db *gorm.DB) []string {
	t.Helper()
	stmt := &gorm.Statement{DB: db}
	if err := stmt.Parse(&OperLog{}); err != nil {
		t.Fatal(err)
	}
	cols := make([]string, 0, len(stmt.Schema.Fields))
	for _, f := range stmt.Schema.Fields {
		if f.DBName != "" {
			cols = append(cols, f.DBName)
		}
	}
	return cols
}

// capture 预置一条写入 rows 行的 INSERT 期望，把实参按顺序收进 out。
func capture(mock sqlmock.Sqlmock, rows, cols int, out *[]driver.Value, mu *sync.Mutex) {
	args := make([]driver.Value, 0, rows*cols)
	for i := 0; i < rows*cols; i++ {
		args = append(args, recArg{mu: mu, out: out})
	}
	// 不锚定 ^：队列路径的多行 VALUES 也归同一条期望。
	mock.ExpectExec("INSERT INTO `sys_oper_log`").WithArgs(args...).
		WillReturnResult(sqlmock.NewResult(1, int64(rows)))
}

func indexOf(cols []string, name string) int {
	for i, c := range cols {
		if c == name {
			return i
		}
	}
	return -1
}

func sampleOper(id int64) *OperLog {
	l := &OperLog{Module: "订单", Type: "create", Operator: "alice", IP: "10.0.0.1", Status: StatusSuccess,
		CostMs: 12, Method: "POST", URL: "/api/v1/orders", Params: `{"id":1}`, Response: `{"code":0}`}
	l.TenantID = idgen.ID(1)
	l.ID = idgen.ID(id)
	return l
}

// sameRows 逐列比较；created_at/updated_at 由 GORM 在写入时填当前时间，两次写入必然不同，
// 所以只要求两边同为时间值，其余列必须完全相等。
func sameRows(a, b []driver.Value) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		_, at := a[i].(time.Time)
		_, bt := b[i].(time.Time)
		if at && bt {
			continue
		}
		if !reflect.DeepEqual(a[i], b[i]) {
			return false
		}
	}
	return true
}

func TestQueueWritesSameRecordAsDirectWrite(t *testing.T) {
	db, mock, stopDB := newQueueDB(t)
	defer stopDB()
	log, obs := obsLogger()
	svc := NewService(db, "UTC", log)
	cols := len(operLogColumns(t, db))

	var (
		mu             sync.Mutex
		direct, queued []driver.Value
	)
	capture(mock, 1, cols, &direct, &mu)
	if err := svc.WriteOper(context.Background(), sampleOper(1)); err != nil {
		t.Fatalf("直写失败: %v", err)
	}

	svc.startOperQueue()
	capture(mock, 1, cols, &queued, &mu)
	svc.enqueue(context.Background(), sampleOper(1))
	svc.stopOperQueue()

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("两条写入路径都应有落库调用: %v", err)
	}
	if !sameRows(direct, queued) {
		t.Errorf("队列写入与直写内容不一致：直写 %v / 队列 %v", direct, queued)
	}
	if obs.FilterMessage("log: write oper log failed").Len() != 0 {
		t.Error("不应出现写失败告警")
	}
}

func TestQueueBatchesAcrossTenantsInOneInsert(t *testing.T) {
	db, mock, stopDB := newQueueDB(t)
	defer stopDB()
	log, _ := obsLogger()
	svc := NewService(db, "UTC", log)
	cols := operLogColumns(t, db)
	idx := indexOf(cols, "tenant_id")
	if idx < 0 {
		t.Fatal("模型里没有 tenant_id 列")
	}

	svc.startOperQueue()
	var (
		mu    sync.Mutex
		batch []driver.Value
	)
	// 只预置一条 2 行的期望：若实现把两个租户拆成两次写入，第二条 INSERT 会当场报错。
	capture(mock, 2, len(cols), &batch, &mu)

	a, b := sampleOper(2), sampleOper(3)
	b.TenantID = idgen.ID(7)
	svc.enqueue(context.Background(), a)
	svc.enqueue(context.Background(), b)
	if n := svc.stopOperQueue(); n != 0 {
		t.Fatalf("排空后仍有 %d 条未落库", n)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("跨租户日志应合并成一条 INSERT: %v", err)
	}
	got := map[int64]bool{toI64(batch[idx]): true, toI64(batch[idx+len(cols)]): true}
	if !got[1] || !got[7] {
		t.Errorf("批量写入漏掉租户归属: %v", got)
	}
	_ = log
}

func toI64(v driver.Value) int64 {
	if x, ok := v.(int64); ok {
		return x
	}
	return -1
}
