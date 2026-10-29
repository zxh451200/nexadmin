package plugin

// 离线吊销：授权仍是离线签名文件，不联网回查。发行方可以另外签发一份「吊销清单」
// （config/license-revoked.json），框架读取并验签后，把清单里序列号命中的授权标为 revoked。
//
// 重要：这是辅助手段，不是强保护。源码交付下买家可以直接删除清单文件、回退到旧版本清单，
// 甚至改掉校验代码；本机制只能挡住“不改代码、只按流程升级”的正常使用场景。
// 真正的约束来自合同、升级服务与技术支持，吊销清单随升级包分发才有意义（见 nexadmin release check 的人工确认项）。
//
// 设计要点：
//   - 清单文件格式与授权文件同构（envelope：kid / payload / signature），用与授权相同的公钥集合验签，kid 指明签发钥。
//     载荷带 type 字段，避免把授权文件当清单（或反过来）。
//   - 验签失败 / 格式错误 / 版本回退（seq 小于内存中已采信的版本）的清单一律忽略并告警，
//     沿用此前已验证的清单——这样篡改清单只会“无效”，不会造成误吊销或悄悄解除吊销。
//   - 清单文件被删除时同样沿用内存中已验证的清单，直到进程重启；这只是顺带的加固，不要当作保证。
//   - 没有序列号的旧授权不能被单独吊销（序列号为空永不命中）。

import (
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"time"

	"go.uber.org/zap"
)

// RevokedFile 是吊销清单（相对项目根目录）。放在 config/ 而不是 data/：
// 它由发行方签发、随升级包分发，属于“发行方拥有”的文件，与 config/license-keys.pub 并列；
// data/ 是运行产生的数据，release check 要求发布前清空。
const RevokedFile = "config/license-revoked.json"

// MaxRevocationSize 是吊销清单的大小上限。
const MaxRevocationSize = 512 << 10

const (
	revocationsType     = "nexadmin-license-revocations"
	maxRevokeReasonRune = 500
)

