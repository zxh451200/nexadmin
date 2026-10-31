package engine

import (
	"errors"
	"strings"
	"testing"
	"time"
)

// 本文件补充覆盖 engine.go / model.go / condition.go 的未覆盖分支：
// 直接以白盒方式调用引擎内部函数，并手工构造实例，覆盖 Validate 的各类错误、
// autoAdvance / follow 的异常出口、加签 / 回退 / 升级的错误分支与语义。

func nowT() time.Time { return time.Date(2026, 10, 22, 9, 30, 0, 0, time.UTC) }

// runningInst 造一个「运行中」的最小实例，供直接调用内部函数时使用。
func runningInst(current ...string) *Instance {
	return &Instance{
		Status:         StatusRunning,
		Current:        current,
		Done:           []string{},
		Votes:          map[string]Vote{},
		ExtraApprovers: map[string][]string{},
		Vars:           map[string]string{},
	}
}

func hasErr(errs []string, sub string) bool {
	for _, e := range errs {
		if strings.Contains(e, sub) {
			return true
		}
	}
	return false
}

// rollbackDef 是带头排他网关的三级定义：start -> a -> g(条件分支) -> b/c -> end。
func rollbackDef() Definition {
	return Definition{
		Key: "d4", Name: "回退可达集", Start: "start",
		Nodes: []Node{
			{Key: "start", Kind: KindStart},
			{Key: "a", Name: "一级", Kind: KindApproval, Approvers: []string{"u1"}},
			{Key: "g", Kind: KindExclusive},
			{Key: "b", Name: "大额", Kind: KindApproval, Approvers: []string{"u2"}},
			{Key: "c", Name: "兜底", Kind: KindApproval, Approvers: []string{"u3"}},
			{Key: "end", Kind: KindEnd},
		},
		Edges: []Edge{
			{From: "start", To: "a"},
			{From: "a", To: "g"},
			{From: "g", To: "b", When: "amount>1000"},
			{From: "g", To: "c", When: ""},
			{From: "b", To: "end"},
			{From: "c", To: "end"},
		},
	}
}

// TestStartRejectsInvalidDefinition Start 内部再次校验定义，非法定义直接返回错误。
func TestStartRejectsInvalidDefinition(t *testing.T) {
	inst, err := Start(Definition{Key: ""}, nil, "u1", nowT())
	if err == nil || inst != nil {
		t.Fatalf("非法定义应被 Start 拒绝：inst=%v err=%v", inst, err)
	}
	if !errors.Is(err, ErrInvalidDefinition) {
		t.Fatalf("错误类型不符：%v", err)
	}
}

// TestAutoAdvanceUnknownNodeErrors autoAdvance 遇到定义中不存在的节点时报错。
func TestAutoAdvanceUnknownNodeErrors(t *testing.T) {
	inst := &Instance{Current: []string{"ghost"}, Votes: map[string]Vote{}, Vars: map[string]string{}}
	if err := autoAdvance(seqDef(), inst); err == nil || !errors.Is(err, ErrUnknownNode) {
		t.Fatalf("未知节点应报 ErrUnknownNode，实际 %v", err)
	}
}

// TestAutoAdvanceExclusiveNoMatchingBranch 排他网关所有分支条件都不命中时报错（含默认流缺失）。
func TestAutoAdvanceExclusiveNoMatchingBranch(t *testing.T) {
	def := Definition{
		Nodes: []Node{{Key: "g", Kind: KindExclusive}},
		Edges: []Edge{{From: "g", To: "x", When: "flag==yes"}},
	}
	inst := &Instance{Current: []string{"g"}, Votes: map[string]Vote{}, Vars: map[string]string{}}
	if err := autoAdvance(def, inst); err == nil || !errors.Is(err, ErrNoBranch) {
		t.Fatalf("无命中分支应报 ErrNoBranch，实际 %v", err)
	}
}

// TestAutoAdvanceTokenDisappears 无出边的开始节点让 token 消失，实例自动通过。
func TestAutoAdvanceTokenDisappears(t *testing.T) {
	def := Definition{Nodes: []Node{{Key: "s", Kind: KindStart}}}
	inst := &Instance{Current: []string{"s"}, Votes: map[string]Vote{}, Vars: map[string]string{}}
	if err := autoAdvance(def, inst); err != nil {
		t.Fatalf("无出边的开始节点不应报错：%v", err)
	}
	if inst.Status != StatusApproved || len(inst.Current) != 0 {
		t.Fatalf("token 消失后应自动通过：status=%s current=%v", inst.Status, inst.Current)
	}
}

