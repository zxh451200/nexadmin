package file

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strconv"
	"strings"
	"syscall"
	"time"

	"go.uber.org/zap"
)

// 外链登记（Register 的 URL 导入）出站探测的 SSRF 防护（本模块独立实现，同
// modules/openapi、plugins/aigateway、plugins/cron 的既有约定：各模块一份）。
//
// URL 导入的目标是用户随手贴的任意地址，服务端替它发请求，绝不能变成探内网的跳板
// （云元数据 169.254.169.254、数据库、内部管理端口）。三层防线，与 openapi 一致：
//  1. 请求前静态检查：只允许 http/https、不带账号密码、拒绝 localhost 类主机名和字面内网 IP；
//  2. 发送前解析域名，解析出的"每一个"地址都必须是公网地址；
//  3. 连接时 net.Dialer.Control 对真正要 connect 的地址再校验一次（挡 DNS rebinding）。
//
// 与 webhook 的差别：这里跟随重定向（图片 CDN 常见 http→https 跳转），但每一跳都要
// 重新 dial、Control 都会再校验，跳到内网一样被拦。不使用环境代理——代理会让 Control
// 只看到代理地址，经内网代理转发即绕过。
// 被拒绝：回环、私有、链路本地（含云元数据）、未指定、组播、CGNAT、基准测试、保留/文档网段、IPv4-mapped IPv6。

const urlProbeTimeout = 5 * time.Second

// 默认的 Go UA 会被不少 CDN 的风控拦掉，给个能过防火的通用标识
const probeUserAgent = "Mozilla/5.0 (compatible; NexAdmin-FileProbe/1.0)"

// errProbeBlocked 表示目标地址不在允许范围内。
var errProbeBlocked = errors.New("file: probe target is not allowed (internal/private network)")

var probeBlockedNets = probePrefixes(
	"0.0.0.0/8", "100.64.0.0/10", "192.0.0.0/24", "192.0.2.0/24", "198.18.0.0/15",
	"198.51.100.0/24", "203.0.113.0/24", "240.0.0.0/4", "64:ff9b::/96", "2001:db8::/32",
)

func probePrefixes(list ...string) []netip.Prefix {
	out := make([]netip.Prefix, len(list))
	for i, s := range list {
		out[i] = netip.MustParsePrefix(s)
	}
	return out
}

func probeCheckIP(ip net.IP) error {
	addr, ok := netip.AddrFromSlice(ip)
	if !ok {
		return fmt.Errorf("%w: invalid ip", errProbeBlocked)
	}
	addr = addr.Unmap().WithZone("")
	if addr.IsLoopback() || addr.IsPrivate() || addr.IsLinkLocalUnicast() || addr.IsLinkLocalMulticast() ||
		addr.IsInterfaceLocalMulticast() || addr.IsMulticast() || addr.IsUnspecified() || !addr.IsGlobalUnicast() {
		return fmt.Errorf("%w: %s", errProbeBlocked, addr)
	}
	for _, p := range probeBlockedNets {
		if p.Contains(addr) {
			return fmt.Errorf("%w: %s", errProbeBlocked, addr)
		}
	}
	return nil
}

// probeValidateURL 静态检查（不联网的部分）之上解析域名，要求所有解析结果都是公网地址。
func probeValidateURL(ctx context.Context, raw string) error {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return fmt.Errorf("file: invalid url: %w", err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return errors.New("file: only http and https urls are allowed")
	}
	if u.User != nil {
		return errors.New("file: credentials in url are not allowed")
	}
	host := u.Hostname()
	if host == "" {
		return errors.New("file: url has no host")
	}
	lower := strings.ToLower(strings.TrimSuffix(host, "."))
	if lower == "localhost" || strings.HasSuffix(lower, ".localhost") || strings.HasSuffix(lower, ".internal") || strings.HasSuffix(lower, ".local") {
		return fmt.Errorf("%w: %s", errProbeBlocked, host)
	}
	if ip := net.ParseIP(host); ip != nil {
		return probeCheckIP(ip)
	}
	ips, err := net.DefaultResolver.LookupIP(ctx, "ip", host)
	if err != nil {
		return fmt.Errorf("file: resolve %s: %w", host, err)
	}
	if len(ips) == 0 {
		return fmt.Errorf("file: resolve %s: no addresses", host)
	}
	for _, ip := range ips {
		if err := probeCheckIP(ip); err != nil {
			return err
		}
	}
	return nil
}

// probeDialControl 在 connect 之前校验"实际要连接的地址"。
func probeDialControl(_, address string, _ syscall.RawConn) error {
	host, _, err := net.SplitHostPort(address)
	if err != nil {
		return err
	}
	ip := net.ParseIP(host)
	if ip == nil {
		return fmt.Errorf("%w: %s", errProbeBlocked, host)
	}
	return probeCheckIP(ip)
}

