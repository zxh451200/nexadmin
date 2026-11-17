package website

import (
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"html/template"
	"io/fs"
	"math"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"unicode"
)

//go:embed tpl/*.tmpl
var tplFS embed.FS

//go:embed static
var staticFS embed.FS

// assetVersion 返回内嵌静态资源的内容哈希（前 8 字节十六进制），模板拿它当 ?v=。
// 原来 ?v= 是手写的字面量，改完 CSS 忘了改它，浏览器就一直用缓存；
// 内容哈希让地址随文件内容自动变化，不需要手动硬刷新。
// 主题资产用 "theme/<id>/<file>" 形式的名字：从主题插件的文件系统里取。
// 文件不存在时返回空串（地址退化成不带版本，至少不会 404）。
var assetCache sync.Map // name -> string

func assetVersion(name string) string {
	if v, ok := assetCache.Load(name); ok {
		return v.(string)
	}
	sum := ""
	if rest, ok := strings.CutPrefix(name, "theme/"); ok {
		id, file, _ := strings.Cut(rest, "/")
		if t := lookupTheme(id); t != nil && file != "" {
			if b, err := fs.ReadFile(t.FS, "static/"+file); err == nil {
				h := sha256.Sum256(b)
				sum = hex.EncodeToString(h[:8])
			}
		}
	} else if b, err := staticFS.ReadFile("static/" + name); err == nil {
		h := sha256.Sum256(b)
		sum = hex.EncodeToString(h[:8])
	}
	assetCache.Store(name, sum)
	return sum
}

// 图标集：特性卡片 Icon 字段引用这些名字；未知名渲染默认圆点。
// 24×24 线性风格（stroke），路径取自 Tabler Icons v3.36.0 outline（MIT，Copyright (c) 2020-2025 Paweł Kuna）。
// 键名会写进数据库，换图标只改路径不改键；库里没有的语义才手绘（见 defaultIcon）。
var iconPaths = map[string]string{
	"building": `<path d="M3 21l18 0" /><path d="M9 8l1 0" /><path d="M9 12l1 0" /><path d="M9 16l1 0" /><path d="M14 8l1 0" /><path d="M14 12l1 0" /><path d="M14 16l1 0" /><path d="M5 21v-16a2 2 0 0 1 2 -2h10a2 2 0 0 1 2 2v16" />`,
	"shield":   `<path d="M12 3a12 12 0 0 0 8.5 3a12 12 0 0 1 -8.5 15a12 12 0 0 1 -8.5 -15a12 12 0 0 0 8.5 -3" />`,
	"key":      `<path d="M16.555 3.843l3.602 3.602a2.877 2.877 0 0 1 0 4.069l-2.643 2.643a2.877 2.877 0 0 1 -4.069 0l-.301 -.301l-6.558 6.558a2 2 0 0 1 -1.239 .578l-.175 .008h-1.172a1 1 0 0 1 -.993 -.883l-.007 -.117v-1.172a2 2 0 0 1 .467 -1.284l.119 -.13l.414 -.414h2v-2h2v-2l2.144 -2.144l-.301 -.301a2.877 2.877 0 0 1 0 -4.069l2.643 -2.643a2.877 2.877 0 0 1 4.069 0z" /><path d="M15 9h.01" />`,
	"plug":     `<path d="M9.785 6l8.215 8.215l-2.054 2.054a5.81 5.81 0 1 1 -8.215 -8.215l2.054 -2.054z" /><path d="M4 20l3.5 -3.5" /><path d="M15 4l-3.5 3.5" /><path d="M20 9l-3.5 3.5" />`,
	"file":     `<path d="M14 3v4a1 1 0 0 0 1 1h4" /><path d="M17 21h-10a2 2 0 0 1 -2 -2v-14a2 2 0 0 1 2 -2h7l5 5v11a2 2 0 0 1 -2 2z" />`,
	"chart":    `<path d="M3 13a1 1 0 0 1 1 -1h4a1 1 0 0 1 1 1v6a1 1 0 0 1 -1 1h-4a1 1 0 0 1 -1 -1z" /><path d="M15 9a1 1 0 0 1 1 -1h4a1 1 0 0 1 1 1v10a1 1 0 0 1 -1 1h-4a1 1 0 0 1 -1 -1z" /><path d="M9 5a1 1 0 0 1 1 -1h4a1 1 0 0 1 1 1v14a1 1 0 0 1 -1 1h-4a1 1 0 0 1 -1 -1z" /><path d="M4 20h14" />`,
	"globe":    `<path d="M7 9a4 4 0 1 0 8 0a4 4 0 0 0 -8 0" /><path d="M5.75 15a8.015 8.015 0 1 0 9.25 -13" /><path d="M11 17v4" /><path d="M7 21h8" />`,
	"puzzle":   `<path d="M4 7h3a1 1 0 0 0 1 -1v-1a2 2 0 0 1 4 0v1a1 1 0 0 0 1 1h3a1 1 0 0 1 1 1v3a1 1 0 0 0 1 1h1a2 2 0 0 1 0 4h-1a1 1 0 0 0 -1 1v3a1 1 0 0 1 -1 1h-3a1 1 0 0 1 -1 -1v-1a2 2 0 0 0 -4 0v1a1 1 0 0 1 -1 1h-3a1 1 0 0 1 -1 -1v-3a1 1 0 0 1 1 -1h1a2 2 0 0 0 0 -4h-1a1 1 0 0 1 -1 -1v-3a1 1 0 0 1 1 -1" />`,
	"terminal": `<path d="M8 9l3 3l-3 3" /><path d="M13 15l3 0" /><path d="M3 4m0 2a2 2 0 0 1 2 -2h14a2 2 0 0 1 2 2v12a2 2 0 0 1 -2 2h-14a2 2 0 0 1 -2 -2z" />`,
	"zap":      `<path d="M13 3l0 7l6 0l-8 11l0 -7l-6 0l8 -11" />`,
	"layers":   `<path d="M12 6l-8 4l8 4l8 -4l-8 -4" /><path d="M4 14l8 4l8 -4" />`,
	"box":      `<path d="M12 3l8 4.5l0 9l-8 4.5l-8 -4.5l0 -9l8 -4.5" /><path d="M12 12l8 -4.5" /><path d="M12 12l0 9" /><path d="M12 12l-8 -4.5" />`,
	"users":    `<path d="M9 7m-4 0a4 4 0 1 0 8 0a4 4 0 1 0 -8 0" /><path d="M3 21v-2a4 4 0 0 1 4 -4h4a4 4 0 0 1 4 4v2" /><path d="M16 3.13a4 4 0 0 1 0 7.75" /><path d="M21 21v-2a4 4 0 0 0 -3 -3.85" />`,
	"bell":     `<path d="M10 5a2 2 0 1 1 4 0a7 7 0 0 1 4 6v3a4 4 0 0 0 2 3h-16a4 4 0 0 0 2 -3v-3a7 7 0 0 1 4 -6" /><path d="M9 17v1a3 3 0 0 0 6 0v-1" />`,
	"lock":     `<path d="M5 13a2 2 0 0 1 2 -2h10a2 2 0 0 1 2 2v6a2 2 0 0 1 -2 2h-10a2 2 0 0 1 -2 -2v-6z" /><path d="M11 16a1 1 0 1 0 2 0a1 1 0 0 0 -2 0" /><path d="M8 11v-4a4 4 0 1 1 8 0v4" />`,
	"code":     `<path d="M7 8l-4 4l4 4" /><path d="M17 8l4 4l-4 4" /><path d="M14 4l-4 16" />`,
	"mail":     `<path d="M3 7a2 2 0 0 1 2 -2h14a2 2 0 0 1 2 2v10a2 2 0 0 1 -2 2h-14a2 2 0 0 1 -2 -2v-10z" /><path d="M3 7l9 6l9 -6" />`,
	"phone":    `<path d="M5 4h4l2 5l-2.5 1.5a11 11 0 0 0 5 5l1.5 -2.5l5 2v4a2 2 0 0 1 -2 2a16 16 0 0 1 -15 -15a2 2 0 0 1 2 -2" />`,
}

