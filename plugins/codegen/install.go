package codegen

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"go/format"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"

	"nexadmin/core/response"
)

type InstallFile struct {
	Path          string `json:"path"`
	Action        string `json:"action"`
	CurrentHash   string `json:"currentHash,omitempty"`
	GeneratedHash string `json:"generatedHash,omitempty"`
	Content       string `json:"-"`
}

type InstallPlan struct {
	Root          string            `json:"root"`
	CanInstall    bool              `json:"canInstall"`
	Files         []InstallFile     `json:"files"`
	Validation    *ValidationReport `json:"validation"`
	RegisterNote  string            `json:"registerNote,omitempty"`
	RegisterError string            `json:"registerError,omitempty"`
	BackupDir     string            `json:"backupDir,omitempty"`
}

type InstallResult struct {
	Root          string            `json:"root"`
	Written       []string          `json:"written"`
	Skipped       []string          `json:"skipped"`
	Overwritten   []string          `json:"overwritten"`
	BackupDir     string            `json:"backupDir,omitempty"`
	Registered    bool              `json:"registered"`
	RegisterNote  string            `json:"registerNote,omitempty"`
	RegisterError string            `json:"registerError,omitempty"`
	Validation    *ValidationReport `json:"validation,omitempty"`
}

const (
	modulesMarkBegin = "// nexadmin:modules:begin"
	modulesMarkEnd   = "// nexadmin:modules:end"
)

func (s *Service) PlanInstall(ctx context.Context, req GenRequest, overwrite, register bool) (*InstallPlan, error) {
	spec, out, err := s.generate(ctx, req)
	if err != nil {
		return nil, err
	}
	root, err := projectRoot()
	if err != nil {
		return nil, response.BadRequest("codegen.install_no_root").Wrap(err)
	}
	plan, err := buildInstallPlan(root, out)
	if err != nil {
		return nil, response.BadRequest("codegen.install_conflict").Wrap(err)
	}
	blocked := map[string]bool{}
	for _, issue := range out.Validation.Issues {
		if issue.Code == "migration_applied" || issue.Code == "migration_exists" {
			blocked[issue.Path] = true
		}
	}
	for _, f := range out.Files {
		if !validOutPath(f.Path) {
			return nil, response.Internal().Wrap(fmt.Errorf("unsafe output path %q", f.Path))
		}
		entry := InstallFile{Path: f.Path, GeneratedHash: contentHash(f.Content), Content: f.Content}
		full, err := safeTarget(root, f.Path)
		if err != nil {
			return nil, response.BadRequest("codegen.install_conflict").Wrap(err)
		}
		old, err := os.ReadFile(full)
		switch {
		case errors.Is(err, fs.ErrNotExist):
			entry.Action = "new"
		case err != nil:
			return nil, response.BadRequest("codegen.install_conflict").Wrap(err)
		case string(old) == f.Content:
			entry.Action = "unchanged"
		case blocked[f.Path]:
			entry.Action = "protected"
		case !overwrite:
			entry.Action = "skipped"
		default:
			entry.Action = "overwrite"
		}
		if err == nil {
			entry.CurrentHash = contentHash(string(old))
		}
		plan.Files = append(plan.Files, entry)
	}
	if register {
		content, changed, note, regErr := moduleRegistration(root, spec.GoModule+"/internal/modules/"+spec.Module)
		plan.RegisterNote, plan.RegisterError = note, ""
		if regErr != nil {
			plan.RegisterNote, plan.RegisterError = "failed", regErr.Error()
		} else if changed {
			old, _ := os.ReadFile(filepath.Join(root, "internal", "modules", "modules.go"))
			plan.Files = append(plan.Files, InstallFile{Path: "internal/modules/modules.go", Action: "register", CurrentHash: contentHash(string(old)), GeneratedHash: contentHash(content), Content: content})
		}
	}
	for _, f := range plan.Files {
		if f.Action == "protected" {
			plan.CanInstall = false
		}
	}
	return plan, nil
}

