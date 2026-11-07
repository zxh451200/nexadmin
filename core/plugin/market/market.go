// Package market 实现插件市场：目录解析、GitHub 下载、安装/移除、重建与自重启。
//
// 安装核心逻辑（校验清单、防 zip 穿越解压、backend/+web/ 拷贝、plugins.go 空导入维护、
// 数据库清理）与 `nexadmin plugin install|remove` 命令共用，由本包提供；
// CLI（cmd/nexadmin）与后台接口（modules/plugin）都只是它的调用方。
package market

import (
	"archive/zip"
	"bytes"
	"errors"
	"fmt"
	"go/format"
	"go/parser"
	"go/token"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"gorm.io/gorm"

	"nexadmin/core"
	"nexadmin/core/ctl"
	"nexadmin/core/database"
	"nexadmin/core/migrate"
	"nexadmin/core/plugin"
)

const (
	markBegin = "// nexadmin:plugins:begin"
	markEnd   = "// nexadmin:plugins:end"

	maxZipFiles = 20000
	maxZipTotal = 256 << 20 // 解压后总大小上限，防压缩炸弹
)

// installWarn 是 install 的警告输出（测试可替换）。
var installWarn io.Writer = os.Stderr

func modulePath(root string) (string, error) {
	raw, err := os.ReadFile(filepath.Join(root, "go.mod"))
	if err != nil {
		return "", err
	}
	for _, line := range strings.Split(string(raw), "\n") {
		if f := strings.Fields(line); len(f) == 2 && f[0] == "module" {
			return strings.Trim(f[1], `"`), nil
		}
	}
	return "", errors.New("go.mod 里没有 module 行")
}

var importRe = regexp.MustCompile(`^\s*_\s+"([^"]+)"\s*(//.*)?$`)

// updatePluginsFile 重写两个标记行之间的空导入：add=true 加入，false 移除；结果按路径排序、去重。
func updatePluginsFile(src []byte, importPath string, add bool) ([]byte, error) {
	lines := strings.Split(string(src), "\n")
	begin, end := -1, -1
	for i, l := range lines {
		switch strings.TrimSpace(l) {
		case markBegin:
			if begin >= 0 {
				return nil, errors.New("plugins.go: 起始标记行重复")
			}
			begin = i
		case markEnd:
			end = i
		}
	}
	if begin < 0 || end < 0 || end <= begin {
		return nil, fmt.Errorf("plugins.go: 缺少标记行 %q / %q（或顺序不对）", markBegin, markEnd)
	}
	set := map[string]bool{}
	for _, l := range lines[begin+1 : end] {
		if m := importRe.FindStringSubmatch(l); m != nil {
			set[m[1]] = true
		}
	}
	if add {
		set[importPath] = true
	} else {
		delete(set, importPath)
	}
	paths := make([]string, 0, len(set))
	for p := range set {
		paths = append(paths, p)
	}
	sort.Strings(paths)
	// gofmt 的规则：块内没有导入时注释顶格，有导入时全部缩进一个 tab。这里按它输出，保证文件始终是 gofmt 稳定的。
	indent := ""
	if len(paths) > 0 {
		indent = "\t"
	}
	out := append([]string{}, lines[:begin]...)
	out = append(out, indent+markBegin)
	for _, p := range paths {
		out = append(out, indent+`_ "`+p+`"`)
	}
	out = append(out, indent+markEnd)
	out = append(out, lines[end+1:]...)
	return []byte(strings.Join(out, "\n")), nil
}

func rewritePluginsFile(root, importPath string, add bool) error {
	p := filepath.Join(root, "plugins", "plugins.go")
	raw, err := os.ReadFile(p)
	if err != nil {
		return err
	}
	nw, err := updatePluginsFile(raw, importPath, add)
	if err != nil {
		return err
	}
	tmp := p + ".tmp"
	if err := os.WriteFile(tmp, nw, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, p)
}