var serialRe = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{5,63}$`)

// ValidSerial 判断序列号格式：小写字母/数字/连字符，6~64 位，不以连字符开头。
func ValidSerial(s string) bool { return serialRe.MatchString(s) }

// NewSerial 生成一个随机序列号（12 位小写十六进制，与发行方后台的签发记录 ID 同格式）。
func NewSerial() string {
	var b [6]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic("plugin: crypto/rand unavailable: " + err.Error())
	}
	return hex.EncodeToString(b[:])
}

// RevokedEntry 是清单里的一条：被吊销授权的序列号、吊销时间与原因。
// Plugin 仅供人读，命中只看序列号。
type RevokedEntry struct {
	Serial    string `json:"serial"`
	Plugin    string `json:"plugin,omitempty"`
	RevokedAt string `json:"revoked_at"`
	Reason    string `json:"reason,omitempty"`
}

// Revocations 是吊销清单的载荷。清单是“全量”的：每次签发都包含此前所有条目，Seq 单调递增。
type Revocations struct {
	Type      string         `json:"type"`
	Seq       int64          `json:"seq"`
	UpdatedAt string         `json:"updated_at"`
	Entries   []RevokedEntry `json:"entries"`
}

// Find 按序列号查找；空序列号永不命中。
func (r *Revocations) Find(serial string) (RevokedEntry, bool) {
	if r == nil || serial == "" {
		return RevokedEntry{}, false
	}
	for _, e := range r.Entries {
		if e.Serial == serial {
			return e, true
		}
	}
	return RevokedEntry{}, false
}

// RevocationsPath 返回 <root>/config/license-revoked.json。
func RevocationsPath(root string) string {
	return filepath.Join(root, filepath.FromSlash(RevokedFile))
}

func revokedReason(e RevokedEntry) string {
	s := "授权已被发行方吊销（序列号 " + e.Serial
	if e.RevokedAt != "" {
		s += "，" + e.RevokedAt
	}
	s += "）"
	if e.Reason != "" {
		s += "：" + e.Reason
	}
	return s
}

func revokedError(e RevokedEntry) *LicenseError {
	return &LicenseError{Status: StatusRevoked, Reason: revokedReason(e)}
}

// SignRevocations 用私钥签发吊销清单文本（与授权文件同一种 envelope）。
func SignRevocations(priv ed25519.PrivateKey, r Revocations) ([]byte, error) {
	r.Type = revocationsType
	if r.Entries == nil {
		r.Entries = []RevokedEntry{}
	}
	payload, err := json.Marshal(r)
	if err != nil {
		return nil, err
	}
	env := envelope{
		KID:       KeyID(priv.Public().(ed25519.PublicKey)),
		Payload:   base64.StdEncoding.EncodeToString(payload),
		Signature: base64.StdEncoding.EncodeToString(ed25519.Sign(priv, payload)),
	}
	return json.MarshalIndent(env, "", "  ")
}

// VerifyRevocations 校验吊销清单：签名（kid 选钥规则同授权）-> 载荷格式 -> 类型 -> 条目合法性。
// 任何一步失败都返回错误，调用方应当整份忽略。
func VerifyRevocations(raw []byte, keys []Key) (*Revocations, error) {
	payload, lerr := openEnvelope(raw, keys, MaxRevocationSize, "吊销清单")
	if lerr != nil {
		return nil, errors.New(lerr.Reason)
	}
	var r Revocations
	if err := json.Unmarshal(payload, &r); err != nil {
		return nil, errors.New("吊销清单载荷格式错误")
	}
	if r.Type != revocationsType {
		return nil, errors.New("吊销清单类型不符")
	}
	if r.Seq < 0 {
		return nil, errors.New("吊销清单 seq 非法")
	}
	seen := make(map[string]bool, len(r.Entries))
	for _, e := range r.Entries {
		if !ValidSerial(e.Serial) {
			return nil, fmt.Errorf("吊销清单含非法序列号 %q", e.Serial)
		}
		if seen[e.Serial] {
			return nil, fmt.Errorf("吊销清单序列号重复 %q", e.Serial)
		}
		seen[e.Serial] = true
	}
	return &r, nil
}

// LoadRevocations 读取并验签 <root>/config/license-revoked.json。文件不存在返回 (nil, nil)。
func LoadRevocations(root string, keys []Key) (*Revocations, error) {
	raw, err := os.ReadFile(RevocationsPath(root))
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("读取吊销清单失败: %w", err)
	}
	return VerifyRevocations(raw, keys)
}

// AddRevocation 在现有清单（prev，可为空）上追加一条并用私钥重新签发整份清单（seq 加一）。
// 现有清单必须能用该私钥对应的公钥验签，否则拒绝追加，避免在被篡改或别人签发的清单上继续签名。
// 序列号已在清单里时返回错误。
func AddRevocation(priv ed25519.PrivateKey, prev []byte, e RevokedEntry, now time.Time) ([]byte, error) {
	if !ValidSerial(e.Serial) {
		return nil, fmt.Errorf("非法的序列号 %q", e.Serial)
	}
	if e.Plugin != "" && !ValidID(e.Plugin) {
		return nil, fmt.Errorf("非法的插件 id %q", e.Plugin)
	}
	if len([]rune(e.Reason)) > maxRevokeReasonRune {
		return nil, fmt.Errorf("原因不超过 %d 字", maxRevokeReasonRune)
	}
	pub := priv.Public().(ed25519.PublicKey)
	r := &Revocations{}
	if len(bytes.TrimSpace(prev)) > 0 {
		old, err := VerifyRevocations(prev, []Key{{KID: KeyID(pub), Pub: pub}})
		if err != nil {
			return nil, fmt.Errorf("现有清单无法用该私钥验签，拒绝在其上追加: %w", err)
		}
		r = old
	}
	if _, dup := r.Find(e.Serial); dup {
		return nil, fmt.Errorf("序列号 %s 已在清单中", e.Serial)
	}
	ts := now.UTC().Format(time.RFC3339)
	if e.RevokedAt == "" {
		e.RevokedAt = ts
	}
	r.Entries = append(r.Entries, e)
	r.Seq++
	r.UpdatedAt = ts
	return SignRevocations(priv, *r)
}

// ---- Manager 侧：热加载 ----

type pendingWarn struct {
	msg    string
	fields []zap.Field
}

// warnLocked 记告警日志；Attach 之前还没有 logger，先缓存，Attach 时补发。调用方需持有 m.mu（或在构造阶段）。
func (m *Manager) warnLocked(msg string, fields ...zap.Field) {
	if m.deps == nil || m.deps.Log == nil {
		m.pendingWarns = append(m.pendingWarns, pendingWarn{msg, fields})
		return
	}
	m.deps.Log.Warn(msg, fields...)
}

func (m *Manager) flushWarnsLocked() {
	if m.deps == nil || m.deps.Log == nil {
		return
	}
	for _, w := range m.pendingWarns {
		m.deps.Log.Warn(w.msg, w.fields...)
	}
	m.pendingWarns = nil
}

// refreshRevocationsLocked 检查吊销清单文件是否变化（一次 stat，文件没变就直接返回），
// 变化则重新读取、验签，并把结果应用到所有插件。随定时检查、插件列表读取与请求前检查调用，不需要重启。
// 调用方需持有 m.mu（或在构造阶段）。
func (m *Manager) refreshRevocationsLocked() {
	path := RevocationsPath(m.opts.Root)
	fi, err := os.Stat(path)
	if err != nil {
		if m.revFP != "" && m.revFP != "missing" {
			m.warnLocked("plugin revocation list is gone; keeping the last verified list until restart", zap.String("file", RevokedFile))
		}
		if m.revFP != "" {
			m.revFP = "missing"
		}
		return
	}
	fp := fmt.Sprintf("%d-%d", fi.ModTime().UnixNano(), fi.Size())
	if fp == m.revFP {
		return
	}
	m.revFP = fp // 先记下指纹：无效的清单只告警一次，文件再变化才会重新处理
	if fi.Size() > MaxRevocationSize {
		m.warnLocked("plugin revocation list ignored: too large", zap.String("file", RevokedFile))
		return
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		m.warnLocked("plugin revocation list ignored: unreadable", zap.String("file", RevokedFile), zap.Error(err))
		return
	}
	rev, err := VerifyRevocations(raw, m.opts.Keys)
	if err != nil {
		m.warnLocked("plugin revocation list ignored: verification failed (tampered or not signed by a trusted key)",
			zap.String("file", RevokedFile), zap.Error(err))
		return
	}
	if rev.Seq < m.revSeq {
		m.warnLocked("plugin revocation list ignored: older than the one already in use",
			zap.String("file", RevokedFile), zap.Int64("seq", rev.Seq), zap.Int64("current", m.revSeq))
		return
	}
	by := make(map[string]RevokedEntry, len(rev.Entries))
	for _, e := range rev.Entries {
		by[e.Serial] = e
	}
	m.revSeq, m.revBy = rev.Seq, by
	m.reapplyRevocationsLocked()
}

// reapplyRevocationsLocked 把当前清单应用到所有插件：命中的标 revoked；
// 之前被吊销、新清单已不含的，回到授权文件本身的状态（重新校验授权文件）。
// 注意：解除吊销只恢复状态，插件路由在启动时没挂载的话仍需重新导入授权或重启才会挂载。
func (m *Manager) reapplyRevocationsLocked() {
	for _, e := range m.entries {
		if e.fatal || e.serial == "" {
			continue
		}
		if r, ok := m.revBy[e.serial]; ok {
			m.markRevokedLocked(e, r)
		} else if e.info.Status == StatusRevoked {
			l, err := checkFile(m.opts.Root, e.p.ID, m.opts.Now(), m.opts.Keys)
			m.applyLic(e, l, err)
		}
	}
}

// markRevokedLocked 把插件标为 revoked（授权的其它字段保留，便于页面展示客户与到期时间）。
func (m *Manager) markRevokedLocked(e *entry, r RevokedEntry) {
	prev := e.info.Status
	e.info.Status, e.info.Reason = StatusRevoked, revokedReason(r)
	m.syncLifecycleLocked(e)
	if prev != StatusRevoked {
		m.warnLocked("plugin license revoked, routes disabled",
			zap.String("plugin", e.p.ID), zap.String("serial", r.Serial), zap.String("reason", r.Reason))
	}
}

// applyRevokedLocked 在授权状态刷新后检查序列号是否命中当前清单。
func (m *Manager) applyRevokedLocked(e *entry) {
	if e.fatal || e.serial == "" {
		return
	}
	if r, ok := m.revBy[e.serial]; ok {
		m.markRevokedLocked(e, r)
	}
}