func buildInstallPlan(root string, out *Output) (*InstallPlan, error) {
	resolved, err := filepath.EvalSymlinks(root)
	if err != nil {
		return nil, err
	}
	st, err := os.Stat(resolved)
	if err != nil || !st.IsDir() {
		return nil, fmt.Errorf("project root is not a directory")
	}
	return &InstallPlan{Root: resolved, CanInstall: out.Validation != nil && out.Validation.OK, Files: []InstallFile{}, Validation: out.Validation}, nil
}

func (s *Service) Install(ctx context.Context, req GenRequest, overwrite, register bool) (*InstallResult, error) {
	plan, err := s.PlanInstall(ctx, req, overwrite, register)
	if err != nil {
		return nil, err
	}
	if !plan.CanInstall {
		return nil, response.BadRequest("codegen.validation_failed")
	}
	if err := commitInstall(plan); err != nil {
		return nil, response.Internal().Wrap(err)
	}
	return plan.result(), nil
}

func (p *InstallPlan) result() *InstallResult {
	res := &InstallResult{Root: p.Root, Written: []string{}, Skipped: []string{}, Overwritten: []string{}, BackupDir: p.BackupDir, RegisterNote: p.RegisterNote, RegisterError: p.RegisterError, Validation: p.Validation}
	for _, f := range p.Files {
		switch f.Action {
		case "new", "overwrite", "register":
			res.Written = append(res.Written, f.Path)
			if f.Action == "register" {
				res.Registered = true
			}
		case "skipped", "unchanged":
			res.Skipped = append(res.Skipped, f.Path)
		}
		if f.Action == "overwrite" || f.Action == "register" {
			res.Overwritten = append(res.Overwritten, f.Path)
		}
	}
	return res
}

type installBackup struct {
	Path        string      `json:"path"`
	Action      string      `json:"action"`
	Mode        fs.FileMode `json:"mode"`
	Hash        string      `json:"hash,omitempty"`
	CurrentHash string      `json:"currentHash,omitempty"`
}

func commitInstall(plan *InstallPlan) error {
	backup, err := os.MkdirTemp("", "nexadmin-codegen-backup-")
	if err != nil {
		return fmt.Errorf("create backup: %w", err)
	}
	keep := false
	defer func() {
		if !keep {
			_ = os.RemoveAll(backup)
		}
	}()
	staging := filepath.Join(backup, "generated")
	originals := filepath.Join(backup, "original")
	manifest := make([]installBackup, 0, len(plan.Files))
	var writes []InstallFile
	for _, f := range plan.Files {
		if f.Action == "skipped" || f.Action == "unchanged" || f.Action == "protected" {
			continue
		}
		full, err := safeTarget(plan.Root, f.Path)
		if err != nil {
			return err
		}
		stage := filepath.Join(staging, filepath.FromSlash(f.Path))
		if err := os.MkdirAll(filepath.Dir(stage), 0o700); err != nil {
			return fmt.Errorf("stage %s: %w", f.Path, err)
		}
		if err := os.WriteFile(stage, []byte(f.Content), 0o600); err != nil {
			return fmt.Errorf("stage %s: %w", f.Path, err)
		}
		old, err := os.ReadFile(full)
		if f.CurrentHash != "" && (err != nil || contentHash(string(old)) != f.CurrentHash) {
			return fmt.Errorf("target changed after planning: %s", f.Path)
		}
		if f.CurrentHash == "" {
			if _, statErr := os.Lstat(full); statErr == nil {
				return fmt.Errorf("new target appeared after planning: %s", f.Path)
			} else if !errors.Is(statErr, fs.ErrNotExist) {
				return statErr
			}
		}
		rec := installBackup{Path: f.Path, Action: f.Action, Mode: 0o644, Hash: f.GeneratedHash, CurrentHash: f.CurrentHash}
		if err == nil {
			if st, statErr := os.Stat(full); statErr == nil {
				rec.Mode = st.Mode().Perm()
			}
			dest := filepath.Join(originals, filepath.FromSlash(f.Path))
			if err := os.MkdirAll(filepath.Dir(dest), 0o700); err != nil {
				return err
			}
			if err := os.WriteFile(dest, old, rec.Mode); err != nil {
				return fmt.Errorf("backup %s: %w", f.Path, err)
			}
		} else if !errors.Is(err, fs.ErrNotExist) {
			return fmt.Errorf("read %s: %w", f.Path, err)
		}
		manifest = append(manifest, rec)
		writes = append(writes, f)
	}
	raw, _ := json.MarshalIndent(manifest, "", "  ")
	if err := os.WriteFile(filepath.Join(backup, "manifest.json"), raw, 0o600); err != nil {
		return err
	}
	written := make([]installBackup, 0, len(writes))
	rollback := func(cause error) error {
		keep = true
		var errs []error
		for i := len(written) - 1; i >= 0; i-- {
			rec := written[i]
			full, err := safeTarget(plan.Root, rec.Path)
			if err != nil {
				errs = append(errs, err)
				continue
			}
			if rec.Action == "new" {
				errs = append(errs, os.Remove(full))
				continue
			}
			old, err := os.ReadFile(filepath.Join(originals, filepath.FromSlash(rec.Path)))
			if err != nil {
				errs = append(errs, err)
				continue
			}
			errs = append(errs, writeFileAtomic(full, old, rec.Mode))
		}
		errs = append(errs, fmt.Errorf("install failed and was rolled back: %w (backup: %s)", cause, backup))
		return errors.Join(errs...)
	}
	for i, f := range writes {
		full, err := safeTarget(plan.Root, f.Path)
		if err != nil {
			return rollback(err)
		}
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			return rollback(err)
		}
		mode := manifest[i].Mode
		if err := writeFileAtomic(full, []byte(f.Content), mode); err != nil {
			return rollback(err)
		}
		written = append(written, manifest[i])
	}
	plan.BackupDir = backup
	keep = true
	return nil
}

