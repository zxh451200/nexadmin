package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"nexadmin/core/plugin/market"
	"nexadmin/pkg/vrange"
)

const (
	frameworkManifest = ".nexadmin.manifest.json"
	filesRecord       = ".nexadmin.files.json"
	trashDir          = ".nexadmin-trash"
	migrationsPrefix  = "migrations/framework/"
)

// upManifest 是框架清单与产品清单共用的结构；产品清单用 product_paths 代替 framework_paths。
type upManifest struct {
	FrameworkVersion string   `json:"framework_version,omitempty"`
	FrameworkPaths   []string `json:"framework_paths,omitempty"`
	MergePaths       []string `json:"merge_paths,omitempty"` // 合并而非覆盖：go.mod、go.sum、web/package.json
	UserPaths        []string `json:"user_paths"`
	ProductName      string   `json:"product_name,omitempty"`
	ProductVersion   string   `json:"product_version,omitempty"`
	BasedOnFramework string   `json:"based_on_framework,omitempty"`
	ProductPaths     []string `json:"product_paths,omitempty"`
}

func (m *upManifest) isProduct() bool { return len(m.ProductPaths) > 0 || m.ProductName != "" }

func (m *upManifest) managed() []string {
	if m.isProduct() {
		return m.ProductPaths
	}
	return m.FrameworkPaths
}

// upCleanPath 规范化清单里的路径：统一正斜杠，拒绝空、绝对、含 .. 的路径；保留结尾的 /。
func upCleanPath(p string) (string, error) {
	n := strings.ReplaceAll(strings.TrimSpace(p), "\\", "/")
	if n == "" || strings.HasPrefix(n, "/") || strings.ContainsRune(n, 0) || (len(n) >= 2 && n[1] == ':') {
		return "", fmt.Errorf("非法路径 %q", p)
	}
	for _, part := range strings.Split(n, "/") {
		if part == ".." {
			return "", fmt.Errorf("路径包含 ..: %q", p)
		}
	}
	dir := strings.HasSuffix(n, "/")
	n = strings.TrimPrefix(strings.TrimRight(n, "/"), "./")
	if n == "" || n == "." {
		return "", fmt.Errorf("非法路径 %q", p)
	}
	if dir {
		n += "/"
	}
	return n, nil
}

func upCleanAll(list []string) ([]string, error) {
	out := make([]string, 0, len(list))
	for _, p := range list {
		c, err := upCleanPath(p)
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, nil
}

// upMatch 判断 rel 是否落在 prefixes 中：以 / 结尾按目录前缀，否则按精确文件（或同名目录）；
// 含 * ? [ 的条目按 path.Match 通配（不跨目录，如 web/tsconfig.*.json）。
func upMatch(rel string, prefixes []string) bool {
	for _, p := range prefixes {
		if strings.ContainsAny(p, "*?[") {
			if ok, _ := path.Match(p, rel); ok {
				return true
			}
		} else if strings.HasSuffix(p, "/") {
			if strings.HasPrefix(rel, p) {
				return true
			}
		} else if rel == p || strings.HasPrefix(rel, p+"/") {
			return true
		}
	}
	return false
}

func loadUpManifest(path string) (*upManifest, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var m upManifest
	if err := json.Unmarshal(raw, &m); err != nil {
		return nil, fmt.Errorf("%s: %w", filepath.Base(path), err)
	}
	if m.FrameworkPaths, err = upCleanAll(m.FrameworkPaths); err != nil {
		return nil, fmt.Errorf("framework_paths: %w", err)
	}
	if m.ProductPaths, err = upCleanAll(m.ProductPaths); err != nil {
		return nil, fmt.Errorf("product_paths: %w", err)
	}
	if m.MergePaths, err = upCleanAll(m.MergePaths); err != nil {
		return nil, fmt.Errorf("merge_paths: %w", err)
	}
	if m.UserPaths, err = upCleanAll(m.UserPaths); err != nil {
		return nil, fmt.Errorf("user_paths: %w", err)
	}
	if m.isProduct() {
		if m.ProductName == "" || m.ProductVersion == "" || m.BasedOnFramework == "" || len(m.ProductPaths) == 0 {
			return nil, errors.New("产品清单需要 product_name、product_version、based_on_framework、product_paths")
		}
	} else if m.FrameworkVersion == "" || len(m.FrameworkPaths) == 0 {
		return nil, errors.New("框架清单需要 framework_version 与 framework_paths")
	}
	return &m, nil
}

// ---- 版本约束（够用的 semver 子集：>= > <= < = ^ ~，多个条件用空格或逗号分隔；忽略预发布后缀）----

// versionSatisfies 判断 version 是否满足 constraint；实现在共享包 pkg/vrange（install 与运行时插件加载同用）。
func versionSatisfies(version, constraint string) (bool, error) {
	return vrange.Satisfies(version, constraint)
}

// ---- git 工作区检查 ----

// upGitState 返回 (是否 git 仓库, 是否有未提交改动)。git 不存在或不是仓库都视为"不是仓库"。
func upGitState(root string) (isRepo, dirty bool, err error) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	chk := exec.CommandContext(ctx, "git", "rev-parse", "--is-inside-work-tree")
	chk.Dir = root
	if b, e := chk.Output(); e != nil || strings.TrimSpace(string(b)) != "true" {
		return false, false, nil
	}
	st := exec.CommandContext(ctx, "git", "status", "--porcelain", "--", ".")
	st.Dir = root
	b, e := st.Output()
	if e != nil {
		return true, false, fmt.Errorf("git status 失败: %w", e)
	}
	return true, len(bytes.TrimSpace(b)) > 0, nil
}

