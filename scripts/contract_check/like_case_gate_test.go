package main

// like_case_gate_test.go —— 全仓「LIKE 大小写折叠」结构门禁。
//
// 背景：PostgreSQL 的 LIKE 区分大小写，MySQL/MariaDB 在本项目里的列排序规则不区分。
// 任何匹配「人手输入关键字」的 `col LIKE ?`，一旦用户在搜索框打了大写，PG 部署上会
// 静默返回 0 行。项目规范：这类筛选两侧都要折叠 —— LOWER(col) LIKE LOWER(?)。
//
// 本门禁纯静态扫描（不连库、不起服务、不 import 业务包）：走 modules/、core/、cmd/、
// pkg/ 下所有非测试 .go，用 go/ast 取字符串字面量（注释、Go 标识符如 escapeLike 天然
// 不进视野），找出 SQL 形状的 LIKE 条件，要求列侧（同窗口内、且位于该 LIKE 之前）出现
// LOWER(。plugins/ 不在范围内——那一半由另一条门禁独立负责，此处重复扫会撞车。
//
// 判定面刻意略宽于「GORM 条件实参」：凡形如 `LIKE ?` / `LIKE '...'` / `LIKE LOWER(`
// 的字面量都计入，包括脚手架模板与常量——宁可多拦给人解释，不能漏放真人筛选。
// 误拦由下方 likeGateExemptions 逐条化解：每条必须写明为何「构造上大小写恒定」，
// 只允许登记机器写入的值（数字物化路径、小写表名等），不允许登记人打的搜索框。

