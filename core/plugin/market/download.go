package market

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// GitHubBaseURL 允许测试替换（打到 httptest 服务器）；私有部署可用 NEXADMIN_MARKET_GITHUB_API 覆盖。
var GitHubBaseURL = "https://api.github.com"

func init() {
	if v := strings.TrimSpace(os.Getenv("NEXADMIN_MARKET_GITHUB_API")); v != "" {
		GitHubBaseURL = strings.TrimRight(v, "/")
	}
}

const (
	downloadTimeout = 10 * time.Minute
	maxDownload     = 512 << 20 // 压缩包下载上限
	maxChecksumText = 64 << 10  // 校验文件只有几十字节，超了就是异常
)

// createTempZip 是下载落盘的接缝：默认落到系统临时目录，测试替换后可断言失败路径的清理。
var createTempZip = func() (*os.File, error) { return os.CreateTemp("", "nexadmin-market-*.zip") }

// requireSecureURL 只放行 HTTPS（本机环回地址例外，供测试与本地镜像使用），
// 防止明文直链或重定向把插件源码换成中间人内容。
func requireSecureURL(raw string) error {
	u, err := url.Parse(raw)
	if err != nil {
		return err
	}
	if u.Scheme == "https" {
		return nil
	}
	switch u.Hostname() {
	case "127.0.0.1", "localhost", "::1":
		if u.Scheme == "http" {
			return nil
		}
	}
	return fmt.Errorf("插件包地址必须是 HTTPS: %s", raw)
}

// resolveSourceURL 按条目算出实际下载地址：Entry.URL 直链（OSS/自建镜像）优先；
// 否则用 GitHub zipball（ref 为 tag/分支；空 = 默认分支）。
func resolveSourceURL(e Entry) (string, error) {
	switch {
	case e.URL != "":
		return e.URL, nil
	case e.Repo != "":
		url := GitHubBaseURL + "/repos/" + e.Repo + "/zipball"
		if e.Ref != "" {
			url += "/" + e.Ref
		}
		return url, nil
	default:
		return "", errors.New("目录条目缺少 repo 或 url")
	}
}

// Download 拉取插件包 zip 并落到临时文件，返回路径。
//
// 返回之前会在**解压之前**做 SHA-256 完整性校验（见 verify.go）：校验不通过就删除临时
// zip 并返回错误，调用方拿不到路径，也就不会把来路不明的包喂给 extractZip / go build。
// 私有 GitHub 仓库：Entry.TokenEnv 指定的环境变量里放 GitHub token（PAT），请求带 Authorization。
func Download(ctx context.Context, e Entry) (string, error) {
	src, url, err := downloadSource(ctx, e)
	if err != nil {
		return "", err
	}
	if err := verifyPackage(ctx, src, url, e); err != nil {
		os.Remove(src) //nolint:errcheck // 校验失败的包不许留在磁盘上，解压与安装都不许碰到它
		return "", err
	}
	return src, nil
}

// downloadSource 是纯粹的“取字节”步骤，不做完整性校验，返回（临时 zip 路径, 实际下载地址）。
// 官网打包链路（Package）用它：那是产出校验值的一侧，拿不到也不该拿期望值。
func downloadSource(ctx context.Context, e Entry) (string, string, error) {
	url, err := resolveSourceURL(e)
	if err != nil {
		return "", "", err
	}
	if err := requireSecureURL(url); err != nil {
		return "", "", err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", "", err
	}
	req.Header.Set("User-Agent", "nexadmin-plugin-market")
	req.Header.Set("Accept", "application/vnd.github+json")
	if e.TokenEnv != "" {
		if tok := strings.TrimSpace(os.Getenv(e.TokenEnv)); tok != "" {
			req.Header.Set("Authorization", "Bearer "+tok)
		}
	}
	ctx, cancel := context.WithTimeout(ctx, downloadTimeout)
	defer cancel()
	req = req.WithContext(ctx)

	client := &http.Client{CheckRedirect: func(r *http.Request, via []*http.Request) error {
		if len(via) >= 5 {
			return errors.New("插件包下载跳转次数过多")
		}
		return requireSecureURL(r.URL.String())
	}}
	resp, err := client.Do(req)
	if err != nil {
		return "", "", fmt.Errorf("下载 %s 失败: %w", url, err)
	}
	defer resp.Body.Close() //nolint:errcheck
	switch {
	case resp.StatusCode == http.StatusNotFound:
		if e.URL != "" {
			return "", url, fmt.Errorf("下载 %s 失败: HTTP 404（直链失效？）", url)
		}
		return "", url, fmt.Errorf("仓库 %s 不存在或没有该版本（%d）；私有仓库请在环境变量 %s 配置 token", e.Repo, resp.StatusCode, e.TokenEnv)
	case resp.StatusCode == http.StatusUnauthorized, resp.StatusCode == http.StatusForbidden:
		return "", url, fmt.Errorf("下载源拒绝访问（%d）：私有仓库检查 token，公开仓库可能是限流，稍后重试", resp.StatusCode)
	case resp.StatusCode != http.StatusOK:
		return "", url, fmt.Errorf("下载 %s 失败: HTTP %d", url, resp.StatusCode)
	}

	tmp, err := createTempZip()
	if err != nil {
		return "", url, err
	}
	if _, err := io.Copy(tmp, io.LimitReader(resp.Body, maxDownload+1)); err != nil {
		tmp.Close()           //nolint:errcheck
		os.Remove(tmp.Name()) //nolint:errcheck
		return "", url, fmt.Errorf("下载中断: %w", err)
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmp.Name()) //nolint:errcheck
		return "", url, err
	}
	if fi, err := os.Stat(tmp.Name()); err == nil && fi.Size() > maxDownload {
		os.Remove(tmp.Name()) //nolint:errcheck
		return "", url, errors.New("插件包过大（超过 512MB）")
	}
	return filepath.ToSlash(tmp.Name()), url, nil
}

// fetchChecksumText 拉取校验文件（小文本）：与包下载同一套 HTTPS 与跳转约束，
// 404 用 errChecksumNotFound 表达，调用方据此区分“没有侧文件”和“取侧文件失败”。
func fetchChecksumText(ctx context.Context, raw string) (string, error) {
	if err := requireSecureURL(raw); err != nil {
		return "", err
	}
	ctx, cancel := context.WithTimeout(ctx, downloadTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, raw, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", "nexadmin-plugin-market")
	client := &http.Client{CheckRedirect: func(r *http.Request, via []*http.Request) error {
		if len(via) >= 5 {
			return errors.New("校验文件下载跳转次数过多")
		}
		return requireSecureURL(r.URL.String())
	}}
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("下载 %s 失败: %w", raw, err)
	}
	defer resp.Body.Close() //nolint:errcheck
	switch resp.StatusCode {
	case http.StatusNotFound:
		return "", errChecksumNotFound
	case http.StatusOK:
	default:
		return "", fmt.Errorf("下载 %s 失败: HTTP %d", raw, resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxChecksumText+1))
	if err != nil {
		return "", fmt.Errorf("下载 %s 中断: %w", raw, err)
	}
	if len(body) > maxChecksumText {
		return "", fmt.Errorf("校验文件过大: %s", raw)
	}
	return string(body), nil
}
