package main

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

const curGoMod = `module example.com/demo

go 1.24.0

require (
	github.com/a/keep v1.0.0
	github.com/a/up v1.2.0
	github.com/a/same v1.0.0
	github.com/a/newer v1.5.0
)

require github.com/a/ind v0.1.0 // indirect

replace github.com/a/up => ../local-up
`

const newGoMod = `module nexadmin

go 1.25.0

require (
	github.com/a/up v1.3.0
	github.com/a/same v1.0.0
	github.com/a/newer v1.4.0
	github.com/b/added v2.0.1+incompatible
)

replace github.com/a/up => ../ignored
replace github.com/c/need => github.com/c/fork v1.0.0
replace github.com/d/local => ../d
`

func TestMergeGoMod(t *testing.T) {
	out, rep, changed, err := mergeGoMod([]byte(curGoMod), []byte(newGoMod))
	if err != nil || !changed {
		t.Fatal(err, changed)
	}
	got := string(out)
	for _, want := range []string{
		"module example.com/demo",                               // module 行以当前工程为准
		"go 1.25.0",                                             // go 版本取较高者
		"github.com/a/keep v1.0.0",                              // 二开独有保留
		"github.com/a/ind v0.1.0",                               // 独有的间接依赖保留
		"github.com/a/up v1.3.0",                                // 新包更高 -> 升级
		"github.com/a/newer v1.5.0",                             // 当前更高 -> 不降级
		"github.com/b/added v2.0.1+incompatible",                // 新增
		"replace github.com/a/up => ../local-up",                // replace 以当前为准
		"replace github.com/c/need => github.com/c/fork v1.0.0", // 补入框架必需 replace
	} {
		if !strings.Contains(got, want) {
			t.Errorf("缺少 %q:\n%s", want, got)
		}
	}
	for _, bad := range []string{"module nexadmin", "../ignored", "../d", "github.com/a/newer v1.4.0", "github.com/a/up v1.2.0"} {
		if strings.Contains(got, bad) {
			t.Errorf("不应出现 %q:\n%s", bad, got)
		}
	}
	joined := strings.Join(rep, "\n")
	for _, want := range []string{"+ require github.com/b/added", "↑ require github.com/a/up v1.2.0 -> v1.3.0", "↑ go 1.24.0 -> 1.25.0",
		"= 保留二开独有依赖 github.com/a/keep v1.0.0", "+ replace github.com/c/need", "忽略新包里指向本地路径的 replace"} {
		if !strings.Contains(joined, want) {
			t.Errorf("清单缺少 %q:\n%s", want, joined)
		}
	}
	// 再合并一次应无变化（幂等），且原样返回。
	out2, _, changed2, err := mergeGoMod(out, []byte(newGoMod))
	if err != nil || changed2 || !bytes.Equal(out2, out) {
		t.Errorf("二次合并应无变化: %v %v", err, changed2)
	}
}

func TestMergeGoModKeepsHigherGoVersion(t *testing.T) {
	cur := "module m\n\ngo 1.26.0\n\nrequire github.com/a/x v1.0.0\n"
	nw := "module n\n\ngo 1.25.0\n\nrequire github.com/a/x v1.0.0\n"
	out, _, changed, err := mergeGoMod([]byte(cur), []byte(nw))
	if err != nil || changed || string(out) != cur {
		t.Errorf("当前 go 版本更高应保持不变: %v %v\n%s", err, changed, out)
	}
	if !goVerLess("1.25rc1", "1.25.0") || goVerLess("1.25.0", "1.25rc1") || !goVerLess("1.9.9", "1.10.0") {
		t.Error("goVerLess 比较有误")
	}
}

func TestMergeGoSum(t *testing.T) {
	cur := "github.com/b/y v1.10.0 h1:b=\ngithub.com/b/y v1.10.0/go.mod h1:bm=\ngithub.com/a/x v1.0.0 h1:a=\n"
	nw := "github.com/a/x v1.0.0 h1:a=\ngithub.com/b/y v1.9.0 h1:b9=\ngithub.com/b/y v1.9.0/go.mod h1:b9m=\ngithub.com/c/z v0.1.0 h1:c=\n\n"
	out, rep, changed, err := mergeGoSum([]byte(cur), []byte(nw))
	if err != nil || !changed {
		t.Fatal(err, changed)
	}
	want := "github.com/a/x v1.0.0 h1:a=\ngithub.com/b/y v1.9.0 h1:b9=\ngithub.com/b/y v1.9.0/go.mod h1:b9m=\n" +
		"github.com/b/y v1.10.0 h1:b=\ngithub.com/b/y v1.10.0/go.mod h1:bm=\ngithub.com/c/z v0.1.0 h1:c=\n"
	if string(out) != want {
		t.Errorf("go.sum 并集/排序不对:\n%s\nwant:\n%s", out, want)
	}
	if len(rep) == 0 || !strings.Contains(rep[0], "+ 3 行") {
		t.Errorf("清单: %v", rep)
	}
	// 新包的行都已存在 -> 不动（即使顺序不同）。
	same, _, changed2, _ := mergeGoSum([]byte(want), []byte("github.com/c/z v0.1.0 h1:c=\ngithub.com/a/x v1.0.0 h1:a=\n"))
	if changed2 || string(same) != want {
		t.Error("子集合并不应改写")
	}
	// 同键不同哈希：两行保留并告警。
	_, rep3, _, _ := mergeGoSum([]byte("github.com/a/x v1.0.0 h1:a=\n"), []byte("github.com/a/x v1.0.0 h1:DIFF=\n"))
	if !strings.Contains(strings.Join(rep3, "\n"), "冲突") {
		t.Errorf("应提示哈希冲突: %v", rep3)
	}
}

