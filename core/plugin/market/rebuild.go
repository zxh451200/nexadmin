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
)

const buildTimeout = 20 * time.Minute

// Rebuild 重建产物并替换当前二进制：npm build（可选）→ go mod tidy → go build → 原子换文件。
// 换完不重启，由调用方决定（重启用 Restart）。
func Rebuild(ctx context.Context, root string, withWeb bool, log func(string)) error {
	if withWeb {
		if err := runCmd(ctx, root, log, "构建前端", npmCmd(), npmArgs(filepath.Join(root, "web"), "run", "build")...); err != nil {
			return fmt.Errorf("前端构建失败: %w", err)
		}
	}
	if err := runCmd(ctx, root, log, "go mod tidy", "go", "mod", "tidy"); err != nil {
		return fmt.Errorf("go mod tidy 失败: %w", err)
	}

	exe, err := os.Executable()
	if err != nil {
		return err
	}
	exe, err = filepath.Abs(exe)
	if err != nil {
		return err
	}
	newExe := exe + ".new"
	_ = os.Remove(newExe)
	log(fmt.Sprintf("编译后端 %s -> %s", "./cmd/server", filepath.Base(exe)))
	build := []string{"build", "-trimpath", "-tags", "embedweb", "-ldflags", "-s -w", "-o", newExe, "./cmd/server"}
	if err := runCmd(ctx, root, log, "go build", "go", build...); err != nil {
		_ = os.Remove(newExe)
		return fmt.Errorf("后端构建失败: %w", err)
	}

	// Windows 允许 rename 运行中的 exe：当前进程换名让位，新二进制顶上；旧文件保留一份便于回退。
	old := exe + ".old"
	_ = os.Remove(old)
	if err := os.Rename(exe, old); err != nil {
		return fmt.Errorf("备份当前二进制失败: %w", err)
	}
	if err := os.Rename(newExe, exe); err != nil {
		// 换回，保证服务还能继续用旧二进制跑
		if rb := os.Rename(old, exe); rb != nil {
			return fmt.Errorf("二进制替换失败且回滚失败（%v），请手动把 %s 恢复为 %s", err, filepath.Base(old), filepath.Base(exe))
		}
		return fmt.Errorf("二进制替换失败（已回滚）: %w", err)
	}
	log("二进制已替换（旧版本保留为 " + filepath.Base(old) + "）")
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
	if err := c.Restart(); err != nil {
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
	var cmd *exec.Cmd
	if runtime.GOOS == "windows" && name == "cmd" {
		cmd = exec.CommandContext(ctx, name, args...)
	} else {
		cmd = exec.CommandContext(ctx, name, args...)
	}
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
