package main

import (
	"archive/zip"
	"bytes"
	"io/fs"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// ---- release pack ----

func zipNames(t *testing.T, zp string) []string {
	t.Helper()
	zr, err := zip.OpenReader(zp)
	if err != nil {
		t.Fatal(err)
	}
	defer zr.Close()
	var names []string
	for _, f := range zr.File {
		names = append(names, f.Name)
	}
	sort.Strings(names)
	return names
}

func treeFiles(t *testing.T, root string) []string {
	t.Helper()
	var out []string
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err == nil && !d.IsDir() {
			r, _ := filepath.Rel(root, p)
			out = append(out, filepath.ToSlash(r))
		}
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	sort.Strings(out)
	return out
}

func TestReleasePackExcludesLikeNew(t *testing.T) {
	tpl := makeTemplate(t)
	wfile(t, tpl, ".nexadmin.manifest.json", `{"framework_version":"0.2.0","framework_paths":["core/"],"user_paths":["internal/"]}`)
	zp := filepath.Join(t.TempDir(), "p.zip")
	var buf bytes.Buffer
	if err := releasePack(tpl, zp, true, &buf); err != nil { // makeTemplate 带 .dev-keys / config.yaml，必有 FAIL，需 --force
		t.Fatal(err, buf.String())
	}
	if !strings.Contains(buf.String(), "sha256 ") || !strings.Contains(buf.String(), "文件数") {
		t.Errorf("应打印文件数与 sha256:\n%s", buf.String())
	}
	// 与 copyTemplate（nexadmin new 用的拷贝）逐文件对比：排除集合必须一致。
	cp := filepath.Join(t.TempDir(), "cp")
	if _, err := copyTemplate(tpl, cp, false); err != nil {
		t.Fatal(err)
	}
	var want []string
	for _, f := range treeFiles(t, cp) {
		want = append(want, "nexadmin-0.2.0/"+f)
	}
	got := zipNames(t, zp)
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("pack 与 new 的文件集合不一致\npack:\n%s\nnew:\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
	for _, bad := range []string{".git", "node_modules", "web/dist", "bin/", "data/app.db", "data/licenses", "logs/s.log", "backup/",
		"config/config.yaml", ".dev-keys", ".nexadmin.files.json", ".nexadmin-trash", "scripts/derive_check", "scripts/contract_check", "scripts/gen_icons", ".github/workflows/derive.yml",
		"plugins/cron", "run.log"} {
		for _, n := range got {
			if strings.Contains("/"+n+"/", "/"+strings.TrimSuffix(bad, "/")+"/") || strings.HasSuffix(n, bad) {
				t.Errorf("包里不应有 %s（%s）", bad, n)
			}
		}
	}
	joined := strings.Join(got, "\n")
	for _, must := range []string{"nexadmin-0.2.0/.nexadmin.manifest.json", "nexadmin-0.2.0/plugins/plugins.go", "nexadmin-0.2.0/data/.gitkeep", "nexadmin-0.2.0/.github/workflows/ci.yml"} {
		if !strings.Contains(joined, must) {
			t.Errorf("包里应有 %s", must)
		}
	}
	// plugins.go 里已安装插件的导入被清掉，与 new 一致。
	zr, _ := zip.OpenReader(zp)
	defer zr.Close()
	for _, f := range zr.File {
		if f.Name == "nexadmin-0.2.0/plugins/plugins.go" {
			rc, _ := f.Open()
			var b bytes.Buffer
			_, _ = b.ReadFrom(rc)
			rc.Close()
			if strings.Contains(b.String(), "cron") || !strings.Contains(b.String(), "nexadmin:plugins:begin") {
				t.Errorf("plugins.go 应清空导入并保留标记:\n%s", b.String())
			}
		}
	}
}

func TestReleasePackRefusesOnFail(t *testing.T) {
	tpl := makeTemplate(t) // 含 .dev-keys/、config/config.yaml、已装插件 => FAIL
	zp := filepath.Join(t.TempDir(), "p.zip")
	var buf bytes.Buffer
	err := releasePack(tpl, zp, false, &buf)
	if err == nil || !strings.Contains(err.Error(), "FAIL") {
		t.Fatalf("有 FAIL 必须拒绝打包, got %v", err)
	}
	for _, want := range []string{"[FAIL] .dev-keys/", "[FAIL] config/config.yaml"} {
		if !strings.Contains(buf.String(), want) {
			t.Errorf("应列出 %q:\n%s", want, buf.String())
		}
	}
	if exists(zp) {
		t.Error("拒绝打包时不应产生 zip")
	}
	if err := releasePack(tpl, filepath.Join(t.TempDir(), "x.txt"), true, &buf); err == nil {
		t.Error("--out 不是 .zip 应报错")
	}
}

func TestReleasePackZipFeedsUpgrade(t *testing.T) {
	tpl := makeTemplate(t)
	wfile(t, tpl, ".nexadmin.manifest.json", `{"framework_version":"0.3.0","framework_paths":["core/"],"user_paths":["internal/"]}`)
	wfile(t, tpl, "core/a.go", "package core\n\n// v0.3.0\n")
	zp := filepath.Join(t.TempDir(), "nexadmin-0.3.0.zip")
	if err := releasePack(tpl, zp, true, &bytes.Buffer{}); err != nil {
		t.Fatal(err)
	}
	root := upNewProject(t)
	out, err := upRun(t, root, zp, false)
	if err != nil {
		t.Fatal(err, out)
	}
	if !strings.Contains(read(t, filepath.Join(root, "core", "a.go")), "v0.3.0") {
		t.Error("upgrade --from <pack zip> 没有应用 core/a.go")
	}
	if !strings.Contains(read(t, filepath.Join(root, frameworkManifest)), `"framework_version": "0.3.0"`) {
		t.Error("framework_version 未更新")
	}
}

// ---- upgrade 的 merge_paths ----

const upMergeManifest = `{"framework_version":"%s","framework_paths":["core/","web/tsconfig.*.json"],"merge_paths":["go.mod","go.sum","web/package.json"],` +
	`"user_paths":["internal/","web/package-lock.json"]}`

func upMergeProject(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	write(t, filepath.Join(root, frameworkManifest), strings.Replace(upMergeManifest, "%s", "1.0.0", 1))
	write(t, filepath.Join(root, "core", "a.go"), "old a\n")
	write(t, filepath.Join(root, "go.mod"), "module example.com/demo\n\ngo 1.25.0\n\nrequire (\n\tgithub.com/a/mine v1.0.0\n\tgithub.com/a/fw v1.0.0\n)\n")
	write(t, filepath.Join(root, "go.sum"), "github.com/a/mine v1.0.0 h1:m=\n")
	write(t, filepath.Join(root, "web", "package.json"), "{\n  \"name\": \"demo\",\n  \"dependencies\": {\n    \"mine\": \"1.0.0\"\n  }\n}\n")
	write(t, filepath.Join(root, "web", "package-lock.json"), "LOCK-MINE\n")
	return root
}

func upMergePkg(t *testing.T) string {
	t.Helper()
	return upPkg(t, strings.Replace(upMergeManifest, "%s", "1.1.0", 1), map[string]string{
		"core/a.go":               "package core\n// new a\n",
		"go.mod":                  "module nexadmin\n\ngo 1.25.0\n\nrequire (\n\tgithub.com/a/fw v1.1.0\n\tgithub.com/a/newdep v0.2.0\n)\n",
		"go.sum":                  "github.com/a/fw v1.1.0 h1:f=\n",
		"web/package.json":        "{\"name\":\"nexadmin-web\",\"dependencies\":{\"fw\":\"2.0.0\"}}",
		"web/package-lock.json":   "LOCK-FRAMEWORK\n",
		"web/tsconfig.app.json":   "{}\n",
		"web/tsconfig.node.json":  "{}\n",
		"web/tsconfig-other.json": "{}\n", // 不匹配 tsconfig.*.json
	})
}

func TestUpgradeMergePathsDryRunLists(t *testing.T) {
	root := upMergeProject(t)
	before := upSnapshot(t, root)
	out, err := upRun(t, root, upMergePkg(t), true)
	if err != nil {
		t.Fatal(err, out)
	}
	if upSnapshot(t, root) != before {
		t.Error("dry-run 改动了工程")
	}
	for _, want := range []string{"~  go.mod", "~  go.sum", "~  web/package.json", "+ require github.com/a/newdep v0.2.0",
		"↑ require github.com/a/fw v1.0.0 -> v1.1.0", "= 保留二开独有依赖 github.com/a/mine v1.0.0",
		"+ dependencies.fw: 2.0.0", "= 保留二开独有 dependencies: mine", "合并 3", "A  web/tsconfig.app.json", "A  web/tsconfig.node.json"} {
		if !strings.Contains(out, want) {
			t.Errorf("dry-run 清单缺少 %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "tsconfig-other.json") || strings.Contains(out, "package-lock.json") {
		t.Errorf("通配不应匹配 tsconfig-other.json、用户区锁文件不应出现:\n%s", out)
	}
}

func TestUpgradeMergePathsApply(t *testing.T) {
	root := upMergeProject(t)
	out, err := upRun(t, root, upMergePkg(t), false)
	if err != nil {
		t.Fatal(err, out)
	}
	gm := read(t, filepath.Join(root, "go.mod"))
	for _, want := range []string{"module example.com/demo", "github.com/a/mine v1.0.0", "github.com/a/fw v1.1.0", "github.com/a/newdep v0.2.0"} {
		if !strings.Contains(gm, want) {
			t.Errorf("go.mod 缺少 %q:\n%s", want, gm)
		}
	}
	if sum := read(t, filepath.Join(root, "go.sum")); !strings.Contains(sum, "a/mine") || !strings.Contains(sum, "a/fw") {
		t.Errorf("go.sum 应为并集:\n%s", sum)
	}
	pj := read(t, filepath.Join(root, "web", "package.json"))
	if !strings.Contains(pj, `"name": "demo"`) || !strings.Contains(pj, `"mine": "1.0.0"`) || !strings.Contains(pj, `"fw": "2.0.0"`) {
		t.Errorf("package.json 合并有误:\n%s", pj)
	}
	if read(t, filepath.Join(root, "web", "package-lock.json")) != "LOCK-MINE\n" {
		t.Error("package-lock.json 属用户区，不得被覆盖")
	}
	if !strings.Contains(out, "go mod tidy") || !strings.Contains(out, "npm install") {
		t.Errorf("升级后应提示 go mod tidy 与 npm install:\n%s", out)
	}
	if rec := read(t, filepath.Join(root, filesRecord)); strings.Contains(rec, "go.mod") || strings.Contains(rec, "package.json") {
		t.Errorf("合并文件不应进哈希记录:\n%s", rec)
	}
	// 再升一次同一个包：合并无变化 -> 不再列出、不写文件。
	before := read(t, filepath.Join(root, "go.mod"))
	out2, err := upRun(t, root, upMergePkg(t), true)
	if err != nil {
		t.Fatal(err, out2)
	}
	if strings.Contains(out2, "~  ") || read(t, filepath.Join(root, "go.mod")) != before {
		t.Errorf("无变化时不应再列合并项:\n%s", out2)
	}
}

func TestUpgradeMergeUnsupportedFile(t *testing.T) {
	root := upMergeProject(t)
	write(t, filepath.Join(root, "other.txt"), "x\n")
	pkg := upPkg(t, `{"framework_version":"1.1.0","framework_paths":["core/"],"merge_paths":["other.txt"],"user_paths":["internal/"]}`,
		map[string]string{"other.txt": "y\n"})
	if _, err := upRun(t, root, pkg, false); err == nil || !strings.Contains(err.Error(), "不支持合并") {
		t.Fatalf("不支持的合并类型必须报错, got %v", err)
	}
	if read(t, filepath.Join(root, "other.txt")) != "x\n" {
		t.Error("报错时不应改动文件")
	}
}

func TestUpMatchGlob(t *testing.T) {
	ps := []string{"web/tsconfig.*.json"}
	if !upMatch("web/tsconfig.app.json", ps) || upMatch("web/tsconfig.json", ps) || upMatch("web/sub/tsconfig.app.json", ps) {
		t.Error("通配应只匹配同目录下的 tsconfig.*.json")
	}
}

// release pack：license-keys.pub / license-revoked.json 在清单里；不存在时照常打包（且包里没有它们），存在时带进包里。
func TestReleasePackCarriesOptionalLicenseFiles(t *testing.T) {
	const manifest = `{"framework_version":"0.4.0","framework_paths":["core/","config/license-keys.pub","config/license-revoked.json"],"user_paths":["internal/"]}`
	pack := func(t *testing.T, tpl string) []string {
		t.Helper()
		zp := filepath.Join(t.TempDir(), "p.zip")
		var buf bytes.Buffer
		if err := releasePack(tpl, zp, true, &buf); err != nil { // makeTemplate 必有其他 FAIL，需 --force
			t.Fatal(err, buf.String())
		}
		if strings.Contains(buf.String(), "license-") {
			t.Errorf("可选路径不应出现在检查输出里:\n%s", buf.String())
		}
		return zipNames(t, zp)
	}
	t.Run("不存在", func(t *testing.T) {
		tpl := makeTemplate(t)
		wfile(t, tpl, ".nexadmin.manifest.json", manifest)
		for _, n := range pack(t, tpl) {
			if strings.Contains(n, "license-") {
				t.Errorf("不应凭空出现 %s", n)
			}
		}
	})
	t.Run("存在", func(t *testing.T) {
		tpl := makeTemplate(t)
		wfile(t, tpl, ".nexadmin.manifest.json", manifest)
		wfile(t, tpl, "config/license-keys.pub", "KEY\n")
		wfile(t, tpl, "config/license-revoked.json", "{\"seq\":1}")
		joined := strings.Join(pack(t, tpl), "\n")
		for _, must := range []string{"nexadmin-0.4.0/config/license-keys.pub", "nexadmin-0.4.0/config/license-revoked.json"} {
			if !strings.Contains(joined, must) {
				t.Errorf("包里应有 %s:\n%s", must, joined)
			}
		}
	})
}
