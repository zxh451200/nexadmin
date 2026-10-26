package market

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"gopkg.in/yaml.v3"

	"nexadmin/core/plugin"
)

// Version 是市场目录里的一条版本历史。Ref 为空表示不提供该版本的下载源。
type Version struct {
	Version string `json:"version" yaml:"version"`
	Date    string `json:"date"    yaml:"date"`  // YYYY-MM-DD，展示用
	Ref     string `json:"ref"     yaml:"ref"`   // GitHub tag/分支
	Notes   string `json:"notes"   yaml:"notes"` // 一句话变更
}

// catalogFile 是市场目录文件的位置（项目根下）。
const catalogFile = "config/plugin-market.yaml"

// EntryText 是一条插件的本地化展示文案；未填的字段回退到条目原文。
type EntryText struct {
	Name        string `json:"name" yaml:"name"`
	Description string `json:"description" yaml:"description"`
}

// Entry 是市场目录里的一条插件记录。展示用字段（名称/简介/分类/版本）写在目录里，
// 安装时会与插件包内 plugin.yaml 交叉校验（id/edition 必须一致），目录造假装不进错误的包。
type Entry struct {
	ID          string         `json:"id"          yaml:"id"`
	Name        string         `json:"name"        yaml:"name"`
	Description string         `json:"description" yaml:"description"`
	Category    string         `json:"category"    yaml:"category"`
	Edition     plugin.Edition `json:"edition"     yaml:"edition"`
	Version     string         `json:"version"     yaml:"version"`
	Repo        string         `json:"repo"        yaml:"repo"`   // GitHub owner/name；多插件 monorepo 也填它
	Ref         string         `json:"ref"         yaml:"ref"`    // tag 或分支；空 = 默认分支
	Subdir      string         `json:"subdir"      yaml:"subdir"` // monorepo 里本插件所在子目录（如 cron）；单插件仓库留空
	URL         string         `json:"url"         yaml:"url"`    // 可选 zip 直链（OSS/自建镜像）；设置后优先于 GitHub
	Homepage    string         `json:"homepage"    yaml:"homepage"`
	TokenEnv    string         `json:"tokenEnv"    yaml:"token_env"` // 私有仓库可选：从该环境变量读 GitHub token
	Price       string         `json:"price"       yaml:"price"`     // 展示用："待定"、"¥299"、"免费"；静态导出时原样输出
	Author      string         `json:"author"      yaml:"author"`
	// 截图（http(s) 或站内 /...）、更新日志、版本历史；旧版本要能下载必须在 Versions 里填 ref。
	Screenshots  []string  `json:"screenshots"  yaml:"screenshots"`
	ChangelogURL string    `json:"changelogUrl" yaml:"changelog_url"`
	Versions     []Version `json:"versions"     yaml:"versions"`
	// I18n 是官网展示用的本地化名称/简介，键是语言 code（如 en-US）；其余字段回退原文。
	I18n map[string]EntryText `json:"i18n,omitempty" yaml:"i18n"`
}

type catalogFileLayout struct {
	Plugins []Entry `yaml:"plugins"`
}

var repoRe = regexp.MustCompile(`^[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+$`)
var catalogVersionRe = regexp.MustCompile(`^[0-9A-Za-z][0-9A-Za-z._+-]{0,31}$`)
var refRe = regexp.MustCompile(`^[A-Za-z0-9._/-]+$`)
var localeRe = regexp.MustCompile(`^[a-z]{2}(-[A-Z]{2})?$`)

// validRef 限制版本 ref 只含 URL 路径安全字符，且不能是绝对路径或包含 ..。
func validRef(r string) bool {
	return refRe.MatchString(r) && !strings.HasPrefix(r, "/") && !strings.Contains(r, "..")
}

// validScreenshot 只接受 http(s) 绝对地址或站内根相对路径（拒绝 //host 的协议相对地址）。
func validScreenshot(u string) bool {
	if strings.HasPrefix(u, "/") {
		return len(u) > 1 && !strings.HasPrefix(u, "//") && !strings.ContainsAny(u, " \t\r\n")
	}
	if !(strings.HasPrefix(u, "https://") || strings.HasPrefix(u, "http://")) {
		return false
	}
	return !strings.ContainsAny(u, " \t\r\n")
}

