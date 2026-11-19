package website

import (
	"strings"
	"testing"
)

// 提示块只该吃掉带 [!TYPE] 标记的引用块，普通引用必须原样活着。
func TestApplyCallouts(t *testing.T) {
	cases := []struct {
		name     string
		in       string
		lang     string
		want     []string
		wantKeep []string
	}{
		{
			name: "标记独占一段",
			in:   "<blockquote>\n<p>[!WARNING]</p>\n<p>停服再改配置。</p>\n</blockquote>",
			lang: "zh",
			want: []string{`class="docs-callout docs-callout--warning"`, `class="docs-callout-title">警告</p>`, "<p>停服再改配置。</p>"},
		},
		{
			name: "标记与正文同段",
			in:   "<blockquote>\n<p>[!TIP]\n先备份数据库。</p>\n</blockquote>",
			lang: "en",
			want: []string{`docs-callout--tip`, `class="docs-callout-title">Tip</p>`, "先备份数据库。</p>"},
		},
		{
			name: "普通引用不动",
			in:   "<blockquote>\n<p>引用了一句话。</p>\n</blockquote>",
			lang: "zh",
			wantKeep: []string{"<blockquote>", "引用了一句话。"},
		},
		{
			name: "未知类型当普通引用",
			in:   "<blockquote>\n<p>[!DANGER]</p>\n<p>x</p>\n</blockquote>",
			lang: "zh",
			wantKeep: []string{"[!DANGER]", "<blockquote>"},
		},
		{
			name: "块内嵌套引用要跟着走",
			in:   "<blockquote>\n<p>[!NOTE]</p>\n<blockquote>\n<p>内层</p>\n</blockquote>\n<p>尾段</p>\n</blockquote><p>之后</p>",
			lang: "zh",
			want: []string{"docs-callout--note", "内层", "尾段"},
			wantKeep: []string{"<p>之后</p>"},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := applyCallouts(c.in, c.lang)
			for _, w := range c.want {
				if !strings.Contains(got, w) {
					t.Fatalf("缺少 %q，实际输出：%s", w, got)
				}
			}
			for _, w := range c.wantKeep {
				if !strings.Contains(got, w) {
					t.Fatalf("丢了 %q，实际输出：%s", w, got)
				}
			}
			if len(c.want) > 0 && strings.Contains(got, "<blockquote>") && !strings.Contains(got, "内层") {
				t.Fatalf("提示块没被替换：%s", got)
			}
		})
	}
}
