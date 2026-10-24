// contract_check 是前后端契约静态核对工具（仅框架仓库使用；nexadmin new / release pack 不下发）。
//
// 把前端调用的接口（api.get/post/... 、RemoteSelect url、上传固定路径）与后端 go/ast 解析出的路由注册逐一对照，
// 不依赖数据库、浏览器，也不 import 项目业务包。
//
//	go run ./scripts/contract_check [--root DIR] [--json] [--warn-only] [--out FILE]
//
// 退出码：A 类（前端调用了但后端没有）非空返回 1；--warn-only 降为 0；参数错误或扫描失败返回 2。
package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

// apiPrefix 与 core/router.go 里 e.Group("/api/v1") 的挂载点一致。
const apiPrefix = "/api/v1"

// Loc 是源码位置（相对仓库根，正斜杠）。
type Loc struct {
	File string `json:"file"`
	Line int    `json:"line"`
}

func (l Loc) String() string { return fmt.Sprintf("%s:%d", l.File, l.Line) }

// Route 是后端注册的一条路由。Dynamic 表示路径含无法静态解析的段（?）或通配（*），不参与匹配。
type Route struct {
	Method  string `json:"method"`
	Path    string `json:"path"`
	Module  string `json:"module"`
	Perm    string `json:"perm,omitempty"`
	Dynamic bool   `json:"dynamic,omitempty"`
	Loc
}

// Call 是前端的一次接口调用，或前端 mock 声明的一条路由。
type Call struct {
	Method string `json:"method"`
	Path   string `json:"path"`
	Module string `json:"module"`
	Kind   string `json:"kind"` // api / remote / upload / fetch / mock
	Loc
}

// Unresolved 是静态分析解析不了的条目，单独列出，避免漏看。
type Unresolved struct {
	Side   string `json:"side"` // backend / frontend / mock
	Reason string `json:"reason"`
	Text   string `json:"text"`
	Loc
}

func main() { os.Exit(run(os.Args[1:], os.Stdout, os.Stderr)) }

func run(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("contract_check", flag.ContinueOnError)
	fs.SetOutput(stderr)
	root := fs.String("root", "", "仓库根目录（默认自动向上查找含 go.mod 与 web/src 的目录）")
	asJSON := fs.Bool("json", false, "输出 JSON")
	warnOnly := fs.Bool("warn-only", false, "A 类非空时也返回 0")
	out := fs.String("out", "", "同时把报告写到该文件")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	dir := *root
	if dir == "" {
		dir = findRoot()
	}
	rep, err := Check(dir)
	if err != nil {
		fmt.Fprintln(stderr, "contract_check:", err)
		return 2
	}
	var buf bytes.Buffer
	if *asJSON {
		enc := json.NewEncoder(&buf)
		enc.SetIndent("", "  ")
		_ = enc.Encode(rep)
	} else {
		writeText(&buf, rep)
	}
	_, _ = stdout.Write(buf.Bytes())
	if *out != "" {
		if err := os.WriteFile(*out, buf.Bytes(), 0o644); err != nil {
			fmt.Fprintln(stderr, "contract_check:", err)
			return 2
		}
	}
	if len(rep.A) > 0 && !*warnOnly {
		return 1
	}
	return 0
}

// findRoot 从当前目录向上找同时含 go.mod 与 web/src 的目录。
func findRoot() string {
	wd, err := os.Getwd()
	if err != nil {
		return "."
	}
	for d := wd; ; d = filepath.Dir(d) {
		_, e1 := os.Stat(filepath.Join(d, "go.mod"))
		_, e2 := os.Stat(filepath.Join(d, "web", "src"))
		if e1 == nil && e2 == nil {
			return d
		}
		if filepath.Dir(d) == d {
			return wd
		}
	}
}
