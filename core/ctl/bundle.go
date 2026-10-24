package ctl

import (
	"compress/gzip"
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"nexadmin/core/config"
	"nexadmin/core/database"
)

// Bundle contains sequential, independent SQL dumps, not an atomic snapshot.
// Connection credentials and environment variable names never enter its manifest.
type bundleTarget struct {
	ID     string `json:"id"` // platform | tenant:<canonical positive decimal ID>
	Engine string `json:"engine"`
	Host   string `json:"host"`
	Port   int    `json:"port"`
	Name   string `json:"name"`
}

type bundleEntry struct {
	bundleTarget
	File   string `json:"file"`
	Size   int64  `json:"size"`
	SHA256 string `json:"sha256"`
}

type bundleManifest struct {
	Format    int           `json:"format"`
	CreatedAt time.Time     `json:"created_at"`
	Snapshot  string        `json:"snapshot"`
	Entries   []bundleEntry `json:"entries"`
}

type bundleConn struct {
	target bundleTarget
	cfg    config.Database
}

var passwordEnvName = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

func bundleConnections(cfg *config.Config) ([]bundleConn, error) {
	base := cfg.Database
	base.Isolation = config.Isolation{}
	kind, err := database.Normalize(base.Type)
	if err != nil {
		return nil, errors.New("unsupported platform database engine")
	}
	if err := ValidateConn(base); err != nil {
		return nil, err
	}
	if strings.ContainsRune(base.Password, '\x00') {
		return nil, errors.New("invalid platform password")
	}
	target := func(id string, c config.Database) bundleConn {
		return bundleConn{bundleTarget{id, Engine(c), c.Host, c.Port, c.Name}, c}
	}
	result := []bundleConn{target("platform", base)}
	isolation := cfg.Database.Isolation
	if !isolation.Enabled {
		if len(isolation.Tenants) != 0 {
			return nil, errors.New("tenant configuration exists but isolation is disabled; refusing ambiguous scope")
		}
		return result, nil
	}
	if len(isolation.Tenants) == 0 {
		return nil, errors.New("isolation has no configured tenants")
	}
	ids := make([]string, 0, len(isolation.Tenants))
	for id := range isolation.Tenants {
		n, err := strconv.ParseInt(id, 10, 64)
		if err != nil || n <= 0 || strconv.FormatInt(n, 10) != id {
			return nil, errors.New("tenant keys must be canonical positive decimal IDs")
		}
		ids = append(ids, id)
	}
	sort.Strings(ids)
	endpoint := func(c config.Database) string {
		return fmt.Sprintf("%s:%d/%s", strings.ToLower(c.Host), c.Port, strings.ToLower(c.Name))
	}
	seen := map[string]bool{endpoint(base): true}
	for _, id := range ids {
		t := isolation.Tenants[id]
		k, err := database.Normalize(t.Type)
		if err != nil || k != kind || !passwordEnvName.MatchString(t.PasswordEnv) {
			return nil, fmt.Errorf("invalid engine or password environment configuration for tenant %s", id)
		}
		password, ok := os.LookupEnv(t.PasswordEnv)
		if !ok || password == "" || strings.ContainsRune(password, '\x00') {
			return nil, fmt.Errorf("password environment value missing or invalid for tenant %s", id)
		}
		c := config.Database{Type: t.Type, Host: t.Host, Port: t.Port, User: t.User, Password: password, Name: t.Name}
		if ValidateConn(c) != nil {
			return nil, fmt.Errorf("invalid connection configuration for tenant %s", id)
		}
		if seen[endpoint(c)] {
			return nil, errors.New("platform and tenant databases must have distinct endpoints and names")
		}
		seen[endpoint(c)] = true
		result = append(result, target("tenant:"+id, c))
	}
	return result, nil
}

