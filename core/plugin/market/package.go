package market

import (
	"archive/zip"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	"nexadmin/core/plugin"
)

// maxPackageBytes 是重打产物的上限。插件源码包远小于此，超过即视为异常（防止把大文件打进官网下载）。
const maxPackageBytes = 64 << 20

// zipEpoch 固定为 1980-01-01（zip 能表示的最早时间）：同一份源码每次打包字节一致，
// 校验和才能跨实例、跨重复构建对比。
var zipEpoch = time.Date(1980, 1, 1, 0, 0, 0, 0, time.UTC)

// Package 下载市场条目对应的源码，重打成可直接 `plugin install` 的安装包：
// 包根是 plugin.yaml，另有 backend/（后端源码与迁移）和 web/（前端源码）。
//
// 这一步是官网下载与“下载后用 CLI 安装”的关键：市场仓库通常是 monorepo
// （nexadmin-plugins/<id>/…），GitHub zipball 直接给用户会带上全部插件；
// 这里按目录的 subdir 定位插件、校验清单 id/edition，只保留安装器认识的三个入口。
func Package(ctx context.Context, e Entry, dst string) error {
	if !plugin.ValidID(e.ID) {
		return fmt.Errorf("非法的插件 id %q", e.ID)
	}
	if e.Subdir != "" {
		sub := filepath.ToSlash(e.Subdir)
		if filepath.IsAbs(e.Subdir) || sub == ".." || strings.HasPrefix(sub, "../") || strings.Contains(sub, "/../") {
			return fmt.Errorf("市场条目的 subdir %q 不能是绝对路径或包含 ..", e.Subdir)
		}
	}
	// 打包是“产出校验值”的一侧（官网 /download 与 /download.sha256 都由它生成），
	// 这里只取上游源码，不做完整性校验；校验发生在消费侧的 Download。
	src, _, err := downloadSource(ctx, e)
	if err != nil {
		return err
	}
	defer os.Remove(src) //nolint:errcheck

	work, err := os.MkdirTemp("", "nexadmin-market-pkg-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(work) //nolint:errcheck

	unpack := filepath.Join(work, "src")
	if err := os.MkdirAll(unpack, 0o755); err != nil {
		return err
	}
	if err := extractZip(src, unpack); err != nil {
		return fmt.Errorf("解压插件包失败: %w", err)
	}
	repoRoot, err := locateRepoRoot(unpack)
	if err != nil {
		return err
	}

	pkgDir := repoRoot
	if e.Subdir != "" {
		pkgDir = filepath.Join(repoRoot, filepath.FromSlash(e.Subdir))
		if !isDir(pkgDir) {
			return fmt.Errorf("市场条目的 subdir %q 在源码包里不存在", e.Subdir)
		}
	}
	if !fileExists(filepath.Join(pkgDir, plugin.ManifestFile)) {
		p, err := locatePackage(pkgDir)
		if err != nil {
			return fmt.Errorf("源码包里找不到 %s（目录的 subdir 是否正确？）", plugin.ManifestFile)
		}
		pkgDir = p
	}

	man, err := plugin.LoadManifest(pkgDir)
	if err != nil {
		return err
	}
	if man.ID != e.ID {
		return fmt.Errorf("源码包里的插件 id 是 %q，与市场条目 %q 不一致", man.ID, e.ID)
	}
	if e.Edition != "" && man.Edition != e.Edition {
		return fmt.Errorf("源码包里的 edition 是 %q，与市场目录的 %q 不一致", man.Edition, e.Edition)
	}
	if e.Version != "" && man.Version != e.Version {
		return fmt.Errorf("源码包里的版本是 %q，与市场目录的 %q 不一致", man.Version, e.Version)
	}

	return writePackageZip(dst, pkgDir)
}

// packageSkip 判定打包时要剔除的路径（相对插件目录）。授权文件与依赖目录绝不进发行包。
func packageSkip(rel string) bool {
	for _, part := range strings.Split(filepath.ToSlash(rel), "/") {
		switch part {
		case ".git", "node_modules", "__MACOSX":
			return true
		}
	}
	base := filepath.Base(rel)
	switch base {
	case ".DS_Store", ".env":
		return true
	}
	if strings.HasSuffix(base, ".lic") || strings.HasPrefix(base, ".env.") {
		return true
	}
	return false
}

// writePackageZip 按安装器约定写出 zip：plugin.yaml 在根，随后是 backend/ 与 web/。
// 结果先写到临时文件再替换，避免下载端读到半包。
func writePackageZip(dst, pkgDir string) (err error) {
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(dst), ".pkg-*.part")
	if err != nil {
		return err
	}
	tmp := f.Name()
	defer func() {
		if err != nil {
			f.Close()      //nolint:errcheck
			os.Remove(tmp) //nolint:errcheck
		}
	}()

	zw := zip.NewWriter(f)
	var total int64
	addFile := func(name, src string) error {
		st, err := os.Stat(src)
		if err != nil {
			return err
		}
		total += st.Size()
		if total > maxPackageBytes {
			return errors.New("插件包过大（超过 64MB）")
		}
		b, err := os.ReadFile(src)
		if err != nil {
			return err
		}
		w, err := zw.CreateHeader(&zip.FileHeader{Name: name, Method: zip.Deflate, Modified: zipEpoch})
		if err != nil {
			return err
		}
		_, err = w.Write(b)
		return err
	}

	if err := addFile(plugin.ManifestFile, filepath.Join(pkgDir, plugin.ManifestFile)); err != nil {
		return err
	}
	if !isDir(filepath.Join(pkgDir, "backend")) {
		return errors.New("插件包缺少 backend/ 目录（安装器要求后端源码）")
	}
	for _, prefix := range []string{"backend", "web"} {
		srcDir := filepath.Join(pkgDir, prefix)
		if !isDir(srcDir) {
			continue
		}
		err := filepath.WalkDir(srcDir, func(p string, d fs.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			rel, err := filepath.Rel(pkgDir, p)
			if err != nil {
				return err
			}
			if rel == prefix {
				return nil
			}
			if packageSkip(rel) {
				if d.IsDir() {
					return fs.SkipDir
				}
				return nil
			}
			if d.IsDir() || !d.Type().IsRegular() {
				return nil // 目录条目可省；符号链接等非常规文件不进包
			}
			return addFile(filepath.ToSlash(rel), p)
		})
		if err != nil {
			return err
		}
	}
	if err := zw.Close(); err != nil {
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmp, dst); err != nil {
		return err // dst 由调用方保证唯一；失败时由调用方清理临时文件
	}
	return nil
}
