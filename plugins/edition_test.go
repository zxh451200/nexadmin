package plugins_test

import (
	"encoding/json"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"

	"nexadmin/core/plugin"
	"nexadmin/core/plugin/market"
)

// Only these general-purpose enhancements move to Free; other plugins stay Pro.
var freePluginIDs = map[string]bool{
	"cron": true, "codegen": true, "imports": true, "totp": true,
	"ldap": true, "maps": true, "crm": true, "realname": true,
}

// isFrameworkMaster 判断这棵树是不是框架母版：母版的 Go 模块名就是 nexadmin，
// 派生项目改成了自己的模块名。母版才有全部插件，「目录必须对上本地清单」这类闸只在母版成立。
func isFrameworkMaster(root string) bool {
	raw, err := os.ReadFile(filepath.Join(root, "go.mod"))
	if err != nil {
		return false
	}
	for _, l := range strings.Split(string(raw), "\n") {
		if f := strings.Fields(l); len(f) == 2 && f[0] == "module" {
			return strings.Trim(f[1], `"`) == "nexadmin"
		}
	}
	return false
}

func editionContractRoot(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("cannot locate plugin contract tests")
	}
	return filepath.Dir(filepath.Dir(file))
}

func TestPluginEditionsMatchManifestRegistrationAndCatalog(t *testing.T) {
	runEditionContract(t, editionContractRoot(t))
}

// runEditionContract 核对「本地 plugin.yaml ↔ plugin.go 注册 ↔ 市场目录条目」三者一致。
// 抽成接收 root 的函数，是为了能被一条合成树用例（只带部分插件的派生项目）当成靶子跑。
func runEditionContract(t *testing.T, root string) {
	t.Helper()
	paths, err := filepath.Glob(filepath.Join(root, "plugins", "*", plugin.ManifestFile))
	if err != nil || len(paths) == 0 {
		t.Fatalf("find manifests: %v (%d found)", err, len(paths))
	}
	manifests := make(map[string]*plugin.Manifest)
	for _, path := range paths {
		dir := filepath.Dir(path)
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		man := new(plugin.Manifest)
		decoder := yaml.NewDecoder(strings.NewReader(string(raw)))
		decoder.KnownFields(true)
		if err := decoder.Decode(man); err != nil {
			t.Fatalf("%s: %v", path, err)
		}
		if err := man.Validate(); err != nil {
			t.Fatalf("%s: %v", path, err)
		}
		if man.ID != filepath.Base(dir) {
			t.Fatalf("%s: manifest ID %q differs from directory", path, man.ID)
		}
		manifests[man.ID] = man
		t.Run(man.ID, func(t *testing.T) {
			want := plugin.Pro
			if freePluginIDs[man.ID] {
				want = plugin.Free
			}
			if man.Edition != want {
				t.Errorf("manifest edition = %q, want %q", man.Edition, want)
			}
			reg := readPluginRegistration(t, filepath.Join(dir, "plugin.go"))
			if reg.ID != man.ID || reg.Name != man.Name || reg.Version != man.Version || reg.Edition != man.Edition {
				t.Errorf("registration metadata %+v differs from manifest %+v", reg, man)
			}
		})
	}

	entries, err := market.LoadCatalog(root)
	if err != nil || len(entries) == 0 {
		t.Fatalf("load catalog: %v (%d entries)", err, len(entries))
	}
	listed := make(map[string]bool)
	master := isFrameworkMaster(root)
	for _, entry := range entries {
		listed[entry.ID] = true
		if manifests[entry.ID] == nil && !master {
			// 派生项目只带部分插件：目录里没落地的条目不算失败，落地的仍逐条核对
			continue
		}
		t.Run("catalog/"+entry.ID, func(t *testing.T) {
			man := manifests[entry.ID]
			if man == nil {
				t.Fatal("catalog entry has no local manifest")
			}
			if entry.Edition != man.Edition || entry.Version != man.Version || entry.Name != man.Name {
				t.Errorf("catalog edition/version/name differs from manifest: %+v", entry)
			}
			if freePluginIDs[entry.ID] {
				if entry.Price != "免费" {
					t.Errorf("free plugin price = %q, want 免费", entry.Price)
				}
				if entry.Description != man.Description || !strings.Contains(entry.Description, "免费") || strings.Contains(entry.Description, "付费") {
					t.Error("free catalog and manifest descriptions must agree and describe the free tier")
				}
				en := strings.ToLower(entry.I18n["en-US"].Description)
				if !strings.HasPrefix(en, "free ") || strings.Contains(en, "paid") {
					t.Error("English catalog description must describe the free tier")
				}
			} else if entry.Edition != plugin.Pro || entry.Price == "免费" {
				t.Error("other catalog plugins must remain Pro")
			}
		})
	}
	for id := range freePluginIDs {
		if !master && manifests[id] == nil {
			// 派生项目按 --plugins 点名只带一部分插件：没落地的免费插件不该被要求补齐
			continue
		}
		if manifests[id] == nil || !listed[id] {
			t.Errorf("free plugin %s must have both a manifest and a catalog entry", id)
		}
	}
}