// socialIconPaths 是页脚社交图标：键集合必须与 service.go 的 socialPlatforms 一致（有测试兜底）。
// 路径取自 Tabler Icons v3.36.0 的 outline 系列（MIT，Copyright (c) 2020-2025 Paweł Kuna，https://tabler.io/icons），
// 已去掉每个图标开头的透明底板；品牌标识的商标权仍归各自所有，这里只是几何描摹。
// 刷新时按同名文件覆盖即可，注意 wrapper 的 stroke-width 要保持 Tabler 自己的 2。
var socialIconPaths = map[string]string{
	"github":    `<path d="M9 19c-4.3 1.4 -4.3 -2.5 -6 -3m12 5v-3.5c0 -1 .1 -1.4 -.5 -2c2.8 -.3 5.5 -1.4 5.5 -6a4.6 4.6 0 0 0 -1.3 -3.2a4.2 4.2 0 0 0 -.1 -3.2s-1.1 -.3 -3.5 1.3a12.3 12.3 0 0 0 -6.2 0c-2.4 -1.6 -3.5 -1.3 -3.5 -1.3a4.2 4.2 0 0 0 -.1 3.2a4.6 4.6 0 0 0 -1.3 3.2c0 4.6 2.7 5.7 5.5 6c-.6 .6 -.6 1.2 -.5 2v3.5"/>`,
	"x":         `<path d="M4 4l11.733 16h4.267l-11.733 -16z"/><path d="M4 20l6.768 -6.768m2.46 -2.46l6.772 -6.772"/>`,
	"linkedin":  `<path d="M8 11v5"/><path d="M8 8v.01"/><path d="M12 16v-5"/><path d="M16 16v-3a2 2 0 1 0 -4 0"/><path d="M3 7a4 4 0 0 1 4 -4h10a4 4 0 0 1 4 4v10a4 4 0 0 1 -4 4h-10a4 4 0 0 1 -4 -4z"/>`,
	"youtube":   `<path d="M2 8a4 4 0 0 1 4 -4h12a4 4 0 0 1 4 4v8a4 4 0 0 1 -4 4h-12a4 4 0 0 1 -4 -4v-8z"/><path d="M10 9l5 3l-5 3z"/>`,
	"instagram": `<path d="M4 8a4 4 0 0 1 4 -4h8a4 4 0 0 1 4 4v8a4 4 0 0 1 -4 4h-8a4 4 0 0 1 -4 -4z"/><path d="M9 12a3 3 0 1 0 6 0a3 3 0 0 0 -6 0"/><path d="M16.5 7.5v.01"/>`,
	"facebook":  `<path d="M7 10v4h3v7h4v-7h3l1 -4h-4v-2a1 1 0 0 1 1 -1h3v-4h-3a5 5 0 0 0 -5 5v2h-3"/>`,
	"discord":   `<path d="M8 12a1 1 0 1 0 2 0a1 1 0 0 0 -2 0"/><path d="M14 12a1 1 0 1 0 2 0a1 1 0 0 0 -2 0"/><path d="M15.5 17c0 1 1.5 3 2 3c1.5 0 2.833 -1.667 3.5 -3c.667 -1.667 .5 -5.833 -1.5 -11.5c-1.457 -1.015 -3 -1.34 -4.5 -1.5l-.972 1.923a11.913 11.913 0 0 0 -4.053 0l-.975 -1.923c-1.5 .16 -3.043 .485 -4.5 1.5c-2 5.667 -2.167 9.833 -1.5 11.5c.667 1.333 2 3 3.5 3c.5 0 2 -2 2 -3"/><path d="M7 16.5c3.5 1 6.5 1 10 0"/>`,
	"telegram":  `<path d="M15 10l-4 4l6 6l4 -16l-18 7l4 2l2 6l3 -4"/>`,
	"whatsapp":  `<path d="M3 21l1.65 -3.8a9 9 0 1 1 3.4 2.9l-5.05 .9"/><path d="M9 10a.5 .5 0 0 0 1 0v-1a.5 .5 0 0 0 -1 0v1a5 5 0 0 0 5 5h1a.5 .5 0 0 0 0 -1h-1a.5 .5 0 0 0 0 1"/>`,
	"reddit":    `<path d="M12 8c2.648 0 5.028 .826 6.675 2.14a2.5 2.5 0 0 1 2.326 4.36c0 3.59 -4.03 6.5 -9 6.5c-4.875 0 -8.845 -2.8 -9 -6.294l-1 -.206a2.5 2.5 0 0 1 2.326 -4.36c1.646 -1.313 4.026 -2.14 6.674 -2.14z"/><path d="M12 8l1 -5l6 1"/><path d="M19 4m-1 0a1 1 0 1 0 2 0a1 1 0 1 0 -2 0"/><circle cx="9" cy="13" r=".5" fill="currentColor"/><circle cx="15" cy="13" r=".5" fill="currentColor"/><path d="M10 17c.667 .333 1.333 .5 2 .5s1.333 -.167 2 -.5"/>`,
	"tiktok":    `<path d="M21 7.917v4.034a9.948 9.948 0 0 1 -5 -1.951v4.5a6.5 6.5 0 1 1 -8 -6.326v4.326a2.5 2.5 0 1 0 4 2v-11.5h4.083a6.005 6.005 0 0 0 4.917 4.917z"/>`,
	"twitch":    `<path d="M4 5v11a1 1 0 0 0 1 1h2v4l4 -4h5.584c.266 0 .52 -.105 .707 -.293l2.415 -2.414c.187 -.188 .293 -.442 .293 -.708v-8.585a1 1 0 0 0 -1 -1h-14a1 1 0 0 0 -1 1z"/><path d="M16 8l0 4"/><path d="M12 8l0 4"/>`,
	"mastodon":  `<path d="M18.648 15.254c-1.816 1.763 -6.648 1.626 -6.648 1.626a18.262 18.262 0 0 1 -3.288 -.256c1.127 1.985 4.12 2.81 8.982 2.475c-1.945 2.013 -13.598 5.257 -13.668 -7.636l-.026 -1.154c0 -3.036 .023 -4.115 1.352 -5.633c1.671 -1.91 6.648 -1.666 6.648 -1.666s4.977 -.243 6.648 1.667c1.329 1.518 1.352 2.597 1.352 5.633s-.456 4.074 -1.352 4.944z"/><path d="M12 11.204v-2.926c0 -1.258 -.895 -2.278 -2 -2.278s-2 1.02 -2 2.278v4.722m4 -4.722c0 -1.258 .895 -2.278 2 -2.278s2 1.02 2 2.278v4.722"/>`,
	"wechat":    `<path d="M16.5 10c3.038 0 5.5 2.015 5.5 4.5c0 1.397 -.778 2.645 -2 3.47l0 2.03l-1.964 -1.178a6.649 6.649 0 0 1 -1.536 .178c-3.038 0 -5.5 -2.015 -5.5 -4.5s2.462 -4.5 5.5 -4.5z"/><path d="M11.197 15.698c-.69 .196 -1.43 .302 -2.197 .302a8.008 8.008 0 0 1 -2.612 -.432l-2.388 1.432v-2.801c-1.237 -1.082 -2 -2.564 -2 -4.199c0 -3.314 3.134 -6 7 -6c3.782 0 6.863 2.57 7 5.785l0 .233"/><path d="M10 8h.01"/><path d="M7 8h.01"/><path d="M15 14h.01"/><path d="M18 14h.01"/>`,
	"weibo":     `<path d="M19 14.127c0 3.073 -3.502 5.873 -8 5.873c-4.126 0 -8 -2.224 -8 -5.565c0 -1.78 .984 -3.737 2.7 -5.567c2.362 -2.51 5.193 -3.687 6.551 -2.238c.415 .44 .752 1.39 .749 2.062c2 -1.615 4.308 .387 3.5 2.693c1.26 .557 2.5 .538 2.5 2.742z"/><path d="M15 4h1a5 5 0 0 1 5 5v1"/>`,
	"bilibili":  `<path d="M3 10a4 4 0 0 1 4 -4h10a4 4 0 0 1 4 4v6a4 4 0 0 1 -4 4h-10a4 4 0 0 1 -4 -4v-6z"/><path d="M8 3l2 3"/><path d="M16 3l-2 3"/><path d="M9 13v-2"/><path d="M15 11v2"/>`,
	"rss":       `<path d="M5 19m-1 0a1 1 0 1 0 2 0a1 1 0 1 0 -2 0"/><path d="M4 4a16 16 0 0 1 16 16"/><path d="M4 11a9 9 0 0 1 9 9"/>`,
	"mail":      `<path d="M3 7a2 2 0 0 1 2 -2h14a2 2 0 0 1 2 2v10a2 2 0 0 1 -2 2h-14a2 2 0 0 1 -2 -2v-10z"/><path d="M3 7l9 6l9 -6"/>`,
}