// ---- 哈希记录 .nexadmin.files.json ----

type upRecord struct {
	Version int               `json:"version"`
	Files   map[string]string `json:"files"`
}

func loadUpRecord(root string) (rec map[string]string, had bool, err error) {
	raw, err := os.ReadFile(filepath.Join(root, filesRecord))
	if errors.Is(err, fs.ErrNotExist) {
		return map[string]string{}, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	var r upRecord
	if err := json.Unmarshal(raw, &r); err != nil {
		return nil, false, fmt.Errorf("%s: %w", filesRecord, err)
	}
	rec = map[string]string{}
	for p, h := range r.Files {
		c, err := upCleanPath(p)
		if err != nil || strings.HasSuffix(c, "/") {
			return nil, false, fmt.Errorf("%s 含非法路径 %q", filesRecord, p)
		}
		rec[c] = h
	}
	return rec, true, nil
}

func upHashFile(p string) (string, error) {
	f, err := os.Open(p)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// ---- 源码包定位 ----

// upLocate 找到包含 manName 的目录：根目录，或唯一的顶层子目录（与 locatePackage 同思路）。
func upLocate(dir, manName string) (string, error) {
	if _, err := os.Stat(filepath.Join(dir, manName)); err == nil {
		return dir, nil
	}
	ents, err := os.ReadDir(dir)
	if err != nil {
		return "", err
	}
	var found []string
	for _, e := range ents {
		if e.IsDir() && e.Name() != "__MACOSX" {
			if _, err := os.Stat(filepath.Join(dir, e.Name(), manName)); err == nil {
				found = append(found, filepath.Join(dir, e.Name()))
			}
		}
	}
	if len(found) != 1 {
		return "", fmt.Errorf("源码包里找不到唯一的 %s", manName)
	}
	return found[0], nil
}

// upResolveSource 返回源码包目录；zip 先用 extractZip（含 zip-slip/大小限制）解压到临时目录。
func upResolveSource(from, manName string) (string, func(), error) {
	noop := func() {}
	st, err := os.Stat(from)
	if err != nil {
		return "", noop, err
	}
	if st.IsDir() {
		dir, err := upLocate(from, manName)
		return dir, noop, err
	}
	tmp, err := os.MkdirTemp("", "nexadmin-upgrade-")
	if err != nil {
		return "", noop, err
	}
	cleanup := func() { os.RemoveAll(tmp) } //nolint:errcheck
	if err := market.ExtractZip(from, tmp); err != nil {
		cleanup()
		return "", noop, err
	}
	dir, err := upLocate(tmp, manName)
	if err != nil {
		cleanup()
		return "", noop, err
	}
	return dir, cleanup, nil
}

// upCheckDest 确保写入目标不是符号链接，且其最近的已存在祖先目录解析后仍在工程根内。
func upCheckDest(rootReal, root, rel string) error {
	dst := filepath.Join(root, filepath.FromSlash(rel))
	if st, err := os.Lstat(dst); err == nil && st.Mode()&fs.ModeSymlink != 0 {
		return fmt.Errorf("目标是符号链接，拒绝覆盖: %s", rel)
	}
	anc := filepath.Dir(dst)
	for {
		if _, err := os.Lstat(anc); err == nil {
			break
		}
		anc = filepath.Dir(anc)
	}
	real, err := filepath.EvalSymlinks(anc)
	if err != nil {
		return err
	}
	r, err := filepath.Rel(rootReal, real)
	if err != nil || r == ".." || strings.HasPrefix(r, ".."+string(filepath.Separator)) {
		return fmt.Errorf("目标越出工程根（符号链接目录？）: %s", rel)
	}
	return nil
}

// ---- 计划 ----

type upOptions struct {
	From       string
	To         string
	Manifest   string
	DryRun     bool
	AllowDirty bool
}

type upChange struct {
	Path   string
	Kind   string // add | modify | delete | keep（仅提示：optionalManifestPaths 在包里缺失，保留现有文件）
	Warn   bool   // 框架文件被手改过
	Backup bool   // 覆盖前先存 .bak
	Note   string
	src    string
}

type upPlan struct {
	changes    []upChange
	unchanged  int
	newHashes  map[string]string // 新包里每个框架文件的哈希
	migrations []string
	merges     []upMerge // merge_paths 的合并结果（只含有变化的）
	hadRec     bool
}

func upReserved(rel, manName string) bool {
	return rel == filesRecord || rel == manName || rel == frameworkManifest ||
		rel == ".git" || strings.HasPrefix(rel, ".git/") || strings.HasPrefix(rel, trashDir+"/")
}

// upRewrite 在升级包的模块路径与当前工程不同时，把升级包 .go 文件的 import 前缀改写成当前工程的
// 模块路径（与 `new` 共用 rewriteImports）。改写结果写到临时目录，不动升级包本身；哈希与手改检测都基于改写后的内容。
type upRewrite struct{ from, to, tmp string }

func (r *upRewrite) apply(src, rel string) (string, error) {
	raw, err := os.ReadFile(src)
	if err != nil {
		return "", err
	}
	out, changed, err := rewriteImports(raw, src, r.from, r.to)
	if err != nil {
		return "", fmt.Errorf("解析 %s 失败: %w", rel, err)
	}
	if !changed {
		return src, nil
	}
	dst := filepath.Join(r.tmp, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return "", err
	}
	return dst, os.WriteFile(dst, out, 0o644)
}

// upBuildPlan 只读：比较新包、当前工程与哈希记录，得出变更清单。
func upBuildPlan(root, rootReal, pkg, manName string, man *upManifest, rec map[string]string, hadRec bool, rw *upRewrite) (*upPlan, error) {
	managed := man.managed()
	skip := func(rel string) bool {
		// newSkip：与 new / release pack 共用的排除集合（scripts/derive_check、.git、node_modules 等不下发）
		return !upMatch(rel, managed) || upMatch(rel, man.UserPaths) || upMatch(rel, man.MergePaths) ||
			upReserved(rel, manName) || newSkip(rel, false, true)
	}
	newFiles := map[string]string{}
	err := filepath.WalkDir(pkg, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if d.Name() == ".git" {
				return filepath.SkipDir
			}
			return nil
		}
		r, err := filepath.Rel(pkg, p)
		if err != nil {
			return err
		}
		rel := filepath.ToSlash(r)
		if skip(rel) {
			return nil
		}
		if !d.Type().IsRegular() {
			return fmt.Errorf("源码包含非常规文件（符号链接？）: %s", rel)
		}
		if rw != nil && strings.HasSuffix(rel, ".go") {
			if p, err = rw.apply(p, rel); err != nil {
				return err
			}
		}
		newFiles[rel] = p
		return nil
	})
	if err != nil {
		return nil, err
	}
	plan := &upPlan{newHashes: map[string]string{}, hadRec: hadRec}
	for rel, src := range newFiles {
		nh, err := upHashFile(src)
		if err != nil {
			return nil, err
		}
		plan.newHashes[rel] = nh
		if err := upCheckDest(rootReal, root, rel); err != nil {
			return nil, err
		}
		dst := filepath.Join(root, filepath.FromSlash(rel))
		if _, err := os.Lstat(dst); errors.Is(err, fs.ErrNotExist) {
			plan.changes = append(plan.changes, upChange{Path: rel, Kind: "add", src: src})
			if strings.HasPrefix(rel, migrationsPrefix) {
				plan.migrations = append(plan.migrations, rel)
			}
			continue
		}
		cur, err := upHashFile(dst)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", rel, err)
		}
		if cur == nh {
			plan.unchanged++
			continue
		}
		c := upChange{Path: rel, Kind: "modify", src: src, Backup: true}
		if old, ok := rec[rel]; ok && old != cur {
			c.Warn, c.Note = true, "被手改过，旧内容备份为 "+rel+".bak"
		} else if ok {
			c.Backup = false // 与记录一致：没人改过，直接覆盖
		} else {
			c.Note = "无哈希记录，按未知处理，旧内容备份为 " + rel + ".bak"
		}
		plan.changes = append(plan.changes, c)
	}
	for rel, old := range rec {
		if _, ok := newFiles[rel]; ok || skip(rel) {
			continue
		}
		if err := upCheckDest(rootReal, root, rel); err != nil {
			return nil, err
		}
		cur, err := upHashFile(filepath.Join(root, filepath.FromSlash(rel)))
		if errors.Is(err, fs.ErrNotExist) {
			continue // 已经不在磁盘上，只需从记录里清掉
		}
		if err != nil {
			return nil, fmt.Errorf("%s: %w", rel, err)
		}
		if optionalManifestPaths[rel] {
			// 发行方随升级包分发的文件（附加公钥、吊销清单）：新包里没有只表示这次没发，不是框架删除了它。
			// 移走会让此前下发的（或客户自己放的）吊销清单悄悄失效，所以保留现有文件，只在清单里提示。
			plan.changes = append(plan.changes, upChange{Path: rel, Kind: "keep", Note: "升级包未包含，保留现有文件"})
			continue
		}
		c := upChange{Path: rel, Kind: "delete", Note: "框架已删除，移入 " + trashDir + "/<时间>/"}
		if cur != old {
			c.Warn, c.Note = true, "框架已删除，但本地被手改过；移入 "+trashDir+"/<时间>/"
		}
		plan.changes = append(plan.changes, c)
	}
	sort.Slice(plan.changes, func(i, j int) bool { return plan.changes[i].Path < plan.changes[j].Path })
	sort.Strings(plan.migrations)
	return plan, nil
}

