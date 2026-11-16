package website

// ali_pull.go 是 D 阶段「从阿里反向拉取」：调 alibaba.icbu.product.schema.render
// （官方：获取商品发布的字段填写规则和单个商品对应填写数据），把已填值的 xml 解回来，
// 映射成本地商品。不认识的字段（各类目特有属性）落进动态属性 specs，不丢信息。
//
// 为什么用 render 而不是 product.get：render 返回的就是我们发品时用的同一套 field 结构，
// 反向映射可以直接按 field id 走；product.get 是旧接口，字段与 schema 两套。

import (
	"context"
	"encoding/xml"
	"fmt"
	"sort"
	"strings"

	"nexadmin/core/i18n"
	"nexadmin/core/response"
)

// rendNode 是渲染后 xml 的通用节点（值与全部属性都收下）。
type rendNode struct {
	XMLName  xml.Name
	ID       string     `xml:"id,attr"`
	Name     string     `xml:"name,attr"`
	Type     string     `xml:"type,attr"`
	Attrs    []xml.Attr `xml:",any,attr"`
	Text     string     `xml:",chardata"`
	Children []rendNode `xml:",any"`
}

// rendVal 是一个字段的值（与 aliVal 同构，用于反序列化）。
type rendVal struct {
	Text   string
	Attrs  map[string]string
	Values []rendVal
	Sub    map[string]rendVal
	Rows   []map[string]rendVal
}

// parseRenderedValues 把 render 返回的 xml 解析成 field id → 值。
func parseRenderedValues(raw string) (map[string]rendVal, error) {
	var root rendNode
	if err := xml.Unmarshal([]byte(raw), &root); err != nil {
		return nil, fmt.Errorf("rendered xml: %w", err)
	}
	out := map[string]rendVal{}
	collectRenderedFields(&root, out)
	if len(out) == 0 {
		return nil, fmt.Errorf("rendered xml has no field")
	}
	return out, nil
}

func collectRenderedFields(n *rendNode, out map[string]rendVal) {
	for i := range n.Children {
		c := &n.Children[i]
		if c.ID != "" && c.Type != "" {
			out[c.ID] = rendValueOf(c)
			continue
		}
		collectRenderedFields(c, out)
	}
}

func rendValueOf(n *rendNode) rendVal {
	v := rendVal{}
	for i := range n.Children {
		c := &n.Children[i]
		switch c.XMLName.Local {
		case "value":
			v.Values = append(v.Values, rendValueLeaf(c))
		case "values":
			for j := range c.Children {
				if c.Children[j].XMLName.Local == "value" {
					v.Values = append(v.Values, rendValueLeaf(&c.Children[j]))
				}
			}
		case "complex-value":
			v.Sub = map[string]rendVal{}
			for j := range c.Children {
				if f := &c.Children[j]; f.ID != "" && f.Type != "" {
					v.Sub[f.ID] = rendValueOf(f)
				}
			}
		case "complex-values":
			row := map[string]rendVal{}
			for j := range c.Children {
				if f := &c.Children[j]; f.ID != "" && f.Type != "" {
					row[f.ID] = rendValueOf(f)
				}
			}
			if len(row) > 0 {
				v.Rows = append(v.Rows, row)
			}
		}
	}
	if len(v.Values) == 1 && len(v.Sub) == 0 && len(v.Rows) == 0 {
		v.Text, v.Attrs = v.Values[0].Text, v.Values[0].Attrs
	}
	return v
}

func rendValueLeaf(n *rendNode) rendVal {
	attrs := map[string]string{}
	for _, a := range n.Attrs {
		attrs[a.Name.Local] = a.Value
	}
	return rendVal{Text: strings.TrimSpace(n.Text), Attrs: attrs}
}

// AliPullResult 是一次拉取的结果摘要（给前端提示用）。
type AliPullResult struct {
	ProductID string   `json:"productId"`
	Title     string   `json:"title"`
	Notes     []string `json:"notes"`
}