// inquiryDialog 渲染商详页的「发送询盘」弹窗（对齐阿里商详的表单式询盘）：
// 详细需求 + 常见问题快填 + 联系方式 + 隐私勾选，提交走现有的 POST /contact（Accept: json）。
// 结构和文案都在服务端出，site.js 只管开合、快填与提交，四套主题共用一份。
func inquiryDialog(v any) template.HTML {
	sv, _ := v.(*siteView)
	t := func(k string) string { return k }
	tf := func(k string, args ...any) string { return k }
	contactURL := ""
	consent, consentURL := false, ""
	var product *Product
	if sv != nil {
		t = sv.T
		tf = sv.TF
		contactURL = sv.URL("/contact")
		consent, consentURL = sv.Consent, sv.ConsentURL
		product = sv.Product
	}
	esc := template.HTMLEscapeString
	// 可见的单位走 unitName（中文站 set → 套）；data-* 与正文提交仍用 PriceUnit 原值。
	unit := func(raw string) string {
		if sv == nil {
			return raw
		}
		return sv.UnitName(raw)
	}
	money := func(v float64) string {
		if v == math.Trunc(v) {
			return strconv.FormatFloat(v, 'f', 0, 64)
		}
		return strconv.FormatFloat(v, 'f', 2, 64)
	}

	var b strings.Builder
	b.WriteString(`<div class="inq" data-inquiry hidden><div class="inq-mask" data-inquiry-close></div>`)
	b.WriteString(`<div class="inq-card" role="dialog" aria-modal="true" aria-labelledby="inq-title">`)
	b.WriteString(`<button class="inq-x" type="button" data-inquiry-close aria-label="` + esc(t("inquiry.close")) + `">` + svgWrap(uiIconPaths["close"], "") + `</button>`)
	b.WriteString(`<h2 class="inq-title" id="inq-title">` + esc(t("inquiry.title")) + `</h2>`)
	// 收件人：阿里的弹窗第一行就写清楚询盘发给谁。公开站只有一个供应商（站点自己），
	// 所以用站点名 + 对外邮箱，不画认证徽标（没有资质数据，画了就是假的）。
	if sv != nil && (sv.SiteName != "" || sv.ContactEmail != "") {
		b.WriteString(`<div class="inq-to"><span class="inq-to-label">` + esc(t("inquiry.recipient")) + `</span>`)
		b.WriteString(`<span class="inq-to-body">`)
		if sv.SiteName != "" {
			b.WriteString(`<strong>` + esc(sv.SiteName) + `</strong>`)
		}
		if sv.ContactEmail != "" {
			b.WriteString(`<span class="inq-to-mail">` + esc(sv.ContactEmail) + `</span>`)
		}
		b.WriteString(`</span></div>`)
	}

	if product != nil && (product.CoverURL != "" || product.Title != "") {
		b.WriteString(`<div class="inq-prod">`)
		if product.CoverURL != "" {
			b.WriteString(`<img class="inq-prod-cover" src="` + esc(product.CoverURL) + `" alt="" loading="lazy">`)
		}
		b.WriteString(`<span class="inq-prod-text"><strong>` + esc(product.Title) + `</strong>`)
		if product.PriceMin > 0 {
			price := product.Currency + money(product.PriceMin)
			if product.PriceMax > product.PriceMin {
				price += "-" + money(product.PriceMax)
			}
			b.WriteString(`<span class="inq-prod-price">` + esc(price))
			if product.PriceUnit != "" {
				b.WriteString(` / ` + esc(unit(product.PriceUnit)))
			}
			b.WriteString(`</span>`)
		}
		// 数量：默认取起订量，提交时拼进正文（ContactForm 不认额外字段，供应商在后台只能读到正文）。
		// 起订量是 0 或 1 时从 1 开始；单位跟商品的价格单位走，没有就不显示。
		qtyMin := 1
		if product.Moq > 1 {
			qtyMin = product.Moq
		}
		b.WriteString(`<span class="inq-qty" data-inq-qty data-inq-qty-min="` + strconv.Itoa(qtyMin) + `"`)
		if product.PriceUnit != "" {
			b.WriteString(` data-inq-qty-unit="` + esc(product.PriceUnit) + `"`)
		}
		b.WriteString(`>`)
		b.WriteString(`<span class="inq-qty-label">` + esc(t("product.quantity")) + `</span>`)
		b.WriteString(`<span class="inq-stepper">`)
		b.WriteString(`<button type="button" class="inq-step" data-inq-qty-minus aria-label="` + esc(t("product.qtyDec")) + `">-</button>`)
		b.WriteString(`<input class="inq-step-input" type="text" inputmode="numeric" autocomplete="off" value="` + strconv.Itoa(qtyMin) + `" data-inq-qty-input aria-label="` + esc(t("product.quantity")) + `">`)
		b.WriteString(`<button type="button" class="inq-step" data-inq-qty-plus aria-label="` + esc(t("product.qtyInc")) + `">+</button>`)
		b.WriteString(`</span>`)
		if product.PriceUnit != "" {
			b.WriteString(`<span class="inq-qty-unit">` + esc(unit(product.PriceUnit)) + `</span>`)
		}
		b.WriteString(`</span>`)
		b.WriteString(`</span></div>`)
	}

	b.WriteString(`<form class="inq-form" method="post" action="` + esc(contactURL) + `" data-inquiry-form novalidate>`)
	// 表单主体单独一层：内容长时只有它滚动，「确认发送」始终钉在卡片底部。
	// 弹窗加上附件、常问快填、共享名片后内容接近一屏，按钮跟着滚下去会让买家以为只有半张表。
	b.WriteString(`<div class="inq-body">`)
	// 详细需求
	b.WriteString(`<label class="inq-field"><span class="inq-label"><i class="inq-req">*</i>` + esc(t("inquiry.detail")) + `</span>`)
	b.WriteString(`<textarea name="content" rows="5" required minlength="5" maxlength="2000" placeholder="` + esc(t("inquiry.placeholder")) + `" data-inquiry-content></textarea></label>`)
	b.WriteString(`<p class="inq-hint">` + esc(t("inquiry.detailHint")) + `</p>`)
	// 附件：买家常要带图纸/规格书/实拍图。文件先传（拿 token），提交时随表单绑定到留言。
	// 上限随页面下发（siteView.Inquiry*），前端不再手抄一份 accept。
	accept := ""
	maxText := ""
	maxFiles := 0
	maxBytes := int64(0)
	if sv != nil {
		accept, maxText, maxFiles, maxBytes = sv.InquiryAccept, sv.InquiryMaxText, sv.InquiryMaxFiles, sv.InquiryMaxBytes
	}
	if maxFiles > 0 {
		b.WriteString(`<div class="inq-attach" data-inq-attach data-max-files="` + strconv.Itoa(maxFiles) + `" data-max-bytes="` + strconv.FormatInt(maxBytes, 10) + `">`)
		b.WriteString(`<button class="inq-attach-btn" type="button" data-inq-attach-btn>` + svgWrap(uiIconPaths["paperclip"], "") + `<span>` + esc(t("inquiry.attach")) + `</span></button>`)
		b.WriteString(`<input class="inq-attach-input" type="file" multiple hidden data-inq-attach-input accept="` + esc(accept) + `">`)
		b.WriteString(`<p class="inq-attach-hint" data-inq-attach-hint>` + esc(tf("inquiry.attachHint", maxText, strconv.Itoa(maxFiles))) + `</p>`)
		b.WriteString(`<ul class="inq-attach-list" data-inq-attach-list hidden></ul>`)
		b.WriteString(`</div>`)
	}
	// 常见问题快填
	b.WriteString(`<div class="inq-suggest"><p class="inq-suggest-title">` + esc(t("inquiry.suggestHint")) + `</p><div class="inq-chips">`)
	for _, q := range []string{"inquiry.q1", "inquiry.q2", "inquiry.q3", "inquiry.q4"} {
		b.WriteString(`<button type="button" class="inq-chip" data-inquiry-chip>` + esc(t(q)) + `</button>`)
	}
	b.WriteString(`</div></div>`)
	// 联系方式：公开站是匿名入口，必须留得上联系方式才能回
	b.WriteString(`<div class="inq-row">`)
	b.WriteString(`<label class="inq-field"><span class="inq-label"><i class="inq-req">*</i>` + esc(t("contact.name")) + `</span><input name="name" required maxlength="64" autocomplete="name"></label>`)
	b.WriteString(`<label class="inq-field"><span class="inq-label"><i class="inq-req">*</i>` + esc(t("contact.email")) + `</span><input name="email" type="email" required maxlength="128" autocomplete="email"></label>`)
	b.WriteString(`</div>`)
	b.WriteString(`<label class="inq-field"><span class="inq-label">` + esc(t("contact.phoneOpt")) + `</span><input name="phone" type="tel" maxlength="32" autocomplete="tel"></label>`)
	b.WriteString(`<label class="inq-hp" aria-hidden="true"><span>Company</span><input name="company" tabindex="-1" autocomplete="off"></label>`)
	b.WriteString(`</div>`) // .inq-body
	// 商品上下文与「共享名片」：后台据此知道这条询盘问的是哪个商品，并能做超时推荐。
	// 这两项和隐私说明、发送按钮一起钉在底部：勾选项滚出视野等于没提示过。
	if product != nil {
		b.WriteString(`<input type="hidden" name="productId" value="` + esc(product.ID.String()) + `">`)
	}
	b.WriteString(`<label class="inq-share"><input type="checkbox" name="cardConsent" value="1" checked><span>` + esc(t("inquiry.cardConsent")) + `</span></label>`)
	if consent {
		b.WriteString(`<label class="inq-consent"><input type="checkbox" name="consent" value="1" required><span>` + esc(t("contact.consent")))
		if consentURL != "" {
			b.WriteString(` <a href="` + esc(consentURL) + `" target="_blank" rel="noopener noreferrer">` + esc(t("contact.consentPolicy")) + `</a>`)
		}
		b.WriteString(`</span></label>`)
	}
	b.WriteString(`<p class="inq-err" data-inquiry-err role="alert" hidden></p>`)
	b.WriteString(`<p class="inq-note">` + esc(t("inquiry.privacy")) + `</p>`)
	b.WriteString(`<button class="inq-send" type="submit">` + esc(t("inquiry.send")) + `</button>`)
	b.WriteString(`</form></div></div>`)
	return template.HTML(b.String())
}

