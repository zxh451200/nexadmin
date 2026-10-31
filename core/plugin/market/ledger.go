package market

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"
)

// LedgerEntry 是本机安装台账里的一条记录：只记录「用户装了什么」。
// 随二进制编译进来的内置插件不进台账，后台「插件管理」据此把已安装与内置分开列。
type LedgerEntry struct {
	ID          string    `json:"id"`
	Version     string    `json:"version"`
	InstalledAt time.Time `json:"installedAt"`
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
	raw, err := os.ReadFile(LedgerPath(root))
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var f ledgerFile
	if err := json.Unmarshal(raw, &f); err != nil {
		return nil, fmt.Errorf("安装台账 %s 解析失败: %w", LedgerPath(root), err)
	}
	return f.Plugins, nil
}

// AddLedger 记录一次安装；同 id 覆盖版本与时间。
func AddLedger(root, id, version string) error {
	return updateLedger(root, func(entries []LedgerEntry) []LedgerEntry {
		now := time.Now().UTC()
		for i := range entries {
			if entries[i].ID == id {
				entries[i].Version, entries[i].InstalledAt = version, now
				return entries
			}
		}
		return append(entries, LedgerEntry{ID: id, Version: version, InstalledAt: now})
	})
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
