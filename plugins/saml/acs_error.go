package saml

import (
	"errors"
	"html"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"nexadmin/core/i18n"
	"nexadmin/core/response"
)

// wantsHTML 判断这次 ACS 回调是不是浏览器在等页面：IdP 用 POST 把用户送回来时都会带 text/html。
func wantsHTML(r *http.Request) bool {
	accept := strings.ToLower(r.Header.Get("Accept"))
	return strings.Contains(accept, "text/html") || strings.Contains(accept, "application/xhtml+xml")
}

// asResponseError 把任意登录错误收敛成 response.Error，保留业务文案与状态码。
func asResponseError(err error) *response.Error {
	var e *response.Error
	if errors.As(err, &e) {
		return e
	}
	return response.Internal().Wrap(err)
}

// failACS 是 ACS 的唯一失败出口：浏览器拿到可读的双语错误页（原因 + 返回登录），
// 明确要 JSON 的调用方（脚本 / 监控）仍拿到与原来同形的 {code,message}。
func (s *Service) failACS(c *gin.Context, e *response.Error, detail string) {
	if e == nil {
		e = response.Internal()
	}
	ctx := c.Request.Context()
	msg := e.Msg
	if msg == "" {
		msg = i18n.T(ctx, e.Key, e.Args...)
	}
	if detail != "" {
		msg += ": " + detail
	}
	if !wantsHTML(c.Request) {
		response.Fail(c, e.WithMsg(msg))
		return
	}
	lang := i18n.Lang(ctx)
	if lang == "" {
		lang = "zh-CN"
	}
	c.Header("Cache-Control", "no-store")
	c.Header("X-Content-Type-Options", "nosniff")
	c.Data(e.Status, "text/html; charset=utf-8", []byte(renderACSErrorPage(acsErrorView{
		Lang:        lang,
		Title:       i18n.T(ctx, "saml.acs.title"),
		ReasonLabel: i18n.T(ctx, "saml.acs.reason"),
		Reason:      msg,
		Hint:        i18n.T(ctx, "saml.acs.hint"),
		BackLabel:   i18n.T(ctx, "saml.acs.back"),
		BackURL:     baseURL(c) + "/admin/login",
	})))
}

type acsErrorView struct {
	Lang        string
	Title       string
	ReasonLabel string
	Reason      string
	Hint        string
	BackLabel   string
	BackURL     string
}

// renderACSErrorPage 输出自包含的最小页面：不引用前端资源，样式跟随浅/深色系统偏好。
func renderACSErrorPage(v acsErrorView) string {
	esc := html.EscapeString
	return `<!doctype html>
<html lang="` + esc(v.Lang) + `">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>` + esc(v.Title) + `</title>
<style>
:root{color-scheme:light dark}
body{margin:0;min-height:100vh;display:flex;align-items:center;justify-content:center;padding:24px;box-sizing:border-box;background:#f5f6f8;font:15px/1.6 system-ui,-apple-system,"Segoe UI",sans-serif;color:#111827}
main{width:min(100%,520px);background:#fff;border:1px solid #e5e7eb;border-radius:12px;padding:28px 24px;box-shadow:0 8px 24px rgb(15 23 42 / 6%)}
h1{margin:0 0 12px;font-size:18px;line-height:1.4}
dt{margin-top:12px;font-size:13px;font-weight:500;color:#6b7280}
dd{margin:2px 0 0;word-break:break-word}
p.hint{margin:12px 0 0;font-size:13px;color:#6b7280}
a{display:inline-block;margin-top:20px;color:#2563eb;font-weight:500;text-decoration:none}
a:focus-visible{outline:2px solid #2563eb;outline-offset:2px;border-radius:4px}
@media (prefers-color-scheme:dark){body{background:#0b1020;color:#e5e7eb}main{background:#111827;border-color:#1f2937}dt,hint{color:#9ca3af}p.hint{color:#9ca3af}a{color:#93c5fd}}
</style>
</head>
<body>
<main>
<h1>` + esc(v.Title) + `</h1>
<dl>
<dt>` + esc(v.ReasonLabel) + `</dt>
<dd>` + esc(v.Reason) + `</dd>
</dl>
<p class="hint">` + esc(v.Hint) + `</p>
<a href="` + esc(v.BackURL) + `">` + esc(v.BackLabel) + `</a>
</main>
</body>
</html>`
}
