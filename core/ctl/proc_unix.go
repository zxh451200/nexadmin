//go:build !windows

package ctl

import (
	"os/exec"
	"syscall"
)

// detach 让后台进程脱离当前终端会话，关掉终端窗口不会带走它。
func detach(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
}
