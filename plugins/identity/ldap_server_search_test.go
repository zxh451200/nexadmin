package identity

import (
	"strconv"
	"strings"
)

// parsePagedReq 解出请求里的 RFC 2696 分页 control（页大小与 cookie）。
func parsePagedReq(ctl []byte) (size int, cookie string, ok bool) {
	_, list, _, err := parseTLV(ctl)
	if err != nil {
		return 0, "", false
	}
	for len(list) > 0 {
		var c []byte
		_, c, list, _ = parseTLV(list)
		_, oid, rest, _ := parseTLV(c)
		if string(oid) != pagedResultsOID {
			continue
		}
		vt, v, rest, _ := parseTLV(rest)
		if vt == tagBool {
			_, v, _, _ = parseTLV(rest)
		}
		_, sv, _, _ := parseTLV(v)
		_, szb, sv, _ := parseTLV(sv)
		_, ck, _, _ := parseTLV(sv)
		size, _ = decodeInt(szb)
		return size, string(ck), true
	}
	return 0, "", false
}

func pagedDoneCtl(cookie string) []byte {
	val := enc(tagSeq, encInt(tagInt, 0), enc(tagOctet, []byte(cookie)))
	return enc(tagControls, enc(tagSeq, encStr(tagOctet, pagedResultsOID), enc(tagOctet, val)))
}

// doSearch 解码搜索请求，按 scope 与过滤器匹配内存目录，返回要写回的整条报文序列。
// 支持 base / sub 范围（与真实服务端一致：base 只匹配 DN 恰为 base 的条目）和 RFC 2696 分页。
func (s *testLDAP) doSearch(id int, op, ctl []byte) [][]byte {
	_, base, rest, _ := parseTLV(op)
	_, scv, rest, _ := parseTLV(rest) // scope
	scope, _ := decodeInt(scv)
	_, _, rest, _ = parseTLV(rest) // deref
	_, sz, rest, _ := parseTLV(rest)
	_, _, rest, _ = parseTLV(rest) // time
	_, _, rest, _ = parseTLV(rest) // typesOnly
	ftag, fval, rest, _ := parseTLV(rest)
	_, attrList, _, _ := parseTLV(rest)
	limit, _ := decodeInt(sz)

	match, repr := decodeFilter(ftag, fval)
	s.mu.Lock()
	s.filters = append(s.filters, repr)
	entries := append([]testEntry(nil), s.entries...)
	noPaging := s.noPaging
	s.pageReqs = append(s.pageReqs, len(ctl) > 0)
	s.mu.Unlock()

	var want []string
	for len(attrList) > 0 {
		var v []byte
		_, v, attrList, _ = parseTLV(attrList)
		want = append(want, strings.ToLower(string(v)))
	}
	var matched []testEntry
	for _, e := range entries {
		dn, b := strings.ToLower(e.dn), strings.ToLower(string(base))
		inScope := strings.HasSuffix(dn, b)
		if scope == scopeBase {
			inScope = dn == b
		}
		if inScope && match(e.attrs) {
			matched = append(matched, e)
		}
	}
	var out [][]byte
	code := 0
	pageSize, cookie, paged := parsePagedReq(ctl)
	paged = paged && !noPaging
	nextCookie := ""
	if paged {
		off, _ := strconv.Atoi(cookie)
		end := min(off+pageSize, len(matched))
		if off > len(matched) {
			off = len(matched)
		}
		if end < len(matched) {
			nextCookie = strconv.Itoa(end)
		}
		matched = matched[off:end]
	} else if limit > 0 && len(matched) > limit {
		matched, code = matched[:limit], 4
	}
	for _, e := range matched {
		var attrs [][]byte
		for _, a := range want {
			if vals, ok := e.attrs[a]; ok {
				var vs [][]byte
				for _, v := range vals {
					vs = append(vs, encStr(tagOctet, v))
				}
				attrs = append(attrs, enc(tagSeq, encStr(tagOctet, a), enc(tagSet, vs...)))
			}
		}
		out = append(out, ldapMsg(id, enc(tagSearchEnt, encStr(tagOctet, e.dn), enc(tagSeq, attrs...))))
	}
	done := ldapResult(tagSearchDone, code, "")
	if paged {
		return append(out, enc(tagSeq, encInt(tagInt, id), done, pagedDoneCtl(nextCookie)))
	}
	return append(out, ldapMsg(id, done))
}

func children(b []byte) [][2]any {
	var out [][2]any
	for len(b) > 0 {
		t, v, rest, err := parseTLV(b)
		if err != nil {
			break
		}
		out = append(out, [2]any{t, v})
		b = rest
	}
	return out
}

// decodeFilter 把 BER 过滤器还原成匹配函数和可读字符串，字符串用来断言“注入没有改变过滤器结构”。
func decodeFilter(tag byte, val []byte) (func(map[string][]string) bool, string) {
	switch tag {
	case fAnd, fOr:
		var ms []func(map[string][]string) bool
		var rs []string
		for _, c := range children(val) {
			m, r := decodeFilter(c[0].(byte), c[1].([]byte))
			ms, rs = append(ms, m), append(rs, r)
		}
		op, sym := "&", true
		if tag == fOr {
			op, sym = "|", false
		}
		return func(a map[string][]string) bool {
			for _, m := range ms {
				if m(a) != sym {
					return !sym
				}
			}
			return sym
		}, "(" + op + strings.Join(rs, "") + ")"
	case fNot:
		_, v, _, _ := parseTLV(val)
		t, _, _, _ := parseTLV(val)
		m, r := decodeFilter(t, v)
		return func(a map[string][]string) bool { return !m(a) }, "(!" + r + ")"
	case fPresent:
		name := strings.ToLower(string(val))
		return func(a map[string][]string) bool { _, ok := a[name]; return ok }, "(" + name + "=*)"
	case fEq:
		_, an, rest, _ := parseTLV(val)
		_, av, _, _ := parseTLV(rest)
		name, want := strings.ToLower(string(an)), string(av)
		return func(a map[string][]string) bool {
			for _, v := range a[name] {
				if strings.EqualFold(v, want) {
					return true
				}
			}
			return false
		}, "(" + name + "=" + want + ")"
	case fSub:
		_, an, rest, _ := parseTLV(val)
		_, parts, _, _ := parseTLV(rest)
		name := strings.ToLower(string(an))
		pat := ""
		for _, p := range children(parts) {
			switch p[0].(byte) {
			case 0x80:
				pat += string(p[1].([]byte)) + "*"
			case 0x81:
				if !strings.HasSuffix(pat, "*") {
					pat += "*"
				}
				pat += string(p[1].([]byte)) + "*"
			case 0x82:
				if !strings.HasSuffix(pat, "*") {
					pat += "*"
				}
				pat += string(p[1].([]byte))
			}
		}
		return func(a map[string][]string) bool {
			for _, v := range a[name] {
				if globMatch(strings.ToLower(pat), strings.ToLower(v)) {
					return true
				}
			}
			return false
		}, "(" + name + "=" + pat + ")"
	}
	return func(map[string][]string) bool { return false }, "(unsupported)"
}

func globMatch(pat, s string) bool {
	if !strings.Contains(pat, "*") {
		return pat == s
	}
	parts := strings.Split(pat, "*")
	if !strings.HasPrefix(s, parts[0]) {
		return false
	}
	s = s[len(parts[0]):]
	for _, p := range parts[1 : len(parts)-1] {
		i := strings.Index(s, p)
		if i < 0 {
			return false
		}
		s = s[i+len(p):]
	}
	return strings.HasSuffix(s, parts[len(parts)-1])
}
