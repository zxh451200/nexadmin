package menu

// 出厂菜单的标题存的是 i18n 键（侧边栏与菜单管理都用 t(title) 渲染）。
// 少一条词条，界面就直接显示 sysMenu.btn.edit 这种原键——新增菜单/按钮时必须同批补双语。
// 词典按运行时口径合并：web/src/**/locales/<lang>.json，框架打底、插件覆盖、其余最后。

import (
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

func localeLookup(dict map[string]any, key string) string {
	cur := any(dict)
	for _, seg := range strings.Split(key, ".") {
		m, ok := cur.(map[string]any)
		if !ok {
			return ""
		}
		cur = m[seg]
	}
	s, _ := cur.(string)
	return s
}

func mergeDict(dst, src map[string]any) {
	for k, v := range src {
		if sm, ok := v.(map[string]any); ok {
			if dm, ok := dst[k].(map[string]any); ok {
				mergeDict(dm, sm)
				continue
			}
		}
		dst[k] = v
	}
}

// webDict 按前端 init.ts 的合并顺序读一份词典（框架 → 插件 → 用户）。
func webDict(t *testing.T, lang string) map[string]any {
	t.Helper()
	srcRoot := filepath.Join("..", "..", "..", "web", "src")
	var files []string
	err := filepath.WalkDir(srcRoot, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() && filepath.Base(p) == lang+".json" && filepath.Base(filepath.Dir(p)) == "locales" {
			files = append(files, p)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("遍历 %s 失败（门禁依赖它，不能跳过）: %v", srcRoot, err)
	}
	rank := func(p string) int {
		switch {
		case strings.Contains(p, string(filepath.Separator)+"framework"+string(filepath.Separator)):
			return 0
		case strings.Contains(p, string(filepath.Separator)+"plugins"+string(filepath.Separator)):
			return 1
		default:
			return 2
		}
	}
	sort.Slice(files, func(i, j int) bool {
		if rank(files[i]) != rank(files[j]) {
			return rank(files[i]) < rank(files[j])
		}
		return files[i] < files[j]
	})
	dict := map[string]any{}
	for _, f := range files {
		raw, err := os.ReadFile(f)
		if err != nil {
			t.Fatalf("读 %s: %v", f, err)
		}
		var one map[string]any
		if err := json.Unmarshal(raw, &one); err != nil {
			t.Fatalf("解析 %s: %v", f, err)
		}
		mergeDict(dict, one)
	}
	if len(files) == 0 {
		t.Fatalf("%s 下没有找到 %s.json", srcRoot, lang)
	}
	return dict
}

func TestSeedTitlesTranslated(t *testing.T) {
	for _, lang := range []string{"zh-CN", "en-US"} {
		dict := webDict(t, lang)
		checked := 0
		for _, it := range buildSeed() {
			if it.m.Title == "" {
				continue
			}
			checked++
			if localeLookup(dict, it.m.Title) == "" {
				t.Errorf("%s：菜单标题 %q 没有文案（界面会显示原键）", lang, it.m.Title)
			}
		}
		if checked < 50 {
			t.Fatalf("%s：只清点到 %d 个标题，出厂菜单解析可能出了问题", lang, checked)
		}
	}
}
