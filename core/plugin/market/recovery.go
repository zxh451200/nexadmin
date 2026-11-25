package market

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"sort"
	"strings"
	"time"
)

const (
	recoveryPrepared   = "prepared"
	recoveryActivating = "activating"
	recoveryReady      = "ready"
	recoveryRestored   = "restored"
	recoveryRequired   = "recovery_required"
	recoverySuperseded = "superseded"
)

type recoveryJournal struct {
	ID        string    `json:"id"`
	Phase     string    `json:"phase"`
	Root      string    `json:"root"`
	Exe       string    `json:"exe"`
	OldExe    string    `json:"oldExe"`
	Targets   []string  `json:"targets"`
	Absent    []string  `json:"absent,omitempty"`
	OldSHA    string    `json:"oldSha"`
	NewSHA    string    `json:"newSha,omitempty"`
	BackupSHA string    `json:"backupSha"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
	Error     string    `json:"error,omitempty"`
}

type recoveryTransaction struct {
	root    string
	dir     string
	journal recoveryJournal
}

func currentExecutable() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	return canonicalExecutable(exe)
}

func canonicalExecutable(path string) (string, error) {
	exe, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	if resolved, err := filepath.EvalSymlinks(exe); err == nil {
		exe = resolved
	}
	return filepath.Abs(exe)
}

func sameExecutablePath(a, b string) bool {
	a, b = filepath.Clean(a), filepath.Clean(b)
	if a == b {
		return true
	}
	return runtime.GOOS == "windows" && strings.EqualFold(a, b)
}

func recoveryBase(root string) (string, error) {
	if err := checkSourcePath(root, "data/plugins/recovery"); err != nil {
		return "", err
	}
	base, err := filepath.Abs(filepath.Join(root, "data", "plugins", "recovery"))
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(base, 0o700); err != nil {
		return "", err
	}
	return base, nil
}

func beginRecovery(root string, paths []string) (*recoveryTransaction, error) {
	if err := CheckRecovery(root); err != nil {
		return nil, err
	}
	root, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	exe, err := currentExecutable()
	if err != nil {
		return nil, err
	}
	base, err := recoveryBase(root)
	if err != nil {
		return nil, err
	}
	workDir, err := os.MkdirTemp(filepath.Dir(base), ".recovery-")
	if err != nil {
		return nil, err
	}
	name := "job-" + strings.TrimPrefix(filepath.Base(workDir), ".recovery-")
	dir := filepath.Join(base, name)
	t := &recoveryTransaction{root: root, dir: workDir}
	cleanup := true
	defer func() {
		if cleanup {
			_ = os.RemoveAll(workDir)
		}
	}()
	marker := filepath.Join(workDir, ".initializing")
	if err := os.WriteFile(marker, []byte(name), 0o600); err != nil {
		return nil, err
	}
	var absent []string
	for _, rel := range paths {
		if err := checkSourcePath(root, rel); err != nil {
			return nil, err
		}
		src := filepath.Join(root, filepath.FromSlash(rel))
		if _, err := os.Lstat(src); errors.Is(err, os.ErrNotExist) {
			absent = append(absent, rel)
			continue
		} else if err != nil {
			return nil, err
		}
		if err := copySource(src, filepath.Join(workDir, "source", filepath.FromSlash(rel))); err != nil {
			return nil, fmt.Errorf("备份 %s 失败: %w", rel, err)
		}
	}
	oldSHA, err := fileSHA(exe)
	if err != nil {
		return nil, err
	}
	backup := filepath.Join(workDir, "executable.backup")
	if err := copySource(exe, backup); err != nil {
		return nil, fmt.Errorf("备份当前二进制失败: %w", err)
	}
	backupSHA, err := fileSHA(backup)
	if err != nil {
		return nil, err
	}
	if backupSHA != oldSHA {
		return nil, errors.New("二进制备份校验失败")
	}
	now := time.Now().UTC()
	t.journal = recoveryJournal{
		ID: name, Phase: recoveryPrepared, Root: root, Exe: exe, OldExe: exe + "." + name + ".old",
		Targets: append([]string(nil), paths...), Absent: absent, OldSHA: oldSHA, BackupSHA: backupSHA,
		CreatedAt: now, UpdatedAt: now,
	}
	if err := t.save(); err != nil {
		return nil, err
	}
	if err := os.Rename(workDir, dir); err != nil {
		return nil, err
	}
	if err := syncRecoveryDir(base); err != nil {
		return nil, err
	}
	t.dir = dir
	_ = os.Remove(filepath.Join(dir, ".initializing"))
	cleanup = false
	return t, nil
}

func (t *recoveryTransaction) save() error {
	t.journal.UpdatedAt = time.Now().UTC()
	raw, err := json.MarshalIndent(t.journal, "", "  ")
	if err != nil {
		return err
	}
	path := filepath.Join(t.dir, "manifest.json")
	tmp, err := os.CreateTemp(t.dir, ".manifest-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		return err
	}
	if _, err := tmp.Write(append(raw, '\n')); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmp.Name(), path); err != nil {
		return err
	}
	return syncRecoveryDir(t.dir)
}

func syncRecoveryDir(dir string) error {
	if runtime.GOOS == "windows" {
		return nil
	}
	d, err := os.Open(dir)
	if err != nil {
		return err
	}
	defer d.Close()
	return d.Sync()
}

func (t *recoveryTransaction) event(phase string, err error) {
	line := struct {
		Time  time.Time `json:"time"`
		Phase string    `json:"phase"`
		Error string    `json:"error,omitempty"`
	}{Time: time.Now().UTC(), Phase: phase}
	if err != nil {
		line.Error = err.Error()
	}
	raw, _ := json.Marshal(line)
	f, e := os.OpenFile(filepath.Join(t.dir, "events.jsonl"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if e == nil {
		_, _ = f.Write(append(raw, '\n'))
		_ = f.Sync()
		_ = f.Close()
	}
}

func (t *recoveryTransaction) fail(err error) error {
	t.journal.Phase = recoveryRequired
	t.journal.Error = err.Error()
	if saveErr := t.save(); saveErr != nil {
		err = errors.Join(err, fmt.Errorf("写入恢复失败状态: %w", saveErr))
	}
	t.event(recoveryRequired, err)
	return fmt.Errorf("%w；自动恢复未完成，现场保留在 %s（recovery=%s）", err, t.journalPath(), t.journal.ID)
}

func (t *recoveryTransaction) journalPath() string {
	return filepath.Join(t.dir, "manifest.json")
}

func (t *recoveryTransaction) restore(cause error) error {
	if err := t.rollback(cause); err != nil {
		return err
	}
	return fmt.Errorf("%w；源码、依赖、前端产物、台账与二进制已恢复（recovery=%s）", cause, t.journal.ID)
}

func (t *recoveryTransaction) rollback(cause error) error {
	var restoreErr error
	sum, hashErr := fileSHA(t.journal.Exe)
	if hashErr != nil || sum != t.journal.OldSHA {
		err := restoreExecutable(t.journal.Exe, t.journal.OldExe, t.journal.OldSHA)
		if err != nil {
			backupErr := restoreExecutable(t.journal.Exe, filepath.Join(t.dir, "executable.backup"), t.journal.OldSHA)
			if backupErr != nil {
				restoreErr = errors.Join(restoreErr, fmt.Errorf("恢复二进制: %w", errors.Join(err, backupErr)))
			}
		}
	}
	for i := len(t.journal.Targets) - 1; i >= 0; i-- {
		rel := t.journal.Targets[i]
		if err := checkSourcePath(t.root, rel); err != nil {
			restoreErr = errors.Join(restoreErr, err)
			continue
		}
		dst := filepath.Join(t.root, filepath.FromSlash(rel))
		src := filepath.Join(t.dir, "source", filepath.FromSlash(rel))
		if _, err := os.Lstat(src); errors.Is(err, os.ErrNotExist) {
			if !slices.Contains(t.journal.Absent, rel) {
				restoreErr = errors.Join(restoreErr, fmt.Errorf("恢复备份缺失，拒绝删除当前目标: %s", rel))
				continue
			}
			if err := os.RemoveAll(dst); err != nil {
				restoreErr = errors.Join(restoreErr, err)
			}
			continue
		} else if err != nil {
			restoreErr = errors.Join(restoreErr, err)
			continue
		}
		if err := os.RemoveAll(dst); err != nil {
			restoreErr = errors.Join(restoreErr, err)
			continue
		}
		restoreErr = errors.Join(restoreErr, copySource(src, dst))
	}
	if restoreErr != nil {
		return t.fail(errors.Join(cause, fmt.Errorf("恢复失败: %w", restoreErr)))
	}
	t.journal.Phase = recoveryRestored
	t.journal.Error = cause.Error()
	if err := t.save(); err != nil {
		return t.fail(errors.Join(cause, err))
	}
	t.event(recoveryRestored, cause)
	return nil
}

func (t *recoveryTransaction) commit() error {
	j, err := readRecoveryJournal(t.dir)
	if err != nil {
		return fmt.Errorf("操作已完成，但读取恢复记录失败（recovery=%s，现场保留在 %s）: %w", t.journal.ID, t.journalPath(), err)
	}
	t.journal = j
	t.journal.Phase = recoveryReady
	t.journal.Error = ""
	if err := t.save(); err != nil {
		return fmt.Errorf("操作已完成，但写入恢复完成状态失败（recovery=%s，现场保留在 %s）: %w", t.journal.ID, t.journalPath(), err)
	}
	t.event(recoveryReady, nil)
	if err := t.cleanupReady(); err != nil {
		t.event(recoveryReady, fmt.Errorf("清理历史恢复记录失败: %w", err))
	}
	return nil
}

func (t *recoveryTransaction) cleanupReady() error {
	entries, err := os.ReadDir(filepath.Join(t.root, "data", "plugins", "recovery"))
	if err != nil {
		return err
	}
	type readyRecovery struct {
		name string
		j    recoveryJournal
	}
	var ready []readyRecovery
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		j, err := readRecoveryJournal(filepath.Join(t.root, "data", "plugins", "recovery", entry.Name()))
		if err != nil || (j.Phase != recoveryReady && j.Phase != recoveryRestored && j.Phase != recoverySuperseded) || time.Since(j.CreatedAt) < 7*24*time.Hour {
			continue
		}
		ready = append(ready, readyRecovery{name: entry.Name(), j: j})
	}
	sort.Slice(ready, func(i, j int) bool {
		if ready[i].j.CreatedAt.Equal(ready[j].j.CreatedAt) {
			return ready[i].name < ready[j].name
		}
		return ready[i].j.CreatedAt.Before(ready[j].j.CreatedAt)
	})
	for len(ready) > 2 {
		if err := os.Remove(ready[0].j.OldExe); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		if err := os.RemoveAll(filepath.Join(t.root, "data", "plugins", "recovery", ready[0].name)); err != nil {
			return err
		}
		ready = ready[1:]
	}
	return nil
}

func loadRecovery(root string) (*recoveryTransaction, error) {
	base, err := recoveryBase(root)
	if err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(base)
	if err != nil {
		return nil, err
	}
	var active []recoveryTransaction
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		dir := filepath.Join(base, entry.Name())
		if _, err := os.Lstat(filepath.Join(dir, "manifest.json")); errors.Is(err, os.ErrNotExist) {
			if _, markerErr := os.Lstat(filepath.Join(dir, ".initializing")); markerErr == nil {
				continue
			}
		}
		j, err := readRecoveryJournal(dir)
		if err != nil {
			return nil, err
		}
		if j.Phase == recoveryPrepared || j.Phase == recoveryActivating {
			root, err := filepath.Abs(root)
			if err != nil {
				return nil, err
			}
			if filepath.Clean(j.Root) != root {
				return nil, fmt.Errorf("恢复记录 %s 不属于当前项目根目录", j.ID)
			}
			active = append(active, recoveryTransaction{root: j.Root, dir: dir, journal: j})
		}
	}
	if len(active) == 0 {
		return nil, errors.New("没有进行中的插件恢复事务")
	}
	if len(active) > 1 {
		return nil, errors.New("存在多个进行中的插件恢复事务，拒绝替换二进制")
	}
	return &active[0], nil
}

func readRecoveryJournal(dir string) (recoveryJournal, error) {
	var j recoveryJournal
	path := filepath.Join(dir, "manifest.json")
	raw, err := os.ReadFile(path)
	if err != nil {
		return j, err
	}
	if err := json.Unmarshal(raw, &j); err != nil {
		return j, fmt.Errorf("恢复记录损坏 %s: %w", path, err)
	}
	if err := validateRecoveryJournal(j, dir); err != nil {
		return j, fmt.Errorf("恢复记录无效 %s: %w", path, err)
	}
	return j, nil
}

func validateRecoveryJournal(j recoveryJournal, dir string) error {
	if j.ID == "" || j.ID != filepath.Base(dir) {
		return errors.New("事务 ID 与目录不一致")
	}
	switch j.Phase {
	case recoveryPrepared, recoveryActivating, recoveryReady, recoveryRestored, recoveryRequired, recoverySuperseded:
	default:
		return fmt.Errorf("未知阶段 %q", j.Phase)
	}
	if !filepath.IsAbs(j.Root) || !filepath.IsAbs(j.Exe) || !filepath.IsAbs(j.OldExe) {
		return errors.New("根目录与二进制路径必须是绝对路径")
	}
	if j.OldExe != j.Exe+"."+j.ID+".old" {
		return errors.New("旧二进制路径与事务不一致")
	}
	if !validSHA(j.OldSHA) || !validSHA(j.BackupSHA) || (j.NewSHA != "" && !validSHA(j.NewSHA)) {
		return errors.New("二进制校验值无效")
	}
	seen := make(map[string]bool, len(j.Targets))
	for _, rel := range j.Targets {
		if !validRecoveryPath(rel) || seen[rel] {
			return fmt.Errorf("非法或重复的恢复路径 %q", rel)
		}
		seen[rel] = true
	}
	for _, rel := range j.Absent {
		if !seen[rel] {
			return fmt.Errorf("原先不存在的恢复路径不在目标清单中: %q", rel)
		}
	}
	return nil
}

func validSHA(sum string) bool {
	if len(sum) != sha256.Size*2 {
		return false
	}
	_, err := hex.DecodeString(sum)
	return err == nil
}

func recoveryExecutablePaths(root string) (string, string, string, error) {
	t, err := loadRecovery(root)
	if err != nil {
		return "", "", "", err
	}
	candidate := t.journal.Exe + "." + t.journal.ID + ".new"
	return candidate, t.journal.Exe, t.journal.OldExe, nil
}

// RecoveryInfo 是运维查询用的单条恢复记录。
type RecoveryInfo struct {
	ID        string    `json:"id"`
	Phase     string    `json:"phase"`
	Manifest  string    `json:"manifest"`
	CreatedAt time.Time `json:"createdAt"`
	Error     string    `json:"error,omitempty"`
}

// RecoveryList 返回按时间排序的持久恢复记录；目录不存在时返回空列表。
func RecoveryList(root string) ([]RecoveryInfo, error) {
	root, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	base := filepath.Join(root, "data", "plugins", "recovery")
	entries, err := os.ReadDir(base)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var out []RecoveryInfo
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		dir := filepath.Join(base, entry.Name())
		if _, err := os.Lstat(filepath.Join(dir, "manifest.json")); errors.Is(err, os.ErrNotExist) {
			if _, markerErr := os.Lstat(filepath.Join(dir, ".initializing")); markerErr == nil {
				continue
			}
		}
		j, err := readRecoveryJournal(dir)
		if err != nil {
			return nil, err
		}
		if filepath.Clean(j.Root) != root {
			return nil, fmt.Errorf("恢复记录 %s 不属于当前项目根目录", j.ID)
		}
		out = append(out, RecoveryInfo{ID: j.ID, Phase: j.Phase, Manifest: filepath.Join(dir, "manifest.json"), CreatedAt: j.CreatedAt, Error: j.Error})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].CreatedAt.Equal(out[j].CreatedAt) {
			return out[i].ID < out[j].ID
		}
		return out[i].CreatedAt.Before(out[j].CreatedAt)
	})
	return out, nil
}

// RollbackRecovery 在服务停止后显式回退最近一笔未解决事务或成功事务。
func RollbackRecovery(root string, confirmed bool) error {
	if !confirmed {
		return errors.New("回退恢复记录需要 --yes 明确确认")
	}
	items, err := RecoveryList(root)
	if err != nil {
		return err
	}
	var selected *RecoveryInfo
	for i := range items {
		phase := items[i].Phase
		if phase == recoveryPrepared || phase == recoveryActivating || phase == recoveryRequired {
			if selected != nil {
				return errors.New("存在多个未解决的恢复事务，拒绝自动选择回退目标")
			}
			selected = &items[i]
		}
	}
	if selected == nil {
		for i := len(items) - 1; i >= 0; i-- {
			if items[i].Phase == recoveryReady {
				selected = &items[i]
				break
			}
		}
	}
	if selected == nil {
		return errors.New("没有可回退的恢复事务")
	}
	t, err := loadRecoveryJournal(root, selected.ID)
	if err != nil {
		return err
	}
	return t.rollback(errors.New("运维人员显式回退插件变更"))
}

// SupersedeRecovery 接受当前源码与二进制并放弃最近完成事务的回退点，用于事务外升级后解除哈希门禁。
func SupersedeRecovery(root string, confirmed bool) error {
	if !confirmed {
		return errors.New("接受当前现场需要 --yes 明确确认")
	}
	items, err := RecoveryList(root)
	if err != nil {
		return err
	}
	for _, item := range items {
		if item.Phase == recoveryPrepared || item.Phase == recoveryActivating || item.Phase == recoveryRequired {
			return errors.New("存在未解决的恢复事务，必须先回退，不能接受当前现场")
		}
	}
	var selected []*recoveryTransaction
	for i := len(items) - 1; i >= 0; i-- {
		if items[i].Phase == recoveryReady || items[i].Phase == recoveryRestored {
			t, err := loadRecoveryJournal(root, items[i].ID)
			if err != nil {
				return err
			}
			selected = append(selected, t)
		}
	}
	if len(selected) == 0 {
		return errors.New("没有可接受的已完成恢复事务")
	}
	sum, err := fileSHA(selected[0].journal.Exe)
	if err != nil {
		return err
	}
	for i := len(selected) - 1; i >= 0; i-- {
		t := selected[i]
		t.journal.NewSHA = sum
		t.journal.Phase = recoverySuperseded
		t.journal.Error = "运维人员明确接受当前源码与二进制，放弃此恢复点"
		if err := t.save(); err != nil {
			return err
		}
		t.event(recoverySuperseded, nil)
	}
	return nil
}

func loadRecoveryJournal(root, id string) (*recoveryTransaction, error) {
	if !validRecoveryPath(id) {
		return nil, fmt.Errorf("非法的恢复事务 ID %q", id)
	}
	root, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	dir := filepath.Join(root, "data", "plugins", "recovery", id)
	j, err := readRecoveryJournal(dir)
	if err != nil {
		return nil, err
	}
	if filepath.Clean(j.Root) != root {
		return nil, errors.New("恢复记录不属于当前项目根目录")
	}
	return &recoveryTransaction{root: root, dir: dir, journal: j}, nil
}

// CheckRecovery 在 CLI 预检或新变更前拒绝未解决记录，并核对最近一笔完成记录对应的 canonical 二进制。
func CheckRecovery(root string) error {
	return checkRecovery(root, "", false)
}

// CheckRecoveryForExecutable 额外要求当前运行程序与恢复记录中的二进制路径一致。
func CheckRecoveryForExecutable(root, exe string) error {
	return checkRecovery(root, exe, true)
}

func checkRecovery(root, exe string, checkExecutable bool) error {
	items, err := RecoveryList(root)
	if err != nil {
		return err
	}
	var problems []error
	var latest *RecoveryInfo
	for i := range items {
		switch items[i].Phase {
		case recoveryPrepared, recoveryActivating, recoveryRequired:
			problems = append(problems, fmt.Errorf("插件恢复尚未完成（recovery=%s，phase=%s，manifest=%s）", items[i].ID, items[i].Phase, items[i].Manifest))
		default:
			latest = &items[i]
		}
	}
	if latest != nil && latest.Phase != recoverySuperseded {
		t, err := loadRecoveryJournal(root, latest.ID)
		if err != nil {
			problems = append(problems, err)
		} else if t.journal.NewSHA != "" {
			want := t.journal.NewSHA
			if latest.Phase == recoveryRestored {
				want = t.journal.OldSHA
			}
			gotPath := t.journal.Exe
			identityOK := true
			if checkExecutable {
				current, err := canonicalExecutable(exe)
				if err != nil {
					problems = append(problems, fmt.Errorf("解析当前运行程序失败（recovery=%s）: %w", latest.ID, err))
					identityOK = false
				} else if !sameExecutablePath(current, t.journal.Exe) {
					problems = append(problems, fmt.Errorf("当前运行程序与最近恢复记录的二进制路径不一致（recovery=%s，phase=%s，manifest=%s）", latest.ID, latest.Phase, latest.Manifest))
					identityOK = false
				} else {
					gotPath = current
				}
			}
			if identityOK {
				got, err := fileSHA(gotPath)
				if err != nil || got != want {
					problems = append(problems, fmt.Errorf("当前二进制与最近恢复记录不一致（recovery=%s，phase=%s，manifest=%s）", latest.ID, latest.Phase, latest.Manifest))
				}
			}
		}
	}
	return errors.Join(problems...)
}

// ActivateRecovery 校验候选与旧二进制后切换；失败时尽量恢复旧 exe，源码由外层事务恢复。
func ActivateRecovery(root, exe, candidate, oldExe string, log func(string)) error {
	t, err := loadRecovery(root)
	if err != nil {
		return err
	}
	if filepath.Clean(exe) != filepath.Clean(t.journal.Exe) || filepath.Clean(oldExe) != filepath.Clean(t.journal.OldExe) {
		return errors.New("二进制路径与恢复记录不一致")
	}
	if filepath.Clean(candidate) != filepath.Clean(t.journal.Exe+"."+t.journal.ID+".new") {
		return errors.New("候选二进制路径与恢复记录不一致")
	}
	oldSHA, err := fileSHA(exe)
	if err != nil {
		return err
	}
	if oldSHA != t.journal.OldSHA {
		return errors.New("当前二进制与恢复记录中的旧版本不一致")
	}
	newSHA, err := fileSHA(candidate)
	if err != nil {
		return err
	}
	t.journal.NewSHA = newSHA
	t.journal.Phase = recoveryActivating
	if err := t.save(); err != nil {
		return err
	}
	t.event(recoveryActivating, nil)
	if err := os.Remove(oldExe); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if err := os.Rename(exe, oldExe); err != nil {
		return fmt.Errorf("旧二进制让位失败: %w", err)
	}
	if err := os.Rename(candidate, exe); err != nil {
		if rb := os.Rename(oldExe, exe); rb != nil {
			return t.fail(fmt.Errorf("候选切换失败且旧二进制恢复失败: %w", errors.Join(err, rb)))
		}
		return fmt.Errorf("候选切换失败，旧二进制已恢复: %w", err)
	}
	got, err := fileSHA(exe)
	if err != nil || got != newSHA {
		verifyErr := err
		if verifyErr == nil {
			verifyErr = errors.New("checksum mismatch")
		}
		if restoreErr := restoreExecutable(exe, oldExe, t.journal.OldSHA); restoreErr != nil {
			return t.fail(fmt.Errorf("新二进制校验失败且回滚失败: %w", errors.Join(verifyErr, restoreErr)))
		}
		return fmt.Errorf("新二进制校验失败，旧二进制已恢复: %w", verifyErr)
	}
	log(fmt.Sprintf("二进制切换已记录（recovery=%s，manifest=%s）", t.journal.ID, t.journalPath()))
	return nil
}

func restoreExecutable(exe, oldExe, want string) error {
	if got, err := fileSHA(exe); err == nil && got == want {
		return nil
	}
	if _, err := os.Stat(oldExe); err != nil {
		return err
	}
	if got, err := fileSHA(oldExe); err != nil || got != want {
		if err == nil {
			err = errors.New("old executable checksum mismatch")
		}
		return err
	}
	if err := os.Remove(exe); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return os.Rename(oldExe, exe)
}

func fileSHA(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func validRecoveryPath(rel string) bool {
	clean := filepath.Clean(filepath.FromSlash(rel))
	return clean != "." && !filepath.IsAbs(clean) && clean != ".." && !strings.HasPrefix(clean, ".."+string(filepath.Separator))
}
