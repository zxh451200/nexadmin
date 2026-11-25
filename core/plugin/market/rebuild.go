package market

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"time"

	"nexadmin/core"
	"nexadmin/core/ctl"
	"nexadmin/pkg/pluginlint"
)

const buildTimeout = 20 * time.Minute

// Rebuild 在项目锁与独立的源码、二进制恢复事务中重建产物。
func Rebuild(ctx context.Context, root string, withWeb bool, log func(string)) error {
	return WithProjectLock(root, func() error {
		return ChangeProject(root, func() error {
			return rebuildInTransaction(ctx, root, withWeb, log)
		})
	})
}

// RebuildInTransaction 在调用方已建立的 ChangeSources/ChangeProject 事务内重建产物。
func RebuildInTransaction(ctx context.Context, root string, withWeb bool, log func(string)) error {
	return rebuildInTransaction(ctx, root, withWeb, log)
}

func rebuildInTransaction(ctx context.Context, root string, withWeb bool, log func(string)) error {
	log("检查插件静态契约…")
	manifests, err := pluginlint.LintSources(root)
	if err != nil {
		return fmt.Errorf("插件静态检查失败: %w", err)
	}
	log(fmt.Sprintf("插件静态检查通过（%d 个）", len(manifests)))
	if withWeb {
		if err := runCmd(ctx, root, log, "构建前端", npmCmd(), npmArgs(filepath.Join(root, "web"), "run", "build")...); err != nil {
			return fmt.Errorf("前端构建失败: %w", err)
		}
	}
	if err := runCmd(ctx, root, log, "go mod tidy", "go", "mod", "tidy"); err != nil {
		return fmt.Errorf("go mod tidy 失败: %w", err)
	}

	newExe, exe, oldExe, err := recoveryExecutablePaths(root)
	if err != nil {
		return err
	}
	_ = os.Remove(newExe)
	log(fmt.Sprintf("编译后端 %s -> %s", "./cmd/server", filepath.Base(exe)))
	build := []string{"build", "-trimpath", "-tags", "embedweb", "-ldflags", "-s -w", "-o", newExe, "./cmd/server"}
	if err := runCmd(ctx, root, log, "go build", "go", build...); err != nil {
		_ = os.Remove(newExe)
		return fmt.Errorf("后端构建失败: %w", err)
	}

	if err := ActivateRecovery(root, exe, newExe, oldExe, log); err != nil {
		_ = os.Remove(newExe)
		return fmt.Errorf("二进制替换失败: %w", err)
	}
	log("二进制已替换（旧版本保留为 " + filepath.Base(oldExe) + "；完整恢复点为本次恢复记录）")
	return nil
}

// Restart 自重启：优先走 ctl 的托管重启（pidfile 管理的部署形态）；
// 前台运行等非托管形态自动重启不可用，返回错误由调用方提示手动重启。
// 必须在响应已写回之后调用：它会先停掉当前进程。
func Restart(root string, log func(string)) error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	items, err := RecoveryList(root)
	if err != nil {
		return err
	}
	if len(items) > 0 {
		t, err := loadRecoveryJournal(root, items[len(items)-1].ID)
		if err != nil {
			return err
		}
		// Linux 允许运行中的已改名进程继续执行，os.Executable 可能指向 .old；重启必须使用台账里的 canonical 路径。
		exe = t.journal.Exe
	}
	cfg, err := core.LoadConfig(filepath.Join(root, "config", "default.yaml"), filepath.Join(root, "config", "config.yaml"))
	if err != nil {
		return fmt.Errorf("读取配置失败: %w", err)
	}
	var buf bytes.Buffer
	c := ctl.New(cfg, exe, io.MultiWriter(&buf, writerFunc(func(p []byte) (int, error) {
		log(string(p))
		return len(p), nil
	})))
	log("2 秒后重启服务（等待响应返回）…")
	time.Sleep(2 * time.Second)
	if err := c.ScheduleRestart(); err != nil {
		return fmt.Errorf("自动重启不可用（%v）：%s", err, buf.String())
	}
	return nil
}

type writerFunc func([]byte) (int, error)

func (f writerFunc) Write(p []byte) (int, error) { return f(p) }

func npmCmd() string {
	if runtime.GOOS == "windows" {
		return "cmd"
	}
	return "npm"
}

func npmArgs(webDir string, args ...string) []string {
	full := append([]string{"--prefix", webDir}, args...)
	if runtime.GOOS == "windows" {
		// Windows 上 npm 是 npm.cmd，不能直接 exec；借 cmd /c 执行。
		return append([]string{"/c", "npm"}, full...)
	}
	return full
}

// runCmd 在 root 下执行命令，stdout/stderr 逐行进任务日志。
func runCmd(ctx context.Context, root string, log func(string), title string, name string, args ...string) error {
	log("$ " + title)
	ctx, cancel := context.WithTimeout(ctx, buildTimeout)
	defer cancel()
	// exec.CommandContext 不经 shell；Windows 上 npm 走 cmd /c 由 npmArgs 拼参数（见 npmCmd/npmArgs）。
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir = root
	out, err := cmd.CombinedOutput()
	for _, line := range splitLines(string(out)) {
		log("  " + line)
	}
	if err != nil {
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return errors.New("超时（20 分钟）")
		}
		return err
	}
	return nil
}

func splitLines(s string) []string {
	var out []string
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] == '\n' {
			line := s[start:i]
			if len(line) > 0 && line[len(line)-1] == '\r' {
				line = line[:len(line)-1]
			}
			if line != "" {
				out = append(out, line)
			}
			start = i + 1
		}
	}
	if start < len(s) {
		out = append(out, s[start:])
	}
	if len(out) > 120 { // 只留头尾，避免巨量 npm 输出刷爆任务日志
		out = append(out[:60], out[len(out)-40:]...)
	}
	return out
}
