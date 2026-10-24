package ctl

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	"nexadmin/core/config"
	"nexadmin/core/setup"
)

// 下面几个接口把“真正碰外部系统”的部分隔开：生产用 reinit_impl.go 里的实现，单测用假实现。

// DBInfo 是从库里实际读到的信息。
type DBInfo struct {
	Engine string // mysql | postgres
	Host   string // 连接成功的主机:端口里的主机
	Port   int
	Name   string // 服务器自己报告的当前库名（SELECT DATABASE()），不是配置里的猜测
	Tables []string
	Rows   int64 // 估算值（information_schema / pg_class），清单里写“约”
	Bytes  int64
}

// Database 读取库信息并清空所有表。
type Database interface {
	Inspect(ctx context.Context) (DBInfo, error)
	DropTables(ctx context.Context, tables []string) error
	Close() error
}

// KeyPurger 只处理本系统前缀（nex:*）的缓存 key；memory 缓存不需要，传 nil。
type KeyPurger interface {
	CountKeys(ctx context.Context) (int, error)
	Purge(ctx context.Context) (int, error)
	Close() error
}

// Backuper 用本机 mysqldump / pg_dump 备份。
type Backuper interface {
	Tool() string // 找不到工具时返回 ""
	Backup(ctx context.Context, dir, stamp string) (path string, err error)
}

// Service 是 start / stop 能力（*Ctl 实现）。
type Service interface {
	State() State
	Stop() error
	Start() error
}

// Plan 是清单：每个数字都来自实际读取。
type Plan struct {
	Running    bool
	PID        int
	DB         DBInfo
	CacheKeys  int // -1 = memory 缓存，无需处理
	Uploads    Dir // data/uploads
	BackupTool string
	BackupFile string // 已备份时的文件路径
	BackupDir  string // 备份目录（backup.dir，与备份页、restore 同一个）
	DryRun     bool
	Stamp      string
}

// Dir 是目录统计。
type Dir struct {
	Path  string
	Files int
	Bytes int64
}

// Reinit 是恢复出厂（设计文档 §17「恢复出厂」）。
type Reinit struct {
	Cfg    *config.Config
	Svc    Service
	DB     Database
	Cache  KeyPurger // nil = memory
	Backup Backuper
	In     io.Reader
	Out    io.Writer
	Now    func() time.Time
}

// BuildPlan 只读地生成清单；库信息、缓存、上传目录任何一项读不到都返回错误（中止，不执行）。
func (r *Reinit) BuildPlan(ctx context.Context) (*Plan, error) {
	p := &Plan{CacheKeys: -1, Stamp: r.Now().Format("20060102-150405"), BackupDir: BackupDir(r.Cfg)}
	st := r.Svc.State()
	p.Running = st.Running
	if st.Running {
		if st.Pid == nil {
			return nil, fmt.Errorf("服务正在运行，但不是由 start 启动的（没有 pid 文件），无法自动停止；请先手动停止后再试")
		}
		p.PID = st.Pid.PID
	}
	info, err := r.DB.Inspect(ctx)
	if err != nil {
		return nil, fmt.Errorf("读取数据库信息失败，已中止: %w", err)
	}
	if info.Name == "" {
		return nil, fmt.Errorf("读不到数据库名，已中止")
	}
	p.DB = info
	if r.Cache != nil {
		if p.CacheKeys, err = r.Cache.CountKeys(ctx); err != nil {
			return nil, fmt.Errorf("读取缓存信息失败，已中止: %w", err)
		}
	}
	if p.Uploads, err = scanDir(filepath.Join(r.Cfg.DataDir, "uploads")); err != nil {
		return nil, fmt.Errorf("读取上传目录失败，已中止: %w", err)
	}
	p.BackupTool = r.Backup.Tool()
	return p, nil
}

// displayDir 把备份目录写成便于阅读的样子：相对路径加 "./"，绝对路径原样。
func displayDir(dir string) string {
	d := filepath.ToSlash(filepath.Clean(dir))
	if filepath.IsAbs(dir) {
		return d
	}
	return "./" + d
}

func scanDir(dir string) (Dir, error) {
	d := Dir{Path: dir}
	err := filepath.WalkDir(dir, func(_ string, e fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !e.IsDir() {
			d.Files++
			if fi, err := e.Info(); err == nil {
				d.Bytes += fi.Size()
			}
		}
		return nil
	})
	if os.IsNotExist(err) {
		return d, nil // 没有上传目录就是 0 个文件
	}
	return d, err
}

func humanBytes(n int64) string {
	switch {
	case n >= 1<<30:
		return fmt.Sprintf("%.1f GB", float64(n)/(1<<30))
	case n >= 1<<20:
		return fmt.Sprintf("%.1f MB", float64(n)/(1<<20))
	case n >= 1<<10:
		return fmt.Sprintf("%.1f KB", float64(n)/(1<<10))
	}
	return fmt.Sprintf("%d B", n)
}

func humanRows(n int64) string {
	if n >= 10000 {
		return fmt.Sprintf("约 %.1f 万行", float64(n)/10000)
	}
	return fmt.Sprintf("约 %d 行", n)
}