// TestActiveApprovalAmbiguous 同时存在多个活动审批节点时无法确定操作目标。
func TestActiveApprovalAmbiguous(t *testing.T) {
	def := Definition{Nodes: []Node{{Key: "a", Kind: KindApproval}, {Key: "b", Kind: KindApproval}}}
	if _, err := activeApproval(def, &Instance{Current: []string{"a", "b"}}); err == nil {
		t.Fatal("多个活动审批节点应报错")
	}
}

// TestSatisfiedModes 逐条覆盖 satisfied 的 any/all 判定，含「无人同意」的兜底返回。
func TestSatisfiedModes(t *testing.T) {
	votes := Vote{"a": VoteApprove, "b": VoteReject}
	if !satisfied(ModeAny, []string{"a", "b"}, votes) {
		t.Fatal("any 任一同意应满足")
	}
	if satisfied(ModeAny, []string{"b"}, votes) {
		t.Fatal("any 无人同意不应满足")
	}
	if satisfied(ModeAll, []string{"a", "b"}, votes) {
		t.Fatal("all 有人否决不应满足")
	}
	if !satisfied(ModeAll, []string{"a"}, votes) {
		t.Fatal("all 全部同意应满足")
	}
	if satisfied(ModeAny, nil, Vote{}) {
		t.Fatal("无审批人且无人表态不应满足")
	}
	if !satisfied(ModeAll, nil, Vote{"x": VoteApprove}) {
		t.Fatal("无审批人时有一人表态即满足")
	}
}

// TestApproveErrors 覆盖 Approve 的「已结束」与「无活动审批节点」错误分支。
func TestApproveErrors(t *testing.T) {
	def := seqDef()
	now := nowT()
	if err := Approve(def, &Instance{Status: StatusApproved}, "u1", "", now); err == nil {
		t.Fatal("已结束实例不应可审批")
	}
	if err := Approve(def, runningInst(), "u1", "", now); err == nil {
		t.Fatal("无活动审批节点时应报错")
	}
}

// TestRejectErrors 覆盖 Reject 的错误分支，并验证 nodeName 在节点无 name 时回退到 key。
func TestRejectErrors(t *testing.T) {
	def := seqDef()
	now := nowT()
	if err := Reject(def, &Instance{Status: StatusCancelled}, "u1", "", now); err == nil {
		t.Fatal("已结束实例不应可驳回")
	}
	if err := Reject(def, runningInst(), "u1", "", now); err == nil {
		t.Fatal("无活动审批节点时应报错")
	}
	noName := Definition{Nodes: []Node{{Key: "a", Kind: KindApproval, Approvers: []string{"u1"}}}}
	err := Reject(noName, runningInst("a"), "stranger", "", now)
	if !errors.Is(err, ErrNotApprover) {
		t.Fatalf("非审批人驳回应报 ErrNotApprover，实际 %v", err)
	}
}

// TestCountersignErrorsAndExtras 覆盖加签的错误分支、ExtraApprovers 懒初始化与默认意见文案。
func TestCountersignErrorsAndExtras(t *testing.T) {
	def := seqDef()
	now := nowT()
	if err := Countersign(def, &Instance{Status: StatusRejected}, "u1", "u2", "", now); err == nil {
		t.Fatal("已结束实例不应可加签")
	}
	if err := Countersign(def, runningInst("n1"), "u1", "   ", "", now); err == nil {
		t.Fatal("空加签人应报错")
	}
	if err := Countersign(def, runningInst(), "u1", "u2", "", now); err == nil {
		t.Fatal("无活动审批节点时应报错")
	}

	one := Definition{Nodes: []Node{{Key: "a", Kind: KindApproval, Approvers: []string{"u1"}}}}
	inst := &Instance{Status: StatusRunning, Current: []string{"a"}, Votes: map[string]Vote{}}
	if err := Countersign(one, inst, "u1", "u2", "", now); err != nil {
		t.Fatalf("加签失败：%v", err)
	}
	if got := inst.ExtraApprovers["a"]; len(got) != 1 || got[0] != "u2" {
		t.Fatalf("加签人未记录：%v", inst.ExtraApprovers)
	}
	if last := inst.History[len(inst.History)-1]; last.Comment != "加签 u2" {
		t.Fatalf("空意见应回退默认文案，实际 %q", last.Comment)
	}
	if err := Countersign(one, inst, "u1", "u2", "再次加签", now); err != nil {
		t.Fatalf("重复加签失败：%v", err)
	}
	if got := inst.ExtraApprovers["a"]; len(got) != 1 {
		t.Fatalf("重复加签同一人不应追加：%v", got)
	}
}

