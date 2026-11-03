package engine

import (
	"errors"
	"testing"
	"time"
)

// 转办：节点审批人整组替换为受让人，转办人随即失去审批权；
// 已投出的票保留；回退清除替换让节点回到定义的审批人。
func TestTransferReplacesApprovers(t *testing.T) {
	def := Definition{
		Key: "d", Start: "s",
		Nodes: []Node{
			{Key: "s", Kind: KindStart},
			{Key: "a", Kind: KindApproval, Approvers: []string{"u1"}, Mode: ModeAny},
			{Key: "b", Kind: KindApproval, Approvers: []string{"u2"}},
			{Key: "end", Kind: KindEnd},
		},
		Edges: []Edge{{From: "s", To: "a"}, {From: "a", To: "b"}, {From: "b", To: "end"}},
	}
	inst, err := Start(def, nil, "u0", time.Now())
	if err != nil {
		t.Fatal(err)
	}

	// 非审批人不能转办
	if err := Transfer(def, inst, "u2", "u9", "", time.Now()); !errors.Is(err, ErrNotApprover) {
		t.Fatalf("want ErrNotApprover, got %v", err)
	}
	// 受让人为空
	if err := Transfer(def, inst, "u1", "  ", "", time.Now()); err == nil {
		t.Fatal("empty assignee must fail")
	}

	if err := Transfer(def, inst, "u1", "u9", "", time.Now()); err != nil {
		t.Fatal(err)
	}
	tasks := PendingTasks(def, inst)
	if len(tasks) != 1 || tasks[0].Assignee != "u9" {
		t.Fatalf("待办应转给 u9, got %+v", tasks)
	}
	if err := Approve(def, inst, "u1", "", time.Now()); !errors.Is(err, ErrNotApprover) {
		t.Fatalf("转办后原审批人不应再能批, got %v", err)
	}
	if err := Approve(def, inst, "u9", "", time.Now()); err != nil {
		t.Fatalf("受让人审批失败: %v", err)
	}
	if inst.Status != StatusRunning || len(inst.Current) != 1 || inst.Current[0] != "b" {
		t.Fatalf("转办不应影响推进, status=%s current=%v", inst.Status, inst.Current)
	}
}

// 回退到已转办的节点：转办覆盖被清除，节点回到定义的审批人。
func TestRollbackClearsTransfer(t *testing.T) {
	def := Definition{
		Key: "d", Start: "s",
		Nodes: []Node{
			{Key: "s", Kind: KindStart},
			{Key: "a", Kind: KindApproval, Approvers: []string{"u1"}},
			{Key: "b", Kind: KindApproval, Approvers: []string{"u2"}},
			{Key: "end", Kind: KindEnd},
		},
		Edges: []Edge{{From: "s", To: "a"}, {From: "a", To: "b"}, {From: "b", To: "end"}},
	}
	inst, err := Start(def, nil, "u0", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if err := Approve(def, inst, "u1", "", time.Now()); err != nil {
		t.Fatal(err)
	}
	if err := Transfer(def, inst, "u2", "u9", "", time.Now()); err != nil {
		t.Fatal(err)
	}
	if err := Rollback(def, inst, "u0", "a", "退回重审", time.Now()); err != nil {
		t.Fatal(err)
	}
	if _, ok := inst.ReplacedApprovers["a"]; ok {
		t.Fatal("回退应清除该节点的转办覆盖")
	}
	tasks := PendingTasks(def, inst)
	if len(tasks) != 1 || tasks[0].Assignee != "u1" {
		t.Fatalf("回退后应回到 u1, got %+v", tasks)
	}
}
