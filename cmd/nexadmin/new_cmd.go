package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// newManifest 是新项目的框架清单：保留 framework_version 与路径分区，另记来源模板版本与项目名。
type newManifest struct {
	FrameworkVersion string   `json:"framework_version"`
	DerivedFrom      string   `json:"derived_from"`
	ProjectName      string   `json:"project_name"`
	FrameworkPaths   []string `json:"framework_paths"`
	MergePaths       []string `json:"merge_paths,omitempty"`
	UserPaths        []string `json:"user_paths"`
}

func writeJSON(path string, v any) error {
	raw, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(raw, '\n'), 0o644)
}

// writeNewManifest 重写新项目的清单，返回读到的模板清单。
func writeNewManifest(root, name string) (*upManifest, error) {
	man, err := loadUpManifest(filepath.Join(root, frameworkManifest))
	if err != nil {
		return nil, err
	}
	if man.isProduct() {
		return nil, errors.New("当前工程的清单是产品清单，new 只能在框架模板工程根运行")
	}
	nm := newManifest{
		FrameworkVersion: man.FrameworkVersion,
		DerivedFrom:      man.FrameworkVersion,
		ProjectName:      name,
		FrameworkPaths:   man.FrameworkPaths,
		MergePaths:       man.MergePaths,
		UserPaths:        man.UserPaths,
	}
	return man, writeJSON(filepath.Join(root, frameworkManifest), nm)
}

// writeBaseline 记录新项目当前框架区文件的 sha256，格式与 upgrade 的 .nexadmin.files.json 一致。
func writeBaseline(root string, man *upManifest) (int, error) {
	files := map[string]string{}
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if d.Name() == ".git" {
				return filepath.SkipDir
			}
			return nil
		}
		r, err := filepath.Rel(root, p)
		if err != nil {
			return err
		}
		rel := filepath.ToSlash(r)
		if !upMatch(rel, man.managed()) || upMatch(rel, man.UserPaths) || upReserved(rel, frameworkManifest) {
			return nil
		}
		if !d.Type().IsRegular() {
			return nil
		}
		h, err := upHashFile(p)
		if err != nil {
			return err
		}
		files[rel] = h
		return nil
	})
	if err != nil {
		return 0, err
	}
	return len(files), writeJSON(filepath.Join(root, filesRecord), upRecord{Version: 1, Files: files})
}

// gitInit 在新目录 init 并提交初始版本；任何失败只返回错误供调用方警告。
func gitInit(root, version string) error {
	run := func(args ...string) error {
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()
		c := exec.CommandContext(ctx, "git", args...)
		c.Dir = root
		var buf bytes.Buffer
		c.Stderr = &buf
		if err := c.Run(); err != nil {
			return fmt.Errorf("git %s: %w %s", args[0], err, strings.TrimSpace(buf.String()))
		}
		return nil
	}
	if err := run("init", "-q"); err != nil {
		return err
	}
	if err := run("add", "-A"); err != nil {
		return err
	}
	return run("commit", "-q", "-m", "chore: 由 nexadmin "+version+" 派生的初始版本")
}

// ---- 入口 ----

func cmdNew(args []string, out io.Writer) error {
	fl := newFlags("new")
	var o newOptions
	fl.StringVar(&o.Out, "out", "", "输出目录（默认 ../<名称>）")
	fl.StringVar(&o.Module, "module", "", "Go 模块路径（默认 <名称>）")
	fl.BoolVar(&o.WithPlugins, "with-plugins", false, "连同已安装的插件一起带上")
	fl.BoolVar(&o.Git, "git", false, "在新目录 git init 并提交初始版本")
	// 允许名称写在选项前后：先取出第一个非选项参数
	var pos []string
	rest := args
	for len(rest) > 0 {
		if err := fl.Parse(rest); err != nil {
			return err
		}
		if fl.NArg() == 0 {
			break
		}
		pos = append(pos, fl.Arg(0))
		rest = fl.Args()[1:]
	}
	if len(pos) != 1 {
		return errors.New("用法: nexadmin new <名称> [--out <目录>] [--module <go模块路径>] [--with-plugins] [--git]")
	}
	o.Name = pos[0]
	root, err := findRoot()
	if err != nil {
		return err
	}
	return newProject(root, o, out)
}