// uiIconPaths 是官网自己的界面小图标（导航箭头、主题切换、客服挂件、搜索框）：
// 与 socialIconPaths 同源 Tabler Icons v3.36.0 outline（MIT），统一走 stroke-width 2；
// 模板里写死一份会让笔画口径各走各的，改由 uiIcon 函数出图，键名与模板的引用由测试兜底。
// mood-smile / language / dots 专门给客服挂件：那三处以前是 ☺ / 文A / ··· 文本字符，
// 字号随系统字体变、笔画粗细跟按钮对不上，一排下来最土的就是它们，统一换成同源线框图标。
var uiIconPaths = map[string]string{
	"chevron-down": `<path d="M6 9l6 6l6 -6" />`,
	"sun":          `<path d="M12 12m-4 0a4 4 0 1 0 8 0a4 4 0 1 0 -8 0" /><path d="M3 12h1m8 -9v1m8 8h1m-9 8v1m-6.4 -15.4l.7 .7m12.1 -.7l-.7 .7m0 11.4l.7 .7m-12.1 -.7l-.7 .7" />`,
	"moon":         `<path d="M12 3c.132 0 .263 0 .393 0a7.5 7.5 0 0 0 7.92 12.446a9 9 0 1 1 -8.313 -12.454z" />`,
	"message":      `<path d="M3 20l1.3 -3.9c-2.324 -3.437 -1.426 -7.872 2.1 -10.374c3.526 -2.501 8.59 -2.296 11.845 .48c3.255 2.777 3.695 7.266 1.029 10.501c-2.666 3.235 -7.615 4.215 -11.574 2.293l-4.7 1" />`,
	"close":        `<path d="M18 6l-12 12" /><path d="M6 6l12 12" />`,
	"paperclip":    `<path d="M15 7l-6.5 6.5a1.5 1.5 0 0 0 3 3l6.5 -6.5a3 3 0 0 0 -6 -6l-6.5 6.5a4.5 4.5 0 0 0 9 9l6.5 -6.5" />`,
	"send":         `<path d="M10 14l11 -11" /><path d="M21 3l-6.5 18a.55 .55 0 0 1 -1 0l-3.5 -7l-7 -3.5a.55 .55 0 0 1 0 -1l18 -6.5" />`,
	"search":       `<path d="M10 10m-7 0a7 7 0 1 0 14 0a7 7 0 1 0 -14 0" /><path d="M21 21l-6 -6" />`,
	"copy":         `<path d="M7 7m0 2.667a2.667 2.667 0 0 1 2.667 -2.667h8.666a2.667 2.667 0 0 1 2.667 2.667v8.666a2.667 2.667 0 0 1 -2.667 2.667h-8.666a2.667 2.667 0 0 1 -2.667 -2.667z" /><path d="M4.012 16.737a2.005 2.005 0 0 1 -1.012 -1.737v-10c0 -1.1 .9 -2 2 -2h10c.75 0 1.158 .385 1.5 1" />`,
	"check":        `<path d="M5 12l5 5l10 -10" />`,
	"shield":       `<path d="M12 3a12 12 0 0 0 8.5 3a12 12 0 0 1 -8.5 15a12 12 0 0 1 -8.5 -15a12 12 0 0 0 8.5 -3" />`,
	// 认证徽章与评价用：盾牌勾（安全合规类认证）、证书（体系类认证）、锯齿徽章（已验证采购）。
	"shield-check": `<path d="M12 3a12 12 0 0 0 8.5 3a12 12 0 0 1 -8.5 15a12 12 0 0 1 -8.5 -15a12 12 0 0 0 8.5 -3" /><path d="M9 12l2 2l4 -4" />`,
	"certificate":  `<path d="M14 3v4a1 1 0 0 0 1 1h4" /><path d="M17 21h-10a2 2 0 0 1 -2 -2v-14a2 2 0 0 1 2 -2h7l5 5v11a2 2 0 0 1 -2 2" /><path d="M9 13l2 2l4 -4" />`,
	"badge-check":  `<path d="M3.85 8.62a4 4 0 0 1 4.78 -4.77a4 4 0 0 1 6.74 0a4 4 0 0 1 4.78 4.78a4 4 0 0 1 0 6.74a4 4 0 0 1 -4.77 4.78a4 4 0 0 1 -6.75 0a4 4 0 0 1 -4.78 -4.77a4 4 0 0 1 0 -6.76" /><path d="M9 12l2 2l4 -4" />`,
	"lock":         `<path d="M5 13a2 2 0 0 1 2 -2h10a2 2 0 0 1 2 2v6a2 2 0 0 1 -2 2h-10a2 2 0 0 1 -2 -2v-6z" /><path d="M11 16a1 1 0 1 0 2 0a1 1 0 0 0 -2 0" /><path d="M8 11v-4a4 4 0 1 1 8 0v4" />`,
	"mood-smile":   `<path d="M12 12m-9 0a9 9 0 1 0 18 0a9 9 0 1 0 -18 0" /><path d="M9 10l.01 0" /><path d="M15 10l.01 0" /><path d="M9.5 15a3.5 3.5 0 0 0 5 0" />`,
	"language":     `<path d="M4 5h7" /><path d="M9 3v2c0 4.418 -2.239 8 -5 8" /><path d="M5 9c0 2.144 2.952 3.908 6.7 4" /><path d="M12 20l4 -9l4 9" /><path d="M19.1 18h-6.2" />`,
	"dots":         `<path d="M5 12m-1 0a1 1 0 1 0 2 0a1 1 0 1 0 -2 0" /><path d="M12 12m-1 0a1 1 0 1 0 2 0a1 1 0 1 0 -2 0" /><path d="M19 12m-1 0a1 1 0 1 0 2 0a1 1 0 1 0 -2 0" />`,
	// 客服头部「放大/还原」是同一个按钮位上的两态，配「清空输入」；
	// minimize 是 maximize 的向内收版本，切换只换图标不换按钮，避免和关闭 × 挤成三个。
	"maximize": `<path d="M4 8v-2a2 2 0 0 1 2 -2h2" /><path d="M4 16v2a2 2 0 0 0 2 2h2" /><path d="M16 4h2a2 2 0 0 1 2 2v2" /><path d="M16 20h2a2 2 0 0 0 2 -2v-2" />`,
	"minimize": `<path d="M15 19v-2a2 2 0 0 1 2 -2h2" /><path d="M15 5v2a2 2 0 0 0 2 2h2" /><path d="M5 15h2a2 2 0 0 1 2 2v2" /><path d="M5 9h2a2 2 0 0 0 2 -2v-2" />`,
	"eraser":   `<path d="M19 20h-10.5l-4.21 -4.3a1 1 0 0 1 0 -1.41l10 -10a1 1 0 0 1 1.41 0l5 5a1 1 0 0 1 0 1.41l-9.2 9.3" /><path d="M18 13.3l-6.3 -6.3" />`,
	// 「安全支付」贸易术语徽标，同样来自 Tabler v3.36.0 outline：
	// bank=电汇 T/T、file-check=信用证 L/C、file-text=托收 D/P·D/A、
	// file-invoice=赊账 O/A、wallet=电子钱包（支付宝/微信）、cash=现金。
	"bank":         `<path d="M3 21l18 0" /><path d="M5 21v-11l7 -5l7 5v11" /><path d="M9 21v-6a2 2 0 0 1 4 0v6" /><path d="M3 10l18 0" />`,
	"file-check":   `<path d="M14 3v4a1 1 0 0 0 1 1h4" /><path d="M17 21h-10a2 2 0 0 1 -2 -2v-14a2 2 0 0 1 2 -2h7l5 5v11a2 2 0 0 1 -2 2z" /><path d="M9 15l2 2l4 -4" />`,
	"file-text":    `<path d="M14 3v4a1 1 0 0 0 1 1h4" /><path d="M17 21h-10a2 2 0 0 1 -2 -2v-14a2 2 0 0 1 2 -2h7l5 5v11a2 2 0 0 1 -2 2z" /><path d="M9 9h6" /><path d="M9 13h6" /><path d="M9 17h4" />`,
	"file-invoice": `<path d="M14 3v4a1 1 0 0 0 1 1h4" /><path d="M18 21h-12a2 2 0 0 1 -2 -2v-14a2 2 0 0 1 2 -2h8l6 6v10a2 2 0 0 1 -2 2z" /><path d="M9 10h3" /><path d="M9 14h6" /><path d="M9 18h4" />`,
	"wallet":       `<path d="M3 7m0 3a3 3 0 0 1 3 -3h12a3 3 0 0 1 3 3v7a3 3 0 0 1 -3 3h-12a3 3 0 0 1 -3 -3z" /><path d="M3 10h18" /><path d="M16 14h2" />`,
	"cash":         `<path d="M7 12m-4 0a4 4 0 1 0 8 0a4 4 0 1 0 -8 0" /><path d="M3 16h8" /><path d="M15 8h2a2 2 0 0 1 2 2v6a2 2 0 0 1 -2 2h-3" /><path d="M12 6h9a1 1 0 0 1 1 1v9" />`,
}

