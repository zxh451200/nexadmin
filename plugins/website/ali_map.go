package website

// ali_map.go 把本地商品映射成 schema field 值表（官方文档 4.3 要求的「动态映射」）。
//
// 原则：
//   - 只填 schema 里存在的字段，枚举值一律从 schema 的 options 解析（展示名匹配），不写死数字；
//   - 本地有值但枚举匹配不到 → 记进 notes（预览与失败原因里透出），不静默丢；
//   - 本地还没有对应数据的必填字段（运费模板、包装尺寸、物流属性、SKU 等）留空，
//     由 Render 收进 missing，提交前就告诉管理员「阿里要求填什么」，而不是等网关报错。

import (
	"context"
	"strconv"
	"strings"

	"nexadmin/core/i18n"
)

// aliImage 是一张已进阿里图片银行的图片。
type aliImage struct {
	FileID int64
	URL    string
}

// aliMapResult 是一次映射的产物。
type aliMapResult struct {
	XML     string
	Missing []aliMissing
	Notes   []string
}

// aliMap 按 schema 组装提交 xml。market 是阿里口径（1=下单品 2=询盘品）。
// 提醒文案一律走后端字典（i18n key），随请求语言出译文，不在 Go 里拼中文。
func aliMap(ctx context.Context, sc aliSchema, p *Product, market, title, keywords, content string, images []aliImage) aliMapResult {
	vals := map[string]aliVal{}
	// notes / missing 一律给空切片而非 nil：接口出去是 JSON 数组，前端不用防 null
	notes := make([]string, 0, 4)
	note := func(key string, a ...any) { notes = append(notes, i18n.T(ctx, key, a...)) }

	// ① 商品类型与标题（3.2 / 3.3.1）
	if _, ok := sc.find("market"); ok {
		vals["market"] = aliInput(market)
	}
	if f, ok := sc.find("productTitle"); ok {
		vals["productTitle"] = aliInput(f.clampBytes(title))
	}

	// ② 关键词（3.3.1）：官方是 productKeywords 的 complex，子字段 productKeywords_0/1/2
	if f, ok := sc.find("productKeywords"); ok && f.Type == aliTypeComplex {
		kws := splitAliKeywords(keywords)
		if n := len(kws); n > len(f.Children) {
			note("website.ali_note_keywords_dropped", n, len(f.Children))
		}
		vals["productKeywords"] = aliVal{Sub: fillSequential(f, kws, nil)}
	}

	// ③ 主图（3.4.1）：值必须是图片银行 URL，且带 fileId
	if f, ok := sc.find("scImages"); ok && f.Type == aliTypeComplex && len(images) > 0 {
		texts := make([]string, 0, len(images))
		ids := make([]string, 0, len(images))
		for _, img := range images {
			texts = append(texts, img.URL)
			ids = append(ids, strconv.FormatInt(img.FileID, 10))
		}
		vals["scImages"] = aliVal{Sub: fillSequential(f, texts, func(i int) map[string]string {
			if i >= len(ids) || ids[i] == "0" {
				return nil
			}
			return map[string]string{"fileId": ids[i]}
		})}
	}

	// ④ 详情（3.4.2）：API 只支持普通编辑，productDescType 取其 options，superText 收富文本
	if f, ok := sc.find("productDescType"); ok {
		if v, _, found := f.resolveOption("普通编辑"); found {
			vals["productDescType"] = aliInput(v)
		} else if v, _, found := f.resolveOption("standard"); found {
			vals["productDescType"] = aliInput(v)
		} else if len(f.Options) > 0 {
			// 展示名对不上（多语言/改版）时取第一个可选项，并留话：真实联调若报错按这里调
			vals["productDescType"] = aliInput(f.Options[0].Value)
			note("website.ali_note_desc_type_fallback", f.Options[0].DisplayName)
		}
	}
	if f, ok := sc.find("superText"); ok && strings.TrimSpace(content) != "" {
		vals["superText"] = aliInput(f.clampBytes(content))
	}

	// ⑤ 类目属性（3.3.4）：属性名对得上 schema 的进 icbuCatProp，对不上的退到自定义属性（3.3.5）
	var custom []SpecItem
	catPropSub := map[string]aliVal{}
	if cat, ok := sc.find("icbuCatProp"); ok {
		byName := map[string]aliField{}
		for _, c := range cat.Children {
			if c.Name != "" {
				byName[normalizeDisplay(c.Name)] = c
			}
		}
		for _, sp := range p.ProductSpecs() {
			if sp.Label == "" || sp.Value == "" {
				continue
			}
			c, found := byName[normalizeDisplay(sp.Label)]
			if !found {
				custom = append(custom, sp)
				continue
			}
			if v, ok := aliAttrValue(c, sp.Value); ok {
				catPropSub[c.ID] = v
				continue
			}
			custom = append(custom, sp)
			note("website.ali_note_attr_to_custom", c.Name, sp.Value)
		}
		if len(catPropSub) > 0 {
			vals["icbuCatProp"] = aliVal{Sub: catPropSub}
		}
	} else {
		custom = append(custom, p.ProductSpecs()...)
	}

	// ⑥ 自定义属性（3.3.5）：子字段 customMoreProperty_0..9，每个再包 propName / valueName
	if f, ok := sc.find("customMoreProperty"); ok && len(custom) > 0 {
		sub := map[string]aliVal{}
		used := 0
		for _, child := range f.Children {
			if used >= len(custom) {
				break
			}
			g, ok := child.child("propName")
			if !ok {
				continue
			}
			v, ok := child.child("valueName")
			if !ok {
				continue
			}
			sub[child.ID] = aliVal{Sub: map[string]aliVal{
				g.ID: aliInput(custom[used].Label),
				v.ID: aliInput(custom[used].Value),
			}}
			used++
		}
		if len(sub) > 0 {
			vals["customMoreProperty"] = aliVal{Sub: sub}
		}
		if left := len(custom) - used; left > 0 {
			note("website.ali_note_custom_overflow", len(f.Children), left)
		}
	}

	// ⑦ 价格（3.5.1-3.5.4）：询盘品 scPrice（1=阶梯价 2=FOB 一口价），下单品 marketPrice（1=阶梯价 2=SKU 价）
	// 本地一期只有价格区间，没有阶梯价/SKU 明细 → 询盘品走 FOB 区间价；下单品留空并说明。
	if market == "2" {
		if f, ok := sc.find("scPrice"); ok {
			if v, _, found := f.resolveOption("one fob price"); found {
				vals["scPrice"] = aliInput(v)
			} else if len(f.Options) > 1 {
				vals["scPrice"] = aliInput(f.Options[1].Value)
			} else {
				note("website.ali_note_scprice_unknown")
			}
		}
	} else if _, ok := sc.find("marketPrice"); ok {
		note("website.ali_note_wholesale_price_type")
	}

	if p.PriceMin > 0 {
		if f, ok := sc.find("fob"); ok && f.Type == aliTypeComplex {
			sub := map[string]aliVal{}
			if c, ok := f.child("range_min"); ok {
				sub[c.ID] = aliInput(money(p.PriceMin))
			}
			if c, ok := f.child("range_max"); ok {
				sub[c.ID] = aliInput(money(max(p.PriceMax, p.PriceMin)))
			}
			if c, ok := f.child("unit_type"); ok {
				if v, _, found := c.resolveOption(p.Currency); found {
					sub[c.ID] = aliInputAttrs(v, map[string]string{"text": p.Currency})
				} else {
					note("website.ali_note_currency_unmatched", p.Currency)
				}
			}
			vals["fob"] = aliVal{Sub: sub}
		}
	}

	// 计量单位（3.5.4，必填）：本地存的是展示名文本（piece / set…），按 options 解析
	if f, ok := sc.find("priceUnit"); ok && strings.TrimSpace(p.PriceUnit) != "" {
		if v, customVal, found := f.resolveOption(p.PriceUnit); found {
			if customVal {
				vals["priceUnit"] = aliInputAttrs(v, map[string]string{"inputValue": p.PriceUnit})
			} else {
				vals["priceUnit"] = aliInput(v)
			}
		} else {
			note("website.ali_note_unit_unmatched", p.PriceUnit)
		}
	}

	// 起订量（3.5.3）与发货周期（3.6.1）：官方按「起订量区间 → 天数」表达，本地只有一档
	if _, ok := sc.find("minOrderQuantity"); ok && p.Moq > 0 {
		vals["minOrderQuantity"] = aliInput(strconv.Itoa(p.Moq))
	}
	if f, ok := sc.find("ladderPeriod"); ok && f.Type == aliTypeComplex && p.Moq > 0 && p.LeadTimeDays > 0 {
		for _, child := range f.Children {
			q, ok1 := child.child("quantity")
			d, ok2 := child.child("day")
			if !ok1 || !ok2 {
				continue
			}
			vals["ladderPeriod"] = aliVal{Sub: map[string]aliVal{child.ID: aliVal{Sub: map[string]aliVal{
				q.ID: aliInput(strconv.Itoa(p.Moq)),
				d.ID: aliInput(strconv.Itoa(p.LeadTimeDays)),
			}}}}
			break
		}
	}

	// 装运港口（3.6.2，询盘品特有）
	if f, ok := sc.find("port"); ok && strings.TrimSpace(p.Port) != "" {
		vals["port"] = aliInput(f.clampBytes(p.Port))
	}

	// 付款方式（3.5.7，询盘品特有）：预设项进 predefined_method，剩下的一种进 self_defined_0
	if f, ok := sc.find("paymentMethod"); ok && strings.TrimSpace(p.PaymentTerms) != "" {
		pre, ok := f.child("predefined_method")
		if !ok {
			note("website.ali_note_payment_no_predefined")
		} else {
			var matched []aliVal
			var rest []string
			for _, term := range splitAliList(p.PaymentTerms) {
				if v, _, found := pre.resolveOption(term); found {
					matched = append(matched, aliInput(v))
					continue
				}
				rest = append(rest, term)
			}
			sub := map[string]aliVal{}
			if len(matched) > 0 {
				sub[pre.ID] = aliVal{List: matched}
			}
			if len(rest) > 0 {
				if self, ok := f.child("self_defined_0"); ok {
					sub[self.ID] = aliInput(strings.Join(rest, ", "))
				} else {
					note("website.ali_note_payment_dropped", strings.Join(rest, ", "))
				}
			}
			if len(sub) > 0 {
				vals["paymentMethod"] = aliVal{Sub: sub}
			}
		}
	}

	// ⑧ 交易与规格：阶梯价/价格类型、SKU、供货能力、包装与运费模板、物流属性、样品、定制、分组
	// （数据在 AliTrade，映射见 ali_map_trade.go；没填的必填项由 Render 统一报进 missing）
	applyAliTrade(vals, sc, p, market, note)

	xmlText, missing := sc.Render(vals)
	return aliMapResult{XML: xmlText, Missing: missing, Notes: notes}
}

