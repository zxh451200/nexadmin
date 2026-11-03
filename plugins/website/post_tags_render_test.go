package website

import (
	"strings"
	"testing"
)

// 分类与标签必须在公开侧真的可点：栏目是访客浏览动态的入口，标签是文章之间的横向联系。
// 归档页还要把筛选参数带进 canonical 与翻页链接，否则搜索引擎会把每个归档当成 /news 本身，
// 访客翻到第 2 页也会掉回「全部动态」。

func testCategories() []Category {
	return []Category{
		{Title: "产品动态", Slug: "product", PostCount: 3},
		{Title: "插件发布", Slug: "plugins", PostCount: 1},
	}
}

func TestNewsListRendersCategoryNav(t *testing.T) {
	v := testView()
	v.NewsCategories = CategoryLinks(testCategories(), "")
	v.Posts[0].Categories = testCategories()[:1]
	var b strings.Builder
	if err := pageTmpl["news"].ExecuteTemplate(&b, "layout", v); err != nil {
		t.Fatal(err)
	}
	out := b.String()
	for _, want := range []string{`?category=product`, `?category=plugins`, `产品动态`, `插件发布`} {
		if !strings.Contains(out, want) {
			t.Errorf("新闻列表没有渲染栏目导航项 %q", want)
		}
	}
	// 站点主导航自己也在用 aria-current，判定高亮只能在栏目导航这一段里看。
	if nav := termNav(out); strings.Contains(nav, `aria-current="true"`) {
		t.Errorf("未筛选时不该有高亮栏目：%s", nav)
	}
	// 列表整行是链接，行内的栏目只能是徽标不能再套一层 <a>。
	row := out[strings.Index(out, `class="post-row"`) : strings.Index(out, `post-row-arrow`)]
	if strings.Contains(row, "<a") && strings.Contains(row, "?category=") {
		t.Errorf("列表行内嵌了栏目链接（<a> 套 <a> 是非法 HTML）")
	}
}

// termNav 截出栏目导航那一段，避免把站点主导航的属性算进来。
func termNav(out string) string {
	i := strings.Index(out, `class="chips term-nav"`)
	if i < 0 {
		return ""
	}
	end := strings.Index(out[i:], "</nav>")
	if end < 0 {
		return out[i:]
	}
	return out[i : i+end]
}

func TestNewsArchiveCarriesTermAcrossCanonicalAndPager(t *testing.T) {
	v := testView()
	v.Path = "/news" // 生产上由请求路径决定，测试夹具要自己摆到归档页的位置
	v.NewsKind, v.NewsTermName, v.NewsTermQuery = "category", "产品动态", "category=product"
	v.NewsCategories = CategoryLinks(testCategories(), "product")
	v.NewsPage, v.NewsTotalPages = 2, 3
	var b strings.Builder
	if err := pageTmpl["news"].ExecuteTemplate(&b, "layout", v); err != nil {
		t.Fatal(err)
	}
	out := b.String()
	if !strings.Contains(out, `<link rel="canonical" href="http://localhost:8080/news?category=product">`) {
		t.Errorf("归档页 canonical 没带上筛选参数：%s", canonicalSnippet(out))
	}
	if !strings.Contains(out, "分类：产品动态") {
		t.Errorf("归档页标题没说明当前栏目")
	}
	if !strings.Contains(out, `rel="next" href="/news?category=product&amp;page=3"`) {
		t.Errorf("下一页丢了栏目筛选：%s", pagerSnippet(out))
	}
	if nav := termNav(out); !strings.Contains(nav, `aria-current="true"`) {
		t.Errorf("当前栏目没有标 aria-current：%s", nav)
	}
	// 翻回第 1 页要回到归档的规范地址：带筛选，但不带 ?page=1。
	if !strings.Contains(out, `rel="prev" href="/news?category=product"`) {
		t.Errorf("上一页链接形态不对：%s", pagerSnippet(out))
	}
}

// 标签归档同理：中文 slug 由模板编码进 canonical，链接仍然可分享。
func TestTagArchiveRendersEncodedSlug(t *testing.T) {
	v := testView()
	v.Path = "/news"
	v.NewsKind, v.NewsTermName, v.NewsTermQuery = "tag", "性能优化", "tag=性能优化"
	var b strings.Builder
	if err := pageTmpl["news"].ExecuteTemplate(&b, "layout", v); err != nil {
		t.Fatal(err)
	}
	out := b.String()
	if !strings.Contains(out, "标签：性能优化") {
		t.Errorf("标签归档标题不对")
	}
	// html/template 的百分号转义是小写十六进制，断言要按实际渲染结果写。
	if !strings.Contains(out, `href="http://localhost:8080/news?tag=%e6%80%a7%e8%83%bd%e4%bc%98%e5%8c%96"`) {
		t.Errorf("标签归档的 canonical 没带上编码后的筛选参数：%s", canonicalSnippet(out))
	}
}

func TestArticleTaxonomyLinks(t *testing.T) {
	v := testView()
	v.Post = &v.Posts[0]
	v.Post.Categories = testCategories()[:1]
	v.Post.Tags = []Tag{{Name: "性能优化", Slug: "性能优化"}, {Name: "release", Slug: "release"}}
	var b strings.Builder
	if err := pageTmpl["post"].ExecuteTemplate(&b, "layout", v); err != nil {
		t.Fatal(err)
	}
	out := b.String()
	if !strings.Contains(out, `?category=product`) {
		t.Errorf("文章页没有渲染可点的栏目")
	}
	if !strings.Contains(out, `?tag=release`) {
		t.Errorf("文章页没有渲染可点的标签")
	}
	// 中文标签名进 URL 由模板百分号编码（小写十六进制），断言按实际渲染结果写。
	if !strings.Contains(out, "?tag=%e6%80%a7%e8%83%bd%e4%bc%98%e5%8c%96") {
		t.Errorf("中文标签没有编码进链接：%s", tagHrefSnippet(out))
	}
	for _, want := range []string{"分类：", "标签："} {
		if !strings.Contains(out, want) {
			t.Errorf("文章页缺了 %q 这一行的标注", want)
		}
	}
}

// 没挂任何词条的文章不该出现空的词条区：留一个空 footer 会让版式莫名多出一段间距。
func TestArticleWithoutTermsHasNoTaxonomyBlock(t *testing.T) {
	v := testView()
	v.Post = &v.Posts[0]
	var b strings.Builder
	if err := pageTmpl["post"].ExecuteTemplate(&b, "layout", v); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(b.String(), `class="article-terms"`) {
		t.Errorf("无分类无标签的文章渲染出了空词条区")
	}
}

// tagHrefSnippet 截出文章页词条区那一段，编码不对时能直接看到渲染出来的 href。
func tagHrefSnippet(out string) string {
	i := strings.Index(out, `class="article-terms"`)
	if i < 0 {
		return "（没有词条区）"
	}
	end := i + 700
	if end > len(out) {
		end = len(out)
	}
	return out[i:end]
}

func canonicalSnippet(out string) string {	i := strings.Index(out, `rel="canonical"`)
	if i < 0 {
		return "（没有 canonical）"
	}
	end := i + 160
	if end > len(out) {
		end = len(out)
	}
	return out[i:end]
}

func pagerSnippet(out string) string {	i := strings.Index(out, `class="pager"`)
	if i < 0 {
		return "（没有 pager）"
	}
	end := i + 600
	if end > len(out) {
		end = len(out)
	}
	return out[i:end]
}
