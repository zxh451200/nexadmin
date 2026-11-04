package plugins_test

// 大小写折叠闸（LIKE 的 PG 方言闸）。
//
// 为什么要这条闸：MySQL/MariaDB 的 utf8mb4 列排序规则不区分大小写，`col LIKE '%Abc%'`
// 天然能命中；PostgreSQL 的 LIKE 区分大小写，同一句在 PG 上会静默返回 0 行，用户看到的是
// 「搜不到」。三引擎都是受支持引擎，所以任何带 LIKE 的关键词筛选都必须把**两侧**都折叠成
// `LOWER(<列>) LIKE LOWER(?)`（不能用 ILIKE —— MySQL/MariaDB 没有 ILIKE）。
//
// 本闸扫 plugins/ 下每个非测试 .go 文件里的 Where/Or/Not 调用：只要 SQL 片段里出现 LIKE，
// 每个 LIKE 的列侧必须被 LOWER(...) 包着、模式侧必须以 LOWER( 开头。允许显式豁免：在调用行
// 或其前三行写注释 `nexadmin:like-exempt`，并说明为什么大小写对该站点无意义
// （例：匹配的是同源常量、值由构造保证大小写一致，不是人随手打的关键字）。
//
// 已知盲区（有意不在本闸取景）：把 LIKE 片段拼在 Where 之外再传变量的写法，例如
// plugins/report/query.go 的 filterSQL —— 它由 plugins/report/tenant_test.go 里的整句 SQL
// 断言盯住折叠形状。

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

const likeExemptMarker = "nexadmin:like-exempt"

// TestPluginsLikeFiltersFoldCase 断言 plugins/ 的每个 LIKE 筛选都做了大小写折叠。
func TestPluginsLikeFiltersFoldCase(t *testing.T) {
	root := "."
	fset := token.NewFileSet()

	var files []string
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			switch d.Name() {
			case "testdata", "node_modules", ".git", "dist", "build":
				return filepath.SkipDir
			}
			return nil
		}
		name := d.Name()
		if !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			return nil
		}
		files = append(files, path)
		return nil
	})
	if err != nil {
		t.Fatalf("walk plugins: %v", err)
	}
	if len(files) == 0 {
		t.Fatalf("闸空转：在 %s 下没找到任何非测试 .go 文件（应覆盖整个 plugins 目录）", root)
	}

	checked := 0
	for _, path := range files {
		src, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		f, err := parser.ParseFile(fset, path, src, parser.ParseComments)
		if err != nil {
			t.Errorf("parse %s: %v", path, err)
			continue
		}
		lines := strings.Split(string(src), "\n")
		ast.Inspect(f, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			sel, ok := call.Fun.(*ast.SelectorExpr)
			if !ok || !isFilterMethod(sel.Sel.Name) {
				return true
			}
			for _, arg := range call.Args {
				sql, ok := sqlText(arg)
				if !ok || !containsKeyword(sql, "LIKE") {
					continue
				}
				checked++
				if bad := unfoldedLike(sql); bad != "" {
					pos := fset.Position(call.Lparen)
					if hasExemption(lines, pos.Line) {
						continue
					}
					t.Errorf("%s:%d: %s(...) 里的 LIKE 没有折叠大小写：%s\n"+
						"PG 的 LIKE 区分大小写，这种写法在 PostgreSQL 部署上会「搜不到」；"+
						"改成 LOWER(<列>) LIKE LOWER(?)，或确有价值不受大小写影响时加 %s 注释豁免并写明理由",
						path, pos.Line, sel.Sel.Name, bad, likeExemptMarker)
				}
			}
			return true
		})
	}
	if checked == 0 {
		t.Fatalf("闸空转：扫了 %d 个文件却一个 LIKE 筛选都没看到，说明取景条件失效", len(files))
	}
	t.Logf("已检查 %d 处含 LIKE 的筛选片段（覆盖 %d 个非测试文件）", checked, len(files))
}

// isFilterMethod 只认 GORM 里传 SQL 片段的筛选链式方法。
func isFilterMethod(name string) bool {
	switch name {
	case "Where", "Or", "Not":
		return true
	}
	return false
}

