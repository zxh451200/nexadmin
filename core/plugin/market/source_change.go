package market

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"nexadmin/core/plugin"
)

// ChangeSources 恢复失败安装或构建改写的源码、依赖清单与前端产物，不涉及数据库。
func ChangeSources(root, id string, change func() error) error {
	if !plugin.ValidID(id) {
		return fmt.Errorf("非法的插件 id %q", id)
	}
	paths := []string{
		"plugins/" + id, "web/src/plugins/" + id, "plugins/plugins.go",
		"go.mod", "go.sum", "web/package.json", "web/package-lock.json", "web/dist",
	}
	backup, err := os.MkdirTemp(root, ".nexadmin-recovery-plugin-")
	if err != nil {
		return err
	}
	present := make(map[string]bool, len(paths))
	for _, rel := range paths {
		src := filepath.Join(root, filepath.FromSlash(rel))
		if _, err := os.Lstat(src); errors.Is(err, os.ErrNotExist) {
			continue
		} else if err != nil {
			_ = os.RemoveAll(backup)
			return err
		}
		if err := copySource(src, filepath.Join(backup, filepath.FromSlash(rel))); err != nil {
			_ = os.RemoveAll(backup)
			return fmt.Errorf("备份 %s 失败: %w", rel, err)
		}
		present[rel] = true
	}
	err = func() (err error) {
		defer func() {
			if p := recover(); p != nil {
				err = fmt.Errorf("源码操作异常: %v", p)
			}
		}()
		return change()
	}()
	if err != nil {
		var restoreErr error
		for _, rel := range paths {
			dst := filepath.Join(root, filepath.FromSlash(rel))
			if e := os.RemoveAll(dst); e != nil {
				restoreErr = errors.Join(restoreErr, e)
				continue
			}
			if present[rel] {
				restoreErr = errors.Join(restoreErr, copySource(filepath.Join(backup, filepath.FromSlash(rel)), dst))
			}
		}
		if restoreErr != nil {
			return fmt.Errorf("%w；源码恢复失败（备份保留在 %s）: %v", err, backup, restoreErr)
		}
		_ = os.RemoveAll(backup)
		return fmt.Errorf("%w；源码、依赖清单与前端产物已恢复", err)
	}
	if err := os.RemoveAll(backup); err != nil {
		return fmt.Errorf("操作已完成，但临时备份清理失败（%s）: %w", backup, err)
	}
	return nil
}

func copySource(src, dst string) error {
	info, err := os.Lstat(src)
	if err != nil {
		return err
	}
	if info.IsDir() {
		if err := os.MkdirAll(dst, info.Mode().Perm()); err != nil {
			return err
		}
		entries, err := os.ReadDir(src)
		if err != nil {
			return err
		}
		for _, entry := range entries {
			if err := copySource(filepath.Join(src, entry.Name()), filepath.Join(dst, entry.Name())); err != nil {
				return err
			}
		}
		return nil
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("不支持的文件类型: %s", src)
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	if err := copyFile(src, dst); err != nil {
		return err
	}
	return os.Chmod(dst, info.Mode().Perm())
}