// PullAliProduct 把阿里侧一个商品拉回本地：已有 alibaba_product_id 的更新，否则新建。
// categoryID 可空（空则只按 product_id 渲染）；传了就一并回写本地商品的阿里类目。
func (s *Service) PullAliProduct(ctx context.Context, aliProductID, categoryID string) (*AliPullResult, error) {
	aliProductID = strings.TrimSpace(aliProductID)
	if !aliNumericID(aliProductID) {
		return nil, response.BadRequest("website.ali_product_id_invalid")
	}
	c, err := s.aliCredentials(ctx)
	if err != nil {
		return nil, err
	}
	res, err := aliCallNested(ctx, c, "alibaba.icbu.product.schema.render", aliPublishParam{
		CatID: strings.TrimSpace(categoryID), ProductID: aliProductID, Language: aliLanguageDefault,
	})
	if err != nil {
		return nil, err
	}
	biz := aliReadBiz(res)
	if e := biz.err(); e != nil {
		return nil, response.BadRequest("website.ali_gateway_rejected", e.Error())
	}
	vals, err := parseRenderedValues(biz.Data)
	if err != nil {
		return nil, response.BadRequest("website.ali_schema_unreadable", err.Error())
	}
	// render 只回值；枚举值翻回展示名要用规则里的 options，所以规则再取一次（只读）。
	var sc aliSchema
	if rules, rerr := aliCallNested(ctx, c, "alibaba.icbu.product.schema.get", aliPublishParam{
		CatID: strings.TrimSpace(categoryID), Language: aliLanguageDefault,
	}); rerr == nil {
		if rb := aliReadBiz(rules); rb.err() == nil {
			sc, _ = parseAliSchema(rb.Data)
		}
	}

	f, notes := productFormFromRendered(ctx, sc, vals)
	if strings.TrimSpace(f.Title) == "" {
		return nil, response.BadRequest("website.ali_product_title_missing")
	}
	id, err := s.productIDByAliID(ctx, aliProductID)
	if err != nil {
		return nil, err
	}
	saved, err := s.SaveProduct(ctx, id, f)
	if err != nil {
		return nil, err
	}
	updates := map[string]any{"alibaba_product_id": aliProductID, "sync_status": ProductSyncSynced}
	if cat := strings.TrimSpace(categoryID); cat != "" {
		updates["alibaba_category_id"] = cat
	}
	if err := s.db.WithContext(ctx).Model(&Product{}).Where("id = ?", int64(saved.ID)).Updates(updates).Error; err != nil {
		return nil, err
	}
	return &AliPullResult{ProductID: saved.ID.String(), Title: saved.Title, Notes: notes}, nil
}

