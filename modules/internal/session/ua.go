package session

import "strings"

// ParseUA 从 User-Agent 粗略识别设备类型、浏览器和系统（不引第三方库，够展示用）。
func ParseUA(ua string) (device, browser, os string) {
	l := strings.ToLower(ua)
	switch {
	case strings.Contains(l, "ipad") || strings.Contains(l, "tablet"):
		device = "tablet"
	case strings.Contains(l, "mobile") || strings.Contains(l, "iphone") || strings.Contains(l, "android"):
		device = "mobile"
	case ua == "":
		device = "other"
	default:
		device = "web"
	}

	switch {
	case strings.Contains(ua, "Edg/"):
		browser = "Edge " + major(ua, "Edg/")
	case strings.Contains(ua, "OPR/"):
		browser = "Opera " + major(ua, "OPR/")
	case strings.Contains(ua, "Firefox/"):
		browser = "Firefox " + major(ua, "Firefox/")
	case strings.Contains(ua, "Chrome/"):
		browser = "Chrome " + major(ua, "Chrome/")
	case strings.Contains(ua, "Safari/") && strings.Contains(ua, "Version/"):
		browser = "Safari " + major(ua, "Version/")
	case strings.HasPrefix(ua, "curl/"):
		browser = "curl " + major(ua, "curl/")
	}
	browser = strings.TrimSpace(browser)

	switch {
	case strings.Contains(ua, "iPhone"):
		os = "iOS"
	case strings.Contains(ua, "iPad"):
		os = "iPadOS"
	case strings.Contains(ua, "Android"):
		os = "Android"
	case strings.Contains(ua, "Windows"):
		os = "Windows"
	case strings.Contains(ua, "Mac OS X") || strings.Contains(ua, "Macintosh"):
		os = "macOS"
	case strings.Contains(ua, "Linux") || strings.Contains(ua, "X11"):
		os = "Linux"
	}
	return device, browser, os
}

// major 取 token 后面的主版本号（连续数字）。
func major(ua, token string) string {
	i := strings.Index(ua, token)
	if i < 0 {
		return ""
	}
	rest := ua[i+len(token):]
	n := 0
	for n < len(rest) && rest[n] >= '0' && rest[n] <= '9' {
		n++
	}
	return rest[:n]
}