func upPrintPlan(out io.Writer, p *upPlan, dry bool) {
	if dry {
		fmt.Fprintln(out, "[dry-run] 以下为将发生的变更，未改动任何文件：")
	}
	tag := map[string]string{"add": "A", "modify": "M", "delete": "D", "keep": "K"}
	var a, m, d, k, w int
	for _, c := range p.changes {
		line := fmt.Sprintf("  %s  %s", tag[c.Kind], c.Path)
		if c.Warn {
			line += "  WARN: " + c.Note
			w++
		} else if c.Note != "" {
			line += "  (" + c.Note + ")"
		}
		fmt.Fprintln(out, line)
		switch c.Kind {
		case "add":
			a++
		case "modify":
			m++
		case "keep":
			k++
		default:
			d++
		}
	}
	if len(p.changes) == 0 && len(p.merges) == 0 {
		fmt.Fprintln(out, "  (没有文件变化)")
	}
	if len(p.merges) > 0 {
		fmt.Fprintln(out, "合并（merge_paths：合并而非覆盖，二开独有的依赖/脚本保留）：")
		upPrintMerges(out, p.merges)
	}
	if !p.hadRec {
		fmt.Fprintf(out, "提示: 未找到 %s（首次升级），与新包不同的文件一律备份，且无法识别框架删除的文件。\n", filesRecord)
	}
	if len(p.migrations) > 0 {
		fmt.Fprintln(out, "新增框架迁移（升级不执行，服务启动时由 runner 执行）：")
		for _, f := range p.migrations {
			fmt.Fprintln(out, "  +", f)
		}
	}
	fmt.Fprintf(out, "摘要: 新增 %d，修改 %d，删除 %d，未变 %d，WARN %d", a, m, d, p.unchanged, w)
	if k > 0 {
		fmt.Fprintf(out, "，保留 %d", k)
	}
	if len(p.merges) > 0 {
		fmt.Fprintf(out, "，合并 %d", len(p.merges))
	}
	fmt.Fprintln(out)
}

