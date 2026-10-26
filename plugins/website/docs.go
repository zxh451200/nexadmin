package website

import (
	"bytes"
	"fmt"
	"html"
	"html/template"
	"net/http"
	"regexp"
	"strings"
	"sync"

	"github.com/gin-gonic/gin"
	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/extension"
	highlighting "github.com/yuin/goldmark-highlighting/v2"
	"github.com/yuin/goldmark/parser"

	"nexadmin/docs"
)

// 官网 /docs 文档区：渲染框架仓库 docs/*.md（经 nexadmin/docs 包内嵌进二进制）。
// 文档是框架区内容，与官网插件解耦——这里只负责目录、渲染与路由。

// DocTocItem 文档页“本页目录”条目（从渲染后的 h2/h3 锚点提取）。
type DocTocItem struct {
	Level int
	Text  string
	Href  string
}

// DocView 当前渲染的文档页。
type DocView struct {
	Slug    string
	Title   string
	Content template.HTML
	TOC     []DocTocItem
}

// docItem 目录清单里的一篇文档；slug 用于 URL（ASCII），file 指向 nexadmin/docs 内嵌文件名。
// FileEn/TitleEn 是非默认语言（当前 en-US）的译文文件与侧边栏标题；缺省回退中文。
type docItem struct {
	Slug    string
	File    string
	Title   string
	FileEn  string
	TitleEn string
}

// docGroup 目录清单分组。
type docGroup struct {
	Key   string
	Items []docItem
}

// DocItem 侧边栏一篇文档（URL 已带语言前缀，Active 标记当前页）。
type DocItem struct {
	Slug   string
	Title  string
	URL    string
	Active bool
}

// DocGroup 侧边栏分组。
type DocGroup struct {
	Key   string
	Title string
	Items []DocItem
}

// docGroups 文档目录：顺序即侧边栏顺序。新增文档 = docs/ 放 md + 这里加一行。
var docGroups = []docGroup{
	{Key: "dev", Items: []docItem{
		{Slug: "dev-guide", File: "二次开发指南.md", Title: "二次开发指南",
			FileEn: "en/dev-guide.md", TitleEn: "Secondary Development Guide"},
		{Slug: "plugin-dev", File: "plugin-development.md", Title: "插件开发指南",
			FileEn: "en/plugin-dev.md", TitleEn: "Plugin Development Guide"},
	}},
	{Key: "ref", Items: []docItem{
		{Slug: "features", File: "功能与权限清单.md", Title: "功能与权限清单",
			FileEn: "en/features.md", TitleEn: "Features & Permissions"},
		{Slug: "config", File: "配置参考.md", Title: "配置参考",
			FileEn: "en/config.md", TitleEn: "Configuration Reference"},
		{Slug: "deploy", File: "部署与运维指南.md", Title: "部署与运维指南",
			FileEn: "en/deploy.md", TitleEn: "Deployment & Operations"},
	}},
}

// docFileFor 按语言取译文文件与标题：非默认语言且有译文用译文，否则回退中文。
func docFileFor(it docItem, lang Language) (string, string) {
	if !lang.Default && it.FileEn != "" {
		return it.FileEn, it.TitleEn
	}
	return it.File, it.Title
}

func findDoc(slug string) (docItem, bool) {
	for _, g := range docGroups {
		for _, it := range g.Items {
			if it.Slug == slug {
				return it, true
			}
		}
	}
	return docItem{}, false
}

// firstDoc 文档区入口默认打开的一篇。
func firstDoc() docItem { return docGroups[0].Items[0] }

// GFM 扩展：表格/删除线/任务列表/自动链接（文档里大量使用表格）。
// WithAutoHeadingID：为标题生成锚点 id，供“本页目录”跳转。
// 代码高亮用 github-dark 内联样式：正文代码块两套主题下都固定深底（配色由 chroma 内联输出，无需额外 CSS）。
var mdRenderer = goldmark.New(
	goldmark.WithParserOptions(parser.WithAutoHeadingID()),
	goldmark.WithExtensions(
		extension.GFM,
		highlighting.NewHighlighting(highlighting.WithStyle("github-dark")),
	),
)

