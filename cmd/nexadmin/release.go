package main

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// 发布前检查：把"上线前必须做、容易忘"的事变成机器能查的项。
// 用法：nexadmin release check [--strict]
// FAIL 一律让命令返回非零（可接进 CI / 打包脚本）；--strict 时 WARN 也算失败。

type relItem struct {
	level string // FAIL / WARN
	msg   string
}

func cmdRelease(args []string, out io.Writer) error {
	if len(args) > 0 && args[0] == "pack" {
		return cmdReleasePack(args[1:], out)
	}
	if len(args) == 0 || args[0] != "check" {
		return errors.New("用法: nexadmin release check [--strict] | nexadmin release pack [--out <路径.zip>] [--force]")
	}
	strict := false
	for _, a := range args[1:] {
		if a == "--strict" {
			strict = true
		}
	}
	root, err := findRoot()
	if err != nil {
		return err
	}
	items := releaseChecks(root)

	fails, warns := 0, 0
	for _, it := range items {
		fmt.Fprintf(out, "[%s] %s\n", it.level, it.msg)
		if it.level == "FAIL" {
			fails++
		} else {
			warns++
		}
	}
	if len(items) == 0 {
		fmt.Fprintln(out, "自动检查全部通过。")
	}
	fmt.Fprintln(out, "\n以下是工具查不出来、需要人工确认的事：")
	for _, m := range releaseManual {
		fmt.Fprintln(out, "  [ ] "+m)
	}
	if fails > 0 || (strict && warns > 0) {
		return fmt.Errorf("发布前检查未通过：%d 项 FAIL，%d 项 WARN", fails, warns)
	}
	return nil
}

// releaseManual 是自动检查覆盖不到的事项，每次发布都打印，防止遗忘。
var releaseManual = []string{
	"正式授权私钥已生成（plugin keygen），加密备份过，并且只放在发行方自己的机器上",
	"吊销清单（config/license-revoked.json，由 plugin revoke 或授权后台导出）的分发流程已落实：每次发布升级包前放入最新清单，且 .nexadmin.manifest.json 的 framework_paths 已包含该文件，否则升级不会下发；注意源码交付下买家可以删除清单，这只是辅助手段，不是强保护",
	"在一台干净机器上完整走过一遍：nexadmin new → 构建 → deploy/ctl 启动 → 初始化向导 → reinit",
	"三种数据库（mysql / mariadb / postgres）的迁移与种子都在真实库上跑过",
	"升级包用 nexadmin upgrade 在 new 出来的项目上实际升级过一次",
	"框架版本号、升级说明（没有手动步骤就写「无」）已更新",
	"go mod tidy 在有网环境执行过，go.mod 里不再有错误的 // indirect",
	"飞书登录图标目前用 IconPark 的 new-lark 单色图标；若要彩色官方标志，从飞书开放平台品牌资源下载 SVG 替换 BrandIcon.tsx 的 FeishuIcon",
}

func releaseChecks(root string) []relItem {
	var items []relItem
	add := func(level, format string, a ...any) { items = append(items, relItem{level, fmt.Sprintf(format, a...)}) }
	exists := func(rel string) bool { _, err := os.Stat(filepath.Join(root, rel)); return err == nil }

	// 1. 内置公钥集合里有开发公钥 —— 用开发私钥签的授权谁都能伪造
	if err := checkReleaseTargetKeys(root); err != nil {
		add("FAIL", "目标源码公钥检查失败：%v", err)
	}
	// 2. 开发私钥 / 演示授权不能随源码发出去
	if exists(".dev-keys") {
		add("FAIL", ".dev-keys/ 存在（开发私钥），发布源码包前必须删除")
	}
	if ls, _ := filepath.Glob(filepath.Join(root, "data", "licenses", "*.lic")); len(ls) > 0 {
		add("FAIL", "data/licenses/ 下有 %d 个授权文件（演示授权），发布前必须清掉", len(ls))
	}
	// 3. 生产前端不能带 mock：前端只有 VITE_MOCK=true 才启用 mock（未设置即关闭），这里仍要求 .env.production 显式写 false，防止被覆盖
	if err := checkReleaseMockEnv(root); err != nil {
		add("FAIL", "生产 mock 配置检查失败：%v", err)
	}

	// 4. 本机配置不能随包发出
	if exists(filepath.Join("config", "config.yaml")) {
		add("FAIL", "config/config.yaml 存在（含数据库密码等），发布前必须删除")
	}
	// 5. 数据与日志目录里不应有内容
	for _, d := range []string{"data", "logs"} {
		if es, _ := os.ReadDir(filepath.Join(root, d)); hasRealFiles(es) {
			add("WARN", "%s/ 下有运行产生的文件，发布前建议清空", d)
		}
	}
	// 6. 模板里带着已安装的付费插件
	if es, _ := os.ReadDir(filepath.Join(root, "plugins")); hasDirs(es) {
		add("FAIL", "plugins/ 下有已安装的插件目录；模板发布包里不能带付费插件，发布前请移除（nexadmin new 默认会排除）")
	}
	// 7. 版本还在 0.x
	if b, err := os.ReadFile(filepath.Join(root, ".nexadmin.manifest.json")); err == nil && strings.Contains(string(b), `"framework_version": "0.`) {
		add("WARN", "framework_version 仍是 0.x，确认这是预期的")
	}
	// 8. 清单里列出的路径必须真实存在（防拼写漏项：写错的路径升级时静默匹配不到任何文件）
	if man, err := loadUpManifest(filepath.Join(root, frameworkManifest)); err == nil {
		miss, missGlob := missingManifestPaths(root, man)
		for _, m := range miss {
			add("FAIL", "%s 清单里的路径 %q 在仓库里不存在（拼写错误或文件已删除但清单没改）", frameworkManifest, m)
		}
		for _, m := range missGlob { // 通配项可以是“预留”（如 tsconfig.*.json 当前没有匹配文件），只提醒
			add("WARN", "%s 清单里的通配路径 %q 当前没有匹配到任何文件，确认不是拼写错误", frameworkManifest, m)
		}
	}
	// 9. go.mod 不能留本地路径 replace：别人拉到源码就编译不过
	for _, r := range localReplaces(filepath.Join(root, "go.mod")) {
		add("FAIL", "go.mod 里有本地路径 replace：%s；发布前改成正式版本", r)
	}
	// 10. 残留 .bak / .orig（upgrade 备份、合并冲突遗留）会被一起打进包
	if left := leftoverFiles(root); len(left) > 0 {
		show := left
		if len(show) > 5 {
			show = show[:5]
		}
		add("FAIL", "存在残留的 .bak/.orig 文件（共 %d 个，如 %s）；确认无用后删除", len(left), strings.Join(show, ", "))
	}
	// 11. 构建产物：web/dist 与 bin 是本机产物，发布前清掉免得被误带（new / pack 本身会排除 web/dist）
	for _, d := range []string{"web/dist", "bin"} {
		if exists(d) {
			add("WARN", "%s/ 存在（本机构建产物），发布前建议删除", d)
		}
	}
	return items
}

