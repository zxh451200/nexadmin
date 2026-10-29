package core

import (
	"strings"
	"testing"

	"nexadmin/core/config"
)

func normalizeOK(t *testing.T, list []Language) []Language {
	t.Helper()
	out, err := NormalizeLanguages(list)
	if err != nil {
		t.Fatalf("合法清单被拒: %v", err)
	}
	return out
}

// config.yaml 允许只写 code/name/default，其余按语标推导；推导错了会静默改变 URL 与 <html lang>。
func TestNormalizeLanguagesDerivesFields(t *testing.T) {
	out := normalizeOK(t, []Language{
		{Code: "zh-CN", Name: "简体中文", Default: true},
		{Code: "en-US", Name: "English"},
	})
	if len(out) != 2 {
		t.Fatalf("字段数 = %d, want 2", len(out))
	}
	zh, en := out[0], out[1]
	if zh.HTMLLang != "zh-CN" || zh.OGLocale != "zh_CN" || zh.DateFormat != "2006-01-02" || zh.Dir != "ltr" {
		t.Errorf("默认语言推导不符: %+v", zh)
	}
	if zh.Prefix != "" {
		t.Errorf("默认语言必须挂根路径，实得前缀 %q", zh.Prefix)
	}
	// 非默认语言没写 prefix 时按语基推导——这是「加一门语言不用改配置」的关键。
	if en.Prefix != "en" || en.HTMLLang != "en-US" || en.OGLocale != "en_US" {
		t.Errorf("en-US 推导不符: %+v", en)
	}
}

// 语言切换器显示 Short；没配就得兜底，否则渲染出一个没有文字的按钮。
func TestNormalizeLanguagesDerivesShort(t *testing.T) {
	out := normalizeOK(t, []Language{
		{Code: "zh-CN", Name: "简体中文", Default: true},
		{Code: "ja-JP"},
	})
	if out[0].Short != "ZH" {
		t.Errorf("zh-CN 的 short = %q, want ZH", out[0].Short)
	}
	if out[1].Short != "JA" {
		t.Errorf("ja-JP 的 short = %q, want JA", out[1].Short)
	}
	// 显式配置不能被推导覆盖
	filled := normalizeOK(t, []Language{{Code: "zh-CN", Name: "中文", Short: "中", Default: true}})
	if filled[0].Short != "中" {
		t.Errorf("显式 short 被覆盖成 %q", filled[0].Short)
	}
}

func TestNormalizeLanguagesRejects(t *testing.T) {
	bad := []struct {
		name string
		list []Language
		want string
	}{
		{"空清单", nil, "至少需要一种语言"},
		{"没有默认语言", []Language{{Code: "en-US", Prefix: "en"}}, "默认语言"},
		{"两个默认语言", []Language{
			{Code: "zh-CN", Default: true},
			{Code: "en-US", Default: true, Prefix: "en"},
		}, "默认语言只能有一个"},
		{"默认语言带前缀", []Language{
			{Code: "zh-CN", Default: true, Prefix: "zh"},
			{Code: "en-US", Prefix: "en"},
		}, "默认语言必须挂在根路径"},
		{"代码重复", []Language{
			{Code: "zh-CN", Default: true},
			{Code: "zh-CN", Prefix: "zc"},
		}, "语言代码重复"},
		{"前缀重复", []Language{
			{Code: "zh-CN", Default: true},
			{Code: "en-US", Prefix: "x"},
			{Code: "ja-JP", Prefix: "x"},
		}, "前缀重复"},
		{"代码非法", []Language{{Code: "zh CN", Default: true}}, "语言代码不合法"},
		{"前缀含非法字符", []Language{
			{Code: "zh-CN", Default: true},
			{Code: "en-US", Prefix: "en/uk"},
		}, "前缀非法"},
		{"dir 非法", []Language{
			{Code: "zh-CN", Default: true, Dir: "up"},
		}, "dir 只能是"},
	}
	for _, c := range bad {
		_, err := NormalizeLanguages(c.list)
		if err == nil {
			t.Errorf("%s：应当报错但通过了", c.name)
			continue
		}
		if !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s：报错文案 %q 未包含 %q", c.name, err.Error(), c.want)
		}
	}
}

// 同语基的两种变体（en-US / en-GB）都缺前缀时，第二个必须换名而不是撞车。
func TestNormalizeLanguagesDerivesDistinctPrefixes(t *testing.T) {
	out := normalizeOK(t, []Language{
		{Code: "zh-CN", Default: true},
		{Code: "en-US"},
		{Code: "en-GB"},
	})
	if out[1].Prefix != "en" {
		t.Errorf("en-US 前缀 = %q, want en", out[1].Prefix)
	}
	if out[2].Prefix != "en_gb" {
		t.Errorf("en-GB 前缀 = %q, want en_gb（撞车时要退回完整码）", out[2].Prefix)
	}
}

func TestNormalizeLanguagesDerivesRTL(t *testing.T) {
	out := normalizeOK(t, []Language{
		{Code: "zh-CN", Default: true},
		{Code: "ar-SA"},
	})
	if out[1].Dir != "rtl" {
		t.Errorf("ar-SA 的 dir = %q, want rtl", out[1].Dir)
	}
	if out[0].Dir != "ltr" {
		t.Errorf("zh-CN 的 dir = %q, want ltr", out[0].Dir)
	}
}

func TestLanguageLookup(t *testing.T) {
	list := []Language{
		{Code: "zh-CN", Default: true},
		{Code: "en-US", Prefix: "en"},
	}
	prev := loadedLangs.Load()
	loadedLangs.Store(&list)
	t.Cleanup(func() {
		if prev != nil {
			loadedLangs.Store(prev)
		} else {
			loadedLangs.Store(nil)
		}
	})

	if got := DefaultLanguage(); got.Code != "zh-CN" {
		t.Errorf("默认语言 = %q, want zh-CN", got.Code)
	}
	// 精确码
	if l, ok := LanguageByCode("en-US"); !ok || l.Code != "en-US" {
		t.Error("精确码未命中 en-US")
	}
	// 大小写不敏感
	if _, ok := LanguageByCode("EN-us"); !ok {
		t.Error("大小写不同的码应当命中")
	}
	// 未配置变体按语基回退：en-GB → en → en-US，与内容翻译取值口径一致
	if l, ok := LanguageByCode("en-GB"); !ok || l.Code != "en-US" {
		t.Errorf("en-GB 未回退到 en-US：%+v %v", l, ok)
	}
	if _, ok := LanguageByCode("ja-JP"); ok {
		t.Error("未配置的 ja-JP 不该命中")
	}
}

// 清单没加载时不能 panic，也不能凭空造出一个默认语言。
func TestLanguagesBeforeSync(t *testing.T) {
	prev := loadedLangs.Load()
	loadedLangs.Store(nil)
	t.Cleanup(func() { loadedLangs.Store(prev) })

	if got := Languages(); got != nil {
		t.Errorf("未同步时 Languages() = %v, want nil", got)
	}
	if got := DefaultLanguage(); got.Code != "" {
		t.Errorf("未同步时默认语言应为空码，实得 %q", got.Code)
	}
}

// SyncLanguageTable 在没有库时必须报错，而不是静默返回一份看起来可用的清单。
func TestSyncLanguageTableNeedsDB(t *testing.T) {
	if _, err := SyncLanguageTable(nil, &config.Config{}); err == nil {
		t.Error("db=nil 应当报错")
	}
}
