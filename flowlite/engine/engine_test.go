package engine

import (
	"errors"
	"testing"
	"time"
)

// seqDef 是两级审批：n1 或签（可升级到 n2），n2 会签（u2、u3 都要同意）。
func seqDef() Definition {
	return Definition{
		Key: "d1", Name: "两级会签", Start: "start",
		Nodes: []Node{
			{Key: "start", Name: "开始", Kind: KindStart},
			{Key: "n1", Name: "保管员", Kind: KindApproval, Approvers: []string{"u1"}, Mode: ModeAny, EscalateTo: "n2"},
			{Key: "n2", Name: "科长", Kind: KindApproval, Approvers: []string{"u2", "u3"}, Mode: ModeAll},
			{Key: "end", Name: "结束", Kind: KindEnd},
		},
		Edges: []Edge{
			{From: "start", To: "n1"},
			{From: "n1", To: "n2"},
			{From: "n2", To: "end"},
		},
	}
}

// branchDef 含排他网关：金额 > 10000 走 boss，否则走 clerk。
func branchDef() Definition {
	return Definition{
		Key: "d2", Name: "金额分支", Start: "start",
		Nodes: []Node{
			{Key: "start", Kind: KindStart},
			{Key: "n1", Kind: KindApproval, Approvers: []string{"u1"}},
			{Key: "g", Kind: KindExclusive},
			{Key: "hi", Kind: KindApproval, Approvers: []string{"boss"}},
			{Key: "lo", Kind: KindApproval, Approvers: []string{"clerk"}},
			{Key: "end", Kind: KindEnd},
		},
		Edges: []Edge{
			{From: "start", To: "n1"},
			{From: "n1", To: "g"},
			{From: "g", To: "hi", When: "amount>10000"},
			{From: "g", To: "lo", When: ""},
			{From: "hi", To: "end"},
			{From: "lo", To: "end"},
		},
	}
}

func mustStart(t *testing.T, def Definition, vars map[string]string) *Instance {
	t.Helper()
	inst, err := Start(def, vars, "u1", time.Now())
	if err != nil {
		t.Fatalf("Start 失败: %v", err)
	}
	return inst
}

func TestValidateOK(t *testing.T) {
	if errs := Validate(seqDef()); len(errs) != 0 {
		t.Fatalf("合法定义被报错: %v", errs)
	}
	if errs := Validate(branchDef()); len(errs) != 0 {
		t.Fatalf("合法定义被报错: %v", errs)
	}
}

func TestValidateErrors(t *testing.T) {
	cases := map[string]func(Definition) Definition{
		"缺流程key":   func(d Definition) Definition { d.Key = ""; return d },
		"缺结束节点":    func(d Definition) Definition { d.Nodes = d.Nodes[:3]; return d },
		"开始节点类型错":  func(d Definition) Definition { d.Nodes[0].Kind = KindApproval; return d },
		"节点key重复":  func(d Definition) Definition { d.Nodes = append(d.Nodes, d.Nodes[1]); return d },
		"连线终点不存在":  func(d Definition) Definition { d.Edges = append(d.Edges, Edge{From: "n1", To: "ghost"}); return d },
		"升级目标不存在":  func(d Definition) Definition { d.Nodes[1].EscalateTo = "ghost"; return d },
		"升级目标不是审批": func(d Definition) Definition { d.Nodes[1].EscalateTo = "end"; return d },
		"审批模式非法":   func(d Definition) Definition { d.Nodes[1].Mode = "xor"; return d },
		"审批人含空值":   func(d Definition) Definition { d.Nodes[1].Approvers = []string{"u1", " "}; return d },
		"排他网关缺默认边": func(Definition) Definition {
			d := branchDef()
			d.Edges[3].When = "amount<=10000"
			return d
		},
		"节点不可达": func(d Definition) Definition {
			d.Nodes = append(d.Nodes, Node{Key: "orphan", Kind: KindEnd})
			return d
		},
		"结束节点有出边": func(d Definition) Definition {
			d.Edges = append(d.Edges, Edge{From: "end", To: "n1"})
			return d
		},
		"开始节点有入边": func(d Definition) Definition {
			d.Edges = append(d.Edges, Edge{From: "n2", To: "start"})
			return d
		},
		"非结束节点无出边": func(d Definition) Definition {
			d.Edges = d.Edges[:2]
			return d
		},
		"重复连线": func(d Definition) Definition {
			d.Edges = append(d.Edges, Edge{From: "start", To: "n1"})
			return d
		},
		"多个开始节点": func(d Definition) Definition {
			d.Nodes = append(d.Nodes, Node{Key: "start2", Kind: KindStart})
			d.Edges = append(d.Edges, Edge{From: "start2", To: "n1"})
			return d
		},
	}
	for name, mut := range cases {
		if errs := Validate(mut(seqDef())); len(errs) == 0 {
			t.Errorf("%s: 期望报错，实际通过", name)
		}
	}
}

