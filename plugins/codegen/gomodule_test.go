package codegen

import (
	"os"
	"path/filepath"
	"testing"
)

// 「Go 模块路径」留空时必须落到本项目 go.mod 的 module 上：
// 写死 nexadmin 会让派生项目生成的代码 import 一个根本不存在的包。
func TestBuildSpecGoModuleFromProjectGoMod(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module example.com/shop\n\ngo 1.25.0\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("NEXADMIN_ROOT", "")
	t.Chdir(dir)

	a, err := analyze("demo_item", richColumns())
	if err != nil {
		t.Fatal(err)
	}
	s, err := buildSpec(a, "", "demo", dictCols())
	if err != nil {
		t.Fatal(err)
	}
	if s.GoModule != "example.com/shop" {
		t.Errorf("留空应取本项目 go.mod 的 module，实得 %q", s.GoModule)
	}
}

// NEXADMIN_ROOT 指向的项目优先于工作目录；带引号的 module 行也要能读。
func TestDetectGoModulePrefersRootEnvAndQuotes(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module \"example.com/quoted\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("NEXADMIN_ROOT", dir)
	if got := detectGoModule(); got != "example.com/quoted" {
		t.Errorf("实得 %q", got)
	}
}
