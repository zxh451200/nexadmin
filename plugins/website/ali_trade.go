package website

// ali_trade.go 是「发品到阿里」用的交易与规格数据：阶梯价、包装与物流、供货能力、样品、
// 定制服务、商品分组、销售属性与 SKU。
//
// 为什么一个 JSON 列：这些字段只在同步阿里（或按阿里模板导出）时使用，本地商城详情页不展示，
// 拆成十几个列会让产品表变成阿里的镜像；结构与校验集中在这里，schema 映射在 ali_map_trade.go。
// 字段口径对齐官方《商品发布接入文档》3.5（交易）/ 3.6（物流）/ 3.7（特殊服务）。

import (
	"encoding/json"
	"sort"
	"strings"

	"nexadmin/core/response"
)

// AliLadder 是阶梯价的一档：起订量 → 单价。
type AliLadder struct {
	Qty   int     `json:"qty"`
	Price float64 `json:"price"`
}

// AliPackage 是包装信息（官方 3.6.4：pkgMeasure 单位为 cm，pkgWeight 单位为 kg）。
type AliPackage struct {
	Length           float64 `json:"length"`
	Width            float64 `json:"width"`
	Height           float64 `json:"height"`
	Weight           float64 `json:"weight"`
	ShippingTemplate string  `json:"shippingTemplate"` // 阿里运费模板 ID（下单品 Pkg/物流必填）
}

// AliSupply 是供货能力（官方 3.6.2，询盘品特有）：例如 1000 Set / Month。
type AliSupply struct {
	Number       float64 `json:"number"`
	QuantityUnit string  `json:"quantityUnit"`
	TimeUnit     string  `json:"timeUnit"`
}

// AliSample 是样品服务（官方 3.5.9，下单品特有）。
type AliSample struct {
	Enabled     bool    `json:"enabled"`
	MaxQty      int     `json:"maxQty"`
	Price       float64 `json:"price"`
	Description string  `json:"description"`
}

// AliCustom 是一条定制服务（官方 3.7.1）：定制项 → 起订量。
type AliCustom struct {
	Type     string `json:"type"` // 官方预设：Customized logo / Customized packaging / Graphic customization，也可自定义
	Quantity int    `json:"quantity"`
}

// AliSKUProp 是一条销售属性（官方 3.5.5 saleProp）：如 Color = [Red, Black]。
type AliSKUProp struct {
	Name   string   `json:"name"`
	Values []string `json:"values"`
}

// AliSKU 是一条 SKU 组合（官方 3.5.6）：属性组合 → 价格 / 库存 / SKU 编码。
type AliSKU struct {
	Props map[string]string `json:"props"` // 属性名 → 选中值
	Price float64           `json:"price"`
	Stock int               `json:"stock"`
	Code  string            `json:"code"`
}

// AliTrade 是发品用的整块交易信息。PriceType 取值：
//   - fob    ：询盘品 FOB 区间价（默认）
//   - ladder ：阶梯价（sub 1-4 档）
//   - sku    ：SKU 价（下单品，配 saleProp + skus）
type AliTrade struct {
	// PriceType 是价格类型；空 = 按 market 与数据推断（询盘有阶梯就 ladder，否则 fob）。
	PriceType string `json:"priceType"`
	// Ladder 是阶梯价（最多 4 档，按 Qty 升序）。
	Ladder  []AliLadder `json:"ladder"`
	Package AliPackage  `json:"package"`
	Supply  AliSupply   `json:"supply"`
	// Logistics 是物流属性（官方 3.6.5 的 option value，如 battery.battery.oneLessHundredWh）；
	// 下单品至少一项，普货与带电属性不能同时选（阿里侧校验，这里只做上限与去重）。
	Logistics []string   `json:"logistics"`
	Sample    AliSample  `json:"sample"`
	Custom    []AliCustom `json:"custom"`
	// Group 是商品分组路径 ID（一级/二级/三级），来自 alibaba.icbu.product.group.get。
	Group []string `json:"group"`
	// SKUProps / SKUs 是销售属性与 SKU 组合（下单品 SKU 价时使用）。
	SKUProps []AliSKUProp `json:"skuProps"`
	SKUs     []AliSKU     `json:"skus"`
}

const (
	aliMaxLadder    = 4
	aliMaxSKU       = 200
	aliMaxLogistics = 20
	aliMaxGroup     = 3
	aliMaxCustom    = 10
)

// AliTrade 解出商品的交易信息块；坏数据当空（同步时按缺省处理，不让一条脏 JSON 挡住商品编辑）。
func (p Product) AliTrade() AliTrade {
	var t AliTrade
	if strings.TrimSpace(p.AliTradeJSON) == "" {
		return t
	}
	if json.Unmarshal([]byte(p.AliTradeJSON), &t) != nil {
		return AliTrade{}
	}
	return t
}