func TestStartStopsAtFirstApproval(t *testing.T) {
	inst := mustStart(t, seqDef(), nil)
	if inst.Status != StatusRunning {
		t.Fatalf("状态 = %s", inst.Status)
	}
	if len(inst.Current) != 1 || inst.Current[0] != "n1" {
		t.Fatalf("Current = %v", inst.Current)
	}
	if len(inst.Done) != 1 || inst.Done[0] != "start" {
		t.Fatalf("Done = %v", inst.Done)
	}
	if inst.Revision != 1 {
		t.Fatalf("Revision = %d, want 1", inst.Revision)
	}
}

func TestApproveAnyThenAll(t *testing.T) {
	inst := mustStart(t, seqDef(), nil)
	now := time.Now()

	if err := Approve(seqDef(), inst, "u1", "同意", now); err != nil {
		t.Fatalf("n1 通过失败: %v", err)
	}
	if len(inst.Current) != 1 || inst.Current[0] != "n2" {
		t.Fatalf("进入 n2 失败: %v", inst.Current)
	}
	if inst.Revision != 2 {
		t.Fatalf("Revision = %d, want 2", inst.Revision)
	}
	if err := Approve(seqDef(), inst, "u2", "", now); err != nil {
		t.Fatalf("n2 u2 通过失败: %v", err)
	}
	if inst.Status != StatusRunning {
		t.Fatalf("会签未齐就通过了: %s", inst.Status)
	}
	if err := Approve(seqDef(), inst, "u3", "", now); err != nil {
		t.Fatalf("n2 u3 通过失败: %v", err)
	}
	if inst.Status != StatusApproved {
		t.Fatalf("会签齐了未通过: %s", inst.Status)
	}
	if len(inst.Current) != 0 {
		t.Fatalf("Current 应为空: %v", inst.Current)
	}
}

func TestNonApproverRejected(t *testing.T) {
	inst := mustStart(t, seqDef(), nil)
	err := Approve(seqDef(), inst, "stranger", "", time.Now())
	if err == nil {
		t.Fatal("非审批人应被拒绝")
	}
	if !errors.Is(err, ErrNotApprover) {
		t.Fatalf("错误类型 = %v, want ErrNotApprover", err)
	}
	if err := Reject(seqDef(), inst, "stranger", "", time.Now()); !errors.Is(err, ErrNotApprover) {
		t.Fatalf("非审批人驳回的错误类型 = %v, want ErrNotApprover", err)
	}
}

func TestRejectTerminates(t *testing.T) {
	inst := mustStart(t, seqDef(), nil)
	now := time.Now()
	if err := Approve(seqDef(), inst, "u1", "", now); err != nil {
		t.Fatal(err)
	}
	if err := Reject(seqDef(), inst, "u2", "不合格", now); err != nil {
		t.Fatalf("驳回失败: %v", err)
	}
	if inst.Status != StatusRejected {
		t.Fatalf("状态 = %s", inst.Status)
	}
	if err := Approve(seqDef(), inst, "u2", "", now); !errors.Is(err, ErrNotRunning) {
		t.Fatalf("结束后应返回 ErrNotRunning，实际 %v", err)
	}
}

func TestCountersignAddsApprover(t *testing.T) {
	inst := mustStart(t, seqDef(), nil)
	now := time.Now()
	if err := Approve(seqDef(), inst, "u1", "", now); err != nil {
		t.Fatal(err)
	}
	if err := Countersign(seqDef(), inst, "u2", "u4", "请会签", now); err != nil {
		t.Fatalf("加签失败: %v", err)
	}
	if err := Approve(seqDef(), inst, "u2", "", now); err != nil {
		t.Fatal(err)
	}
	if err := Approve(seqDef(), inst, "u3", "", now); err != nil {
		t.Fatal(err)
	}
	if inst.Status != StatusRunning {
		t.Fatalf("加签人未签就通过了: %s", inst.Status)
	}
	if err := Approve(seqDef(), inst, "u4", "", now); err != nil {
		t.Fatalf("加签人审批失败: %v", err)
	}
	if inst.Status != StatusApproved {
		t.Fatalf("状态 = %s", inst.Status)
	}
}

func TestRollbackClearsVotes(t *testing.T) {
	def := seqDef()
	inst := mustStart(t, def, nil)
	now := time.Now()
	if err := Approve(def, inst, "u1", "", now); err != nil {
		t.Fatal(err)
	}
	if err := Rollback(def, inst, "u2", "n1", "退回补充材料", now); err != nil {
		t.Fatalf("回退失败: %v", err)
	}
	if len(inst.Current) != 1 || inst.Current[0] != "n1" {
		t.Fatalf("Current = %v", inst.Current)
	}
	if _, ok := inst.Votes["n1"]; ok {
		t.Fatal("回退后应清除 n1 的投票")
	}
	if err := Rollback(def, inst, "u2", "end", "", now); err == nil {
		t.Fatal("回退到结束节点应报错")
	}
	if err := Approve(def, inst, "u1", "", now); err != nil {
		t.Fatalf("回退后重新审批失败: %v", err)
	}
	if len(inst.Current) != 1 || inst.Current[0] != "n2" {
		t.Fatalf("Current = %v", inst.Current)
	}
}