// ---- 执行 ----

func upBackupName(dst string) string {
	bak := dst + ".bak"
	for i := 1; market.Exists(bak); i++ {
		bak = fmt.Sprintf("%s.bak.%d", dst, i)
	}
	return bak
}

// upWriteFrom 先写同目录临时文件再 rename，避免半截文件。
func upWriteFrom(src, dst string) error {
	st, err := os.Stat(src)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(dst), ".nexadmin-up-")
	if err != nil {
		return err
	}
	in, err := os.Open(src)
	if err != nil {
		tmp.Close()           //nolint:errcheck
		os.Remove(tmp.Name()) //nolint:errcheck
		return err
	}
	defer in.Close()
	_, err = io.Copy(tmp, in)
	if cerr := tmp.Close(); err == nil {
		err = cerr
	}
	if err == nil {
		err = os.Chmod(tmp.Name(), st.Mode().Perm()|0o600)
	}
	if err == nil {
		err = os.Rename(tmp.Name(), dst)
	}
	if err != nil {
		os.Remove(tmp.Name()) //nolint:errcheck
	}
	return err
}

func upApply(root string, p *upPlan, rec map[string]string, man *upManifest, now time.Time) error {
	trash := filepath.Join(root, trashDir, now.Format("20060102-150405"))
	for _, c := range p.changes {
		dst := filepath.Join(root, filepath.FromSlash(c.Path))
		switch c.Kind {
		case "add", "modify":
			if c.Backup {
				if err := market.CopyFile(dst, upBackupName(dst)); err != nil {
					return fmt.Errorf("备份 %s 失败: %w", c.Path, err)
				}
			}
			if err := upWriteFrom(c.src, dst); err != nil {
				return fmt.Errorf("写入 %s 失败: %w", c.Path, err)
			}
		case "delete":
			t := filepath.Join(trash, filepath.FromSlash(c.Path))
			if err := os.MkdirAll(filepath.Dir(t), 0o755); err != nil {
				return err
			}
			if err := os.Rename(dst, t); err != nil {
				return fmt.Errorf("移入回收目录失败 %s: %w", c.Path, err)
			}
		}
	}
	// 更新哈希记录：清掉本层已不在新包的条目，写入新包所有文件的哈希；其他层的条目原样保留。
	// 例外：包里缺失而被保留的可选文件（keep）仍在磁盘上，记录原样保留，下次它再随包下发时才能正确判断是否被手改。
	kept := map[string]string{}
	for _, c := range p.changes {
		if c.Kind == "keep" {
			kept[c.Path] = rec[c.Path]
		}
	}
	for rel := range rec {
		if upMatch(rel, man.managed()) && !upMatch(rel, man.UserPaths) {
			delete(rec, rel)
		}
	}
	for rel, h := range p.newHashes {
		rec[rel] = h
	}
	for rel, h := range kept {
		rec[rel] = h
	}
	raw, err := json.MarshalIndent(upRecord{Version: 1, Files: rec}, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(root, filesRecord), append(raw, '\n'), 0o644)
}