// normalizeAliTrade 清洗并校验交易信息：负数归零、阶梯价按起订量升序且不重复、上限截断。
// 返回落库 JSON 文本；空对象存空串（老数据语义：没有阿里交易信息）。
func normalizeAliTrade(t *AliTrade) (string, error) {
	if t == nil {
		return "", nil
	}
	switch t.PriceType {
	case "", "fob", "ladder", "sku":
	default:
		return "", response.BadRequest("website.ali_price_type_invalid")
	}
	// 阶梯价：去掉无效档、按起订量升序、同起订量取最后一档、最多 4 档
	seen := map[int]bool{}
	ladder := make([]AliLadder, 0, len(t.Ladder))
	for _, l := range t.Ladder {
		if l.Qty <= 0 || l.Price <= 0 || seen[l.Qty] {
			continue
		}
		seen[l.Qty] = true
		ladder = append(ladder, AliLadder{Qty: l.Qty, Price: l.Price})
	}
	sort.Slice(ladder, func(i, j int) bool { return ladder[i].Qty < ladder[j].Qty })
	if len(ladder) > aliMaxLadder {
		ladder = ladder[:aliMaxLadder]
	}
	t.Ladder = ladder

	pkg := t.Package
	for _, v := range []*float64{&pkg.Length, &pkg.Width, &pkg.Height, &pkg.Weight} {
		if *v < 0 {
			*v = 0
		}
	}
	pkg.ShippingTemplate = trimLen(strings.TrimSpace(pkg.ShippingTemplate), 32)
	t.Package = pkg

	if t.Supply.Number < 0 {
		t.Supply.Number = 0
	}
	t.Supply.QuantityUnit = trimLen(strings.TrimSpace(t.Supply.QuantityUnit), 32)
	t.Supply.TimeUnit = trimLen(strings.TrimSpace(t.Supply.TimeUnit), 32)

	props := make([]string, 0, len(t.Logistics))
	seenProp := map[string]bool{}
	for _, v := range t.Logistics {
		if v = trimLen(strings.TrimSpace(v), 80); v == "" || seenProp[v] {
			continue
		}
		seenProp[v] = true
		props = append(props, v)
		if len(props) == aliMaxLogistics {
			break
		}
	}
	t.Logistics = props

	if t.Sample.MaxQty < 0 {
		t.Sample.MaxQty = 0
	}
	if t.Sample.Price < 0 {
		t.Sample.Price = 0
	}
	t.Sample.Description = trimLen(strings.TrimSpace(t.Sample.Description), 500)

	group := make([]string, 0, len(t.Group))
	for _, g := range t.Group {
		if g = trimLen(strings.TrimSpace(g), 32); g != "" && len(group) < aliMaxGroup {
			group = append(group, g)
		}
	}
	t.Group = group

	custom := make([]AliCustom, 0, len(t.Custom))
	for _, c := range t.Custom {
		c.Type = trimLen(strings.TrimSpace(c.Type), 100)
		if c.Type == "" || c.Quantity <= 0 || len(custom) == aliMaxCustom {
			continue
		}
		custom = append(custom, c)
	}
	t.Custom = custom

	// 销售属性：去掉没有值的属性；值去重
	propOut := make([]AliSKUProp, 0, len(t.SKUProps))
	for _, sp := range t.SKUProps {
		sp.Name = trimLen(strings.TrimSpace(sp.Name), 100)
		if sp.Name == "" {
			continue
		}
		vals := make([]string, 0, len(sp.Values))
		seenVal := map[string]bool{}
		for _, v := range sp.Values {
			if v = trimLen(strings.TrimSpace(v), 100); v == "" || seenVal[v] {
				continue
			}
			seenVal[v] = true
			vals = append(vals, v)
		}
		if len(vals) == 0 {
			continue
		}
		sp.Values = vals
		propOut = append(propOut, sp)
	}
	t.SKUProps = propOut

	skus := make([]AliSKU, 0, len(t.SKUs))
	for _, s := range t.SKUs {
		if len(skus) == aliMaxSKU {
			break
		}
		if s.Price < 0 {
			s.Price = 0
		}
		if s.Stock < 0 {
			s.Stock = 0
		}
		s.Code = trimLen(strings.TrimSpace(s.Code), 64)
		cleaned := map[string]string{}
		for k, v := range s.Props {
			if k = strings.TrimSpace(k); k != "" && strings.TrimSpace(v) != "" {
				cleaned[k] = strings.TrimSpace(v)
			}
		}
		s.Props = cleaned
		skus = append(skus, s)
	}
	t.SKUs = skus

	// 只存非空块：全空时落空串，保持「老商品没有阿里交易信息」的语义
	if t.PriceType == "" && len(t.Ladder) == 0 && t.Package == (AliPackage{}) && t.Supply == (AliSupply{}) &&
		len(t.Logistics) == 0 && t.Sample == (AliSample{}) && len(t.Custom) == 0 && len(t.Group) == 0 &&
		len(t.SKUProps) == 0 && len(t.SKUs) == 0 {
		return "", nil
	}
	b, err := json.Marshal(t)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

// aliPriceType 解析本次发品实际使用的价格类型（表单没填时按数据推断）。
func (t AliTrade) aliPriceType(market string) string {
	if t.PriceType != "" {
		return t.PriceType
	}
	if len(t.Ladder) > 0 {
		return "ladder"
	}
	if market == "1" && len(t.SKUs) > 0 {
		return "sku"
	}
	return "fob"
}