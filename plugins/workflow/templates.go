package workflow

// templates.go 内置流程定义模板（不落库，供前端「新增定义」时套用）。
// 审批人为占位用户 ID（"1" = 超管），使用方在页面上改成真实用户。

// templateDefinitions 返回内置模板副本：覆盖「会签 + 加签 + 回退 + 超时升级」
// 与「排他网关条件分支」两种典型形态。
func templateDefinitions() []DefTemplate {
	return []DefTemplate{
		{
			Key:  "general_two_step",
			Name: "两级审批（或签 + 超时升级）",
			Definition: Definition{
				Key: "general_two_step", Name: "两级审批", Start: "start",
				Nodes: []Node{
					{Key: "start", Name: "开始", Kind: KindStart},
					{Key: "first", Name: "一级审批", Kind: KindApproval, Approvers: []string{"1"}, Mode: ModeAny, EscalateTo: "second", TimeoutMinutes: 1440},
					{Key: "second", Name: "二级审批", Kind: KindApproval, Approvers: []string{"1"}, Mode: ModeAny},
					{Key: "end", Name: "结束", Kind: KindEnd},
				},
				Edges: []Edge{
					{From: "start", To: "first"},
					{From: "first", To: "second"},
					{From: "second", To: "end"},
				},
			},
		},
		{
			Key:  "amount_gate",
			Name: "金额分级审批（排他网关）",
			Definition: Definition{
				Key: "amount_gate", Name: "金额分级审批", Start: "start",
				Nodes: []Node{
					{Key: "start", Name: "开始", Kind: KindStart},
					{Key: "apply", Name: "提交审批", Kind: KindApproval, Approvers: []string{"1"}, Mode: ModeAny},
					{Key: "gw", Name: "是否超限额", Kind: KindExclusive},
					{Key: "manager", Name: "主管审批", Kind: KindApproval, Approvers: []string{"1"}, Mode: ModeAny},
					{Key: "director", Name: "总监审批", Kind: KindApproval, Approvers: []string{"1"}, Mode: ModeAny},
					{Key: "end", Name: "结束", Kind: KindEnd},
				},
				Edges: []Edge{
					{From: "start", To: "apply"},
					{From: "apply", To: "gw"},
					{From: "gw", To: "director", When: "amount>50000"},
					{From: "gw", To: "manager", When: ""},
					{From: "manager", To: "end"},
					{From: "director", To: "end"},
				},
			},
		},
		{
			Key:  "joint_sign",
			Name: "会签（全部同意才通过）",
			Definition: Definition{
				Key: "joint_sign", Name: "会签审批", Start: "start",
				Nodes: []Node{
					{Key: "start", Name: "开始", Kind: KindStart},
					{Key: "joint", Name: "双人会签", Kind: KindApproval, Approvers: []string{"1", "2"}, Mode: ModeAll},
					{Key: "end", Name: "结束", Kind: KindEnd},
				},
				Edges: []Edge{
					{From: "start", To: "joint"},
					{From: "joint", To: "end"},
				},
			},
		},
	}
}

// 编译期断言：模板必须通过引擎校验（定义写错时 build 就会失败，而不是等到运行期）。
var _ = func() bool {
	for _, t := range templateDefinitions() {
		if errs := Validate(t.Definition); len(errs) > 0 {
			panic("内置流程模板非法: " + t.Key)
		}
	}
	return true
}()
