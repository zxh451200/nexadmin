package plugins_test

import (
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// 站内信/告警是「上屏」文本：写死中文的文案让英文部署的收件人看不懂，
// 词典里缺的 key 又会被 core/i18n 原样回退成 key 名。两条都在这里钉住。

var cjkRe = regexp.MustCompile("[一-鿿]")

// notifyMsgRe 认出「这个文件在发站内信」：只有这类文件受文案规则约束。
var notifyMsgRe = regexp.MustCompile(`notify\.Msg\{`)

// literalSinkRe 抓 Title:/Content: 直接写字面量，或 title/content 变量被赋成字面量
// （以前的写法是 title := "审批待办：" + name 再塞进 Msg，肉眼扫容易漏）。
var literalSinkRe = regexp.MustCompile(`^[ \t]*(?:Title|Content|title|content)[ \t]*:?=?[ \t]*(?:fmt\.[Ss]?printf\()?"`)

// 站内信/告警是「上屏」文本：写死中文的文案让英文部署的收件人看不懂，
// 词典里缺的 key 又会被 core/i18n 原样回退成 key 名。
//
// 取景范围是 plugins/workflow、plugins/cron、plugins/ticket、plugins/website。
// 前两笔（f9ee14e）把硬中文的待发文本换成 i18n.T；ticket 与 website 原本是
// compose*(s.lang, …) 里 en/zh 两分支——两种语言都出得来，只是没过词典，
// 现已统一走 i18n.T，这条闸一并防它们回退。
// 不收 devicehub：那条泳道还在途（service.go 的「设备告警：」+ d.Name 仍是硬编码），
// 由 devicehub 的负责人收尾时自己收进取景目录；这里先不挡它。
func TestInAppMessageTextGoesThroughDict(t *testing.T) {
	root := editionContractRoot(t)
	var offenders []string
	for _, pkg := range []string{"workflow", "cron", "ticket", "website"} {
		dir := filepath.Join(root, "plugins", pkg)
		ents, err := os.ReadDir(dir)
		if err != nil {
			t.Fatal(err)
		}
		for _, e := range ents {
			name := e.Name()
			if e.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
				continue
			}
			raw, rerr := os.ReadFile(filepath.Join(dir, name))
			if rerr != nil {
				t.Fatal(rerr)
			}
			if !notifyMsgRe.Match(raw) {
				continue
			}
			rel := "plugins/" + pkg + "/" + name
			for i, ln := range strings.Split(string(raw), "\n") {
				if literalSinkRe.MatchString(ln) && cjkRe.MatchString(ln) {
					offenders = append(offenders, rel+":"+strconv.Itoa(i+1)+" "+strings.TrimSpace(ln))
				}
			}
		}
	}
	if len(offenders) > 0 {
		t.Errorf("站内信文本要过 i18n.T（词典在 core/i18n/locales），不能写死中文：\n%s", strings.Join(offenders, "\n"))
	}
}

// 双语模板都要在，且占位符序列要一致：类型或个数对不上，调用方按一份语言的
// 模板传参就会在另一种语言里露出 %!d(MISSING) 或 %!s(string=x)。
func TestInAppMessageKeysMatchAcrossLangs(t *testing.T) {
	root := editionContractRoot(t)
	zh := loadLocaleLeaf(t, filepath.Join(root, "core", "i18n", "locales", "zh-CN.yaml"))
	en := loadLocaleLeaf(t, filepath.Join(root, "core", "i18n", "locales", "en-US.yaml"))
	verbs := regexp.MustCompile(`%[sdv]|%%`)
	for _, key := range []string{
		"workflow.notify_todo_title", "workflow.notify_todo_content",
		"workflow.notify_urge_title", "workflow.notify_urge_content",
		"cron.alert_title", "cron.alert_content",
		"ticket.notify_assigned_title", "ticket.notify_bulk_assigned_title",
		"ticket.notify_reply_title", "ticket.notify_new_ticket_title",
		"ticket.notify_overdue_response_title", "ticket.notify_overdue_resolution_title",
		"website.notify_inquiry_title", "website.notify_content_empty",
	} {
		t.Run(key, func(t *testing.T) {
			z, zok := zh[key]
			e, eok := en[key]
			if !zok || !eok {
				t.Fatalf("词典缺失：zh=%v en=%v", zok, eok)
			}
			if !cjkRe.MatchString(z) {
				t.Errorf("中文文案不像是中文：%q", z)
			}
			if cjkRe.MatchString(e) {
				t.Errorf("英文文案里还有中文：%q", e)
			}
			zv := verbs.FindAllString(z, -1)
			ev := verbs.FindAllString(e, -1)
			if strings.Join(zv, ",") != strings.Join(ev, ",") {
				t.Errorf("占位符序列不一致：zh=%v en=%v", zv, ev)
			}
		})
	}
}

// loadLocaleLeaf 把两层 YAML（组.名）展平成点分 key → 文案。
func loadLocaleLeaf(t *testing.T, path string) map[string]string {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var tree map[string]any
	if err := yaml.Unmarshal(raw, &tree); err != nil {
		t.Fatalf("%s: %v", path, err)
	}
	out := map[string]string{}
	var walk func(prefix string, node map[string]any)
	walk = func(prefix string, node map[string]any) {
		for k, v := range node {
			join := k
			if prefix != "" {
				join = prefix + "." + k
			}
			switch child := v.(type) {
			case map[string]any:
				walk(join, child)
			case string:
				out[join] = child
			}
		}
	}
	walk("", tree)
	return out
}
