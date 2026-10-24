package main

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const upRootManifest = `{
  "framework_version": "1.0.0",
  "framework_paths": ["core/", "migrations/framework/", "config/default.yaml"],
  "user_paths": ["internal/", "config/config.yaml"]
}
`

func upFwManifest(version string) string {
	return `{"framework_version": "` + version + `", "framework_paths": ["core/", "migrations/framework/", "config/default.yaml"], ` +
		`"user_paths": ["internal/", "config/config.yaml"]}`
}

// upNewProject 创建一个带框架文件与用户文件的工程根（非 git 仓库）。
func upNewProject(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	write(t, filepath.Join(root, frameworkManifest), upRootManifest)
	write(t, filepath.Join(root, "core", "a.go"), "old a\n")
	write(t, filepath.Join(root, "internal", "mine.go"), "mine\n")
	write(t, filepath.Join(root, "config", "config.yaml"), "mine: true\n")
	return root
}

func upPkg(t *testing.T, manifest string, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	write(t, filepath.Join(dir, frameworkManifest), manifest)
	for p, c := range files {
		write(t, filepath.Join(dir, filepath.FromSlash(p)), c)
	}
	return dir
}

func upRun(t *testing.T, root, from string, dry bool) (string, error) {
	t.Helper()
	var buf bytes.Buffer
	err := upgrade(root, upOptions{From: from, Manifest: frameworkManifest, DryRun: dry}, &buf, time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC))
	return buf.String(), err
}

func TestUpgradeKeepsUserPaths(t *testing.T) {
	root := upNewProject(t)
	pkg := upPkg(t, upFwManifest("1.1.0"), map[string]string{
		"core/a.go":             "new a\n",
		"internal/mine.go":      "OVERWRITE\n", // 包里同名的用户区文件必须被忽略
		"config/config.yaml":    "OVERWRITE\n",
		"config/default.yaml":   "d: 1\n",
		"internal/extra/new.go": "NEW\n",
	})
	out, err := upRun(t, root, pkg, false)
	if err != nil {
		t.Fatal(err, out)
	}
	if read(t, filepath.Join(root, "core", "a.go")) != "new a\n" {
		t.Error("framework file not updated")
	}
	if read(t, filepath.Join(root, "internal", "mine.go")) != "mine\n" || read(t, filepath.Join(root, "config", "config.yaml")) != "mine: true\n" {
		t.Error("user files were touched")
	}
	if exists(filepath.Join(root, "internal", "extra")) {
		t.Error("user path dir created from package")
	}
	rec := read(t, filepath.Join(root, filesRecord))
	if !strings.Contains(rec, "core/a.go") || strings.Contains(rec, "internal/") {
		t.Errorf("bad record: %s", rec)
	}
	if !strings.Contains(read(t, filepath.Join(root, frameworkManifest)), `"framework_version": "1.1.0"`) {
		t.Error("framework_version not updated")
	}
}

func TestUpgradeBacksUpHandEdited(t *testing.T) {
	root := upNewProject(t)
	// 首次（无记录）：与新包不同的文件一律备份。
	pkg1 := upPkg(t, upFwManifest("1.1.0"), map[string]string{"core/a.go": "v1\n"})
	out, err := upRun(t, root, pkg1, false)
	if err != nil {
		t.Fatal(err, out)
	}
	if read(t, filepath.Join(root, "core", "a.go.bak")) != "old a\n" {
		t.Error("first upgrade: unknown file not backed up")
	}
	// 第二次：手改后升级 -> .bak 保存手改内容并标 WARN；未手改的文件不备份。
	write(t, filepath.Join(root, "core", "a.go"), "hand edited\n")
	write(t, filepath.Join(root, "core", "b.go"), "b1\n")
	pkg0 := upPkg(t, upFwManifest("1.1.0"), map[string]string{"core/a.go": "v1\n", "core/b.go": "b1\n"})
	if out, err = upRun(t, root, pkg0, false); err != nil {
		t.Fatal(err, out)
	}
	if !strings.Contains(out, "WARN:") {
		t.Errorf("no WARN in output:\n%s", out)
	}
	if read(t, filepath.Join(root, "core", "a.go.bak.1")) != "hand edited\n" {
		t.Error("hand-edited content not backed up")
	}
	pkg2 := upPkg(t, upFwManifest("1.2.0"), map[string]string{"core/a.go": "v2\n", "core/b.go": "b2\n"})
	if out, err = upRun(t, root, pkg2, false); err != nil {
		t.Fatal(err, out)
	}
	if strings.Contains(out, "WARN:") {
		t.Errorf("unexpected WARN:\n%s", out)
	}
	if read(t, filepath.Join(root, "core", "a.go")) != "v2\n" || read(t, filepath.Join(root, "core", "b.go")) != "b2\n" {
		t.Error("files not updated")
	}
	if exists(filepath.Join(root, "core", "b.go.bak")) {
		t.Error("untouched file must not be backed up")
	}
}

