package plugin

import (
	"crypto/ed25519"
	"crypto/rand"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// disabled_plugins 命中的插件：完全不挂载，状态 disabled；导入授权被拒绝。
func TestDisabledPlugin(t *testing.T) {
	pub, _, _ := ed25519.GenerateKey(rand.Reader)
	root := t.TempDir()
	dir := filepath.Join(root, "plugins", "cron")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	y := "id: cron\nname: cron\nversion: 1.0.0\nedition: pro\n"
	if err := os.WriteFile(filepath.Join(dir, "plugin.yaml"), []byte(y), 0o644); err != nil {
		t.Fatal(err)
	}

	m := newManager([]Plugin{{ID: "cron", Edition: Pro, Module: fakeModule{}}},
		Options{Root: root, PublicKey: pub, Now: func() time.Time { return now }, DisabledPlugins: []string{"cron"}})
	list := m.List()
	if len(list) != 1 || list[0].Status != StatusDisabled {
		t.Fatalf("want disabled, got %+v", list)
	}
	if _, dormant := m.Routes(); len(dormant) != 0 {
		t.Fatalf("disabled plugin must not be dispatchable, got %v", dormant)
	}
	// 停用时不校验授权：导入一律拒绝，也不落盘。
	if _, err := m.ImportLicense("cron", "whatever"); err == nil || !strings.Contains(err.Error(), "disabled_plugins") {
		t.Fatalf("import on disabled plugin should be refused, got %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "data", "licenses", "cron.lic")); !os.IsNotExist(err) {
		t.Fatal("license must not be written for disabled plugin")
	}

	// 未停用的插件不受影响（other 也需要清单）。
	if err := os.MkdirAll(filepath.Join(root, "plugins", "other"), 0o755); err != nil {
		t.Fatal(err)
	}
	y2 := "id: other\nname: other\nversion: 1.0.0\nedition: free\n"
	if err := os.WriteFile(filepath.Join(root, "plugins", "other", "plugin.yaml"), []byte(y2), 0o644); err != nil {
		t.Fatal(err)
	}
	m2 := newManager([]Plugin{
		{ID: "cron", Edition: Pro, Module: fakeModule{}},
		{ID: "other", Edition: Free, Module: fakeModule{}},
	}, Options{Root: root, PublicKey: pub, Now: func() time.Time { return now }, DisabledPlugins: []string{"nope"}})
	got := map[string]string{}
	for _, i := range m2.List() {
		got[i.ID] = i.Status
	}
	if got["cron"] != StatusUnlicensed || got["other"] != StatusActive {
		t.Fatalf("unrelated plugins affected: %v", got)
	}
}
