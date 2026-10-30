package media

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"
)

// streamProvider 是视频托管的最小接口：创建一条待上传的视频，返回客户端直传所需参数。
// 新增厂商只需实现这三个方法，并在 providerFor 里登记。
type streamProvider interface {
	Name() string
	// SupportsUpload 为 false 表示该厂商只支持「登记已有视频」（直传需要厂商客户端 SDK，暂未内置）。
	SupportsUpload() bool
	// CreateUpload 返回：直传地址、附加请求头（如 Bunny 的签名）、厂商视频 id。
	CreateUpload(ctx context.Context, cfg VideoConfig, name string) (uploadURL string, headers map[string]string, videoID string, err error)
	PlaybackURL(cfg VideoConfig, videoID string) string
	DownloadURL(cfg VideoConfig, videoID string) string
}

func providerFor(name string) (streamProvider, error) {
	switch name {
	case ProviderCloudflare:
		return newCloudflareStream(), nil
	case ProviderBunny:
		return newBunnyStream(), nil
	case ProviderAliyun:
		return &registerOnly{name: ProviderAliyun, cdn: "%s/%s/index.m3u8"}, nil
	case ProviderTencent:
		return &registerOnly{name: ProviderTencent, cdn: "%s/%s.m3u8"}, nil
	}
	return nil, errors.New("未知的视频托管厂商: " + name)
}

// ---------- Cloudflare Stream ----------

type cloudflareStream struct {
	hc      *http.Client
	baseURL string // 测试里指向 httptest
}

func newCloudflareStream() *cloudflareStream {
	return &cloudflareStream{hc: &http.Client{Timeout: 20 * time.Second}, baseURL: "https://api.cloudflare.com/client/v4"}
}

func (c *cloudflareStream) Name() string { return ProviderCloudflare }

func (c *cloudflareStream) SupportsUpload() bool { return true }

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

func (c *cloudflareStream) CreateUpload(ctx context.Context, cfg VideoConfig, name string) (string, map[string]string, string, error) {
	if cfg.KeyA == "" || cfg.KeyB == "" {
		return "", nil, "", errors.New("尚未配置 Cloudflare 的 Account ID 或 API Token")
	}
	body, _ := json.Marshal(map[string]any{
		"maxDurationSeconds": 3600,
		"meta":               map[string]string{"name": name},
	})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		fmt.Sprintf("%s/accounts/%s/stream/direct_upload", c.baseURL, cfg.KeyA), bytes.NewReader(body))
	if err != nil {
		return "", nil, "", err
	}
	req.Header.Set("Authorization", "Bearer "+cfg.KeyB)
	req.Header.Set("Content-Type", "application/json")
	res, err := c.hc.Do(req)
	if err != nil {
		return "", nil, "", err
	}
	defer res.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(res.Body, 1<<20))
	var out cfDirectUpload
	if err := json.Unmarshal(raw, &out); err != nil {
		return "", nil, "", fmt.Errorf("Cloudflare Stream 返回无法解析（HTTP %d）", res.StatusCode)
	}
	if !out.Success || out.Result.UploadURL == "" || out.Result.UID == "" {
		msg := fmt.Sprintf("HTTP %d", res.StatusCode)
		if len(out.Errors) > 0 && out.Errors[0].Message != "" {
			msg = out.Errors[0].Message
		}
		return "", nil, "", errors.New("Cloudflare Stream: " + msg)
	}
	return out.Result.UploadURL, nil, out.Result.UID, nil
}

func (c *cloudflareStream) PlaybackURL(cfg VideoConfig, videoID string) string {
	return fmt.Sprintf("https://customer-%s.cloudflarestream.com/%s/iframe", strings.TrimSpace(cfg.KeyC), videoID)
}

// DownloadURL 需要账号开启「允许 MP4 下载」，否则该地址 404。
func (c *cloudflareStream) DownloadURL(cfg VideoConfig, videoID string) string {
	if strings.TrimSpace(cfg.KeyC) == "" {
		return ""
	}
	return fmt.Sprintf("https://customer-%s.cloudflarestream.com/%s/downloads/default.mp4", strings.TrimSpace(cfg.KeyC), videoID)
}

// ---------- Bunny Stream ----------

type bunnyStream struct {
	hc      *http.Client
	baseURL string // 默认 https://video.bunnycdn.com
	cdnBase string // 测试里替换；实际用 pull zone
	now     func() time.Time
}

