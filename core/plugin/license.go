package plugin

import (
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// 插件状态。
const (
	StatusActive     = "active"
	StatusUnlicensed = "unlicensed"
	StatusExpired    = "expired"
	StatusInvalid    = "invalid"
	StatusRevoked    = "revoked"  // 序列号在已验签的吊销清单里（见 revoke.go）
	StatusDisabled   = "disabled" // 在配置 disabled_plugins 中被停用（free 插件没有授权闸门，用配置开关软停用）
)

// MaxLicenseSize 是授权文件的大小上限。
const MaxLicenseSize = 16 << 10

// License 是授权文件的载荷。
type License struct {
	Plugin    string   `json:"plugin"`
	Customer  string   `json:"customer"`
	IssuedAt  string   `json:"issued_at"`
	ExpiresAt string   `json:"expires_at"`
	Domains   []string `json:"domains,omitempty"`
	// Serial 是授权的唯一序列号，吊销清单靠它命中。放在最后且 omitempty：
	// 旧授权文件没有该字段，载荷字节与签名都不变，仍可验证；没有序列号的授权不能被单独吊销。
	Serial string `json:"serial,omitempty"`
}

// envelope 是授权文件的文本格式：payload 是载荷 JSON 的 base64，signature 是对 payload 原始字节的 ed25519 签名。
// 签名对象是原始字节而不是重新序列化的结构，所以不存在字段顺序/空白导致的验签歧义。
// kid 是签发公钥的指纹短哈希；旧授权文件没有它，校验时依次尝试全部公钥。
type envelope struct {
	KID       string `json:"kid,omitempty"`
	Payload   string `json:"payload"`
	Signature string `json:"signature"`
}

// LicenseError 带着对应的插件状态（unlicensed / expired / invalid）和人能读懂的原因。
type LicenseError struct {
	Status string
	Reason string
	// ActionHint 是给使用者的下一步建议；展示时拼在 Reason 之后。
	ActionHint string
}

func (e *LicenseError) Error() string { return e.Status + ": " + e.Reason }

// Message 返回「原因 + 下一步」，供后台直接展示；没有建议时只返回原因。
func (e *LicenseError) Message() string {
	if e.ActionHint == "" {
		return e.Reason
	}
	return e.Reason + "；" + e.ActionHint
}

func invalid(format string, a ...any) *LicenseError {
	return &LicenseError{
		Status:     StatusInvalid,
		Reason:     fmt.Sprintf(format, a...),
		ActionHint: "请确认授权文件属于该插件且由受信任的发行方签发；必要时向发行方重新获取",
	}
}

// Sign 用私钥签发授权文件文本。
func Sign(priv ed25519.PrivateKey, l License) ([]byte, error) {
	payload, err := json.Marshal(l)
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

// Verify 校验授权文件文本：签名 -> 载荷 -> 插件名 -> 有效期。失败时返回 *LicenseError。
func Verify(raw []byte, pluginID string, now time.Time, pub ed25519.PublicKey) (*License, error) {
	if len(pub) != ed25519.PublicKeySize {
		return nil, invalid("框架未内置有效的授权公钥")
	}
	return VerifyKeys(raw, pluginID, now, []Key{{KID: KeyID(pub), Pub: pub}})
}

// openEnvelope 做授权文件 / 吊销清单共用的外层校验：大小 -> 公钥 -> 信封格式 -> kid 选钥 -> 验签，
// 返回已验签的载荷原始字节。what 是错误信息里的名词（如“授权文件”“吊销清单”）。
// 授权与吊销清单使用同一套公钥集合与 kid 规则：带 kid 时只用 kid 相同的公钥，没有 kid 时依次尝试全部公钥。
func openEnvelope(raw []byte, keys []Key, maxSize int, what string) ([]byte, *LicenseError) {
	if len(raw) == 0 || len(raw) > maxSize {
		return nil, invalid("%s为空或过大", what)
	}
	valid := keys[:0:0]
	for _, k := range keys {
		if len(k.Pub) == ed25519.PublicKeySize {
			valid = append(valid, k)
		}
	}
	if len(valid) == 0 {
		return nil, invalid("框架未内置有效的授权公钥")
	}
	var env envelope
	if err := json.Unmarshal(raw, &env); err != nil {
		return nil, invalid("%s格式错误", what)
	}
	payload, err1 := base64.StdEncoding.DecodeString(env.Payload)
	sig, err2 := base64.StdEncoding.DecodeString(env.Signature)
	if err1 != nil || err2 != nil || len(sig) != ed25519.SignatureSize {
		return nil, invalid("%s格式错误", what)
	}
	cands := valid
	if env.KID != "" {
		cands = nil
		for _, k := range valid {
			if k.KID == env.KID {
				cands = append(cands, k)
			}
		}
		if len(cands) == 0 {
			return nil, invalid("%s的签发密钥 %q 不在框架内置公钥中", what, env.KID)
		}
	}
	for _, k := range cands {
		if ed25519.Verify(k.Pub, payload, sig) {
			return payload, nil
		}
	}
	return nil, invalid("签名校验失败，%s被篡改或不是官方签发", what)
}

// VerifyKeys 同 Verify，但接受一组公钥：授权文件带 kid 时只用 kid 相同的公钥；
// 没有 kid（旧文件）时依次尝试全部公钥。
// 本函数只判断签名、插件名与有效期；是否被吊销由 Manager / CheckFile 结合吊销清单另行判定（见 revoke.go）。
func VerifyKeys(raw []byte, pluginID string, now time.Time, keys []Key) (*License, error) {
	payload, lerr := openEnvelope(raw, keys, MaxLicenseSize, "授权文件")
	if lerr != nil {
		return nil, lerr
	}
	var l License
	if err := json.Unmarshal(payload, &l); err != nil {
		return nil, invalid("授权载荷格式错误")
	}
	if l.Plugin != pluginID {
		return nil, invalid("授权文件属于插件 %q，不能用于 %q", l.Plugin, pluginID)
	}
	exp, err := time.Parse(time.RFC3339, l.ExpiresAt)
	if err != nil {
		return nil, invalid("expires_at 格式错误")
	}
	if !now.Before(exp) {
		return &l, &LicenseError{Status: StatusExpired, Reason: "授权已于 " + l.ExpiresAt + " 到期",
			ActionHint: "请联系发行方续期，然后在插件管理导入新的授权文件"}
	}
	return &l, nil
}

// LicensePath 返回 <root>/data/licenses/<id>.lic。id 必须合法，避免路径穿越。
func LicensePath(root, id string) (string, error) {
	if !ValidID(id) {
		return "", fmt.Errorf("非法的插件 id %q", id)
	}
	return filepath.Join(root, "data", "licenses", id+".lic"), nil
}

// checkFile 读取并校验授权文件；文件不存在返回 unlicensed。
func checkFile(root, id string, now time.Time, keys []Key) (*License, error) {
	p, err := LicensePath(root, id)
	if err != nil {
		return nil, invalid("%v", err)
	}
	raw, err := os.ReadFile(p)
	if errors.Is(err, os.ErrNotExist) {
		return nil, &LicenseError{Status: StatusUnlicensed, Reason: "未找到授权文件 data/licenses/" + id + ".lic",
			ActionHint: "请在插件市场购买授权，或改用免费版本"}
	}
	if err != nil {
		return nil, invalid("读取授权文件失败: %v", err)
	}
	return VerifyKeys(raw, id, now, keys)
}

// CheckFile 供 CLI 使用：校验 <root>/data/licenses/<id>.lic（使用内置公钥 + config/license-keys.pub），
// 并对照 config/license-revoked.json（验签通过才采信）；命中时返回 Status 为 revoked 的 *LicenseError。
func CheckFile(root, id string, now time.Time) (*License, error) {
	keys := PublicKeys(root)
	l, err := checkFile(root, id, now, keys)
	if l == nil || l.Serial == "" {
		return l, err
	}
	if rev, rerr := LoadRevocations(root, keys); rerr == nil && rev != nil {
		if e, ok := rev.Find(l.Serial); ok {
			return l, revokedError(e)
		}
	}
	return l, err
}

// writeLicense 原子写入授权文件（临时文件 + rename，权限 0600）。
func writeLicense(root, id string, raw []byte) error {
	p, err := LicensePath(root, id)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o750); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(p), "."+id+"-*.tmp")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name()) //nolint:errcheck // rename 成功后文件已不存在
	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close() //nolint:errcheck
		return err
	}
	if _, err := tmp.Write([]byte(strings.TrimSpace(string(raw)) + "\n")); err != nil {
		tmp.Close() //nolint:errcheck
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), p)
}
