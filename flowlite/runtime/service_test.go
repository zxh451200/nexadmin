package runtime

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/nexadmin/flowlite/engine"
	"github.com/nexadmin/flowlite/store"
	"github.com/nexadmin/flowlite/store/memory"
)

func seqDef() engine.Definition {
	return engine.Definition{
		Key: "leave", Name: "请假", Start: "start",
		Nodes: []engine.Node{
			{Key: "start", Name: "开始", Kind: engine.KindStart},
			{Key: "n1", Name: "主管", Kind: engine.KindApproval, Approvers: []string{"u1"}},
			{Key: "n2", Name: "HR", Kind: engine.KindApproval, Approvers: []string{"u2", "u3"}, Mode: engine.ModeAll},
			{Key: "end", Name: "结束", Kind: engine.KindEnd},
		},
		Edges: []engine.Edge{
			{From: "start", To: "n1"},
			{From: "n1", To: "n2"},
			{From: "n2", To: "end"},
		},
	}
}

func seed(t *testing.T, svc *Service, def engine.Definition) *store.Definition {
	t.Helper()
	raw, err := json.Marshal(def)
	if err != nil {
		t.Fatal(err)
	}
	rec, err := svc.Defs.Save(context.Background(), &store.Definition{
		Key: def.Key, Name: def.Name, Status: store.DefPublished, Definition: raw,
	})
	if err != nil {
		t.Fatal(err)
	}
	return rec
}

func newTest(t *testing.T) (*Service, *memory.Bundle, *store.Definition) {
	t.Helper()
	b := memory.New()
	svc := New(b.Defs, b.Insts, b.Tasks, b.Actions)
	def := seed(t, svc, seqDef())
	return svc, b, def
}

func TestStartApproveFlow(t *testing.T) {
	svc, _, def := newTest(t)
	ctx := context.Background()

	res, err := svc.Start(ctx, StartInput{DefID: def.ID, Title: "张三请假"}, "u1")
	if err != nil {
		t.Fatalf("发起失败: %v", err)
	}
	if res.State.Status != engine.StatusRunning || res.State.Current[0] != "n1" {
		t.Fatalf("发起后状态 = %s %v", res.State.Status, res.State.Current)
	}
	if res.Inst.DefVersion != 1 || len(res.Inst.Snapshot) == 0 {
		t.Fatal("实例必须带定义版本与快照")
	}

	tasks, total, err := svc.ListMinePending(ctx, "u1", 1, 20)
	if err != nil || total != 1 || len(tasks) != 1 || tasks[0].Inst.ID != res.Inst.ID {
		t.Fatalf("u1 待办 = %d/%d %v", total, len(tasks), err)
	}

	if _, err := svc.Act(ctx, res.Inst.ID, "u1", ActionApprove, ActionInput{Comment: "同意"}); err != nil {
		t.Fatalf("u1 审批失败: %v", err)
	}
	tasks, total, _ = svc.ListMinePending(ctx, "u2", 1, 20)
	if total != 1 || len(tasks) != 1 {
		t.Fatalf("会签待办应到 u2/u3，实际 total=%d", total)
	}

	if _, err := svc.Act(ctx, res.Inst.ID, "u2", ActionApprove, ActionInput{}); err != nil {
		t.Fatalf("u2 审批失败: %v", err)
	}
	out, err := svc.Act(ctx, res.Inst.ID, "u3", ActionApprove, ActionInput{})
	if err != nil {
		t.Fatalf("u3 审批失败: %v", err)
	}
	if out.State.Status != engine.StatusApproved {
		t.Fatalf("状态 = %s", out.State.Status)
	}
	_, total, _ = svc.ListMinePending(ctx, "u3", 1, 20)
	if total != 0 {
		t.Fatalf("结束后不应有待办，实际 %d", total)
	}

	handled, total, err := svc.ListMineHandled(ctx, "u2", 1, 20)
	if err != nil || total != 1 || handled[0].Inst.ID != res.Inst.ID {
		t.Fatalf("我已办 = %d %v", total, err)
	}
}

func TestStartIdempotentByBiz(t *testing.T) {
	svc, _, def := newTest(t)
	ctx := context.Background()
	in := StartInput{DefID: def.ID, Title: "订单 1", BizType: "order", BusinessKey: "o-1"}

	a, err := svc.Start(ctx, in, "u1")
	if err != nil {
		t.Fatal(err)
	}
	b, err := svc.Start(ctx, in, "u9")
	if err != nil {
		t.Fatal(err)
	}
	if a.Inst.ID != b.Inst.ID {
		t.Fatalf("同业务键应幂等返回同一实例: %s vs %s", a.Inst.ID, b.Inst.ID)
	}
	_, total, err := svc.Insts.List(ctx, store.InstQuery{Page: 1, PageSize: 20})
	if err != nil || total != 1 {
		t.Fatalf("实例数 = %d, err=%v", total, err)
	}
}