var defaultIcon = `<circle cx="12" cy="12" r="3"/>`

// svgWrap 给路径套上统一的 Tabler 口径（24 网格、stroke-width 2、圆角端点）。
// 页脚社交、界面小图标、详情页分享条都走这一份，站点上不留第二套图标语言。
func svgWrap(paths, class string) string {
	cls := ""
	if class != "" {
		cls = ` class="` + template.HTMLEscapeString(class) + `"`
	}
	return `<svg` + cls + ` viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">` + paths + `</svg>`
}

// shareBarPlatforms 是详情页分享条的按钮顺序：图标直接取页脚的 socialIconPaths，
// 文案走 site.js.* 字典，复制按钮另用 uiIconPaths 的 copy/check/close。
var shareBarPlatforms = []struct{ key, label string }{
	{"wechat", "site.js.shareWechat"},
	{"weibo", "site.js.shareWeibo"},
	{"facebook", "site.js.shareFacebook"},
	{"x", "site.js.shareX"},
	{"linkedin", "site.js.shareLinkedIn"},
}

// shareBar 在服务端渲染详情页（产品/文章）的分享条，site.js 只负责绑事件。
// 以前这条分享条的图标是 site.js 里另存的 @ant-design/icons 实心路径，和页脚的 Tabler
// 线框图标成了两套风格；现在统一回 Tabler，JS 不再自带任何图标数据。
func shareBar(v any) template.HTML {
	t := func(k string) string { return k }
	if sv, ok := v.(*siteView); ok && sv != nil {
		t = sv.T
	}
	var b strings.Builder
	b.WriteString(`<div class="dt-share" data-share-bar><span class="dt-share-label">`)
	b.WriteString(template.HTMLEscapeString(t("site.js.shareLabel")))
	b.WriteString(`</span><div class="dt-share-icons">`)
	for _, p := range shareBarPlatforms {
		tip := template.HTMLEscapeString(t(p.label))
		b.WriteString(`<button class="dt-share-btn" type="button" data-share="` + p.key + `" data-tip="` + tip + `" aria-label="` + tip + `">`)
		b.WriteString(svgWrap(socialIconPaths[p.key], ""))
		b.WriteString(`</button>`)
	}
	copyTip := template.HTMLEscapeString(t("site.js.shareCopy"))
	b.WriteString(`<button class="dt-share-btn" type="button" data-share="copy" data-tip="` + copyTip + `" aria-label="` + copyTip + `">`)
	// 复制成功/失败时只切状态 class，三个图标都常驻 DOM（见 site.js 的 DT_CSS）。
	b.WriteString(`<span class="dt-ico dt-ico-copy">` + svgWrap(uiIconPaths["copy"], "") + `</span>`)
	b.WriteString(`<span class="dt-ico dt-ico-check">` + svgWrap(uiIconPaths["check"], "") + `</span>`)
	b.WriteString(`<span class="dt-ico dt-ico-fail">` + svgWrap(uiIconPaths["close"], "") + `</span>`)
	b.WriteString(`</button>`)
	b.WriteString(`</div></div>`)
	return template.HTML(b.String())
}

// featureIconOrder 是管理端图标下拉的展示顺序；键集合必须与 iconPaths 完全一致（有测试兜底），
// 下拉的预览由 /plugins/website/feature-icons 直接读 iconPaths，避免前端另存一份路径后与官网走样。
var featureIconOrder = []string{
	"building", "shield", "key", "plug", "file", "chart", "globe", "puzzle",
	"terminal", "zap", "layers", "box", "users", "bell", "lock", "code", "mail", "phone",
}

