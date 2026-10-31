package market

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLedgerLifecycle(t *testing.T) {
	root := t.TempDir()

	// 全新项目：文件不存在 == 空台账，不是错误。
	got, err := LoadLedger(root)
	if err != nil || len(got) != 0 {
		t.Fatalf("empty ledger: %v %v", got, err)
	}

	if err := AddLedger(root, "website", "0.6.0"); err != nil {
		t.Fatal(err)
	}
	if err := AddLedger(root, "cron", "1.2.0"); err != nil {
		t.Fatal(err)
	}
	got, err = LoadLedger(root)
	if err != nil || len(got) != 2 {
		t.Fatalf("load after add: %v %v", got, err)
	}
	if got[0].ID != "cron" || got[1].ID != "website" { // 按 id 排序，写入顺序无关
		t.Fatalf("ledger not sorted: %v", got)
	}
	if got[1].Version != "0.6.0" || got[1].InstalledAt.IsZero() {
		t.Fatalf("entry fields: %+v", got[1])
	}

	// 同 id 重装覆盖版本，不产生重复行。
	if err := AddLedger(root, "website", "0.7.0"); err != nil {
		t.Fatal(err)
	}
	got, _ = LoadLedger(root)
	if len(got) != 2 || got[1].Version != "0.7.0" {
		t.Fatalf("repeat add: %v", got)
	}

	if err := RemoveLedger(root, "website"); err != nil {
		t.Fatal(err)
	}
	// 删除不存在的 id 是空操作。
	if err := RemoveLedger(root, "nope"); err != nil {
		t.Fatal(err)
	}
	got, _ = LoadLedger(root)
	if len(got) != 1 || got[0].ID != "cron" {
		t.Fatalf("after remove: %v", got)
	}

	// 临时文件不能留在目录里。
	entries, err := os.ReadDir(filepath.Dir(LedgerPath(root)))
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if filepath.Ext(e.Name()) == ".json" && e.Name() != "installed.json" {
			t.Fatalf("leftover temp file: %s", e.Name())
		}
	}
}

func TestLedgerCorruptFileIsAnError(t *testing.T) {
	root := t.TempDir()
	p := LedgerPath(root)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte("{not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadLedger(root); err == nil {
		t.Fatal("corrupt ledger must be reported, not silently treated as empty")
	}
	if err := AddLedger(root, "cron", "1.0.0"); err == nil {
		t.Fatal("add must refuse to overwrite a corrupt ledger")
	}
}