func TestEscalateMovesToTarget(t *testing.T) {
	def := seqDef()
	inst := mustStart(t, def, nil)
	if err := Escalate(def, inst, "system", "超 24 小时未处理", time.Now()); err != nil {
		t.Fatalf("升级失败: %v", err)
	}
	if len(inst.Current) != 1 || inst.Current[0] != "n2" {
		t.Fatalf("Current = %v", inst.Current)
	}
	if err := Escalate(branchDef(), mustStart(t, branchDef(), nil), "system", "", time.Now()); !errors.Is(err, ErrNoEscalateTarget) {
		t.Fatalf("未配置升级目标应返回 ErrNoEscalateTarget，实际 %v", err)
	}
}

func TestWithdraw(t *testing.T) {
	inst := mustStart(t, seqDef(), nil)
	if err := Withdraw(inst, "u1", "不需要了", time.Now()); err != nil {
		t.Fatalf("撤回失败: %v", err)
	}
	if inst.Status != StatusCancelled {
		t.Fatalf("状态 = %s", inst.Status)
	}
	if err := Withdraw(inst, "u1", "", time.Now()); !errors.Is(err, ErrNotRunning) {
		t.Fatalf("重复撤回应返回 ErrNotRunning，实际 %v", err)
	}
}

func TestExclusiveBranch(t *testing.T) {
	def := branchDef()
	now := time.Now()

	hi := mustStart(t, def, map[string]string{"amount": "20000"})
	if err := Approve(def, hi, "u1", "", now); err != nil {
		t.Fatal(err)
	}
	if len(hi.Current) != 1 || hi.Current[0] != "hi" {
		t.Fatalf("大额应走 hi: %v", hi.Current)
	}

	lo := mustStart(t, def, map[string]string{"amount": "100"})
	if err := Approve(def, lo, "u1", "", now); err != nil {
		t.Fatal(err)
	}
	if len(lo.Current) != 1 || lo.Current[0] != "lo" {
		t.Fatalf("小额应走 lo: %v", lo.Current)
	}
}

func TestEvalCondition(t *testing.T) {
	vars := map[string]string{"amount": "5000", "level": "high"}
	cases := []struct {
		expr string
		want bool
	}{
		{"", true},
		{"amount>10000", false},
		{"amount>=5000", true},
		{"amount<=4999", false},
		{"level==high", true},
		{"level!=low", true},
		{"missing>1", false},
		{"amount", false},
		{"==5", false},
	}
	for _, c := range cases {
		if got := EvalCondition(c.expr, vars); got != c.want {
			t.Errorf("EvalCondition(%q) = %v, want %v", c.expr, got, c.want)
		}
	}
}

// TestAnyoneWhenNoApprovers 审批节点未配置审批人时是「公开任务」：同意与驳回口径一致。
// 回归：旧实现 Approve 放行空审批人、Reject 却严格匹配，导致公开任务永远驳不回。
func TestAnyoneWhenNoApprovers(t *testing.T) {
	def := Definition{
		Key: "d3", Name: "无审批人", Start: "start",
		Nodes: []Node{
			{Key: "start", Kind: KindStart},
			{Key: "a", Kind: KindApproval},
			{Key: "end", Kind: KindEnd},
		},
		Edges: []Edge{{From: "start", To: "a"}, {From: "a", To: "end"}},
	}
	approved := mustStart(t, def, nil)
	if err := Approve(def, approved, "anyone", "同意", time.Now()); err != nil {
		t.Fatalf("公开任务应放行任意审批人，实际报错: %v", err)
	}
	if approved.Status != StatusApproved {
		t.Fatalf("应自动流转通过，实际 status=%s current=%v", approved.Status, approved.Current)
	}

	rejected := mustStart(t, def, nil)
	if err := Reject(def, rejected, "anyone", "不同意", time.Now()); err != nil {
		t.Fatalf("公开任务应允许任意人驳回，实际报错: %v", err)
	}
	if rejected.Status != StatusRejected {
		t.Fatalf("状态 = %s, want rejected", rejected.Status)
	}
}

func TestPendingTasksAndActiveApprover(t *testing.T) {
	def := seqDef()
	inst := mustStart(t, def, nil)
	tasks := PendingTasks(def, inst)
	if len(tasks) != 1 || tasks[0].Assignee != "u1" || tasks[0].NodeKey != "n1" {
		t.Fatalf("PendingTasks = %+v", tasks)
	}
	if tasks[0].NodeName != "保管员" {
		t.Fatalf("NodeName = %q", tasks[0].NodeName)
	}
	if !IsActiveApprover(def, inst, "u1") {
		t.Fatal("u1 应是当前审批人")
	}
	if IsActiveApprover(def, inst, "u2") {
		t.Fatal("u2 不是当前审批人")
	}
	if err := Approve(def, inst, "u1", "", time.Now()); err != nil {
		t.Fatal(err)
	}
	tasks = PendingTasks(def, inst)
	if len(tasks) != 2 {
		t.Fatalf("会签节点应有 2 条待办，实际 %+v", tasks)
	}
}
