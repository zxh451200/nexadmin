package website

import (
	"strings"
	"testing"
)

// 标签是「名字定身份、slug 定 URL」的两段式：slug 派生与名字归一是纯函数，
// 这里钉死口径；找或建、让号、关联替换这些落库语义由集成测试覆盖。

func TestNormalizeTagName(t *testing.T) {
	cases := []struct{ in, want string }{
		{"  性能优化  ", "性能优化"},
		{"多   个\n空格", "多 个 空格"},
		{"\t  ", ""},
		{"Release   Notes", "Release Notes"},
	}
	for _, c := range cases {
		if got := normalizeTagName(c.in); got != c.want {
			t.Errorf("normalizeTagName(%q) = %q, want %q", c.in, got, c.want)
		}
	}
	// 列宽是 VARCHAR(64) 按字符计：超长必须按字符截，不能按字节切出半个汉字。
	long := strings.Repeat("标", 100)
	if got := normalizeTagName(long); len([]rune(got)) != tagMaxRunes {
		t.Errorf("限长后应为 %d 个字符，实得 %d", tagMaxRunes, len([]rune(got)))
	}
}

func TestTagSlugOf(t *testing.T) {
	cases := []struct{ in, want string }{
		// 拉丁名：压成小写连字符，首尾不留连字符。
		{"Release Notes", "release-notes"},
		{"--Foo--Bar--", "foo-bar"},
		{"v2.0 GA", "v2-0-ga"},
		{"C++ & Go", "c-go"},
		// 中文与混排压不出拉丁名：直接用名字，链接里由浏览器百分号编码。
		{"性能优化", "性能优化"},
		// 混排名字里只要有拉丁可派生就用它：身份仍是名字，slug 只要能用且唯一。
		{"产品动态 2026", "2026"},
		{"发布 · 第二版", "发布 · 第二版"},
	}
	for _, c := range cases {
		if got := tagSlugOf(c.in); got != c.want {
			t.Errorf("tagSlugOf(%q) = %q, want %q", c.in, got, c.want)
		}
	}
	// slug 不能长过列宽，否则让号加后缀时先撞列宽再撞唯一索引。
	if got := tagSlugOf(strings.Repeat("a", 200)); len(got) > tagMaxRunes {
		t.Errorf("slug 长度 %d 超过列宽 %d", len(got), tagMaxRunes)
	}
}

// asciiSlug 的符号只压成一个连字符：连续标点变成一串连字符会让 slug 又长又难读。
func TestAsciiSlugCollapsesRuns(t *testing.T) {
	for _, in := range []string{"a!!!b", "a   b", "a-_-.b"} {
		got := asciiSlug(in)
		if strings.Contains(got, "--") {
			t.Errorf("asciiSlug(%q) = %q，出现连续连字符", in, got)
		}
	}
}

func TestSortTagsByUsage(t *testing.T) {
	rows := []Tag{
		{Name: "beta", PostCount: 3},
		{Name: "alpha", PostCount: 3},
		{Name: "big", PostCount: 9},
		{Name: "small", PostCount: 1},
	}
	sortTagsByUsage(rows)
	want := []string{"big", "alpha", "beta", "small"}
	for i, w := range want {
		if rows[i].Name != w {
			t.Fatalf("排序结果 %v，期望第 %d 个是 %q", []string{rows[0].Name, rows[1].Name, rows[2].Name, rows[3].Name}, i+1, w)
		}
	}
}

// 归档页高亮只认 slug 相同的那一项：active 传空串时不该有任何一项被点亮。
func TestCategoryLinksActive(t *testing.T) {
	rows := []Category{{Title: "产品动态", Slug: "product"}, {Title: "插件发布", Slug: "plugins"}}
	links := CategoryLinks(rows, "plugins")
	if len(links) != 2 {
		t.Fatalf("应下发 2 个栏目，实得 %d", len(links))
	}
	if links[0].Active || !links[1].Active {
		t.Errorf("高亮落点不对：%v / %v", links[0].Active, links[1].Active)
	}
	for _, l := range CategoryLinks(rows, "") {
		if l.Active {
			t.Errorf("未筛选时不应有高亮：%+v", l)
		}
	}
}