// optionalManifestPaths 是清单里允许暂时不存在的路径：由发行方在发版时才放入的文件
// （附加公钥、吊销清单）。它们列在 framework_paths 里是为了让升级包能把它们分发出去，没有时不算拼写错误。
var optionalManifestPaths = map[string]bool{
	"config/license-keys.pub":     true,
	"config/license-revoked.json": true,
}

// missingManifestPaths 返回 managed + merge 路径中在 root 下找不到的项：精确路径一类，通配（* ? [，不跨目录）一类。
func missingManifestPaths(root string, m *upManifest) (miss, missGlob []string) {
	for _, p := range append(append([]string{}, m.managed()...), m.MergePaths...) {
		if optionalManifestPaths[p] {
			continue
		}
		rel := strings.TrimSuffix(filepath.FromSlash(p), string(filepath.Separator))
		if strings.ContainsAny(rel, "*?[") {
			if ms, _ := filepath.Glob(filepath.Join(root, rel)); len(ms) == 0 {
				missGlob = append(missGlob, p)
			}
		} else if _, err := os.Stat(filepath.Join(root, rel)); err != nil {
			miss = append(miss, p)
		}
	}
	return miss, missGlob
}

// localReplaces 解析 go.mod 里 replace ... => <本地路径>（./ ../ 绝对路径 / Windows 盘符）的行，单行与块写法都认。
func localReplaces(gomod string) []string {
	b, err := os.ReadFile(gomod)
	if err != nil {
		return nil
	}
	var out []string
	inBlock := false
	for _, line := range strings.Split(string(b), "\n") {
		if i := strings.Index(line, "//"); i >= 0 {
			line = line[:i]
		}
		line = strings.TrimSpace(line)
		switch {
		case inBlock && line == ")":
			inBlock = false
			continue
		case line == "replace (":
			inBlock = true
			continue
		case strings.HasPrefix(line, "replace "):
			line = strings.TrimSpace(strings.TrimPrefix(line, "replace "))
		case !inBlock:
			continue
		}
		_, rhs, ok := strings.Cut(line, "=>")
		if !ok {
			continue
		}
		f := strings.Fields(rhs)
		if len(f) == 0 {
			continue
		}
		t := f[0]
		if strings.HasPrefix(t, "./") || strings.HasPrefix(t, "../") || strings.HasPrefix(t, "/") ||
			strings.HasPrefix(t, ".\\") || strings.HasPrefix(t, "..\\") || (len(t) >= 2 && t[1] == ':') || t == "." || t == ".." {
			out = append(out, strings.TrimSpace(line))
		}
	}
	return out
}

// leftoverFiles 列出 root 下的 *.bak / *.orig（相对路径，正斜杠）；跳过 .git、node_modules、.nexadmin-trash。
func leftoverFiles(root string) []string {
	var out []string
	_ = filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			if n := d.Name(); n == ".git" || n == "node_modules" || n == trashDir {
				return filepath.SkipDir
			}
			return nil
		}
		if n := d.Name(); strings.HasSuffix(n, ".bak") || strings.HasSuffix(n, ".orig") {
			r, _ := filepath.Rel(root, p)
			out = append(out, filepath.ToSlash(r))
		}
		return nil
	})
	return out
}

func hasRealFiles(es []os.DirEntry) bool {
	for _, e := range es {
		if e.Name() != ".gitkeep" {
			return true
		}
	}
	return false
}

func hasDirs(es []os.DirEntry) bool {
	for _, e := range es {
		if e.IsDir() {
			return true
		}
	}
	return false
}
