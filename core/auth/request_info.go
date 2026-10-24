package auth

import (
	"context"
	"net"
	"net/http"
)

// RequestInfo 是登录 / 刷新请求的网络来源信息，由 auth.Handler 放进 ctx，供前置检查和刷新检查（插件）读取。
//   - IP     是 c.ClientIP()（受 server.trusted_proxies 控制）。
//   - Peer   是 TCP 对端地址（RemoteAddr 的主机部分），转发头无法伪造。
//   - Proxied 为真表示请求带有任何常见转发头（客户端自带也可能，所以只能“更严”）。
type RequestInfo struct {
	IP      string
	Peer    string
	Proxied bool
}

type requestInfoKey struct{}

// WithRequestInfo 把来源信息放进 ctx。
func WithRequestInfo(ctx context.Context, ri RequestInfo) context.Context {
	return context.WithValue(ctx, requestInfoKey{}, ri)
}

// RequestInfoFrom 读取来源信息；不是经 auth.Handler 进来的调用（没有 HTTP 请求）返回 ok=false。
func RequestInfoFrom(ctx context.Context) (RequestInfo, bool) {
	ri, ok := ctx.Value(requestInfoKey{}).(RequestInfo)
	return ri, ok
}

// NewRequestInfo 从 HTTP 请求提取来源信息；clientIP 是已解析好的 c.ClientIP()。
func NewRequestInfo(r *http.Request, clientIP string) RequestInfo {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	return RequestInfo{IP: clientIP, Peer: host, Proxied: viaProxy(r.Header)}
}

// DirectLoopback 报告请求是否由本机直接发起：TCP 对端是回环地址，且没有任何转发头。
// 同机反向代理（nginx 在 127.0.0.1）转发的外部请求总会带转发头，所以不算；
// 与 core/setup 的“本机”判定同一口径。
func (ri RequestInfo) DirectLoopback() bool {
	if ri.Proxied {
		return false
	}
	ip := net.ParseIP(ri.Peer)
	return ip != nil && ip.IsLoopback()
}

func viaProxy(h http.Header) bool {
	for _, k := range []string{"X-Forwarded-For", "X-Real-IP", "Forwarded", "X-Forwarded-Host"} {
		if h.Get(k) != "" {
			return true
		}
	}
	return false
}
