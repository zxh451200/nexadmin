package main

import (
	"bytes"
	"encoding/json"
	"os"
	"strings"
	"testing"
)

const defaultCfg = "name: NexAdmin\nlanguages:\n  - { code: zh-CN, name: 简体中文, dir: ltr, default: true }\n  - { code: en-US, name: English, dir: ltr }\nauth:\n  methods: [password]\n"

func TestAddLanguageLine(t *testing.T) {
	// 没有 config.yaml：以默认语言为底整块写出，不丢默认语言
	got, ch, err := addLanguageLine("", defaultCfg, "fr-FR", "Français", "ltr")
	if err != nil || !ch {
		t.Fatal(err, ch)
	}
	want := "languages:\n  - { code: zh-CN, name: 简体中文, dir: ltr, default: true }\n  - { code: en-US, name: English, dir: ltr }\n  - { code: fr-FR, name: Français, dir: ltr }\n"
	if got != want {
		t.Errorf("got:\n%s\nwant:\n%s", got, want)
	}
	// 幂等
	again, ch, _ := addLanguageLine(got, defaultCfg, "fr-FR", "Français", "ltr")
	if ch || again != got {
		t.Error("同一语言重复添加应无变化")
	}
	// 已有 languages 块：只加一行，保留其它配置
	cfg := "server:\n  port: 9000\nlanguages:\n  - { code: zh-CN, name: 简体中文, dir: ltr, default: true }\n\n# 其它\nauth:\n  x: 1\n"
	got, ch, _ = addLanguageLine(cfg, defaultCfg, "ar-SA", "العربية", "rtl")
	if !ch || !strings.Contains(got, "dir: ltr, default: true }\n  - { code: ar-SA, name: العربية, dir: rtl }\n\n# 其它\nauth:") || !strings.HasPrefix(got, "server:\n  port: 9000\n") {
		t.Errorf("应在块末追加并保留其它内容:\n%s", got)
	}
	// 名称里有特殊字符要加引号
	got, _, _ = addLanguageLine("", defaultCfg, "xx-YY", "A, B", "ltr")
	if !strings.Contains(got, `name: "A, B"`) {
		t.Errorf("特殊字符应加引号:\n%s", got)
	}
	if _, _, err := addLanguageLine("", "name: x\n", "fr-FR", "F", "ltr"); err == nil {
		t.Error("default.yaml 没有 languages 应报错")
	}
}

func TestMarkUntranslated(t *testing.T) {
	out, err := markUntranslated([]byte("{\n  \"post\": {\"a\": \"A\"}\n}\n"))
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(out, &m); err != nil || m[untranslatedKey] != true || m["post"] == nil {
		t.Errorf("标记后应仍是合法 JSON 且保留原内容: %v\n%s", err, out)
	}
	if !strings.HasPrefix(string(out), "{\n  \""+untranslatedKey) {
		t.Errorf("标记应在最前:\n%s", out)
	}
	out, err = markUntranslated([]byte("{}"))
	if err != nil || json.Unmarshal(out, &m) != nil || m[untranslatedKey] != true {
		t.Errorf("空对象也要能标记: %v %s", err, out)
	}
	if _, err := markUntranslated([]byte("not json")); err == nil {
		t.Error("非法 JSON 应报错")
	}
}

func makeI18nProject(t *testing.T) string {
	t.Helper()
	r := makeProject(t)
	wfile(t, r, "config/default.yaml", defaultCfg)
	wfile(t, r, "core/i18n/locales/en-US.yaml", "error:\n  internal: Internal error\n  not_found: Not found\n")
	wfile(t, r, "core/i18n/locales/zh-CN.yaml", "error:\n  internal: 内部错误\n  not_found: 未找到\n")
	wfile(t, r, "web/src/framework/locales/en-US.json", `{"common": {"save": "Save", "cancel": "Cancel"}}`)
	wfile(t, r, "web/src/framework/locales/zh-CN.json", `{"common": {"save": "保存", "cancel": "取消"}}`)
	wfile(t, r, "web/src/framework/modules/post/locales/en-US.json", `{"post": {"menu": "Posts", "title": {"x": "X"}}}`)
	wfile(t, r, "web/src/framework/modules/post/locales/zh-CN.json", `{"post": {"menu": "岗位"}}`)
	wfile(t, r, "web/src/pages/article/locales/en-US.json", `{"article": {"menu": "Article"}}`)
	wfile(t, r, "web/src/pages/article/locales/zh-CN.json", `{"article": {"menu": "文章"}}`)
	wfile(t, r, "web/node_modules/pkg/locales/en-US.json", `{"junk": "x"}`)
	return r
}

