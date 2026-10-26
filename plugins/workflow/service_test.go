package workflow

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/gin-gonic/gin"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"

	"nexadmin/core/event"
	"nexadmin/core/response"
	"nexadmin/core/tenant"
	"nexadmin/pkg/idgen"
)

// skipCtx 是无租户上下文的测试 ctx(TenantModel 写入时 tenant_id=0,查询不加过滤)。
func skipCtx() context.Context { return tenant.Skip(context.Background()) }

func TestMain(m *testing.M) {
	if err := idgen.Init(1); err != nil {
		panic(err)
	}
	gin.SetMode(gin.TestMode)
	m.Run()
}

var testNow = time.Date(2025, 6, 1, 0, 0, 0, 0, time.UTC)

func mockDB(t *testing.T) (*gorm.DB, sqlmock.Sqlmock) {
	t.Helper()
	sqlDB, m, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	db, err := gorm.Open(mysql.New(mysql.Config{Conn: sqlDB, SkipInitializeWithVersion: true}), &gorm.Config{
		DisableAutomaticPing: true,
		NowFunc:              func() time.Time { return testNow },
	})
	if err != nil {
		t.Fatal(err)
	}
	return db, m
}

type nopLogger struct{}

func (nopLogger) Warn(string, ...any)  {}
func (nopLogger) Error(string, ...any) {}

const defJSON = `{"key":"d1","name":"两级会签","start":"start","nodes":[` +
	`{"key":"start","name":"开始","kind":"start"},` +
	`{"key":"n1","name":"保管员","kind":"approval","approvers":["u1"],"mode":"any","escalateTo":"n2"},` +
	`{"key":"n2","name":"科长","kind":"approval","approvers":["u2","u3"],"mode":"all"},` +
	`{"key":"end","name":"结束","kind":"end"}],` +
	`"edges":[{"from":"start","to":"n1"},{"from":"n1","to":"n2"},{"from":"n2","to":"end"}]}`

func defRowWithStatus(status string) *sqlmock.Rows {
	return sqlmock.NewRows([]string{
		"id", "tenant_id", "def_key", "name", "status", "version", "definition", "created_at", "updated_at",
	}).AddRow(1001, 1, "d1", "两级会签", status, 1, defJSON, testNow, testNow)
}

func defRow() *sqlmock.Rows { return defRowWithStatus(DefPublished) }

func instRow(no int64, status string, stateJSON string) *sqlmock.Rows {
	return sqlmock.NewRows([]string{
		"id", "tenant_id", "def_id", "def_key", "title", "biz_type", "business_key",
		"submitter", "status", "state_json", "created_at", "updated_at",
	}).AddRow(no, 1, 1001, "d1", "E2E 审批", "test", "B-1",
		"u0", status, stateJSON, testNow, testNow)
}

func startedState() string {
	// Start 后 token 停在 n1。
	return `{"defKey":"d1","status":"running","current":["n1"],"done":["start"],` +
		`"votes":{},"extraApprovers":{},"vars":{},` +
		`"history":[{"at":"2025-06-01T00:00:00Z","node":"start","actor":"u0","action":"start"}]}`
}

func TestTemplatesValid(t *testing.T) {
	for _, tpl := range Templates() {
		if errs := Validate(tpl.Definition); len(errs) != 0 {
			t.Fatalf("模板 %s 非法: %v", tpl.Key, errs)
		}
	}
}

