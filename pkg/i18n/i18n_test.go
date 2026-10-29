package i18n

import (
	"encoding/json"
	"strings"
	"testing"
)

// fakeLocales 是清单的最小可信实现：zh-CN 默认，en-US 带 en 语基，ja-JP 未配置。
// 别名口径与官网 applyLanguages 一致：每门语言同时以完整码和语基可寻
// （所以 "en" 和 "en-GB" 都解析到 en-US）。
type fakeLocales struct{ list []Language }

func (f fakeLocales) All() []Language { return f.list }

func (f fakeLocales) Resolve(code string) (Language, bool) {
	want := strings.ToLower(strings.TrimSpace(code))
	for _, l := range f.list {
		if want == strings.ToLower(l.Code) || want == baseOf(l.Code) {
			return l, true
		}
	}
	if base, _, found := strings.Cut(want, "-"); found {
		for _, l := range f.list {
			if base == baseOf(l.Code) {
				return l, true
			}
		}
	}
	return Language{}, false
}

// baseOf 取语基：en-US → en-us。
func baseOf(code string) string {
	if b, _, found := strings.Cut(strings.ToLower(code), "-"); found {
		return b
	}
	return strings.ToLower(code)
}

func locales() fakeLocales {
	return fakeLocales{list: []Language{
		{Code: "zh-CN", Default: true},
		{Code: "en-US"},
	}}
}

var postFields = Fields{
	"title":   {Max: 20},
	"summary": {Max: 500},
	"content": {Rich: true},
}

func TestReviewMarkerSurvivesNormalizeButNotParseDoc(t *testing.T) {
	raw := `{"en-US":{"title":"T"},"_review":{"en-US":"mt"}}`
	out, err := Normalize(raw, locales(), postFields)
	if err != nil {
		t.Fatalf("Normalize 不应报错：%v", err)
	}
	if !strings.Contains(out, `"_review"`) || !strings.Contains(out, `"en-US":"mt"`) {
		t.Fatalf("机翻标记应原样保留，实得 %s", out)
	}

	// 公开渲染侧只按语言取译文：标记不能冒充一门语言。
	if _, ok := ParseDoc(out)["_review"]; ok {
		t.Fatalf("ParseDoc 应剔除保留键，实得 %v", ParseDoc(out))
	}
	if got := ParseDoc(out).For("en-US")["title"]; got != "T" {
		t.Fatalf("译文本身不能被标记连带丢掉，实得 %q", got)
	}
}

func TestReviewMarkerIsNarrowedLikeTranslations(t *testing.T) {
	// 未配置的语言 / 非法值 / 默认语言都要被丢弃；只有 "mt" 是合法值。
	raw := `{"en-US":{"title":"T"},"_review":{"fr-FR":"mt","zh-CN":"mt","en":"mt","en-GB":"done"}}`
	out, err := Normalize(raw, locales(), postFields)
	if err != nil {
		t.Fatalf("Normalize 不应报错：%v", err)
	}
	if strings.Contains(out, "fr-FR") || strings.Contains(out, "zh-CN") || strings.Contains(out, `"done"`) {
		t.Fatalf("非法的校对声明应被丢弃，实得 %s", out)
	}
	// "en" 是 en-US 的语基，和译文一样按清单码归一化。
	if !strings.Contains(out, `"_review":{"en-US":"mt"}`) {
		t.Fatalf("语基应归一化成清单码，实得 %s", out)
	}
}

func TestReviewMarkerAloneStillPersists(t *testing.T) {
	// 译文被清空但标记还在：不能因为「没有可留的译文」就把状态一起抹掉。
	out, err := Normalize(`{"_review":{"en-US":"mt"}}`, locales(), postFields)
	if err != nil {
		t.Fatalf("Normalize 不应报错：%v", err)
	}
	if out != `{"_review":{"en-US":"mt"}}` {
		t.Fatalf("只剩标记时也应落盘，实得 %s", out)
	}
}

func TestParseDoc(t *testing.T) {
	if got := ParseDoc(""); got != nil {
		t.Errorf("空串应为 nil，实得 %v", got)
	}
	if got := ParseDoc("{坏 JSON"); got != nil {
		t.Errorf("损坏 JSON 应为 nil，实得 %v", got)
	}
	got := ParseDoc(`{"en-US":{"title":"T"}}`)
	if got.For("en-US")["title"] != "T" {
		t.Errorf("解析结果不符: %v", got)
	}
}

