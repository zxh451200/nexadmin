package main

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"
)

// upMerge 是一个 merge_paths 文件的合并结果（只收有变化的）。
type upMerge struct {
	Path    string
	Kind    string // add | merge
	Content []byte
	Report  []string
}

// upMergeFile 按文件名分派合并器；不支持的类型直接报错，避免静默漏合。
func upMergeFile(rel string, cur, nw []byte) ([]byte, []string, bool, error) {
	switch path.Base(rel) {
	case "go.mod":
		return mergeGoMod(cur, nw)
	case "go.sum":
		return mergeGoSum(cur, nw)
	case "package.json":
		return mergePackageJSON(cur, nw)
	}
	return nil, nil, false, fmt.Errorf("merge_paths 不支持合并 %s（仅 go.mod、go.sum、package.json）", rel)
}

// upPlanMerges 只读：对每个 merge_paths 文件算出合并结果；新包没有该文件则跳过，当前工程没有则直接加入。
func upPlanMerges(root, rootReal, pkg string, man *upManifest) ([]upMerge, error) {
	var out []upMerge
	for _, rel := range man.MergePaths {
		if strings.HasSuffix(rel, "/") {
			return nil, fmt.Errorf("merge_paths 只能列文件，不能列目录: %s", rel)
		}
		if upMatch(rel, man.UserPaths) {
			continue // 用户区永不触碰
		}
		nw, err := os.ReadFile(filepath.Join(pkg, filepath.FromSlash(rel)))
		if errors.Is(err, fs.ErrNotExist) {
			continue
		}
		if err != nil {
			return nil, err
		}
		if err := upCheckDest(rootReal, root, rel); err != nil {
			return nil, err
		}
		cur, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
		if errors.Is(err, fs.ErrNotExist) {
			out = append(out, upMerge{Path: rel, Kind: "add", Content: nw, Report: []string{"当前工程没有该文件，直接加入"}})
			continue
		}
		if err != nil {
			return nil, err
		}
		merged, rep, changed, err := upMergeFile(rel, cur, nw)
		if err != nil {
			return nil, fmt.Errorf("合并 %s 失败: %w", rel, err)
		}
		if changed {
			out = append(out, upMerge{Path: rel, Kind: "merge", Content: merged, Report: rep})
		}
	}
	return out, nil
}

func upPrintMerges(out io.Writer, ms []upMerge) {
	for _, m := range ms {
		label := "合并"
		if m.Kind == "add" {
			label = "新增"
		}
		fmt.Fprintf(out, "  ~  %s  (%s)\n", m.Path, label)
		for _, l := range m.Report {
			fmt.Fprintln(out, "       "+l)
		}
	}
}

func upApplyMerges(root string, ms []upMerge) error {
	for _, m := range ms {
		dst := filepath.Join(root, filepath.FromSlash(m.Path))
		if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
			return err
		}
		mode := os.FileMode(0o644)
		if st, err := os.Stat(dst); err == nil {
			mode = st.Mode().Perm()
		}
		tmp, err := os.CreateTemp(filepath.Dir(dst), ".nexadmin-merge-")
		if err != nil {
			return err
		}
		_, err = tmp.Write(m.Content)
		if cerr := tmp.Close(); err == nil {
			err = cerr
		}
		if err == nil {
			err = os.Chmod(tmp.Name(), mode|0o600)
		}
		if err == nil {
			err = os.Rename(tmp.Name(), dst)
		}
		if err != nil {
			os.Remove(tmp.Name()) //nolint:errcheck
			return fmt.Errorf("写入合并结果 %s 失败: %w", m.Path, err)
		}
	}
	return nil
}
