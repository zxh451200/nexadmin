package market

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestChangeSourcesRestoresFailedBuild(t *testing.T) {
	root := newRoot(t)
	original := map[string][]byte{}
	for _, rel := range []string{"plugins/demo/plugin.go", "plugins/plugins.go", "web/src/plugins/demo/route.tsx", "go.mod", "go.sum", "web/package.json", "web/package-lock.json", "web/dist/index.html", "plugins/other/plugin.go"} {
		write(t, filepath.Join(root, rel), "original:"+rel)
		var err error
		original[rel], err = os.ReadFile(filepath.Join(root, rel))
		if err != nil {
			t.Fatal(err)
		}
	}
	buildErr := errors.New("build failed")
	err := ChangeSources(root, "demo", func() error {
		for rel := range original {
			if rel != "plugins/other/plugin.go" {
				write(t, filepath.Join(root, rel), "changed")
			}
		}
		write(t, filepath.Join(root, "plugins/demo/new.go"), "new")
		write(t, filepath.Join(root, "web/dist/new.js"), "new bundle")
		return buildErr
	})
	if !errors.Is(err, buildErr) {
		t.Fatalf("lost original build failure: %v", err)
	}
	for rel, before := range original {
		after, err := os.ReadFile(filepath.Join(root, rel))
		if err != nil || !bytes.Equal(before, after) {
			t.Errorf("%s was not restored byte-for-byte: %q err=%v", rel, after, err)
		}
	}
	for _, rel := range []string{"plugins/demo/new.go", "web/dist/new.js"} {
		if _, err := os.Stat(filepath.Join(root, rel)); !errors.Is(err, os.ErrNotExist) {
			t.Errorf("new file was not removed: %s err=%v", rel, err)
		}
	}
}

func TestChangeSourcesNewInstallAndSuccess(t *testing.T) {
	for _, succeeds := range []bool{false, true} {
		root := newRoot(t)
		path := filepath.Join(root, "plugins/demo/plugin.go")
		err := ChangeSources(root, "demo", func() error {
			write(t, path, "new plugin")
			if !succeeds {
				return errors.New("failed")
			}
			return nil
		})
		if (err == nil) != succeeds || exists(path) != succeeds {
			t.Fatalf("success=%v exists=%v err=%v", succeeds, exists(path), err)
		}
		matches, err := filepath.Glob(filepath.Join(root, ".nexadmin-recovery-plugin-*"))
		if err != nil || len(matches) != 0 {
			t.Fatalf("unexpected backup residue: %v %v", matches, err)
		}
	}
}

func TestChangeSourcesRestoresAfterPanic(t *testing.T) {
	root := newRoot(t)
	path := filepath.Join(root, "plugins/demo/plugin.go")
	write(t, path, "before")
	err := ChangeSources(root, "demo", func() error {
		write(t, path, "after")
		panic("unexpected build failure")
	})
	if err == nil {
		t.Fatal("panic must be reported as a failure")
	}
	content, err := os.ReadFile(path)
	if err != nil || string(content) != "before" {
		t.Fatalf("panic left modified source: %q %v", content, err)
	}
}

func TestChangeSourcesRejectsInvalidID(t *testing.T) {
	if err := ChangeSources(t.TempDir(), "../other", func() error {
		t.Fatal("invalid ID must not invoke the change")
		return nil
	}); err == nil {
		t.Fatal("invalid ID was accepted")
	}
}
