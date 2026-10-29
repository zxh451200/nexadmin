package formbuilder

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"nexadmin/core/config"
	corei18n "nexadmin/core/i18n"
	"nexadmin/pkg/excel"
)

// 导出表头里框架固定的前四列必须走全局词条目录：
// 旧实现是 map[string][4]string，字面上只有 zh-CN 与 en-US 两个键，
// 第三种语言一律掉进兜底拿简体中文，加一门语言就得改 Go 代码。

func exportHeadRow(t *testing.T, lang string) []string {
	t.Helper()
	s, _, _ := newSvc()
	id := publish(t, s, ctxT1, simpleDef("head-form", fa))
	var buf bytes.Buffer
	if _, err := s.WriteExport(ctxT1, &buf, SubQuery{FormID: id}, 10, lang); err != nil {
		t.Fatal(err)
	}
	rows, err := excel.ReadAll(bytes.NewReader(buf.Bytes()), int64(buf.Len()), excel.ReadOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) == 0 {
		t.Fatal("导出没有表头行")
	}
	if rows[0][len(exportFixedCols)] != "A" {
		t.Errorf("字段列被改动: %v", rows[0])
	}
	return rows[0][:len(exportFixedCols)]
}

func TestExportHeadPerLanguage(t *testing.T) {
	cases := []struct {
		lang string
		want []string
	}{
		{"zh-CN", []string{"提交ID", "提交人", "提交时间", "版本"}},
		{"en-US", []string{"Submission ID", "Submitter", "Submitted at", "Version"}},
		// 没有词条的语言退回默认语言（和其它后端文案一致），不是英文、也不是裸 key。
		{"ja-JP", []string{"提交ID", "提交人", "提交时间", "版本"}},
	}
	for _, c := range cases {
		got := exportHeadRow(t, c.lang)
		for i, w := range c.want {
			if got[i] != w {
				t.Errorf("lang %s: 第 %d 列 = %q, want %q", c.lang, i, got[i], w)
			}
		}
	}
}

// TestExportHeadNewLanguageNeedsNoGoChange 是 n 元的正靶：
// 一门新语言只要往词条目录里补四个键，表头就跟着变，Go 代码一行不动。
func TestExportHeadNewLanguageNeedsNoGoChange(t *testing.T) {
	dir := t.TempDir()
	content := "formbuilder:\n  head_id: 提出ID\n  head_submitter: 提出者\n  head_time: 提出日時\n  head_version: バージョン\n"
	if err := os.WriteFile(filepath.Join(dir, "ja-JP.yaml"), []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	langs := []config.Language{
		{Code: "zh-CN", Default: true},
		{Code: "en-US"},
		{Code: "ja-JP"},
	}
	b, err := corei18n.New(langs, "zh-CN", dir)
	if err != nil {
		t.Fatal(err)
	}
	prev := corei18n.Default()
	corei18n.SetDefault(b)
	t.Cleanup(func() { corei18n.SetDefault(prev) })

	want := []string{"提出ID", "提出者", "提出日時", "バージョン"}
	got := exportHeadRow(t, "ja-JP")
	for i, w := range want {
		if got[i] != w {
			t.Errorf("第 %d 列 = %q, want %q（词条目录没生效？）", i, got[i], w)
		}
	}
	// 同一次装载里英文仍须正常，证明新语言不是靠把其它语言挤掉实现的。
	if en := exportHeadRow(t, "en-US"); en[0] != "Submission ID" {
		t.Errorf("en-US 表头被日语词条影响: %v", en)
	}
}