func TestDefinitionSnapshotPinned(t *testing.T) {
	svc, _, def := newTest(t)
	ctx := context.Background()
	res, err := svc.Start(ctx, StartInput{DefID: def.ID, Title: "改流程"}, "u1")
	if err != nil {
		t.Fatal(err)
	}

	// 模拟定义被改成「n1 直接到结束」，并删除 n2。
	changed := seqDef()
	changed.Nodes = []engine.Node{
		{Key: "start", Kind: engine.KindStart},
		{Key: "n1", Name: "主管", Kind: engine.KindApproval, Approvers: []string{"u1"}},
		{Key: "end", Kind: engine.KindEnd},
	}
	changed.Edges = []engine.Edge{{From: "start", To: "n1"}, {From: "n1", To: "end"}}
	raw, _ := json.Marshal(changed)
	cur, _ := svc.Defs.Get(ctx, def.ID)
	cur.Definition = raw
	if _, err := svc.Defs.Save(ctx, cur); err != nil {
		t.Fatal(err)
	}

	out, err := svc.Act(ctx, res.Inst.ID, "u1", ActionApprove, ActionInput{})
	if err != nil {
		t.Fatalf("在途实例审批失败: %v", err)
	}
	if len(out.State.Current) != 1 || out.State.Current[0] != "n2" {
		t.Fatalf("在途实例必须按发起时快照执行，Current = %v", out.State.Current)
	}
}

func TestConcurrentApprovalsRetry(t *testing.T) {
	svc, _, def := newTest(t)
	ctx := context.Background()
	res, err := svc.Start(ctx, StartInput{DefID: def.ID, Title: "并发"}, "u1")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Act(ctx, res.Inst.ID, "u1", ActionApprove, ActionInput{}); err != nil {
		t.Fatal(err)
	}

	var wg sync.WaitGroup
	errs := make([]error, 2)
	for i, actor := range []string{"u2", "u3"} {
		wg.Add(1)
		go func(i int, actor string) {
			defer wg.Done()
			_, errs[i] = svc.Act(ctx, res.Inst.ID, actor, ActionApprove, ActionInput{})
		}(i, actor)
	}
	wg.Wait()
	for i, err := range errs {
		if err != nil {
			t.Fatalf("并发审批 %d 失败: %v", i, err)
		}
	}
	out, err := svc.Get(ctx, res.Inst.ID)
	if err != nil {
		t.Fatal(err)
	}
	if out.State.Status != engine.StatusApproved {
		t.Fatalf("并发会签后状态 = %s", out.State.Status)
	}
}

func TestWithdrawOwnerOnly(t *testing.T) {
	svc, _, def := newTest(t)
	ctx := context.Background()
	res, err := svc.Start(ctx, StartInput{DefID: def.ID, Title: "撤回"}, "u1")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Act(ctx, res.Inst.ID, "u2", ActionWithdraw, ActionInput{}); !errors.Is(err, ErrNotOwner) {
		t.Fatalf("非提交人撤回应报 ErrNotOwner，实际 %v", err)
	}
	out, err := svc.Act(ctx, res.Inst.ID, "u1", ActionWithdraw, ActionInput{Comment: "不需要了"})
	if err != nil {
		t.Fatalf("提交人撤回失败: %v", err)
	}
	if out.State.Status != engine.StatusCancelled {
		t.Fatalf("状态 = %s", out.State.Status)
	}
}

func TestCountersignRequiresApprover(t *testing.T) {
	svc, _, def := newTest(t)
	ctx := context.Background()
	res, err := svc.Start(ctx, StartInput{DefID: def.ID, Title: "加签"}, "u1")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Act(ctx, res.Inst.ID, "stranger", ActionCountersign, ActionInput{Approver: "u9"}); !errors.Is(err, engine.ErrNotApprover) {
		t.Fatalf("非当前审批人加签应报 ErrNotApprover，实际 %v", err)
	}
	if _, err := svc.Act(ctx, res.Inst.ID, "u1", ActionCountersign, ActionInput{Approver: "u9", Comment: "请一起看"}); err != nil {
		t.Fatalf("当前审批人加签失败: %v", err)
	}
	_, total, _ := svc.ListMinePending(ctx, "u9", 1, 20)
	if total != 1 {
		t.Fatalf("加签后 u9 应有待办，实际 %d", total)
	}
}

func TestSweepEscalates(t *testing.T) {
	base := time.Date(2026, 1, 1, 9, 0, 0, 0, time.UTC)
	b := memory.New()
	b.SetClock(func() time.Time { return base })
	svc := New(b.Defs, b.Insts, b.Tasks, b.Actions)
	svc.Now = func() time.Time { return base }

	def := seqDef()
	def.Nodes[1].TimeoutMinutes = 60
	def.Nodes[1].EscalateTo = "n2"
	latest := seed(t, svc, def)

	res, err := svc.Start(context.Background(), StartInput{DefID: latest.ID, Title: "超时"}, "u1")
	if err != nil {
		t.Fatal(err)
	}

	later := base.Add(61 * time.Minute)
	b.SetClock(func() time.Time { return later })
	svc.Now = func() time.Time { return later }
	n, err := svc.Sweep(context.Background(), later)
	if err != nil || n != 1 {
		t.Fatalf("Sweep = %d, err=%v", n, err)
	}
	out, err := svc.Get(context.Background(), res.Inst.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(out.State.Current) != 1 || out.State.Current[0] != "n2" {
		t.Fatalf("超时后应升级到 n2，Current = %v", out.State.Current)
	}
	_, total, _ := svc.ListMinePending(context.Background(), "u2", 1, 20)
	if total != 1 {
		t.Fatalf("升级后 u2 应有待办，实际 %d", total)
	}
}