import (
	"bytes"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// likeGateDirs 是扫描范围（相对仓库根）。plugins/ 明确排除，见文件头注释。
var likeGateDirs = []string{"modules", "core", "cmd", "pkg"}

// likeGateExemption 是一条豁免：整文件生效，Reason 必填且说明大小写恒定的构造性理由。
type likeGateExemption struct {
	File   string // 相对仓库根、正斜杠
	Reason string // 为什么该文件里的 LIKE 值构造上大小写恒定（不是人手输入）
}

// likeGateExemptions 是显式豁免清单（数据，不是静默跳过；跑完后未命中的条目会记日志防过期）。
var likeGateExemptions = []likeGateExemption{
	{"modules/system/dept/service.go", "path 是机器写入的数字物化路径（/1/4/…），匹配值不来自人手输入，大小写天然恒定"},
	{"modules/system/dept/scope.go", "同上：sys_dept.path 数字物化路径前缀匹配，非关键字筛选"},
	{"modules/system/user/scope.go", "数据范围子查询匹配 sys_dept.path 数字物化路径，非关键字筛选"},
	{"core/datascope/datascope.go", "同上：框架内核按 dept 数字物化路径取子树 id，非关键字筛选"},
	{"core/plugin/market/market.go", "匹配的是框架建的 plg_<id>_ 表名（id 为小写插件标识），来自数据库目录而非用户输入"},
	{"cmd/nexadmin/add_spec.go", "脚手架代码生成模板：拼出的片段写进派生项目源码，本仓库不执行该条件；折叠策略归 codegen 与派生项目"},
}

// likeGateOccurRe 找 SQL 形状的 LIKE：后接 ?、LOWER( 或内联引号模式。
// 散文里的 "looks like a" 因缺少这些后继而被排除；Go 标识符（escapeLike 等）
// 由 AST 只取字符串字面量来排除。
var likeGateOccurRe = regexp.MustCompile(`(?i)\bLIKE\b\s*(?:\?|LOWER\s*\(|')`)

// likeGateCutRe 是列侧操作数的回溯切断点：上一个条件的 ?、布尔词 or/and、语句分隔 ; 。
var likeGateCutRe = regexp.MustCompile(`(?i)[?]|\bor\b|\band\b|[;]`)

type likeGateFinding struct {
	File    string `json:"file"`
	Line    int    `json:"line"`
	Snippet string `json:"snippet"`
}

func (f likeGateFinding) String() string { return fmt.Sprintf("%s:%d", f.File, f.Line) }

// likeGateCheck 扫描 root 下 likeGateDirs 的全部非测试 .go 文件。
// 返回：未折叠清单、检出的 LIKE 条件总数、其中被豁免数、未命中的豁免文件（防过期）。
func likeGateCheck(root string, exemptions []likeGateExemption) (findings []likeGateFinding, inspected, exempted int, unused []string, err error) {
	why := make(map[string]string, len(exemptions))
	for _, e := range exemptions {
		if strings.TrimSpace(e.Reason) == "" {
			return nil, 0, 0, nil, fmt.Errorf("豁免条目 %s 缺 Reason：豁免必须声明为何大小写恒定", e.File)
		}
		why[filepath.ToSlash(filepath.Clean(e.File))] = e.Reason
	}
	used := map[string]bool{}
	fset := token.NewFileSet()

	for _, dir := range likeGateDirs {
		base := filepath.Join(root, filepath.FromSlash(dir))
		if st, e := os.Stat(base); e != nil || !st.IsDir() {
			continue
		}
		walkErr := filepath.WalkDir(base, func(p string, d fs.DirEntry, err error) error {
			if err != nil {
				return nil // 与 errkeys.go 同口径：单点不可读不中断整扫
			}
			rel, rerr := filepath.Rel(root, p)
			if rerr != nil {
				return nil
			}
			rel = filepath.ToSlash(rel)
			if d.IsDir() {
				if d.Name() == "testdata" || d.Name() == "node_modules" || strings.HasPrefix(d.Name(), ".") {
					return fs.SkipDir
				}
				return nil
			}
			if !strings.HasSuffix(rel, ".go") || strings.HasSuffix(rel, "_test.go") {
				return nil
			}
			src, rerr := os.ReadFile(p)
			if rerr != nil {
				return nil
			}
			af, perr := parser.ParseFile(fset, p, src, parser.SkipObjectResolution)
			if perr != nil {
				findings = append(findings, likeGateFinding{File: rel, Snippet: "解析失败: " + perr.Error()})
				return nil
			}
			tf := fset.File(af.Pos())
			ast.Inspect(af, func(n ast.Node) bool {
				bl, ok := n.(*ast.BasicLit)
				if !ok || bl.Kind != token.STRING || tf == nil {
					return true
				}
				off := tf.Offset(bl.Pos())
				if off < 0 || off+len(bl.Value) > len(src) || string(src[off:off+len(bl.Value)]) != bl.Value {
					return true
				}
				for _, m := range likeGateOccurRe.FindAllStringSubmatchIndex(bl.Value, -1) {
					inspected++
					if _, hit := why[rel]; hit {
						exempted++
						used[rel] = true
						continue
					}
					occ := off + m[0]
					// 列侧操作数窗口：字面量所在行行首 → LIKE；再回溯到最近的切断点。
					lineStart := bytes.LastIndexByte(src[:occ], '\n') + 1
					pre := src[lineStart:occ]
					cut := 0
					if idx := likeGateCutRe.FindAllIndex(pre, -1); len(idx) > 0 {
						cut = idx[len(idx)-1][1]
					}
					if strings.Contains(strings.ToLower(string(pre[cut:])), "lower(") {
						continue
					}
					lineEnd := bytes.IndexByte(src[occ:], '\n')
					end := len(src)
					if lineEnd >= 0 {
						end = occ + lineEnd
					}
					findings = append(findings, likeGateFinding{
						File:    rel,
						Line:    bytes.Count(src[:occ], []byte("\n")) + 1,
						Snippet: strings.TrimSpace(string(src[lineStart:end])),
					})
				}
				return true
			})
			return nil
		})
		if walkErr != nil {
			return nil, inspected, exempted, nil, walkErr
		}
	}

	for _, e := range exemptions {
		if !used[filepath.ToSlash(filepath.Clean(e.File))] {
			unused = append(unused, e.File)
		}
	}
	sort.Slice(findings, func(i, j int) bool {
		if findings[i].File != findings[j].File {
			return findings[i].File < findings[j].File
		}
		return findings[i].Line < findings[j].Line
	})
	return findings, inspected, exempted, unused, nil
}

// —— 门禁自身的 fixture 单元测试（负控制，保证门禁非空转）——

func TestLikeCaseFoldGateDetectsBareLike(t *testing.T) {
	root := writeTree(t, map[string]string{
		"modules/bad/bad.go": "package bad\n\nimport \"gorm.io/gorm\"\n\nfunc list(db *gorm.DB, kw string) *gorm.DB {\n\tlike := \"%\" + kw + \"%\"\n\treturn db.Where(\"name LIKE ?\", like)\n}\n",
		// 折叠过的、两侧折叠的、以及散文里的 like 都不该被拦。
		"modules/good/good.go": "package good\n\nvar a = \"LOWER(name) LIKE LOWER(?)\"\nvar b = \"(LOWER(x) LIKE ? OR LOWER(y) LIKE ?)\"\nvar c = \"it looks like a plain sentence?\"\n",
	})
	findings, inspected, exempted, _, err := likeGateCheck(root, nil)
	if err != nil {
		t.Fatal(err)
	}
	// bad.go 1 处 + good.go 3 处 SQL 形状（c 的散文不算），共 4 处。
	if inspected != 4 || exempted != 0 {
		t.Fatalf("计数不对: inspected=%d exempted=%d", inspected, exempted)
	}
	if len(findings) != 1 || findings[0].File != "modules/bad/bad.go" || findings[0].Line != 7 {
		t.Fatalf("应且只应拦到裸 LIKE 那处，实际: %+v", findings)
	}
	if !strings.Contains(findings[0].Snippet, "name LIKE ?") {
		t.Errorf("片段应含原文: %q", findings[0].Snippet)
	}
}

func TestLikeCaseFoldGateConcatFoldedPasses(t *testing.T) {
	// dict/log 的真实写法：LOWER( 在拼接的另一个片段里，窗口按整行回溯仍应判定为已折叠。
	root := writeTree(t, map[string]string{
		"modules/ok/ok.go": "package ok\n\nfunc c(col string) string { return \"LOWER(\" + col + \") LIKE ?\" }\n",
	})
	findings, inspected, _, _, err := likeGateCheck(root, nil)
	if err != nil {
		t.Fatal(err)
	}
	if inspected != 1 || len(findings) != 0 {
		t.Fatalf("拼接折叠应放行: inspected=%d findings=%+v", inspected, findings)
	}
}

func TestLikeCaseFoldGateExemptionWorks(t *testing.T) {
	root := writeTree(t, map[string]string{
		"modules/deptish/deptish.go": "package deptish\n\nvar cond = \"path LIKE ?\"\n",
	})
	before, inspected, _, _, err := likeGateCheck(root, nil)
	if err != nil || inspected != 1 || len(before) != 1 {
		t.Fatalf("未豁免前应拦 1 处: %+v (inspected=%d, err=%v)", before, inspected, err)
	}
	// 豁免必须带 Reason，否则门禁拒绝启动（防止变成静默跳过）。
	if _, _, _, _, err := likeGateCheck(root, []likeGateExemption{{File: "modules/deptish/deptish.go"}}); err == nil {
		t.Fatal("空 Reason 的豁免应被拒绝")
	}
	after, inspected, exempted, unused, err := likeGateCheck(root, []likeGateExemption{
		{File: "modules/deptish/deptish.go", Reason: "数字物化路径，机器写入，大小写恒定"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(after) != 0 || exempted != 1 {
		t.Fatalf("豁免后应放行且计数命中: findings=%+v exempted=%d", after, exempted)
	}
	// 命中过的豁免不该被报为过期。
	if len(unused) != 0 {
		t.Errorf("unused 应为空: %v", unused)
	}
}

func TestLikeCaseFoldGateEmptyScanFails(t *testing.T) {
	// 空扫不是通过：门禁对真实仓库跑之前先断言这条语义。
	root := writeTree(t, map[string]string{"modules/quiet/quiet.go": "package quiet\n\nvar x = 1\n"})
	_, inspected, _, _, err := likeGateCheck(root, nil)
	if err != nil {
		t.Fatal(err)
	}
	if inspected != 0 {
		t.Fatalf("无 LIKE 的 fixture 应计 0，实际 %d", inspected)
	}
}

// —— 对真实仓库跑的门禁。跑空即失败：inspected==0 视为扫描失效，而非绿灯。 ——

func TestLikeCaseFoldGateRepo(t *testing.T) {
	root := findRoot()
	findings, inspected, exempted, unused, err := likeGateCheck(root, likeGateExemptions)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("LIKE 折叠门禁: 检出 %d 处 LIKE 条件，其中豁免 %d 处（构造上大小写恒定），违规 %d 处", inspected, exempted, len(findings))
	if inspected == 0 {
		t.Fatalf("门禁没扫到任何 LIKE 条件（root=%s）：扫描路径失效，空扫不算通过", root)
	}
	// 下限防「扫描悄悄塌掉」：当前在范围内的折叠点约 50+，塌到 20 以下多半是接错目录。
	if inspected < 20 {
		t.Errorf("检出 LIKE 条件数异常偏低（%d），怀疑扫描范围接错（root=%s）", inspected, root)
	}
	for _, f := range findings {
		t.Errorf("%s 的 LIKE 条件列侧没做 LOWER() 折叠: %s（若确属构造上大小写恒定，往 likeGateExemptions 加一条并写明理由）", f, f.Snippet)
	}
	for _, u := range unused {
		t.Logf("豁免条目 %s 本轮未命中任何 LIKE，注意是否已过期", u)
	}
}