// upSyncManifest 把版本号写进工程根的清单（保留原有排版）；清单不存在则直接放入包里的那份。
func upSyncManifest(rootPath, pkgPath, key, value string) error {
	cur, err := os.ReadFile(rootPath)
	if errors.Is(err, fs.ErrNotExist) {
		raw, rerr := os.ReadFile(pkgPath)
		if rerr != nil {
			return rerr
		}
		return os.WriteFile(rootPath, raw, 0o644)
	}
	if err != nil {
		return err
	}
	re := regexp.MustCompile(`("` + key + `"\s*:\s*)"[^"]*"`)
	if re.Match(cur) {
		nw := re.ReplaceAll(cur, []byte(`${1}`+strconv.Quote(value)))
		return os.WriteFile(rootPath, nw, 0o644)
	}
	var m map[string]json.RawMessage
	if err := json.Unmarshal(cur, &m); err != nil {
		return err
	}
	m[key], _ = json.Marshal(value)
	nw, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(rootPath, append(nw, '\n'), 0o644)
}

// ---- 入口 ----

func cmdUpgrade(args []string, out io.Writer) error {
	fl := newFlags("upgrade")
	var o upOptions
	fl.StringVar(&o.From, "from", "", "新版本源码包（目录或 zip）")
	fl.StringVar(&o.To, "to", "", "目标框架版本；必须与本地升级包清单一致")
	fl.StringVar(&o.Manifest, "manifest", frameworkManifest, "清单文件（框架或产品）")
	fl.BoolVar(&o.DryRun, "dry-run", false, "只列出变更，不改任何东西")
	fl.BoolVar(&o.AllowDirty, "allow-dirty", false, "git 工作区不干净时也继续")
	if err := fl.Parse(args); err != nil {
		return err
	}
	if o.From == "" || fl.NArg() != 0 {
		return errors.New("用法: nexadmin upgrade --from <目录或zip> [--to <版本>] [--dry-run] [--manifest .nexadmin.manifest.json] [--allow-dirty]")
	}
	root, err := findRoot()
	if err != nil {
		return err
	}
	return upgrade(root, o, out, time.Now())
}

