package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// A test seam for filesystem execution failures; nil in production.
var upgradeTransactionHook func(operation, path string) error

func upTxnHook(op, path string) error {
	if upgradeTransactionHook != nil {
		return upgradeTransactionHook(op, path)
	}
	return nil
}

type upTxnEntry struct {
	Path    string
	Existed bool
	Mode    fs.FileMode
	Old     string
	New     string
}

// upTransaction stages all results before changing the project. The journal is
// retained if recovery fails. This is filesystem error recovery, not a power-loss
// guarantee and does not roll back database migrations.
func upTransaction(root, rootMan, pkgMan, key, value string, plan *upPlan, rec map[string]string, man *upManifest, now time.Time) error {
	real, err := filepath.EvalSymlinks(root)
	if err != nil {
		return err
	}
	relMan, err := filepath.Rel(root, rootMan)
	if err != nil {
		return err
	}
	if relMan == ".." || strings.HasPrefix(relMan, ".."+string(filepath.Separator)) || filepath.IsAbs(relMan) {
		return fmt.Errorf("manifest outside project")
	}
	work, err := os.MkdirTemp(root, ".nexadmin-recovery-")
	if err != nil {
		return err
	}
	keep := false
	defer func() {
		if !keep {
			_ = os.RemoveAll(work)
		}
	}()
	stage := filepath.Join(work, "stage")
	if err := os.Mkdir(stage, 0700); err != nil {
		return err
	}
	affected := map[string]bool{filesRecord: true, filepath.ToSlash(relMan): true}
	for _, c := range plan.changes {
		if c.Kind != "keep" {
			affected[c.Path] = true
		}
	}
	for _, m := range plan.merges {
		affected[m.Path] = true
	}
	// Validate every destination and snapshot its original bytes and permission.
	paths := make([]string, 0, len(affected))
	for p := range affected {
		paths = append(paths, p)
	}
	sort.Strings(paths)
	for _, rel := range paths {
		if err := upCheckDest(real, root, rel); err != nil {
			return err
		}
		src := filepath.Join(root, filepath.FromSlash(rel))
		st, err := os.Lstat(src)
		if errors.Is(err, fs.ErrNotExist) {
			continue
		}
		if err != nil {
			return err
		}
		if !st.Mode().IsRegular() {
			return fmt.Errorf("nonregular transaction path %s", rel)
		}
		dst := filepath.Join(stage, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(dst), 0755); err != nil {
			return err
		}
		if err := copyFile(src, dst); err != nil {
			return err
		}
		if err := os.Chmod(dst, st.Mode().Perm()); err != nil {
			return err
		}
	}
	stagedRec := map[string]string{}
	for k, v := range rec {
		stagedRec[k] = v
	}
	stagedPlan := *plan
	stagedPlan.changes = append([]upChange(nil), plan.changes...)
	for i, c := range stagedPlan.changes {
		if !c.Backup {
			continue
		}
		original := filepath.Join(root, filepath.FromSlash(c.Path))
		backup := upBackupName(original)
		rel, err := filepath.Rel(root, backup)
		if err != nil {
			return err
		}
		if err := upCheckDest(real, root, filepath.ToSlash(rel)); err != nil {
			return err
		}
		dst := filepath.Join(stage, rel)
		if err := os.MkdirAll(filepath.Dir(dst), 0755); err != nil {
			return err
		}
		if err := copyFile(original, dst); err != nil {
			return err
		}
		stagedPlan.changes[i].Backup = false
	}
	if err := upApply(stage, &stagedPlan, stagedRec, man, now); err != nil {
		return err
	}
	if err := upApplyMerges(stage, plan.merges); err != nil {
		return err
	}
	if err := upSyncManifest(filepath.Join(stage, relMan), pkgMan, key, value); err != nil {
		return err
	}
	// Include backup and trash artifacts produced by the existing policy.
	if err := filepath.WalkDir(stage, func(p string, d fs.DirEntry, e error) error {
		if e != nil {
			return e
		}
		if !d.IsDir() {
			rel, e := filepath.Rel(stage, p)
			if e != nil {
				return e
			}
			affected[filepath.ToSlash(rel)] = true
		}
		return nil
	}); err != nil {
		return err
	}
	paths = paths[:0]
	for p := range affected {
		paths = append(paths, p)
	}
	sort.Slice(paths, func(i, j int) bool {
		record := func(p string) bool { return p == filesRecord || p == filepath.ToSlash(relMan) }
		if record(paths[i]) != record(paths[j]) {
			return !record(paths[i])
		}
		return paths[i] < paths[j]
	})
	entries := []upTxnEntry{}
	for i, rel := range paths {
		if err := upCheckDest(real, root, rel); err != nil {
			return err
		}
		e := upTxnEntry{Path: rel}
		dst := filepath.Join(root, filepath.FromSlash(rel))
		st, err := os.Lstat(dst)
		if err == nil {
			if !st.Mode().IsRegular() {
				return fmt.Errorf("nonregular destination %s", rel)
			}
			e.Existed = true
			e.Mode = st.Mode().Perm()
			e.Old = filepath.Join(work, fmt.Sprintf("old-%d", i))
			if err := copyFile(dst, e.Old); err != nil {
				return err
			}
		} else if !errors.Is(err, fs.ErrNotExist) {
			return err
		}
		next := filepath.Join(stage, filepath.FromSlash(rel))
		if st, err := os.Stat(next); err == nil {
			e.New = next
			if !e.Existed {
				e.Mode = st.Mode().Perm()
			}
		} else if !errors.Is(err, fs.ErrNotExist) {
			return err
		}
		entries = append(entries, e)
	}
	raw, err := json.MarshalIndent(entries, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(work, "journal.json"), raw, 0600); err != nil {
		return err
	}
	created := []string{}
	atomic := func(src, dst string, mode fs.FileMode) error {
		missing := []string{}
		for dir := filepath.Dir(dst); dir != root; dir = filepath.Dir(dir) {
			if _, err := os.Stat(dir); err == nil {
				break
			} else if !errors.Is(err, fs.ErrNotExist) {
				return err
			}
			missing = append(missing, dir)
		}
		for i := len(missing) - 1; i >= 0; i-- {
			if err := os.Mkdir(missing[i], 0755); err != nil {
				return err
			}
			created = append(created, missing[i])
		}
		f, err := os.CreateTemp(filepath.Dir(dst), ".nexadmin-txn-")
		if err != nil {
			return err
		}
		name := f.Name()
		if err := f.Close(); err != nil {
			return err
		}
		defer os.Remove(name)
		if err := upTxnHook("write", dst); err != nil {
			return err
		}
		if err := copyFile(src, name); err != nil {
			return err
		}
		if err := os.Chmod(name, mode); err != nil {
			return err
		}
		if err := upTxnHook("rename", dst); err != nil {
			return err
		}
		return os.Rename(name, dst)
	}
	applied := 0
	rollback := func(cause error) error {
		var failures []error
		for i := applied - 1; i >= 0; i-- {
			e := entries[i]
			dst := filepath.Join(root, filepath.FromSlash(e.Path))
			if err := upTxnHook("restore", dst); err != nil {
				failures = append(failures, err)
				continue
			}
			var err error
			if e.Existed {
				err = atomic(e.Old, dst, e.Mode)
			} else {
				err = os.Remove(dst)
				if errors.Is(err, fs.ErrNotExist) {
					err = nil
				}
			}
			if err != nil {
				failures = append(failures, fmt.Errorf("%s: %w", e.Path, err))
			}
		}
		for i := len(created) - 1; i >= 0; i-- {
			if err := os.Remove(created[i]); err != nil && !errors.Is(err, fs.ErrNotExist) {
				failures = append(failures, err)
			}
		}
		if len(failures) > 0 {
			keep = true
			return fmt.Errorf("upgrade failed: %w; rollback failed: %v; recovery materials: %s", cause, errors.Join(failures...), work)
		}
		return fmt.Errorf("upgrade failed, original files restored: %w", cause)
	}
	for i, e := range entries {
		dst := filepath.Join(root, filepath.FromSlash(e.Path))
		applied = i + 1
		if e.Path == filesRecord || e.Path == filepath.ToSlash(relMan) {
			if err := upTxnHook("record", dst); err != nil {
				return rollback(err)
			}
		}
		if e.New == "" {
			if !e.Existed {
				continue
			}
			if err := upTxnHook("delete", dst); err != nil {
				return rollback(err)
			}
			if err := os.Remove(dst); err != nil {
				return rollback(err)
			}
		} else if err := atomic(e.New, dst, e.Mode); err != nil {
			return rollback(err)
		}
	}
	for k := range rec {
		delete(rec, k)
	}
	for k, v := range stagedRec {
		rec[k] = v
	}
	return nil
}