var probeHTTPClient = newProbeClient()

func newProbeClient() *http.Client {
	dialer := &net.Dialer{Timeout: urlProbeTimeout, Control: probeDialControl}
	tr := &http.Transport{
		Proxy:                 nil,
		DialContext:           dialer.DialContext,
		TLSHandshakeTimeout:   urlProbeTimeout,
		ResponseHeaderTimeout: urlProbeTimeout,
		DisableKeepAlives:     true,
	}
	return &http.Client{Transport: tr, Timeout: urlProbeTimeout}
}

// urlMeta 是外链探测的结果：size 0 / mime 空 表示没探到，调用方按「未知」落库。
type urlMeta struct {
	size int64
	mime string
}

// probeExternalURL 探测外链的体积与真实类型。失败（死链、防盗链、内网地址被拦、超时）
// 是常态而非异常：返回零值、记一条 Info，不报错、不阻断登记。
func probeExternalURL(ctx context.Context, raw string) urlMeta {
	ctx, cancel := context.WithTimeout(ctx, urlProbeTimeout)
	defer cancel()
	if err := probeValidateURL(ctx, raw); err != nil {
		zap.L().Info("file: 外链探测目标被拒，体积按未知登记",
			zap.String("url", raw), zap.Error(err))
		return urlMeta{}
	}
	size, mime := probeMeta(ctx, raw)
	if size <= 0 && mime == "" {
		zap.L().Info("file: 外链探测未取到体积与类型，按未知登记", zap.String("url", raw))
	}
	return urlMeta{size: size, mime: mime}
}

// probeMeta 探测体积与真实类型。HEAD 优先（一次来回就够）；不少站点对 HEAD 返回 405
// 或不回长度（chunked），退一步 Range: bytes=0-0 的 GET——支持 Range 的服务在
// Content-Range 里给全长，无视 Range 的服务按 200 全文回、Content-Length 也是全长
// （头一拿到就断开，不会真把文件下载下来）。
func probeMeta(ctx context.Context, raw string) (int64, string) {
	req, err := http.NewRequestWithContext(ctx, http.MethodHead, raw, nil)
	if err != nil {
		return 0, ""
	}
	req.Header.Set("User-Agent", probeUserAgent)
	resp, err := probeHTTPClient.Do(req)
	if err != nil {
		return 0, ""
	}
	size, mime := probeHeaderMeta(resp.Header)
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4<<10))
	resp.Body.Close()
	if size > 0 {
		return size, mime
	}

	req, err = http.NewRequestWithContext(ctx, http.MethodGet, raw, nil)
	if err != nil {
		return 0, mime
	}
	req.Header.Set("User-Agent", probeUserAgent)
	req.Header.Set("Range", "bytes=0-0")
	resp, err = probeHTTPClient.Do(req)
	if err != nil {
		return 0, mime
	}
	defer resp.Body.Close()
	switch {
	case resp.StatusCode == http.StatusPartialContent:
		if total, ok := probeContentRangeTotal(resp.Header.Get("Content-Range")); ok {
			size = total
		}
	case resp.StatusCode == http.StatusOK:
		if cl, err := strconv.ParseInt(resp.Header.Get("Content-Length"), 10, 64); err == nil && cl > 0 {
			size = cl
		}
	}
	if mime == "" {
		_, mime = probeHeaderMeta(resp.Header)
	}
	return size, mime
}

// probeHeaderMeta 读响应头里的体积与真实类型：Content-Length > 0 才算数（chunked 没有
// 这个头）；类型取 Content-Type 主值，octet-stream 等于没说。
func probeHeaderMeta(h http.Header) (int64, string) {
	size := int64(0)
	if cl, err := strconv.ParseInt(h.Get("Content-Length"), 10, 64); err == nil && cl > 0 {
		size = cl
	}
	return size, probeMime(h.Get("Content-Type"))
}

// probeMime 归一化 Content-Type：去参数、小写；没有主类型或 octet-stream 返回空。
func probeMime(ct string) string {
	ct = strings.ToLower(strings.TrimSpace(strings.SplitN(ct, ";", 2)[0]))
	if ct == "" || !strings.Contains(ct, "/") || ct == "application/octet-stream" {
		return ""
	}
	return ct
}

// probeContentRangeTotal 解析 206 响应的 "bytes 0-0/123456" 里的全长。
func probeContentRangeTotal(s string) (int64, bool) {
	i := strings.LastIndexByte(s, '/')
	if i < 0 {
		return 0, false
	}
	total, err := strconv.ParseInt(strings.TrimSpace(s[i+1:]), 10, 64)
	if err != nil || total <= 0 {
		return 0, false
	}
	return total, true
}