// Inspect source instead of importing plugin implementations into core or module
// tests: imports is itself used by business modules, so that would create cycles.
// This also covers optional totp/ldap without adding them to plugins.go or mounting
// their login hooks. Runtime rejection remains covered by core/plugin tests.
func readPluginRegistration(t *testing.T, path string) plugin.Plugin {
	t.Helper()
	file, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	alias := "plugin"
	for _, imp := range file.Imports {
		if imp.Path.Value == `"nexadmin/core/plugin"` && imp.Name != nil {
			alias = imp.Name.Name
		}
	}
	isPluginSelector := func(expr ast.Expr, name string) bool {
		sel, ok := expr.(*ast.SelectorExpr)
		if !ok || sel.Sel.Name != name {
			return false
		}
		pkg, ok := sel.X.(*ast.Ident)
		return ok && pkg.Name == alias
	}
	var registrations []plugin.Plugin
	for _, decl := range file.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Name.Name != "init" || fn.Recv != nil {
			continue
		}
		ast.Inspect(fn.Body, func(node ast.Node) bool {
			call, ok := node.(*ast.CallExpr)
			if !ok || !isPluginSelector(call.Fun, "Register") {
				return true
			}
			if len(call.Args) != 1 {
				t.Fatalf("%s: expected one plugin.Register argument", path)
			}
			lit, ok := call.Args[0].(*ast.CompositeLit)
			if !ok || !isPluginSelector(lit.Type, "Plugin") {
				t.Fatalf("%s: expected a literal plugin.Plugin registration", path)
			}
			fields := make(map[string]string)
			for _, element := range lit.Elts {
				kv, ok := element.(*ast.KeyValueExpr)
				if !ok {
					t.Fatalf("%s: registration fields must be named", path)
				}
				key := kv.Key.(*ast.Ident).Name
				switch key {
				case "ID", "Name", "Version":
					value, ok := kv.Value.(*ast.BasicLit)
					if !ok || value.Kind != token.STRING {
						t.Fatalf("%s: %s must be a string literal", path, key)
					}
					fields[key], err = strconv.Unquote(value.Value)
					if err != nil {
						t.Fatal(err)
					}
				case "Edition":
					if isPluginSelector(kv.Value, "Free") {
						fields[key] = string(plugin.Free)
					} else if isPluginSelector(kv.Value, "Pro") {
						fields[key] = string(plugin.Pro)
					} else {
						t.Fatalf("%s: edition must be plugin.Free or plugin.Pro", path)
					}
				}
			}
			registrations = append(registrations, plugin.Plugin{
				ID: fields["ID"], Name: fields["Name"], Version: fields["Version"], Edition: plugin.Edition(fields["Edition"]),
			})
			return false
		})
	}
	if len(registrations) != 1 {
		t.Fatalf("%s: expected one registration, got %d", path, len(registrations))
	}
	return registrations[0]
}