func upgrade(root string, o upOptions, out io.Writer, now time.Time) error {
	rootReal, err := filepath.EvalSymlinks(root)
	if err != nil {
		return err
	}
	manName := filepath.Base(o.Manifest)
	rootMan := o.Manifest
	if !filepath.IsAbs(rootMan) {
		rootMan = filepath.Join(root, rootMan)
	}
	if !o.AllowDirty {
		isRepo, dirty, err := upGitState(root)
		switch {
		case err != nil:
			return err
		case !isRepo:
			fmt.Fprintln(out, "警告: 不是 git 仓库，无法检查工作区，升级后将无法用 git 回滚。")
		case dirty && o.DryRun:
			fmt.Fprintln(out, "警告: git 工作区有未提交改动（dry-run 继续；正式升级会拒绝，除非 --allow-dirty）。")
		case dirty:
			return errors.New("git 工作区不干净，拒绝升级：请先 commit 或 stash（或明确使用 --allow-dirty）")
		}
	}
	pkg, cleanup, err := upResolveSource(o.From, manName)
	if err != nil {
		return err
	}
	defer cleanup()
	man, err := loadUpManifest(filepath.Join(pkg, manName))
	if err != nil {
		return err
	}
	if o.To != "" {
		if man.isProduct() {
			return errors.New("--to 仅适用于框架升级包；产品升级请使用 --manifest 指定产品清单并校验 based_on_framework")
		}
		if man.FrameworkVersion != o.To {
			return fmt.Errorf("升级包版本为 %s，与 --to %s 不一致", man.FrameworkVersion, o.To)
		}
	}
	if man.isProduct() {
		raw, err := os.ReadFile(filepath.Join(root, frameworkManifest))
		if err != nil {
			return fmt.Errorf("读取当前框架清单失败（产品升级需要 framework_version）: %w", err)
		}
		var cur upManifest
		if err := json.Unmarshal(raw, &cur); err != nil || cur.FrameworkVersion == "" {
			return fmt.Errorf("%s 缺少 framework_version", frameworkManifest)
		}
		ok, err := versionSatisfies(cur.FrameworkVersion, man.BasedOnFramework)
		if err != nil {
			return err
		}
		if !ok {
			return fmt.Errorf("产品 %s %s 要求框架版本 %q，当前工程 framework_version 为 %s，不兼容，拒绝升级（请先升级框架，或使用兼容的产品包）",
				man.ProductName, man.ProductVersion, man.BasedOnFramework, cur.FrameworkVersion)
		}
	}
	rec, hadRec, err := loadUpRecord(root)
	if err != nil {
		return err
	}
	var rw *upRewrite
	if from, e1 := market.ModulePath(pkg); e1 == nil {
		if to, e2 := market.ModulePath(root); e2 == nil && from != to {
			tmp, err := os.MkdirTemp("", "nexadmin-upgrade-rw-")
			if err != nil {
				return err
			}
			defer os.RemoveAll(tmp) //nolint:errcheck
			rw = &upRewrite{from: from, to: to, tmp: tmp}
			fmt.Fprintf(out, "升级包模块路径 %s 与当前工程 %s 不同，比较前改写 .go 文件的 import。\n", from, to)
		}
	}
	plan, err := upBuildPlan(root, rootReal, pkg, manName, man, rec, hadRec, rw)
	if err != nil {
		return err
	}
	if plan.merges, err = upPlanMerges(root, rootReal, pkg, man); err != nil {
		return err
	}
	if man.isProduct() {
		fmt.Fprintf(out, "产品升级: %s %s（基于框架 %s）\n", man.ProductName, man.ProductVersion, man.BasedOnFramework)
	} else {
		fmt.Fprintf(out, "框架升级 -> %s\n", man.FrameworkVersion)
	}
	upPrintPlan(out, plan, o.DryRun)
	if o.DryRun {
		return nil
	}
	key, val := "framework_version", man.FrameworkVersion
	if man.isProduct() {
		key, val = "product_version", man.ProductVersion
	}
	if err := upTransaction(root, rootMan, filepath.Join(pkg, manName), key, val, plan, rec, man, now); err != nil {
		return err
	}
	fmt.Fprintf(out, "升级完成: %s 已更新为 %s。迁移由服务启动时的 runner 执行；用 git diff 查看变更。\n", key, val)
	if len(plan.merges) > 0 {
		fmt.Fprintln(out, "已合并依赖清单：请运行 go mod tidy 与 (cd web && npm install)；web/package-lock.json 属用户区，未被覆盖。")
	}
	return nil
}
