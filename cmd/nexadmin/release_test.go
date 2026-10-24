package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func relMsgs(root string) string {
	var b strings.Builder
	for _, it := range releaseChecks(root) {
		b.WriteString("[" + it.level + "] " + it.msg + "\n")
	}
	return b.String()
}

func TestReleaseManifestPathsExist(t *testing.T) {
	r := t.TempDir()
	wfile(t, r, "core/a.go", "x")
	wfile(t, r, "web/tsconfig.app.json", "{}")
	wfile(t, r, "go.mod", "module x\n")
	wfile(t, r, ".nexadmin.manifest.json", `{"framework_version":"1.0.0","framework_paths":["core/","web/tsconfig.*.json","web/vite.*.ts","docs/","scritps/"],"merge_paths":["go.mod","go.sum"],"user_paths":["internal/"]}`)
	got := relMsgs(r)
	for _, want := range []string{`"docs/"`, `"scritps/"`, `"go.sum"`} {
		if !strings.Contains(got, "[FAIL]") || !strings.Contains(got, want) {
			t.Errorf("应报路径不存在 %s:\n%s", want, got)
		}
	}
	if !strings.Contains(got, `[WARN] .nexadmin.manifest.json 清单里的通配路径 "web/vite.*.ts"`) {
		t.Errorf("无匹配的通配项应只 WARN:\n%s", got)
	}
	if strings.Contains(got, `[FAIL] .nexadmin.manifest.json 清单里的路径 "web/vite.*.ts"`) {
		t.Errorf("通配项不应 FAIL:\n%s", got)
	}
	for _, ok := range []string{`"core/"`, `"web/tsconfig.*.json"`, `"go.mod"`} {
		if strings.Contains(got, ok) {
			t.Errorf("存在的路径不应报 %s:\n%s", ok, got)
		}
	}
}

func TestReleaseLocalReplace(t *testing.T) {
	for name, tc := range map[string]struct {
		mod  string
		want int
	}{
		"single":  {"module x\nreplace a/b => ../b\n", 1},
		"block":   {"module x\nreplace (\n\ta/b => ../b // dev\n\tc/d => ./vendor/d\n\te/f => g/h v1.0.0\n)\n", 2},
		"version": {"module x\nreplace a/b v1.0.0 => a/b v1.0.1\n", 0},
		"comment": {"module x\n// replace a/b => ../b\n", 0},
		"abs":     {"module x\nreplace a/b => /home/me/b\n", 1},
		"none":    {"module x\nrequire a/b v1.0.0\n", 0},
	} {
		p := filepath.Join(t.TempDir(), "go.mod")
		if err := os.WriteFile(p, []byte(tc.mod), 0o644); err != nil {
			t.Fatal(err)
		}
		if got := localReplaces(p); len(got) != tc.want {
			t.Errorf("%s: got %v, want %d", name, got, tc.want)
		}
	}
	r := t.TempDir()
	wfile(t, r, "go.mod", "module x\nreplace a/b => ../b\n")
	if got := relMsgs(r); !strings.Contains(got, "[FAIL] go.mod 里有本地路径 replace") {
		t.Errorf("release check 应 FAIL:\n%s", got)
	}
}

func TestReleaseLeftoversAndBuildOutputs(t *testing.T) {
	r := t.TempDir()
	wfile(t, r, "core/health.go.bak", "x")
	wfile(t, r, "internal/a.orig", "x")
	wfile(t, r, "web/node_modules/p/x.bak", "x") // 依赖目录不算
	wfile(t, r, ".nexadmin-trash/t/y.bak", "x")  // 垃圾桶不算
	wfile(t, r, "web/dist/index.html", "x")
	wfile(t, r, "bin/server", "x")
	got := relMsgs(r)
	if !strings.Contains(got, "[FAIL] 存在残留的 .bak/.orig 文件（共 2 个") {
		t.Errorf("应只统计 2 个残留:\n%s", got)
	}
	for _, d := range []string{"web/dist/", "bin/"} {
		if !strings.Contains(got, "[WARN] "+d+" 存在") {
			t.Errorf("应提示 %s:\n%s", d, got)
		}
	}
	clean := t.TempDir()
	wfile(t, clean, "core/a.go", "x")
	if g := relMsgs(clean); strings.Contains(g, "残留") || strings.Contains(g, "本地路径") || strings.Contains(g, "bin/") {
		t.Errorf("干净目录不应报新增项:\n%s", g)
	}
}

// 清单里的 config/license-keys.pub、config/license-revoked.json 由发行方发版时才放入：
// 仓库里不存在时 missingManifestPaths / release check 不报错；其他缺失路径照旧报错；文件存在时同样不报。
func TestReleaseOptionalManifestPathsMayBeMissing(t *testing.T) {
	r := t.TempDir()
	wfile(t, r, "core/a.go", "x")
	man := &upManifest{FrameworkPaths: []string{"core/", "config/license-keys.pub", "config/license-revoked.json", "config/typo.json", "web/vite.*.ts"}}
	miss, missGlob := missingManifestPaths(r, man)
	if strings.Join(miss, ",") != "config/typo.json" {
		t.Errorf("只有非可选的缺失路径应报错，got %v", miss)
	}
	if strings.Join(missGlob, ",") != "web/vite.*.ts" {
		t.Errorf("通配项行为不变，got %v", missGlob)
	}
	for _, p := range []string{"config/license-keys.pub", "config/license-revoked.json"} {
		if !optionalManifestPaths[p] {
			t.Errorf("%s 应在 optionalManifestPaths 里", p)
		}
	}

	wfile(t, r, "go.mod", "module x\n")
	wfile(t, r, ".nexadmin.manifest.json", `{"framework_version":"1.0.0","framework_paths":["core/","config/license-keys.pub","config/license-revoked.json","scritps/"],"merge_paths":["go.mod"],"user_paths":["internal/"]}`)
	got := relMsgs(r)
	if !strings.Contains(got, `[FAIL] .nexadmin.manifest.json 清单里的路径 "scritps/"`) {
		t.Errorf("其他缺失路径仍应 FAIL:\n%s", got)
	}
	if strings.Contains(got, "license-keys.pub") || strings.Contains(got, "license-revoked.json\" 在仓库里不存在") {
		t.Errorf("可选路径缺失不应报错:\n%s", got)
	}
	// 文件存在时同样没有问题
	wfile(t, r, "config/license-keys.pub", "k\n")
	wfile(t, r, "config/license-revoked.json", "{}")
	if miss, _ := missingManifestPaths(r, man); strings.Join(miss, ",") != "config/typo.json" {
		t.Errorf("存在时也不应报错，got %v", miss)
	}
}
