package market

import (
	"os"
	"path/filepath"
	"testing"

	"nexadmin/core/plugin"
)

func catalogWith(t *testing.T, extra string) ([]Entry, error) {
	t.Helper()
	root := t.TempDir()
	body := "plugins:\n" +
		"  - id: cron\n" +
		"    name: 定时任务\n" +
		"    edition: pro\n" +
		"    version: 1.0.0\n" +
		"    repo: acme/plugins\n" +
		extra
	if err := os.MkdirAll(filepath.Join(root, "config"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "config", "plugin-market.yaml"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return LoadCatalog(root)
}

func TestCatalogScreenshotsAndVersions(t *testing.T) {
	entries, err := catalogWith(t, `
    screenshots:
      - /website-assets/og.png
      - https://example.com/a.png
    changelog_url: https://example.com/changelog
    versions:
      - version: 0.9.0
        date: 2026-01-02
        ref: v0.9.0
        notes: 首个版本
      - version: 0.8.0
        ref: v0.8.0
`)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Fatalf("entries = %d", len(entries))
	}
	e := entries[0]
	if len(e.Screenshots) != 2 || len(e.Versions) != 2 {
		t.Fatalf("screenshots=%d versions=%d", len(e.Screenshots), len(e.Versions))
	}
	if e.ChangelogURL != "https://example.com/changelog" {
		t.Errorf("changelog = %q", e.ChangelogURL)
	}
}

func TestCatalogRejectsBadDisplayFields(t *testing.T) {
	cases := []struct {
		name  string
		extra string
	}{
		{"screenshot protocol-relative", "    screenshots:\n      - //evil.example/x.png\n"},
		{"screenshot javascript", "    screenshots:\n      - javascript:alert(1)\n"},
		{"duplicate version", "    versions:\n      - version: 0.9.0\n        ref: v1\n      - version: 0.9.0\n        ref: v2\n"},
		{"bad ref", "    versions:\n      - version: 0.9.0\n        ref: ../x\n"},
		{"bad date", "    versions:\n      - version: 0.9.0\n        ref: v0.9.0\n        date: 2026/01/02\n"},
		{"bad version", "    versions:\n      - version: \"1.0 bad\"\n        ref: v1\n"},
	}
	for _, c := range cases {
		if _, err := catalogWith(t, c.extra); err == nil {
			t.Errorf("%s: accepted", c.name)
		}
	}
}

func TestEntryForVersion(t *testing.T) {
	e := Entry{ID: "cron", Version: "1.0.0", Ref: "v1.0.0", Edition: plugin.Pro,
		Versions: []Version{
			{Version: "0.9.0", Ref: "v0.9.0"},
			{Version: "0.8.0"},
		}}

	if got, ok := EntryForVersion(e, ""); !ok || got.Version != "1.0.0" || got.Ref != "v1.0.0" {
		t.Errorf("latest = %+v, %v", got, ok)
	}
	if got, ok := EntryForVersion(e, "1.0.0"); !ok || got.Version != "1.0.0" {
		t.Errorf("explicit latest = %+v, %v", got, ok)
	}
	if got, ok := EntryForVersion(e, "0.9.0"); !ok || got.Version != "0.9.0" || got.Ref != "v0.9.0" {
		t.Errorf("old = %+v, %v", got, ok)
	}
	if _, ok := EntryForVersion(e, "0.8.0"); ok {
		t.Error("version without ref should not be downloadable")
	}
	if _, ok := EntryForVersion(e, "9.9.9"); ok {
		t.Error("unknown version accepted")
	}
}
