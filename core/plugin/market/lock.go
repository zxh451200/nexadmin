package market

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

const (
	projectLockFile = ".nexadmin-plugin.lock"
	projectLockWait = 2 * time.Minute
)

func WithProjectLock(root string, change func() error) error {
	return withProjectLock(root, true, change)
}

// WithRecoveryLock serializes recovery mutations without rejecting the unresolved records they resolve.
func WithRecoveryLock(root string, change func() error) error {
	return withProjectLock(root, false, change)
}

func withProjectLock(root string, checkRecovery bool, change func() error) error {
	root, err := filepath.Abs(root)
	if err != nil {
		return err
	}
	if err := checkSourcePath(root, projectLockFile); err != nil {
		return err
	}
	f, err := os.OpenFile(filepath.Join(root, projectLockFile), os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return fmt.Errorf("打开插件变更锁: %w", err)
	}
	defer f.Close()
	unlock, err := lockProjectFile(f)
	if err != nil {
		return err
	}
	defer unlock()
	if checkRecovery {
		if err := CheckRecovery(root); err != nil {
			return err
		}
	}
	return change()
}

func lockProjectFile(f *os.File) (func(), error) {
	deadline := time.Now().Add(projectLockWait)
	for {
		locked, err := tryProjectLock(f)
		if err != nil {
			return nil, fmt.Errorf("获取插件变更锁: %w", err)
		}
		if locked {
			return func() { _ = unlockProjectFile(f) }, nil
		}
		if time.Now().After(deadline) {
			return nil, errors.New("获取插件变更锁超时，另一个安装或构建任务仍在运行")
		}
		time.Sleep(100 * time.Millisecond)
	}
}
