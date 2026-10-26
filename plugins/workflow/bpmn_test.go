package workflow

import (
	"encoding/xml"
	"strings"
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
		"缺流程key":  func(d Definition) Definition { d.Key = ""; return d },
		"缺结束节点":   func(d Definition) Definition { d.Nodes = d.Nodes[:3]; return d },
		"开始节点类型错": func(d Definition) Definition { d.Nodes[0].Kind = KindApproval; return d },
		"节点key重复": func(d Definition) Definition { d.Nodes = append(d.Nodes, d.Nodes[1]); return d },
		"连线终点不存在": func(d Definition) Definition { d.Edges = append(d.Edges, Edge{From: "n1", To: "ghost"}); return d },
		"升级目标不存在": func(d Definition) Definition { d.Nodes[1].EscalateTo = "ghost"; return d },
		"审批模式非法":  func(d Definition) Definition { d.Nodes[1].Mode = "xor"; return d },
		"排他网关缺默认边": func(Definition) Definition {
			d := branchDef()
			d.Edges[3].When = "amount<=10000"
			return d
		},
		"节点不可达": func(d Definition) Definition {
			d.Nodes = append(d.Nodes, Node{Key: "orphan", Kind: KindApproval})
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
	if err := Approve(seqDef(), inst, "stranger", "", time.Now()); err == nil {
		t.Fatal("非审批人应被拒绝")
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
	if err := Approve(seqDef(), inst, "u2", "", now); err == nil {
		t.Fatal("结束后不应还能审批")
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
	if err := Escalate(branchDef(), inst, "system", "", time.Now()); err == nil {
		t.Fatal("未配置升级目标的节点应报错")
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

func TestExportXML(t *testing.T) {
	out, err := ExportXML(branchDef())
	if err != nil {
		t.Fatalf("导出失败: %v", err)
	}
	for _, want := range []string{"<bpmn:startEvent", "<bpmn:userTask", "<bpmn:exclusiveGateway", "<bpmn:endEvent", "conditionExpression", "amount&gt;10000", "bpmn:process"} {
		if !strings.Contains(out, want) {
			t.Errorf("XML 缺少 %q:\n%s", want, out)
		}
	}
	var parsed struct {
		XMLName xml.Name
		Process struct {
			ID string `xml:"id,attr"`
		} `xml:"process"`
	}
	if err := xml.Unmarshal([]byte(out), &parsed); err != nil {
		t.Fatalf("XML 不可解析: %v", err)
	}
	if parsed.Process.ID != "d2" {
		t.Fatalf("process id = %q", parsed.Process.ID)
	}
	if _, err := ExportXML(Definition{Key: "bad"}); err == nil {
		t.Fatal("非法定义导出应报错")
	}
}

// TestApproveNoApprovers 审批节点未配置审批人时，任意用户表态即完成。
// 回归：此前 Approve/Reject 用 contains(approvers, actor) 拦截，空 approvers 时恒 false，
// 导致该节点永久卡死、satisfied 的 len==0 分支成为死代码。
func TestApproveNoApprovers(t *testing.T) {
	def := Definition{
		Key: "d3", Name: "无审批人", Start: "start",
		Nodes: []Node{
			{Key: "start", Kind: KindStart},
			{Key: "a", Kind: KindApproval},
			{Key: "end", Kind: KindEnd},
		},
		Edges: []Edge{{From: "start", To: "a"}, {From: "a", To: "end"}},
	}
	inst := mustStart(t, def, nil)
	if err := Approve(def, inst, "anyone", "同意", time.Now()); err != nil {
		t.Fatalf("无审批人节点应放行任意审批人，实际报错: %v", err)
	}
	if inst.Status != StatusApproved {
		t.Fatalf("应自动流转通过，实际 status=%s current=%v", inst.Status, inst.Current)
	}
}
