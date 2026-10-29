package website

import (
	"testing"

	"nexadmin/core"
)

// 校验规则已上提到 core（sys_language 是唯一清单），这里只测官网自己的那部分：
// 字典回退与前缀解析。规则本身的用例见 core/languages_test.go。

// 新加的语言没有内置字典时，界面文案回退默认语言，但仍要能注册成一种语言。
func TestApplyLanguagesFallsBackToDefaultDict(t *testing.T) {
	original := Languages()
	defer func() {
		if err := applyLanguages(original); err != nil {
			t.Fatalf("恢复原始语言失败: %v", err)
		}
	}()

	extra := append(append([]Language{}, original...), Language{Code: "ja-JP", Label: "日本語", HTMLLang: "ja", Prefix: "ja", DateFormat: "2006-01-02"})
	if err := applyLanguages(extra); err != nil {
		t.Fatalf("回退字典的语言不应被拒: %v", err)
	}
	if got, want := dicts["ja-JP"]["home.quote.body"], dicts[DefaultLanguage().Code]["home.quote.body"]; got != want || got == "" {
		t.Fatalf("缺字典的语言应回退默认语言文案，got=%q want=%q", got, want)
	}
	if l, ok := LanguageByCode("ja"); !ok || l.Code != "ja-JP" {
		t.Fatalf("新语言的路径前缀应可被解析: %+v %v", l, ok)
	}
}

// 没有默认语言的清单必须被拒：否则默认语言字典无从取起，公开站会静默用零值语言渲染。
func TestApplyLanguagesRequiresDefault(t *testing.T) {
	if err := applyLanguages([]Language{{Code: "en-US", Label: "English", HTMLLang: "en", Prefix: "en"}}); err == nil {
		t.Error("缺默认语言的清单应当报错")
	}
}

// 平台清单要原样映射成官网视角：Label 来自 Name，漏一个字段就会在公开站上显示成空值。
func TestFromPlatformMapsEveryField(t *testing.T) {
	got := fromPlatform([]core.Language{{
		Code: "ja-JP", Name: "日本語", Short: "JA", Dir: "ltr",
		HTMLLang: "ja", OGLocale: "ja_JP", Prefix: "ja", DateFormat: "2006年1月2日", Default: true,
	}})
	if len(got) != 1 {
		t.Fatalf("条数 = %d, want 1", len(got))
	}
	l := got[0]
	if l.Label != "日本語" || l.Code != "ja-JP" || l.Short != "JA" || l.HTMLLang != "ja" ||
		l.OGLocale != "ja_JP" || l.Prefix != "ja" || l.DateFormat != "2006年1月2日" || !l.Default {
		t.Errorf("字段映射不完整: %+v", l)
	}
}

// 空清单不能把官网语言清空——热挂载或装配顺序异常时应当维持现状而不是白屏。
func TestLoadLanguagesIgnoresEmptyList(t *testing.T) {
	before := Languages()
	if len(before) == 0 {
		t.Skip("测试环境未加载任何语言，无法断言「维持现状」")
	}
	if err := LoadLanguages(nil); err != nil {
		t.Fatalf("空清单不该报错: %v", err)
	}
	if len(Languages()) != len(before) {
		t.Errorf("空清单把语言表清空了：%d → %d", len(before), len(Languages()))
	}
}
