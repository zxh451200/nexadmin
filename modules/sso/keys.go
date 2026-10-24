package sso

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
	"errors"
	"fmt"
	"math/big"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// keyStore 管理 RS256 签名密钥：首次使用生成并落盘（私钥 0600），多 key 并存以支持轮换。
// 文件名 <unixnano>-<kid>.pem，按文件名排序，最后一个是当前签名 key，全部公钥都在 JWKS 里发布。
type keyStore struct {
	dir  string
	mu   sync.RWMutex
	keys []*signingKey // 旧 -> 新
	now  func() time.Time
}

type signingKey struct {
	kid     string
	priv    *rsa.PrivateKey
	created time.Time
	file    string
}

// retireAfter 是旧 key 被新 key 取代后继续留在 JWKS 里的时间（让已签发的令牌和下游缓存的 JWKS 平滑过渡）。
const retireAfter = 7 * 24 * time.Hour

func (k *keyStore) clock() time.Time {
	if k.now != nil {
		return k.now()
	}
	return time.Now()
}

// expiredAt 报告第 i 个 key 是否已过保留期：只有被取代的 key 才会过期，当前签名 key 永远不会。
// 保留期从“下一个 key 的创建时间”起算（即被取代的时刻）。调用方需持有 mu。
func (k *keyStore) expiredAt(i int, now time.Time) bool {
	return i < len(k.keys)-1 && !now.Before(k.keys[i+1].created.Add(retireAfter))
}

// pruneLocked 删除已过期 key（内存与磁盘）。调用方需持有写锁。
func (k *keyStore) pruneLocked(now time.Time) {
	kept := k.keys[:0:0]
	for i, s := range k.keys {
		if k.expiredAt(i, now) {
			if s.file != "" {
				_ = os.Remove(s.file)
			}
			continue
		}
		kept = append(kept, s)
	}
	k.keys = kept
}

// keyInfo 是管理接口展示的 key 信息（不含任何密钥材料）。
type keyInfo struct {
	Kid       string     `json:"kid"`
	CreatedAt time.Time  `json:"createdAt"`
	Status    string     `json:"status"`              // active=当前签名 key；retiring=仅用于验签，到期移除
	RetiresAt *time.Time `json:"retiresAt,omitempty"` // 仅 retiring
}

// list 返回仍在 JWKS 里的 key，新的在前。
func (k *keyStore) list() []keyInfo {
	k.mu.Lock()
	defer k.mu.Unlock()
	now := k.clock()
	k.pruneLocked(now)
	out := make([]keyInfo, 0, len(k.keys))
	for i := len(k.keys) - 1; i >= 0; i-- {
		s := k.keys[i]
		info := keyInfo{Kid: s.kid, CreatedAt: s.created, Status: "active"}
		if i < len(k.keys)-1 {
			r := k.keys[i+1].created.Add(retireAfter)
			info.Status, info.RetiresAt = "retiring", &r
		}
		out = append(out, info)
	}
	return out
}

func loadKeyStore(dir string) (*keyStore, error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, fmt.Errorf("sso: create key dir: %w", err)
	}
	ks := &keyStore{dir: dir}
	files, err := filepath.Glob(filepath.Join(dir, "*.pem"))
	if err != nil {
		return nil, err
	}
	sort.Strings(files)
	for _, f := range files {
		raw, err := os.ReadFile(f)
		if err != nil {
			return nil, fmt.Errorf("sso: read key %s: %w", f, err)
		}
		blk, _ := pem.Decode(raw)
		if blk == nil {
			return nil, fmt.Errorf("sso: key %s is not PEM", f)
		}
		k, err := x509.ParsePKCS8PrivateKey(blk.Bytes)
		if err != nil {
			return nil, fmt.Errorf("sso: parse key %s: %w", f, err)
		}
		priv, ok := k.(*rsa.PrivateKey)
		if !ok {
			return nil, fmt.Errorf("sso: key %s is not RSA", f)
		}
		created := time.Time{}
		if ns, _, ok := strings.Cut(filepath.Base(f), "-"); ok {
			if n, err := strconv.ParseInt(ns, 10, 64); err == nil {
				created = time.Unix(0, n)
			}
		}
		if created.IsZero() {
			if st, err := os.Stat(f); err == nil {
				created = st.ModTime()
			}
		}
		ks.keys = append(ks.keys, &signingKey{kid: kidOf(&priv.PublicKey), priv: priv, created: created, file: f})
	}
	ks.mu.Lock()
	ks.pruneLocked(ks.clock())
	ks.mu.Unlock()
	if len(ks.keys) == 0 {
		if err := ks.Rotate(); err != nil {
			return nil, err
		}
	}
	return ks, nil
}