// aliAttrValue 把一条属性值按子字段类型翻成 schema 值；无法表达时返回 ok=false（转自定义属性）。
func aliAttrValue(c aliField, value string) (aliVal, bool) {
	switch c.Type {
	case aliTypeSingleCheck:
		if v, custom, found := c.resolveOption(value); found {
			if custom {
				return aliInputAttrs(v, map[string]string{"inputValue": value}), true
			}
			return aliInput(v), true
		}
		return aliVal{}, false
	case aliTypeMultiCheck:
		var out []aliVal
		for _, part := range splitAliList(value) {
			v, custom, found := c.resolveOption(part)
			if !found {
				return aliVal{}, false
			}
			if custom {
				out = append(out, aliInputAttrs(v, map[string]string{"inputValue": part}))
				continue
			}
			out = append(out, aliInput(v))
		}
		if len(out) == 0 {
			return aliVal{}, false
		}
		return aliVal{List: out}, true
	case aliTypeInput, aliTypeMultiInput:
		// 官方 3.3.4：input 型属性的值必须是负数占位，真实文本放 inputValue
		return aliInputAttrs("-1", map[string]string{"inputValue": value}), true
	}
	return aliVal{}, false
}

// splitAliKeywords 拆关键词：优先按逗号/顿号（保留词组，如 "18V brushless impact wrench"），
// 完全没有分隔符时才按空白拆（兼容历史按空格存的数据）。
func splitAliKeywords(s string) []string {
	parts := splitAliList(s)
	if len(parts) == 0 {
		parts = strings.Fields(s)
	}
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

// splitAliList 按逗号 / 顿号 / 分号 / 换行拆列表。
func splitAliList(s string) []string {
	return strings.FieldsFunc(s, func(r rune) bool {
		return r == ',' || r == '，' || r == ';' || r == '；' || r == '、' || r == '\n' || r == '\r'
	})
}

// money 固定两位小数：阿里的价格字段是 double，且文档示例都是 3.0000 / 12.34 形态。
func money(v float64) string { return strconv.FormatFloat(v, 'f', 2, 64) }
