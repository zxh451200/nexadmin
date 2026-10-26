package main

// 吊销清单格式规范（与 nexadmin 框架 core/plugin/revoke.go 靠协议耦合，不靠代码耦合）。
//
// 清单文件（框架读取 config/license-revoked.json）与授权文件是同一种 envelope（见 format.go 顶部），
// 由签发授权的同一把私钥签名，框架用与授权相同的公钥集合验签，kid 指明签发钥。载荷 JSON：
//
//	type        string          固定 "nexadmin-license-revocations"，防止把授权文件当清单
//	seq         int64           清单版本，单调递增；框架忽略 seq 比内存中已采信版本更小的清单（防回退）
//	updated_at  string          最近一次变更时间，RFC3339（UTC）
//	entries     []RevokedEntry  全量条目（每次导出都包含此前所有吊销）
//
// RevokedEntry: serial（授权序列号）、plugin（可选，仅供人读）、revoked_at（RFC3339）、reason（可选，显示在客户的插件管理页）。
//
// 重要：源码交付下买家可以直接删除清单，这是辅助手段，不是强保护。清单要随升级包分发才有意义。
// 本后台的吊销记录存放在 <data>/revoked.json（含 seq），导出时用当前私钥签名；
// ed25519 签名是确定性的，同一份记录反复导出得到相同字节，只有新增吊销才会让 seq 加一。

import (
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	revocationsType = "nexadmin-license-revocations"
	maxReasonRunes  = 200
)

// RevokedEntry 是吊销清单里的一条。
type RevokedEntry struct {
	Serial    string `json:"serial"`
	Plugin    string `json:"plugin,omitempty"`
	RevokedAt string `json:"revoked_at"`
	Reason    string `json:"reason,omitempty"`
}

// revData 是 revoked.json 的内容，也是签名清单载荷（除 type 外）。
type revData struct {
	Seq       int64          `json:"seq"`
	UpdatedAt string         `json:"updated_at"`
	Entries   []RevokedEntry `json:"entries"`
}

type revocations struct {
	Type      string         `json:"type"`
	Seq       int64          `json:"seq"`
	UpdatedAt string         `json:"updated_at"`
	Entries   []RevokedEntry `json:"entries"`
}

var (
	errNoSuchSerial   = errors.New("没有这个序列号的签发记录（旧授权不带序列号，不能单独吊销）")
	errAlreadyRevoked = errors.New("该授权已经吊销")
)

// signRevocations 按规范签发吊销清单文本（不含末尾换行）。
func signRevocations(priv ed25519.PrivateKey, d revData) ([]byte, error) {
	if d.Entries == nil {
		d.Entries = []RevokedEntry{}
	}
	payload, err := json.Marshal(revocations{Type: revocationsType, Seq: d.Seq, UpdatedAt: d.UpdatedAt, Entries: d.Entries})
	if err != nil {
		return nil, err
	}
	env := envelope{
		KID:       keyID(priv.Public().(ed25519.PublicKey)),
		Payload:   base64.StdEncoding.EncodeToString(payload),
		Signature: base64.StdEncoding.EncodeToString(ed25519.Sign(priv, payload)),
	}
	return json.MarshalIndent(env, "", "  ")
}

// verifyRevocations 是导出后的自检：验签、核对类型。通过则返回载荷。
func verifyRevocations(raw []byte, pub ed25519.PublicKey) (*revocations, error) {
	var env envelope
	if err := json.Unmarshal(raw, &env); err != nil {
		return nil, errors.New("吊销清单格式错误")
	}
	payload, err1 := base64.StdEncoding.DecodeString(env.Payload)
	sig, err2 := base64.StdEncoding.DecodeString(env.Signature)
	if err1 != nil || err2 != nil || len(sig) != ed25519.SignatureSize {
		return nil, errors.New("吊销清单格式错误")
	}
	if env.KID != "" && env.KID != keyID(pub) {
		return nil, errors.New("kid 与公钥不符")
	}
	if !ed25519.Verify(pub, payload, sig) {
		return nil, errors.New("签名校验失败")
	}
	var r revocations
	if err := json.Unmarshal(payload, &r); err != nil || r.Type != revocationsType {
		return nil, errors.New("吊销清单载荷格式错误")
	}
	return &r, nil
}

// loadRevoked 读取 <data>/revoked.json；不存在视为空。
func (s *store) loadRevoked() error {
	raw, err := os.ReadFile(s.revPath)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if len(strings.TrimSpace(string(raw))) == 0 {
		return nil
	}
	return json.Unmarshal(raw, &s.rev)
}

// Revoked 返回吊销记录的副本。
func (s *store) Revoked() revData {
	s.mu.Lock()
	defer s.mu.Unlock()
	d := s.rev
	d.Entries = append([]RevokedEntry(nil), s.rev.Entries...)
	return d
}

// RevokedMap 返回 序列号 -> 吊销条目。
func (s *store) RevokedMap() map[string]RevokedEntry {
	d := s.Revoked()
	m := make(map[string]RevokedEntry, len(d.Entries))
	for _, e := range d.Entries {
		m[e.Serial] = e
	}
	return m
}

// Revoke 吊销一条已签发记录的授权：序列号必须存在于签发记录，不能重复；seq 加一并原子落盘（0600）。
// 落盘失败时内存回滚。
func (s *store) Revoke(serial, reason string, now time.Time) (RevokedEntry, Record, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var rec Record
	found := false
	if serial != "" {
		for _, r := range s.recs {
			if r.Serial == serial {
				rec, found = r, true
				break
			}
		}
	}
	if !found {
		return RevokedEntry{}, Record{}, errNoSuchSerial
	}
	for _, e := range s.rev.Entries {
		if e.Serial == serial {
			return RevokedEntry{}, Record{}, errAlreadyRevoked
		}
	}
	ts := now.UTC().Format(time.RFC3339)
	e := RevokedEntry{Serial: serial, Plugin: rec.Plugin, RevokedAt: ts, Reason: reason}
	next := revData{Seq: s.rev.Seq + 1, UpdatedAt: ts, Entries: append(append([]RevokedEntry(nil), s.rev.Entries...), e)}
	raw, err := json.MarshalIndent(next, "", "  ")
	if err != nil {
		return RevokedEntry{}, Record{}, err
	}
	if err := writeAtomic(s.revPath, ".revoked-*.tmp", append(raw, '\n')); err != nil {
		return RevokedEntry{}, Record{}, err
	}
	s.rev = next
	return e, rec, nil
}

// writeAtomic 临时文件 + rename，权限 0600。
func writeAtomic(path, pattern string, data []byte) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), pattern)
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name()) //nolint:errcheck // rename 成功后已不存在
	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close() //nolint:errcheck
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		tmp.Close() //nolint:errcheck
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}
