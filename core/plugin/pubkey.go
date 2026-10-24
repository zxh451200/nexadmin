package plugin

import (
	"bufio"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
)

// devPublicKeyB64 是开发用公钥（32 字节 ed25519，base64）。
// 对应私钥由 `go run ./cmd/nexadmin plugin keygen` 生成，只在发行方手里，不进仓库。
// 正式发行前必须换成自己的公钥（并从 builtinPublicKeysB64 里去掉它）。
const devPublicKeyB64 = "Z0vlQMyRQ6f4GlrWdKOdJ5laJlIaaKYxzsfS1AXFLtI="

// builtinPublicKeysB64 是内置公钥集合（base64）。发行方日后加新钥：在这里追加，
// 或者不改源码、把公钥每行一把写进 config/license-keys.pub。
var builtinPublicKeysB64 = []string{devPublicKeyB64}

// LicenseKeysFile 是额外公钥文件（相对项目根目录），每行一把 base64 公钥，# 开头或行内 # 之后为注释。
const LicenseKeysFile = "config/license-keys.pub"

// Key 是带 kid 的授权公钥。
type Key struct {
	KID string
	Pub ed25519.PublicKey
}

// KeyID 由公钥指纹得出 kid：sha256(公钥) 前 4 字节的十六进制（8 个字符）。
func KeyID(pub ed25519.PublicKey) string {
	sum := sha256.Sum256(pub)
	return hex.EncodeToString(sum[:4])
}

func parseKey(b64 string) (Key, bool) {
	b, err := base64.StdEncoding.DecodeString(strings.TrimSpace(b64))
	if err != nil || len(b) != ed25519.PublicKeySize {
		return Key{}, false
	}
	pub := ed25519.PublicKey(b)
	return Key{KID: KeyID(pub), Pub: pub}, true
}

// BuiltinKeys 返回内置公钥集合。
func BuiltinKeys() []Key {
	out := make([]Key, 0, len(builtinPublicKeysB64))
	for _, s := range builtinPublicKeysB64 {
		if k, ok := parseKey(s); ok {
			out = append(out, k)
		}
	}
	return out
}

// IsDevKey 报告当前内置公钥集合是否包含开发公钥。`nexadmin release check` 用它拦住带着开发密钥上线。
func IsDevKey() bool {
	dev, ok := parseKey(devPublicKeyB64)
	if !ok {
		return false
	}
	for _, k := range BuiltinKeys() {
		if k.KID == dev.KID && k.Pub.Equal(dev.Pub) {
			return true
		}
	}
	return false
}

// PublicKey 返回内置的第一把授权公钥（兼容旧调用；校验应使用 PublicKeys）。
func PublicKey() ed25519.PublicKey {
	if ks := BuiltinKeys(); len(ks) > 0 {
		return ks[0].Pub
	}
	return nil
}

// PublicKeys 返回内置公钥 + <root>/config/license-keys.pub 里追加的公钥（按 kid 去重）。
// 文件不存在视为没有追加；无法解析的行被忽略。
func PublicKeys(root string) []Key {
	keys := BuiltinKeys()
	seen := map[string]bool{}
	for _, k := range keys {
		seen[k.KID] = true
	}
	f, err := os.Open(filepath.Join(root, filepath.FromSlash(LicenseKeysFile)))
	if err != nil {
		return keys
	}
	defer f.Close() //nolint:errcheck // 只读
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := sc.Text()
		if i := strings.IndexByte(line, '#'); i >= 0 {
			line = line[:i]
		}
		if line = strings.TrimSpace(line); line == "" {
			continue
		}
		if k, ok := parseKey(line); ok && !seen[k.KID] {
			seen[k.KID] = true
			keys = append(keys, k)
		}
	}
	return keys
}