func newProject(root string, o newOptions, out io.Writer) error {
	if !projectNameRe.MatchString(o.Name) {
		return fmt.Errorf("非法的项目名称 %q（字母数字开头，只含字母数字 . _ -）", o.Name)
	}
	if o.Module == "" {
		o.Module = o.Name
	}
	if strings.ContainsAny(o.Module, " \t\r\n\"'`\\") || strings.HasPrefix(o.Module, "/") {
		return fmt.Errorf("非法的 Go 模块路径 %q", o.Module)
	}
	if o.Out == "" {
		o.Out = filepath.Join(filepath.Dir(root), o.Name)
	}
	dst, err := filepath.Abs(o.Out)
	if err != nil {
		return err
	}
	if rel, err := filepath.Rel(root, dst); err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return fmt.Errorf("输出目录不能在模板工程内部: %s", dst)
	}
	if st, err := os.Stat(dst); err == nil {
		if !st.IsDir() {
			return fmt.Errorf("输出路径已存在且不是目录: %s", dst)
		}
		ents, err := os.ReadDir(dst)
		if err != nil {
			return err
		}
		if len(ents) > 0 {
			return fmt.Errorf("目标目录已存在且非空，拒绝覆盖: %s", dst)
		}
	} else if !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	if _, err := loadUpManifest(filepath.Join(root, frameworkManifest)); err != nil {
		return fmt.Errorf("读取模板清单失败: %w", err)
	}
	if err := os.MkdirAll(dst, 0o755); err != nil {
		return err
	}
	n, err := copyTemplate(root, dst, o.WithPlugins)
	if err != nil {
		return fmt.Errorf("拷贝模板失败（%s 可能已部分生成，请删除后重试）: %w", dst, err)
	}
	if !o.WithPlugins {
		p := filepath.Join(dst, "plugins", "plugins.go")
		raw, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		nw, err := clearPluginImports(raw)
		if err != nil {
			return err
		}
		if err := os.WriteFile(p, nw, 0o644); err != nil {
			return err
		}
	}
	nGo, err := rewriteModulePath(dst, o.Module)
	if err != nil {
		return err
	}
	if err := rewritePackageName(dst, o.Name); err != nil {
		return err
	}
	for _, k := range []string{"internal/modules", "web/src/pages", "migrations/business", "data", "logs"} {
		d := filepath.Join(dst, filepath.FromSlash(k))
		if err := os.MkdirAll(d, 0o755); err != nil {
			return err
		}
		if err := touch(filepath.Join(d, ".gitkeep")); err != nil {
			return err
		}
	}
	for _, f := range []string{"AGENTS.md", "CLAUDE.md"} { // CLAUDE.md 用副本，跨平台/打包不丢
		if err := os.WriteFile(filepath.Join(dst, f), []byte(agentsMD), 0o644); err != nil {
			return err
		}
	}
	man, err := writeNewManifest(dst, o.Name)
	if err != nil {
		return err
	}
	nBase, err := writeBaseline(dst, man)
	if err != nil {
		return err
	}
	fmt.Fprintf(out, "已生成项目 %s（%d 个文件，改写 %d 个 .go 的 import，基线 %d 个框架文件）\n", dst, n, nGo, nBase)
	if o.Git {
		if err := gitInit(dst, man.FrameworkVersion); err != nil {
			fmt.Fprintln(out, "警告: git 初始化失败（项目已生成，可手动处理）:", err)
		} else {
			fmt.Fprintln(out, "已 git init 并提交初始版本")
		}
	}
	fmt.Fprintf(out, "\n下一步:\n  cd %s\n  cd web && npm install\n  cd .. && go run ./cmd/server\n  浏览器打开 http://localhost:8080 完成初始化向导（端口以 config 为准）\n", dst)
	return nil
}

func touch(p string) error {
	if exists(p) {
		return nil
	}
	return os.WriteFile(p, nil, 0o644)
}