func TestI18nAddAndCheck(t *testing.T) {
	root := makeI18nProject(t)
	var out bytes.Buffer
	if err := cmdI18nAdd(root, []string{"fr-FR"}, &out); err != nil {
		t.Fatal(err)
	}
	cfg := readStr(t, root, "config/config.yaml")
	if !strings.Contains(cfg, "code: fr-FR, name: Français, dir: ltr") || !strings.Contains(cfg, "code: zh-CN") {
		t.Errorf("config.yaml 语言配置:\n%s", cfg)
	}
	// 后端：以 en-US 为底并标注
	be := readStr(t, root, "config/locales/fr-FR.yaml")
	if !strings.Contains(be, untranslatedKey) || !strings.Contains(be, "internal: Internal error") {
		t.Errorf("后端空壳:\n%s", be)
	}
	// 前端：框架文案合并进用户区 web/src/locales，页面级各自一份；框架区与 node_modules 不被写入
	var top map[string]any
	if err := json.Unmarshal([]byte(readStr(t, root, "web/src/locales/fr-FR.json")), &top); err != nil {
		t.Fatal(err)
	}
	if top[untranslatedKey] != true || top["common"] == nil || top["post"] == nil || top["article"] != nil {
		t.Errorf("web/src/locales/fr-FR.json 内容不对: %v", top)
	}
	if !strings.Contains(readStr(t, root, "web/src/pages/article/locales/fr-FR.json"), `"menu": "Article"`) {
		t.Error("页面级翻译应复制 en-US 内容")
	}
	for _, f := range []string{"web/src/framework/locales/fr-FR.json", "web/src/framework/modules/post/locales/fr-FR.json", "web/node_modules/pkg/locales/fr-FR.json"} {
		if has(root, f) {
			t.Errorf("不应写入 %s", f)
		}
	}
	// 再次执行：幂等，不覆盖已有翻译
	wfile(t, root, "web/src/pages/article/locales/fr-FR.json", `{"article": {"menu": "Article FR"}}`)
	out.Reset()
	if err := cmdI18nAdd(root, []string{"fr-FR"}, &out); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "已存在") || !strings.Contains(readStr(t, root, "web/src/pages/article/locales/fr-FR.json"), "Article FR") ||
		strings.Count(readStr(t, root, "config/config.yaml"), "fr-FR") != 1 {
		t.Errorf("重复执行应幂等:\n%s", out.String())
	}
	// 非法 locale / dir
	if err := cmdI18nAdd(root, []string{"fr_FR"}, &out); err == nil {
		t.Error("非法 locale 应报错")
	}
	if err := cmdI18nAdd(root, []string{"he-IL", "--dir", "up"}, &out); err == nil {
		t.Error("非法 --dir 应报错")
	}
	if err := cmdI18nAdd(root, []string{"--name", "עברית", "he-IL"}, &out); err != nil || !strings.Contains(readStr(t, root, "config/config.yaml"), "code: he-IL, name: עברית, dir: rtl") {
		t.Errorf("he-IL 应默认 rtl: %v", err)
	}
}

func TestI18nCheckMissingKeys(t *testing.T) {
	root := makeI18nProject(t)
	var out bytes.Buffer
	// 缺失 key 只提示，返回 nil（退出码 0）
	if err := cmdI18nCheck(root, nil, &out); err != nil {
		t.Fatal(err)
	}
	s := out.String()
	for _, want := range []string{"zh-CN: 共 4 个 key，缺失 1 个", "  - post.title.x", "en-US: 共 5 个 key，缺失 0 个"} {
		if !strings.Contains(s, want) {
			t.Errorf("前端报告缺少 %q:\n%s", want, s)
		}
	}
	if strings.Contains(s, "junk") {
		t.Error("node_modules 不应参与检查")
	}
	if !strings.Contains(s, "后端") || strings.Contains(s, "internal.") {
		t.Errorf("后端两种语言 key 一致，不应有缺失:\n%s", s)
	}
	// 后端缺一个 key + 新增语言带未翻译标记
	wfile(t, root, "core/i18n/locales/zh-CN.yaml", "error:\n  internal: 内部错误\n")
	if err := cmdI18nAdd(root, []string{"ja-JP"}, &bytes.Buffer{}); err != nil {
		t.Fatal(err)
	}
	out.Reset()
	if err := cmdI18nCheck(root, []string{"--all"}, &out); err != nil {
		t.Fatal(err)
	}
	s = out.String()
	if !strings.Contains(s, "  - error.not_found") || !strings.Contains(s, "ja-JP") || !strings.Contains(s, "仍标注未翻译") {
		t.Errorf("应列出后端缺失 key 与未翻译标注:\n%s", s)
	}
	// 坏文件只提示，不让命令失败
	wfile(t, root, "web/src/pages/bad/locales/en-US.json", "{oops")
	out.Reset()
	if err := cmdI18nCheck(root, nil, &out); err != nil || !strings.Contains(out.String(), "无法解析") {
		t.Errorf("坏 JSON 应提示并继续: %v\n%s", err, out.String())
	}
}