// safeJoin 把 zip 条目名拼到 base 下；绝对路径、盘符、.. 穿越一律拒绝（zip slip）。
func safeJoin(base, name string) (string, error) {
	n := strings.ReplaceAll(name, "\\", "/")
	if n == "" || strings.ContainsRune(n, 0) || strings.HasPrefix(n, "/") || (len(n) >= 2 && n[1] == ':') {
		return "", fmt.Errorf("非法的压缩包路径 %q", name)
	}
	for _, part := range strings.Split(n, "/") {
		if part == ".." {
			return "", fmt.Errorf("压缩包路径包含 ..: %q", name)
		}
	}
	target := filepath.Join(base, filepath.FromSlash(n))
	rel, err := filepath.Rel(base, target)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("压缩包路径越界: %q", name)
	}
	return target, nil
}

func extractZip(zipPath, dest string) error {
	zr, err := zip.OpenReader(zipPath)
	if err != nil {
		return err
	}
	defer zr.Close()
	if len(zr.File) > maxZipFiles {
		return errors.New("压缩包文件数过多")
	}
	var total int64
	for _, f := range zr.File {
		target, err := safeJoin(dest, f.Name)
		if err != nil {
			return err
		}
		mode := f.Mode()
		switch {
		case mode&fs.ModeSymlink != 0:
			return fmt.Errorf("压缩包包含符号链接: %q", f.Name)
		case f.FileInfo().IsDir():
			if err := os.MkdirAll(target, 0o755); err != nil {
				return err
			}
			continue
		case !mode.IsRegular():
			return fmt.Errorf("压缩包包含不支持的文件类型: %q", f.Name)
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		n, err := extractFile(f, target, maxZipTotal-total)
		if err != nil {
			return err
		}
		total += n
	}
	return nil
}

func extractFile(f *zip.File, target string, budget int64) (int64, error) {
	rc, err := f.Open()
	if err != nil {
		return 0, err
	}
	defer rc.Close()
	w, err := os.OpenFile(target, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		return 0, err
	}
	n, err := io.Copy(w, io.LimitReader(rc, budget+1))
	if cerr := w.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return n, err
	}
	if n > budget {
		return n, errors.New("压缩包解压后过大")
	}
	return n, nil
}

// locatePackage 找到包含 plugin.yaml 的目录：解压根目录，或唯一的顶层子目录。
func locatePackage(dir string) (string, error) {
	if _, err := os.Stat(filepath.Join(dir, plugin.ManifestFile)); err == nil {
		return dir, nil
	}
	ents, err := os.ReadDir(dir)
	if err != nil {
		return "", err
	}
	var found []string
	for _, e := range ents {
		if e.IsDir() && e.Name() != "__MACOSX" {
			if _, err := os.Stat(filepath.Join(dir, e.Name(), plugin.ManifestFile)); err == nil {
				found = append(found, filepath.Join(dir, e.Name()))
			}
		}
	}
	if len(found) != 1 {
		return "", errors.New("压缩包里找不到唯一的 plugin.yaml")
	}
	return found[0], nil
}

// locateRepoRoot 定位解压后的仓库根：自打 zip 时插件目录直接在根下；
// GitHub zipball 会多一层 <repo>-<ref>/ 目录。判定：根下只有一个子目录、
// 该子目录自己不带 plugin.yaml（不是单插件包）且其下还有子目录，则进入它。
func locateRepoRoot(root string) (string, error) {
	ents, err := os.ReadDir(root)
	if err != nil {
		return root, nil
	}
	dirs := 0
	var only string
	for _, e := range ents {
		if e.IsDir() && e.Name() != "__MACOSX" {
			dirs++
			only = e.Name()
		}
	}
	if dirs != 1 {
		return root, nil
	}
	inner := filepath.Join(root, only)
	if fileExists(filepath.Join(inner, plugin.ManifestFile)) {
		return root, nil // 唯一子目录就是单插件包，仓库根取解压根
	}
	if sub, err := os.ReadDir(inner); err == nil {
		for _, e := range sub {
			if e.IsDir() {
				return inner, nil
			}
		}
	}
	return root, nil
}