// TestRollbackErrors 覆盖回退的错误分支。
func TestRollbackErrors(t *testing.T) {
	def := seqDef()
	now := nowT()
	if err := Rollback(def, &Instance{Status: StatusApproved}, "u1", "n1", "", now); err == nil {
		t.Fatal("已结束实例不应可回退")
	}
	if err := Rollback(def, runningInst("n1"), "u1", "  ", "", now); err == nil {
		t.Fatal("空回退目标应报错")
	}
	if err := Rollback(def, runningInst("n1"), "u1", "ghost", "", now); err == nil {
		t.Fatal("回退目标不存在应报错")
	}
	if err := Rollback(def, runningInst("n1"), "u1", "start", "", now); err == nil {
		t.Fatal("只能回退到审批节点")
	}
}

// TestRollbackClearsReachableVotes 回退按「从目标节点可达的集合」清空投票、加签与完成标记。
func TestRollbackClearsReachableVotes(t *testing.T) {
	def := rollbackDef()
	now := nowT()
	inst := mustStart(t, def, map[string]string{"amount": "5000"})
	if err := Approve(def, inst, "u1", "", now); err != nil {
		t.Fatalf("一级审批失败：%v", err)
	}
	if len(inst.Current) != 1 || inst.Current[0] != "b" {
		t.Fatalf("大额应进入 b：%v", inst.Current)
	}
	inst.Votes["b"] = Vote{"u2": VoteReject}
	inst.Votes["c"] = Vote{"u3": VoteApprove}
	inst.ExtraApprovers["c"] = []string{"u9"}
	inst.Done = appendUnique(inst.Done, "c")

	if err := Rollback(def, inst, "u2", "a", "退回重审", now); err != nil {
		t.Fatalf("回退失败：%v", err)
	}
	if len(inst.Current) != 1 || inst.Current[0] != "a" {
		t.Fatalf("Current = %v", inst.Current)
	}
	for _, k := range []string{"a", "b", "c"} {
		if _, ok := inst.Votes[k]; ok {
			t.Fatalf("回退应清除 %s 的投票", k)
		}
		if _, ok := inst.ExtraApprovers[k]; ok {
			t.Fatalf("回退应清除 %s 的加签", k)
		}
	}
	if len(inst.Done) != 1 || inst.Done[0] != "start" {
		t.Fatalf("Done 应只剩 start：%v", inst.Done)
	}
}

// TestEscalateErrors 覆盖升级的错误分支。
func TestEscalateErrors(t *testing.T) {
	def := seqDef()
	now := nowT()
	if err := Escalate(def, &Instance{Status: StatusCancelled}, "system", "", now); err == nil {
		t.Fatal("已结束实例不应可升级")
	}
	noTarget := Definition{Nodes: []Node{{Key: "a", Kind: KindApproval, Approvers: []string{"u1"}}}}
	if err := Escalate(noTarget, runningInst("a"), "system", "", now); err == nil {
		t.Fatal("未配置升级目标应报错")
	}
	ghost := Definition{Nodes: []Node{{Key: "a", Kind: KindApproval, EscalateTo: "ghost"}}}
	if err := Escalate(ghost, runningInst("a"), "system", "", now); err == nil {
		t.Fatal("升级目标不存在应报错")
	}
}

// TestEscalateClearsTargetVotes 升级到目标节点时清空该节点已有的投票 / 加签与完成标记。
func TestEscalateClearsTargetVotes(t *testing.T) {
	def := seqDef()
	now := nowT()
	inst := mustStart(t, def, nil)
	inst.Votes["n2"] = Vote{"u2": VoteApprove}
	inst.ExtraApprovers["n2"] = []string{"u3"}
	inst.Done = appendUnique(inst.Done, "n2")

	if err := Escalate(def, inst, "system", "超时升级", now); err != nil {
		t.Fatalf("升级失败：%v", err)
	}
	if len(inst.Current) != 1 || inst.Current[0] != "n2" {
		t.Fatalf("Current = %v", inst.Current)
	}
	if _, ok := inst.Votes["n2"]; ok {
		t.Fatal("升级应清除目标节点投票")
	}
	if _, ok := inst.ExtraApprovers["n2"]; ok {
		t.Fatal("升级应清除目标节点加签")
	}
	if contains(inst.Done, "n2") {
		t.Fatal("升级应把目标节点从 Done 移除")
	}
}