func writeFileAtomic(path string, data []byte, mode fs.FileMode) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), ".nexadmin-codegen-*.tmp")
	if err != nil {
		return err
	}
	name := tmp.Name()
	defer os.Remove(name)
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	_ = os.Chmod(name, mode)
	if err := os.Rename(name, path); err == nil {
		return nil
	} else if runtime.GOOS != "windows" {
		return err
	}
	if err := os.Remove(path); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	return os.Rename(name, path)
}

func safeTarget(root, rel string) (string, error) {
	rel = filepath.Clean(filepath.FromSlash(rel))
	if rel == "." || filepath.IsAbs(rel) || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("unsafe output path %q", rel)
	}
	full := filepath.Join(root, rel)
	cur := root
	parts := strings.Split(rel, string(filepath.Separator))
	for _, part := range parts {
		cur = filepath.Join(cur, part)
		st, err := os.Lstat(cur)
		if errors.Is(err, fs.ErrNotExist) {
			break
		}
		if err != nil {
			return "", err
		}
		if st.Mode()&os.ModeSymlink != 0 {
			return "", fmt.Errorf("output path contains a symlink: %s", rel)
		}
	}
	return full, nil
}

func moduleRegistration(root, importPath string) (content string, changed bool, note string, err error) {
	file, err := safeTarget(root, "internal/modules/modules.go")
	if err != nil {
		return "", false, "", err
	}
	src, err := os.ReadFile(file)
	if errors.Is(err, fs.ErrNotExist) {
		return "", false, "no_file", nil
	}
	if err != nil {
		return "", false, "", err
	}
	line := fmt.Sprintf(`_ %q`, importPath)
	lines := strings.Split(string(src), "\n")
	begin, end := -1, -1
	for i, l := range lines {
		switch strings.TrimSpace(l) {
		case modulesMarkBegin:
			begin = i
		case modulesMarkEnd:
			end = i
		}
	}
	if begin < 0 || end <= begin {
		return "", false, "no_markers", nil
	}
	specs := make([]string, 0, end-begin)
	for _, l := range lines[begin+1 : end] {
		if t := strings.TrimSpace(l); t != "" {
			if t == line {
				return "", false, "already", nil
			}
			specs = append(specs, t)
		}
	}
	specs = append(specs, line)
	sort.Strings(specs)
	out := make([]string, 0, len(lines))
	out = append(out, lines[:begin+1]...)
	for _, s := range specs {
		out = append(out, "\t"+s)
	}
	out = append(out, lines[end:]...)
	formatted, err := format.Source([]byte(strings.Join(out, "\n")))
	if err != nil {
		return "", false, "", err
	}
	return string(formatted), true, "", nil
}