func fileExists(p string) bool {
	st, err := os.Stat(p)
	return err == nil && !st.IsDir()
}

// copyTree 递归拷贝目录；符号链接等非常规文件一律拒绝。
func copyTree(src, dst string) error {
	return filepath.WalkDir(src, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, p)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		switch {
		case d.IsDir():
			return os.MkdirAll(target, 0o755)
		case d.Type().IsRegular():
			return copyFile(p, target)
		}
		return fmt.Errorf("不支持的文件类型（符号链接？）: %s", p)
	})
}

// rewriteModuleImports 把插件源码里对框架的 import（nexadmin/...）改成目标工程的模块名。
// 只改 import 声明，不动注释与字符串；module 相同（母版内安装）时直接返回。
func rewriteModuleImports(dir, module string) error {
	if module == "" || module == "nexadmin" {
		return nil
	}
	return filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !strings.HasSuffix(p, ".go") {
			return nil
		}
		src, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		fset := token.NewFileSet()
		file, err := parser.ParseFile(fset, p, src, parser.ParseComments)
		if err != nil {
			return fmt.Errorf("解析 %s: %w", p, err)
		}
		changed := false
		for _, imp := range file.Imports {
			path, err := strconv.Unquote(imp.Path.Value)
			if err != nil || !strings.HasPrefix(path, "nexadmin/") {
				continue
			}
			imp.Path.Value = strconv.Quote(module + strings.TrimPrefix(path, "nexadmin"))
			changed = true
		}
		if !changed {
			return nil
		}
		var buf bytes.Buffer
		if err := format.Node(&buf, fset, file); err != nil {
			return fmt.Errorf("格式化 %s: %w", p, err)
		}
		return os.WriteFile(p, buf.Bytes(), 0o644)
	})
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o644)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close() //nolint:errcheck
		return err
	}
	return out.Close()
}

func isDir(p string) bool {
	st, err := os.Stat(p)
	return err == nil && st.IsDir()
}

func exists(p string) bool {
	_, err := os.Lstat(p)
	return err == nil
}

// Install 校验清单 -> 拷贝 backend/ 与 web/ -> 更新 plugins.go 和安装指纹。返回插件 id。
func Install(root, src string, opts InstallOptions) (string, error) {
	return InstallSub(root, src, "", opts)
}

// InstallSub 支持从多插件包（目录或 monorepo zip）的 sub 子目录安装。
// 与 CheckInstall 共用完整检查；获准覆盖时永久保留旧源码，写入失败时恢复本次文件改动。
// 调用方应串行执行同一项目的源码变更（批量操作可外包 ChangeSourcesBatch）。
func InstallSub(root, src, sub string, opts InstallOptions) (string, error) {
	plan, err := prepareInstall(root, src, sub, opts)
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(plan.temp) //nolint:errcheck
	if plan.incompatible != nil {
		fmt.Fprintf(installWarn, "警告：%v；已显式设置 AllowIncompatible 跳过\n", plan.incompatible)
	}
	man := plan.manifest
	var backup string
	if plan.status.Exists {
		backup, err = backupInstalledSources(root, man.ID)
		if err != nil {
			return "", err
		}
		fmt.Fprintf(installWarn, "插件 %s 覆盖前源码备份: %s（不含数据库和授权）\n", man.ID, backup)
	}
	// 单个安装也有回滚边界；外层批量回滚另外保护构建可能改写的依赖和产物。
	err = changeSourcePaths(root, []string{
		"plugins/" + man.ID, "web/src/plugins/" + man.ID, "plugins/plugins.go", "data/plugins/installed.json",
	}, func() error {
		return applyInstall(root, plan)
	})
	if err != nil {
		if backup != "" {
			return "", fmt.Errorf("%w；覆盖前源码备份: %s", err, backup)
		}
		return "", err
	}
	return man.ID, nil
}

