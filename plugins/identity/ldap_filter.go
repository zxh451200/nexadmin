package identity

import (
	"fmt"
	"strconv"
	"strings"
)

// userPlaceholder 是用户过滤器模板里代表登录名的占位符，例如 (&(objectClass=person)(sAMAccountName={username}))。
const userPlaceholder = "{username}"

// escapeFilterValue 按 RFC 4515 转义过滤器断言值：\ * ( ) NUL 以及所有控制字符都写成 \HH。
// 用户输入必须先过它再拼进过滤器，否则 "*)(uid=*" 之类的输入会改写过滤器结构（LDAP 注入）。
func escapeFilterValue(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c == '\\' || c == '*' || c == '(' || c == ')' || c < 0x20 || c == 0x7f {
			fmt.Fprintf(&b, `\%02x`, c)
			continue
		}
		b.WriteByte(c)
	}
	return b.String()
}

// buildUserFilter 把转义后的登录名代入模板。只做一次替换，不会二次展开用户输入。
func buildUserFilter(tpl, username string) string {
	return strings.ReplaceAll(tpl, userPlaceholder, escapeFilterValue(username))
}

// LDAP 过滤器标签。
const (
	fAnd     = 0xa0
	fOr      = 0xa1
	fNot     = 0xa2
	fEq      = 0xa3
	fSub     = 0xa4
	fGE      = 0xa5
	fLE      = 0xa6
	fPresent = 0x87
	fApprox  = 0xa8
)

// compileFilter 把 RFC 4515 字符串过滤器编码成 BER。不支持扩展匹配（:=），语法错误返回 error。
func compileFilter(s string) ([]byte, error) {
	p := &filterParser{s: s}
	out, err := p.filter(0)
	if err != nil {
		return nil, err
	}
	if p.i != len(p.s) {
		return nil, fmt.Errorf("filter: unexpected trailing characters at %d", p.i)
	}
	return out, nil
}

type filterParser struct {
	s string
	i int
}

func (p *filterParser) filter(depth int) ([]byte, error) {
	if depth > 16 {
		return nil, fmt.Errorf("filter: nested too deeply")
	}
	if p.i >= len(p.s) || p.s[p.i] != '(' {
		return nil, fmt.Errorf("filter: expected '(' at %d", p.i)
	}
	p.i++
	if p.i >= len(p.s) {
		return nil, fmt.Errorf("filter: truncated")
	}
	var out []byte
	switch p.s[p.i] {
	case '&', '|':
		tag := byte(fAnd)
		if p.s[p.i] == '|' {
			tag = fOr
		}
		p.i++
		var parts [][]byte
		for p.i < len(p.s) && p.s[p.i] == '(' {
			f, err := p.filter(depth + 1)
			if err != nil {
				return nil, err
			}
			parts = append(parts, f)
		}
		if len(parts) == 0 {
			return nil, fmt.Errorf("filter: empty and/or list")
		}
		out = enc(tag, parts...)
	case '!':
		p.i++
		f, err := p.filter(depth + 1)
		if err != nil {
			return nil, err
		}
		out = enc(fNot, f)
	default:
		var err error
		if out, err = p.item(); err != nil {
			return nil, err
		}
	}
	if p.i >= len(p.s) || p.s[p.i] != ')' {
		return nil, fmt.Errorf("filter: expected ')' at %d", p.i)
	}
	p.i++
	return out, nil
}

// item 解析 attr op value，直到当前括号结束。
func (p *filterParser) item() ([]byte, error) {
	start := p.i
	for p.i < len(p.s) && !strings.ContainsRune("=<>~():", rune(p.s[p.i])) {
		p.i++
	}
	attr := p.s[start:p.i]
	if attr == "" || p.i >= len(p.s) {
		return nil, fmt.Errorf("filter: missing attribute or operator")
	}
	tag := byte(fEq)
	switch {
	case p.s[p.i] == '=':
	case strings.HasPrefix(p.s[p.i:], ">="):
		tag, p.i = fGE, p.i+1
	case strings.HasPrefix(p.s[p.i:], "<="):
		tag, p.i = fLE, p.i+1
	case strings.HasPrefix(p.s[p.i:], "~="):
		tag, p.i = fApprox, p.i+1
	default:
		return nil, fmt.Errorf("filter: unsupported operator at %d", p.i)
	}
	p.i++ // 跳过 '='
	vstart := p.i
	for p.i < len(p.s) && p.s[p.i] != ')' {
		if p.s[p.i] == '(' {
			return nil, fmt.Errorf("filter: unescaped '(' in value")
		}
		p.i++
	}
	raw := p.s[vstart:p.i]
	if tag == fEq && raw == "*" {
		return encStr(fPresent, attr), nil
	}
	if tag == fEq && strings.Contains(raw, "*") {
		return encodeSubstrings(attr, raw)
	}
	val, err := unescapeValue(raw)
	if err != nil || strings.Contains(raw, "*") {
		return nil, fmt.Errorf("filter: bad value for %q", attr)
	}
	return enc(tag, encStr(tagOctet, attr), encStr(tagOctet, val)), nil
}

func encodeSubstrings(attr, raw string) ([]byte, error) {
	segs := strings.Split(raw, "*")
	var parts [][]byte
	for i, sg := range segs {
		if sg == "" {
			continue
		}
		v, err := unescapeValue(sg)
		if err != nil {
			return nil, err
		}
		tag := byte(0x81) // any
		if i == 0 {
			tag = 0x80 // initial
		} else if i == len(segs)-1 {
			tag = 0x82 // final
		}
		parts = append(parts, encStr(tag, v))
	}
	if len(parts) == 0 {
		return nil, fmt.Errorf("filter: empty substring")
	}
	return enc(fSub, encStr(tagOctet, attr), enc(tagSeq, parts...)), nil
}

// unescapeValue 还原 \HH 转义；裸 '\' 后不是两位十六进制即报错。
func unescapeValue(s string) (string, error) {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] != '\\' {
			b.WriteByte(s[i])
			continue
		}
		if i+2 >= len(s) {
			return "", fmt.Errorf("bad escape")
		}
		v, err := strconv.ParseUint(s[i+1:i+3], 16, 8)
		if err != nil {
			return "", fmt.Errorf("bad escape")
		}
		b.WriteByte(byte(v))
		i += 2
	}
	return b.String(), nil
}
