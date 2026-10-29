package main

// 错误文案 key 静态核对：扫描 Go 源码里 response 错误构造的 key 字面量，
// 与内置语言包（core/i18n/locales/*.yaml）+ 用户覆盖（config/locales/*.yaml）比对。
// 缺 key 的错误会把 key 原文返给用户（core/i18n 的回退语义），所以缺 key 视为 E 类问题。

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// errKeyRe 匹配同行的 key 字面量；key 必须含点号（组.名，真实 key 的形状），
// 排除 response.New 的动态/非常量用法与无命名空间的误匹配。跨行的调用点不参与（宁缺毋滥）。
var errKeyRe = regexp.MustCompile(
	`response\.(?:New|BadRequest|Unauthorized|Forbidden|NotFound|Conflict|Unavailable)\(\s*"([a-zA-Z][a-zA-Z0-9_]*\.[a-zA-Z0-9_.]+)"`)

// skipErrKeyDir 的补充：cmd/ 是 nexadmin CLI（new_agents.go 内嵌文本里有 "response." 字样），不扫描。
var errKeySkipDirs = []string{".git/", "node_modules/", "web/", "scripts/", "testdata/", "testkit/", ".mimosa/", "vendor/", "cmd/"}

// errLocales 是要核对的语言（缺 zh 用户看到 key，缺 en 英文用户看到 key）。
var errLocales = []struct {
	lang  string
	paths []string // 后者覆盖前者；任一文件声明了该 key 即算存在
}{
	{"zh-CN", []string{"core/i18n/locales/zh-CN.yaml", "config/locales/zh-CN.yaml"}},
	{"en-US", []string{"core/i18n/locales/en-US.yaml", "config/locales/en-US.yaml"}},
}

type ErrKeyIssue struct {
	Loc
	Key  string `json:"key"`
	Lang string `json:"lang"` // 缺失的语言
}

// flattenYAML 把嵌套 map 展开成点分 key 集合（列表值不展开，出现列表的分支视为叶子）。
func flattenYAML(node any, prefix string, out map[string]bool) {
	switch v := node.(type) {
	case map[string]any:
		for k, child := range v {
			join := k
			if prefix != "" {
				join = prefix + "." + k
			}
			flattenYAML(child, join, out)
		}
	default:
		if prefix != "" {
			out[prefix] = true
		}
	}
}

func loadLocaleKeys(root, rel string) (map[string]bool, error) {
	out := map[string]bool{}
	b, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
	if err != nil {
		if os.IsNotExist(err) {
			return out, nil
		}
		return nil, err
	}
	var node map[string]any
	if err := yaml.Unmarshal(b, &node); err != nil {
		return nil, fmt.Errorf("解析 %s: %w", rel, err)
	}
	flattenYAML(node, "", out)
	return out, nil
}

// skipErrKeyDir 排除测试固件、脚本、CLI 与前端目录（测试里的假 key 不参与核对）。
func skipErrKeyDir(rel string) bool {
	for _, p := range errKeySkipDirs {
		if strings.Contains(rel, p) {
			return true
		}
	}
	return strings.HasSuffix(rel, "_test.go")
}

// checkErrorKeys 返回缺失的（key, 语言）清单与扫描的 key 总数。
func checkErrorKeys(root string) ([]ErrKeyIssue, int, error) {
	localeSets := make([]map[string]bool, len(errLocales))
	for i, l := range errLocales {
		merged := map[string]bool{}
		for _, p := range l.paths {
			keys, err := loadLocaleKeys(root, p)
			if err != nil {
				return nil, 0, err
			}
			for k := range keys {
				merged[k] = true
			}
		}
		localeSets[i] = merged
	}

	var used []struct {
		key string
		Loc
	}
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		rel, rerr := filepath.Rel(root, p)
		if rerr != nil {
			return nil
		}
		rel = filepath.ToSlash(rel)
		if d.IsDir() {
			if skipErrKeyDir(rel+"/") && rel != "." {
				return fs.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(rel, ".go") || skipErrKeyDir(rel) {
			return nil
		}
		b, rerr := os.ReadFile(p)
		if rerr != nil {
			return nil
		}
		line := 1
		for _, ln := range strings.Split(string(b), "\n") {
			for _, m := range errKeyRe.FindAllStringSubmatch(ln, -1) {
				used = append(used, struct {
					key string
					Loc
				}{m[1], Loc{File: rel, Line: line}})
			}
			line++
		}
		return nil
	})
	if err != nil {
		return nil, 0, err
	}

	sort.Slice(used, func(i, j int) bool { return used[i].key < used[j].key })
	var issues []ErrKeyIssue
	for _, u := range used {
		for i, l := range errLocales {
			if !localeSets[i][u.key] {
				issues = append(issues, ErrKeyIssue{Loc: u.Loc, Key: u.key, Lang: l.lang})
			}
		}
	}
	return issues, len(used), nil
}