func applyInstall(root string, plan *installPlan) error {
	man := plan.manifest
	dstBackend := filepath.Join(root, "plugins", man.ID)
	dstWeb := filepath.Join(root, "web", "src", "plugins", man.ID)
	// 先拷到目标同盘的临时目录，成功后再换入，避免半成品。
	stageB, err := os.MkdirTemp(filepath.Join(root, "plugins"), ".install-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(stageB) //nolint:errcheck
	if err := copyTree(plan.backend, stageB); err != nil {
		return err
	}
	var stageW string
	if plan.web != "" {
		if err := os.MkdirAll(filepath.Dir(dstWeb), 0o755); err != nil {
			return err
		}
		if stageW, err = os.MkdirTemp(filepath.Dir(dstWeb), ".install-"); err != nil {
			return err
		}
		defer os.RemoveAll(stageW) //nolint:errcheck
		if err := copyTree(plan.web, stageW); err != nil {
			return err
		}
	}
	if err := os.RemoveAll(dstBackend); err != nil {
		return err
	}
	if err := os.Rename(stageB, dstBackend); err != nil {
		return err
	}
	// 无 web 的新版本也必须删除旧前端。
	if err := os.RemoveAll(dstWeb); err != nil {
		return err
	}
	if stageW != "" {
		if err := os.Rename(stageW, dstWeb); err != nil {
			return err
		}
	}
	if err := os.WriteFile(filepath.Join(root, "plugins", "plugins.go"), plan.newGo, 0o644); err != nil {
		return err
	}
	if err := AddLedger(root, man.ID, man.Version); err != nil {
		return fmt.Errorf("写入安装台账失败: %w", err)
	}
	return nil
}

// HasWeb 报告插件包（目录或 zip）是否带前端：决定重建时是否需要 npm 构建。
// 解析失败一律按“带前端”处理（宁可多构建一次）。
func HasWeb(src string) bool {
	if isDir(src) {
		return isDir(filepath.Join(src, "web"))
	}
	zr, err := zip.OpenReader(src)
	if err != nil {
		return true
	}
	defer zr.Close()
	for _, f := range zr.File {
		n := strings.ReplaceAll(f.Name, "\\", "/")
		if strings.HasPrefix(n, "web/") || strings.Contains(n, "/web/") {
			return true
		}
	}
	return false
}

// Remove 移除插件文件并更新 plugins.go；purge=true 时先 DROP 插件表、删菜单。
func Remove(root, id string, purge bool, out io.Writer) error {
	if !plugin.ValidID(id) {
		return fmt.Errorf("非法的插件 id %q", id)
	}
	mod, err := modulePath(root)
	if err != nil {
		return err
	}
	dstBackend := filepath.Join(root, "plugins", id)
	dstWeb := filepath.Join(root, "web", "src", "plugins", id)
	installed := exists(dstBackend) || exists(dstWeb)
	if !installed && !purge {
		return fmt.Errorf("插件 %s 未安装", id)
	}
	if purge {
		// 先清数据库再删文件：数据库清不掉就中止，文件留在原地可以重试。
		if err := purgePluginData(root, id, out); err != nil {
			return err
		}
	}
	if !installed {
		// 文件已不在（可能手工删过）：数据库已按需清理，台账里若还记着也一并清掉。
		if err := RemoveLedger(root, id); err != nil {
			return fmt.Errorf("更新安装台账失败: %w", err)
		}
		fmt.Fprintf(out, "插件 %s 未安装，仅清理了数据库。\n", id)
		return nil
	}
	if err := rewritePluginsFile(root, mod+"/plugins/"+id, false); err != nil {
		return err
	}
	if err := os.RemoveAll(dstBackend); err != nil {
		return err
	}
	if err := os.RemoveAll(dstWeb); err != nil {
		return err
	}
	if err := RemoveLedger(root, id); err != nil {
		return fmt.Errorf("更新安装台账失败: %w", err)
	}
	if purge {
		fmt.Fprintf(out, "插件 %s 已移除并清理数据库（表已 DROP、菜单已删除；授权文件 data/licenses/%s.lic 保留，重装可复用）。请重新编译并重启。\n", id, id)
	} else {
		fmt.Fprintf(out, "插件 %s 已移除（数据表与授权文件保留）。请重新编译并重启。\n", id)
	}
	return nil
}

// purgePluginData 连接项目数据库，DROP 插件的表并删除它的菜单。清单先读（菜单码来自它）；
// 任一步失败都报错，由调用方中止——文件尚未删除，处理完可重试。
func purgePluginData(root, id string, out io.Writer) error {
	man, err := plugin.LoadManifest(filepath.Join(root, "plugins", id))
	if err != nil {
		man = nil // 清单缺失/损坏仍继续：表按前缀清，菜单清不了会提示
	}
	cfg, err := core.LoadConfig(filepath.Join(root, "config", "default.yaml"), filepath.Join(root, "config", "config.yaml"))
	if err != nil {
		return fmt.Errorf("读取配置失败: %w", err)
	}
	if ctl.New(cfg, "", io.Discard).State().Running {
		return errors.New("彻底清理插件前必须停止服务并备份数据库；请停服后通过 plugin remove --purge 执行")
	}
	db, err := database.Open(cfg.Database)
	if err != nil {
		return fmt.Errorf("连接数据库失败（未初始化？）: %w", err)
	}
	defer database.Close(db)

	// 与迁移同规则：platform 插件只动平台库；tenant 插件动各租户业务库（没有业务库池则回落平台库）。
	targets := []*gorm.DB{database.Platform(db)}
	if man != nil && man.MigrationTarget == "tenant" {
		pools := database.TenantDatabases(db)
		if len(pools) != 0 {
			targets = nil
			ids := make([]string, 0, len(pools))
			for k := range pools {
				ids = append(ids, k)
			}
			sort.Strings(ids)
			for _, k := range ids {
				targets = append(targets, pools[k])
			}
		}
	}

	total := 0
	for i, t := range targets {
		n, err := purgePluginDatabase(t, id, out)
		if err != nil {
			return fmt.Errorf("清理表失败（目标 %d/%d）: %w", i+1, len(targets), err)
		}
		total += n
	}
	menus := int64(0)
	if man != nil && len(man.Menus) > 0 {
		p := database.Platform(db)
		for _, mu := range man.Menus {
			r := p.Exec("DELETE FROM sys_menu WHERE code = ?", mu.Code())
			if r.Error != nil {
				return fmt.Errorf("删除菜单失败: %w", r.Error)
			}
			menus += r.RowsAffected
		}
	}
	fmt.Fprintf(out, "数据库清理完成：DROP %d 张表，删除 %d 条菜单", total, menus)
	if man == nil {
		fmt.Fprintf(out, "（清单缺失，菜单未清理，可在菜单管理里手动处理）")
	}
	fmt.Fprintln(out)
	return nil
}

func purgePluginDatabase(db *gorm.DB, id string, out io.Writer) (int, error) {
	if !plugin.ValidID(id) {
		return 0, fmt.Errorf("非法的插件 id %q", id)
	}
	store, err := migrate.NewGormStore(db)
	if err != nil {
		return 0, err
	}
	unlock, ok, err := store.TryLock()
	if err != nil {
		return 0, err
	}
	if !ok {
		return 0, errors.New("数据库正在迁移，不能清理插件")
	}
	defer unlock()
	dropped := 0
	run := func(tx *gorm.DB) error {
		var err error
		dropped, err = purgePluginTables(tx, store.Engine(), id, out)
		if err != nil {
			return err
		}
		return forgetPluginMigrations(tx, store.Engine(), id)
	}
	if store.Atomic() {
		err = db.Transaction(run)
	} else {
		err = run(db)
	}
	return dropped, err
}

func forgetPluginMigrations(db *gorm.DB, engine, id string) error {
	for _, record := range []struct{ table, column, value string }{
		{migrate.Table, "scope", migrate.ScopePrefix + id},
		{"sys_plugin_migration", "plugin_id", id},
	} {
		var count int64
		query := "SELECT COUNT(*) FROM information_schema.tables WHERE table_schema = DATABASE() AND table_name = ?"
		if engine == "postgres" {
			query = "SELECT COUNT(*) FROM information_schema.tables WHERE table_schema = current_schema() AND table_name = ?"
		}
		if err := db.Raw(query, record.table).Scan(&count).Error; err != nil {
			return err
		}
		if count == 0 {
			continue
		}
		if err := db.Exec("DELETE FROM "+record.table+" WHERE "+record.column+" = ?", record.value).Error; err != nil {
			return err
		}
	}
	return nil
}

// purgePluginTables 找出并 DROP 当前库里前缀为 plg_<id>_ 的表。表名从数据库目录读出，
// DROP 前再按白名单正则校验一次，名字一律加引号。
func purgePluginTables(db *gorm.DB, engine, id string, out io.Writer) (int, error) {
	pattern := strings.ReplaceAll("plg_"+id+"_", "_", `\_`) + "%"
	var names []string
	switch engine {
	case "mysql", "mariadb":
		if err := db.Raw("SELECT table_name FROM information_schema.tables WHERE table_schema = DATABASE() AND table_name LIKE ?",
			pattern).Scan(&names).Error; err != nil {
			return 0, err
		}
	case "postgres":
		if err := db.Raw("SELECT tablename FROM pg_tables WHERE schemaname = current_schema() AND tablename LIKE ?",
			pattern).Scan(&names).Error; err != nil {
			return 0, err
		}
	default:
		return 0, fmt.Errorf("不支持的数据库类型 %q", engine)
	}
	nameRe := regexp.MustCompile(`^plg_` + regexp.QuoteMeta(id) + `_[a-z0-9_]+$`)
	dropped := 0
	for _, name := range names {
		if !nameRe.MatchString(name) {
			continue
		}
		q := "`" + name + "`"
		if engine == "postgres" {
			q = `"` + name + `"`
		}
		if err := db.Exec("DROP TABLE IF EXISTS " + q).Error; err != nil {
			return dropped, fmt.Errorf("DROP %s: %w", name, err)
		}
		fmt.Fprintf(out, "  已删除表 %s\n", name)
		dropped++
	}
	return dropped, nil
}

// SourcePresent 报告插件的源码目录在不在树里（backend 或 web 目录存在）。
//
// 这**不是**后台「已安装」的口径：母版仓库里每个源码插件的目录都在盘上，
// 用它判安装会把内置插件说成用户装过（并给出会删源码目录的卸载按钮）。
// 界面用的 installed 只看安装台账（LoadLedger）；这里只服务安装/删除的前置判断。
func SourcePresent(root, id string) bool {
	if !plugin.ValidID(id) {
		return false
	}
	return exists(filepath.Join(root, "plugins", id)) || exists(filepath.Join(root, "web", "src", "plugins", id))
}

// ---- 导出给 CLI（cmd/nexadmin）复用的助手；标记行等约定必须单一来源，避免两处定义走漂。 ----

// MarkBegin / MarkEnd 是 plugins.go 空导入区块的标记行。
const (
	MarkBegin = markBegin
	MarkEnd   = markEnd
)

// ImportRe 匹配 plugins.go 标记区内的一行空导入。
var ImportRe = importRe

// Exists 报告路径是否存在（符号链接也算）。
func Exists(p string) bool { return exists(p) }

// ModulePath 从项目根的 go.mod 读 module 名。
func ModulePath(root string) (string, error) { return modulePath(root) }

// CopyFile 拷贝单个文件。
func CopyFile(src, dst string) error { return copyFile(src, dst) }

// UpdatePluginsFile 重写标记区空导入；CLI 的 new/upgrade 命令与市场安装共用同一实现。
func UpdatePluginsFile(src []byte, importPath string, add bool) ([]byte, error) {
	return updatePluginsFile(src, importPath, add)
}

// ExtractZip 把 zip 安全解压到 dest（防穿越、防符号链接、限大小）。
func ExtractZip(zipPath, dest string) error { return extractZip(zipPath, dest) }

// MaxZipFiles / MaxZipTotal 是解包安全上限。
const (
	MaxZipFiles = maxZipFiles
	MaxZipTotal = maxZipTotal
)
