package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestUpgradeTransaction(t *testing.T) {
	for _, fault := range []struct {
		op string
		n  int
	}{{"success", 0}, {"write", 1}, {"write", 2}, {"write", 3}, {"write", 4}, {"write", 5}, {"rename", 1}, {"rename", 2}, {"rename", 3}, {"rename", 4}, {"rename", 5}, {"delete", 1}, {"record", 1}, {"record", 2}, {"restore", 1}} {
		op := fault.op
		t.Run(fmt.Sprintf("%s-%d", op, fault.n), func(t *testing.T) {
			root := t.TempDir()
			pkg := t.TempDir()
			write := func(base, p, s string) {
				t.Helper()
				dst := filepath.Join(base, p)
				if err := os.MkdirAll(filepath.Dir(dst), 0755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(dst, []byte(s), 0640); err != nil {
					t.Fatal(err)
				}
			}
			write(root, "core/a.go", "old")
			write(root, "core/z.go", "deleted")
			write(root, "internal/modules/user.go", "user")
			write(root, filesRecord, "{}\n")
			write(root, frameworkManifest, `{"framework_version":"1.0.0"}`)
			write(pkg, frameworkManifest, `{"framework_version":"1.1.0"}`)
			write(pkg, "a.go", "new")
			write(pkg, "b.go", "added")
			plan := &upPlan{changes: []upChange{{Path: "core/a.go", Kind: "modify", src: filepath.Join(pkg, "a.go")}, {Path: "core/b.go", Kind: "add", src: filepath.Join(pkg, "b.go")}, {Path: "core/z.go", Kind: "delete"}}, newHashes: map[string]string{}}
			before := upSnapshot(t, root)
			fired := false
			calls := 0
			upgradeTransactionHook = func(kind, p string) error {
				if op == "restore" && kind == "restore" {
					return errors.New("restore injected")
				}
				target := op
				if op == "restore" {
					target = "delete"
				}
				if kind == target {
					calls++
				}
				if kind == target && calls == fault.n && !fired {
					fired = true
					return errors.New("injected " + kind)
				}
				return nil
			}
			defer func() { upgradeTransactionHook = nil }()
			err := upTransaction(root, filepath.Join(root, frameworkManifest), filepath.Join(pkg, frameworkManifest), "framework_version", "1.1.0", plan, map[string]string{}, &upManifest{FrameworkPaths: []string{"core/"}}, time.Now())
			if op == "success" {
				if err != nil {
					t.Fatal(err)
				}
			} else if err == nil {
				t.Fatal("expected failure")
			}
			if op == "restore" {
				if !strings.Contains(err.Error(), "recovery materials") {
					t.Fatal(err)
				}
				return
			}
			want := "old"
			if op == "success" {
				want = "new"
			}
			raw, _ := os.ReadFile(filepath.Join(root, "core/a.go"))
			if string(raw) != want {
				t.Fatalf("a=%q", raw)
			}
			if op != "success" {
				if !fired {
					t.Fatal("fault was not reached")
				}
				if upSnapshot(t, root) != before {
					t.Fatal("rollback did not restore complete contents and records")
				}
				if _, err := os.Stat(filepath.Join(root, "core/b.go")); !os.IsNotExist(err) {
					t.Fatal("added file remains")
				}
				st, err := os.Stat(filepath.Join(root, "core/a.go"))
				if err != nil || st.Mode().Perm() != 0640 {
					t.Fatal("permissions changed")
				}
			}
			raw, _ = os.ReadFile(filepath.Join(root, "internal/modules/user.go"))
			if string(raw) != "user" {
				t.Fatal("user changed")
			}
		})
	}
}

func TestCodegenOwnershipUpgrade(t *testing.T) {
	root := upNewProject(t)
	generated := "package generated\n// user codegen output\n"
	path := filepath.Join(root, "internal/modules/generated/module.go")
	write(t, path, generated)
	pkg := upPkg(t, `{"framework_version":"1.1.0","framework_paths":["core/"],"user_paths":["internal/"]}`, map[string]string{"core/a.go": "updated", "internal/modules/generated/module.go": "overwrite attempt"})
	if out, err := upRun(t, root, pkg, false); err != nil {
		t.Fatal(err, out)
	}
	if got := read(t, path); got != generated {
		t.Fatalf("generated user module overwritten: %s", got)
	}
}
