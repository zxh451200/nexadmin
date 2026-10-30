package imports

import (
	"context"
	"strings"
	"testing"

	"nexadmin/core/config"
	"nexadmin/core/i18n"
)

// withBundle 装上真实的双语包，结束后还原（避免影响同包其它用例的兜底文案）。
func withBundle(t *testing.T) {
	t.Helper()
	prev := i18n.Default()
	b, err := i18n.New([]config.Language{{Code: "zh-CN"}, {Code: "en-US"}}, "zh-CN", "")
	if err != nil {
		t.Fatal(err)
	}
	i18n.SetDefault(b)
	t.Cleanup(func() { i18n.SetDefault(prev) })
}

func i18nSource() Source {
	return Source{
		Name:     "t-i18n",
		Title:    "客户导入",
		TitleKey: "imports.source.crm_customers.title",
		Columns: []Column{
			{Key: "name", Label: "客户名称", LabelKey: "imports.source.crm_customers.name", Required: true, Example: "张三"},
			{Key: "phone", Label: "手机号", LabelKey: "imports.source.crm_customers.phone"},
		},
		Handler: func(context.Context, int, map[string]string) error { return nil },
	}
}

// 来源列表按请求语言渲染标题与列名。
func TestListSourcesLocalized(t *testing.T) {
	withBundle(t)
	if err := RegisterSource(i18nSource()); err != nil {
		t.Fatal(err)
	}
	en := i18n.WithLang(context.Background(), "en-US")
	metas := ListSources(en)
	var got *SourceMeta
	for k := range metas {
		if metas[k].Name == "t-i18n" {
			got = &metas[k]
		}
	}
	if got == nil {
		t.Fatal("source t-i18n not listed")
	}
	if got.Title != "Customer import" || got.Columns[0].Label != "Customer name" {
		t.Fatalf("not localized: %+v", *got)
	}

	zh := ListSources(i18n.WithLang(context.Background(), "zh-CN"))
	for _, m := range zh {
		if m.Name == "t-i18n" && (m.Title != "客户导入" || m.Columns[0].Label != "客户名称") {
			t.Fatalf("zh fallback broken: %+v", m)
		}
	}
}

// 模板随语言本地化，且任一语言的模板都能被解析回去（切语言上传不丢列）。
func TestTemplateCrossLanguageRoundTrip(t *testing.T) {
	withBundle(t)
	src := i18nSource()

	en := BuildTemplateCSV(i18n.WithLang(context.Background(), "en-US"), src)
	if !strings.Contains(string(en), "Customer name(required)") {
		t.Fatalf("english header missing: %s", en)
	}
	rows, err := ParseUpload(src, en, "t.csv")
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0]["name"] != "张三" || rows[0]["phone"] != "" {
		t.Fatalf("english template not parsed: %+v", rows)
	}

	zh := BuildTemplateCSV(i18n.WithLang(context.Background(), "zh-CN"), src)
	if !strings.Contains(string(zh), "客户名称(必填)") {
		t.Fatalf("chinese header missing: %s", zh)
	}
	// 用中文模板、英文请求解析：表头匹配必须跨语言成立。
	rows, err = ParseUpload(src, zh, "t.csv")
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0]["name"] != "张三" {
		t.Fatalf("chinese template not parsed: %+v", rows)
	}
}
