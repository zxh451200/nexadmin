package ctl

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"nexadmin/core/config"
	"nexadmin/core/database"
)

// Restorer 用本机 mysql / psql 导入 gzip 压缩的备份；单测用假实现。
type Restorer interface {
	Tool() string // 找不到工具时返回 ""
	Restore(ctx context.Context, path string) error
}

// StateReader 只需要读服务状态（*Ctl 实现）。
type StateReader interface{ State() State }

// Restore 是 `nexadmin-server restore <备份文件>`：恢复只提供命令行入口，不提供网页入口。
type Restore struct {
	Cfg   *config.Config
	Svc   StateReader
	DB    Database  // 只用 Inspect 列出将被覆盖的库
	Pre   Backuper  // 恢复前备份
	Tool  Restorer  // 导入
	In    io.Reader // 交互确认
	Out   io.Writer
	Now   func() time.Time
	Close func() // 释放连接，可为 nil
}

// RestoreOptions 是 restore 子命令的参数。
type RestoreOptions struct {
	File   string
	SHA256 string // 可选：没有 .sha256 旁路文件时用它提供期望值
	Yes    bool   // 跳过交互确认（自动化）
	DryRun bool   // 只列清单
}

// BackupDir 是备份目录（backup.dir，空则 ./backup）。
func BackupDir(c *config.Config) string {
	if c.Backup.Dir == "" {
		return "backup"
	}
	return c.Backup.Dir
}

// Run 执行：必须停服务 -> 校验 sha256 -> 列出将覆盖的库 -> 确认 -> 恢复前备份 -> 导入。
func (r *Restore) Run(ctx context.Context, o RestoreOptions) error {
	st, err := os.Stat(o.File)
	if err != nil {
		return fmt.Errorf("读不到备份文件: %w", err)
	}
	if !st.Mode().IsRegular() {
		return fmt.Errorf("%s 不是普通文件", o.File)
	}
	if !strings.HasSuffix(o.File, ".sql.gz") {
		return fmt.Errorf("只支持本系统生成的 .sql.gz 备份文件")
	}
	running := r.Svc.State().Running
	if running && !o.DryRun {
		return errors.New("服务正在运行，已拒绝恢复；请先执行 stop 停止服务（恢复会覆盖线上数据）")
	}

	// 先校验 sha256：期望值来自旁路文件或 --sha256；两者都没有就拒绝，不做“盲恢复”。
	want := strings.ToLower(strings.TrimSpace(o.SHA256))
	if want == "" {
		if want, err = ReadSidecar(o.File); err != nil {
			return fmt.Errorf("找不到 %s，无法校验完整性；可用 --sha256 <值> 手工提供，已中止: %w", SidecarPath(o.File), err)
		}
	}
	if err := VerifySHA256(o.File, want); err != nil {
		return fmt.Errorf("%w；已拒绝恢复，未改动任何数据", err)
	}

	if r.Tool.Tool() == "" {
		return errors.New("本机没有找到 mysql / psql，无法恢复，已中止")
	}
	if r.Pre.Tool() == "" {
		return errors.New("本机没有找到 mysqldump / pg_dump，无法做“恢复前备份”，已中止")
	}
	info, err := r.DB.Inspect(ctx)
	if err != nil {
		return fmt.Errorf("读取数据库信息失败，已中止: %w", err)
	}
	if info.Name == "" {
		return errors.New("读不到数据库名，已中止")
	}

	dir := BackupDir(r.Cfg)
	fmt.Fprintf(r.Out, "\n========== NexAdmin 数据库恢复 ==========\n将要执行：\n")
	fmt.Fprintf(r.Out, "  1. 校验 sha256 通过  %s（%s，%s）\n", filepath.ToSlash(o.File), humanBytes(st.Size()), want[:12])
	if running {
		fmt.Fprintf(r.Out, "  2. 服务当前正在运行：真正执行时会被拒绝，请先 stop\n")
	} else {
		fmt.Fprintf(r.Out, "  2. 服务已停止\n")
	}
	fmt.Fprintf(r.Out, "  3. 恢复前备份  用 %s 把当前库备份到 ./%s/（失败则中止）\n", r.Pre.Tool(), filepath.ToSlash(dir))
	fmt.Fprintf(r.Out, "  4. 覆盖数据库  %s://%s:%d/%s\n       当前共 %d 张表，%s，%s；备份中包含的表会被删除后重建，备份里没有的表保留\n",
		info.Engine, info.Host, info.Port, info.Name, len(info.Tables), humanRows(info.Rows), humanBytes(info.Bytes))
	fmt.Fprintf(r.Out, "     使用 %s 导入\n", r.Tool.Tool())
	if o.DryRun {
		fmt.Fprintln(r.Out, "\n（--dry-run：只列清单，未执行任何操作）")
		return nil
	}

	if !o.Yes {
		in := bufio.NewReader(r.In)
		ask := func(prompt string) string {
			fmt.Fprint(r.Out, prompt)
			line, _ := in.ReadString('\n') // 读不到（EOF）当作空，等于取消
			return strings.TrimSpace(line)
		}
		if ask("确认恢复？[y/N] ") != "y" {
			fmt.Fprintln(r.Out, "已取消，未做任何改动。")
			return nil
		}
		if r.Cfg.IsProduction() && ask(fmt.Sprintf("app.env 为 production，请输入库名 %q 确认: ", info.Name)) != info.Name {
			fmt.Fprintln(r.Out, "库名不匹配，已取消，未做任何改动。")
			return nil
		}
	}

	pre, err := r.Pre.Backup(ctx, dir, r.Now().Format("20060102-150405"))
	if err != nil {
		return fmt.Errorf("恢复前备份失败，已中止（未改动任何数据）: %w", err)
	}
	fmt.Fprintf(r.Out, "  ✓ 恢复前备份  %s\n", pre)
	if err := r.Tool.Restore(ctx, o.File); err != nil {
		return fmt.Errorf("导入失败: %w（库可能处于部分恢复状态；恢复前备份在 %s，可用 restore 回滚）", err, pre)
	}
	fmt.Fprintf(r.Out, "  ✓ 恢复完成\n启动服务请执行 start。\n")
	return nil
}