func TestI18nCheckViaRun(t *testing.T) {
	root := makeI18nProject(t)
	cwd, _ := os.Getwd()
	if err := os.Chdir(root); err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(cwd) //nolint:errcheck
	var out bytes.Buffer
	if err := run([]string{"i18n", "check"}, &out); err != nil {
		t.Fatalf("i18n check 缺 key 也应退出码 0: %v", err)
	}
	if err := run([]string{"i18n"}, &out); err == nil {
		t.Error("缺少子命令应报用法")
	}
}

// 旧版按各语言 key 的并集判缺失，会把「中文没有 *_one」报成缺翻译——
// 中文的 CLDR 复数规则只有 other，补上 _one 是永远不会被取用的死条目。
// 现在复数族按语言各自的 CLDR 类别要求：有基准 key 就算覆盖（i18next 会回退到它），
// 整族一条都没有才报基准 key。
func TestMissingKeysFoldsCldrPluralCategories(t *testing.T) {
	s := newLocaleSet()
	s.add("en-US", []string{"tenantSelf.daysLeft_one", "tenantSelf.daysLeft_other", "report.name"})
	s.add("zh-CN", []string{"tenantSelf.daysLeft", "report.name"})
	s.add("fr-FR", []string{"report.name"})

	miss := missingKeys(s)
	if len(miss["zh-CN"]) != 0 {
		t.Errorf("zh-CN 只有基准 key，不该被判缺复数变体：%v", miss["zh-CN"])
	}
	if len(miss["en-US"]) != 0 {
		t.Errorf("en-US 只有复数变体，不该被判缺基准 key：%v", miss["en-US"])
	}
	// 真缺的仍然要报出来（折族不能把「这一族一条都没有」也折没了）
	if len(miss["fr-FR"]) != 1 || miss["fr-FR"][0] != "tenantSelf.daysLeft" {
		t.Errorf("fr-FR 整族缺失应报基准 key，实际：%v", miss["fr-FR"])
	}
}

// 复数类别知识放在「按语言查 CLDR 类别表」里，不是「只有英文有单数」：
// 俄语/乌克兰语/波兰语有 one+few+many+other，中日韩泰越只有 other，
// 表外的语言也只要求 other（漏报一个 *_one 比逼人选不出来的形式好）。
func TestCldrSuffixesFor(t *testing.T) {
	cases := map[string]string{
		"en-US": "_one _other", "pt-BR": "_one _other", "de-DE": "_one _other",
		"zh-CN": "_other", "zh-TW": "_other", "ja-JP": "_other", "ko-KR": "_other", "th-TH": "_other",
		"ru-RU": "_one _few _many _other", "uk-UA": "_one _few _many _other", "pl-PL": "_one _few _many _other",
		"cs-CZ": "_one _few _other", "ar-SA": "_zero _one _two _few _many _other",
		"xx-YY": "_other", // 表外的语言：只要求 other
	}
	for code, want := range cases {
		if got := strings.Join(cldrSuffixesFor(code), " "); got != want {
			t.Errorf("cldrSuffixesFor(%s)=%q want %q", code, got, want)
		}
	}
}

