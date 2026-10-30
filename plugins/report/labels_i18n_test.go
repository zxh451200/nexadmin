package report

// 报表的数据表名与列名上屏走前端字典（plugin_report.source.<表> / plugin_report.field.<表>.<列>）。
// 少一条，英文界面就退回元数据里的中文兜底；中文界面则可能和元数据各说各话。
// 白名单放开新表/新列时必须同批补双语词条——这条测试把它变成硬门禁。

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func localeNamespace(t *testing.T, lang, ns string) map[string]any {
	t.Helper()
	path := filepath.Join("..", "..", "web", "src", "plugins", "report", "locales", lang+".json")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("读语言包 %s 失败（门禁依赖它，不能跳过）: %v", path, err)
	}
	var all map[string]any
	if err := json.Unmarshal(raw, &all); err != nil {
		t.Fatalf("解析 %s: %v", lang, err)
	}
	return childMap(all[ns])
}

func childMap(v any) map[string]any {
	m, ok := v.(map[string]any)
	if !ok {
		return map[string]any{}
	}
	return m
}

func labelOf(t *testing.T, dict map[string]any, path ...string) string {
	t.Helper()
	cur := dict
	for i, seg := range path {
		if i == len(path)-1 {
			s, _ := cur[seg].(string)
			return s
		}
		cur = childMap(cur[seg])
		if cur == nil {
			return ""
		}
	}
	return ""
}

func TestWhitelistLabelsTranslated(t *testing.T) {
	sources := map[string]map[string]any{}
	fields := map[string]map[string]any{}
	for _, lang := range []string{"zh-CN", "en-US"} {
		ns := localeNamespace(t, lang, "plugin_report")
		sources[lang] = childMap(ns["source"])
		fields[lang] = childMap(ns["field"])
	}
	checked := 0
	for _, rs := range defaultRaw() {
		for _, lang := range []string{"zh-CN", "en-US"} {
			src := labelOf(t, sources[lang], rs.Table)
			if src == "" {
				t.Errorf("%s：数据表 %s 没有译名，界面会退回中文兜底 %q", lang, rs.Table, rs.Label)
			}
			for _, c := range rs.Columns {
				label := labelOf(t, fields[lang], rs.Table, c.Name)
				if label == "" {
					t.Errorf("%s：列 %s.%s 没有译名，界面会退回中文兜底 %q", lang, rs.Table, c.Name, c.Label)
				}
				// 中文词条必须与元数据兜底一致，否则「字典里的中文」和「白名单里的中文」会分叉
				if lang == "zh-CN" && label != "" && label != c.Label {
					t.Errorf("zh-CN：列 %s.%s 词条 %q 与元数据兜底 %q 不一致", rs.Table, c.Name, label, c.Label)
				}
				checked++
			}
		}
	}
	if checked < 40 {
		t.Fatalf("只清点了 %d 个列标签，白名单解析可能出了问题", checked)
	}
	t.Logf("清点 %d 个列标签（双语各一次）", checked)
}
