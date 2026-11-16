package website

import "testing"

// TestProductTradeInfo 钉住「交易信息」从 AliTradeJSON 到模板视图的换算：
// 阶梯价区间是算出来的（1-9 / 10+），最后一段必须标 Last，否则模板会把 "10+"
// 渲染成 "≥10" 的同时还显示一个不存在的上界；包装体积按 cm³→m³ 换算。
func TestProductTradeInfo(t *testing.T) {
	empty := Product{}
	if tr := empty.TradeInfo(); len(tr.Ladder) != 0 || tr.Pkg != nil || tr.Sample != nil || tr.Supply != nil {
		t.Errorf("没有交易数据时应全部为空，实际 %+v", tr)
	}

	p := Product{
		PriceUnit: "Set/Sets",
		AliTradeJSON: `{"priceType":"ladder","ladder":[{"qty":1,"price":1200},{"qty":10,"price":1100}],` +
			`"package":{"length":120,"width":100,"height":220,"weight":900},` +
			`"supply":{"number":500,"quantityUnit":"Set/Sets","timeUnit":"Month"},` +
			`"sample":{"enabled":true,"maxQty":2,"price":1500},` +
			`"custom":[{"type":"Customized logo","quantity":100}]}`,
	}
	tr := p.TradeInfo()

	if len(tr.Ladder) != 2 {
		t.Fatalf("阶梯价应有 2 档，实际 %d", len(tr.Ladder))
	}
	if tr.Ladder[0].Range != "1-9" || tr.Ladder[0].Last {
		t.Errorf("第一档区间应为 1-9 且不是最后一档，实际 %+v", tr.Ladder[0])
	}
	if tr.Ladder[1].Range != "10+" || !tr.Ladder[1].Last {
		t.Errorf("最后一档应为 10+ 且 Last=true，实际 %+v", tr.Ladder[1])
	}
	if tr.Pkg == nil || tr.Pkg.Dim != "120 × 100 × 220 cm" {
		t.Errorf("包装尺寸应格式化成 cm，实际 %+v", tr.Pkg)
	}
	if tr.Pkg != nil && tr.Pkg.Cubic != 2.64 {
		t.Errorf("体积应为 2.64 m³，实际 %v", tr.Pkg.Cubic)
	}
	if tr.Supply == nil || tr.Supply.Number != 500 || tr.Supply.Period != "Month" {
		t.Errorf("供货能力解析错误，实际 %+v", tr.Supply)
	}
	if tr.Sample == nil || tr.Sample.MaxQty != 2 || tr.Sample.Price != 1500 {
		t.Errorf("样品服务解析错误，实际 %+v", tr.Sample)
	}
	if len(tr.Custom) != 1 || tr.Custom[0].Quantity != 100 {
		t.Errorf("定制服务解析错误，实际 %+v", tr.Custom)
	}

	// 坏 JSON 不能把详情页搞挂：返回空视图
	bad := Product{AliTradeJSON: "{not json"}
	if tr := bad.TradeInfo(); len(tr.Ladder) != 0 || tr.Pkg != nil {
		t.Errorf("坏 JSON 应返回空视图，实际 %+v", tr)
	}
}