// checkedPath rejects traversal and symlinks in every existing path component.
// Output parents must already exist; no recursive creation or deletion is used.
func checkedPath(path string, newDir bool) (string, error) {
	if path == "" {
		return "", errors.New("path is required")
	}
	for _, part := range strings.FieldsFunc(path, func(r rune) bool { return r == '/' || r == '\\' }) {
		if part == ".." {
			return "", errors.New("parent traversal is not allowed")
		}
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	current := string(filepath.Separator)
	parts := strings.Split(strings.TrimPrefix(abs, current), string(filepath.Separator))
	for i, part := range parts {
		current = filepath.Join(current, part)
		st, err := os.Lstat(current)
		if newDir && i == len(parts)-1 {
			if os.IsNotExist(err) {
				return abs, nil
			}
			if err != nil {
				return "", err
			}
			return "", errors.New("output path already exists")
		}
		if err != nil {
			return "", err
		}
		if st.Mode()&os.ModeSymlink != 0 || !st.IsDir() {
			return "", errors.New("path components must be directories, never symlinks")
		}
	}
	return abs, nil
}

func regularBundleFile(dir, name string) (string, error) {
	if name != "manifest.json" && !SafeBackupName(strings.TrimSuffix(name, ".sha256")) {
		return "", errors.New("invalid bundle filename")
	}
	path := filepath.Join(dir, name)
	st, err := os.Lstat(path)
	if err != nil {
		return "", err
	}
	if !st.Mode().IsRegular() {
		return "", errors.New("bundle members must be regular files, never symlinks")
	}
	return path, nil
}

func bundleTools(conns []bundleConn, restore bool) ([]ToolSpec, []ToolSpec, error) {
	dumps, imports := make([]ToolSpec, len(conns)), make([]ToolSpec, len(conns))
	for i, c := range conns {
		var err error
		dumps[i], err = DumpTool(c.cfg, nil)
		if err != nil {
			return nil, nil, fmt.Errorf("%s: %w", c.target.ID, err)
		}
		if restore {
			imports[i], err = RestoreTool(c.cfg, nil)
			if err != nil {
				return nil, nil, fmt.Errorf("%s: %w", c.target.ID, err)
			}
		}
	}
	return dumps, imports, nil
}

func bundleServiceStopped(svc StateReader) bool {
	st := svc.State()
	// A tracked process may still be alive while its HTTP endpoint is unavailable.
	// Refuse even a stale PID file: the operator must resolve it with stop first.
	return !st.Running && st.Pid == nil
}

func writeBundle(ctx context.Context, dir string, conns []bundleConn, tools []ToolSpec, out io.Writer) error {
	m := bundleManifest{Format: 1, CreatedAt: time.Now().UTC(), Snapshot: "sequential-not-atomic"}
	for i, c := range conns {
		// Generated names do not contain configuration values.
		name := "platform.sql.gz"
		if c.target.ID != "platform" {
			name = "tenant-" + strings.TrimPrefix(c.target.ID, "tenant:") + ".sql.gz"
		}
		res, err := DumpGzip(ctx, tools[i], filepath.Join(dir, name))
		if err != nil {
			// Do not expose external tool output: it may include credentials.
			return fmt.Errorf("backup failed for %s; incomplete bundle retained at %s", c.target.ID, dir)
		}
		m.Entries = append(m.Entries, bundleEntry{c.target, name, res.Size, res.SHA256})
		fmt.Fprintf(out, "Backed up %s\n", c.target.ID)
	}
	b, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	f, err := os.OpenFile(filepath.Join(dir, "manifest.json"), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	_, err = f.Write(append(b, '\n'))
	if err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err != nil {
		return err
	}
	return closeErr
}

// BackupBundle exports the platform and every configured tenant into a new
// private directory. No database connections are opened by configuration lookup.
func BackupBundle(ctx context.Context, cfg *config.Config, output string, out io.Writer) error {
	conns, err := bundleConnections(cfg)
	if err != nil {
		return err
	}
	dir, err := checkedPath(output, true)
	if err != nil {
		return err
	}
	dumps, _, err := bundleTools(conns, false)
	if err != nil {
		return err
	}
	if err := os.Mkdir(dir, 0o700); err != nil {
		return err
	}
	fmt.Fprintln(out, "Exporting independent databases sequentially; this is NOT an atomic snapshot.")
	if err := writeBundle(ctx, dir, conns, dumps, out); err != nil {
		return err
	}
	fmt.Fprintf(out, "Complete bundle: %s\n", dir)
	return nil
}

func validateBundle(ctx context.Context, dir string, conns []bundleConn) ([]bundleEntry, error) {
	path, err := regularBundleFile(dir, "manifest.json")
	if err != nil {
		return nil, err
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil || st.Size() > 4<<20 {
		return nil, errors.New("bundle manifest exceeds size limit")
	}
	dec := json.NewDecoder(io.LimitReader(f, 4<<20))
	dec.DisallowUnknownFields()
	var m bundleManifest
	if err := dec.Decode(&m); err != nil {
		return nil, errors.New("invalid bundle manifest")
	}
	var extra any
	if err := dec.Decode(&extra); err != io.EOF {
		return nil, errors.New("trailing or oversized bundle manifest")
	}
	if m.Format != 1 || m.Snapshot != "sequential-not-atomic" || len(m.Entries) != len(conns) {
		return nil, errors.New("bundle format or configured database membership mismatch")
	}
	byID := make(map[string]bundleEntry, len(m.Entries))
	files := make(map[string]bool, len(m.Entries))
	for _, e := range m.Entries {
		if _, ok := byID[e.ID]; ok || files[e.File] || !SafeBackupName(e.File) {
			return nil, errors.New("duplicate or invalid bundle member")
		}
		byID[e.ID], files[e.File] = e, true
	}
	entries := make([]bundleEntry, len(conns))
	for i, c := range conns {
		e, ok := byID[c.target.ID]
		if !ok || e.bundleTarget != c.target {
			return nil, fmt.Errorf("bundle engine or target mismatch for %s", c.target.ID)
		}
		sum, err := hex.DecodeString(e.SHA256)
		if err != nil || len(sum) != 32 || e.Size <= 0 {
			return nil, errors.New("invalid bundle checksum or size")
		}
		path, err := regularBundleFile(dir, e.File)
		if err != nil {
			return nil, err
		}
		sidecarPath, err := regularBundleFile(dir, e.File+".sha256")
		if err != nil {
			return nil, err
		}
		sidecarStat, err := os.Stat(sidecarPath)
		if err != nil || sidecarStat.Size() > 1024 {
			return nil, errors.New("bundle sidecar exceeds size limit")
		}
		st, err := os.Stat(path)
		if err != nil || st.Size() != e.Size {
			return nil, fmt.Errorf("bundle size mismatch for %s", c.target.ID)
		}
		sidecar, err := ReadSidecar(path)
		if err != nil || sidecar != strings.ToLower(e.SHA256) {
			return nil, fmt.Errorf("bundle sidecar mismatch for %s", c.target.ID)
		}
		if err := VerifySHA256(path, e.SHA256); err != nil {
			return nil, fmt.Errorf("bundle checksum mismatch for %s", c.target.ID)
		}
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		// Read the whole gzip before touching any database, including its trailer.
		f, err := os.Open(path)
		if err != nil {
			return nil, err
		}
		gz, err := gzip.NewReader(f)
		if err != nil {
			f.Close()
			return nil, errors.New("invalid bundle gzip")
		}
		n, readErr := io.Copy(io.Discard, gz)
		gz.Close()
		f.Close()
		if readErr != nil || n == 0 {
			return nil, errors.New("corrupt or empty bundle SQL stream")
		}
		entries[i] = e
	}
	return entries, nil
}

// RestoreBundleOptions requires explicit noninteractive consent for writes.
// Tenant selects exactly one tenant from a complete bundle; empty means all.
type RestoreBundleOptions struct {
	Dir    string
	Tenant string
	DryRun bool
	Yes    bool
}

func RestoreBundle(ctx context.Context, cfg *config.Config, svc StateReader, o RestoreBundleOptions, out io.Writer) error {
	if o.DryRun == o.Yes {
		return errors.New("choose exactly one of --dry-run or --yes")
	}
	conns, err := bundleConnections(cfg)
	if err != nil {
		return err
	}
	dir, err := checkedPath(o.Dir, false)
	if err != nil {
		return err
	}
	entries, err := validateBundle(ctx, dir, conns)
	if err != nil {
		return err
	}
	selected := make([]bool, len(conns))
	found := o.Tenant == ""
	for i, c := range conns {
		selected[i] = o.Tenant == "" || c.target.ID == "tenant:"+o.Tenant
		if selected[i] {
			found = true
			fmt.Fprintf(out, "Restore target: %s (%s %s:%d/%s)\n", c.target.ID, c.target.Engine, c.target.Host, c.target.Port, c.target.Name)
		}
	}
	if !found {
		return errors.New("--tenant must be a configured canonical tenant ID")
	}
	dumps, imports, err := bundleTools(conns, true)
	if err != nil {
		return err
	}
	fmt.Fprintln(out, "All bundle checksums, gzip streams, engines and configured targets validated. Restore is sequential and may partially fail; SQL absent from the dump is not removed.")
	if !bundleServiceStopped(svc) {
		return errors.New("service is running or has a PID file; stop it before bundle restore (including dry-run)")
	}
	if o.DryRun {
		fmt.Fprintln(out, "Dry run: no database commands executed and no files created.")
		return nil
	}
	if !o.Yes {
		return errors.New("restore-bundle requires --yes or --dry-run")
	}
	parent, err := checkedPath(BackupDir(cfg), false)
	if err != nil {
		return fmt.Errorf("recovery backup parent must already exist and contain no symlinks: %w", err)
	}
	recovery, err := os.MkdirTemp(parent, "pre-restore-bundle-")
	if err != nil {
		return err
	}
	// Back up the full configured set even for a single tenant restore, so the
	// recovery directory itself remains a complete, independently usable bundle.
	fmt.Fprintf(out, "Recovery bundle: %s\n", recovery)
	if err := writeBundle(ctx, recovery, conns, dumps, out); err != nil {
		return fmt.Errorf("prebackup failed; no restore attempted: %w", err)
	}
	if _, err := validateBundle(ctx, recovery, conns); err != nil {
		return fmt.Errorf("prebackup validation failed; no restore attempted: %w", err)
	}
	// Recheck inputs and service state after potentially long prebackups.
	if _, err := checkedPath(dir, false); err != nil {
		return err
	}
	entries, err = validateBundle(ctx, dir, conns)
	if err != nil {
		return err
	}
	completed := []string{}
	for i, c := range conns {
		if !selected[i] {
			continue
		}
		if !bundleServiceStopped(svc) {
			return fmt.Errorf("service is no longer stopped; completed=%v; recovery=%s; keep service stopped; no automatic rollback", completed, recovery)
		}
		if err := RestoreGzip(ctx, imports[i], filepath.Join(dir, entries[i].File)); err != nil {
			return fmt.Errorf("PARTIAL RESTORE: completed=%v; %s may be partially restored; later targets were not attempted; recovery bundle=%s; keep service stopped; no automatic rollback", completed, c.target.ID, recovery)
		}
		completed = append(completed, c.target.ID)
	}
	fmt.Fprintf(out, "Restored %v. Recovery bundle: %s. Service remains stopped.\n", completed, recovery)
	return nil
}

// legacyPlatformConfig makes platform-only intent explicit before opening a DB.
func legacyPlatformConfig(cfg *config.Config, platformOnly []bool) (*config.Config, error) {
	if !cfg.Database.Isolation.Enabled {
		return cfg, nil
	}
	if len(platformOnly) != 1 || !platformOnly[0] {
		return nil, errors.New("isolation enabled: use restore-bundle, or explicitly acknowledge legacy platform-only restore/reinit with --platform-only")
	}
	copy := *cfg
	copy.Database.Isolation = config.Isolation{}
	return &copy, nil
}