// Print 按设计文档的样式输出清单。
func (p *Plan) Print(w io.Writer, marker string) {
	fmt.Fprintf(w, "\n========== NexAdmin 恢复出厂设置 ==========\n将要执行：\n")
	if p.Running {
		fmt.Fprintf(w, "  1. 停止服务（运行中，pid %d）\n", p.PID)
	} else {
		fmt.Fprintf(w, "  1. 停止服务（当前未运行，无需停止）\n")
	}
	fmt.Fprintf(w, "  2. 清空数据库  %s://%s:%d/%s\n       共 %d 张表，%s，%s；该库中所有表都会被删除\n",
		p.DB.Engine, p.DB.Host, p.DB.Port, p.DB.Name, len(p.DB.Tables), humanRows(p.DB.Rows), humanBytes(p.DB.Bytes))
	if p.CacheKeys < 0 {
		fmt.Fprintf(w, "  3. 清理缓存    内存缓存，随进程重启清空，无需处理\n")
	} else {
		fmt.Fprintf(w, "  3. 清理缓存    仅删除 key 前缀 nex:*（不会 FLUSHALL），当前 %d 个 key\n", p.CacheKeys)
	}
	fmt.Fprintf(w, "  4. 删除本地上传目录  ./%s（%d 个文件，%s）\n", filepath.ToSlash(p.Uploads.Path), p.Uploads.Files, humanBytes(p.Uploads.Bytes))
	fmt.Fprintf(w, "  5. 删除初始化标记（%s），重启后进入初始化向导\n", filepath.ToSlash(marker))
	fmt.Fprintf(w, "保留：config/config.yaml、internal/、migrations/business/、日志\n")
	switch {
	case p.BackupTool == "":
		fmt.Fprintf(w, "备份：未备份（本机没有找到 mysqldump / pg_dump）\n")
	case p.BackupFile != "":
		fmt.Fprintf(w, "备份：已用 %s 备份到 %s\n", p.BackupTool, p.BackupFile)
	default:
		fmt.Fprintf(w, "备份：将用 %s 备份到 %s/（nexadmin-reinit-*-%s.sql.gz + .sha256，与备份页同格式，可直接用 restore 恢复）\n", p.BackupTool, displayDir(p.BackupDir), p.Stamp)
	}
	fmt.Fprintf(w, "注意：远程对象存储（S3 / OSS）里的文件不会被删除，需要手动清理。\n")
}

// Run 执行完整流程：清单 -> （备份）-> 确认 -> 执行 -> 自动启动。dryRun 只列清单。
func (r *Reinit) Run(ctx context.Context, dryRun bool) error {
	p, err := r.BuildPlan(ctx)
	if err != nil {
		return err
	}
	p.DryRun = dryRun
	if !dryRun && p.BackupTool != "" {
		if p.BackupFile, err = r.Backup.Backup(ctx, p.BackupDir, p.Stamp); err != nil {
			return fmt.Errorf("备份失败，已中止（未改动任何数据）: %w", err)
		}
	}
	p.Print(r.Out, setup.MarkerPath(r.Cfg))
	if dryRun {
		fmt.Fprintln(r.Out, "\n（--dry-run：只列清单，未执行任何操作）")
		return nil
	}

	in := bufio.NewReader(r.In)
	ask := func(prompt string) string {
		fmt.Fprint(r.Out, prompt)
		line, _ := in.ReadString('\n') // 读不到（EOF）当作空，等于取消
		return strings.TrimSpace(line)
	}
	if ask("确认执行？[y/N] ") != "y" {
		fmt.Fprintln(r.Out, "已取消，未做任何改动。")
		return nil
	}
	if r.Cfg.IsProduction() && ask(fmt.Sprintf("app.env 为 production，请输入库名 %q 确认: ", p.DB.Name)) != p.DB.Name {
		fmt.Fprintln(r.Out, "库名不匹配，已取消，未做任何改动。")
		return nil
	}
	return r.execute(ctx, p)
}

func (r *Reinit) execute(ctx context.Context, p *Plan) error {
	step := func(name string, err error) error {
		if err != nil {
			return fmt.Errorf("%s失败: %w（已完成的步骤不会回滚，请处理后重新运行 reinit）", name, err)
		}
		fmt.Fprintf(r.Out, "  ✓ %s\n", name)
		return nil
	}
	if p.Running {
		if err := step("停止服务", r.Svc.Stop()); err != nil {
			return err
		}
	}
	if err := step("清空数据库", r.DB.DropTables(ctx, p.DB.Tables)); err != nil {
		return err
	}
	if r.Cache != nil {
		_, err := r.Cache.Purge(ctx)
		if err := step("清理缓存（nex:*）", err); err != nil {
			return err
		}
	}
	if err := step("删除上传目录", os.RemoveAll(p.Uploads.Path)); err != nil {
		return err
	}
	rmErr := os.Remove(setup.MarkerPath(r.Cfg))
	if os.IsNotExist(rmErr) {
		rmErr = nil
	}
	if err := step("删除初始化标记", rmErr); err != nil {
		return err
	}

	fmt.Fprintf(r.Out, "\n恢复出厂完成，正在启动……\n")
	if err := r.Svc.Start(); err != nil {
		return fmt.Errorf("启动失败: %w（可稍后运行 start）", err)
	}
	d := r.Cfg.Database
	fmt.Fprintf(r.Out, "向导里的数据库配置请按旧值填写：类型 %s，主机 %s，端口 %d，账号 %s，库名 %s（密码沿用 config/config.yaml 里的旧值，此处不显示）\n",
		d.Type, d.Host, d.Port, d.User, d.Name)
	return nil
}