var tplFuncs = template.FuncMap{
	// assetV 给静态资源做内容哈希缓存键（见 assetVersion）。
	"assetV": assetVersion,
	// safeHTML 只用于落库前已经过 htmlsafe.Sanitize 的富文本内容。
	"safeHTML": func(s string) template.HTML { return template.HTML(s) },
	"icon": func(name string) template.HTML {
		p, ok := iconPaths[name]
		if !ok {
			p = defaultIcon
		}
		return template.HTML(svgWrap(p, ""))
	},
	"socialIcon": func(platform string) template.HTML {
		p, ok := socialIconPaths[platform]
		if !ok {
			p = socialIconPaths["rss"]
		}
		return template.HTML(svgWrap(p, ""))
	},
	// uiIcon 出界面小图标；第二个参数把 CSS 需要的 class 留在 svg 上（.ico-sun/.ico-moon 靠它切换显隐）。
	"uiIcon": func(name string, class ...string) template.HTML {
		cls := ""
		if len(class) > 0 {
			cls = class[0]
		}
		p, ok := uiIconPaths[name]
		if !ok {
			p = defaultIcon
		}
		return template.HTML(svgWrap(p, cls))
	},
	// shareBar 出详情页分享条（图标与页脚同源，见函数注释）。
	"shareBar": shareBar,
	// inquiryDialog 出商详页的「发送询盘」弹窗。
	"inquiryDialog": inquiryDialog,
	"fmtDate":       func(l Language, v any) string { return formatDate(l, v) },
	"hasPrefix":     strings.HasPrefix,
	// isMediaURL 判断视频地址是不是浏览器能直接播放的媒体文件（.mp4/.webm/.ogg/.m4v）：
	// 是就渲染 <video> 控件，否则（YouTube 等页面地址）渲染一条外链按钮。
	"isMediaURL": func(s string) bool {
		u := strings.ToLower(strings.TrimSpace(s))
		for _, ext := range []string{".mp4", ".webm", ".ogg", ".m4v"} {
			if strings.HasSuffix(u, ext) {
				return true
			}
		}
		return false
	},
	"titleLines": func(s string) []string {
		return strings.Split(strings.TrimSpace(s), "\n")
	},
	"initial": func(s string) string {
		s = strings.TrimSpace(s)
		if s == "" {
			return "N"
		}
		r := []rune(s)
		return string(unicode.ToUpper(r[0]))
	},
	// money 把价格压成阿里商详那种紧凑写法：整数不带小数，非整数保留两位。
	// 列表卡片窄，9800.00-12200.00 会折行，9800-12200 能一行放下。
	"money": func(v float64) string {
		if v == math.Trunc(v) {
			return strconv.FormatFloat(v, 'f', 0, 64)
		}
		return strconv.FormatFloat(v, 'f', 2, 64)
	},
	// specOf 按一组候选标签取商品的第一个匹配规格值（主题选型表用）：
	// 标签在库里的写法与语言相关，传入多个语言的写法（如 "功率", "Power"）谁先命中用谁。
	"specOf": func(p Product, labels ...string) string {
		want := map[string]bool{}
		for _, l := range labels {
			want[strings.TrimSpace(l)] = true
		}
		for _, spec := range p.ProductSpecs() {
			if want[strings.TrimSpace(spec.Label)] && spec.Value != "" {
				return spec.Value
			}
		}
		return ""
	},
	// hasTerm 报告新闻是否挂在某个栏目 slug 下（主题案例区用它从最新新闻里筛案例）。
	"hasTerm": func(p Post, slug string) bool {
		for _, c := range p.Categories {
			if c.Slug == slug {
				return true
			}
		}
		return false
	},
}

// layout 是所有页面共用的骨架；各页面模板定义自己的 "content"。
var layoutTmpl = template.Must(template.New("layout").Funcs(tplFuncs).ParseFS(tplFS, "tpl/layout.tmpl"))

func mustPage(name string) *template.Template {
	return template.Must(template.Must(layoutTmpl.Clone()).ParseFS(tplFS, "tpl/"+name+".tmpl"))
}

var pageTmpl = map[string]*template.Template{
	"docs":       mustPage("docs"),
	"docs-index": mustPage("docs-index"),
	"home":       mustPage("home"),
	"plugins":    mustPage("plugins"),
	"plugin":     mustPage("plugin"),
	"news":       mustPage("news"),
	"post":       mustPage("post"),
	"products":   mustPage("products"),
	"product":    mustPage("product"),
	"page":       mustPage("page"),
	"contact":    mustPage("contact"),
	"notfound":   mustPage("notfound"),
	"error":      mustPage("error"),
}

// themePacks 是主题插件注册的已编译模板集；default 恒用 pageTmpl 本身（见 theme.go）。
var themePacks = map[string]*themePack{}

// themePack 是一套主题编译后的模板集：[页面名] → 模板。缺页面回退 default 的模板，
// 所以主题只需要覆盖自己关心的页面（layout/home/内容页等）。
type themePack struct {
	tmpl map[string]*template.Template
}

// mustThemePage 解析主题的一张页面模板：layout 与页面都来自主题自己的文件系统。
func mustThemePage(fsys fs.FS, name string) *template.Template {
	layout := template.Must(template.New("layout").Funcs(tplFuncs).ParseFS(fsys, "tpl/layout.tmpl"))
	return template.Must(template.Must(layout.Clone()).ParseFS(fsys, "tpl/"+name+".tmpl"))
}

// buildThemePack 编译一套主题模板集；layout 缺失报错（没有骨架就没法渲染）。
func buildThemePack(t *Theme) (*themePack, error) {
	if _, err := fs.Stat(t.FS, "tpl/layout.tmpl"); err != nil {
		return nil, fmt.Errorf("theme %s 缺少 tpl/layout.tmpl", t.ID)
	}
	pack := &themePack{tmpl: map[string]*template.Template{}}
	for _, name := range pageNames {
		if _, err := fs.Stat(t.FS, "tpl/"+name+".tmpl"); err == nil {
			pack.tmpl[name] = mustThemePage(t.FS, name)
			continue
		}
		def, ok := pageTmpl[name]
		if !ok {
			return nil, fmt.Errorf("theme %s 缺少模板 %s 且 default 也没有", t.ID, name)
		}
		pack.tmpl[name] = def
	}
	return pack, nil
}

