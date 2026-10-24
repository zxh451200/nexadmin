// Package vrange 是够用的 semver 范围解析：支持 >= > <= < = == ^ ~，多个条件用空格或逗号分隔（全部满足），忽略预发布 / 构建后缀。
// nexadmin upgrade（based_on_framework）、nexadmin install 与运行时插件加载（requires_nexadmin）共用，别在别处再写一份。
package vrange

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"unicode"
)

// Version 是 major.minor.patch。
type Version [3]int

// Parse 解析 "1.2.3" / "v1.2" / "1.2.3-rc1+meta"；缺省段补 0。
func Parse(s string) (Version, error) {
	var v Version
	s = strings.TrimPrefix(strings.TrimSpace(s), "v")
	if i := strings.IndexAny(s, "-+"); i >= 0 {
		s = s[:i]
	}
	parts := strings.Split(s, ".")
	if s == "" || len(parts) > 3 {
		return v, fmt.Errorf("无法解析版本号 %q", s)
	}
	for i, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil || n < 0 {
			return v, fmt.Errorf("无法解析版本号 %q", s)
		}
		v[i] = n
	}
	return v, nil
}

// Cmp 比较两个版本：-1 / 0 / 1。
func (a Version) Cmp(b Version) int {
	for i := range a {
		if a[i] != b[i] {
			if a[i] < b[i] {
				return -1
			}
			return 1
		}
	}
	return 0
}

// Satisfies 判断 version 是否满足 constraint（所有条件都满足）。
func Satisfies(version, constraint string) (bool, error) {
	ver, err := Parse(version)
	if err != nil {
		return false, err
	}
	terms := strings.FieldsFunc(constraint, func(r rune) bool { return r == ',' || unicode.IsSpace(r) })
	if len(terms) == 0 {
		return false, errors.New("版本范围为空")
	}
	for _, t := range terms {
		op := ""
		for _, o := range []string{">=", "<=", "==", ">", "<", "=", "^", "~"} {
			if strings.HasPrefix(t, o) {
				op = o
				break
			}
		}
		v, err := Parse(strings.TrimPrefix(t, op))
		if err != nil {
			return false, fmt.Errorf("版本范围 %q: %w", constraint, err)
		}
		c := ver.Cmp(v)
		var ok bool
		switch op {
		case ">=":
			ok = c >= 0
		case "<=":
			ok = c <= 0
		case ">":
			ok = c > 0
		case "<":
			ok = c < 0
		case "", "=", "==":
			ok = c == 0
		case "^":
			up := Version{v[0] + 1}
			if v[0] == 0 {
				up = Version{0, v[1] + 1}
				if v[1] == 0 {
					up = Version{0, 0, v[2] + 1}
				}
			}
			ok = c >= 0 && ver.Cmp(up) < 0
		case "~":
			ok = c >= 0 && ver.Cmp(Version{v[0], v[1] + 1}) < 0
		}
		if !ok {
			return false, nil
		}
	}
	return true, nil
}
