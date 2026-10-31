package ticket

import "testing"

func TestRuleMatches(t *testing.T) {
	tk := Ticket{Priority: PriorityHigh, BizType: "order", Title: "Refund for order 42"}
	cases := []struct {
		name string
		rule Rule
		want bool
	}{
		{"no conditions", Rule{}, true},
		{"priority hit", Rule{MatchPriority: PriorityHigh}, true},
		{"priority miss", Rule{MatchPriority: PriorityLow}, false},
		{"biz type hit", Rule{MatchBizType: "order"}, true},
		{"biz type miss", Rule{MatchBizType: "refund"}, false},
		{"keyword hit case-insensitive", Rule{MatchKeyword: "REFUND"}, true},
		{"keyword miss", Rule{MatchKeyword: "invoice"}, false},
		{"all conditions", Rule{MatchPriority: PriorityHigh, MatchBizType: "order", MatchKeyword: "42"}, true},
		{"one condition fails", Rule{MatchPriority: PriorityHigh, MatchKeyword: "invoice"}, false},
	}
	for _, c := range cases {
		if got := ruleMatches(c.rule, tk); got != c.want {
			t.Errorf("%s: got %v, want %v", c.name, got, c.want)
		}
	}
}

func TestRuleInputNormalize(t *testing.T) {
	var bad RuleInput
	if err := bad.normalize(); err == nil {
		t.Fatal("empty rule accepted")
	}
	// 没有动作
	bad = RuleInput{Name: "x"}
	if err := bad.normalize(); err == nil {
		t.Fatal("rule without actions accepted")
	}
	// 非法优先级
	bad = RuleInput{Name: "x", MatchPriority: "nonsense", SetPriority: PriorityHigh}
	if err := bad.normalize(); err == nil {
		t.Fatal("invalid match priority accepted")
	}
	good := RuleInput{Name: "  高优先转组  ", Sort: -1, MatchPriority: PriorityHigh, SetPriority: PriorityUrgent, SetGroupID: 9, Note: "  自动备注  "}
	if err := good.normalize(); err != nil {
		t.Fatal(err)
	}
	if good.Name != "高优先转组" || good.Sort != 0 || good.Note != "自动备注" {
		t.Fatalf("unexpected normalized rule: %+v", good)
	}
}