// LoadCatalog 读取项目根下的市场目录；文件不存在返回空目录（市场页显示为空，不算错误）。
func LoadCatalog(root string) ([]Entry, error) {
	raw, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(catalogFile)))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var c catalogFileLayout
	if err := yaml.Unmarshal(raw, &c); err != nil {
		return nil, fmt.Errorf("解析 %s 失败: %w", catalogFile, err)
	}
	seen := map[string]bool{}
	for i := range c.Plugins {
		e := &c.Plugins[i]
		if !plugin.ValidID(e.ID) {
			return nil, fmt.Errorf("%s 第 %d 条：非法插件 id %q", catalogFile, i+1, e.ID)
		}
		if seen[e.ID] {
			return nil, fmt.Errorf("%s 第 %d 条：插件 id %q 重复", catalogFile, i+1, e.ID)
		}
		seen[e.ID] = true
		// 来源二选一：GitHub 仓库（repo）或 zip 直链（url，OSS/自建镜像）。
		if e.URL == "" && !repoRe.MatchString(e.Repo) {
			return nil, fmt.Errorf("%s 第 %d 条：%q 的 repo %q 不是 owner/name 形式（或改用 url 直链）", catalogFile, i+1, e.ID, e.Repo)
		}
		if e.URL != "" && e.Repo != "" {
			return nil, fmt.Errorf("%s 第 %d 条：%q 的 repo 与 url 只能填一个", catalogFile, i+1, e.ID)
		}
		if e.URL != "" && !(strings.HasPrefix(e.URL, "https://") || strings.HasPrefix(e.URL, "http://")) {
			return nil, fmt.Errorf("%s 第 %d 条：%q 的 url 必须是 http(s) 直链", catalogFile, i+1, e.ID)
		}
		if e.Edition != plugin.Free && e.Edition != plugin.Pro {
			return nil, fmt.Errorf("%s 第 %d 条：%q 的 edition 必须是 free 或 pro", catalogFile, i+1, e.ID)
		}
		for _, u := range e.Screenshots {
			if !validScreenshot(u) {
				return nil, fmt.Errorf("%s 第 %d 条：%q 的截图 %q 必须是 http(s) 或站内 / 路径", catalogFile, i+1, e.ID, u)
			}
		}
		for code, text := range e.I18n {
			if !localeRe.MatchString(code) {
				return nil, fmt.Errorf("%s 第 %d 条：%q 的翻译语言 code %q 非法（示例 en-US）", catalogFile, i+1, e.ID, code)
			}
			if strings.TrimSpace(text.Name) == "" && strings.TrimSpace(text.Description) == "" {
				return nil, fmt.Errorf("%s 第 %d 条：%q 的 %s 译文为空", catalogFile, i+1, e.ID, code)
			}
		}
		seenVer := map[string]bool{}
		for j, v := range e.Versions {
			if !catalogVersionRe.MatchString(v.Version) {
				return nil, fmt.Errorf("%s 第 %d 条：版本历史第 %d 项版本号 %q 非法", catalogFile, i+1, j+1, v.Version)
			}
			if seenVer[v.Version] {
				return nil, fmt.Errorf("%s 第 %d 条：版本号 %q 重复", catalogFile, i+1, v.Version)
			}
			seenVer[v.Version] = true
			if v.Ref != "" && !validRef(v.Ref) {
				return nil, fmt.Errorf("%s 第 %d 条：版本 %q 的 ref %q 非法", catalogFile, i+1, v.Version, v.Ref)
			}
			if v.Date != "" {
				if _, err := time.Parse("2006-01-02", v.Date); err != nil {
					return nil, fmt.Errorf("%s 第 %d 条：版本 %q 的日期 %q 必须是 YYYY-MM-DD", catalogFile, i+1, v.Version, v.Date)
				}
			}
		}
	}
	return c.Plugins, nil
}

// FindEntry 在目录里按 id 查找。
func FindEntry(entries []Entry, id string) (Entry, bool) {
	for _, e := range entries {
		if e.ID == id {
			return e, true
		}
	}
	return Entry{}, false
}

// EntryForVersion 返回指定版本的可下载条目：空/当前版本返回条目本身；
// 旧版本必须在 Versions 里配置 ref，否则无法解析下载源。
func EntryForVersion(e Entry, version string) (Entry, bool) {
	if version == "" || version == e.Version {
		return e, true
	}
	for _, v := range e.Versions {
		if v.Version != version {
			continue
		}
		if v.Ref == "" {
			return Entry{}, false
		}
		e.Version = v.Version
		e.Ref = v.Ref
		return e, true
	}
	return Entry{}, false
}

// DocsPlugin 是静态导出的条目结构（`plugin market sync --docs` 使用）。
// 市场目录（config/plugin-market.yaml）仍是唯一数据源；官网与后台直接读 YAML。
type DocsPlugin struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Version     string `json:"version"`
	Edition     string `json:"edition"`
	Description string `json:"description"`
	Price       string `json:"price"`
	Author      string `json:"author"`
}

// RenderDocsJSON 把市场目录渲染成静态导出 JSON（两格缩进、末尾换行，保持 git diff 干净）。
func RenderDocsJSON(entries []Entry) ([]byte, error) {
	out := make([]DocsPlugin, 0, len(entries))
	for _, e := range entries {
		out = append(out, DocsPlugin{
			ID: e.ID, Name: e.Name, Version: e.Version, Edition: string(e.Edition),
			Description: e.Description, Price: e.Price, Author: e.Author,
		})
	}
	b, err := json.MarshalIndent(out, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(b, '\n'), nil
}
