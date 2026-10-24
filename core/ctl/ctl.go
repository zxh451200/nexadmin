package ctl

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"nexadmin/core/config"
)

// Ctl 是 start / stop / restart / status 的执行者。
type Ctl struct {
	Cfg *config.Config
	Exe string    // start 用哪个二进制拉起后台进程（通常是 os.Executable()）
	Out io.Writer // 给用户看的输出

	client *http.Client
}

// New 创建 Ctl。
func New(cfg *config.Config, exe string, out io.Writer) *Ctl {
	return &Ctl{
		Cfg: cfg, Exe: exe, Out: out,
		// 只访问本机回环，不走系统代理。
		client: &http.Client{Timeout: 3 * time.Second, Transport: &http.Transport{Proxy: nil}},
	}
}

func (c *Ctl) pidPath() string { return PidPath(c.Cfg.DataDir) }

// ConsoleLog 是后台进程的控制台输出（含未初始化时的向导地址与令牌），权限 0600。
func (c *Ctl) ConsoleLog() string { return filepath.Join("logs", "nexadmin.out") }

// State 是服务当前状态。
type State struct {
	Running     bool
	Initialized bool
	Port        int
	Pid         *PidFile // 没有 pid 文件时为 nil（例如前台运行）
}

// State 通过本机 /api/v1/system/status 判断是否在运行、是否已初始化；pid 文件只提供 pid 和端口。
func (c *Ctl) State() State {
	st := State{Port: c.Cfg.Server.Port}
	if p, err := ReadPid(c.pidPath()); err == nil {
		st.Pid, st.Port = &p, p.Port
	}
	st.Running, st.Initialized = c.probe(st.Port)
	return st
}

func (c *Ctl) base(port int) string { return fmt.Sprintf("http://127.0.0.1:%d", port) }

func (c *Ctl) probe(port int) (running, initialized bool) {
	resp, err := c.client.Get(c.base(port) + "/api/v1/system/status")
	if err != nil {
		return false, false
	}
	defer resp.Body.Close()
	var body struct {
		Data struct {
			Initialized bool `json:"initialized"`
		} `json:"data"`
	}
	if resp.StatusCode != http.StatusOK || json.NewDecoder(resp.Body).Decode(&body) != nil {
		return false, false
	}
	return true, body.Data.Initialized
}

func (c *Ctl) waitUntil(timeout time.Duration, port int, wantUp bool, dead <-chan error) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if up, _ := c.probe(port); up == wantUp {
			return nil
		}
		select {
		case err := <-dead:
			if wantUp {
				return fmt.Errorf("进程已退出（%v），详见 %s", err, c.ConsoleLog())
			}
		case <-time.After(200 * time.Millisecond):
		}
	}
	return fmt.Errorf("等待 %s 超时", map[bool]string{true: "服务启动", false: "服务退出"}[wantUp])
}

// Start 后台启动：写 pid 文件（含关停令牌），日志进 logs/。已在运行则什么都不做。
func (c *Ctl) Start() error {
	if st := c.State(); st.Running {
		fmt.Fprintf(c.Out, "已在运行（端口 %d）\n", st.Port)
		return nil
	}
	if err := os.MkdirAll("logs", 0o755); err != nil {
		return err
	}
	logf, err := os.OpenFile(c.ConsoleLog(), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	defer logf.Close()

	token := NewToken()
	cmd := exec.Command(c.Exe)
	cmd.Env = append(os.Environ(), TokenEnv+"="+token)
	cmd.Stdout, cmd.Stderr = logf, logf
	detach(cmd)
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("启动失败: %w", err)
	}
	port := c.Cfg.Server.Port
	if err := WritePid(c.pidPath(), PidFile{PID: cmd.Process.Pid, Port: port, Token: token, Started: time.Now()}); err != nil {
		_ = cmd.Process.Kill()
		return err
	}
	dead := make(chan error, 1)
	go func() { dead <- cmd.Wait() }()
	if err := c.waitUntil(20*time.Second, port, true, dead); err != nil {
		_ = cmd.Process.Kill()
		<-dead
		_ = os.Remove(c.pidPath())
		return err
	}
	fmt.Fprintf(c.Out, "已启动（pid %d，端口 %d），日志: logs/\n", cmd.Process.Pid, port)
	if st := c.State(); !st.Initialized {
		fmt.Fprintf(c.Out, "系统尚未初始化\n  向导地址:   http://localhost:%d/setup\n  初始化令牌: %s\n", port, SetupToken(c.ConsoleLog()))
	}
	return nil
}

// Stop 通过回环地址上带令牌的关停接口优雅退出，不依赖信号（Windows 与 Linux 一致）。
func (c *Ctl) Stop() error {
	p, err := ReadPid(c.pidPath())
	if err != nil {
		if up, _ := c.probe(c.Cfg.Server.Port); up {
			return fmt.Errorf("服务在运行，但不是由 start 启动的（没有 pid 文件），请到它所在的终端按 Ctrl+C 停止")
		}
		fmt.Fprintln(c.Out, "未运行")
		return nil
	}
	if up, _ := c.probe(p.Port); !up {
		_ = os.Remove(c.pidPath())
		fmt.Fprintln(c.Out, "未运行（已清理过期的 pid 文件）")
		return nil
	}
	req, _ := http.NewRequestWithContext(context.Background(), http.MethodPost, c.base(p.Port)+ShutdownPath, nil)
	req.Header.Set(ShutdownHeader, p.Token)
	resp, err := c.client.Do(req)
	if err != nil {
		return fmt.Errorf("调用关停接口失败: %w", err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("关停被拒绝（HTTP %d），pid 文件里的令牌与运行中的进程不符", resp.StatusCode)
	}
	if err := c.waitUntil(20*time.Second, p.Port, false, nil); err != nil {
		return err
	}
	_ = os.Remove(c.pidPath())
	fmt.Fprintf(c.Out, "已停止（pid %d）\n", p.PID)
	return nil
}

// Restart = Stop + Start。
func (c *Ctl) Restart() error {
	if err := c.Stop(); err != nil {
		return err
	}
	return c.Start()
}

// Status 打印运行状态、pid、监听地址、是否已初始化。
func (c *Ctl) Status() error {
	st := c.State()
	if !st.Running {
		fmt.Fprintln(c.Out, "状态:     已停止")
		return nil
	}
	pid := "未知（非 start 启动）"
	if st.Pid != nil {
		pid = fmt.Sprint(st.Pid.PID)
	}
	fmt.Fprintf(c.Out, "状态:     运行中\npid:      %s\n监听地址: :%d（http://127.0.0.1:%d）\n已初始化: %s\n", pid, st.Port, st.Port, map[bool]string{true: "是", false: "否"}[st.Initialized])
	return nil
}

// SetupToken 从控制台输出文件里取最近一次打印的初始化令牌；读不到返回提示文字。
func SetupToken(consoleLog string) string {
	f, err := os.Open(consoleLog)
	if err != nil {
		return "(读不到 " + consoleLog + ")"
	}
	defer f.Close()
	if fi, err := f.Stat(); err == nil && fi.Size() > 64<<10 {
		_, _ = f.Seek(fi.Size()-64<<10, io.SeekStart) // 令牌在最近的启动横幅里，只看文件末尾
	}
	token := ""
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		if _, v, ok := strings.Cut(sc.Text(), "初始化令牌:"); ok {
			token = strings.TrimSpace(v)
		}
	}
	if token == "" {
		return "(控制台输出里没有找到，见 " + consoleLog + ")"
	}
	return token
}