// productFormFromRendered 把 render 的 field 值翻成本地商品表单。
func productFormFromRendered(ctx context.Context, sc aliSchema, vals map[string]rendVal) (ProductForm, []string) {
	var f ProductForm
	notes := make([]string, 0, 2)
	note := func(key string, a ...any) { notes = append(notes, i18n.T(ctx, key, a...)) }

	text := func(id string) string {
		if v, ok := vals[id]; ok {
			return v.Text
		}
		return ""
	}
	sub := func(id, child string) string {
		if v, ok := vals[id]; ok {
			return v.Sub[child].Text
		}
		return ""
	}

	f.Title = text("productTitle")
	f.Content = text("superText")
	f.Port = text("port")
	f.Moq = intOf(text("minOrderQuantity"))
	f.Currency = "USD"

	// 关键词：complex 子字段 productKeywords_0..2，按 id 排序保证稳定
	if v, ok := vals["productKeywords"]; ok {
		f.Keywords = strings.Join(sortedSubTexts(v), ", ")
	}
	// 图片：scImages_0..5，值文本是图片银行 URL
	if v, ok := vals["scImages"]; ok {
		imgs := sortedSubTexts(v)
		if len(imgs) > 0 {
			f.CoverURL = imgs[0]
			f.Images = imgs[1:]
		}
	}
	// 价格与币种/单位
	f.PriceMin = floatOf(sub("fob", "range_min"))
	f.PriceMax = floatOf(sub("fob", "range_max"))
	if cur := optionDisplayOf(sc, "fob.unit_type", sub("fob", "unit_type")); cur != "" {
		f.Currency = cur
	}
	f.PriceUnit = optionDisplayOf(sc, "priceUnit", text("priceUnit"))

	// 发货期：ladderPeriod 里第一档的 day（官方按起订量区间表达）
	if v, ok := vals["ladderPeriod"]; ok {
		for _, row := range v.Sub {
			if d := intOf(row.Sub["day"].Text); d > 0 {
				f.LeadTimeDays = d
				break
			}
		}
	}
	// 付款方式：预设多选 + 自定义一项
	if v, ok := vals["paymentMethod"]; ok {
		var terms []string
		for _, pv := range v.Sub["predefined_method"].Values {
			if pv.Text != "" {
				terms = append(terms, pv.Text)
			}
		}
		if s := v.Sub["self_defined_0"].Text; s != "" {
			terms = append(terms, s)
		}
		f.PaymentTerms = strings.Join(terms, ", ")
	}

	// 类目属性 / 自定义属性 → specs
	specs := make([]SpecItem, 0, 20)
	if v, ok := vals["icbuCatProp"]; ok {
		for id, cv := range v.Sub {
			label := attrChildName(sc, "icbuCatProp", id)
			val := cv.Attrs["inputValue"]
			if val == "" {
				val = optionDisplayOf(sc, "icbuCatProp."+id, cv.Text)
			}
			if val == "" {
				val = cv.Text
			}
			if label != "" && val != "" {
				specs = append(specs, SpecItem{Label: label, Value: val})
			}
		}
	}
	if v, ok := vals["customMoreProperty"]; ok {
		for _, row := range v.Sub {
			if name, val := row.Sub["propName"].Text, row.Sub["valueName"].Text; name != "" && val != "" {
				specs = append(specs, SpecItem{Label: name, Value: val})
			}
		}
	}
	sort.Slice(specs, func(i, j int) bool {
		if specs[i].Label != specs[j].Label {
			return specs[i].Label < specs[j].Label
		}
		return specs[i].Value < specs[j].Value
	})
	f.Specs = specs

	// 交易与规格 → AliTrade
	trade := AliTrade{}
	if v, ok := vals["ladderPrice"]; ok {
		for _, row := range v.Sub {
			q, p := intOf(row.Sub["quantity"].Text), floatOf(row.Sub["price"].Text)
			if q > 0 && p > 0 {
				trade.Ladder = append(trade.Ladder, AliLadder{Qty: q, Price: p})
			}
		}
		if len(trade.Ladder) > 0 {
			trade.PriceType = "ladder"
		}
	}
	if v, ok := vals["pkgMeasure"]; ok {
		trade.Package.Length = floatOf(v.Sub["length"].Text)
		trade.Package.Width = floatOf(v.Sub["width"].Text)
		trade.Package.Height = floatOf(v.Sub["height"].Text)
	}
	trade.Package.Weight = floatOf(text("pkgWeight"))
	trade.Package.ShippingTemplate = text("shippingTemplateId")
	if v, ok := vals["supply"]; ok {
		trade.Supply.Number = floatOf(v.Sub["supply_number"].Text)
		trade.Supply.QuantityUnit = optionDisplayOf(sc, "supply.quantity_unit", v.Sub["quantity_unit"].Text)
		trade.Supply.TimeUnit = v.Sub["time_unit"].Text // 文档示例即 "Day" 这类文本
	}
	if v, ok := vals["logisticsProperty"]; ok {
		for _, lv := range v.Values {
			if lv.Text != "" {
				trade.Logistics = append(trade.Logistics, lv.Text)
			}
		}
	}
	if _, ok := vals["marketSample"]; ok && text("marketSample") != "" {
		trade.Sample.Enabled = true
		trade.Sample.MaxQty = intOf(text("marketSamplingQuantity"))
		trade.Sample.Price = floatOf(text("marketSamplingPrice"))
		trade.Sample.Description = text("marketSamplingDescription")
	}
	if v, ok := vals["customizedServices"]; ok {
		for _, row := range v.Sub {
			ct := row.Sub["custom_type"]
			typ := ct.Attrs["inputValue"]
			if typ == "" {
				typ = optionDisplayOf(sc, "customizedServices.custom_type", ct.Text)
			}
			if typ == "" {
				typ = ct.Text
			}
			if q := intOf(row.Sub["quantity"].Text); typ != "" && q > 0 {
				trade.Custom = append(trade.Custom, AliCustom{Type: typ, Quantity: q})
			}
		}
	}
	if v, ok := vals["productGroup"]; ok {
		for _, id := range []string{"first_group_id", "second_group_id", "third_group_id"} {
			if g := v.Sub[id].Text; g != "" {
				trade.Group = append(trade.Group, g)
			}
		}
	}
	// 销售属性与 SKU：p-xxx → 属性名，值 → 展示名
	if v, ok := vals["saleProp"]; ok && len(v.Sub) > 0 {
		for id, pv := range v.Sub {
			prop := AliSKUProp{Name: firstNonEmptyStr(attrChildName(sc, "saleProp", id), id)}
			for _, val := range pv.Values {
				label := val.Attrs["inputValue"]
				if label == "" {
					label = optionDisplayOf(sc, "saleProp."+id, val.Text)
				}
				if label == "" {
					label = val.Text
				}
				if label != "" {
					prop.Values = append(prop.Values, label)
				}
			}
			if len(prop.Values) > 0 {
				trade.SKUProps = append(trade.SKUProps, prop)
			}
		}
		sort.Slice(trade.SKUProps, func(i, j int) bool { return trade.SKUProps[i].Name < trade.SKUProps[j].Name })
	}
	if v, ok := vals["sku"]; ok {
		for _, row := range v.Rows {
			sku := AliSKU{Props: map[string]string{}}
			for _, pv := range row["props"].Values {
				name := firstNonEmptyStr(
					attrChildName(sc, "saleProp", pv.Attrs["propName"]),
					pv.Attrs["propName"],
					pv.Attrs["propId"],
				)
				if name != "" && pv.Text != "" {
					sku.Props[name] = pv.Text
				}
			}
			sku.Price = floatOf(row["price"].Text)
			if len(row["skuStock"].Values) > 0 {
				sku.Stock = intOf(row["skuStock"].Values[0].Text)
			}
			sku.Code = row["skuOuterId"].Text
			if len(sku.Props) > 0 {
				trade.SKUs = append(trade.SKUs, sku)
			}
		}
		if len(trade.SKUs) > 0 {
			trade.PriceType = "sku"
		}
	}
	if len(trade.Ladder) > 0 || trade.Package != (AliPackage{}) || trade.Supply != (AliSupply{}) ||
		len(trade.Logistics) > 0 || trade.Sample.Enabled || len(trade.Custom) > 0 || len(trade.Group) > 0 ||
		len(trade.SKUProps) > 0 || len(trade.SKUs) > 0 {
		f.AliTrade = &trade
	}
	if f.Content == "" {
		note("website.ali_pull_no_content")
	}
	return f, notes
}

