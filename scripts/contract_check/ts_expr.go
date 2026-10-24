package main

import (
	"regexp"
	"strings"
)

// tsParser 在去注释后的源码上解析“字符串表达式”：字面量、模板字符串、文件内常量、+ 拼接。
type tsParser struct {
	b      []byte
	consts map[string]string
	lax    bool // 不要求表达式后面紧跟 , ) 等（用于无分号风格的 const 声明）
}

var identRe = regexp.MustCompile(`^[A-Za-z_$][\w$]*(?:\.[A-Za-z_$][\w$]*)*`)

// strExpr 是解析结果：val 里无法确定的部分是 ":param"；ok=false 表示整体取不到字面信息。
type strExpr struct {
	val string
	ok  bool
	end int // 表达式结束后的下标
}

// parse 从 i 开始解析一个字符串表达式（到逗号 / 右括号为止）。
func (p *tsParser) parse(i int) strExpr {
	var sb strings.Builder
	known := false
	i = skipWS(p.b, i)
	for {
		v, k, end := p.operand(i)
		sb.WriteString(v)
		known = known || k
		i = skipWS(p.b, end)
		if i+1 < len(p.b) && p.b[i] == '+' && p.b[i+1] != '+' && p.b[i+1] != '=' {
			i = skipWS(p.b, i+1)
			continue
		}
		break
	}
	ok := known
	if !p.lax && i < len(p.b) && p.b[i] != ',' && p.b[i] != ')' && p.b[i] != '}' && p.b[i] != ']' {
		ok = false // 后面还有 ? : . 等运算（三元、方法调用），不当作字面路径
	}
	return strExpr{val: sb.String(), ok: ok, end: i}
}

// operand 解析一个操作数，返回 (文本, 是否含确定信息, 结束下标)。
func (p *tsParser) operand(i int) (string, bool, int) {
	if i >= len(p.b) {
		return ":param", false, i
	}
	switch c := p.b[i]; {
	case c == '\'' || c == '"':
		end := skipQuoted(p.b, i, c)
		return strings.ReplaceAll(string(p.b[i+1:max(i+1, end-1)]), `\'`, `'`), true, end
	case c == '`':
		end := skipTemplate(p.b, i)
		v, known := p.template(p.b[i+1 : max(i+1, end-1)])
		return v, known, end
	case c == '(' || c == '[' || c == '{':
		if e := matchClose(p.b, i); e > 0 {
			return ":param", false, e + 1
		}
	}
	if m := identRe.Find(p.b[i:]); m != nil {
		id := string(m)
		end := i + len(m)
		if v, ok := p.consts[id]; ok {
			return v, true, end
		}
		// 调用（foo(...)）整体吞掉
		if e := skipWS(p.b, end); e < len(p.b) && p.b[e] == '(' {
			if ce := matchClose(p.b, e); ce > 0 {
				return ":param", false, ce + 1
			}
		}
		return ":param", false, end
	}
	return ":param", false, i + 1
}

// template 展开模板字符串内容：${常量} 代入，VITE_API_BASE 之类视为空，其余记为 :param。
func (p *tsParser) template(inner []byte) (string, bool) {
	var sb strings.Builder
	known := false
	for i := 0; i < len(inner); i++ {
		if inner[i] == '\\' && i+1 < len(inner) {
			i++
			sb.WriteByte(inner[i])
			known = true
			continue
		}
		if inner[i] == '$' && i+1 < len(inner) && inner[i+1] == '{' {
			e := matchClose(inner, i+1)
			if e < 0 {
				sb.WriteString(":param")
				break
			}
			expr := strings.TrimSpace(string(inner[i+2 : e]))
			switch {
			case strings.Contains(expr, "API_BASE"):
				known = true
			case hasKey(p.consts, expr):
				sb.WriteString(p.consts[expr])
				known = true
			default:
				sb.WriteString(":param")
			}
			i = e
			continue
		}
		sb.WriteByte(inner[i])
		known = true
	}
	return sb.String(), known
}

func hasKey(m map[string]string, k string) bool { _, ok := m[k]; return ok }

var constDeclRe = regexp.MustCompile(`(?m)\bconst\s+([A-Za-z_$][\w$]*)\s*(?::\s*[A-Za-z_$][\w$<>\[\]| ]*)?=\s*`)

// collectConsts 收集文件内 const NAME = <字符串表达式>（多轮，常量可互相引用）。
func collectTSConsts(t *tsText) map[string]string {
	b := t.b
	consts := map[string]string{}
	p := &tsParser{b: b, consts: consts, lax: true}
	var ms [][]int
	for _, m := range constDeclRe.FindAllSubmatchIndex(b, -1) {
		if !t.inStr(m[0]) {
			ms = append(ms, m)
		}
	}
	for round := 0; round < 3; round++ {
		for _, m := range ms {
			name := string(b[m[2]:m[3]])
			start := m[1]
			if strings.HasPrefix(string(b[start:min(start+40, len(b))]), "import.meta.env.VITE_API_BASE") {
				consts[name] = "" // 形如 import.meta.env.VITE_API_BASE || '/api/v1'，视为 API 基址
				continue
			}
			if e := p.parse(start); e.ok && !strings.Contains(e.val, ":param") {
				consts[name] = e.val
			}
		}
	}
	return consts
}
