package assistant

import (
	"go/parser"
	"go/token"
	"strconv"
	"strings"
	"testing"
)

// 插件必须只依赖 core/pkg：import 内置模块会让插件无法独立交付，也会在停用模块时连带失败。
func TestPluginImportsOnlyCoreAndPkg(t *testing.T) {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "plugin.go", nil, parser.ImportsOnly)
	if err != nil {
		t.Fatal(err)
	}
	for _, imp := range file.Imports {
		path, err := strconv.Unquote(imp.Path.Value)
		if err != nil {
			t.Fatal(err)
		}
		if strings.HasPrefix(path, "nexadmin/modules/") {
			t.Errorf("assistant imports built-in module %q; add a core factory instead", path)
		}
	}
}
