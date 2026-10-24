package openapi

import (
	"bytes"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"math/big"
	"net"
	"net/netip"
	"net/url"
	"sort"
	"strings"
)

// TokenPrefix 是个人访问令牌的固定前缀。
const TokenPrefix = "nxp_"

const base62 = "0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz"

// RandString 返回 n 位 base62 随机串（crypto/rand，无取模偏差）。
func RandString(n int) string {
	max := big.NewInt(int64(len(base62)))
	var sb strings.Builder
	for i := 0; i < n; i++ {
		v, err := rand.Int(rand.Reader, max)
		if err != nil {
			panic("openapi: crypto/rand failed: " + err.Error())
		}
		sb.WriteByte(base62[v.Int64()])
	}
	return sb.String()
}

// NewPATToken 生成令牌明文 nxp_<32 位随机>，同时返回库里要存的 sha256 哈希（hex）和前 8 位 prefix。
func NewPATToken() (plain, hash, prefix string) {
	plain = TokenPrefix + RandString(32)
	return plain, HashToken(plain), plain[:8]
}

// HashToken 返回令牌的 sha256（hex）。令牌是 190+ 位熵的随机串，不需要加盐慢哈希。
func HashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

// IsPATFormat 粗略判断是不是 nxp_ + 32 位字母数字（过滤明显的垃圾输入，避免无谓查库）。
func IsPATFormat(token string) bool {
	if len(token) != len(TokenPrefix)+32 || !strings.HasPrefix(token, TokenPrefix) {
		return false
	}
	for _, r := range token[len(TokenPrefix):] {
		if !(r >= '0' && r <= '9' || r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z') {
			return false
		}
	}
	return true
}

// CanonicalQuery 规范化查询串：按 key 排序，同 key 的 value 也排序，key/value 都用 url.QueryEscape，
// 以 & 连接。无参数返回空串。客户端签名时必须用同样的规则。
func CanonicalQuery(raw string) (string, error) {
	if raw == "" {
		return "", nil
	}
	vals, err := url.ParseQuery(raw)
	if err != nil {
		return "", err
	}
	keys := make([]string, 0, len(vals))
	for k := range vals {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var parts []string
	for _, k := range keys {
		vs := append([]string(nil), vals[k]...)
		sort.Strings(vs)
		for _, v := range vs {
			parts = append(parts, url.QueryEscape(k)+"="+url.QueryEscape(v))
		}
	}
	return strings.Join(parts, "&"), nil
}

// StringToSign 拼出待签名串：METHOD \n path \n 规范化query \n sha256hex(body) \n timestamp \n nonce。
func StringToSign(method, path, canonicalQuery string, body []byte, timestamp, nonce string) string {
	sum := sha256.Sum256(body)
	return strings.ToUpper(method) + "\n" + path + "\n" + canonicalQuery + "\n" + hex.EncodeToString(sum[:]) + "\n" + timestamp + "\n" + nonce
}

// Sign 计算请求签名 hex(HMAC-SHA256(appSecret, StringToSign))。rawQuery 是原始查询串，函数内部规范化。
// 它同时是服务端校验和客户端（含单测）生成签名用的同一个实现。
func Sign(appSecret, method, path, rawQuery string, body []byte, timestamp, nonce string) (string, error) {
	cq, err := CanonicalQuery(rawQuery)
	if err != nil {
		return "", err
	}
	mac := hmac.New(sha256.New, []byte(appSecret))
	mac.Write([]byte(StringToSign(method, path, cq, body, timestamp, nonce)))
	return hex.EncodeToString(mac.Sum(nil)), nil
}

// VerifySignature 常量时间比较签名（hex）。
func VerifySignature(appSecret, method, path, rawQuery string, body []byte, timestamp, nonce, got string) bool {
	want, err := Sign(appSecret, method, path, rawQuery, body, timestamp, nonce)
	if err != nil {
		return false
	}
	g, err1 := hex.DecodeString(strings.TrimSpace(got))
	w, err2 := hex.DecodeString(want)
	if err1 != nil || err2 != nil {
		return false
	}
	return hmac.Equal(g, w)
}

// ParseAllowEntry 校验白名单条目：单个 IP 或 CIDR。返回规整后的字符串。
func ParseAllowEntry(s string) (string, bool) {
	s = strings.TrimSpace(s)
	if p, err := netip.ParsePrefix(s); err == nil {
		return p.Masked().String(), true
	}
	if a, err := netip.ParseAddr(s); err == nil {
		return a.Unmap().String(), true
	}
	return "", false
}

// IPAllowed 判断 ip 是否在白名单内；白名单为空表示不限制。
func IPAllowed(allow []string, ip string) bool {
	if len(allow) == 0 {
		return true
	}
	addr, err := netip.ParseAddr(strings.TrimSpace(ip))
	if err != nil {
		if h, _, e := net.SplitHostPort(ip); e == nil {
			addr, err = netip.ParseAddr(h)
		}
		if err != nil {
			return false
		}
	}
	addr = addr.Unmap().WithZone("")
	for _, e := range allow {
		if p, err := netip.ParsePrefix(e); err == nil {
			if p.Contains(addr) {
				return true
			}
			continue
		}
		if a, err := netip.ParseAddr(e); err == nil && a.Unmap() == addr {
			return true
		}
	}
	return false
}

func bodyReader(b []byte) *bytes.Reader { return bytes.NewReader(b) }
