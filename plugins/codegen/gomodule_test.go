package codegen

import (
	"os"
	"path/filepath"
	"strings"
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
	got, err := detectGoModule()
	if err != nil {
		t.Fatal(err)
	}
	if got != "example.com/quoted" {
		t.Errorf("实得 %q", got)
	}
}

// 定位不到 go.mod 时必须当场报错，不能悄悄用 nexadmin 当前缀：
// 悄悄生成的代码要到编译才炸，报的还是「package nexadmin/... does not exist」。
func TestDetectGoModuleFailsWithoutGoMod(t *testing.T) {
	dir := t.TempDir()
	// 这个分支要求 temp 目录到盘根之间真的没有 go.mod；机器上若有（例如把 Go 工作区装在某个
	// module 里），向上找会得到答案，断言就不成立了——此时如实跳过，不改成放宽期望。
	for d := filepath.Clean(dir); ; {
		if _, err := os.Stat(filepath.Join(d, "go.mod")); err == nil {
			t.Skipf("%s 一带本来就有 go.mod，测不到「向上都找不到」的分支", d)
		}
		up := filepath.Dir(d)
		if up == d {
			break
		}
		d = up
	}
	t.Setenv("NEXADMIN_ROOT", dir)
	t.Chdir(dir)
	_, err := detectGoModule()
	if err == nil {
		t.Fatal("没有 go.mod 时不能默默返回一个前缀")
	}
	if !strings.Contains(err.Error(), "go.mod") {
		t.Errorf("报错要指向真正的原因（找不到 go.mod）: %v", err)
	}
}