// ---- 真实实现 ----

// NewRestore 装配真实的 Restore。version 用于“恢复前备份”的文件名（框架版本）。
func NewRestore(cfg *config.Config, svc StateReader, version string, in io.Reader, out io.Writer, platformOnly ...bool) (*Restore, error) {
	var err error
	cfg, err = legacyPlatformConfig(cfg, platformOnly)
	if err != nil {
		return nil, err
	}
	db, err := database.Open(cfg.Database)
	if err != nil {
		return nil, fmt.Errorf("连不上数据库，已中止: %w", err)
	}
	g := &gormDB{db: db, cfg: cfg.Database}
	return &Restore{
		Cfg: cfg, Svc: svc, DB: g, In: in, Out: out, Now: time.Now,
		Pre:   &gzipBackup{cfg: cfg.Database, version: version, kind: "pre-restore"},
		Tool:  &dbRestorer{cfg: cfg.Database},
		Close: func() { _ = g.Close() },
	}, nil
}

type dbRestorer struct{ cfg config.Database }

func (d *dbRestorer) Tool() string {
	t, err := RestoreTool(d.cfg, nil)
	if err != nil {
		return ""
	}
	return t.Name
}

func (d *dbRestorer) Restore(ctx context.Context, path string) error {
	t, err := RestoreTool(d.cfg, nil)
	if err != nil {
		return err
	}
	return RestoreGzip(ctx, t, path)
}

// gzipBackup 是 Backuper 的 gzip 实现：文件名 nexadmin-<kind>-<引擎>-v<版本>-<时间>.sql.gz，带 .sha256，
// 与备份页同一格式、同一目录，restore 可以直接恢复。kind 是 pre-restore（恢复前备份）或 reinit（恢复出厂前备份）。
type gzipBackup struct {
	cfg     config.Database
	version string
	kind    string
}

func (g *gzipBackup) Tool() string {
	t, err := DumpTool(g.cfg, nil)
	if err != nil {
		return ""
	}
	return t.Name
}

func (g *gzipBackup) Backup(ctx context.Context, dir, stamp string) (string, error) {
	t, err := DumpTool(g.cfg, nil)
	if err != nil {
		return "", err
	}
	path, err := BackupPath(dir, BackupFileNameStamp(g.kind+"-"+Engine(g.cfg), g.version, stamp))
	if err != nil {
		return "", err
	}
	res, err := DumpGzip(ctx, t, path)
	if err != nil {
		return "", err
	}
	return res.Path, nil
}
