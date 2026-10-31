package plugin

import (
	"testing"

	coreplugin "nexadmin/core/plugin"
	"nexadmin/core/plugin/market"
)

// 全新项目（台账为空）：所有插件都算内置，已安装页为空。
func TestClassifyInstalledEmptyLedger(t *testing.T) {
	infos := []coreplugin.Info{{ID: "website"}, {ID: "cron"}}
	got := classifyInstalled(infos, map[string]string{"website": "content"}, nil)
	if len(got) != 2 {
		t.Fatalf("want 2 entries, got %d", len(got))
	}
	for _, e := range got {
		if !e.Bundled {
			t.Fatalf("%s should be bundled with an empty ledger", e.ID)
		}
	}
	if got[0].Category != "content" {
		t.Fatalf("category not carried over: %+v", got[0])
	}
}

// 台账里的插件算已安装；不在注册表里的台账记录不会凭空多出一行。
func TestClassifyInstalledWithLedger(t *testing.T) {
	infos := []coreplugin.Info{{ID: "website"}, {ID: "cron"}}
	ledger := []market.LedgerEntry{{ID: "website", Version: "0.6.0"}}
	got := classifyInstalled(infos, nil, ledger)
	if len(got) != len(infos) {
		t.Fatalf("ledger entries must not add rows: got %d", len(got))
	}
	if got[0].Bundled || got[0].ID != "website" {
		t.Fatalf("website should be user-installed: %+v", got[0])
	}
	if !got[1].Bundled || got[1].ID != "cron" {
		t.Fatalf("cron should stay bundled: %+v", got[1])
	}
}
