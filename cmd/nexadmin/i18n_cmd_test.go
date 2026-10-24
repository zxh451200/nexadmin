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
