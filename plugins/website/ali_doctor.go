package website

// ali_doctor.go 是联调自检：用真实凭据把「只有跑一次接口才知道」的几件事一次性打出来，
// 管理员不需要懂 schema，也可直接把报告贴给开发者：
//
//  1. 该类目有哪些必填字段（requiredRule=true）；
//  2. logisticsProperty / priceUnit / scPrice / marketPrice 的真实选项（值 + 展示名）；
//  3. schema.add 吃 flat 还是嵌套入参（两种各试一次，看网关是抱怨参数还是抱怨内容）；
//  4. 可选：render 一个已有商品，把它已填值里关键字段的原始形态打出来（如 supply.time_unit）。
//
// 全部是只读或「必然失败的空内容提交」：不产生商品、不改任何数据。

import (
	"context"
	"strings"

	"nexadmin/core/response"
)

// AliDoctorOption 是 schema 里的一个枚举选项。
type AliDoctorOption struct {
	Value       string `json:"value"`
	DisplayName string `json:"displayName"`
}

// AliDoctorReport 是自检报告（前端原样展示，可复制给开发者）。
type AliDoctorReport struct {
	CategoryID   string                       `json:"categoryId"`
	FieldCount   int                          `json:"fieldCount"`
	Required     []string                     `json:"required"`
	Logistics    []AliDoctorOption            `json:"logistics"`
	PriceUnit    []AliDoctorOption            `json:"priceUnit"`
	PriceTypes   map[string][]AliDoctorOption `json:"priceTypes"`
	RenderSample map[string]string            `json:"renderSample,omitempty"`
	AddShape     string                       `json:"addShape"` // flat | nested | unknown | skipped
	AddErrors    map[string]string            `json:"addErrors,omitempty"`
	Notes        []string                     `json:"notes"`
}

