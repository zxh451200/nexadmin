package market

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"maps"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"nexadmin/core/plugin"
)

// LedgerEntry 是本机安装台账里的一条记录：只记录「用户装了什么」。
// 随二进制编译进来的内置插件不进台账，后台「插件管理」据此把已安装与内置分开列。
type LedgerEntry struct {
	ID          string    `json:"id"`
	Version     string    `json:"version"`
	InstalledAt time.Time `json:"installedAt"`
	// Fingerprints 保存安装后 backend/、web/ 下所有普通文件的 SHA-256；nil 表示旧台账无基线。
	Fingerprints map[string]string `json:"fingerprints,omitempty"`
}

type ledgerFile struct {
	Plugins []LedgerEntry `json:"plugins"`
}

// LedgerPath 是安装台账路径（挂在 data/ 下，随 data/* 不入库，换机器不跟随）。
func LedgerPath(root string) string {
	return filepath.Join(root, "data", "plugins", "installed.json")
}

// 台账写入串行化：同进程内市场任务与 CLI 可能并发；跨进程按最后写入者为准。
var ledgerMu sync.Mutex

// LoadLedger 读取台账。文件不存在返回空（全新项目就是这种情况），格式损坏才报错。
func LoadLedger(root string) ([]LedgerEntry, error) {
	if err := checkSourcePath(root, "data/plugins/installed.json"); err != nil {
		return nil, err
	}
	raw, err := os.ReadFile(LedgerPath(root))
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var f *ledgerFile
	if err := json.Unmarshal(raw, &f); err != nil {
		return nil, fmt.Errorf("安装台账 %s 解析失败: %w", LedgerPath(root), err)
	}
	// null、缺少 plugins 字段、重复 ID 或损坏的指纹都不能被当作空台账覆盖。
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil || f == nil || fields["plugins"] == nil {
		return nil, fmt.Errorf("安装台账 %s 格式损坏", LedgerPath(root))
	}
	seen := make(map[string]bool)
	for _, entry := range f.Plugins {
		if !plugin.ValidID(entry.ID) || seen[entry.ID] {
			return nil, fmt.Errorf("安装台账 %s 包含非法或重复 id %q", LedgerPath(root), entry.ID)
		}
		seen[entry.ID] = true
		if entry.Fingerprints != nil {
			if _, ok := entry.Fingerprints["backend/"+plugin.ManifestFile]; !ok {
				return nil, fmt.Errorf("安装台账 %s 的 %s 指纹缺少清单", LedgerPath(root), entry.ID)
			}
			for name, sum := range entry.Fingerprints {
				decoded, err := hex.DecodeString(sum)
				if err != nil || len(decoded) != sha256.Size || strings.Contains(name, "\\") || path.Clean(name) != name ||
					(!strings.HasPrefix(name, "backend/") && !strings.HasPrefix(name, "web/")) {
					return nil, fmt.Errorf("安装台账 %s 的 %s 指纹损坏", LedgerPath(root), entry.ID)
				}
			}
		}
	}
	return f.Plugins, nil
}

// AddLedger 记录一次安装；指纹取安装后的源码（包括已改写的 import 和 plugin.yaml）。
// 无清单时仍可登记安装，但不凭空建立基线。
func AddLedger(root, id, version string) error {
	fingerprints, _, err := sourceFingerprints(root, id)
	if err != nil {
		return err
	}
	if _, ok := fingerprints["backend/"+plugin.ManifestFile]; !ok {
		fingerprints = nil
	}
	return updateLedger(root, func(entries []LedgerEntry) []LedgerEntry {
		entry := LedgerEntry{ID: id, Version: version, InstalledAt: time.Now().UTC(), Fingerprints: fingerprints}
		for i := range entries {
			if entries[i].ID == id {
				entries[i] = entry
				return entries
			}
		}
		return append(entries, entry)
	})
}

// UpdateStatus 区分正常更新、本地改动和没有可信安装基线的旧插件。
type UpdateStatus struct {
	Exists        bool `json:"exists"`
	Modified      bool `json:"modified"`
	BaselineKnown bool `json:"baselineKnown"`
}

// InspectUpdate 只读比对源码；文件增加、删除、内容改变均算修改，符号链接一律拒绝。
func InspectUpdate(root, id string) (UpdateStatus, error) {
	entries, err := LoadLedger(root)
	if err != nil {
		return UpdateStatus{}, err
	}
	files, present, err := sourceFingerprints(root, id)
	if err != nil {
		return UpdateStatus{}, err
	}
	status := UpdateStatus{Exists: present}
	for _, entry := range entries {
		if entry.ID == id {
			// 源码被全部删除仍然是一次有本地修改的更新，不应绕过保护。
			status.Exists = true
			status.BaselineKnown = entry.Fingerprints != nil
			status.Modified = status.BaselineKnown && !maps.Equal(entry.Fingerprints, files)
			break
		}
	}
	return status, nil
}

func sourceFingerprints(root, id string) (map[string]string, bool, error) {
	if !plugin.ValidID(id) {
		return nil, false, fmt.Errorf("非法的插件 id %q", id)
	}
	files := make(map[string]string)
	present := false
	for _, tree := range []struct{ prefix, rel string }{
		{"backend", "plugins/" + id}, {"web", "web/src/plugins/" + id},
	} {
		if err := checkSourcePath(root, tree.rel); err != nil {
			return nil, false, err
		}
		base := filepath.Join(root, filepath.FromSlash(tree.rel))
		info, err := os.Lstat(base)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return nil, false, err
		}
		if !info.IsDir() {
			return nil, false, fmt.Errorf("插件源码不是目录: %s", base)
		}
		present = true
		if err := filepath.WalkDir(base, func(p string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() {
				return err
			}
			if !d.Type().IsRegular() {
				return fmt.Errorf("不支持的文件类型（符号链接？）: %s", p)
			}
			f, err := os.Open(p)
			if err != nil {
				return err
			}
			hash := sha256.New()
			_, err = io.Copy(hash, f)
			closeErr := f.Close()
			if err != nil || closeErr != nil {
				return errors.Join(err, closeErr)
			}
			rel, err := filepath.Rel(base, p)
			if err != nil {
				return err
			}
			files[tree.prefix+"/"+filepath.ToSlash(rel)] = hex.EncodeToString(hash.Sum(nil))
			return nil
		}); err != nil {
			return nil, false, err
		}
	}
	return files, present, nil
}

// RemoveLedger 删除一条安装记录；不存在时为空操作。
func RemoveLedger(root, id string) error {
	return updateLedger(root, func(entries []LedgerEntry) []LedgerEntry {
		out := entries[:0]
		for _, e := range entries {
			if e.ID != id {
				out = append(out, e)
			}
		}
		return out
	})
}

// updateLedger 读-改-写整份台账，临时文件 + rename 保证不会留下半截 JSON。
func updateLedger(root string, fn func([]LedgerEntry) []LedgerEntry) error {
	ledgerMu.Lock()
	defer ledgerMu.Unlock()
	entries, err := LoadLedger(root)
	if err != nil {
		return err
	}
	entries = fn(entries)
	sort.Slice(entries, func(i, j int) bool { return entries[i].ID < entries[j].ID })
	raw, err := json.MarshalIndent(ledgerFile{Plugins: entries}, "", "  ")
	if err != nil {
		return err
	}
	p := LedgerPath(root)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(p), ".installed-*.json")
	if err != nil {
		return err
	}
	name := tmp.Name()
	defer os.Remove(name) //nolint:errcheck
	if _, err := tmp.Write(append(raw, '\n')); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(name, p)
}