func upSnapshot(t *testing.T, root string) string {
	t.Helper()
	var sb strings.Builder
	_ = filepath.Walk(root, func(p string, info os.FileInfo, err error) error {
		if err == nil && !info.IsDir() {
			b, _ := os.ReadFile(p)
			sb.WriteString(p + "\x00" + string(b) + "\x00")
		} else if err == nil {
			sb.WriteString(p + "/\x00")
		}
		return nil
	})
	return sb.String()
}

func TestUpgradeDryRunChangesNothing(t *testing.T) {
	root := upNewProject(t)
	write(t, filepath.Join(root, "migrations", "framework", "mysql", "a_001.sql"), "x\n")
	write(t, filepath.Join(root, "core", "gone.go"), "gone\n")
	pkg := upPkg(t, upFwManifest("1.1.0"), map[string]string{
		"core/a.go":                              "new\n",
		"migrations/framework/mysql/a_001.sql":   "x\n",
		"migrations/framework/mysql/a_002_x.sql": "y\n",
	})
	// 先正式升一次以建立记录，再让新包删掉 gone.go（它在记录里）。
	pkg0 := upPkg(t, upFwManifest("1.0.5"), map[string]string{"core/a.go": "old a\n", "core/gone.go": "gone\n"})
	if out, err := upRun(t, root, pkg0, false); err != nil {
		t.Fatal(err, out)
	}
	before := upSnapshot(t, root)
	out, err := upRun(t, root, pkg, true)
	if err != nil {
		t.Fatal(err, out)
	}
	if after := upSnapshot(t, root); after != before {
		t.Error("dry-run modified the tree")
	}
	for _, want := range []string{"a_002_x.sql", "D  core/gone.go", "M  core/a.go", "dry-run"} {
		if !strings.Contains(out, want) {
			t.Errorf("dry-run output missing %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "+ migrations/framework/mysql/a_001.sql") {
		t.Error("unchanged migration listed as new")
	}
}

func TestUpgradeDeleteGoesToTrash(t *testing.T) {
	root := upNewProject(t)
	pkg0 := upPkg(t, upFwManifest("1.0.5"), map[string]string{"core/a.go": "a\n", "core/gone.go": "gone\n"})
	if out, err := upRun(t, root, pkg0, false); err != nil {
		t.Fatal(err, out)
	}
	pkg := upPkg(t, upFwManifest("1.1.0"), map[string]string{"core/a.go": "a\n"})
	if out, err := upRun(t, root, pkg, false); err != nil {
		t.Fatal(err, out)
	}
	if exists(filepath.Join(root, "core", "gone.go")) {
		t.Error("deleted file still in place")
	}
	if read(t, filepath.Join(root, trashDir, "20260102-030405", "core", "gone.go")) != "gone\n" {
		t.Error("deleted file not in trash")
	}
	if strings.Contains(read(t, filepath.Join(root, filesRecord)), "gone.go") {
		t.Error("record still lists deleted file")
	}
}

func TestUpgradeDirtyTreeRefused(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	root := upNewProject(t)
	if out, err := exec.Command("git", "-C", root, "init", "-q").CombinedOutput(); err != nil {
		t.Skipf("git init failed: %v %s", err, out)
	}
	pkg := upPkg(t, upFwManifest("1.1.0"), map[string]string{"core/a.go": "new\n"})
	_, err := upRun(t, root, pkg, false) // 未跟踪文件 => 不干净
	if err == nil || !strings.Contains(err.Error(), "不干净") {
		t.Fatalf("want dirty refusal, got %v", err)
	}
	if read(t, filepath.Join(root, "core", "a.go")) != "old a\n" {
		t.Error("files changed despite refusal")
	}
	var buf bytes.Buffer
	if err := upgrade(root, upOptions{From: pkg, Manifest: frameworkManifest, AllowDirty: true}, &buf, time.Now()); err != nil {
		t.Fatalf("--allow-dirty should proceed: %v", err)
	}
}

func TestUpgradeZipSlipRejected(t *testing.T) {
	for _, name := range []string{"../evil.txt", "core/../../evil.txt", "/abs/evil.txt"} {
		root := upNewProject(t)
		z := makeZip(t, map[string]string{
			frameworkManifest: upFwManifest("1.1.0"),
			"core/a.go":       "new\n",
			name:              "pwn",
		})
		if out, err := upRun(t, root, z, false); err == nil {
			t.Errorf("zip entry %q must be rejected\n%s", name, out)
		}
		if read(t, filepath.Join(root, "core", "a.go")) != "old a\n" {
			t.Errorf("entry %q: project modified", name)
		}
	}
}

const upProductManifest = `{
  "product_name": "bi", "product_version": "2.0.0", "based_on_framework": "%s",
  "product_paths": ["modules/bi/"], "user_paths": ["internal/"]
}`

func TestUpgradeProductManifest(t *testing.T) {
	mk := func(constraint string) string {
		dir := t.TempDir()
		write(t, filepath.Join(dir, ".product.manifest.json"), strings.Replace(upProductManifest, "%s", constraint, 1))
		write(t, filepath.Join(dir, "modules", "bi", "x.go"), "bi\n")
		write(t, filepath.Join(dir, "internal", "x.go"), "OVERWRITE\n")
		write(t, filepath.Join(dir, "core", "a.go"), "OVERWRITE\n") // 产品包不能碰框架区
		return dir
	}
	run := func(root, from string) error {
		return upgrade(root, upOptions{From: from, Manifest: ".product.manifest.json"}, &bytes.Buffer{}, time.Now())
	}
	root := upNewProject(t)
	err := run(root, mk(">=2.0.0 <3.0.0"))
	if err == nil || !strings.Contains(err.Error(), "不兼容") || !strings.Contains(err.Error(), "1.0.0") {
		t.Fatalf("want incompatible refusal, got %v", err)
	}
	if exists(filepath.Join(root, "modules")) {
		t.Error("files written despite refusal")
	}
	if err := run(root, mk("^1.0.0")); err != nil {
		t.Fatal(err)
	}
	if read(t, filepath.Join(root, "modules", "bi", "x.go")) != "bi\n" {
		t.Error("product file not installed")
	}
	if read(t, filepath.Join(root, "core", "a.go")) != "old a\n" || exists(filepath.Join(root, "internal", "x.go")) {
		t.Error("product upgrade touched framework/user area")
	}
	if !strings.Contains(read(t, filepath.Join(root, ".product.manifest.json")), `"product_version": "2.0.0"`) {
		t.Error("product manifest not written")
	}
	if !strings.Contains(read(t, filepath.Join(root, frameworkManifest)), `"1.0.0"`) {
		t.Error("framework manifest must be untouched")
	}
}

func TestUpgradeProductRejectsFrameworkToFlag(t *testing.T) {
	root := upNewProject(t)
	dir := t.TempDir()
	write(t, filepath.Join(dir, ".product.manifest.json"), strings.Replace(upProductManifest, "%s", "^1.0.0", 1))
	write(t, filepath.Join(dir, "modules", "bi", "x.go"), "bi\n")
	if err := upgrade(root, upOptions{
		From: dir, To: "9.9.9", Manifest: ".product.manifest.json", AllowDirty: true,
	}, &bytes.Buffer{}, time.Now()); err == nil || !strings.Contains(err.Error(), "--to 仅适用于框架升级包") {
		t.Fatalf("product upgrade with framework --to should be rejected explicitly: %v", err)
	}
}

func TestVersionSatisfies(t *testing.T) {
	cases := []struct {
		v, c string
		want bool
	}{
		{"1.3.0", ">=1.2.0 <2.0.0", true}, {"2.0.0", ">=1.2.0,<2.0.0", false}, {"v1.9.9", "^1.2", true},
		{"0.2.5", "^0.2.0", true}, {"0.3.0", "^0.2.0", false}, {"1.2.9", "~1.2.0", true}, {"1.3.0", "~1.2.0", false},
		{"1.0.0", "1.0.0", true},
	}
	for _, c := range cases {
		got, err := versionSatisfies(c.v, c.c)
		if err != nil || got != c.want {
			t.Errorf("%s %s = %v,%v want %v", c.v, c.c, got, err, c.want)
		}
	}
	if _, err := versionSatisfies("1.0.0", "abc"); err == nil {
		t.Error("bad constraint must error")
	}
}

const upLicenseManifest = `{"framework_version": "%s", "framework_paths": ["core/", "config/default.yaml", "config/license-keys.pub", "config/license-revoked.json"], ` +
	`"user_paths": ["internal/", "config/config.yaml"]}`

// 吊销清单 / 附加公钥由发行方随包分发：新包里没有它们时必须保留现有文件（移走会让吊销悄悄失效），
// dry-run 清单里体现为 K（保留），普通框架文件被删除仍移入回收目录。
func TestUpgradeKeepsOptionalManifestPathsWhenMissingInPackage(t *testing.T) {
	root := upNewProject(t)
	write(t, filepath.Join(root, frameworkManifest), strings.Replace(upLicenseManifest, "%s", "1.0.0", 1))
	pkg0 := upPkg(t, strings.Replace(upLicenseManifest, "%s", "1.0.5", 1), map[string]string{
		"core/a.go": "a\n", "core/gone.go": "gone\n", "config/license-revoked.json": "{\"seq\":1}", "config/license-keys.pub": "KEY1\n",
	})
	if out, err := upRun(t, root, pkg0, false); err != nil {
		t.Fatal(err, out)
	}
	rev := filepath.Join(root, "config", "license-revoked.json")
	pkg1 := upPkg(t, strings.Replace(upLicenseManifest, "%s", "1.1.0", 1), map[string]string{"core/a.go": "a\n"})

	out, err := upRun(t, root, pkg1, true)
	if err != nil {
		t.Fatal(err, out)
	}
	for _, want := range []string{"K  config/license-revoked.json", "K  config/license-keys.pub", "D  core/gone.go", "保留 2"} {
		if !strings.Contains(out, want) {
			t.Errorf("dry-run 清单应含 %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "D  config/license-") {
		t.Errorf("可选路径不应列为删除:\n%s", out)
	}
	if read(t, rev) != "{\"seq\":1}" {
		t.Fatal("dry-run 不应改动文件")
	}

	if out, err := upRun(t, root, pkg1, false); err != nil {
		t.Fatal(err, out)
	}
	if read(t, rev) != "{\"seq\":1}" || read(t, filepath.Join(root, "config", "license-keys.pub")) != "KEY1\n" {
		t.Error("包里缺失时，吊销清单与附加公钥必须保留")
	}
	if exists(filepath.Join(root, trashDir, "20260102-030405", "config")) {
		t.Error("可选路径不应进回收目录")
	}
	if exists(filepath.Join(root, "core", "gone.go")) || read(t, filepath.Join(root, trashDir, "20260102-030405", "core", "gone.go")) != "gone\n" {
		t.Error("普通框架文件被删除仍应进回收目录")
	}
	rec := read(t, filepath.Join(root, filesRecord))
	if !strings.Contains(rec, "config/license-revoked.json") || strings.Contains(rec, "gone.go") {
		t.Errorf("保留文件的记录应继续存在、被删文件的记录应清掉:\n%s", rec)
	}

	// 之后的包里再带上新清单：按框架区覆盖（与记录一致 = 没人手改，直接覆盖）
	pkg2 := upPkg(t, strings.Replace(upLicenseManifest, "%s", "1.2.0", 1), map[string]string{"core/a.go": "a\n", "config/license-revoked.json": "{\"seq\":2}"})
	if out, err := upRun(t, root, pkg2, false); err != nil {
		t.Fatal(err, out)
	}
	if read(t, rev) != "{\"seq\":2}" {
		t.Error("包里带了新清单时应覆盖")
	}
}
