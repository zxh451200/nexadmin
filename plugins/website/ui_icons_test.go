package website

import (
	"io/fs"
	"regexp"
	"testing"
)

// 模板里 uiIcon "xxx" 的键名打错不会编译报错，只会静默画成默认圆点，
// 所以这里把模板全文扫一遍：引用的键必须在 uiIconPaths 里，表里也不许留闲置键。
func TestUIIconPathsMatchTemplates(t *testing.T) {
	names, err := fs.Glob(tplFS, "tpl/*.tmpl")
	if err != nil {
		t.Fatalf("glob templates: %v", err)
	}
	re := regexp.MustCompile(`uiIcon "([a-z0-9-]+)"`)
	used := map[string]bool{}
	for _, name := range names {
		b, err := tplFS.ReadFile(name)
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		for _, m := range re.FindAllStringSubmatch(string(b), -1) {
			used[m[1]] = true
			if _, ok := uiIconPaths[m[1]]; !ok {
				t.Errorf("%s uses uiIcon %q but uiIconPaths does not have it", name, m[1])
			}
		}
	}
	if len(used) == 0 {
		t.Fatal("no uiIcon reference found in templates")
	}
	for key := range uiIconPaths {
		if !used[key] {
			t.Errorf("uiIconPaths has %q but no template uses it", key)
		}
	}
}
