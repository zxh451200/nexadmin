package media

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// streamProvider 是视频托管的最小接口。目前只有 Cloudflare Stream；
// 换 Bunny/Mux 时新增一个实现即可，测试用假的 HTTP 上游。
type streamProvider interface {
	// CreateDirectUpload 申请一次性直传地址（浏览器直传厂商，不经过我们的存储）。
	CreateDirectUpload(ctx context.Context, cfg VideoConfig, name string) (uploadURL, videoID string, err error)
	// PlaybackURL 由 videoId 拼出可嵌入的播放地址（不信客户端传来的 URL）。
	PlaybackURL(cfg VideoConfig, videoID string) string
	// DownloadURL 是可直接放进 <video src> 的 MP4 直链（厂商侧需开启 MP4 下载）。
	DownloadURL(cfg VideoConfig, videoID string) string
	Name() string
}

type cloudflareStream struct {
	hc      *http.Client
	baseURL string // 测试里指向 httptest
}

func newCloudflareStream() *cloudflareStream {
	return &cloudflareStream{
		hc:      &http.Client{Timeout: 20 * time.Second},
		baseURL: "https://api.cloudflare.com/client/v4",
	}
}

func (c *cloudflareStream) Name() string { return "cloudflare" }

type cfDirectUpload struct {
	Success bool `json:"success"`
	Errors  []struct {
		Message string `json:"message"`
	} `json:"errors"`
	Result struct {
		UploadURL string `json:"uploadURL"`
		UID       string `json:"uid"`
	} `json:"result"`
}

func (c *cloudflareStream) CreateDirectUpload(ctx context.Context, cfg VideoConfig, name string) (string, string, error) {
	if cfg.AccountID == "" || cfg.Token == "" {
		return "", "", errors.New("尚未配置 Cloudflare Stream 的 Account ID 或 API Token")
	}
	body, _ := json.Marshal(map[string]any{
		"maxDurationSeconds": 3600,
		"meta":               map[string]string{"name": name},
	})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		fmt.Sprintf("%s/accounts/%s/stream/direct_upload", c.baseURL, cfg.AccountID), bytes.NewReader(body))
	if err != nil {
		return "", "", err
	}
	req.Header.Set("Authorization", "Bearer "+cfg.Token)
	req.Header.Set("Content-Type", "application/json")
	res, err := c.hc.Do(req)
	if err != nil {
		return "", "", err
	}
	defer res.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(res.Body, 1<<20))
	var out cfDirectUpload
	if err := json.Unmarshal(raw, &out); err != nil {
		return "", "", fmt.Errorf("Cloudflare Stream 返回无法解析（HTTP %d）", res.StatusCode)
	}
	if !out.Success || out.Result.UploadURL == "" || out.Result.UID == "" {
		msg := fmt.Sprintf("HTTP %d", res.StatusCode)
		if len(out.Errors) > 0 && out.Errors[0].Message != "" {
			msg = out.Errors[0].Message
		}
		return "", "", errors.New("Cloudflare Stream: " + msg)
	}
	return out.Result.UploadURL, out.Result.UID, nil
}

func (c *cloudflareStream) PlaybackURL(cfg VideoConfig, videoID string) string {
	return fmt.Sprintf("https://customer-%s.cloudflarestream.com/%s/iframe", strings.TrimSpace(cfg.Subdomain), videoID)
}

// DownloadURL 需要账号在 Cloudflare 控制台开启「允许 MP4 下载」，否则该地址 404。
func (c *cloudflareStream) DownloadURL(cfg VideoConfig, videoID string) string {
	if strings.TrimSpace(cfg.Subdomain) == "" {
		return ""
	}
	return fmt.Sprintf("https://customer-%s.cloudflarestream.com/%s/downloads/default.mp4", strings.TrimSpace(cfg.Subdomain), videoID)
}