// TestAppendUniqueKeepsExisting 已存在的元素不再追加（保持原切片长度）。
func TestAppendUniqueKeepsExisting(t *testing.T) {
	got := appendUnique([]string{"a", "b"}, "a")
	if len(got) != 2 || got[0] != "a" || got[1] != "b" {
		t.Fatalf("重复元素不应追加：%v", got)
	}
}

// TestCompareExtraOperators 覆盖 compare 的数字比较全部运算符分支与不可识别运算符的兜底。
func TestCompareExtraOperators(t *testing.T) {
	cases := []struct {
		op, a, b string
		want     bool
	}{
		{"<", "1", "2", true},
		{"<", "2", "1", false},
		{"==", "5", "5.0", true},
		{"!=", "5", "6", true},
		{"!=", "5", "5", false},
		{"~", "1", "2", false}, // 数字可比但运算符不可识别
		{"~", "a", "b", false}, // 非数字且运算符不可识别
	}
	for _, c := range cases {
		if got := compare(c.op, c.a, c.b); got != c.want {
			t.Errorf("compare(%q,%q,%q) = %v，期望 %v", c.op, c.a, c.b, got, c.want)
		}
	}
}

// TestEvalConditionExtraOps 通过公开入口覆盖数字 <、==、!= 与字符串不支持的大小运算。
func TestEvalConditionExtraOps(t *testing.T) {
	vars := map[string]string{"amount": "5000", "level": "high"}
	cases := []struct {
		expr string
		want bool
	}{
		{"amount<10000", true},
		{"amount<1000", false},
		{"amount==5000", true},
		{"amount!=6000", true},
		{"level>zzz", false},
	}
	for _, c := range cases {
		if got := EvalCondition(c.expr, vars); got != c.want {
			t.Errorf("EvalCondition(%q) = %v，期望 %v", c.expr, got, c.want)
		}
	}
}

// TestValidateEmptyKeyAndExclusiveNoOutEdge 覆盖空 key 节点、排他网关无出边、
// 以及可达性检查里跳过空 key 节点的 continue 分支。
func TestValidateEmptyKeyAndExclusiveNoOutEdge(t *testing.T) {
	def := seqDef()
	def.Nodes = append(def.Nodes,
		Node{Key: "", Kind: KindApproval},
		Node{Key: "g2", Kind: KindExclusive},
	)
	errs := Validate(def)
	if !hasErr(errs, "存在 key 为空的节点") {
		t.Fatalf("应报空 key 节点：%v", errs)
	}
	if !hasErr(errs, "g2 没有出边") {
		t.Fatalf("应报排他网关无出边：%v", errs)
	}
}

// TestValidateMoreErrors 覆盖节点类型非法、未指定 / 不存在的开始节点、连线起点不存在。
func TestValidateMoreErrors(t *testing.T) {
	badKind := seqDef()
	badKind.Nodes[1].Kind = NodeKind("weird")
	if errs := Validate(badKind); !hasErr(errs, "类型非法") {
		t.Fatalf("应报节点类型非法：%v", errs)
	}

	noStart := seqDef()
	noStart.Start = ""
	if errs := Validate(noStart); !hasErr(errs, "未指定开始节点") {
		t.Fatalf("应报未指定开始节点：%v", errs)
	}

	ghostStart := seqDef()
	ghostStart.Start = "ghost"
	if errs := Validate(ghostStart); !hasErr(errs, "开始节点不存在") {
		t.Fatalf("应报开始节点不存在：%v", errs)
	}

	ghostFrom := seqDef()
	ghostFrom.Edges = append(ghostFrom.Edges, Edge{From: "ghost", To: "n1"})
	if errs := Validate(ghostFrom); !hasErr(errs, "连线起点不存在") {
		t.Fatalf("应报连线起点不存在：%v", errs)
	}
}