func TestFreePluginFrontendDescriptions(t *testing.T) {
	runFreePluginFrontendDescriptions(t, editionContractRoot(t))
}

// runFreePluginFrontendDescriptions 核对免费插件的前端简介确实写成「免费」口径。
// 母版必须有全部免费插件；派生项目只核对它实际带进树里的那些。
func runFreePluginFrontendDescriptions(t *testing.T, root string) {
	t.Helper()
	master := isFrameworkMaster(root)
	for id := range freePluginIDs {
		if !master {
			if _, err := os.Stat(filepath.Join(root, "plugins", id, plugin.ManifestFile)); err != nil {
				continue
			}
		}
		for _, lang := range []string{"zh-CN", "en-US"} {
			t.Run(id+"/"+lang, func(t *testing.T) {
				path := filepath.Join(root, "web", "src", "plugins", id, "locales", lang+".json")
				raw, err := os.ReadFile(path)
				if err != nil {
					t.Fatal(err)
				}
				var messages map[string]map[string]any
				if err := json.Unmarshal(raw, &messages); err != nil {
					t.Fatal(err)
				}
				key := "description"
				if id == "maps" {
					key = "desc"
				}
				desc, _ := messages["plugin_"+id][key].(string)
				if lang == "zh-CN" {
					if !strings.Contains(desc, "免费") || strings.Contains(desc, "付费") {
						t.Errorf("description does not describe the free tier: %s", desc)
					}
				} else if !strings.HasPrefix(strings.ToLower(desc), "free ") || strings.Contains(strings.ToLower(desc), "paid") {
					t.Errorf("description does not describe the free tier: %s", desc)
				}
			})
		}
	}
}

// TestDerivedProjectCarryingSomeFreePlugins 是上面两处「母版专属断言」的负控制：
// 合成一棵只带 totp 的派生树（模块名不是 nexadmin），两条闸都不许报「缺插件」。
// 删掉那两处 !master 守卫，这条用例会红在「free plugin crm must have both…」——
// 派生项目默认只点名带几个插件，原来的写法让它的 go test ./plugins 天生红。
func TestDerivedProjectCarryingSomeFreePlugins(t *testing.T) {
	root := t.TempDir()
	write := func(rel, body string) {
		t.Helper()
		path := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("go.mod", "module acme.cn/app\n\ngo 1.24\n")
	write("plugins/totp/plugin.yaml", "id: totp\nname: 两步验证\nversion: 1.0.0\nedition: free\ndescription: 免费的 TOTP 两步验证\n")
	write("plugins/totp/plugin.go", `package totp

import "nexadmin/core/plugin"

func init() {
	plugin.Register(plugin.Plugin{ID: "totp", Name: "两步验证", Version: "1.0.0", Edition: plugin.Free})
}
`)
	write("config/plugin-market.yaml", "plugins:\n"+
		"  - id: totp\n"+
		"    name: 两步验证\n"+
		"    description: 免费的 TOTP 两步验证\n"+
		"    category: security\n"+
		"    edition: free\n"+
		"    version: 1.0.0\n"+
		"    repo: acme/nexadmin-plugin-totp\n"+
		"    price: 免费\n"+
		"    i18n:\n"+
		"      en-US:\n"+
		"        description: Free TOTP two-factor sign-in\n")
	write("web/src/plugins/totp/locales/zh-CN.json", `{"plugin_totp":{"description":"免费的 TOTP 两步验证"}}`)
	write("web/src/plugins/totp/locales/en-US.json", `{"plugin_totp":{"description":"Free TOTP two-factor sign-in"}}`)

	runEditionContract(t, root)
	runFreePluginFrontendDescriptions(t, root)
}