// tocRe 从渲染 HTML 提取 h2/h3 锚点（goldmark 自动生成 heading id）。
var tocRe = regexp.MustCompile(`<h([23]) id="([^"]+)"[^>]*>(.*?)</h[23]>`)
var htmlTagRe = regexp.MustCompile(`<[^>]+>`)

func extractTOC(s string) []DocTocItem {
	out := []DocTocItem{}
	for _, m := range tocRe.FindAllStringSubmatch(s, -1) {
		lvl := 0
		_, _ = fmt.Sscanf(m[1], "%d", &lvl)
		// 先反转义再去标签：标题原文交回模板后再转义一次，避免 & 显示成 &amp;
		text := html.UnescapeString(htmlTagRe.ReplaceAllString(m[3], ""))
		out = append(out, DocTocItem{Level: lvl, Text: text, Href: "#" + m[2]})
	}
	return out
}

// docHTMLCache 缓存渲染结果：文档内容编译期固定，渲染一次终身使用。
var docHTMLCache sync.Map // file -> template.HTML

func renderDocHTML(file string) (template.HTML, error) {
	if v, ok := docHTMLCache.Load(file); ok {
		return v.(template.HTML), nil
	}
	b, err := docs.Files.ReadFile(file)
	if err != nil {
		return "", fmt.Errorf("docs: %w", err)
	}
	var buf bytes.Buffer
	if err := mdRenderer.Convert(b, &buf); err != nil {
		return "", fmt.Errorf("docs markdown: %w", err)
	}
	// 正文标题交给页面模板渲染（docs.tmpl 用 manifest 的标题 + 站点样式），去掉 md 里自己的 h1。
	s := buf.String()
	if i := strings.Index(s, "<h1"); i >= 0 {
		if j := strings.Index(s[i:], "</h1>"); j >= 0 {
			s = s[:i] + s[i+j+len("</h1>"):]
		}
	}
	out := template.HTML(s)
	docHTMLCache.Store(file, out)
	return out, nil
}

// docNav 组装侧边栏：组名走 i18n，链接带语言前缀，标记当前页；非默认语言用译文标题。
func (v *siteView) docNav(current string) []DocGroup {
	out := make([]DocGroup, 0, len(docGroups))
	for _, g := range docGroups {
		items := make([]DocItem, 0, len(g.Items))
		for _, it := range g.Items {
			_, title := docFileFor(it, v.Lang)
			items = append(items, DocItem{
				Slug:   it.Slug,
				Title:  title,
				URL:    v.URL("/docs/" + it.Slug),
				Active: it.Slug == current,
			})
		}
		out = append(out, DocGroup{Key: g.Key, Title: v.T("docs.group." + g.Key), Items: items})
	}
	return out
}

// GET|HEAD /docs —— 重定向到第一篇文档。
func (h *siteHandler) docsIndex(c *gin.Context) {
	lang := langOf(c)
	c.Redirect(http.StatusFound, lang.Path("/docs/"+firstDoc().Slug))
}

// GET|HEAD /docs/:slug —— 渲染一篇框架文档。
func (h *siteHandler) docPage(c *gin.Context) {
	item, ok := findDoc(c.Param("slug"))
	if !ok {
		v, err := h.baseView(c.Request.Context(), langOf(c).Code, 3)
		if err != nil {
			c.String(http.StatusInternalServerError, "site unavailable")
			return
		}
		h.notFound(c, v)
		return
	}
	lang := langOf(c)
	file, title := docFileFor(item, lang)
	content, err := renderDocHTML(file)
	if err != nil {
		c.String(http.StatusInternalServerError, "docs unavailable")
		return
	}
	v, err := h.baseView(c.Request.Context(), lang.Code, 3)
	if err != nil {
		c.String(http.StatusInternalServerError, "site unavailable")
		return
	}
	v.DocsEnabled = true
	v.DocNav = v.docNav(item.Slug)
	v.Doc = &DocView{Slug: item.Slug, Title: title, Content: content, TOC: extractTOC(string(content))}
	v.Title = title + " - " + v.SiteName
	v.Desc = v.T("docs.desc")
	h.render(c, "docs", http.StatusOK, v)
}