// siteView 是模板数据。公共字段来自站点设置与导航；Title/Desc 用于 <title> 与 meta。
type siteView struct {
	// ThemeID 是站点设置选中的主题 id：render 据此选模板集（"" = default）。
	// 不进模板逻辑，仅渲染出口消费；主题模板可以用 {{.ThemeID}} 区分自己。
	ThemeID  string
	SiteName string
	SiteDesc string
	LogoURL  string
	// FaviconURL / OgImageURL 是站点设置里的资产：浏览器标签图标与默认分享图（og:image 兜底），
	// 空 = 模板回退内置 favicon.svg / og.png。
	FaviconURL string
	OgImageURL string
	// HeadCode 是站点设置里的自定义 <head> 代码（统计脚本等）：管理员可信输入，经
	// template.HTML 显式标记后原样注入；空 = 不渲染任何内容。
	HeadCode template.HTML
	// TitleSuffix 拼在 <title> 的「{标题}」之后；空 = 「 - {站点名}」。由 composeTitle 消费。
	TitleSuffix string
	// HomeSlug 是被设为首页的单页 slug（站点设置）；空 = 内置首页。
	// 不进模板，由 home 处理器读取决定渲染哪张页面。
	HomeSlug     string
	HomeTitle    string
	HomeSubtitle string
	HomeCTALabel string
	HomeCTAHref  string
	ContactEmail string
	ContactPhone string
	// 供应商实力档案（对标阿里商详供应商卡）：全部来自站点设置，
	// 空值由模板逐个判断，缺哪项就不渲染那一格。
	EstablishedYear int
	StaffCount      string
	AnnualRevenue   string
	MainMarkets     string
	ResponseTime    string
	QRCodeURL       string
	QRCodeCaption   string
	Socials         []SocialLink
	FriendLinks     []FriendLink
	ICP             string
	FooterNote      string
	SeoKeywords     string
	BaseURL         string // 绝对地址，用于 canonical / og:url / og:image
	Path            string // 当前请求路径（含语言前缀），用于 canonical
	Route           string // 去掉语言前缀的站内路径，用于导航高亮与语言切换
	Query           string // 原始 query，语言切换时保留筛选
	Navs            []Nav
	Features        []Feature
	Title           string
	Desc            string
	// SeoTitle 是内容级的完整 meta 标题覆盖：空 = 用 Title（「标题 - 站点名」的组合交给装配方）。
	SeoTitle string
	OGImage  string // 绝对地址；空 = 用站点默认分享图
	Noindex  bool
	Posts    []Post
	// ProductsPage / ProductsTotalPages 是公开产品列表的分页状态（products.tmpl 渲染翻页链接）。
	Products           []Product
	ProductsPage       int
	ProductsTotalPages int
	// ProductCategories 是产品页的类目清单（先序扁平，含层级与当前高亮/祖先链）。
	ProductCategories []productCatLink
	// ProductCategoryTree 是同一份类目的层级树（一级为根、二级挂在 Children 上），
	// 左栏按阿里店铺的 hover 浮层渲染，不需要前端再按深度拼层级。
	ProductCategoryTree []productCatLink
	// ProductTermName / ProductTermQuery 说明这一页的筛选状态（类目 + 检索词）：
	// 空 = 全部产品；筛选串必须进 canonical 与翻页链接，语义同 NewsTermQuery。
	ProductTermName  string
	ProductTermQuery string
	// ProductTermSlug 是当前类目 slug，供搜索表单以 hidden 字段保留分类；
	// ProductQuery 是当前检索词，ProductAllURL 是带检索词的「全部产品」链接。
	ProductTermSlug string
	ProductQuery    string
	ProductAllURL   string
	// ProductsTotal 是筛选后的命中数，结果栏展示「共 N 个产品」；
	// ProductSort 是当前排序口径，ProductsFeatured 是左栏「精选橱窗」的最新商品。
	ProductsTotal    int
	ProductSort      string
	ProductsFeatured []Product
	Product          *Product
	// InquiryAccept / InquiryMaxBytes / InquiryMaxText / InquiryMaxFiles 是询盘附件的生效限制，
	// 只有商详页填（弹窗在那里）；空表示附件不可用（存储未装配）。
	InquiryAccept   string
	InquiryMaxBytes int64
	InquiryMaxText  string
	InquiryMaxFiles int
	// PostPrev/PostNext、ProductPrev/ProductNext 是详情页按各自列表顺序取的相邻两项；
	// 没有相邻内容（只有一篇、或是预览中的草稿）时为 nil，模板整段不渲染导航。
	PostPrev    *ContentNeighbor
	PostNext    *ContentNeighbor
	ProductPrev *ContentNeighbor
	ProductNext *ContentNeighbor
	// ProductsRelated 是详情页的「同类产品」推荐（同类目已上架商品，不含自身）。
	ProductsRelated []Product
	// NewsPage / NewsTotalPages 是公开新闻列表的分页状态（news.tmpl 渲染翻页链接）。
	NewsPage       int
	NewsTotalPages int
	// NewsCategories 是新闻页的栏目导航（含当前高亮那一项）。
	NewsCategories []termLink
	// NewsKind/NewsTermName 说明这一页是栏目还是标签归档：空 = 全部新闻。
	NewsKind     string
	NewsTermName string
	// NewsTermQuery 是当前归档的筛选查询串（category=launch / tag=性能优化）：
	// canonical、og:url 与翻页都要带上它，空 = 全部新闻。
	NewsTermQuery string
	Post          *Post
	Page          *Page
	Year          int
	// 联系页的提交结果：无 JS 的整页提交回跳后由模板渲染提示。
	ContactSent  bool
	ContactError string

	// Consent / ConsentURL：站点开启隐私同意勾选时的勾选项与政策地址。
	Consent    bool
	ConsentURL string

	Lang      Language
	Languages []Language
	// Suggest/SuggestLang：无语言 cookie 的默认语言首页，对浏览器偏好其他语言的访客给出提示。
	Suggest     bool
	SuggestLang Language

	// 插件市场（目录来自 config/plugin-market.yaml，与后台插件市场同源）
	MarketEnabled    bool
	MarketAvailable  bool
	PluginCount      int
	MarketFiltered   int
	MarketQuery      string
	MarketCategory   string
	MarketEdition    string
	MarketCategories []marketChip
	MarketEditions   []marketChip
	Plugins          []marketCard
	Plugin           *marketCard
	Related          []marketCard
	FeaturedPlugins  []marketCard
	MarketError      string
	InstallCommand   string
	AdminInstallURL  string
	MarketSourceURL  string
	DownloadURL      string
	DownloadFilename string
	PluginLD         template.JS
	// OrgLD 是站级 Organization + WebSite JSON-LD：render() 装配（依赖 BaseURL），layout 恒定输出。
	OrgLD template.JS
	// PageLD 是详情页的结构化数据（产品 Product+Offer / 新闻 Article，均带 BreadcrumbList）：
	// 由各自的详情处理器装配，空 = 该页没有富摘要。
	PageLD template.JS
	// OGType 覆盖 og:type（详情页为 article）；空 = website。
	OGType string
	// OGPublishedAt 是文章的发布时间（article:published_time，RFC3339）；空 = 不输出。
	OGPublishedAt string
	MyScore       int
	Signed        bool
	SignatureURL  string
	PublicKeyURL  string

	// 文档区（/docs，渲染 nexadmin/docs 内嵌的框架使用文档）
	DocsEnabled bool
	DocNav      []DocGroup
	Doc         *DocView
	DocCards    []DocCard
	DocPrev     *DocLink
	DocNext     *DocLink
}

// T 取静态文案；语言缺键时回退英文/默认语言，最后回退 key 本身。
func (v *siteView) T(key string) string { return tr(v.Lang.Code, key) }

// SocialLabel 是社交图标的读屏/悬停名称。
func (v *siteView) SocialLabel(platform string) string { return v.T("social." + platform) }

// TF 是带参数的文案（字典里用 %s / %d）。
func (v *siteView) TF(key string, args ...any) string { return trf(v.Lang.Code, key, args...) }

// TN 是按数量选单复数的文案：n==1 用 <key>.one，其余用 <key>.other。
// 中文两种写法相同；新增有单复数规则的语言时按该语言字典补齐即可。
func (v *siteView) TN(n any, key string, args ...any) string {
	one := false
	switch x := n.(type) {
	case int:
		one = x == 1
	case int64:
		one = x == 1
	case float64:
		one = x == 1
	}
	l := languageOr(v.Lang.Code)
	k := key + ".other"
	if one {
		k = key + ".one"
	}
	if d := dicts[l.Code]; d != nil {
		if _, ok := d[k]; !ok {
			k = key
		}
	}
	return trf(l.Code, k, args...)
}

// Home 返回当前语言的首页链接。
// 根语言（站点默认语言）的首页是无前缀的 "/"，它同时是语言中立入口：服务端会按 cookie /
// 浏览器语言把访客送回合适的选择（langNegotiate）。所以站内链接必须显式带 ?lang=——否则访客
// 正在读中文深链、浏览器却存着旧的英文 cookie 时，点「首页」会被送去别的语言。
func (v *siteView) Home() string {
	if v.Lang.Code == SiteDefaultLanguage().Code {
		return "/?lang=" + url.QueryEscape(v.Lang.Code)
	}
	return v.Lang.Home()
}

// URL 给站内页面链接加语言前缀；锚点回到当前语言首页，外部链接与下载/后台端点保持原样。
func (v *siteView) URL(href string) string {
	if href == "" {
		return ""
	}
	if strings.HasPrefix(href, "#") {
		// 已在首页时锚点保持同页跳转，不必绕一次语言入口。
		if v.Route == "/" {
			return href
		}
		return v.Home() + href
	}
	if !strings.HasPrefix(href, "/") || isNonLocalizedPath(href) {
		return href
	}
	// 根路径对默认语言是语言中立入口，必须显式声明语言（见 Home）。
	if p := v.Lang.Path(href); p != "/" {
		return p
	}
	return v.Home()
}

