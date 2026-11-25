package market

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"nexadmin/core/plugin"
)

// ChangeProject 将依赖、前端产物与可执行文件的重建作为一个恢复单元。
func ChangeProject(root string, change func() error) error {
	return changeSourcePaths(root, nil, change)
}

// ChangeSources 恢复失败安装或构建改写的源码、依赖清单、前端产物、台账与二进制。
func ChangeSources(root, id string, change func() error) error {
	return ChangeSourcesBatch(root, []string{id}, change)
}

// ChangeSourcesBatch 将一批插件源码变更作为一个恢复单元；调用方须串行执行，数据库、授权与用户数据不在回滚范围。
func ChangeSourcesBatch(root string, ids []string, change func() error) error {
	paths := []string{
		"plugins/plugins.go", "go.mod", "go.sum", "flowlite/go.mod", "flowlite/go.sum",
		"web/package.json", "web/package-lock.json", "web/dist", "data/plugins/installed.json",
	}

	seen := make(map[string]bool, len(ids))
	for _, id := range ids {
		if !plugin.ValidID(id) {
			return fmt.Errorf("非法的插件 id %q", id)
		}
		if !seen[id] {
			paths = append(paths, "plugins/"+id, "web/src/plugins/"+id)
			seen[id] = true
		}
	}
	return changeSourcePaths(root, paths, change)
}

func changeSourcePaths(root string, paths []string, change func() error) error {
	if paths == nil {
		paths = []string{
			"plugins/plugins.go", "go.mod", "go.sum", "flowlite/go.mod", "flowlite/go.sum",
			"web/package.json", "web/package-lock.json", "web/dist", "data/plugins/installed.json",
		}
	}
	for _, rel := range paths {
		if !validRecoveryPath(rel) {
			return fmt.Errorf("非法的恢复路径 %q", rel)
		}
		if err := checkSourcePath(root, rel); err != nil {
			return err
		}
	}
	tx, err := beginRecovery(root, paths)
	if err != nil {
		return err
	}
	err = func() (err error) {
		defer func() {
			if p := recover(); p != nil {
				err = fmt.Errorf("源码或构建操作异常: %v", p)
			}
		}()
		return change()
	}()
	if err != nil {
		return tx.restore(err)
	}
	if err := tx.commit(); err != nil {
		return err
	}
	return nil
}

// checkSourcePath 校验根目录到目标的每一层，避免通过父目录符号链接读写项目外文件。
// 不存在的路径允许后续创建；已存在的非常规文件一律拒绝。
func checkSourcePath(root, rel string) error {
	target, err := safeJoin(root, rel)
	if err != nil {
		return err
	}
	root = filepath.Clean(root)
	for p := target; ; p = filepath.Dir(p) {
		info, err := os.Lstat(p)
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		if err == nil {
			if !info.IsDir() && (!info.Mode().IsRegular() || p != target) {
				return fmt.Errorf("不支持的路径类型（符号链接？）: %s", p)
			}
		}
		if p == root {
			return nil
		}
	}
}

// backupInstalledSources 为获准的覆盖保存唯一、持久的源码副本，不包含数据库和授权。
func backupInstalledSources(root, id string) (string, error) {
	if err := checkSourcePath(root, "data/plugins/backups"); err != nil {
		return "", err
	}
	base, err := filepath.Abs(filepath.Join(root, "data", "plugins", "backups"))
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(base, 0o755); err != nil {
		return "", err
	}
	backup, err := os.MkdirTemp(base, id+"-")
	if err != nil {
		return "", err
	}
	for _, rel := range []string{"plugins/" + id, "web/src/plugins/" + id} {
		if err := checkSourcePath(root, rel); err != nil {
			_ = os.RemoveAll(backup)
			return "", err
		}
		src := filepath.Join(root, filepath.FromSlash(rel))
		if _, err := os.Lstat(src); errors.Is(err, os.ErrNotExist) {
			continue
		} else if err != nil {
			_ = os.RemoveAll(backup)
			return "", err
		}
		if err := copySource(src, filepath.Join(backup, filepath.FromSlash(rel))); err != nil {
			_ = os.RemoveAll(backup)
			return "", fmt.Errorf("备份 %s 失败: %w", rel, err)
		}
	}
	return backup, nil
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
