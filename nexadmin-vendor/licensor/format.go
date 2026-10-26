package main

// 授权文件格式规范（与 nexadmin 框架靠协议耦合，不靠代码耦合；本项目不 import 框架任何代码）。
//
// 文件是 UTF-8 JSON（缩进 2 个空格，末尾换行），结构为 envelope:
//
//	{
//	  "kid":       "<8 位小写十六进制>",   // 可选；签发公钥指纹
//	  "payload":   "<base64>",           // 载荷 JSON 的原始字节，标准 base64（带 = 填充）
//	  "signature": "<base64>"            // ed25519 对 payload 原始字节（解码后）的签名，64 字节，标准 base64
//	}
//
// 载荷 JSON（License），字段顺序固定如下，由 Go encoding/json 序列化（默认会转义 < > &）:
//
//	plugin      string    插件 id（小写字母/数字/连字符，最长 48）
//	customer    string    客户名
//	issued_at   string    签发时间，RFC3339（UTC，如 2026-01-01T00:00:00Z）
//	expires_at  string    到期时间，RFC3339（UTC）
//	domains     []string  可选；为空时整个字段省略（omitempty）；签发前排序
//	serial      string    可选；授权唯一序列号（小写字母/数字/连字符，6~64 位），吊销清单靠它命中；
//	                      为空时整个字段省略（omitempty），所以没有序列号的旧授权载荷字节与签名都不变，仍可验证
//	                      （这类旧授权不能被单独吊销）。本后台签发时用签发记录 ID 作序列号。
//
// 吊销清单（config/license-revoked.json）使用同一种 envelope 与同一把签发钥，载荷格式见 revoked.go 顶部。
//
// kid = hex(sha256(公钥 32 字节)[:4])，即前 4 字节的小写十六进制。
//
// 签名对象是 payload 的原始字节（不是重新序列化的结构），所以验签没有字段顺序/空白歧义。
// ed25519 签名是确定性的：同一私钥 + 同一 payload 永远得到相同字节，
// 因此用 testdata/golden.lic（测试专用固定 seed 签发）逐字节比对即可保证与框架格式一致。
// 框架侧在 core/plugin/testdata/ 放有同一份样本并校验通过；两边各读各的，互不引用。
//
// 私钥文件: PKCS#8 PEM（"PRIVATE KEY"），内容为 ed25519 私钥。

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"regexp"
	"time"
)

// License 是授权载荷。字段顺序和 json tag 属于格式规范，不要改动。
type License struct {
	Plugin    string   `json:"plugin"`
	Customer  string   `json:"customer"`
	IssuedAt  string   `json:"issued_at"`
	ExpiresAt string   `json:"expires_at"`
	Domains   []string `json:"domains,omitempty"`
	Serial    string   `json:"serial,omitempty"` // 必须放在最后：旧授权不含该字段，载荷字节保持不变
}

type envelope struct {
	KID       string `json:"kid,omitempty"`
	Payload   string `json:"payload"`
	Signature string `json:"signature"`
}

var idRe = regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`)

// validID：小写字母、数字、连字符，不以连字符开头结尾，最长 48。
func validID(id string) bool { return len(id) <= 48 && idRe.MatchString(id) }

// keyID 返回公钥指纹 kid。
func keyID(pub ed25519.PublicKey) string {
	sum := sha256.Sum256(pub)
	return hex.EncodeToString(sum[:4])
}

// signLicense 按规范签发授权文件文本（不含末尾换行）。
func signLicense(priv ed25519.PrivateKey, l License) ([]byte, error) {
	payload, err := json.Marshal(l)
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

// verifyLicense 是签发后的自检：验签、核对插件 id、未到期。通过则返回载荷。
func verifyLicense(raw []byte, pluginID string, now time.Time, pub ed25519.PublicKey) (*License, error) {
	var env envelope
	if err := json.Unmarshal(raw, &env); err != nil {
		return nil, errors.New("授权文件格式错误")
	}
	payload, err1 := base64.StdEncoding.DecodeString(env.Payload)
	sig, err2 := base64.StdEncoding.DecodeString(env.Signature)
	if err1 != nil || err2 != nil || len(sig) != ed25519.SignatureSize {
		return nil, errors.New("授权文件格式错误")
	}
	if env.KID != "" && env.KID != keyID(pub) {
		return nil, errors.New("kid 与公钥不符")
	}
	if !ed25519.Verify(pub, payload, sig) {
		return nil, errors.New("签名校验失败")
	}
	var l License
	if err := json.Unmarshal(payload, &l); err != nil {
		return nil, errors.New("授权载荷格式错误")
	}
	if l.Plugin != pluginID {
		return nil, errors.New("插件 id 不符")
	}
	exp, err := time.Parse(time.RFC3339, l.ExpiresAt)
	if err != nil || !now.Before(exp) {
		return nil, errors.New("授权已到期或到期时间格式错误")
	}
	return &l, nil
}
