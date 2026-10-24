package main

import (
	"archive/zip"
	"go/format"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const goMod = "module nexadmin\n\ngo 1.25\n"

const pluginsGo = `package plugins

import (
// nexadmin:plugins:begin
// nexadmin:plugins:end
)
`

const manifestYAML = "id: demo-x\nname: Demo\nversion: 1.0.0\nedition: free\n"

func newRoot(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	write(t, filepath.Join(root, "go.mod"), goMod)
	write(t, filepath.Join(root, "plugins", "plugins.go"), pluginsGo)
	return root
}

func write(t *testing.T, p, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func read(t *testing.T, p string) string {
	t.Helper()
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func makePkg(t *testing.T, manifest string) string {
	t.Helper()
	dir := t.TempDir()
	write(t, filepath.Join(dir, "plugin.yaml"), manifest)
	write(t, filepath.Join(dir, "backend", "plugin.go"), "package demo\n")
	write(t, filepath.Join(dir, "backend", "migrations", "mysql", "001_init.sql"), "SELECT 1;\n")
	write(t, filepath.Join(dir, "web", "route.tsx"), "export {}\n")
	return dir
}

func TestUpdatePluginsFile(t *testing.T) {
	src := []byte(pluginsGo)
	a, err := updatePluginsFile(src, "nexadmin/plugins/zeta", true)
	if err != nil {
		t.Fatal(err)
	}
	a, _ = updatePluginsFile(a, "nexadmin/plugins/alpha", true)
	a, _ = updatePluginsFile(a, "nexadmin/plugins/alpha", true) // 重复添加不产生重复行
	got := string(a)
	if strings.Count(got, `"nexadmin/plugins/alpha"`) != 1 {
		t.Fatalf("duplicate import:\n%s", got)
	}
	if strings.Index(got, "alpha") > strings.Index(got, "zeta") {
		t.Fatalf("imports not sorted:\n%s", got)
	}
	if !strings.Contains(got, markBegin) || !strings.Contains(got, markEnd) {
		t.Fatalf("markers lost:\n%s", got)
	}
	if f, err := format.Source(a); err != nil || string(f) != got {
		t.Fatalf("output is not gofmt-stable (err=%v):\n%s", err, got)
	}
	b, _ := updatePluginsFile(a, "nexadmin/plugins/alpha", false)
	b, _ = updatePluginsFile(b, "nexadmin/plugins/zeta", false)
	if string(b) != pluginsGo {
		t.Fatalf("remove did not restore original:\n%s", b)
	}
	if _, err := updatePluginsFile([]byte("package plugins\n"), "x", true); err == nil {
		t.Fatal("missing markers must fail")
	}
}

func TestInstallAndRemoveDir(t *testing.T) {
	root := newRoot(t)
	pkg := makePkg(t, manifestYAML)
	id, err := install(root, pkg, false)
	if err != nil || id != "demo-x" {
		t.Fatalf("install: %q, %v", id, err)
	}
	for _, p := range []string{
		"plugins/demo-x/plugin.go", "plugins/demo-x/plugin.yaml",
		"plugins/demo-x/migrations/mysql/001_init.sql", "web/src/plugins/demo-x/route.tsx",
	} {
		if _, err := os.Stat(filepath.Join(root, p)); err != nil {
			t.Errorf("missing %s", p)
		}
	}
	if !strings.Contains(read(t, filepath.Join(root, "plugins", "plugins.go")), `_ "nexadmin/plugins/demo-x"`) {
		t.Fatal("plugins.go not updated")
	}
	if _, err := install(root, pkg, false); err == nil {
		t.Fatal("second install without --force must fail")
	}
	if _, err := install(root, pkg, true); err != nil {
		t.Fatalf("force reinstall: %v", err)
	}
	if err := remove(root, "demo-x"); err != nil {
		t.Fatal(err)
	}
	if read(t, filepath.Join(root, "plugins", "plugins.go")) != pluginsGo {
		t.Fatal("plugins.go not restored after remove")
	}
	if exists(filepath.Join(root, "plugins", "demo-x")) || exists(filepath.Join(root, "web", "src", "plugins", "demo-x")) {
		t.Fatal("files remain after remove")
	}
	if err := remove(root, "../etc"); err == nil {
		t.Fatal("remove must reject illegal id")
	}
}

func TestInstallRejectsBadManifest(t *testing.T) {
	root := newRoot(t)
	for _, y := range []string{
		"id: ../evil\nname: x\nversion: 1\nedition: free\n",
		"id: Upper\nname: x\nversion: 1\nedition: free\n",
		"id: ok\nname: x\nversion: 1\nedition: gold\n",
	} {
		if _, err := install(root, makePkg(t, y), false); err == nil {
			t.Errorf("manifest should be rejected: %q", y)
		}
	}
}

func makeZip(t *testing.T, entries map[string]string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "p.zip")
	f, err := os.Create(p)
	if err != nil {
		t.Fatal(err)
	}
	zw := zip.NewWriter(f)
	for name, body := range entries {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		_, _ = w.Write([]byte(body))
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestZipSlipRejected(t *testing.T) {
	for _, name := range []string{"../evil.txt", "demo/../../evil.txt", "/abs/evil.txt", `..\evil.txt`, "C:/evil.txt"} {
		root := newRoot(t)
		z := makeZip(t, map[string]string{
			"plugin.yaml":       manifestYAML,
			"backend/plugin.go": "package demo\n",
			name:                "pwn",
		})
		if _, err := install(root, z, false); err == nil {
			t.Errorf("zip entry %q must be rejected", name)
		}
		if exists(filepath.Join(root, "plugins", "demo-x")) {
			t.Errorf("entry %q: partial install left behind", name)
		}
		if read(t, filepath.Join(root, "plugins", "plugins.go")) != pluginsGo {
			t.Errorf("entry %q: plugins.go modified", name)
		}
	}
	// 越界文件不能真的写到解压目录之外。
	outside := filepath.Join(t.TempDir(), "x")
	if _, err := safeJoin(filepath.Join(outside, "base"), "../x"); err == nil {
		t.Fatal("safeJoin must reject ..")
	}
}

func TestInstallZipOK(t *testing.T) {
	root := newRoot(t)
	z := makeZip(t, map[string]string{
		"demo-x/plugin.yaml":       manifestYAML,
		"demo-x/backend/plugin.go": "package demo\n",
		"demo-x/web/route.tsx":     "export {}\n",
	})
	if id, err := install(root, z, false); err != nil || id != "demo-x" {
		t.Fatalf("install zip: %q, %v", id, err)
	}
	if !exists(filepath.Join(root, "web", "src", "plugins", "demo-x", "route.tsx")) {
		t.Fatal("web not installed")
	}
}

func TestSymlinkRejected(t *testing.T) {
	root := newRoot(t)
	pkg := makePkg(t, manifestYAML)
	if err := os.Symlink("/etc/passwd", filepath.Join(pkg, "backend", "link")); err != nil {
		t.Skip("symlink unsupported")
	}
	if _, err := install(root, pkg, false); err == nil {
		t.Fatal("symlink in package must be rejected")
	}
}

// requires_nexadmin：不满足时 install 拒绝；--force 跳过并警告；框架版本取项目根清单。
func TestInstallRequiresNexadmin(t *testing.T) {
	root := newRoot(t)
	write(t, filepath.Join(root, ".nexadmin.manifest.json"), `{"framework_version":"1.2.0"}`)
	var warn strings.Builder
	old := installWarn
	installWarn = &warn
	defer func() { installWarn = old }()

	ok := makePkg(t, manifestYAML+"requires_nexadmin: \">=1.0.0 <2.0.0\"\n")
	if _, err := install(root, ok, false); err != nil {
		t.Fatalf("满足范围应安装: %v", err)
	}
	if err := remove(root, "demo-x"); err != nil {
		t.Fatal(err)
	}
	bad := makePkg(t, manifestYAML+"requires_nexadmin: \">=2.0.0\"\n")
	_, err := install(root, bad, false)
	if err == nil || !strings.Contains(err.Error(), ">=2.0.0") || !strings.Contains(err.Error(), "--force") {
		t.Fatalf("不满足应拒绝并提示 --force: %v", err)
	}
	if exists(filepath.Join(root, "plugins", "demo-x")) {
		t.Fatal("拒绝时不应留下文件")
	}
	if _, err := install(root, bad, true); err != nil {
		t.Fatalf("--force 应跳过: %v", err)
	}
	if !strings.Contains(warn.String(), "警告") || !strings.Contains(warn.String(), ">=2.0.0") {
		t.Errorf("--force 应输出警告: %q", warn.String())
	}
}