// 文档里存的是语基（"en"）而站点语言是 en-US 时的兼容路径；没有这条，
// 手工写入或早期版本留下的数据会静默不生效。
func TestDocForFallsBackToBaseKey(t *testing.T) {
	d := ParseDoc(`{"en":{"title":"T"}}`)
	if got := d.For("en-US")["title"]; got != "T" {
		t.Errorf("en-US 未回退到存于 \"en\" 的译文，实得 %q", got)
	}
	if got := d.For("ja-JP"); got != nil {
		t.Errorf("无关语言不该命中: %v", got)
	}
}

func TestPick(t *testing.T) {
	tr := map[string]string{"title": "T", "summary": "", "blank": "   "}
	if got := Pick(tr, "title", "原文"); got != "T" {
		t.Errorf("有译文时应返回译文，实得 %q", got)
	}
	// 空串与缺失同义：填了又删干净必须退回原文，而不是渲染出一片空白。
	if got := Pick(tr, "summary", "原文"); got != "原文" {
		t.Errorf("空译文应回退原文，实得 %q", got)
	}
	if got := Pick(tr, "missing", "原文"); got != "原文" {
		t.Errorf("缺失字段应回退原文，实得 %q", got)
	}
	if got := Pick(nil, "title", "原文"); got != "原文" {
		t.Errorf("整份译文缺失应回退原文，实得 %q", got)
	}
}

func TestNormalize(t *testing.T) {
	raw := `{
		"en-US": {"title":"  English title that is way too long for the cap  ","content":"<p>hi</p><script>alert(1)</script>","unknown":"x"},
		"fr-FR": {"title":"bonjour"},
		"zh-CN": {"title":"不应覆盖默认语言"}
	}`
	out, err := Normalize(raw, locales(), postFields)
	if err != nil {
		t.Fatal(err)
	}
	var parsed map[string]map[string]string
	if err := json.Unmarshal([]byte(out), &parsed); err != nil {
		t.Fatalf("结果不是合法 JSON: %v (%s)", err, out)
	}
	en, ok := parsed["en-US"]
	if !ok {
		t.Fatalf("丢了 en-US: %s", out)
	}
	if strings.Contains(en["title"], "  ") || len([]rune(en["title"])) > 20 {
		t.Errorf("title 未裁剪: %q", en["title"])
	}
	if strings.Contains(en["content"], "<script") {
		t.Errorf("富文本未清洗: %q", en["content"])
	}
	for _, bad := range []string{"fr-FR", "unknown", "zh-CN", "不应覆盖默认语言"} {
		if strings.Contains(out, bad) {
			t.Errorf("结果里不该有 %q: %s", bad, out)
		}
	}
}

// 语言键要规范成清单里的码：前端传语基 "en" 也得存成 "en-US"，读写口径才一致。
func TestNormalizeCanonicalizesLanguageKey(t *testing.T) {
	out, err := Normalize(`{"en":{"title":"T"}}`, locales(), postFields)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, `"en-US"`) || strings.Contains(out, `"en":`) {
		t.Errorf("语基键未被规范成清单码: %s", out)
	}
}

func TestNormalizeEmptyAndBadInput(t *testing.T) {
	if out, err := Normalize("", locales(), postFields); err != nil || out != "" {
		t.Errorf("空值应原样返回空: %q %v", out, err)
	}
	if _, err := Normalize("{坏", locales(), postFields); err == nil {
		t.Error("非法 JSON 应当失败")
	}
	// 全是空译文时要收成空串，而不是留下 {} 或空对象壳。
	if out, err := Normalize(`{"en-US":{"title":"   "}}`, locales(), postFields); err != nil || out != "" {
		t.Errorf("纯空白译文应收成空串: %q %v", out, err)
	}
}

