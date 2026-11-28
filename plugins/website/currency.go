package website

import (
	"encoding/json"
	"math"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"nexadmin/core/response"
)

// 币种换算（对标阿里国际站右上角的「语言-币种」选择器）。
//
// 汇率表由管理员在站点设置里维护：USD 为基准（恒为 1），其余币种存
// 「1 USD = N 该币种」。展示币种按访客 cookie（nex-site-currency）解析，
// 未选时用站点设置的 default_currency；汇率表为空时不启用换算，
// 价格按商品各自 currency 字段直出（与旧版行为一致）。

const (
	// currencyCookie 存访客选择的展示币种，path 全站、一年有效；前端保存时写入。
	currencyCookie = "nex-site-currency"
	// baseCurrency 是汇率基准：表里 USD 恒为 1，换算统一经 USD 中转。
	baseCurrency = "USD"
)

var currencyCodeRe = regexp.MustCompile(`^[A-Z]{3}$`)

// parseCurrencyRates 解析并校验站点设置里的汇率文本。
// 规则：空串 = 不启用；JSON 对象的键是 3 位大写币种码（值 > 0，最多 24 个，
// USD 缺省补 1、显式非 1 视为非法——基准错了全线都错）。
func parseCurrencyRates(raw string) (map[string]float64, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, nil
	}
	var m map[string]float64
	if err := json.Unmarshal([]byte(raw), &m); err != nil {
		return nil, response.BadRequest("website.currency_rates_invalid")
	}
	if len(m) == 0 {
		return nil, nil
	}
	if len(m) > 24 {
		return nil, response.BadRequest("website.currency_rates_invalid")
	}
	for code, rate := range m {
		if !currencyCodeRe.MatchString(code) || math.IsNaN(rate) || rate <= 0 || math.IsInf(rate, 0) {
			return nil, response.BadRequest("website.currency_rates_invalid")
		}
	}
	if _, ok := m[baseCurrency]; !ok {
		m[baseCurrency] = 1
	} else if m[baseCurrency] != 1 {
		return nil, response.BadRequest("website.currency_rates_invalid")
	}
	return m, nil
}

// normalizeCurrencySetting 清洗管理端提交的币种设置：
// 汇率表为空时两者都清空；表非空时允许默认币种为空（停用换算），
// 非空默认币种必须在表内，且自动补上 USD=1。返回入库用的文本。
func normalizeCurrencySetting(defaultCurrency, ratesRaw string) (string, string, error) {
	rates, err := parseCurrencyRates(ratesRaw)
	if err != nil {
		return "", "", err
	}
	if rates == nil {
		return "", "", nil
	}
	def := strings.ToUpper(strings.TrimSpace(defaultCurrency))
	if def != "" {
		if _, ok := rates[def]; !ok {
			return "", "", response.BadRequest("website.currency_default_invalid")
		}
	}
	b, err := json.Marshal(rates)
	if err != nil {
		return "", "", err
	}
	return def, string(b), nil
}

// currencyNames 是弹层币种下拉的显示名：{英文名, 中文名}。
// 清单外的币种（管理员自己加的）回退币种码本身，不拦保存。
var currencyNames = map[string][2]string{
	"USD": {"US Dollar", "美元"},
	"EUR": {"Euro", "欧元"},
	"GBP": {"British Pound", "英镑"},
	"CNY": {"Chinese Yuan", "人民币"},
	"JPY": {"Japanese Yen", "日元"},
	"AUD": {"Australian Dollar", "澳元"},
	"CAD": {"Canadian Dollar", "加元"},
	"CHF": {"Swiss Franc", "瑞士法郎"},
	"HKD": {"Hong Kong Dollar", "港币"},
	"SGD": {"Singapore Dollar", "新加坡元"},
	"NZD": {"New Zealand Dollar", "新西兰元"},
	"KRW": {"South Korean Won", "韩元"},
	"INR": {"Indian Rupee", "印度卢比"},
	"RUB": {"Russian Ruble", "俄罗斯卢布"},
	"BRL": {"Brazilian Real", "巴西雷亚尔"},
	"AED": {"UAE Dirham", "阿联酋迪拉姆"},
	"SAR": {"Saudi Riyal", "沙特里亚尔"},
	"MXN": {"Mexican Peso", "墨西哥比索"},
	"THB": {"Thai Baht", "泰铢"},
	"VND": {"Vietnamese Dong", "越南盾"},
}

// currencyLabel 弹层下拉的选项文本：「USD - 美元」。lang 是站点界面语言码。
func currencyLabel(code, lang string) string {
	return code + " - " + currencyName(code, lang)
}

// currencyName 返回币种的本地化名称（zh 界面取中文名，其余取英文名；
// 清单外的币种回退币种码本身）。语言-币种触发钮用纯名称。
func currencyName(code, lang string) string {
	names, ok := currencyNames[code]
	if !ok {
		return code
	}
	if strings.HasPrefix(strings.ToLower(lang), "zh") {
		return names[1]
	}
	return names[0]
}

// fx 是请求级换算器：siteView.FxCur / FxMoney 的后端。
// display 是访客的展示币种（cookie > 站点默认），"" = 不启用。
type fx struct {
	rates   map[string]float64
	display string
}

// enabled 报告换算是否生效：没配表或没解析出展示币种时全站按原币种直出。
func (f *fx) enabled() bool { return f != nil && f.display != "" && len(f.rates) > 0 }

// cur 返回价格该用的展示币种码。展示币种与原币种相同、或原币种不在汇率表里
// （管理员没配该币种的汇率）时原样返回——宁可这商品不换算，也不出现
// 「数字没变前缀换了」的错价。
func (f *fx) cur(from string) string {
	if !f.enabled() {
		return from
	}
	from = strings.ToUpper(strings.TrimSpace(from))
	if from == f.display {
		return from
	}
	if _, ok := f.rates[from]; !ok {
		return from
	}
	return f.display
}

// amount 把原币种金额换算成展示币种。原币种不在表里时不换算原值返回。
func (f *fx) amount(v float64, from string) float64 {
	if !f.enabled() {
		return v
	}
	r, ok := f.rates[strings.ToUpper(strings.TrimSpace(from))]
	if !ok || r <= 0 {
		return v
	}
	return v / r * f.rates[f.display]
}

// money 换算并按 money 口径格式化（整数不带小数、非整数两位——与模板 money
// 函数一致，避免同一屏出现两种数字写法）。
func (f *fx) money(v float64, from string) string {
	return compactMoney(f.amount(v, from))
}

// compactMoney 与 render.go tplFuncs["money"] 同一口径，这里独立成函数供 fx 用。
func compactMoney(v float64) string {
	if v == math.Trunc(v) {
		return strconv.FormatFloat(v, 'f', 0, 64)
	}
	return strconv.FormatFloat(v, 'f', 2, 64)
}

// currencyOptions 是头部弹层币种下拉的选项：USD 置顶，其余按码排序。
type currencyOption struct {
	Code  string
	Label string
}

func currencyOptions(rates map[string]float64, lang string) []currencyOption {
	codes := make([]string, 0, len(rates))
	for code := range rates {
		if code != baseCurrency {
			codes = append(codes, code)
		}
	}
	sort.Strings(codes)
	out := make([]currencyOption, 0, len(codes)+1)
	out = append(out, currencyOption{Code: baseCurrency, Label: currencyLabel(baseCurrency, lang)})
	for _, code := range codes {
		out = append(out, currencyOption{Code: code, Label: currencyLabel(code, lang)})
	}
	return out
}