// sortedSubTexts 按子字段 id 升序取文本（productKeywords_0/1/2、scImages_0..5 的顺序表达）。
func sortedSubTexts(v rendVal) []string {
	keys := make([]string, 0, len(v.Sub))
	for k, cv := range v.Sub {
		if cv.Text != "" {
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)
	out := make([]string, 0, len(keys))
	for _, k := range keys {
		out = append(out, v.Sub[k].Text)
	}
	return out
}

// attrChildName 在某个复杂字段的子字段里按 id 找展示名。
func attrChildName(sc aliSchema, fieldID, childID string) string {
	f, ok := sc.find(fieldID)
	if !ok {
		return ""
	}
	for _, c := range f.Children {
		if c.ID == childID {
			return c.Name
		}
	}
	return ""
}

// optionDisplayOf 按字段路径（a.b 指向子字段）把选项值翻成展示名。
func optionDisplayOf(sc aliSchema, fieldPath, value string) string {
	if len(sc) == 0 || value == "" {
		return ""
	}
	parts := strings.Split(fieldPath, ".")
	f, ok := sc.find(parts[0])
	if !ok {
		return ""
	}
	for _, p := range parts[1:] {
		if f, ok = f.child(p); !ok {
			return ""
		}
	}
	for _, o := range f.Options {
		if o.Value == value {
			return o.DisplayName
		}
	}
	return ""
}

func firstNonEmptyStr(ss ...string) string {
	for _, s := range ss {
		if strings.TrimSpace(s) != "" {
			return s
		}
	}
	return ""
}

func floatOf(s string) float64 {
	var v float64
	_, _ = fmt.Sscanf(strings.TrimSpace(s), "%f", &v)
	return v
}

func intOf(s string) int {
	return int(floatOf(s))
}
