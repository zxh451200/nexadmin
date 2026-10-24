//go:build windows

package ctl

import (
	"os/exec"
	"syscall"
)

// detach 让后台进程脱离当前控制台：不弹窗口、不随控制台关闭而退出。
func detach(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{
		CreationFlags: 0x00000200 | 0x00000008 | 0x08000000, // NEW_PROCESS_GROUP | DETACHED_PROCESS | NO_WINDOW
		HideWindow:    true,
	}
}