// sqlText 把一个实参求值成 SQL 文本：字符串字面量取原值，非字面量（列名变量等）替换成
// EXPR 占位符，好让拼接出来的片段也能按括号配对判折叠。整体不是字符串表达式的返回 false。
func sqlText(e ast.Expr) (string, bool) {
	switch v := e.(type) {
	case *ast.BasicLit:
		if v.Kind != token.STRING {
			return "", false
		}
		s, err := strconv.Unquote(v.Value)
		if err != nil {
			return "", false
		}
		return s, true
	case *ast.BinaryExpr:
		if v.Op != token.ADD {
			return "", false
		}
		l, ok := sqlText(v.X)
		if !ok {
			return "", false
		}
		r, ok := sqlText(v.Y)
		if !ok {
			return "", false
		}
		return l + r, true
	case *ast.ParenExpr:
		return sqlText(v.X)
	default:
		// 变量、函数调用、选择器等非字面量：用中性占位符表示，保证括号配对仍可判定。
		return "EXPR", true
	}
}

// containsKeyword 判断 SQL 文本里是否出现独立的 LIKE 关键字（不区分引号内外）。
func containsKeyword(sql, kw string) bool {
	for i := 0; i+len(kw) <= len(sql); {
		if strings.EqualFold(sql[i:i+len(kw)], kw) {
			beforeOK := i == 0 || !isIdentByte(sql[i-1])
			end := i + len(kw)
			afterOK := end == len(sql) || !isIdentByte(sql[end])
			if beforeOK && afterOK {
				return true
			}
			i = end
			continue
		}
		i++
	}
	return false
}

func isIdentByte(b byte) bool {
	return b == '_' || (b >= '0' && b <= '9') || (b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z')
}

// unfoldedLike 返回第一处没有两侧折叠的 LIKE 片段；全部折叠则返回空串。
func unfoldedLike(sql string) string {
	for i := 0; i+4 <= len(sql); {
		if !strings.EqualFold(sql[i:i+4], "LIKE") || !isLikeAt(sql, i) {
			i++
			continue
		}
		if why := unfoldReason(sql, i); why != "" {
			return why
		}
		i += 4
	}
	return ""
}

// isLikeAt 确认第 i 处的 LIKE 前后不是标识符字符（排除 "LIKEly" 这类误命中）。
func isLikeAt(sql string, i int) bool {
	beforeOK := i == 0 || !isIdentByte(sql[i-1])
	end := i + 4
	afterOK := end == len(sql) || !isIdentByte(sql[end])
	return beforeOK && afterOK
}

// unfoldReason 检查第 i 个 LIKE 的列侧是否包在 LOWER(...) 里、模式侧是否 LOWER( 开头。
func unfoldReason(sql string, i int) string {
	left := strings.TrimRight(sql[:i], " \t\n")
	if !strings.HasSuffix(left, ")") {
		return "列侧没有 LOWER(...) 包裹（找到 " + snippet(sql, i) + "）"
	}
	open := matchingOpenParen(left)
	if open < 5 || !strings.EqualFold(left[open-5:open], "LOWER") {
		return "列侧没有 LOWER(...) 包裹（找到 " + snippet(sql, i) + "）"
	}
	right := strings.TrimLeft(sql[i+4:], " \t\n")
	if len(right) < 6 || !strings.EqualFold(right[:6], "LOWER(") {
		return "模式侧没有 LOWER(...) 包裹（找到 " + snippet(sql, i) + "）"
	}
	return ""
}

// matchingOpenParen 从末尾的 ')' 反向找配对的 '('，找不到返回 -1。
func matchingOpenParen(s string) int {
	depth := 0
	for i := len(s) - 1; i >= 0; i-- {
		switch s[i] {
		case ')':
			depth++
		case '(':
			depth--
			if depth == 0 {
				return i
			}
		}
	}
	return -1
}

func snippet(sql string, i int) string {
	lo := i - 24
	if lo < 0 {
		lo = 0
	}
	hi := i + 24
	if hi > len(sql) {
		hi = len(sql)
	}
	return strings.TrimSpace(sql[lo:hi])
}

func hasExemption(lines []string, callLine int) bool {
	for l := callLine - 3; l <= callLine; l++ {
		if l < 1 || l > len(lines) {
			continue
		}
		if strings.Contains(lines[l-1], likeExemptMarker) {
			return true
		}
	}
	return false
}