// 发起：读已发布定义 → 幂等检查 → 引擎 Start → 落库 → 发 started + todo 事件。
func TestInstStartPublishes(t *testing.T) {
	db, m := mockDB(t)
	s := NewService(db, nopLogger{})

	events := make(chan event.Event, 8)
	event.Subscribe(event.SubscriberFunc(func(e event.Event) {
		if strings.HasPrefix(e.Name, "workflow.") {
			events <- e
		}
	}))

	m.ExpectQuery("SELECT \\* FROM `plg_workflow_def`").WillReturnRows(defRow())
	m.ExpectQuery("SELECT \\* FROM `plg_workflow_inst`").WillReturnError(gorm.ErrRecordNotFound)
	m.ExpectBegin()
	m.ExpectExec("INSERT INTO `plg_workflow_inst`").WillReturnResult(sqlmock.NewResult(1, 1))
	m.ExpectCommit()

	v, err := s.InstStart(skipCtx(), StartInput{
		DefID: "1001", Title: "E2E 审批", BizType: "test", BusinessKey: "B-1",
	}, "u0")
	if err != nil {
		t.Fatal(err)
	}
	if v.Status != StatusRunning || len(v.Current) != 1 || v.Current[0] != "n1" {
		t.Fatalf("unexpected instance %+v", v)
	}
	gotStarted, gotTodo := false, false
	for i := 0; i < 2; i++ {
		select {
		case ev := <-events:
			switch ev.Name {
			case EventStarted:
				gotStarted = true
			case EventTodo:
				gotTodo = true
				if data, ok := ev.Data.(EventData); ok && len(data.Approvers) != 1 {
					t.Fatalf("todo approvers = %v", data.Approvers)
				}
			}
		case <-time.After(2 * time.Second):
			t.Fatal("events not published")
		}
	}
	if !gotStarted || !gotTodo {
		t.Fatalf("started=%v todo=%v", gotStarted, gotTodo)
	}
	if err := m.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

// 未发布的定义拒绝发起（不触库写路径）。
func TestInstStartRequiresPublished(t *testing.T) {
	db, m := mockDB(t)
	s := NewService(db, nopLogger{})

	m.ExpectQuery("SELECT \\* FROM `plg_workflow_def`").WillReturnRows(defRowWithStatus(DefDraft))

	_, err := s.InstStart(skipCtx(), StartInput{DefID: "1001"}, "u0")
	e, ok := err.(*response.Error)
	if !ok || e.Status != http.StatusConflict {
		t.Fatalf("want 409, got %v", err)
	}
}

// 同业务键已有运行中实例 → 幂等返回既有实例，不落新单。
func TestInstStartIdempotentByBiz(t *testing.T) {
	db, m := mockDB(t)
	s := NewService(db, nopLogger{})

	m.ExpectQuery("SELECT \\* FROM `plg_workflow_def`").WillReturnRows(defRow())
	m.ExpectQuery("SELECT \\* FROM `plg_workflow_inst`").WillReturnRows(instRow(2005, StatusRunning, startedState()))

	v, err := s.InstStart(skipCtx(), StartInput{
		DefID: "1001", BizType: "test", BusinessKey: "B-1",
	}, "u0")
	if err != nil {
		t.Fatal(err)
	}
	if v.ID != 2005 {
		t.Fatalf("should return existing inst 2005, got %v", v.ID)
	}
	if err := m.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

// 或签：u1 同意后推进到会签节点 n2；非审批人被拒（事务回滚）。
func TestApproveAndReject(t *testing.T) {
	db, m := mockDB(t)
	s := NewService(db, nopLogger{})

	// instAct 顺序：查实例（无事务）→ 查定义 → 事务内保存
	m.ExpectQuery("SELECT \\* FROM `plg_workflow_inst` WHERE id = \\?").WillReturnRows(instRow(2001, StatusRunning, startedState()))
	m.ExpectQuery("SELECT \\* FROM `plg_workflow_def` WHERE id = \\?").WillReturnRows(defRow())
	m.ExpectBegin()
	m.ExpectExec("UPDATE `plg_workflow_inst`").WillReturnResult(sqlmock.NewResult(1, 1))
	m.ExpectCommit()

	v, err := s.InstApprove(skipCtx(), 2001, "u1", "同意")
	if err != nil {
		t.Fatal(err)
	}
	if v.Status != StatusRunning || len(v.Current) != 1 || v.Current[0] != "n2" {
		t.Fatalf("after approve: %+v", v)
	}

	m.ExpectQuery("SELECT \\* FROM `plg_workflow_inst` WHERE id = \\?").WillReturnRows(instRow(2001, StatusRunning, startedState()))
	m.ExpectQuery("SELECT \\* FROM `plg_workflow_def` WHERE id = \\?").WillReturnRows(defRow())
	m.ExpectBegin()
	m.ExpectRollback()
	_, err = s.InstApprove(skipCtx(), 2001, "u9", "越权")
	e, ok := err.(*response.Error)
	if !ok || e.Status != http.StatusBadRequest {
		t.Fatalf("want 400, got %v", err)
	}
}

// 会签：u2 同意后 n2 未完成；u3 也同意才 approved。
func TestCountersignAll(t *testing.T) {
	db, m := mockDB(t)
	s := NewService(db, nopLogger{})

	state := `{"defKey":"d1","status":"running","current":["n2"],"done":["start","n1"],` +
		`"votes":{"n1":{"u1":"approve"}},"extraApprovers":{},"vars":{},"history":[]}`
	m.ExpectQuery("SELECT \\* FROM `plg_workflow_inst` WHERE id = \\?").WillReturnRows(instRow(2002, StatusRunning, state))
	m.ExpectQuery("SELECT \\* FROM `plg_workflow_def` WHERE id = \\?").WillReturnRows(defRow())
	m.ExpectBegin()
	m.ExpectExec("UPDATE `plg_workflow_inst`").WillReturnResult(sqlmock.NewResult(1, 1))
	m.ExpectCommit()

	v, err := s.InstApprove(skipCtx(), 2002, "u2", "")
	if err != nil {
		t.Fatal(err)
	}
	if v.Status != StatusRunning {
		t.Fatalf("会签未齐应仍 running, got %s", v.Status)
	}
	full := `{"defKey":"d1","status":"running","current":["n2"],"done":["start","n1"],` +
		`"votes":{"n1":{"u1":"approve"},"n2":{"u2":"approve"}},"extraApprovers":{},"vars":{},"history":[]}`
	m.ExpectQuery("SELECT \\* FROM `plg_workflow_inst` WHERE id = \\?").WillReturnRows(instRow(2002, StatusRunning, full))
	m.ExpectQuery("SELECT \\* FROM `plg_workflow_def` WHERE id = \\?").WillReturnRows(defRow())
	m.ExpectBegin()
	m.ExpectExec("UPDATE `plg_workflow_inst`").WillReturnResult(sqlmock.NewResult(1, 1))
	m.ExpectCommit()

	v, err = s.InstApprove(skipCtx(), 2002, "u3", "")
	if err != nil {
		t.Fatal(err)
	}
	if v.Status != StatusApproved {
		t.Fatalf("会签齐后应 approved, got %s", v.Status)
	}
}

// 回退：把 token 退回 n1，清除下游投票。
func TestRollback(t *testing.T) {
	db, m := mockDB(t)
	s := NewService(db, nopLogger{})

	state := `{"defKey":"d1","status":"running","current":["n2"],"done":["start","n1"],` +
		`"votes":{"n1":{"u1":"approve"}},"extraApprovers":{},"vars":{},"history":[]}`
	m.ExpectQuery("SELECT \\* FROM `plg_workflow_inst` WHERE id = \\?").WillReturnRows(instRow(2003, StatusRunning, state))
	m.ExpectQuery("SELECT \\* FROM `plg_workflow_def` WHERE id = \\?").WillReturnRows(defRow())
	m.ExpectBegin()
	m.ExpectExec("UPDATE `plg_workflow_inst`").WillReturnResult(sqlmock.NewResult(1, 1))
	m.ExpectCommit()

	v, err := s.InstRollback(skipCtx(), 2003, "u2", "n1", "材料不齐,退回")
	if err != nil {
		t.Fatal(err)
	}
	if len(v.Current) != 1 || v.Current[0] != "n1" {
		t.Fatalf("rollback target = %v", v.Current)
	}
	if v.Votes["n1"] != nil {
		t.Fatal("回退后应清除 n1 的投票")
	}
}

// 撤回：仅提交人可撤。
func TestWithdrawOwnerOnly(t *testing.T) {
	db, m := mockDB(t)
	s := NewService(db, nopLogger{})

	m.ExpectQuery("SELECT \\* FROM `plg_workflow_inst` WHERE id = \\?").WillReturnRows(instRow(2004, StatusRunning, startedState()))
	_, err := s.InstWithdraw(skipCtx(), 2004, "u9", "不是我的")
	e, ok := err.(*response.Error)
	if !ok || e.Status != http.StatusForbidden {
		t.Fatalf("want 403, got %v", err)
	}

	// instWithdraw 先 instGet（查提交人），通过后 instAct 再走一遍（查实例 + 定义 + 事务保存）
	m.ExpectQuery("SELECT \\* FROM `plg_workflow_inst` WHERE id = \\?").WillReturnRows(instRow(2004, StatusRunning, startedState()))
	m.ExpectQuery("SELECT \\* FROM `plg_workflow_inst` WHERE id = \\?").WillReturnRows(instRow(2004, StatusRunning, startedState()))
	m.ExpectQuery("SELECT \\* FROM `plg_workflow_def` WHERE id = \\?").WillReturnRows(defRow())
	m.ExpectBegin()
	m.ExpectExec("UPDATE `plg_workflow_inst`").WillReturnResult(sqlmock.NewResult(1, 1))
	m.ExpectCommit()

	v, err := s.InstWithdraw(skipCtx(), 2004, "u0", "撤回")
	if err != nil {
		t.Fatal(err)
	}
	if v.Status != StatusCancelled {
		t.Fatalf("status = %s", v.Status)
	}
}

// 超时清扫：n1 配置 TimeoutMinutes=1 且实例超过时限 → 自动升级到 n2（未配置则跳过）。
func TestSweepEscalate(t *testing.T) {
	db, m := mockDB(t)
	s := NewService(db, nopLogger{})

	// 断言清扫时不是「一路跳过租户」：扫描可以 Skip，但推进实例的写操作必须带实例所属租户。
	var actTenant idgen.ID
	var actSkipped bool
	db.Callback().Update().Before("gorm:update").Register("test:escalation_tenant", func(tx *gorm.DB) {
		actTenant, _ = tenant.From(tx.Statement.Context)
		actSkipped = tenant.Skipped(tx.Statement.Context)
	})

	old := time.Now().UTC().Add(-2 * time.Minute)
	staleDef := `{"key":"d1","name":"超时升级","start":"start","nodes":[` +
		`{"key":"start","name":"开始","kind":"start"},` +
		`{"key":"n1","name":"一审","kind":"approval","approvers":["u1"],"mode":"any","escalateTo":"n2","timeoutMinutes":1},` +
		`{"key":"n2","name":"二审","kind":"approval","approvers":["u2"],"mode":"any"},` +
		`{"key":"end","name":"结束","kind":"end"}],` +
		`"edges":[{"from":"start","to":"n1"},{"from":"n1","to":"n2"},{"from":"n2","to":"end"}]}`
	staleInst := `{"defKey":"d1","status":"running","current":["n1"],"done":["start"],"votes":{},"extraApprovers":{},"vars":{},"history":[]}`
	staleRows := func() *sqlmock.Rows {
		return sqlmock.NewRows([]string{
			"id", "tenant_id", "def_id", "def_key", "title", "biz_type", "business_key",
			"submitter", "status", "state_json", "created_at", "updated_at",
		}).AddRow(3001, 1, 1002, "d1", "超时单", "test", "B-2", "u0", StatusRunning, staleInst, old, old)
	}
	defRows := func() *sqlmock.Rows {
		return sqlmock.NewRows([]string{
			"id", "tenant_id", "def_key", "name", "status", "version", "definition", "created_at", "updated_at",
		}).AddRow(1002, 1, "d1", "超时升级", DefPublished, 1, staleDef, old, old)
	}

	m.ExpectQuery("SELECT \\* FROM `plg_workflow_inst`").WillReturnRows(staleRows())
	m.ExpectQuery("SELECT \\* FROM `plg_workflow_def`").WillReturnRows(defRows())
	// SweepEscalate 调 InstEscalate → instAct: SELECT inst + SELECT def + 事务保存
	m.ExpectQuery("SELECT \\* FROM `plg_workflow_inst` WHERE id = \\?").WillReturnRows(staleRows())
	m.ExpectQuery("SELECT \\* FROM `plg_workflow_def` WHERE id = \\?").WillReturnRows(defRows())
	m.ExpectBegin()
	m.ExpectExec("UPDATE `plg_workflow_inst`").WillReturnResult(sqlmock.NewResult(1, 1))
	m.ExpectCommit()

	// 生产环境由 plugin.Starter 传入没有租户的进程 ctx，清扫器内部需自行 tenant.Skip。
	n, err := s.SweepEscalate(context.Background(), time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("escalated %d, want 1", n)
	}
	if actTenant != 1 || actSkipped {
		t.Fatalf("escalation ran with tenant=%d skipped=%v, want tenant=1 skipped=false", actTenant, actSkipped)
	}
}
