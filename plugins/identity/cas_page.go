package identity

import (
	"encoding/json"
	"net/http"

	"github.com/gin-gonic/gin"

	"nexadmin/core/response"
)

// 前端登录态存放键，与 web/src/framework/request/http.ts 的 tokenStore 一致。
const (
	accessKey  = "nex-access-token"
	refreshKey = "nex-refresh-token"
)

func respondErr(c *gin.Context, err error) {
	response.Fail(c, err)
}

func pageHeaders(c *gin.Context, csp string) {
	h := c.Writer.Header()
	h.Set("Content-Type", "text/html; charset=utf-8")
	h.Set("Cache-Control", "no-store")
	h.Set("Pragma", "no-cache")
	h.Set("Referrer-Policy", "no-referrer")
	h.Set("X-Content-Type-Options", "nosniff")
	h.Set("Content-Security-Policy", csp)
}

// writeErrorPage 返回固定文案的失败页：不回显任何请求参数，也不暴露失败原因（原因只进服务端日志）。
func writeErrorPage(c *gin.Context, status int) {
	pageHeaders(c, "default-src 'none'; style-src 'unsafe-inline'")
	c.Status(status)
	_, _ = c.Writer.WriteString(`<!doctype html><html lang="en"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1">` +
		`<title>Sign-in failed</title></head><body style="font-family:sans-serif;max-width:32rem;margin:4rem auto;padding:0 1rem">` +
		`<h1>Sign-in failed</h1><p>Single sign-on could not be completed. Please try again or sign in with your account password.</p>` +
		`<p><a href="/login">Back to sign-in</a></p></body></html>`)
	c.Abort()
}

// writeSessionPage 把登录态交给前端：令牌只出现在这个 no-store 页面的内联脚本里（带 nonce 的 CSP，不进 URL / 日志 / Referer），
// 脚本写入 localStorage 后跳转到站内 next。
func writeSessionPage(c *gin.Context, payload any, next string) {
	b, err := json.Marshal(payload) // json.Marshal 默认转义 < > &，不能跳出 <script>
	if err != nil {
		writeErrorPage(c, http.StatusInternalServerError)
		return
	}
	nb, _ := json.Marshal(safeNext(next))
	nonce := newNonce()
	pageHeaders(c, "default-src 'none'; script-src 'nonce-"+nonce+"'")
	c.Status(http.StatusOK)
	_, _ = c.Writer.WriteString(`<!doctype html><html lang="en"><head><meta charset="utf-8"><title>Signing in</title></head><body>` +
		`<script nonce="` + nonce + `">(function(){var r=` + string(b) + `;` +
		`localStorage.setItem(` + quoteJS(accessKey) + `,r.accessToken);` +
		`localStorage.setItem(` + quoteJS(refreshKey) + `,r.refreshToken);` +
		`location.replace(` + string(nb) + `);})();</script><noscript>JavaScript is required to finish signing in.</noscript></body></html>`)
}

func quoteJS(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}

// 浏览器只暂存一次性交接 code；challenge 与令牌均不进入 URL。
func writeMFAHandoffPage(c *gin.Context, code string) {
	nonce := newNonce()
	pageHeaders(c, "default-src 'none'; script-src 'nonce-"+nonce+"'")
	c.Status(http.StatusOK)
	_, _ = c.Writer.WriteString(`<!doctype html><html lang="en"><head><meta charset="utf-8"><title>Continue sign-in</title></head><body>` +
		`<script nonce="` + nonce + `">(function(){try{history.replaceState(null,'','/login');` +
		`sessionStorage.setItem('nex-identity-cas-mfa',` + quoteJS(code) + `);location.replace('/login');` +
		`}catch(e){location.replace('/login');}})();</script><noscript>JavaScript is required to finish signing in.</noscript></body></html>`)
}
