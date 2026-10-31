package workflow

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/nexadmin/flowlite/store"
	"nexadmin/core/tenant"
)

// TestGormStoresIntegration 用真实 MySQL 验证 GORM 适配层：乐观锁、租户隔离、
// 待办替换语义这些是 sqlmock 照不出来的（sqlmock 只正则匹配、从不执行 SQL）。
// 需要一个可丢弃的库：NEXADMIN_IT_MYSQL_DSN=user:pass@tcp(host:port)/dbname?parseTime=true
// 测试会 DROP/CREATE plg_workflow_* 表。
func TestGormStoresIntegration(t *testing.T) {
	db := setupWorkflowDB(t)

	ctx := tenant.With(context.Background(), 42)
	defs := gormDefs{db: db}
	insts := gormInsts{db: db}
	tasks := gormTasks{db: db}
	actions := gormActions{db: db}

	raw, _ := jsonMarshal(seqDefinition())
	def, err := defs.Save(ctx, &store.Definition{Key: "d1", Name: "两级", Status: store.DefPublished, Definition: raw})
	if err != nil {
		t.Fatalf("保存定义: %v", err)
	}
	if def.Version != 1 || def.ID == "" {
		t.Fatalf("定义 = %+v", def)
	}
	if got, err := defs.GetByKey(ctx, "d1"); err != nil || got.ID != def.ID {
		t.Fatalf("按 key 取定义: %+v %v", got, err)
	}

	inst := &store.Instance{
		DefID: def.ID, DefKey: "d1", DefVersion: 1, Title: "真实库实例",
		BizType: "test", BusinessKey: "T-1", Submitter: "u1", Status: StatusRunning,
		Revision: 1, TenantID: "42",
		State: raw, Snapshot: raw,
	}
	if err := insts.Create(ctx, inst); err != nil {
		t.Fatalf("创建实例: %v", err)
	}
	if inst.ID == "" {
		t.Fatal("实例 ID 未回填")
	}

	// 乐观锁：读到的版本才能写成功，旧版本必须冲突。
	inst.Status = StatusApproved
	inst.Revision = 2
	if err := insts.Update(ctx, inst, 1); err != nil {
		t.Fatalf("正确版本更新失败: %v", err)
	}
	if err := insts.Update(ctx, inst, 1); !errors.Is(err, store.ErrConflict) {
		t.Fatalf("旧版本更新应为 ErrConflict，实际 %v", err)
	}

	// 租户隔离：换一个租户读同一个 ID 必须查不到。
	other := tenant.With(context.Background(), 43)
	if _, err := insts.Get(other, inst.ID); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("跨租户读取应 NotFound，实际 %v", err)
	}
	if err := insts.Update(other, inst, 2); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("跨租户写入应 NotFound，实际 %v", err)
	}

	// 待办替换：同一 (node, assignee) 保留主键；消失的置 closed。
	if err := tasks.SyncPending(ctx, inst.ID, "d1", []store.PendingTask{
		{NodeKey: "n1", NodeName: "保管员", Assignee: "u1"},
		{NodeKey: "n1", NodeName: "保管员", Assignee: "u2"},
	}); err != nil {
		t.Fatalf("同步待办: %v", err)
	}
	rows, err := tasks.ListByInstance(ctx, inst.ID)
	if err != nil || len(rows) != 2 {
		t.Fatalf("实例待办 = %d %v", len(rows), err)
	}
	var u2ID string
	for _, r := range rows {
		if r.Assignee == "u2" {
			u2ID = r.ID
		}
	}
	if err := tasks.SyncPending(ctx, inst.ID, "d1", []store.PendingTask{
		{NodeKey: "n1", NodeName: "保管员", Assignee: "u2"},
		{NodeKey: "n2", NodeName: "科长", Assignee: "u3"},
	}); err != nil {
		t.Fatalf("再次同步待办: %v", err)
	}
	if _, total, _ := tasks.ListPending(ctx, "u1", store.TaskQuery{Page: 1, PageSize: 20}); total != 0 {
		t.Fatalf("被撤销的待办应关闭，实际 %d", total)
	}
	mine, total, err := tasks.ListPending(ctx, "u2", store.TaskQuery{Page: 1, PageSize: 20})
	if err != nil || total != 1 || mine[0].ID != u2ID {
		t.Fatalf("保留的待办应沿用原主键: %+v %v", mine, err)
	}
	rows, _ = tasks.ListByInstance(ctx, inst.ID)
	pending := 0
	for _, r := range rows {
		if r.Status == store.TaskPending {
			pending++
		}
	}
	if pending != 2 {
		t.Fatalf("pending = %d, want 2", pending)
	}

	// 动作审计。
	if err := actions.Record(ctx, &store.Action{
		InstID: inst.ID, DefKey: "d1", NodeKey: "n1", Actor: "u1",
		Action: ActionApprove, Comment: "同意", At: time.Now().UTC(),
	}); err != nil {
		t.Fatalf("记录动作: %v", err)
	}
	acts, total, err := actions.List(ctx, store.ActionQuery{
		Actor: "u1", Actions: []string{ActionApprove}, Page: 1, PageSize: 20,
	})
	if err != nil || total != 1 || acts[0].InstID != inst.ID || acts[0].NodeKey != "n1" {
		t.Fatalf("动作查询 = %+v %v", acts, err)
	}
}

func jsonMarshal(v any) ([]byte, error) { return json.Marshal(v) }