const curPkg = `{
  "version": "3.0.0",
  "name": "my-web",
  "private": true,
  "scripts": {
    "dev": "vite",
    "mine": "echo mine",
    "build": "tsc"
  },
  "dependencies": {
    "zod": "3.0.0",
    "mine-dep": "1.0.0",
    "react": "18.0.0"
  },
  "devDependencies": {
    "vite": "5.0.0"
  }
}
`

const newPkg = `{
  "name": "nexadmin-web",
  "private": false,
  "version": "9.9.9",
  "type": "module",
  "scripts": {
    "build": "tsc --noEmit && vite build",
    "dev": "vite",
    "lint": "eslint src"
  },
  "engines": {"node": ">=20"},
  "dependencies": {
    "zod": "3.25.76",
    "react": "18.0.0",
    "zustand": "5.0.0"
  },
  "devDependencies": {
    "vite": "5.4.21",
    "vitest": "2.0.0"
  }
}
`

func TestMergePackageJSON(t *testing.T) {
	out, rep, changed, err := mergePackageJSON([]byte(curPkg), []byte(newPkg))
	if err != nil || !changed {
		t.Fatal(err, changed)
	}
	var m map[string]any
	if err := json.Unmarshal(out, &m); err != nil {
		t.Fatalf("输出不是合法 JSON: %v\n%s", err, out)
	}
	if m["name"] != "my-web" || m["version"] != "3.0.0" || m["private"] != true {
		t.Errorf("标识字段应以当前工程为准: %v %v %v", m["name"], m["version"], m["private"])
	}
	deps := m["dependencies"].(map[string]any)
	if deps["zod"] != "3.25.76" || deps["zustand"] != "5.0.0" || deps["mine-dep"] != "1.0.0" || deps["react"] != "18.0.0" {
		t.Errorf("dependencies 合并有误: %v", deps)
	}
	if dev := m["devDependencies"].(map[string]any); dev["vite"] != "5.4.21" || dev["vitest"] != "2.0.0" {
		t.Errorf("devDependencies: %v", dev)
	}
	sc := m["scripts"].(map[string]any)
	if sc["build"] != "tsc --noEmit && vite build" || sc["mine"] != "echo mine" || sc["lint"] != "eslint src" {
		t.Errorf("scripts: %v", sc)
	}
	if m["type"] != "module" || m["engines"].(map[string]any)["node"] != ">=20" {
		t.Errorf("新增字段缺失: %v", m)
	}
	s := string(out)
	// 键序稳定：顶层沿用当前顺序（version, name, private, scripts, dependencies, devDependencies），新键追加；依赖段字母序。
	order := []string{`"version"`, `"name"`, `"private"`, `"scripts"`, `"dependencies"`, `"devDependencies"`, `"type"`, `"engines"`}
	last := -1
	for _, k := range order {
		i := strings.Index(s, k)
		if i < last {
			t.Errorf("顶层键序不稳定，%s 位置不对:\n%s", k, s)
		}
		last = i
	}
	if i, j, k := strings.Index(s, `"mine-dep"`), strings.Index(s, `"react"`), strings.Index(s, `"zod"`); !(i < j && j < k) {
		t.Errorf("dependencies 应按字母序:\n%s", s)
	}
	if !strings.Contains(s, "\n  \"scripts\": {\n    \"dev\"") || !strings.HasSuffix(s, "}\n") {
		t.Errorf("应 2 空格缩进并以换行结尾:\n%s", s)
	}
	joined := strings.Join(rep, "\n")
	for _, want := range []string{"↑ dependencies.zod: 3.0.0 -> 3.25.76", "+ dependencies.zustand", "= 保留二开独有 dependencies: mine-dep", "= 保留二开独有 scripts: mine"} {
		if !strings.Contains(joined, want) {
			t.Errorf("清单缺少 %q:\n%s", want, joined)
		}
	}
	// 再合并一次应原样返回（幂等、键序稳定）。
	out2, _, changed2, err := mergePackageJSON(out, []byte(newPkg))
	if err != nil || changed2 || !bytes.Equal(out2, out) {
		t.Errorf("二次合并应无变化: %v %v", err, changed2)
	}
}

func TestMergePackageJSONNoChangeKeepsBytes(t *testing.T) {
	nw := `{"name":"fw","dependencies":{"react":"18.0.0"}}`
	out, _, changed, err := mergePackageJSON([]byte(curPkg), []byte(nw))
	if err != nil || changed || string(out) != curPkg {
		t.Errorf("没有实际差异时应原样返回: %v %v", err, changed)
	}
	if _, _, _, err := mergePackageJSON([]byte("{bad"), []byte(nw)); err == nil {
		t.Error("非法 JSON 应报错")
	}
}