// Alternate 返回同一页面在另一语言下的规范地址（不含 query，供 hreflang / og:locale:alternate）。
func (v *siteView) Alternate(l Language) string {
	route := v.Route
	if route == "" {
		route = "/"
	}
	return l.Path(route)
}

// Canonical 是本页的规范地址：归档筛选必须一起进 canonical，否则 /news?category=x 会被
// 搜索引擎当成 /news 的重复页。整串在 Go 里拼好、模板只放一个动作——
// html/template 对「文本 + 动作」拼接的 URL 会把动作里的 `=` 编码成 %3d，链接就废了。
func (v *siteView) Canonical() string {
	// 产品中心的分类 / 检索筛选同样要进 canonical，否则 /products?category=x
	// 会被当成 /products 的重复页；整串在 Go 里拼好，模板只放一个动作。
	if v.Route == "/products" && v.ProductTermQuery != "" {
		return v.BaseURL + v.Path + "?" + v.ProductTermQuery
	}
	if v.NewsTermQuery == "" {
		return v.BaseURL + v.Path
	}
	return v.BaseURL + v.Path + "?" + v.NewsTermQuery
}

// NewsPrevURL / NewsNextURL 返回公开新闻列表的上一页/下一页链接；没有对应页返回空串。
// 第 1 页就是规范地址 /news，翻页链接不带 ?page=1。
func (v *siteView) NewsPrevURL() string {
	if v.NewsPage <= 1 {
		return ""
	}
	return v.newsURL(v.NewsPage - 1)
}

func (v *siteView) NewsNextURL() string {
	if v.NewsPage < 1 || v.NewsPage >= v.NewsTotalPages {
		return ""
	}
	return v.newsURL(v.NewsPage + 1)
}

// newsURL 拼公开新闻列表的链接：栏目/标签筛选必须跟着翻页一起走，
// 否则在归档页第 2 页会掉回「全部动态」。
func (v *siteView) newsURL(page int) string {
	vals, _ := url.ParseQuery(v.NewsTermQuery)
	if page > 1 {
		vals.Set("page", strconv.Itoa(page))
	}
	base := v.URL("/news")
	if enc := vals.Encode(); enc != "" {
		return base + "?" + enc
	}
	return base
}

// ProductsPrevURL / ProductsNextURL 返回公开产品列表的上一页/下一页链接，语义同新闻列表。
func (v *siteView) ProductsPrevURL() string {
	if v.ProductsPage <= 1 {
		return ""
	}
	return v.productsURL(v.ProductsPage - 1)
}

func (v *siteView) ProductsNextURL() string {
	if v.ProductsPage < 1 || v.ProductsPage >= v.ProductsTotalPages {
		return ""
	}
	return v.productsURL(v.ProductsPage + 1)
}

// productsURL 拼公开产品列表的链接：类目归档筛选跟着翻页走（口径同 newsURL）。
func (v *siteView) productsURL(page int) string {
	vals, _ := url.ParseQuery(v.ProductTermQuery)
	if page > 1 {
		vals.Set("page", strconv.Itoa(page))
	}
	base := v.URL("/products")
	if enc := vals.Encode(); enc != "" {
		return base + "?" + enc
	}
	return base
}

// Switch 是语言切换菜单的链接：切到根语言（站点默认语言）时带 ?lang= 覆盖已记住的语言
// cookie，其余情况保留当前筛选参数。
func (v *siteView) Switch(l Language) string {
	route := v.Route
	if route == "" {
		route = "/"
	}
	vals, _ := url.ParseQuery(v.Query)
	vals.Del("lang")
	if l.Code == SiteDefaultLanguage().Code {
		vals.Set("lang", l.Code)
	}
	if enc := vals.Encode(); enc != "" {
		return l.Path(route) + "?" + enc
	}
	return l.Path(route)
}

// JSStrings 把前端脚本用到的文案以 JSON 交给 site.js（无内联脚本时也能工作）。
func (v *siteView) JSStrings() template.JS {
	m := map[string]string{}
	for _, k := range []string{
		"packaging", "packagingHint", "packFailed", "copied", "copyManual", "menuOpen", "menuClose", "emailCopy", "emailCopied",
		"zoom", "zoomClose", "zoomPrev", "zoomNext", "zoomError",
		"zoomIn", "zoomOut", "zoomOneToOne", "zoomReset", "zoomPlay", "zoomRotateLeft", "zoomRotateRight", "zoomFlipX", "zoomFlipY",
		"contactSending", "contactSent", "contactFailed", "contactRate", "contactErrName", "contactErrEmail", "contactErrContent", "contactErrConsent",
		"chatErrSend", "chatErrLoad", "chatClosed", "chatGuest", "chatAgent",
		"chatClosedTitle", "chatClosedAuto", "chatNewNote", "chatRateFailed", "chatUploadFailed", "chatInviteDefault",
		"shareLabel", "shareWechat", "shareWeibo", "shareFacebook", "shareX", "shareLinkedIn", "shareCopy",
		"shareWechatTitle", "shareWechatTip", "shareSystemShare", "shareClose", "shareCopied", "shareCopyFailed",
		"catExpand", "catCollapse",
		"shareCopiedShort", "shareFailedShort",
		"chatTranslateSame", "chatTranslated", "chatTranslateFailed", "chatTranslateIn", "chatTranslateOriginal",
		"chatTranslateSettings", "chatTranslateAutoIn", "chatTranslateInTo", "chatTranslateAutoOut", "chatTranslateOutFrom", "chatTranslateOutTo",
		"chatTranslateHintIn", "chatTranslateHintOut", "chatTranslateSave",
		"chatLocalTime", "chatProductLabel", "chatQuantity", "chatGreeting", "chatCollapse", "chatExpand", "subtotal",
		"skuSelected", "skuLowStock", "skuOutOfStock",
		"inqMetaProduct", "inqMetaQty",
		"inqAttachUploading", "inqAttachRemove", "inqAttachTooLarge", "inqAttachType", "inqAttachFailed", "inqAttachLimit",
		"chatNameCard", "chatAddressCard", "chatCardNameTitle", "chatCardAddressTitle", "chatCardHint", "chatCardName", "chatCardCompany", "chatCardJob", "chatCardEmail", "chatCardPhone",
		"chatCardRecipient", "chatCardCountry", "chatCardRegion", "chatCardStreet", "chatCardPostcode", "chatCardSend", "chatCardCancel", "chatCardRequired", "chatCardNameLabel", "chatCardAddressLabel",
		"chatCardCountrySelect", "chatCardCountrySearch", "chatCardRegionSelect", "chatCardRegionSearch", "chatCardSearchEmpty", "chatCardCity", "chatCardCitySelect", "chatCardCityEmpty", "chatCardCitySearch",
	} {
		m[k] = v.T("site.js." + k)
	}
	b, _ := json.Marshal(m)
	return template.JS(b)
}

// LangsJSON 把站点语言清单以 JSON 交给客服挂件的翻译设置（选择源/目标语言用）：
// 挂件是无脚本不工作的增强层，语言码必须在站点已配置清单里，前端不自带名单。
func (v *siteView) LangsJSON() template.JS {
	type langChip struct {
		C string `json:"c"`
		N string `json:"n"`
		D bool   `json:"d,omitempty"` // 站点默认语言：翻译设置的「翻译成」默认值
	}
	chips := make([]langChip, 0, len(v.Languages))
	for _, l := range v.Languages {
		chips = append(chips, langChip{C: l.Code, N: l.Label, D: l.Default})
	}
	b, _ := json.Marshal(chips)
	return template.JS(b)
}

// ensureLang 给手工构造的 siteView（测试、降级渲染）补齐语言字段。
func (v *siteView) ensureLang() {
	if v.Lang.Code == "" {
		v.Lang = defaultLang
	}
	if len(v.Languages) == 0 {
		v.Languages = Languages()
	}
}
