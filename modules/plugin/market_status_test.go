package plugin

// 市场页与「已安装」页必须同一口径：installed 只认安装台账。
// 母版仓库里每个源码插件的目录都在盘上，早先用「目录是否存在」判安装，
// 结果是「已安装」空空如也、市场页却对全部插件摆出「重装覆盖 / 卸载」——
// 而卸载动作删的是源码目录。这里把口径钉住。

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"nexadmin/core/auth"
	"nexadmin/core/plugin/market"
)

type marketRow struct {
	ID               string `json:"id"`
	Installed        bool   `json:"installed"`
	Bundled          bool   `json:"bundled"`
	InstalledVersion string `json:"installedVersion"`
}

func TestMarketClassifiesBundledApartFromInstalled(t *testing.T) {
	m := mount(t)
	config := filepath.Join(m.Root(), "config")
	if err := os.MkdirAll(config, 0o755); err != nil {
		t.Fatal(err)
	}
	catalog := filepath.Join(config, "plugin-market.yaml")
	// 目录条目必须给 repo（owner/name），否则 LoadCatalog 会拒（市场目录的既有校验）。
	body := "plugins:\n" +
		"  - id: " + proID + "\n    edition: pro\n    repo: test/never-downloaded\n" + // 注册表里有、台账里没有 → 内置
		"  - id: " + otherID + "\n    edition: pro\n    repo: test/never-downloaded\n" + // 台账里有 → 本机装过
		"  - id: catalog-only\n    edition: free\n    repo: test/never-downloaded\n" // 谁都没有 → 可安装
	if err := os.WriteFile(catalog, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Remove(catalog) })
	if err := market.AddLedger(m.Root(), otherID, "1.0.0"); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = market.RemoveLedger(m.Root(), otherID) })

	e, public := securityAPI(&auth.Claims{UserID: 1, Super: true})
	noop := marketActions{
		install: func(string, market.Entry, bool) string { return "job" },
		remove:  func(string, string, bool) string { return "job" },
		rebuild: func(string, bool) string { return "job" },
	}
	registerMarketWith(public, m, noop)

	w := securityRequest(e, &auth.Claims{UserID: 1, Super: true}, http.MethodGet, "/plugins/market", "")
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", w.Code, w.Body.String())
	}
	var resp struct {
		Code int         `json:"code"`
		Data []marketRow `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	byID := map[string]marketRow{}
	for _, row := range resp.Data {
		byID[row.ID] = row
	}

	bundled := byID[proID]
	if bundled.ID == "" {
		t.Fatal("内置插件没出现在市场列表里")
	}
	if bundled.Installed || !bundled.Bundled {
		t.Errorf("内置插件 installed=%v bundled=%v，应为 false/true", bundled.Installed, bundled.Bundled)
	}
	// 内置插件仍要带版本与授权状态：这页的说明就是「版本与授权状态」
	if bundled.InstalledVersion == "" {
		t.Error("内置插件应回显注册表里的版本")
	}

	installed := byID[otherID]
	if !installed.Installed || installed.Bundled {
		t.Errorf("台账里的插件 installed=%v bundled=%v，应为 true/false", installed.Installed, installed.Bundled)
	}

	missing := byID["catalog-only"]
	if missing.Installed || missing.Bundled {
		t.Errorf("未注册的条目 installed=%v bundled=%v，应都是 false（这才是可安装的样子）", missing.Installed, missing.Bundled)
	}
}
