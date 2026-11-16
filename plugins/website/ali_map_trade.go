package website

// ali_map_trade.go 把 local 的阿里交易信息（AliTrade）映射进 schema 字段：
// 阶梯价 / 价格类型、销售属性与 SKU、供货能力、包装与运费模板、物流属性、样品、定制服务、商品分组。
// 与 ali_map.go 同一原则：只填 schema 里存在的字段、枚举按 options 解析、匹配不到就记 notes 不静默丢。

import (
	"strconv"
	"strings"
)

// applyAliTrade 在 aliMap 组装完基础字段后补充交易与规格字段。
func applyAliTrade(vals map[string]aliVal, sc aliSchema, p *Product, market string, note func(string, ...any)) {
	t := p.AliTrade()
	pt := t.aliPriceType(market)

	// ① 价格类型（官方 3.5.1）：询盘品 scPrice（1=阶梯价 2=FOB 价），下单品 marketPrice（1=阶梯价 2=SKU 价）
	var priceTypeField aliField
	if market == "2" {
		priceTypeField, _ = sc.find("scPrice")
	} else {
		priceTypeField, _ = sc.find("marketPrice")
	}
	if market == "1" && pt == "fob" {
		// 官方 3.5.1：下单品只有阶梯价 / SKU 价两种，没有 FOB 价
		note("website.ali_note_wholesale_no_fob")
	} else if priceTypeField.ID != "" {
		if v, ok := aliPriceTypeValue(priceTypeField, pt); ok {
			vals[priceTypeField.ID] = aliInput(v)
		} else {
			note("website.ali_note_price_type_unresolved", pt)
		}
	}

	// ② 阶梯价（官方 3.5.2）：ladderPrice_0..3，每档 quantity + price
	if pt == "ladder" && len(t.Ladder) > 0 {
		if f, ok := sc.find("ladderPrice"); ok && f.Type == aliTypeComplex {
			rows := map[string]aliVal{}
			for i, l := range t.Ladder {
				if i >= len(f.Children) {
					break
				}
				child := f.Children[i]
				q, ok1 := child.child("quantity")
				pr, ok2 := child.child("price")
				if !ok1 || !ok2 {
					continue
				}
				rows[child.ID] = aliVal{Sub: map[string]aliVal{
					q.ID:  aliInput(strconv.Itoa(l.Qty)),
					pr.ID: aliInput(money(l.Price)),
				}}
			}
			if len(rows) > 0 {
				vals["ladderPrice"] = aliVal{Sub: rows}
				// 阶梯价时起订量由档位表达，官方要求 minOrderQuantity 单独传
				if _, ok := sc.find("minOrderQuantity"); ok && t.Ladder[0].Qty > 0 {
					vals["minOrderQuantity"] = aliInput(strconv.Itoa(t.Ladder[0].Qty))
				}
			}
		} else {
			note("website.ali_note_no_ladder_field")
		}
	}

	// ③ 销售属性与 SKU（官方 3.5.5 / 3.5.6）：属性 ID 与选项 ID 都来自 schema，不用本地猜
	if pt == "sku" {
		applyAliSKU(vals, sc, t, note)
	}

	// ④ 供货能力（官方 3.6.2，询盘品特有）
	if t.Supply.Number > 0 {
		if f, ok := sc.find("supply"); ok && f.Type == aliTypeComplex {
			sub := map[string]aliVal{}
			if c, ok := f.child("supply_number"); ok {
				sub[c.ID] = aliInput(formatQty(t.Supply.Number))
			}
			if c, ok := f.child("quantity_unit"); ok && t.Supply.QuantityUnit != "" {
				if v, _, found := c.resolveOption(t.Supply.QuantityUnit); found {
					sub[c.ID] = aliInput(v)
				} else {
					note("website.ali_note_supply_unit_unmatched", t.Supply.QuantityUnit)
				}
			}
			if c, ok := f.child("time_unit"); ok && t.Supply.TimeUnit != "" {
				if v, _, found := c.resolveOption(t.Supply.TimeUnit); found {
					sub[c.ID] = aliInput(v)
				} else {
					note("website.ali_note_supply_time_unmatched", t.Supply.TimeUnit)
				}
			}
			if len(sub) > 0 {
				vals["supply"] = aliVal{Sub: sub}
			}
		}
	}

	// ⑤ 包装与运费模板（官方 3.6.4，下单品必填）
	if t.Package.Length > 0 || t.Package.Width > 0 || t.Package.Height > 0 {
		if f, ok := sc.find("pkgMeasure"); ok && f.Type == aliTypeComplex {
			sub := map[string]aliVal{}
			for id, v := range map[string]float64{"length": t.Package.Length, "width": t.Package.Width, "height": t.Package.Height} {
				if c, ok := f.child(id); ok && v > 0 {
					sub[c.ID] = aliInput(formatQty(v))
				}
			}
			if len(sub) > 0 {
				vals["pkgMeasure"] = aliVal{Sub: sub}
			}
		}
	}
	if t.Package.Weight > 0 {
		if _, ok := sc.find("pkgWeight"); ok {
			vals["pkgWeight"] = aliInput(formatQty(t.Package.Weight))
		}
	}
	if t.Package.ShippingTemplate != "" {
		if f, ok := sc.find("shippingTemplateId"); ok {
			// 本地存的就是阿里模板 ID（选项 value），先按 value 找回，再退按展示名
			if opt, found := aliOptionByValue(f, t.Package.ShippingTemplate); found {
				vals["shippingTemplateId"] = aliInput(opt.Value)
			} else if v, _, found := f.resolveOption(t.Package.ShippingTemplate); found {
				vals["shippingTemplateId"] = aliInput(v)
			} else {
				note("website.ali_note_shipping_template_unmatched", t.Package.ShippingTemplate)
			}
		}
	}

	// ⑥ 物流属性（官方 3.6.5，下单品必填且至少一个有效项）：本地存的就是 option value
	if len(t.Logistics) > 0 {
		if f, ok := sc.find("logisticsProperty"); ok {
			list := make([]aliVal, 0, len(t.Logistics))
			for _, v := range t.Logistics {
				if _, found := aliOptionByValue(f, v); !found {
					note("website.ali_note_logistics_unmatched", v)
					continue
				}
				list = append(list, aliInput(v))
			}
			if len(list) > 0 {
				vals["logisticsProperty"] = aliVal{List: list}
			}
		}
	}

	// ⑦ 样品服务（官方 3.5.9，下单品特有）
	if t.Sample.Enabled {
		if f, ok := sc.find("marketSample"); ok {
			if v, _, found := f.resolveOption("yes"); found {
				vals["marketSample"] = aliInput(v)
			} else if len(f.Options) > 0 {
				vals["marketSample"] = aliInput(f.Options[0].Value)
			}
			if _, ok := sc.find("marketSamplingQuantity"); ok && t.Sample.MaxQty > 0 {
				vals["marketSamplingQuantity"] = aliInput(strconv.Itoa(t.Sample.MaxQty))
			}
			if _, ok := sc.find("marketSamplingPrice"); ok && t.Sample.Price > 0 {
				vals["marketSamplingPrice"] = aliInput(money(t.Sample.Price))
			}
			if _, ok := sc.find("marketSamplingDescription"); ok && t.Sample.Description != "" {
				vals["marketSamplingDescription"] = aliInput(trimLen(t.Sample.Description, 500))
			}
		}
	}

	// ⑧ 定制服务（官方 3.7.1）：customizedServices_0..9，每行 custom_type + quantity
	if len(t.Custom) > 0 {
		if f, ok := sc.find("customizedServices"); ok && f.Type == aliTypeComplex {
			rows := map[string]aliVal{}
			used := 0
			for _, child := range f.Children {
				if used >= len(t.Custom) {
					break
				}
				ct, ok1 := child.child("custom_type")
				qty, ok2 := child.child("quantity")
				if !ok1 || !ok2 {
					continue
				}
				c := t.Custom[used]
				used++
				sub := map[string]aliVal{qty.ID: aliInput(strconv.Itoa(c.Quantity))}
				if v, custom, found := ct.resolveOption(c.Type); found {
					if custom {
						sub[ct.ID] = aliInputAttrs(v, map[string]string{"inputValue": c.Type})
					} else {
						sub[ct.ID] = aliInput(v)
					}
				} else {
					sub[ct.ID] = aliInputAttrs("-1", map[string]string{"inputValue": c.Type})
				}
				rows[child.ID] = aliVal{Sub: sub}
			}
			if len(rows) > 0 {
				vals["customizedServices"] = aliVal{Sub: rows}
			}
		}
	}

	// ⑨ 商品分组（官方 3.3.3）：第一/二/三级分组 ID
	if len(t.Group) > 0 {
		if f, ok := sc.find("productGroup"); ok && f.Type == aliTypeComplex {
			sub := map[string]aliVal{}
			for i, id := range []string{"first_group_id", "second_group_id", "third_group_id"} {
				if i >= len(t.Group) {
					break
				}
				if c, ok := f.child(id); ok && t.Group[i] != "" {
					sub[c.ID] = aliInput(t.Group[i])
				}
			}
			if len(sub) > 0 {
				vals["productGroup"] = aliVal{Sub: sub}
			}
		}
	}
}

