package core

import (
	"bytes"
	"context"
	"encoding/json"
	"io/fs"
	"net/http"
	"net/url"
	"path"
	"strings"
	"sync/atomic"
	"time"
	"unicode/utf8"

	"github.com/gin-gonic/gin"
)

// WithStatic 提供前端构建产物（根目录含 index.html 的文件系统，通常来自 web.Dist()）。
// 传 nil 等同于不设置：未命中的请求仍返回 JSON 404（开发模式用 vite dev server）。
func WithStatic(fsys fs.FS) Option { return func(a *App) { a.static = fsys } }

const (
	cacheImmutable = "public, max-age=31536000, immutable"
	cacheNoCache   = "no-cache"
	cacheShort     = "public, max-age=3600"
)

// noCacheFiles 是必须每次向服务端校验的根目录文件：
// sw.js 若被长缓存，浏览器永远拿不到新版 Service Worker（站点无法更新）；manifest 与离线页同理需及时生效。
// sw.js 位于站点根，默认作用域即 "/"，无需 Service-Worker-Allowed 头。
var noCacheFiles = map[string]bool{"sw.js": true, "manifest.webmanifest": true, "offline.html": true}

// AdminPrefix 是后台 SPA 的固定路径前缀。后台独占该前缀，官网接管其余路径：
// 只有 /admin 与 /admin/* 会回退到 index.html，未注册的其它路径交给官网 404。
const AdminPrefix = "/admin"

// IsAdminPath 判断请求是否落在后台前缀下（/admin 本身或 /admin/...）。
func IsAdminPath(p string) bool {
	return p == AdminPrefix || strings.HasPrefix(p, AdminPrefix+"/")
}

// staticHandler 返回 NoRoute 用的静态资源处理器：
//   - /api/ 下与非 GET/HEAD 请求不处理（返回 false），交给原有 JSON 404 / 503；
//   - /healthz 及其子路径不回退（子路径一律 404，/healthz 本身是注册路由不会走到这里）；
//   - 命中文件直接返回；带扩展名但不存在的资源（如 /assets/x.js）返回 404，避免把 HTML 当 JS 缓存；
//   - 只有后台路径（/admin、/admin/*）回退 index.html，其余未知路径返回 false 由上层决定（官网 404）。
//
// 缓存：assets/ 下是 vite 带 hash 的产物 → immutable；index.html 与其他文件 → no-cache / 短缓存。
func staticHandler(fsys fs.FS) func(c *gin.Context) bool { return staticHandlerWith(fsys, nil) }

// SiteInfo 是动态 manifest 用到的公开站点信息（设置页「站点」分区里匿名可见的部分）。
type SiteInfo struct{ Name, LogoURL string }

var siteSource atomic.Pointer[func(ctx context.Context) (SiteInfo, error)]

// siteNotFound 由官网插件登记：核心把未注册、非 /admin、非 /api 的页面请求交给它渲染 404。
var siteNotFound atomic.Pointer[func(*gin.Context) bool]

// SetSiteNotFoundHandler 登记官网 404 渲染器；fn 返回 true 表示已写出响应。
// 传 nil 取消。后台只占 AdminPrefix，因此其余未知路径归官网。
func SetSiteNotFoundHandler(fn func(*gin.Context) bool) {
	if fn == nil {
		siteNotFound.Store(nil)
		return
	}
	siteNotFound.Store(&fn)
}

// SetPublicSiteSource 由设置模块登记公开站点信息来源（core 不 import 模块）；传 nil 取消。
// ctx 已带请求所属租户。没人登记（或查询失败）时 /manifest.webmanifest 回落到 web/public 里的静态文件。
func SetPublicSiteSource(fn func(ctx context.Context) (SiteInfo, error)) {
	if fn == nil {
		siteSource.Store(nil)
		return
	}
	siteSource.Store(&fn)
}

// maxShortName 是 short_name 的上限（桌面图标下的文字放不下更长的名字）。
const maxShortName = 12

// bitmapMIME 按扩展名判断 logoUrl 是否为位图并返回 MIME；SVG、无扩展名或其它地址返回 ""。
// 只接受 http(s) 绝对地址或站内以 "/" 开头的路径（不接受 "//host" 与 data:/javascript: 等）。
func bitmapMIME(logo string) string {
	logo = strings.TrimSpace(logo)
	u, err := url.Parse(logo)
	if err != nil || strings.HasPrefix(logo, "//") {
		return ""
	}
	switch {
	case u.Scheme == "" && u.Host == "" && strings.HasPrefix(logo, "/"):
	case (u.Scheme == "http" || u.Scheme == "https") && u.Host != "":
	default:
		return ""
	}
	switch strings.ToLower(path.Ext(u.Path)) {
	case ".png":
		return "image/png"
	case ".jpg", ".jpeg":
		return "image/jpeg"
	case ".webp":
		return "image/webp"
	case ".gif":
		return "image/gif"
	}
	return ""
}

