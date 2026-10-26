package main

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"
)

// Record 是一条签发记录。不含私钥，也不含授权全文。
type Record struct {
	ID        string   `json:"id"`
	IssuedAt  string   `json:"issued_at"`
	Plugin    string   `json:"plugin"`
	Customer  string   `json:"customer"`
	ExpiresAt string   `json:"expires_at"`
	Domains   []string `json:"domains,omitempty"`
	KID       string   `json:"kid"`
	Note      string   `json:"note,omitempty"`
	// Serial 是写进授权载荷的序列号（= ID）。旧记录没有：当时签发的授权不带序列号，不能被单独吊销。
	Serial string `json:"serial,omitempty"`
}

// expiringWindow 是「即将到期」的窗口。
const expiringWindow = 30 * 24 * time.Hour

// DaysLeft 返回距到期的整天数（向下取整，已到期为负）。
func (r Record) DaysLeft(now time.Time) int {
	exp, err := time.Parse(time.RFC3339, r.ExpiresAt)
	if err != nil {
		return 0
	}
	d := exp.Sub(now)
	days := int(d / (24 * time.Hour))
	if d < 0 && d%(24*time.Hour) != 0 {
		days--
	}
	return days
}

// ExpiringSoon 报告记录是否在 30 天内到期（已过期的不算，另有「已过期」状态）。
func (r Record) ExpiringSoon(now time.Time) bool {
	exp, err := time.Parse(time.RFC3339, r.ExpiresAt)
	if err != nil {
		return false
	}
	return exp.After(now) && exp.Sub(now) <= expiringWindow
}

type store struct {
	mu   sync.Mutex
	path string
	recs []Record

	revPath string  // <data>/revoked.json：吊销记录（见 revoked.go）
	rev     revData // 与 recs 同受 mu 保护
}

func openStore(dir string) (*store, error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	s := &store{path: filepath.Join(dir, "issued.json"), revPath: filepath.Join(dir, "revoked.json")}
	if err := s.loadRevoked(); err != nil {
		return nil, err
	}
	raw, err := os.ReadFile(s.path)
	if errors.Is(err, os.ErrNotExist) {
		return s, nil
	}
	if err != nil {
		return nil, err
	}
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &s.recs); err != nil {
			return nil, err
		}
	}
	return s, nil
}

// Add 追加一条记录并原子落盘（临时文件 + rename，0600）。落盘失败时内存也回滚。
func (s *store) Add(r Record) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	next := append(append([]Record(nil), s.recs...), r)
	raw, err := json.MarshalIndent(next, "", "  ")
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(s.path), ".issued-*.tmp")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name()) //nolint:errcheck // rename 成功后已不存在
	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close() //nolint:errcheck
		return err
	}
	if _, err := tmp.Write(append(raw, '\n')); err != nil {
		tmp.Close() //nolint:errcheck
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmp.Name(), s.path); err != nil {
		return err
	}
	s.recs = next
	return nil
}

// List 返回按签发时间倒序的记录副本。
func (s *store) List() []Record {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := append([]Record(nil), s.recs...)
	sort.SliceStable(out, func(i, j int) bool { return out[i].IssuedAt > out[j].IssuedAt })
	return out
}

// Expiring 返回 30 天内到期的记录，按到期时间升序。
func (s *store) Expiring(now time.Time) []Record {
	var out []Record
	for _, r := range s.List() {
		if r.ExpiringSoon(now) {
			out = append(out, r)
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].ExpiresAt < out[j].ExpiresAt })
	return out
}
