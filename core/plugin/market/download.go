package market

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
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
)

// Download 拉取插件包 zip：Entry.URL 直链（OSS/自建镜像）优先；否则用 GitHub zipball
// （ref 为 tag/分支；空 = 默认分支）。落到临时 zip 文件并返回路径。
// 私有 GitHub 仓库：Entry.TokenEnv 指定的环境变量里放 GitHub token（PAT），请求带 Authorization。
func Download(ctx context.Context, e Entry) (string, error) {
	var url string
	switch {
	case e.URL != "":
		url = e.URL
	case e.Repo != "":
		url = GitHubBaseURL + "/repos/" + e.Repo + "/zipball"
		if e.Ref != "" {
			url += "/" + e.Ref
		}
	default:
		return "", errors.New("目录条目缺少 repo 或 url")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", err
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

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("下载 %s 失败: %w", url, err)
	}
	defer resp.Body.Close() //nolint:errcheck
	switch {
	case resp.StatusCode == http.StatusNotFound:
		if e.URL != "" {
			return "", fmt.Errorf("下载 %s 失败: HTTP 404（直链失效？）", url)
		}
		return "", fmt.Errorf("仓库 %s 不存在或没有该版本（%d）；私有仓库请在环境变量 %s 配置 token", e.Repo, resp.StatusCode, e.TokenEnv)
	case resp.StatusCode == http.StatusUnauthorized, resp.StatusCode == http.StatusForbidden:
		return "", fmt.Errorf("下载源拒绝访问（%d）：私有仓库检查 token，公开仓库可能是限流，稍后重试", resp.StatusCode)
	case resp.StatusCode != http.StatusOK:
		return "", fmt.Errorf("下载 %s 失败: HTTP %d", url, resp.StatusCode)
	}

	tmp, err := os.CreateTemp("", "nexadmin-market-*.zip")
	if err != nil {
		return "", err
	}
	if _, err := io.Copy(tmp, io.LimitReader(resp.Body, maxDownload+1)); err != nil {
		tmp.Close()           //nolint:errcheck
		os.Remove(tmp.Name()) //nolint:errcheck
		return "", fmt.Errorf("下载中断: %w", err)
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmp.Name()) //nolint:errcheck
		return "", err
	}
	if fi, err := os.Stat(tmp.Name()); err == nil && fi.Size() > maxDownload {
		os.Remove(tmp.Name()) //nolint:errcheck
		return "", errors.New("插件包过大（超过 512MB）")
	}
	return filepath.ToSlash(tmp.Name()), nil
}