func TestTranslatable(t *testing.T) {
	fields := Fields{"title": {Max: 200}, "summary": {Max: 500}}
	doc := ParseDoc(`{"en-US":{"title":"T","summary":"S"}}`)
	base := map[string]string{"title": "标题", "summary": "简介"}
	if !Translatable("en-US", doc, base, fields) {
		t.Error("两个字段都翻完了，仍判为缺译")
	}
	if Translatable("en-US", ParseDoc(`{"en-US":{"title":"T"}}`), base, fields) {
		t.Error("漏了 summary 却判为可发布")
	}
	// 原文本身为空的字段不算缺译：各语言渲染一致，没有伪装问题。
	if !Translatable("en-US", ParseDoc(`{"en-US":{"title":"T"}}`), map[string]string{"title": "标题", "summary": "  "}, fields) {
		t.Error("原文为空的 summary 不应挡住发布")
	}
}

func TestNormalizePublish(t *testing.T) {
	full := ParseDoc(`{"en-US":{"title":"T","summary":"S","content":"<p>C</p>"}}`)
	base := map[string]string{"title": "标题", "summary": "简介", "content": "正文"}

	if out, err := NormalizePublish("", true, full, base, locales(), postFields); err != nil || out != "" {
		t.Errorf("空发布状态应原样返回空: %q %v", out, err)
	}
	if _, err := NormalizePublish("{坏", true, full, base, locales(), postFields); err == nil {
		t.Error("非法 JSON 应当失败")
	}

	// 这是本文件存在的理由：缺译语言的 true 必须被强制改成 false。
	out, err := NormalizePublish(`{"en-US":true}`, true, nil, base, locales(), postFields)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out, "true") {
		t.Errorf("一个字都没翻，却被判定可发布: %s", out)
	}

	// 客户端没点名的语言沿用整行 published。
	on, err := NormalizePublish(`{}`, true, full, base, locales(), postFields)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(on, `"en-US":true`) {
		t.Errorf("整行已发布且译文齐备，未点名的语言应继承 true: %s", on)
	}
	off, _ := NormalizePublish(`{}`, false, full, base, locales(), postFields)
	if strings.Contains(off, "true") {
		t.Errorf("整行是草稿时未点名的语言不该被点亮: %s", off)
	}

	// 默认语言与未配置语言都不落盘。
	both, _ := NormalizePublish(`{"zh-CN":true,"fr-FR":true,"en-US":true}`, true, full, base, locales(), postFields)
	if strings.Contains(both, "zh-CN") || strings.Contains(both, "fr-FR") {
		t.Errorf("默认语言/未配置语言不应出现在发布状态里: %s", both)
	}
}

// 后端不能反过来相信客户端的完成度声明。
func TestNormalizePublishIgnoresClientClaims(t *testing.T) {
	out, err := NormalizePublish(`{"en-US":true}`, true, ParseDoc(`{"en-US":{"title":"T"}}`),
		map[string]string{"title": "标题", "content": "<p>正文</p>"}, locales(), postFields)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out, "true") {
		t.Errorf("客户端的 true 被原样采信了: %s", out)
	}
}

func TestPublished(t *testing.T) {
	const doc = `{"en-US":true,"ja-JP":false}`
	cases := []struct {
		name string
		pub  bool
		raw  string
		code string
		want bool
	}{
		{"原文下线则整篇下线", false, doc, "zh-CN", false},
		{"原文下线时译文也不可见", false, doc, "en-US", false},
		// 默认语言从不进文档：不先按整行 published 判，非空文档会把原文自己判成未发布。
		{"默认语言沿用整行发布", true, doc, "zh-CN", true},
		{"从未按语言设置过就沿用整行", true, "", "ja-JP", true},
		{"点名为 true 的语言可见", true, doc, "en-US", true},
		{"点名为 false 的语言不可见", true, doc, "ja-JP", false},
		{"启用过按语言发布后未点名即不可见", true, doc, "de-DE", false},
		{"坏数据不该顺手打掉已上线页面", true, "{坏", "en-US", true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := Published(c.pub, c.raw, c.code, locales()); got != c.want {
				t.Errorf("Published(%v,%q,%q) = %v, want %v", c.pub, c.raw, c.code, got, c.want)
			}
		})
	}

	t.Run("按 base tag 回退", func(t *testing.T) {
		if !Published(true, `{"en":true}`, "en-GB", locales()) {
			t.Error("en 已发布时 en-GB 应当可见")
		}
	})
}
