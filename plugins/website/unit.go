package website

import "strings"

// 计量单位的「人读」显示。
//
// 商详的 PriceUnit 是卖家自由文本（model.go 里 size:32，后台下拉给的是 piece / set / m²，
// 手填常是 "套"、"pcs"、"Set/Sets"），库里只有一份原文。
// 以前模板直接把它打到页面上，所以中文站也会看到英文 set。这里只做展示层归一：
//
//  1. 常见写法（中英、单复数、Set/Sets、pcs/piece）归一到单位码；
//  2. 单位码翻成当前站点语言（locale 里的 unit.*，与出厂字典 sys_dict.unit 同一套词表）；
//  3. 归不到码的（1x20GP、40HQ、客户自造词）原样返回 —— 宁可显示原文，也不要瞎猜。
//
// 单位码就是 sys_dict.unit 的 value，也是后台商品表单下拉的取值（原 UNITS 常量），
// 存量商品存的就是它们，所以别在这里自创第三套写法。
//
// 数据侧不动：data-* 属性、询价载荷、导入导出仍然用 PriceUnit 原文，
// 只有给人看的文本走 unitName，避免把「展示翻译」渗进数据契约。
var unitAliases = map[string]string{
	"piece": "piece", "pieces": "piece", "pcs": "piece", "pc": "piece", "pce": "piece",
	"件": "piece", "只": "piece", "个": "piece",
	"set": "set", "sets": "set", "套": "set",
	"unit": "unit", "units": "unit", "台": "unit",
	"pair": "pair", "pairs": "pair", "对": "pair", "双": "pair",
	"dozen": "dozen", "dozens": "dozen", "dz": "dozen", "打": "dozen",
	"roll": "roll", "rolls": "roll", "卷": "roll",
	"sheet": "sheet", "sheets": "sheet", "张": "sheet",
	"bag": "bag", "bags": "bag", "袋": "bag",
	"box": "box", "boxes": "box", "箱": "box",
	"carton": "carton", "cartons": "carton", "ctn": "carton", "纸箱": "carton",
	"pallet": "pallet", "pallets": "pallet", "plt": "pallet", "托盘": "pallet",
	"barrel": "barrel", "barrels": "barrel", "drum": "barrel", "drums": "barrel", "桶": "barrel",
	"kg": "kg", "kgs": "kg", "kilogram": "kg", "kilograms": "kg", "千克": "kg", "公斤": "kg",
	"g": "g", "gram": "g", "grams": "g", "克": "g",
	"ton": "ton", "tons": "ton", "tonne": "ton", "tonnes": "ton", "mt": "ton", "吨": "ton",
	"lb": "lb", "lbs": "lb", "pound": "lb", "pounds": "lb", "磅": "lb",
	"m": "m", "meter": "m", "meters": "m", "metre": "m", "metres": "m", "米": "m",
	"cm": "cm", "centimeter": "cm", "centimeters": "cm", "厘米": "cm",
	"mm": "mm", "millimeter": "mm", "millimeters": "mm", "毫米": "mm",
	"m²": "m²", "m2": "m²", "sqm": "m²", "squaremeter": "m²", "平方米": "m²", "平米": "m²",
	"m³": "m³", "m3": "m³", "cbm": "m³", "cubicmeter": "m³", "立方米": "m³", "方": "m³",
	"l": "L", "liter": "L", "liters": "L", "litre": "L", "litres": "L", "升": "L",
	"ml": "mL", "milliliter": "mL", "milliliters": "mL", "millilitre": "mL", "毫升": "mL",
}

// canonicalUnit 把自由文本的单位写法归一到单位码；归不到返回空串。
// 比较时忽略大小写、首尾空白、内部空格与结尾的点（"Pcs." / "Set / Sets" 都能认）。
func canonicalUnit(raw string) string {
	s := strings.ToLower(strings.TrimSpace(raw))
	s = strings.TrimSuffix(s, ".")
	s = strings.Join(strings.Fields(s), "")
	if c, ok := unitAliases[s]; ok {
		return c
	}
	// "Set/Sets"、"piece / pcs" 这类斜杠并写：取斜杠前的第一段再试一次。
	if i := strings.Index(s, "/"); i > 0 {
		if c, ok := unitAliases[s[:i]]; ok {
			return c
		}
	}
	return ""
}

// unitName 返回 lang 语言下给人看的单位名；非标准单位原样返回。
// lang 为空时按站点默认语言处理（与 tr 一致）。
func unitName(lang, raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	code := canonicalUnit(raw)
	if code == "" {
		return raw
	}
	key := "unit." + code
	// 字典缺这条（新增语言还没补译文）时 tr 会回 key 本身，宁可显示原文也不要露出 "unit.set"。
	if s := tr(lang, key); s != "" && s != key {
		return s
	}
	return raw
}

// UnitName 是模板侧的入口：{{.UnitName .Product.PriceUnit}}。
// 只用于可见文本；data-* 属性请继续用 .Product.PriceUnit 原值。
func (v *siteView) UnitName(raw string) string {
	code := ""
	if v != nil {
		code = v.Lang.Code
	}
	return unitName(code, raw)
}