// buildManifest 在静态 manifest（base，来自 web/public/manifest.webmanifest）基础上套用站点信息：
// name / short_name 用站点名；图标只留静态 PNG（没有 PNG 时保持原样），logoUrl 是位图时追加为额外图标项。
func buildManifest(base []byte, site SiteInfo) ([]byte, error) {
	m := map[string]any{}
	if err := json.Unmarshal(base, &m); err != nil {
		return nil, err
	}
	if name := strings.TrimSpace(site.Name); name != "" {
		if utf8.RuneCountInString(name) > 100 {
			name = string([]rune(name)[:100])
		}
		m["name"] = name
		short := name
		if utf8.RuneCountInString(short) > maxShortName {
			short = string([]rune(short)[:maxShortName])
		}
		m["short_name"] = short
	}
	icons, _ := m["icons"].([]any)
	var pngs []any
	for _, it := range icons {
		if im, ok := it.(map[string]any); ok && im["type"] == "image/png" {
			pngs = append(pngs, it)
		}
	}
	if len(pngs) > 0 {
		icons = pngs
	}
	if mime := bitmapMIME(site.LogoURL); mime != "" {
		icons = append(icons, map[string]any{"src": strings.TrimSpace(site.LogoURL), "type": mime, "purpose": "any"})
	}
	if icons != nil {
		m["icons"] = icons
	}
	return json.Marshal(m)
}

// staticHandlerWith 同 staticHandler；site 非 nil 时 /manifest.webmanifest 优先走动态生成
// （no-cache、application/manifest+json），site 返回 false 时回落静态文件。
func staticHandlerWith(fsys fs.FS, site func(c *gin.Context) (SiteInfo, bool)) func(c *gin.Context) bool {
	if fsys == nil {
		return func(*gin.Context) bool { return false }
	}
	index, err := fs.ReadFile(fsys, "index.html")
	if err != nil {
		return func(*gin.Context) bool { return false }
	}
	serveIndex := func(c *gin.Context) {
		c.Header("Cache-Control", cacheNoCache)
		c.Data(http.StatusOK, "text/html; charset=utf-8", index)
	}
	return func(c *gin.Context) bool {
		m := c.Request.Method
		if m != http.MethodGet && m != http.MethodHead {
			return false
		}
		p := c.Request.URL.Path
		if p == "/api" || strings.HasPrefix(p, "/api/") || p == "/healthz" || strings.HasPrefix(p, "/healthz/") {
			return false
		}
		name := strings.TrimPrefix(path.Clean("/"+p), "/")
		if name == "" || name == "index.html" {
			if IsAdminPath(p) {
				serveIndex(c)
				return true
			}
			return false
		}
		if name == "manifest.webmanifest" && site != nil {
			if info, ok := site(c); ok {
				if base, err := fs.ReadFile(fsys, name); err == nil {
					if raw, err := buildManifest(base, info); err == nil {
						c.Header("Content-Type", "application/manifest+json")
						c.Header("Cache-Control", cacheNoCache)
						http.ServeContent(c.Writer, c.Request, name, time.Time{}, bytes.NewReader(raw))
						return true
					}
				}
			}
		}
		if st, err := fs.Stat(fsys, name); err == nil && !st.IsDir() {
			raw, err := fs.ReadFile(fsys, name)
			if err != nil {
				return false
			}
			cc := cacheShort
			if strings.HasPrefix(name, "assets/") {
				cc = cacheImmutable
			} else if noCacheFiles[name] {
				cc = cacheNoCache
			}
			if name == "manifest.webmanifest" { // 标准库 mime 表不含该扩展名，显式声明避免被嗅探成 text/plain
				c.Header("Content-Type", "application/manifest+json")
			}
			c.Header("Cache-Control", cc)
			http.ServeContent(c.Writer, c.Request, name, time.Time{}, bytes.NewReader(raw))
			return true
		}
		if strings.HasPrefix(name, "assets/") || noCacheFiles[name] ||
			name == "favicon.ico" || name == "robots.txt" || name == "manifest.webmanifest" { // 缺失的静态资源
			return false
		}
		if IsAdminPath(p) {
			serveIndex(c)
			return true
		}
		return false
	}
}
