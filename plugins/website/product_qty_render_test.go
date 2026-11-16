package website

import (
	"strings"
	"testing"
)

// TestProductTemplateQuantity 钉住详情页右栏的「数量」步进器：
//  1. 默认值跟起订量一致（MOQ 5 → 5），没有 MOQ 就是 1
//  2. 下限(data-pd-qty-min) 同步写到 DOM 上，site.js 取它当减号的下界
//  3. 计量单位透传到 data-pd-qty-unit，询价时跟着数量一起发出去
//
// 这几个值都由模板算，脚本只读不算：模板改了但脚本没跟上时这里会先红。
func TestProductTemplateQuantity(t *testing.T) {
	tpl, ok := themePackFor("")["product"]
	if !ok {
		t.Fatal("product 模板缺失")
	}
	render := func(p *Product) string {
		v := &siteView{
			SiteName: "Solaris",
			Lang:     Language{Code: "zh-CN"},
			Product:  p,
		}
		var b strings.Builder
		if err := tpl.ExecuteTemplate(&b, "content", v); err != nil {
			t.Fatalf("渲染失败: %v", err)
		}
		return b.String()
	}

	base := Product{Slug: "demo", Title: "Demo", Currency: "USD"}

	// 没有 MOQ：从 1 起，减号一开始就应该是灰的
	html := render(&base)
	if !strings.Contains(html, `data-pd-qty-min="1"`) {
		t.Errorf("没有 MOQ 时下限应为 1，实际：%s", excerpt(html, "pd-qty"))
	}
	if !strings.Contains(html, `value="1"`) {
		t.Errorf("没有 MOQ 时默认数量应为 1")
	}

	// MOQ 5：默认就是 5，且下限 5
	p := base
	p.Moq = 5
	p.PriceUnit = "set"
	html = render(&p)
	if !strings.Contains(html, `data-pd-qty-min="5"`) {
		t.Errorf("MOQ 5 时下限应为 5，实际：%s", excerpt(html, "pd-qty"))
	}
	if !strings.Contains(html, `value="5"`) {
		t.Errorf("MOQ 5 时默认数量应预填 5")
	}
	if !strings.Contains(html, `data-pd-qty-unit="set"`) {
		t.Errorf("计量单位没带到 data-pd-qty-unit 上")
	}
}
