package main

import (
	"bytes"
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSourcePackageBoundary(t *testing.T) {
	for _, p := range []string{".nexadmin-recovery-example/journal.json", "bin/server", "web/__pycache__/x", "scripts/a.pyc", "web/.env.production.local", "web/.env.local"} {
		if !newSkip(p, false, true) {
			t.Errorf("not excluded: %s", p)
		}
	}
	for _, p := range []string{"web/.env.production", "config/config.example.yaml", "core/x.go", "migrations/001.sql", "web/src/locales/en.json"} {
		if newSkip(p, false, true) {
			t.Errorf("required file excluded: %s", p)
		}
	}
}

func TestSourcePackageBoundaryArchive(t *testing.T) {
	root := makeTemplate(t)
	wfile(t, root, frameworkManifest, `{"framework_version":"0.2.0","framework_paths":["core/"],"user_paths":["internal/"]}`)
	excluded := []string{"bin/server", "web/__pycache__/cache", "scripts/a.pyc", "web/.env.production.local", "web/.env.local"}
	required := []string{"web/.env.production", "config/config.example.yaml", "core/necessary.go", "migrations/framework/001.sql", "web/src/locales/en.json"}
	for _, p := range append(append([]string{}, excluded...), required...) {
		wfile(t, root, p, "fixture")
	}
	output := filepath.Join(t.TempDir(), "source.zip")
	var report bytes.Buffer
	if err := releasePack(root, output, true, &report); err != nil {
		t.Fatal(err)
	}
	entries := map[string]bool{}
	for _, name := range zipNames(t, output) {
		entries[strings.TrimPrefix(name, "nexadmin-0.2.0/")] = true
	}
	for _, name := range excluded {
		if entries[name] {
			t.Errorf("artifact archived: %s", name)
		}
	}
	for _, name := range required {
		if !entries[name] {
			t.Errorf("required file absent: %s", name)
		}
	}
	data, err := os.ReadFile(output)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(report.String(), fmt.Sprintf("%x", sha256.Sum256(data))) {
		t.Fatal("reported checksum differs from archive")
	}
	copied := filepath.Join(t.TempDir(), "new")
	if _, err := copyTemplate(root, copied, false); err != nil {
		t.Fatal(err)
	}
	got := treeFiles(t, copied)
	if len(got) != len(entries) {
		t.Fatal("new and pack entries differ")
	}
	for _, name := range got {
		if !entries[name] {
			t.Errorf("new-only entry %s", name)
		}
	}
}

func TestSourcePackageBoundarySymlink(t *testing.T) {
	root := makeTemplate(t)
	if err := os.Symlink(t.TempDir(), filepath.Join(root, "escape")); err != nil {
		t.Fatal(err)
	}
	if _, err := copyTemplate(root, filepath.Join(t.TempDir(), "new"), false); err == nil {
		t.Fatal("new accepted symlink")
	}
	if err := releasePack(root, filepath.Join(t.TempDir(), "bad.zip"), true, &bytes.Buffer{}); err == nil {
		t.Fatal("pack accepted symlink")
	}
}
