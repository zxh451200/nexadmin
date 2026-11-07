package market

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"nexadmin/core/plugin"
)

// InstallOptions 分别授予覆盖、不兼容安装及覆盖本地修改的许可，不能相互替代。
// Expected 字段非空时必须与下载包清单完全一致。
type InstallOptions struct {
	Overwrite         bool
	AllowIncompatible bool
	AllowModified     bool
	ExpectedID        string
	ExpectedVersion   string
	ExpectedEdition   plugin.Edition
}

type installPlan struct {
	manifest     *plugin.Manifest
	temp         string
	backend      string
	web          string
	newGo        []byte
	status       UpdateStatus
	incompatible error
}

// CheckInstall 与 InstallSub 使用同一准备流程，只写系统临时目录，不改项目源码、台账或备份。
// 预检不能锁定后续状态；真正安装始终重新检查，调用方无须先调用预检。
func CheckInstall(root, src, sub string, opts InstallOptions) (*plugin.Manifest, error) {
	plan, err := prepareInstall(root, src, sub, opts)
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(plan.temp) //nolint:errcheck
	return plan.manifest, nil
}

func prepareInstall(root, src, sub string, opts InstallOptions) (_ *installPlan, err error) {
	// 本地目录和 zip 的子路径使用相同的防穿越判据。
	if sub != "" {
		if _, err := safeJoin(".", sub); err != nil {
			return nil, err
		}
	}
	st, err := os.Lstat(src)
	if err != nil {
		return nil, err
	}
	if !st.IsDir() && !st.Mode().IsRegular() {
		return nil, fmt.Errorf("不支持的插件包文件类型: %s", src)
	}
	tmp, err := os.MkdirTemp("", "nexadmin-plugin-")
	if err != nil {
		return nil, err
	}
	defer func() {
		if err != nil {
			_ = os.RemoveAll(tmp)
		}
	}()
	dir := src
	if !st.IsDir() {
		unpacked := filepath.Join(tmp, "package")
		if err := os.MkdirAll(unpacked, 0o755); err != nil {
			return nil, err
		}
		if err := extractZip(src, unpacked); err != nil {
			return nil, err
		}
		if sub != "" {
			dir, err = locateRepoRoot(unpacked)
		} else {
			dir, err = locatePackage(unpacked)
		}
		if err != nil {
			return nil, err
		}
	}
	if sub != "" {
		if err := checkSourcePath(dir, sub); err != nil {
			return nil, err
		}
		dir, err = safeJoin(dir, sub)
		if err != nil {
			return nil, err
		}
		if !isDir(dir) {
			return nil, fmt.Errorf("包里找不到子目录 %q", sub)
		}
	}
	if err := checkSourcePath(dir, plugin.ManifestFile); err != nil {
		return nil, err
	}
	raw, err := os.ReadFile(filepath.Join(dir, plugin.ManifestFile))
	if err != nil {
		return nil, err
	}
	man, err := plugin.ParseManifest(raw)
	if err != nil {
		return nil, err
	}
	for _, expected := range []struct{ field, want, got string }{
		{"id", opts.ExpectedID, man.ID},
		{"version", opts.ExpectedVersion, man.Version},
		{"edition", string(opts.ExpectedEdition), string(man.Edition)},
	} {
		if expected.want != "" && expected.want != expected.got {
			return nil, fmt.Errorf("插件清单 %s 不匹配: 期望 %q，实际 %q", expected.field, expected.want, expected.got)
		}
	}
	plan := &installPlan{manifest: man, temp: tmp}
	plan.incompatible = plugin.CheckRequires(man.RequiresNexadmin, plugin.FrameworkVersion(root, ""))
	if plan.incompatible != nil && !opts.AllowIncompatible {
		return nil, fmt.Errorf("%w（仅显式 AllowIncompatible 可跳过兼容性校验）", plan.incompatible)
	}
	// InspectUpdate 同时读取并校验整份台账；损坏台账在创建项目备份或改写源码前报错。
	plan.status, err = InspectUpdate(root, man.ID)
	if err != nil {
		return nil, err
	}
	if plan.status.Exists {
		if !opts.Overwrite {
			return nil, fmt.Errorf("插件 %s 已安装（需显式 Overwrite 允许覆盖）", man.ID)
		}
		if !opts.AllowModified {
			if !plan.status.BaselineKnown {
				return nil, fmt.Errorf("插件 %s 无安装内容基线，无法确认本地修改（需显式 AllowModified 允许覆盖）", man.ID)
			}
			if plan.status.Modified {
				return nil, fmt.Errorf("插件 %s 存在本地修改（需显式 AllowModified 允许覆盖）", man.ID)
			}
		}
	}
	for _, rel := range []string{"go.mod", "plugins/plugins.go", "data/plugins/backups"} {
		if err := checkSourcePath(root, rel); err != nil {
			return nil, err
		}
	}
	mod, err := modulePath(root)
	if err != nil {
		return nil, err
	}
	oldGo, err := os.ReadFile(filepath.Join(root, "plugins", "plugins.go"))
	if err != nil {
		return nil, err
	}
	plan.newGo, err = updatePluginsFile(oldGo, mod+"/plugins/"+man.ID, true)
	if err != nil {
		return nil, err
	}
	backend, web := filepath.Join(dir, "backend"), filepath.Join(dir, "web")
	if err := checkSourcePath(dir, "backend"); err != nil {
		return nil, err
	}
	if !isDir(backend) {
		return nil, errors.New("插件包缺少 backend/ 目录")
	}
	plan.backend = filepath.Join(tmp, "backend")
	if err := copyTree(backend, plan.backend); err != nil {
		return nil, err
	}
	if err := rewriteModuleImports(plan.backend, mod); err != nil {
		return nil, err
	}
	// 使用已校验的清单字节，不再从下载目录读取另一份清单。
	if err := os.WriteFile(filepath.Join(plan.backend, plugin.ManifestFile), raw, 0o644); err != nil {
		return nil, err
	}
	if err := checkSourcePath(dir, "web"); err != nil {
		return nil, err
	}
	webInfo, err := os.Lstat(web)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	if err == nil {
		if !webInfo.IsDir() {
			return nil, errors.New("插件包 web/ 不是目录")
		}
		plan.web = filepath.Join(tmp, "web")
		if err := copyTree(web, plan.web); err != nil {
			return nil, err
		}
	}
	return plan, nil
}