// AliDoctor 执行联调自检。productID 为空则跳过 render；probeAdd=false 则不试 schema.add 入参形态。
func (s *Service) AliDoctor(ctx context.Context, categoryID, productID string, probeAdd bool) (*AliDoctorReport, error) {
	categoryID = strings.TrimSpace(categoryID)
	productID = strings.TrimSpace(productID)
	if categoryID == "" || !aliNumericID(categoryID) {
		return nil, response.BadRequest("website.ali_category_invalid")
	}
	c, err := s.aliCredentials(ctx)
	if err != nil {
		return nil, err
	}
	rep := &AliDoctorReport{CategoryID: categoryID, PriceTypes: map[string][]AliDoctorOption{}, Notes: []string{}}

	// ① 规则：必填字段与关键枚举
	res, err := aliCallNested(ctx, c, "alibaba.icbu.product.schema.get", aliPublishParam{CatID: categoryID, Language: aliLanguageDefault})
	if err != nil {
		return nil, err
	}
	biz := aliReadBiz(res)
	if e := biz.err(); e != nil {
		return nil, response.BadRequest("website.ali_gateway_rejected", e.Error())
	}
	sc, err := parseAliSchema(biz.Data)
	if err != nil {
		return nil, response.BadRequest("website.ali_schema_unreadable", err.Error())
	}
	rep.FieldCount = len(sc)
	for _, f := range sc {
		if f.required() {
			rep.Required = append(rep.Required, f.ID+"（"+f.Name+"）")
		}
	}
	collect := func(f aliField) []AliDoctorOption {
		out := make([]AliDoctorOption, 0, len(f.Options))
		for _, o := range f.Options {
			out = append(out, AliDoctorOption{Value: o.Value, DisplayName: o.DisplayName})
		}
		return out
	}
	if f, ok := sc.find("logisticsProperty"); ok {
		rep.Logistics = collect(f)
		if len(f.Options) == 0 {
			rep.Notes = append(rep.Notes, "logisticsProperty 没有可选项：该类目可能不需要物流属性，或规则里选项在子层")
		}
	}
	if f, ok := sc.find("priceUnit"); ok {
		rep.PriceUnit = collect(f)
	}
	for _, id := range []string{"scPrice", "marketPrice"} {
		if f, ok := sc.find(id); ok {
			rep.PriceTypes[id] = collect(f)
		}
	}
	if f, ok := sc.find("supply"); ok {
		if tu, ok := f.child("time_unit"); ok {
			if len(tu.Options) == 0 {
				rep.Notes = append(rep.Notes, "supply.time_unit 在规则里没有选项：render 时很可能直接回文本（如 Day），现有实现按文本处理")
			} else {
				rep.Notes = append(rep.Notes, "supply.time_unit 是枚举：render 返回的应是选项 value，映射时需要按选项翻展示名")
			}
		}
	}

	// ② render 一个已有商品：看关键字段的原始值形态
	if productID != "" {
		if !aliNumericID(productID) {
			rep.Notes = append(rep.Notes, "productId 不是数字，已跳过 render 检查")
		} else if rres, rerr := aliCallNested(ctx, c, "alibaba.icbu.product.schema.render", aliPublishParam{
			CatID: categoryID, ProductID: productID, Language: aliLanguageDefault,
		}); rerr != nil {
			rep.Notes = append(rep.Notes, "render 调用失败："+rerr.Error())
		} else if rb := aliReadBiz(rres); rb.err() != nil {
			rep.Notes = append(rep.Notes, "render 业务失败："+rb.err().Error())
		} else if vals, perr := parseRenderedValues(rb.Data); perr != nil {
			rep.Notes = append(rep.Notes, "render 返回值解析失败："+perr.Error())
		} else {
			rep.RenderSample = map[string]string{}
			for _, id := range []string{"market", "priceUnit", "shippingTemplateId", "logisticsProperty", "marketSample"} {
				if v, ok := vals[id]; ok {
					rep.RenderSample[id] = strings.TrimSpace(v.Text + " " + strings.Join(textsOf(v.Values), ","))
				}
			}
			if v, ok := vals["supply"]; ok {
				rep.RenderSample["supply.quantity_unit"] = v.Sub["quantity_unit"].Text
				rep.RenderSample["supply.time_unit"] = v.Sub["time_unit"].Text
			}
			if v, ok := vals["fob"]; ok {
				rep.RenderSample["fob.unit_type"] = v.Sub["unit_type"].Text
			}
		}
	}

	// ③ schema.add 入参形态：空内容分别用 flat / 嵌套各试一次，看网关先抱怨参数还是抱怨内容
	if probeAdd {
		rep.AddShape = "unknown"
		rep.AddErrors = map[string]string{}
		flatRes, flatErr := aliCall(ctx, c, "alibaba.icbu.product.schema.add", map[string]string{
			"category_id": categoryID, "xml": "<itemSchema></itemSchema>", "language": aliLanguageDefault,
		})
		nestedRes, nestedErr := aliCallNested(ctx, c, "alibaba.icbu.product.schema.add", aliPublishParam{
			CatID: categoryID, XML: "<itemSchema></itemSchema>", Language: aliLanguageDefault,
		})
		_ = flatRes
		_ = nestedRes
		if flatErr != nil {
			rep.AddErrors["flat"] = flatErr.Error()
		}
		if nestedErr != nil {
			rep.AddErrors["nested"] = nestedErr.Error()
		}
		switch {
		case flatErr == nil && nestedErr != nil:
			rep.AddShape = "flat"
		case nestedErr == nil && flatErr != nil:
			rep.AddShape = "nested"
		case aliIsInvalidParam(flatErr) && !aliIsInvalidParam(nestedErr):
			rep.AddShape = "nested"
		case aliIsInvalidParam(nestedErr) && !aliIsInvalidParam(flatErr):
			rep.AddShape = "flat"
		case !aliIsInvalidParam(flatErr) && !aliIsInvalidParam(nestedErr):
			// 两种都被「内容」拒绝（缺必填/XML 不合法）→ 说明两种都能进到业务校验，优先 flat（官方文档写法）
			rep.AddShape = "flat"
			rep.Notes = append(rep.Notes, "flat 与嵌套都通过了参数校验（被内容校验拦下），按官方文档优先用 flat")
		}
	} else {
		rep.AddShape = "skipped"
	}
	return rep, nil
}

func textsOf(vs []rendVal) []string {
	out := make([]string, 0, len(vs))
	for _, v := range vs {
		if v.Text != "" {
			out = append(out, v.Text)
		}
	}
	return out
}
