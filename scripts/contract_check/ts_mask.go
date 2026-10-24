package main

import "strings"

// maskStrings 标出落在字符串 / 模板字符串文本里的位置（模板里的 ${} 表达式属于代码，不标）。
// 用来排除“字符串里的示例代码”（如文档页里展示的 fetch(...) 示例）被当成真实调用。
func maskStrings(b []byte) []bool {
	m := make([]bool, len(b))
	maskCode(b, m, 0, false)
	return m
}

func markRange(m []bool, from, to int) {
	for k := from; k < to && k < len(m); k++ {
		m[k] = true
	}
}

func maskCode(b []byte, m []bool, i int, stopAtBrace bool) int {
	depth := 0
	prev := byte(';')
	for i < len(b) {
		c := b[i]
		switch {
		case c == '/' && strings.IndexByte("(,=:[!&|?{};", prev) >= 0:
			i = skipRegex(b, i)
			prev = ')'
			continue
		case c == '\'' || c == '"':
			j := skipQuoted(b, i, c)
			markRange(m, i+1, j-1)
			i, prev = j, ')'
			continue
		case c == '`':
			i = maskTemplate(b, m, i)
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

func maskTemplate(b []byte, m []bool, i int) int {
	start := i + 1
	for i++; i < len(b); {
		switch {
		case b[i] == '\\':
			i += 2
		case b[i] == '`':
			markRange(m, start, i)
			return i + 1
		case b[i] == '$' && i+1 < len(b) && b[i+1] == '{':
			markRange(m, start, i)
			i = maskCode(b, m, i+2, true)
			start = i + 1
			if i < len(b) {
				i++
			}
		default:
			i++
		}
	}
	return len(b)
}