// aliPriceTypeValue 把价格类型解析成 schema 的选项值（按展示名找，找不到再按文档约定回退）。
func aliPriceTypeValue(f aliField, pt string) (string, bool) {
	keywords := map[string][]string{
		"ladder": {"ladder", "tier", "阶梯"},
		"sku":    {"sku", "规格"},
		"fob":    {"fob", "uniform", "一口价"},
	}[pt]
	for _, kw := range keywords {
		for _, o := range f.Options {
			if strings.Contains(normalizeDisplay(o.DisplayName), kw) {
				return o.Value, true
			}
		}
	}
	// 回退：官方文档约定询盘 1=阶梯 2=FOB、下单 1=阶梯 2=SKU
	switch pt {
	case "ladder":
		return "1", len(f.Options) > 0
	case "sku", "fob":
		return "2", len(f.Options) > 1
	}
	return "", false
}

// applyAliSKU 组装 saleProp 与 sku：属性/选项 ID 全部从 schema 解析，
// 本地只提供「属性名 → 值」的文本（AliSKU.props）。
func applyAliSKU(vals map[string]aliVal, sc aliSchema, t AliTrade, note func(string, ...any)) {
	propField, ok := sc.find("saleProp")
	if !ok || propField.Type != aliTypeComplex {
		note("website.ali_note_no_saleprop")
		return
	}
	skuField, ok := sc.find("sku")
	if !ok || skuField.Type != aliTypeMultiComplex {
		note("website.ali_note_no_sku")
		return
	}
	byName := map[string]aliField{}
	for _, c := range propField.Children {
		byName[normalizeDisplay(c.Name)] = c
	}
	// 值 → (选项值, 是否自定义)
	type optInfo struct {
		valueID string
		custom  bool
	}
	resolved := map[string]map[string]optInfo{} // 属性名 → 值 → 选项
	propSub := map[string]aliVal{}
	for _, sp := range t.SKUProps {
		c, found := byName[normalizeDisplay(sp.Name)]
		if !found {
			note("website.ali_note_saleprop_unknown", sp.Name)
			continue
		}
		list := make([]aliVal, 0, len(sp.Values))
		resolved[sp.Name] = map[string]optInfo{}
		for _, v := range sp.Values {
			ov, custom, found := c.resolveOption(v)
			if !found {
				note("website.ali_note_saleprop_value_unmatched", sp.Name, v)
				continue
			}
			resolved[sp.Name][v] = optInfo{valueID: ov, custom: custom}
			if custom {
				list = append(list, aliInputAttrs(ov, map[string]string{"inputValue": v}))
			} else {
				list = append(list, aliInput(ov))
			}
		}
		if len(list) > 0 {
			propSub[c.ID] = aliVal{List: list}
		}
	}
	if len(propSub) > 0 {
		vals["saleProp"] = aliVal{Sub: propSub}
	}

	rows := make([]map[string]aliVal, 0, len(t.SKUs))
	for _, s := range t.SKUs {
		row := map[string]aliVal{}
		propsVals := make([]aliVal, 0, len(s.Props))
		usable := true
		for name, v := range s.Props {
			c, found := byName[normalizeDisplay(name)]
			if !found {
				usable = false
				break
			}
			info, found := resolved[name][v]
			if !found {
				usable = false
				break
			}
			attrs := map[string]string{"propId": strings.TrimPrefix(c.ID, "p-"), "propName": c.ID, "propValueId": info.valueID, "propValueName": v}
			if info.custom {
				attrs["inputValue"] = v
			}
			propsVals = append(propsVals, aliInputAttrs(v, attrs))
		}
		if !usable || len(propsVals) == 0 {
			note("website.ali_note_sku_unmatched", s.Code)
			continue
		}
		if c, ok := skuField.child("props"); ok {
			row[c.ID] = aliVal{List: propsVals}
		}
		if c, ok := skuField.child("price"); ok && s.Price > 0 {
			row[c.ID] = aliInput(money(s.Price))
		}
		if c, ok := skuField.child("skuStock"); ok {
			row[c.ID] = aliVal{List: []aliVal{aliInputAttrs(strconv.Itoa(s.Stock), map[string]string{
				"srcValue": "0", "warehouseCode": "CN_LOCAL_01",
			})}}
		}
		if c, ok := skuField.child("skuOuterId"); ok && s.Code != "" {
			row[c.ID] = aliInput(c.clampBytes(s.Code))
		}
		rows = append(rows, row)
	}
	if len(rows) > 0 {
		vals["sku"] = aliVal{Rows: rows}
	}
}

// aliOptionByValue 按选项值找回选项（本地存的是阿里 ID 时用）。
func aliOptionByValue(f aliField, value string) (aliOption, bool) {
	for _, o := range f.Options {
		if o.Value == value {
			return o, true
		}
	}
	return aliOption{}, false
}

// formatQty 数字转文本：整数不带小数点，带小数保留两位。
func formatQty(v float64) string {
	if v == float64(int64(v)) {
		return strconv.FormatInt(int64(v), 10)
	}
	return strconv.FormatFloat(v, 'f', 2, 64)
}
