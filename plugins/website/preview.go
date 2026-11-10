package website

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strconv"
	"strings"
	"time"

	"nexadmin/core"
	"nexadmin/core/response"
)

// 草稿预览：未发布的内容页在公开站一律 404，管理端可签发一个短时效预览链接
// （/p/:slug?preview=<token>），持有者能像已发布一样看到页面真实版式。
// 签名密钥复用框架的 JWT 签名密钥（Auth.JWTSecret：自动生成并落盘 data/jwt.secret，
// 多实例共享）——为预览单独持久化一把密钥不值得一张三库迁移。
// 令牌形如 "<过期unix秒>.<hex(hmac)>"，MAC 覆盖 slug 与过期时间，跨 slug 不可复用。

// previewTTL 是预览链接的有效期：隔天发同事、隔天回看都还在，但不会永久挂着。
const previewTTL = 24 * time.Hour

const previewScope = "website-page-preview"

// previewScopePost 与页面预览隔离：页面和新闻的 slug 是两张表各自唯一，
// 一个 slug 在两边可以同时存在，作用域分开后令牌不能跨内容类型复用。
const previewScopePost = "website-post-preview"

// previewScopeProduct 与页面/新闻预览隔离：三类内容的 slug 三张表各自唯一，同理分域。
const previewScopeProduct = "website-product-preview"

// previewKeyOf 取预览令牌的签名密钥：复用框架 JWT 签名密钥。测试手工构造 Deps
// 没有 Config 时返回 nil，预览功能随之关闭（签发报错、校验一律拒绝）。
func previewKeyOf(d *core.Deps) []byte {
	if d == nil || d.Config == nil {
		return nil
	}
	return []byte(d.Config.Auth.JWTSecret)
}

func previewMAC(key []byte, scope, slug string, exp int64) string {
	m := hmac.New(sha256.New, key)
	fmt.Fprintf(m, "%s|%s|%d", scope, slug, exp)
	return hex.EncodeToString(m.Sum(nil))
}

func signPreview(key []byte, scope, slug string, now time.Time) (string, error) {
	if len(key) == 0 {
		return "", response.BadRequest("website.preview_unavailable")
	}
	exp := now.Add(previewTTL).Unix()
	return fmt.Sprintf("%d.%s", exp, previewMAC(key, scope, slug, exp)), nil
}

func verifyPreview(key []byte, scope, slug, token string, now time.Time) bool {
	if len(key) == 0 {
		return false
	}
	expStr, sig, ok := strings.Cut(token, ".")
	if !ok {
		return false
	}
	exp, err := strconv.ParseInt(expStr, 10, 64)
	if err != nil || now.Unix() >= exp {
		return false
	}
	return hmac.Equal([]byte(sig), []byte(previewMAC(key, scope, slug, exp)))
}

// signPagePreview 签发页面 slug 的预览令牌；key 为空（未配置签名密钥）时返回业务错误。
func signPagePreview(key []byte, slug string, now time.Time) (string, error) {
	return signPreview(key, previewScope, slug, now)
}

// verifyPagePreview 校验令牌是否属于该页面 slug 且未过期；任何异常都算无效。
func verifyPagePreview(key []byte, slug, token string, now time.Time) bool {
	return verifyPreview(key, previewScope, slug, token, now)
}

// signPostPreview / verifyPostPreview 是新闻详情的预览令牌，语义同页面版。
func signPostPreview(key []byte, slug string, now time.Time) (string, error) {
	return signPreview(key, previewScopePost, slug, now)
}

func verifyPostPreview(key []byte, slug, token string, now time.Time) bool {
	return verifyPreview(key, previewScopePost, slug, token, now)
}

// signProductPreview / verifyProductPreview 是产品详情的预览令牌，语义同页面版。
func signProductPreview(key []byte, slug string, now time.Time) (string, error) {
	return signPreview(key, previewScopeProduct, slug, now)
}

func verifyProductPreview(key []byte, slug, token string, now time.Time) bool {
	return verifyPreview(key, previewScopeProduct, slug, token, now)
}