func newBunnyStream() *bunnyStream {
	return &bunnyStream{
		hc:      &http.Client{Timeout: 20 * time.Second},
		baseURL: "https://video.bunnycdn.com",
		now:     time.Now,
	}
}

func (c *bunnyStream) Name() string { return ProviderBunny }

func (c *bunnyStream) SupportsUpload() bool { return true }

// CreateUpload：先在库里建一条视频拿 guid，再用「签名 + tus」让浏览器直传。
// 签名算法：sha256(libraryId + apiKey + expire + videoId)，十六进制小写。
func (c *bunnyStream) CreateUpload(ctx context.Context, cfg VideoConfig, name string) (string, map[string]string, string, error) {
	if cfg.KeyA == "" || cfg.KeyB == "" || cfg.KeyC == "" {
		return "", nil, "", errors.New("尚未配置 Bunny 的 Library ID / API Key / Pull Zone")
	}
	body, _ := json.Marshal(map[string]string{"title": name})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		fmt.Sprintf("%s/library/%s/videos", strings.TrimSuffix(c.baseURL, "/"), cfg.KeyA), bytes.NewReader(body))
	if err != nil {
		return "", nil, "", err
	}
	req.Header.Set("AccessKey", cfg.KeyB)
	req.Header.Set("Content-Type", "application/json")
	res, err := c.hc.Do(req)
	if err != nil {
		return "", nil, "", err
	}
	defer res.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(res.Body, 1<<20))
	var out struct {
		GUID    string `json:"guid"`
		Message string `json:"message"`
		Success bool   `json:"success"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return "", nil, "", fmt.Errorf("Bunny 返回无法解析（HTTP %d）", res.StatusCode)
	}
	if res.StatusCode >= 300 || out.GUID == "" {
		msg := out.Message
		if msg == "" {
			msg = fmt.Sprintf("HTTP %d", res.StatusCode)
		}
		return "", nil, "", errors.New("Bunny: " + msg)
	}
	expire := c.now().Add(2 * time.Hour).Unix()
	sum := sha256.Sum256([]byte(cfg.KeyA + cfg.KeyB + strconv.FormatInt(expire, 10) + out.GUID))
	headers := map[string]string{
		"AuthorizationSignature": hex.EncodeToString(sum[:]),
		"AuthorizationExpire":    strconv.FormatInt(expire, 10),
		"VideoId":                out.GUID,
		"LibraryId":              cfg.KeyA,
	}
	return strings.TrimSuffix(c.baseURL, "/") + "/tusupload", headers, out.GUID, nil
}

func (c *bunnyStream) cdn(cfg VideoConfig) string {
	return fmt.Sprintf("https://%s.b-cdn.net", strings.TrimSpace(cfg.KeyC))
}

func (c *bunnyStream) PlaybackURL(cfg VideoConfig, videoID string) string {
	return fmt.Sprintf("%s/%s/playlist.m3u8", c.cdn(cfg), videoID)
}

// 720p MP4 需要库设置里开启 MP4 Fallback；未开启时该地址 404。
func (c *bunnyStream) DownloadURL(cfg VideoConfig, videoID string) string {
	if strings.TrimSpace(cfg.KeyC) == "" {
		return ""
	}
	return fmt.Sprintf("%s/%s/play_720p.mp4", c.cdn(cfg), videoID)
}

// sortedKeys 仅用于测试断言头部集合的稳定性。
func sortedKeys(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// registerOnly 用于「已有 VOD 资产」的厂商：不做直传，只按播放域名 + 资产 id 推导播放地址。
// 直传需要各家的客户端 SDK 与转码状态机，等有测试账号再加。
type registerOnly struct {
	name string
	cdn  string // fmt: 播放域名, 资产 id
}

func (p *registerOnly) Name() string         { return p.name }
func (p *registerOnly) SupportsUpload() bool { return false }

func (p *registerOnly) CreateUpload(context.Context, VideoConfig, string) (string, map[string]string, string, error) {
	return "", nil, "", errors.New(p.name + ": 该厂商暂只支持登记已有视频，不支持直传")
}

func (p *registerOnly) PlaybackURL(cfg VideoConfig, videoID string) string {
	return fmt.Sprintf(p.cdn, strings.TrimSuffix(strings.TrimSpace(cfg.KeyA), "/"), videoID)
}

// 两家的 MP4 地址依赖转码模板组，规则不统一，这里不猜：正文请用播放地址或自行填直链。
func (p *registerOnly) DownloadURL(VideoConfig, string) string { return "" }