func kidOf(pub *rsa.PublicKey) string {
	der, _ := x509.MarshalPKIXPublicKey(pub)
	sum := sha256.Sum256(der)
	return base64.RawURLEncoding.EncodeToString(sum[:])[:16]
}

// Rotate 生成新 key 并设为当前签名 key；旧 key 保留在 JWKS 里直到被手工删除（为轮换预留）。
func (k *keyStore) Rotate() error {
	priv, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return err
	}
	der, err := x509.MarshalPKCS8PrivateKey(priv)
	if err != nil {
		return err
	}
	kid := kidOf(&priv.PublicKey)
	created := k.clock()
	name := filepath.Join(k.dir, strconv.FormatInt(created.UnixNano(), 10)+"-"+kid+".pem")
	f, err := os.OpenFile(name, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return fmt.Errorf("sso: write key: %w", err)
	}
	if err := pem.Encode(f, &pem.Block{Type: "PRIVATE KEY", Bytes: der}); err != nil {
		_ = f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	k.mu.Lock()
	k.keys = append(k.keys, &signingKey{kid: kid, priv: priv, created: created, file: name})
	k.pruneLocked(created)
	k.mu.Unlock()
	return nil
}

func (k *keyStore) active() *signingKey {
	k.mu.RLock()
	defer k.mu.RUnlock()
	return k.keys[len(k.keys)-1]
}

func (k *keyStore) public(kid string) *rsa.PublicKey {
	k.mu.RLock()
	defer k.mu.RUnlock()
	now := k.clock()
	for i, s := range k.keys {
		if s.kid == kid && !k.expiredAt(i, now) {
			return &s.priv.PublicKey
		}
	}
	return nil
}

// jwks 返回 JWK Set（只含公钥）。
func (k *keyStore) jwks() map[string]any {
	k.mu.RLock()
	defer k.mu.RUnlock()
	list := make([]map[string]string, 0, len(k.keys))
	now := k.clock()
	for i := len(k.keys) - 1; i >= 0; i-- {
		if k.expiredAt(i, now) {
			continue
		}
		pub := &k.keys[i].priv.PublicKey
		list = append(list, map[string]string{
			"kty": "RSA", "use": "sig", "alg": "RS256", "kid": k.keys[i].kid,
			"n": base64.RawURLEncoding.EncodeToString(pub.N.Bytes()),
			"e": base64.RawURLEncoding.EncodeToString(big.NewInt(int64(pub.E)).Bytes()),
		})
	}
	return map[string]any{"keys": list}
}

// sign 用当前 key 签发 RS256 JWT；typ 写入 JOSE 头（access token 用 "at+jwt"）。
func (k *keyStore) sign(claims jwt.MapClaims, typ string) (string, error) {
	sk := k.active()
	t := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
	t.Header["kid"] = sk.kid
	t.Header["typ"] = typ
	return t.SignedString(sk.priv)
}

var errBadToken = errors.New("sso: invalid token")

// parse 验签（按 kid 选公钥，只接受 RS256）；返回载荷与 typ 头。
func (k *keyStore) parse(token string, opts ...jwt.ParserOption) (jwt.MapClaims, string, error) {
	opts = append([]jwt.ParserOption{jwt.WithValidMethods([]string{"RS256"})}, opts...)
	claims := jwt.MapClaims{}
	t, err := jwt.ParseWithClaims(strings.TrimSpace(token), claims, func(t *jwt.Token) (any, error) {
		kid, _ := t.Header["kid"].(string)
		if pub := k.public(kid); pub != nil {
			return pub, nil
		}
		return nil, errBadToken
	}, opts...)
	if err != nil || !t.Valid {
		return nil, "", errBadToken
	}
	typ, _ := t.Header["typ"].(string)
	return claims, typ, nil
}
