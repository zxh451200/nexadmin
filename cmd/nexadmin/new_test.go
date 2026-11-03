package main

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"

	"nexadmin/core/plugin"
)

func wfile(t *testing.T, root, rel, content string) {
	t.Helper()
	p := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

const tplPlugins = "package plugins\n\nimport (\n\t// nexadmin:plugins:begin\n\t_ \"nexadmin/plugins/cron\"\n\t_ \"nexadmin/plugins/totp\"\n\t// nexadmin:plugins:end\n)\n"

func makeTemplate(t *testing.T) string {
	t.Helper()
	r := t.TempDir()
	wfile(t, r, "go.mod", "module nexadmin\n\ngo 1.25.0\n")
	wfile(t, r, ".nexadmin.manifest.json", `{"framework_version":"0.1.0","framework_paths":["core/","docs/"],"user_paths":["internal/","plugins/","web/src/plugins/","config/config.yaml","data/"]}`)
	wfile(t, r, "core/a.go", "package core\n\n// 来自 nexadmin 的说明，见 \"nexadmin/x\" 注释\nimport (\n\t\"fmt\"\n\n\t\"nexadmin/pkg/b\"\n\tx \"nexadmin/plugins/cron\"\n)\n\nvar s = \"nexadmin/pkg/b 不是 import\"\n")
	wfile(t, r, "cmd/server/main.go", "package main\n\nimport _ \"nexadmin/plugins\"\n\nfunc main() {}\n")
	wfile(t, r, "plugins/plugins.go", tplPlugins)
	wfile(t, r, "plugins/cron/cron.go", "package cron\n")
	wfile(t, r, "plugins/totp/plugin.go", "package totp\n\nimport \"nexadmin/core\"\n\nvar _ = core.Perm\n")
	wfile(t, r, "plugins/totp/migrations/mysql/001_init.sql", "CREATE TABLE plg_totp_secret (id BIGINT);\n")
	wfile(t, r, "web/src/plugins/.gitkeep", "")
	wfile(t, r, "web/src/plugins/cron/index.ts", "x")
	wfile(t, r, "web/src/plugins/totp/route.tsx", "export {}\n")
	wfile(t, r, "web/package.json", "{\n  \"name\": \"nexadmin-web\",\n  \"dependencies\": {\"name\": \"x\"}\n}\n")
	wfile(t, r, "web/node_modules/m/i.js", "x")
	wfile(t, r, "web/dist/i.html", "x")
	wfile(t, r, "data/.gitkeep", "")
	wfile(t, r, "data/app.db", "x")
	wfile(t, r, "data/licenses/a.lic", "x")
	wfile(t, r, "logs/.gitkeep", "")
	wfile(t, r, "logs/s.log", "x")
	wfile(t, r, "backup/b.sql", "x")
	wfile(t, r, "config/config.yaml", "secret: 1")
	wfile(t, r, "config/default.yaml", "name: NexAdmin")
	wfile(t, r, ".dev-keys/private.pem", "k")
	wfile(t, r, ".nexadmin.files.json", "{}")
	wfile(t, r, ".nexadmin-trash/x/y", "x")
	wfile(t, r, "scripts/derive_check/run.sh", "x")
	wfile(t, r, "scripts/contract_check/main.go", "package main")
	wfile(t, r, "scripts/gen_icons/main.go", "package main")
	wfile(t, r, ".github/workflows/derive.yml", "x")
	wfile(t, r, ".github/workflows/ci.yml", "x")
	wfile(t, r, "docs/engines.md", "doc")
	wfile(t, r, "x/run.log", "x")
	return r
}

func newTo(t *testing.T, tpl string, o newOptions) (string, string, error) {
	t.Helper()
	dst := filepath.Join(t.TempDir(), "proj")
	o.Out = dst
	if o.Name == "" {
		o.Name = "demo"
	}
	var buf bytes.Buffer
	err := newProject(tpl, o, &buf)
	return dst, buf.String(), err
}

func has(root, rel string) bool { return exists(filepath.Join(root, filepath.FromSlash(rel))) }

func readStr(t *testing.T, root, rel string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestNewExcludes(t *testing.T) {
	dst, _, err := newTo(t, makeTemplate(t), newOptions{Module: "example.com/demo"})
	if err != nil {
		t.Fatal(err)
	}
	for _, rel := range []string{"web/node_modules", "web/dist", "data/app.db", "data/licenses", "logs/s.log", "backup",
		"config/config.yaml", ".dev-keys", ".nexadmin-trash", "scripts/derive_check", "scripts/contract_check", "scripts/gen_icons", ".github/workflows/derive.yml",
		"x/run.log", "plugins/cron", "web/src/plugins/cron"} {
		if has(dst, rel) {
			t.Errorf("不应拷贝 %s", rel)
		}
	}
	for _, rel := range []string{"data/.gitkeep", "logs/.gitkeep", "web/src/plugins/.gitkeep", "plugins/plugins.go",
		".github/workflows/ci.yml", "config/default.yaml", "docs/engines.md", "internal/modules/.gitkeep",
		"web/src/pages/.gitkeep", "migrations/business/.gitkeep"} {
		if !has(dst, rel) {
			t.Errorf("应保留 %s", rel)
		}
	}
	pl := readStr(t, dst, "plugins/plugins.go")
	if strings.Contains(pl, "cron") || !strings.Contains(pl, "nexadmin:plugins:begin") || !strings.Contains(pl, "nexadmin:plugins:end") {
		t.Errorf("plugins.go 应清空非默认插件导入并保留标记行:\n%s", pl)
	}
	// 两步验证是安全基线：默认随项目交付，import 已按目标模块名改写。
	for _, rel := range []string{"plugins/totp/plugin.go", "web/src/plugins/totp/route.tsx"} {
		if !has(dst, rel) {
			t.Errorf("默认应带上 %s", rel)
		}
	}
	if !strings.Contains(pl, `_ "example.com/demo/plugins/totp"`) {
		t.Errorf("plugins.go 应保留并改写 totp 导入:\n%s", pl)
	}
	if src := readStr(t, dst, "plugins/totp/plugin.go"); !strings.Contains(src, `"example.com/demo/core"`) {
		t.Errorf("totp 源码 import 未改写:\n%s", src)
	}
}

func TestNewWithoutTotp(t *testing.T) {
	dst, _, err := newTo(t, makeTemplate(t), newOptions{Module: "example.com/demo", WithoutTotp: true})
	if err != nil {
		t.Fatal(err)
	}
	for _, rel := range []string{"plugins/totp", "web/src/plugins/totp"} {
		if has(dst, rel) {
			t.Errorf("--without-totp 不应带上 %s", rel)
		}
	}
	if pl := readStr(t, dst, "plugins/plugins.go"); strings.Contains(pl, "totp") {
		t.Errorf("--without-totp 不应保留 totp 导入:\n%s", pl)
	}
}

func TestNewWithPlugins(t *testing.T) {
	dst, _, err := newTo(t, makeTemplate(t), newOptions{Module: "example.com/demo", WithPlugins: true})
	if err != nil {
		t.Fatal(err)
	}
	if !has(dst, "plugins/cron/cron.go") || !has(dst, "web/src/plugins/cron/index.ts") {
		t.Error("--with-plugins 应带上插件目录")
	}
	if pl := readStr(t, dst, "plugins/plugins.go"); !strings.Contains(pl, `_ "example.com/demo/plugins/cron"`) {
		t.Errorf("插件导入应保留并改写前缀:\n%s", pl)
	}
}

func TestNewNamedPlugins(t *testing.T) {
	dst, _, err := newTo(t, makeTemplate(t), newOptions{Module: "example.com/demo", Plugins: []string{"cron"}})
	if err != nil {
		t.Fatal(err)
	}
	for _, rel := range []string{"plugins/cron/cron.go", "web/src/plugins/cron/index.ts"} {
		if !has(dst, rel) {
			t.Errorf("--plugins cron 应带上 %s", rel)
		}
	}
	pl := readStr(t, dst, "plugins/plugins.go")
	if !strings.Contains(pl, `_ "example.com/demo/plugins/cron"`) {
		t.Errorf("--plugins cron 应写入导入:\n%s", pl)
	}
	if !strings.Contains(pl, "totp") {
		t.Errorf("--plugins 不该弄丢 totp 基线:\n%s", pl)
	}
}

func TestNewNamedTotpBeatsWithoutTotp(t *testing.T) {
	dst, _, err := newTo(t, makeTemplate(t), newOptions{Module: "example.com/demo", WithoutTotp: true, Plugins: []string{"totp"}})
	if err != nil {
		t.Fatal(err)
	}
	if !has(dst, "plugins/totp/plugin.go") {
		t.Error("显式点名 totp 应压过 --without-totp")
	}
	if pl := readStr(t, dst, "plugins/plugins.go"); !strings.Contains(pl, "totp") {
		t.Errorf("显式点名的 totp 应写回导入:\n%s", pl)
	}
}

func TestNewUnknownPlugin(t *testing.T) {
	dst, _, err := newTo(t, makeTemplate(t), newOptions{Module: "example.com/demo", Plugins: []string{"nope"}})
	if err == nil {
		t.Fatal("点名的插件不存在时要报错，不能静默少带")
	}
	if !strings.Contains(err.Error(), "cron") {
		t.Errorf("报错应给出可选清单: %v", err)
	}
	if has(dst, "core") || has(dst, "go.mod") {
		t.Errorf("校验必须排在拷贝之前，别留下半成品项目: %s", dst)
	}
}

func TestNewPluginsPathGuard(t *testing.T) {
	_, _, err := newTo(t, makeTemplate(t), newOptions{Module: "example.com/demo", Plugins: []string{"../core"}})
	if err == nil {
		t.Fatal("带路径分隔符的插件名必须拒绝")
	}
	if !strings.Contains(err.Error(), "插件名不合法") {
		t.Errorf("必须被名字校验拦下，而不是走到复制环节才炸: %v", err)
	}
}

func TestNewPluginsMutexWithAll(t *testing.T) {
	_, _, err := newTo(t, makeTemplate(t), newOptions{Module: "example.com/demo", WithPlugins: true, Plugins: []string{"cron"}})
	if err == nil {
		t.Fatal("--plugins 与 --with-plugins 同时给出要报错")
	}
}

func TestExpandPluginPresets(t *testing.T) {
	if got := expandPluginPresets([]string{"basic"}); len(got) != len(pluginPresets["basic"]) {
		t.Errorf("basic 应展开成 %d 个，实得 %v", len(pluginPresets["basic"]), got)
	}
	if got := expandPluginPresets([]string{"cron"}); len(got) != 1 || got[0] != "cron" {
		t.Errorf("不是别名的要原样交给校验，实得 %v", got)
	}
	seen := map[string]bool{}
	for _, id := range carryPlugins(newOptions{Plugins: []string{"basic", "standard", "cron"}}) {
		if seen[id] {
			t.Errorf("%s 被重复下发", id)
		}
		seen[id] = true
	}
	for _, want := range []string{"totp", "crm", "report", "audit", "security"} {
		if !seen[want] {
			t.Errorf("预设展开后缺 %s", want)
		}
	}
}

// 预设是人工策展的清单，最容易烂在两处：加了新插件没归类、别名撞了插件名。
// 这两条都在这里钉死，别等用户 --plugins=basic 少装了一个插件才发现。
func TestPluginPresetsMatchManifests(t *testing.T) {
	root, err := findRoot()
	if err != nil {
		t.Fatal(err)
	}
	dirs, err := os.ReadDir(filepath.Join(root, "plugins"))
	if err != nil {
		t.Fatal(err)
	}
	editions := map[string]string{}
	for _, d := range dirs {
		if !d.IsDir() {
			continue
		}
		raw, err := os.ReadFile(filepath.Join(root, "plugins", d.Name(), plugin.ManifestFile))
		if err != nil {
			continue
		}
		m := regexp.MustCompile(`(?m)^edition:\s*([A-Za-z]+)`).FindSubmatch(raw)
		if m == nil {
			t.Errorf("plugins/%s 的清单没有 edition 行", d.Name())
			continue
		}
		editions[d.Name()] = string(m[1])
	}
	if len(editions) < 10 {
		t.Fatalf("只读到 %d 个插件清单，母版应该更多——测试夹具或路径不对", len(editions))
	}
	for name, ids := range pluginPresets {
		if _, ok := editions[name]; ok {
			t.Errorf("预设别名 %q 与插件同名，展开会盖掉真插件", name)
		}
		for _, id := range ids {
			if _, ok := editions[id]; !ok {
				t.Errorf("预设 %s 里的 %q 在 plugins/ 下不存在", name, id)
			}
		}
	}
	for _, id := range pluginPresets["basic"] {
		if editions[id] != "free" {
			t.Errorf("basic 里的 %s 是 %s：basic 承诺开箱即用，不该含需要授权的插件", id, editions[id])
		}
	}
	basic := pluginPresets["basic"]
	for id, ed := range editions {
		if ed == "free" && !slices.Contains(basic, id) {
			t.Errorf("free 插件 %s 没进 basic 预设：新加通用插件要顺手归类", id)
		}
	}
	for _, id := range pluginPresets["standard"] {
		if !slices.Contains(basic, id) && editions[id] != "pro" {
			t.Errorf("standard 相对 basic 多出的 %s 应该是 pro（买授权即生效那一类）", id)
		}
	}
}

func TestNewRewriteImports(t *testing.T) {
	dst, _, err := newTo(t, makeTemplate(t), newOptions{Module: "example.com/demo"})
	if err != nil {
		t.Fatal(err)
	}
	if got := readStr(t, dst, "go.mod"); !strings.HasPrefix(got, "module example.com/demo\n") {
		t.Errorf("go.mod: %q", got)
	}
	a := readStr(t, dst, "core/a.go")
	for _, want := range []string{`"example.com/demo/pkg/b"`, `x "example.com/demo/plugins/cron"`,
		"来自 nexadmin 的说明，见 \"nexadmin/x\" 注释", `var s = "nexadmin/pkg/b 不是 import"`} {
		if !strings.Contains(a, want) {
			t.Errorf("core/a.go 缺少 %q:\n%s", want, a)
		}
	}
	if m := readStr(t, dst, "cmd/server/main.go"); !strings.Contains(m, `"example.com/demo/plugins"`) {
		t.Errorf("main.go: %s", m)
	}
	pj := readStr(t, dst, "web/package.json")
	if !strings.Contains(pj, `"name": "demo"`) || !strings.Contains(pj, `{"name": "x"}`) {
		t.Errorf("package.json 只应改第一个 name:\n%s", pj)
	}
	if cm := readStr(t, dst, "config/default.yaml"); cm != "name: NexAdmin" {
		t.Errorf("config 站点名应保持: %q", cm)
	}
}

func TestNewRefuseExisting(t *testing.T) {
	tpl := makeTemplate(t)
	dst := t.TempDir()
	wfile(t, dst, "keep.txt", "x")
	var buf bytes.Buffer
	if err := newProject(tpl, newOptions{Name: "demo", Out: dst}, &buf); err == nil || !strings.Contains(err.Error(), "非空") {
		t.Fatalf("非空目录应拒绝, got %v", err)
	}
	if got := readStr(t, dst, "keep.txt"); got != "x" {
		t.Error("已有文件被改动")
	}
	empty := t.TempDir()
	if err := newProject(tpl, newOptions{Name: "demo", Out: empty}, &buf); err != nil {
		t.Errorf("空目录应允许: %v", err)
	}
	if err := newProject(tpl, newOptions{Name: "demo", Out: filepath.Join(tpl, "inner")}, &buf); err == nil {
		t.Error("输出到模板内部应拒绝")
	}
	if err := newProject(tpl, newOptions{Name: "bad name", Out: filepath.Join(t.TempDir(), "p")}, &buf); err == nil {
		t.Error("非法名称应拒绝")
	}
}

func TestNewManifestAndBaseline(t *testing.T) {
	dst, _, err := newTo(t, makeTemplate(t), newOptions{Name: "demo", Module: "example.com/demo"})
	if err != nil {
		t.Fatal(err)
	}
	var m newManifest
	if err := json.Unmarshal([]byte(readStr(t, dst, ".nexadmin.manifest.json")), &m); err != nil {
		t.Fatal(err)
	}
	if m.FrameworkVersion != "0.1.0" || m.DerivedFrom != "0.1.0" || m.ProjectName != "demo" ||
		len(m.FrameworkPaths) != 2 || len(m.UserPaths) != 5 {
		t.Errorf("清单不对: %+v", m)
	}
	if _, err := loadUpManifest(filepath.Join(dst, frameworkManifest)); err != nil {
		t.Errorf("upgrade 应能读取新清单: %v", err)
	}
	rec, had, err := loadUpRecord(dst)
	if err != nil || !had {
		t.Fatalf("基线应可被 upgrade 读取: %v %v", had, err)
	}
	if len(rec) != 2 || rec["core/a.go"] == "" || rec["docs/engines.md"] == "" {
		t.Fatalf("基线应只含框架区文件: %v", rec)
	}
	h, _ := upHashFile(filepath.Join(dst, "core", "a.go"))
	if rec["core/a.go"] != h {
		t.Error("基线哈希应是改写 import 之后的内容")
	}
	for _, f := range []string{"AGENTS.md", "CLAUDE.md"} {
		raw := readStr(t, dst, f)
		if n := strings.Count(raw, "\n"); n > 120 || raw != agentsMD {
			t.Errorf("%s 行数 %d（上限 120）或内容不一致", f, n)
		}
	}
	for _, want := range []string{"add module", "--sortable", "i18n add", "i18n check", "upgrade --from", "--dry-run", "release pack",
		"disabled_modules", "core.RegisterBuiltin", "web/src/pages", "biz_", "migrations/business", ".bak", "go.mod", "package.json",
		"绝不手改框架区", "移动优先", "语义 token", "mock", "docs/二次开发指南.md", "web/src/pages/<名>/cards.tsx", "不要改 framework 的工作台页面"} {
		if !strings.Contains(agentsMD, want) {
			t.Errorf("AGENTS.md 应包含 %q", want)
		}
	}
	// 注册方式已改为 init()，main.go 不再注册模块：不得残留过时说法
	for _, bad := range []string{"尚未实现", "app.Register(", "main.go 里 ", "example.Module"} {
		if strings.Contains(agentsMD, bad) {
			t.Errorf("AGENTS.md 不应再有过时说法 %q", bad)
		}
	}
}

func TestNewGit(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("无 git")
	}
	dst, out, err := newTo(t, makeTemplate(t), newOptions{Module: "example.com/demo", Git: true})
	if err != nil {
		t.Fatal(err)
	}
	if !has(dst, ".git") {
		t.Error("--git 应创建 .git")
	}
	if !strings.Contains(out, "cd web && npm install") || !strings.Contains(out, "go run ./cmd/server") {
		t.Errorf("缺少下一步提示:\n%s", out)
	}
}

// new 出来的项目（模块路径已改）首次用旧模块路径的升级包 upgrade：按改写后的内容比较，不误判手改。
func TestUpgradeAfterNewRewritesImports(t *testing.T) {
	tpl := makeTemplate(t)
	dst, _, err := newTo(t, tpl, newOptions{Module: "example.com/demo"})
	if err != nil {
		t.Fatal(err)
	}
	pkg := t.TempDir()
	wfile(t, pkg, "go.mod", "module nexadmin\n\ngo 1.25.0\n")
	wfile(t, pkg, ".nexadmin.manifest.json", `{"framework_version":"0.2.0","framework_paths":["core/","docs/"],"user_paths":["internal/","plugins/","web/src/plugins/","config/config.yaml","data/"]}`)
	wfile(t, pkg, "core/a.go", readStr(t, tpl, "core/a.go")+"\nvar z = 1\n")
	wfile(t, pkg, "core/b.go", "package core\n\nimport \"nexadmin/pkg/b\"\n\nvar _ = b.X\n")
	wfile(t, pkg, "docs/engines.md", "doc")

	var buf bytes.Buffer
	if err := upgrade(dst, upOptions{From: pkg, Manifest: frameworkManifest, DryRun: true, AllowDirty: true}, &buf, time.Now()); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(buf.String(), "WARN:") || !strings.Contains(buf.String(), "新增 1，修改 1，删除 0，未变 1，WARN 0") {
		t.Fatalf("dry-run 清单不对:\n%s", buf.String())
	}
	if strings.Contains(readStr(t, dst, "core/a.go"), "var z") {
		t.Fatal("dry-run 不应改文件")
	}
	buf.Reset()
	if err := upgrade(dst, upOptions{From: pkg, Manifest: frameworkManifest, AllowDirty: true}, &buf, time.Now()); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(buf.String(), "WARN:") || !strings.Contains(buf.String(), "WARN 0") {
		t.Errorf("不应有 WARN:\n%s", buf.String())
	}
	baks, _ := filepath.Glob(filepath.Join(dst, "core", "*.bak*"))
	if len(baks) != 0 {
		t.Errorf("不应产生 .bak: %v", baks)
	}
	for _, f := range []string{"core/a.go", "core/b.go"} {
		got := readStr(t, dst, f)
		if strings.Contains(got, "\t\"nexadmin/") || strings.Contains(got, "import \"nexadmin/") || !strings.Contains(got, `"example.com/demo/pkg/b"`) {
			t.Errorf("%s import 应为新路径:\n%s", f, got)
		}
	}
	if !strings.Contains(readStr(t, dst, "core/a.go"), "var z = 1") {
		t.Error("a.go 应已升级")
	}
	if !strings.Contains(readStr(t, pkg, "core/b.go"), `"nexadmin/pkg/b"`) {
		t.Error("升级包本身不应被改动")
	}
	// 基线记录的是改写后内容的哈希
	rec, _, _ := loadUpRecord(dst)
	if h, _ := upHashFile(filepath.Join(dst, "core", "b.go")); rec["core/b.go"] != h {
		t.Error("哈希记录应基于改写后的内容")
	}
}

// web/.env.production、web/.env.development 属框架区（升级覆盖）；用户本地覆盖用 .env.local：不下发、不属于任何分区。
func TestEnvFilesPartition(t *testing.T) {
	man, err := loadUpManifest(filepath.Join("..", "..", frameworkManifest))
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range []string{"web/.env.production", "web/.env.development"} {
		if !upMatch(f, man.FrameworkPaths) || upMatch(f, man.UserPaths) {
			t.Errorf("%s 应只属于 framework_paths", f)
		}
		if newSkip(f, false, false) {
			t.Errorf("%s 应随 new 下发", f)
		}
	}
	for _, f := range []string{".env.local", "web/.env.local"} {
		if upMatch(f, man.FrameworkPaths) || upMatch(f, man.UserPaths) || upMatch(f, man.MergePaths) {
			t.Errorf("%s 不应属于任何分区", f)
		}
		if !newSkip(f, false, false) {
			t.Errorf("%s 不应被 new / release pack 带走", f)
		}
	}
	gi, err := os.ReadFile(filepath.Join("..", "..", ".gitignore"))
	if err != nil {
		t.Errorf("读 .gitignore 失败: %v", err)
	} else {
		// Windows 上 core.autocrlf=true 会把签出的文本变成 CRLF，
		// 这里只关心「有没有这条规则」，先归一化行尾再比对。
		text := strings.ReplaceAll(string(gi), "\r\n", "\n")
		if !strings.Contains("\n"+text, "\n.env.local\n") {
			t.Error(".gitignore 应忽略 .env.local")
		}
	}
}
