package website

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func navSection(html, start string) string {
	_, rest, ok := strings.Cut(html, start)
	if !ok {
		return ""
	}
	section, _, _ := strings.Cut(rest, "</nav>")
	return section
}

func TestLayoutBuiltinDocsDoesNotDuplicateConfiguredNav(t *testing.T) {
	tpl, ok := themePackFor("")["docs-index"]
	if !ok {
		t.Fatal("docs-index 模板缺失")
	}
	cases := []struct {
		name      string
		navs      []Nav
		docs      bool
		wantLinks int
	}{
		{name: "未配置文档导航", docs: true, wantLinks: 1},
		{name: "顶级文档导航", navs: []Nav{{Label: "文档", Href: "/docs"}}, docs: true, wantLinks: 1},
		{name: "带语言前缀与查询串", navs: []Nav{{Label: "文档", Href: "/en/docs?from=nav#top"}}, docs: true, wantLinks: 1},
		{name: "子级文档导航", navs: []Nav{{Label: "资源", Href: "/news", Children: []Nav{{Label: "文档", Href: "/docs"}}}}, docs: true, wantLinks: 1},
		{name: "文档详情子页不是重复入口", navs: []Nav{{Label: "功能", Href: "/docs/features"}}, docs: true, wantLinks: 1},
		{name: "文档区不可用", docs: false, wantLinks: 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			v := &siteView{
				SiteName: "NexAdmin", Lang: languageOr("zh-CN"), Languages: Languages(),
				Navs: tc.navs, DocsEnabled: tc.docs, Route: "/docs",
			}
			var b strings.Builder
			if err := tpl.ExecuteTemplate(&b, "layout", v); err != nil {
				t.Fatalf("渲染失败: %v", err)
			}
			html := b.String()
			for _, section := range []string{navSection(html, `<nav class="topnav"`), navSection(html, `<div class="mobile-nav"`)} {
				if section == "" {
					t.Fatal("导航区渲染缺失")
				}
				if got := strings.Count(section, `>文档</a>`); got != tc.wantLinks {
					t.Fatalf("文档链接数量 = %d，期望 %d", got, tc.wantLinks)
				}
			}
		})
	}
}

func TestThemeLayoutMobileNavigation(t *testing.T) {
	for _, theme := range []struct {
		id     string
		dir    string
		prefix string
	}{
		{id: "auvex", dir: "website-theme-auvex", prefix: "au"},
		{id: "solaris", dir: "website-theme-solaris", prefix: "sl"},
		{id: "torkon", dir: "website-theme-torkon", prefix: "tk"},
	} {
		if !themeRegistered(theme.id) {
			if err := RegisterTheme(theme.id, theme.id, "test", os.DirFS(filepath.Join("..", theme.dir)), nil); err != nil {
				t.Fatalf("注册主题 %s 失败: %v", theme.id, err)
			}
		}
		t.Run(theme.id, func(t *testing.T) {
			tpl, ok := themePackFor(theme.id)["home"]
			if !ok {
				t.Fatal("home 模板缺失")
			}
			v := &siteView{
				SiteName: "NexAdmin", Lang: languageOr("zh-CN"), Languages: Languages(),
				Navs:        []Nav{{Label: "文档", Href: "/docs"}, {Label: "新闻动态", Href: "/news"}},
				DocsEnabled: true, Route: "/docs",
			}
			var b strings.Builder
			if err := tpl.ExecuteTemplate(&b, "layout", v); err != nil {
				t.Fatalf("渲染失败: %v", err)
			}
			html := b.String()
			start := `<div class="` + theme.prefix + `-mnav"`
			_, section, ok := strings.Cut(html, start)
			if !ok {
				t.Fatal("移动导航区渲染缺失")
			}
			section, _, _ = strings.Cut(section, "</details>")
			if got := strings.Count(section, `>文档</a>`); got != 1 {
				t.Fatalf("文档链接数量 = %d，期望 1", got)
			}
			langClass := theme.prefix + "-mnav-lang"
			if !strings.Contains(section, `<div class="`+langClass+`">`) ||
				!strings.Contains(section, `<span class="`+langClass+`-current" aria-current="true">简体中文</span>`) ||
				!strings.Contains(section, `href="/en/docs" hreflang="en" lang="en">English</a>`) {
				t.Fatal("移动端语言区没有区分当前语言与可切换语言")
			}
			if strings.Contains(section, `>简体中文</a>`) {
				t.Fatal("当前语言仍渲染为可点击链接")
			}
		})
	}
}
