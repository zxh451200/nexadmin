package main

import (
	"sort"
	"strings"
)

// tsText 是去掉注释后的 TS/TSX 源码（长度与偏移不变，注释换成空格，便于换算行号）。
type tsText struct {
	b    []byte
	nl   []int  // 每个换行符的偏移
	mask []bool // 落在字符串文本里的位置
}

func newTS(src string) *tsText {
	b := []byte(src)
	scanCode(b, 0, false)
	t := &tsText{b: b, mask: maskStrings(b)}
	for i, c := range b {
		if c == '\n' {
			t.nl = append(t.nl, i)
		}
	}
	return t
}

// inStr 判断偏移是否落在字符串 / 模板字符串文本里（那里面的“调用”只是示例文本）。
func (t *tsText) inStr(off int) bool { return off >= 0 && off < len(t.mask) && t.mask[off] }

// line 返回偏移所在的行号（从 1 开始）。
func (t *tsText) line(off int) int { return sort.SearchInts(t.nl, off) + 1 }

// scanCode 扫描代码并把注释改成空格；stopAtBrace 时遇到未配对的 '}' 返回其下标（用于模板里的 ${}）。
func scanCode(b []byte, i int, stopAtBrace bool) int {
	depth := 0
	prev := byte(';')
	for i < len(b) {
		c := b[i]
		switch {
		case c == '/' && i+1 < len(b) && b[i+1] == '/':
			for i < len(b) && b[i] != '\n' {
				b[i] = ' '
				i++
			}
			continue
		case c == '/' && i+1 < len(b) && b[i+1] == '*':
			for i < len(b) && !(b[i] == '*' && i+1 < len(b) && b[i+1] == '/') {
				if b[i] != '\n' {
					b[i] = ' '
				}
				i++
			}
			if i+1 < len(b) {
				b[i], b[i+1] = ' ', ' '
				i += 2
			}
			continue
		case c == '/' && strings.IndexByte("(,=:[!&|?{};", prev) >= 0:
			i = skipRegex(b, i)
			prev = ')'
			continue
		case c == '\'' || c == '"':
			i = skipQuoted(b, i, c)
			prev = ')'
			continue
		case c == '`':
			i = skipTemplate(b, i)
			prev = ')'
			continue
		case c == '{':
			depth++
		case c == '}':
			if depth == 0 && stopAtBrace {
				return i
			}
			depth--
		}
		if c != ' ' && c != '\t' && c != '\n' && c != '\r' {
			prev = c
		}
		i++
	}
	return i
}

// skipQuoted 跳过 '...' 或 "..."，返回结束引号之后的下标；字符串不跨行（JSX 文本里的撇号不会吞掉后面的代码）。
func skipQuoted(b []byte, i int, q byte) int {
	for i++; i < len(b) && b[i] != q; i++ {
		if b[i] == '\\' {
			i++
		} else if b[i] == '\n' {
			return i
		}
	}
	return min(i+1, len(b))
}

func skipTemplate(b []byte, i int) int {
	for i++; i < len(b); {
		switch {
		case b[i] == '\\':
			i += 2
		case b[i] == '`':
			return i + 1
		case b[i] == '$' && i+1 < len(b) && b[i+1] == '{':
			i = scanCode(b, i+2, true)
			if i < len(b) {
				i++
			}
		default:
			i++
		}
	}
	return len(b)
}

func skipRegex(b []byte, i int) int {
	class := false
	for i++; i < len(b); i++ {
		switch b[i] {
		case '\\':
			i++
		case '[':
			class = true
		case ']':
			class = false
		case '/':
			if !class {
				return i + 1
			}
		case '\n':
			return i
		}
	}
	return len(b)
}

// matchClose 返回与 b[i]（一个开括号）配对的闭括号下标，找不到返回 -1。
func matchClose(b []byte, i int) int {
	depth := 0
	for i < len(b) {
		switch c := b[i]; c {
		case '(', '[', '{':
			depth++
		case ')', ']', '}':
			depth--
			if depth == 0 {
				return i
			}
		case '\'', '"':
			i = skipQuoted(b, i, c)
			continue
		case '`':
			i = skipTemplate(b, i)
			continue
		}
		i++
	}
	return -1
}

func skipWS(b []byte, i int) int {
	for i < len(b) && (b[i] == ' ' || b[i] == '\t' || b[i] == '\n' || b[i] == '\r') {
		i++
	}
	return i
}

// skipGeneric 跳过 <...> 泛型实参（箭头 => 不算闭合），返回之后的下标；不是泛型返回原下标。
func skipGeneric(b []byte, i int) int {
	if i >= len(b) || b[i] != '<' {
		return i
	}
	depth := 0
	for j := i; j < len(b); j++ {
		switch b[j] {
		case '<':
			depth++
		case '>':
			if j > 0 && b[j-1] == '=' {
				continue
			}
			depth--
			if depth == 0 {
				return j + 1
			}
		case '\'', '"':
			j = skipQuoted(b, j, b[j]) - 1
		case '`':
			j = skipTemplate(b, j) - 1
		}
	}
	return i
}

// depth0 取 b[s:e] 中不在任何括号内的部分（括号及其内容换成空格，换行保留）；字符串/模板在 0 层时原样保留。
func depth0(b []byte, s, e int) []byte {
	out := make([]byte, e-s)
	depth := 0
	for i := s; i < e; {
		c := b[i]
		j := i + 1
		if c == '\'' || c == '"' {
			j = skipQuoted(b, i, c)
		} else if c == '`' {
			j = skipTemplate(b, i)
		}
		j = min(j, e)
		isOpen, isClose := c == '(' || c == '[' || c == '{', c == ')' || c == ']' || c == '}'
		if isClose {
			depth--
		}
		for k := i; k < j; k++ {
			if depth == 0 && !isOpen && !isClose || b[k] == '\n' {
				out[k-s] = b[k]
			} else {
				out[k-s] = ' '
			}
		}
		if isOpen {
			depth++
		}
		i = j
	}
	return out
}
