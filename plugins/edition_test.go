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
	"ldap": true, "maps": true, "crm": true,
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
	root := editionContractRoot(t)
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
	for _, entry := range entries {
		listed[entry.ID] = true
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
	root := editionContractRoot(t)
	for id := range freePluginIDs {
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
