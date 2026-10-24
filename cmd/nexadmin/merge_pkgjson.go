package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sort"
	"strings"
)

// ---- web/package.json 合并（merge_paths）----
//
// 规则：name/version/private 等标识字段以当前工程为准（当前没有也不补）；dependencies 等依赖段与 scripts/engines
// 等其余对象：新包里的条目覆盖同名条目、当前独有的保留；非对象字段新包覆盖。键顺序稳定：沿用当前文件的顺序，
// 新键按新包顺序追加；依赖段（有改动时）按字母序；缩进 2 空格。无任何实际变化时原样返回。

var pkgIdentityKeys = map[string]bool{
	"name": true, "version": true, "private": true, "description": true, "license": true, "author": true,
	"contributors": true, "repository": true, "homepage": true, "bugs": true, "keywords": true, "funding": true,
}

var pkgSortedSections = map[string]bool{
	"dependencies": true, "devDependencies": true, "peerDependencies": true,
	"optionalDependencies": true, "overrides": true, "resolutions": true,
}

// ojson 是保序的 JSON 对象（值保留原始字节）。
type ojson struct {
	keys []string
	vals map[string]json.RawMessage
}

func (o *ojson) set(k string, v json.RawMessage) {
	if _, ok := o.vals[k]; !ok {
		o.keys = append(o.keys, k)
	}
	o.vals[k] = v
}

func (o *ojson) clone() *ojson {
	c := &ojson{keys: append([]string(nil), o.keys...), vals: make(map[string]json.RawMessage, len(o.vals))}
	for k, v := range o.vals {
		c.vals[k] = v
	}
	return c
}

func parseOJSON(raw []byte) (*ojson, error) {
	dec := json.NewDecoder(bytes.NewReader(raw))
	if t, err := dec.Token(); err != nil {
		return nil, err
	} else if d, ok := t.(json.Delim); !ok || d != '{' {
		return nil, errors.New("不是 JSON 对象")
	}
	o := &ojson{vals: map[string]json.RawMessage{}}
	for dec.More() {
		kt, err := dec.Token()
		if err != nil {
			return nil, err
		}
		var v json.RawMessage
		if err := dec.Decode(&v); err != nil {
			return nil, err
		}
		o.set(kt.(string), v)
	}
	if _, err := dec.Token(); err != nil {
		return nil, err
	}
	if _, err := dec.Token(); err != io.EOF {
		return nil, errors.New("JSON 对象之后还有多余内容")
	}
	return o, nil
}

func jsonQuote(s string) []byte {
	var b bytes.Buffer
	e := json.NewEncoder(&b)
	e.SetEscapeHTML(false)
	_ = e.Encode(s)
	return bytes.TrimRight(b.Bytes(), "\n")
}

func (o *ojson) compact() []byte {
	var b bytes.Buffer
	b.WriteByte('{')
	for i, k := range o.keys {
		if i > 0 {
			b.WriteByte(',')
		}
		b.Write(jsonQuote(k))
		b.WriteByte(':')
		b.Write(o.vals[k])
	}
	b.WriteByte('}')
	return b.Bytes()
}

func asObject(v json.RawMessage) (*ojson, bool) {
	if t := bytes.TrimSpace(v); len(t) == 0 || t[0] != '{' {
		return nil, false
	}
	o, err := parseOJSON(v)
	return o, err == nil
}

func jsonSame(a, b json.RawMessage) bool {
	var x, y bytes.Buffer
	if json.Compact(&x, a) != nil || json.Compact(&y, b) != nil {
		return bytes.Equal(a, b)
	}
	return bytes.Equal(x.Bytes(), y.Bytes())
}

func jsonShow(v json.RawMessage) string {
	var s string
	if json.Unmarshal(v, &s) == nil {
		return s
	}
	var b bytes.Buffer
	if json.Compact(&b, v) == nil {
		return b.String()
	}
	return string(v)
}

// mergePkgSection 合并一个对象段：新包条目覆盖同名条目，当前独有的保留。
func mergePkgSection(name string, c, n *ojson) (res *ojson, lines, kept []string, changed bool) {
	res = c.clone()
	for _, k := range n.keys {
		nv := n.vals[k]
		cv, has := c.vals[k]
		switch {
		case !has:
			res.set(k, nv)
			lines = append(lines, fmt.Sprintf("+ %s.%s: %s（框架新增）", name, k, jsonShow(nv)))
			changed = true
		case !jsonSame(cv, nv):
			res.set(k, nv)
			lines = append(lines, fmt.Sprintf("↑ %s.%s: %s -> %s（框架覆盖）", name, k, jsonShow(cv), jsonShow(nv)))
			changed = true
		}
	}
	for _, k := range c.keys {
		if _, ok := n.vals[k]; !ok {
			kept = append(kept, k)
		}
	}
	if changed && pkgSortedSections[name] {
		sort.Strings(res.keys)
	}
	return res, lines, kept, changed
}

func mergePackageJSON(cur, nw []byte) ([]byte, []string, bool, error) {
	co, err := parseOJSON(cur)
	if err != nil {
		return nil, nil, false, fmt.Errorf("当前 package.json: %w", err)
	}
	no, err := parseOJSON(nw)
	if err != nil {
		return nil, nil, false, fmt.Errorf("新包 package.json: %w", err)
	}
	res := co.clone()
	var rep, kept []string
	changed := false
	for _, k := range no.keys {
		nv := no.vals[k]
		cv, has := co.vals[k]
		if pkgIdentityKeys[k] {
			continue // 标识字段以当前工程为准
		}
		if !has {
			res.set(k, nv)
			rep = append(rep, "+ "+k+"（框架新增）")
			changed = true
			continue
		}
		cobj, ok1 := asObject(cv)
		nobj, ok2 := asObject(nv)
		if ok1 && ok2 {
			m, lines, kp, ch := mergePkgSection(k, cobj, nobj)
			if ch {
				res.vals[k] = m.compact()
				rep = append(rep, lines...)
				changed = true
			}
			if len(kp) > 0 {
				kept = append(kept, fmt.Sprintf("= 保留二开独有 %s: %s", k, strings.Join(kp, ", ")))
			}
		} else if !jsonSame(cv, nv) {
			res.vals[k] = nv
			rep = append(rep, fmt.Sprintf("↑ %s: %s -> %s（框架覆盖）", k, jsonShow(cv), jsonShow(nv)))
			changed = true
		}
	}
	if !changed {
		return cur, nil, false, nil
	}
	var out bytes.Buffer
	if err := json.Indent(&out, res.compact(), "", "  "); err != nil {
		return nil, nil, false, err
	}
	out.WriteByte('\n')
	return out.Bytes(), append(rep, kept...), true, nil
}
