package config

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"strings"
	"testing"

	"nexadmin/core/response"
)

// 只读分区（storage / cache）的两侧一致性：
//   - 后端在碰数据库之前就拒绝写入，并给出 i18n key（不是硬编码中文）；
//   - 前端两套语言包里都有对应的只读说明，否则用户只会看到一个报错却不知道去哪儿改。
//
// 加一个只读分区时若漏了任一侧，这里会红。
func TestReadonlySectionsRejectedBeforeAnyDB(t *testing.T) {
	s := &Service{} // 只读分支不该走到查询：nil 句柄一旦触碰数据库就是 panic
	for name, wantKey := range map[string]string{
		"storage": "config.storage_readonly",
		"cache":   "config.cache_readonly",
	} {
		_, err := s.SaveSection(context.Background(), name, map[string]any{"type": "s3"})
		var re *response.Error
		if !errors.As(err, &re) {
			t.Fatalf("SaveSection(%q) 返回 %v，想要 *response.Error（保存被当成了成功？）", name, err)
		}
		if re.Status != http.StatusBadRequest || re.Key != wantKey {
			t.Errorf("SaveSection(%q) = %d %q, want 400 %q", name, re.Status, re.Key, wantKey)
		}
	}

	// 正控制：可写分区不能被这张表误伤（否则「只读」会悄悄吞掉真正的设置）。
	for _, name := range []string{"ai", "notify", "sms", "auth", "params", "language"} {
		if _, ok := readonlySection(name); ok {
			t.Errorf("%q 是可写分区，不该出现在 readonlySections 里", name)
		}
	}
}

// 拒绝写入用的 key 必须真的能翻译，且前端只读说明双语齐全。
func TestReadonlySectionHintsExistInBothFaces(t *testing.T) {
	langs := []string{"zh-CN", "en-US"}

	for _, lang := range langs {
		b, err := os.ReadFile("../../../core/i18n/locales/" + lang + ".yaml")
		if err != nil {
			t.Fatalf("后端语言包缺失：%v", err)
		}
		for _, key := range readonlySections {
			// key 形如 config.cache_readonly → yaml 里是 config: 下的 cache_readonly:
			if !yamlHasKey(string(b), key) {
				t.Errorf("%s.yaml 缺少后端错误文案 %q（前端会显示整串 key）", lang, key)
			}
		}
	}

	for _, lang := range langs {
		path := "../../../web/src/framework/modules/settings/locales/" + lang + ".json"
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("前端语言包缺失：%s（%v）", path, err)
		}
		var doc map[string]any
		if err := json.Unmarshal(raw, &doc); err != nil {
			t.Fatalf("%s 不是合法 JSON：%v", path, err)
		}
		settings, _ := doc["settings"].(map[string]any)
		for name := range readonlySections {
			section, _ := settings[name].(map[string]any)
			hint, _ := section["readonlyHint"].(string)
			if hint == "" {
				t.Errorf("%s 缺少 settings.%s.readonlyHint（后端已拒绝该分区写入，前端必须说明去哪儿改）", lang, name)
			}
		}
	}
}

// yamlHasKey 在两层 yaml 文本里找 `parent:` 段下缩进的 `child:` 键。
// 只回答「这个 key 有没有文案」——文案真能翻译出来由 core/i18n 的测试负责。
func yamlHasKey(doc, dottedKey string) bool {
	parts := strings.Split(dottedKey, ".")
	if len(parts) < 2 {
		return false
	}
	parent, child := parts[len(parts)-2]+":", parts[len(parts)-1]+":"
	inParent := false
	for _, line := range strings.Split(doc, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		if !strings.HasPrefix(line, " ") && !strings.HasPrefix(line, "\t") {
			inParent = strings.HasPrefix(trimmed, parent)
			continue
		}
		if inParent && strings.HasPrefix(trimmed, child) {
			return true
		}
	}
	return false
}
