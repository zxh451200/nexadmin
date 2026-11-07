package plugin

import (
	"context"
	"fmt"
	"os"
	"strings"

	"nexadmin/core/plugin"
	"nexadmin/core/plugin/market"
	"nexadmin/core/response"
)

type installOperations struct {
	download    func(context.Context, market.Entry) (string, error)
	rebuild     func(context.Context, string, bool, func(string)) error
	restart     func(string, *market.Ctx) error
	environment func(string) market.Environment
}

func productionInstallOperations() installOperations {
	return installOperations{market.Download, market.Rebuild, triggerRestart, market.CheckEnvironment}
}

func validateMarketIDs(ids []string) error {
	if len(ids) == 0 || len(ids) > 64 {
		return response.BadRequest("plugin.market_ids_invalid")
	}
	seen := make(map[string]bool, len(ids))
	for _, id := range ids {
		if !plugin.ValidID(id) || seen[id] {
			return response.BadRequest("plugin.market_ids_invalid")
		}
		seen[id] = true
	}
	return nil
}

func requireInstallEnvironment(root string, ops installOperations) error {
	e := ops.environment(root)
	if !e.Ready {
		return response.BadRequest("plugin.environment_unavailable", strings.Join(e.Issues, ", "))
	}
	return nil
}

func startInstall(root string, entries []market.Entry, opts market.InstallOptions) string {
	return marketRunner.Run("install", "", func(x *market.Ctx) error {
		return runInstall(root, entries, opts, x, productionInstallOperations())
	})
}

func catalogOptions(e market.Entry, opts market.InstallOptions) market.InstallOptions {
	opts.AllowIncompatible = false
	opts.ExpectedID, opts.ExpectedVersion, opts.ExpectedEdition = e.ID, e.Version, e.Edition
	return opts
}

func runInstall(root string, entries []market.Entry, opts market.InstallOptions, x *market.Ctx, ops installOperations) error {
	ids := make([]string, len(entries))
	for i, e := range entries {
		ids[i] = e.ID
	}
	if err := validateMarketIDs(ids); err != nil {
		return err
	}
	if err := requireInstallEnvironment(root, ops); err != nil {
		return err
	}
	packages := make([]string, len(entries))
	defer func() {
		for _, path := range packages {
			if path != "" {
				_ = os.Remove(path)
			}
		}
	}()
	for i, e := range entries {
		x.Log("预检插件 %s %s：下载并核对安装包", e.ID, e.Version)
		path, err := ops.download(context.Background(), e)
		if err != nil {
			return err
		}
		packages[i] = path
		if _, err := market.CheckInstall(root, path, e.Subdir, catalogOptions(e, opts)); err != nil {
			return err
		}
	}
	x.Log("%d 个插件预检通过；覆盖前的源码备份保留在 data/plugins/backups/", len(entries))
	if err := market.ChangeSourcesBatch(root, ids, func() error {
		for i, e := range entries {
			if _, err := market.InstallSub(root, packages[i], e.Subdir, catalogOptions(e, opts)); err != nil {
				return err
			}
			x.Log("插件 %s 已装入源码树", e.ID)
		}
		x.Log("统一构建前后端，仅重启一次")
		if err := ops.rebuild(context.Background(), root, true, x.LogPlain); err != nil {
			return fmt.Errorf("重建失败: %w", err)
		}
		return nil
	}); err != nil {
		return err
	}
	return ops.restart(root, x)
}

func runUpload(root, zipPath string, x *market.Ctx, ops installOperations) error {
	defer os.Remove(zipPath)
	if err := requireInstallEnvironment(root, ops); err != nil {
		return err
	}
	man, err := market.CheckInstall(root, zipPath, "", market.InstallOptions{})
	if err != nil {
		return err
	}
	opts := market.InstallOptions{ExpectedID: man.ID, ExpectedVersion: man.Version, ExpectedEdition: man.Edition}
	if err := market.ChangeSources(root, man.ID, func() error {
		x.Log("安装上传的插件包 %s", man.ID)
		if _, err := market.InstallSub(root, zipPath, "", opts); err != nil {
			return err
		}
		return ops.rebuild(context.Background(), root, true, x.LogPlain)
	}); err != nil {
		return err
	}
	return ops.restart(root, x)
}