// 走一遍真实的文件扫描路径，覆盖三种形状：
//  1. en 有 *_one/*_other、zh 只有基准 key —— 必须放过（i18next 用基准 key 兜底）。
//  2. 某个 key 一门语言整族都没有 —— 必须报。
//  3. zh 写了 *_one 却没写 *_other 也没有基准 key —— 必须报 *_other（这是真缺，渲染会落空）。
func TestI18nCheckPluralCategories(t *testing.T) {
	root := t.TempDir()
	wfile(t, root, "web/src/framework/modules/quota/locales/en-US.json", `{
  "quota": {
    "days": "{{count}} days",
    "days_one": "{{count}} day",
    "days_other": "{{count}} days",
    "calls": "{{count}} calls",
    "hours": "{{count}} hours",
    "hours_one": "{{count}} hour",
    "hours_other": "{{count}} hours"
  }
}`)
	wfile(t, root, "web/src/framework/modules/quota/locales/zh-CN.json", `{
  "quota": {
    "days": "{{count}} 天",
    "hours_one": "{{count}} 小时"
  }
}`)
	var out bytes.Buffer
	if err := cmdI18nCheck(root, []string{"--all"}, &out); err != nil {
		t.Fatal(err)
	}
	s := out.String()
	// 形状 2 + 形状 3 各一条，形状 1 一条都不许多报。
	if !strings.Contains(s, "zh-CN: 共 2 个 key，缺失 2 个") {
		t.Errorf("zh-CN 应只缺 quota.calls 与 quota.hours_other:\n%s", s)
	}
	for _, want := range []string{"  - quota.calls", "  - quota.hours_other"} {
		if !strings.Contains(s, want) {
			t.Errorf("前端报告缺少 %q:\n%s", want, s)
		}
	}
	if strings.Contains(s, "  - quota.days") || strings.Contains(s, "  - quota.calls_other") {
		t.Errorf("基准 key 已覆盖的族不该再报，也不该报出折错的 key:\n%s", s)
	}
	if !strings.Contains(s, "en-US: 共 7 个 key，缺失 0 个") {
		t.Errorf("en-US 不应有缺失:\n%s", s)
	}

	// 同一形状直接喂 missingKeys：zh/ja 只要 other，ru 还要 few/many。
	loc := newLocaleSet()
	loc.add("en-US", []string{"stat.items_one", "stat.items_other"})
	loc.add("zh-CN", []string{"stat.items_one"})
	loc.add("ja-JP", []string{"stat.items"})
	loc.add("ru-RU", []string{"stat.items_one"})
	miss := missingKeys(loc)
	if g := strings.Join(miss["zh-CN"], ","); g != "stat.items_other" {
		t.Errorf("zh-CN 缺 *_other 应报基准族里的 _other，实际：%q", g)
	}
	if len(miss["ja-JP"]) != 0 {
		t.Errorf("ja-JP 有基准 key 不该被判缺失：%v", miss["ja-JP"])
	}
	if g := strings.Join(miss["ru-RU"], ","); g != "stat.items_few,stat.items_many,stat.items_other" {
		t.Errorf("ru-RU 应有 few/many/other 三条缺失（不是只有 other），实际：%q", g)
	}
	// 后端 .yaml 没有复数机制：validation.validation.files_too_many/_few 这种叶子名
	// 恰好以 _many/_few 结尾，不能当成复数族去补 *_one/*_other（真仓库里就有这对 key）。
	wfile(t, root, "core/i18n/locales/en-US.yaml", "validation:\n  files_too_many: At most %v files\n  files_too_few: At least %v files\n")
	wfile(t, root, "core/i18n/locales/zh-CN.yaml", "validation:\n  files_too_many: 最多 %v 个文件\n  files_too_few: 至少 %v 个文件\n")
	out.Reset()
	if err := cmdI18nCheck(root, []string{"--all"}, &out); err != nil {
		t.Fatal(err)
	}
	s = out.String()
	if strings.Contains(s, "files_too_one") || strings.Contains(s, "files_too_other") {
		t.Errorf("后端不该按复数族报假缺:\n%s", s)
	}
	// 后端真缺一个 key 仍要报（字面比对）。
	wfile(t, root, "core/i18n/locales/zh-CN.yaml", "validation:\n  files_too_many: 最多 %v 个文件\n")
	out.Reset()
	if err := cmdI18nCheck(root, []string{"--all"}, &out); err != nil {
		t.Fatal(err)
	}
	if s = out.String(); !strings.Contains(s, "  - validation.files_too_few") {
		t.Errorf("后端缺 key 应报出:\n%s", s)
	}

	// 真复数族（族里见过两种以上形式）里 en 少了 *_one 一样要报：
	// 规则不是「只有别的语言才有单数」，而是「这门语言的 CLDR 有没有这个类别」。
	loc2 := newLocaleSet()
	loc2.add("zh-CN", []string{"stat.items_one", "stat.items_other"})
	loc2.add("en-US", []string{"stat.items_other", "stat.items_few"})
	if g := strings.Join(missingKeys(loc2)["en-US"], ","); g != "stat.items_one" {
		t.Errorf("en-US 缺 *_one 应报出，实际：%q", g)
	}
	if g := strings.Join(missingKeys(loc2)["zh-CN"], ""); g != "" {
		t.Errorf("zh-CN 有 *_other，多余那条 *_one 是死条目但不该判缺：%q", g)
	}
	// 只见过 *_other 的一族分不清是复数族还是叶子名恰好带后缀
	// （plugins 就有 category._other =「其他」分类标签），所以不臆造别的类别。
	loc3 := newLocaleSet()
	loc3.add("en-US", []string{"form.category._other", "report.name"})
	loc3.add("zh-CN", []string{"form.category._other", "report.name"})
	for code, m := range missingKeys(loc3) {
		if len(m) != 0 {
			t.Errorf("%s 不该有缺失：%v", code, m)
		}
	}
}
