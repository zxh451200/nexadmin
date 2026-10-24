package storage

import (
	"context"
	"crypto/tls"
	"encoding/hex"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// S3Config 是 storage.s3.* 配置。字符串字段支持 ${ENV}。
type S3Config struct {
	Endpoint  string `mapstructure:"endpoint"` // https://s3.amazonaws.com、http://minio:9000、https://oss-cn-hangzhou.aliyuncs.com ...；无协议头时按 https
	Region    string `mapstructure:"region"`   // 默认 us-east-1
	Bucket    string `mapstructure:"bucket"`
	AccessKey string `mapstructure:"access_key"`
	SecretKey string `mapstructure:"secret_key"`
	Prefix    string `mapstructure:"prefix"` // 可选键前缀，如 nexadmin/
	// PathStyle=true 用 https://endpoint/bucket/key（MinIO 默认）；false 用 https://bucket.endpoint/key（OSS / COS 必须）。
	// endpoint 是 IP 时虚拟主机风格无法解析，会自动退回 path-style。
	PathStyle          bool `mapstructure:"path_style"`
	InsecureSkipVerify bool `mapstructure:"insecure_skip_verify"` // 不校验 TLS 证书（仅内网自签名场景）
	// PresignRedirect=true 时私有文件 302 到预签名 URL（会暴露 bucket 地址）；默认 false，由后端代理。
	PresignRedirect      bool `mapstructure:"presign_redirect"`
	PartSizeMB           int  `mapstructure:"part_size_mb"`           // 分片大小，默认 8，最小 5
	MultipartThresholdMB int  `mapstructure:"multipart_threshold_mb"` // 超过则走分片上传，默认 16

	HTTPClient *http.Client `mapstructure:"-"` // 测试注入
}

func (c S3Config) expanded() (S3Config, error) {
	for _, p := range []*string{&c.Endpoint, &c.Region, &c.Bucket, &c.AccessKey, &c.SecretKey, &c.Prefix} {
		v, err := ExpandEnv(*p)
		if err != nil {
			return c, err
		}
		*p = strings.TrimSpace(v)
	}
	return c, nil
}

// S3Error 是 S3 返回的 XML 错误。
type S3Error struct {
	Status    int
	Code      string
	Message   string
	RequestID string
	Resource  string
}

func (e *S3Error) Error() string {
	return fmt.Sprintf("s3: %d %s: %s (request id %s)", e.Status, e.Code, e.Message, e.RequestID)
}

// Is 让 NoSuchKey 匹配 ErrNotFound（NoSuchBucket 不算，那是配置错误）。
func (e *S3Error) Is(target error) bool {
	return target == ErrNotFound && e.Status == http.StatusNotFound && (e.Code == "NoSuchKey" || e.Code == "")
}

type s3ErrorXML struct {
	XMLName   xml.Name `xml:"Error"`
	Code      string   `xml:"Code"`
	Message   string   `xml:"Message"`
	RequestID string   `xml:"RequestId"`
	Resource  string   `xml:"Resource"`
}

func (x s3ErrorXML) toError(status int) *S3Error {
	return &S3Error{Status: status, Code: x.Code, Message: x.Message, RequestID: x.RequestID, Resource: x.Resource}
}

// parseS3Error 读取并关闭响应体，解析 XML Error；解析不了就保留状态码和一段原文。
func parseS3Error(resp *http.Response) *S3Error {
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
	var x s3ErrorXML
	if err := xml.Unmarshal(body, &x); err != nil || x.Code == "" {
		msg := strings.TrimSpace(string(body))
		if len(msg) > 200 {
			msg = msg[:200]
		}
		return &S3Error{Status: resp.StatusCode, Message: msg, RequestID: resp.Header.Get("X-Amz-Request-Id")}
	}
	e := x.toError(resp.StatusCode)
	if e.RequestID == "" {
		e.RequestID = resp.Header.Get("X-Amz-Request-Id")
	}
	return e
}

// S3 是 S3 兼容存储驱动。
type S3 struct {
	scheme, host string
	bucket       string
	prefix       string
	pathStyle    bool
	signer       Signer
	client       *http.Client
	partSize     int64
	threshold    int64
	retries      int
	backoff      time.Duration
	now          func() time.Time
}

// NewS3 创建 S3 驱动（cfg 里的 ${ENV} 应已展开）。
func NewS3(cfg S3Config) (*S3, error) {
	if cfg.Endpoint == "" || cfg.Bucket == "" || cfg.AccessKey == "" || cfg.SecretKey == "" {
		return nil, errors.New("storage: s3 needs endpoint, bucket, access_key and secret_key")
	}
	ep := cfg.Endpoint
	if !strings.Contains(ep, "://") {
		ep = "https://" + ep
	}
	u, err := url.Parse(ep)
	if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") {
		return nil, fmt.Errorf("storage: invalid s3 endpoint %q", cfg.Endpoint)
	}
	if p := strings.Trim(u.Path, "/"); p != "" {
		return nil, fmt.Errorf("storage: s3 endpoint must not contain a path: %q", cfg.Endpoint)
	}
	region := cfg.Region
	if region == "" {
		region = "us-east-1"
	}
	pathStyle := cfg.PathStyle
	if h := u.Hostname(); net.ParseIP(h) != nil || h == "localhost" {
		pathStyle = true
	}
	part := int64(cfg.PartSizeMB)
	if part <= 0 {
		part = 8
	}
	part = max(part, 5) << 20
	thr := int64(cfg.MultipartThresholdMB)
	if thr <= 0 {
		thr = 16
	}
	client := cfg.HTTPClient
	if client == nil {
		tr := http.DefaultTransport.(*http.Transport).Clone()
		if cfg.InsecureSkipVerify {
			tr.TLSClientConfig = &tls.Config{InsecureSkipVerify: true} //nolint:gosec // 由配置显式开启
		}
		client = &http.Client{Transport: tr}
	}
	return &S3{
		scheme: u.Scheme, host: u.Host, bucket: cfg.Bucket, pathStyle: pathStyle,
		prefix: strings.Trim(cfg.Prefix, "/"),
		signer: Signer{AccessKey: cfg.AccessKey, SecretKey: cfg.SecretKey, Region: region, Service: "s3"},
		client: client, partSize: part, threshold: max(thr<<20, part), retries: 2, backoff: 200 * time.Millisecond,
		now: time.Now,
	}, nil
}

func (c *S3) fullKey(key string) string {
	if c.prefix == "" {
		return key
	}
	return c.prefix + "/" + key
}

// HeadBucket checks the configured bucket's existence and bucket-level access.
// It never creates buckets, lists objects, or writes data.
func (c *S3) HeadBucket(ctx context.Context) error {
	resp, err := c.do(ctx, http.MethodHead, c.buildURL("", nil), nil, nil, 0, "")
	if err != nil {
		return err
	}
	if resp.StatusCode != http.StatusOK {
		_ = resp.Body.Close()
		return &S3Error{Status: resp.StatusCode}
	}
	_ = resp.Body.Close()
	return nil
}

// buildURL 构造请求 URL。fullKey 为空表示 bucket 级请求。
func (c *S3) buildURL(fullKey string, q url.Values) *url.URL {
	u := &url.URL{Scheme: c.scheme}
	if c.pathStyle {
		u.Host = c.host
		u.Path = "/" + c.bucket + "/" + fullKey
	} else {
		u.Host = c.bucket + "." + c.host
		u.Path = "/" + fullKey
	}
	u.RawPath = uriEncode(u.Path, false)
	u.RawQuery = canonicalQuery(q)
	return u
}

func (c *S3) objectURL(key string, q url.Values) *url.URL { return c.buildURL(c.fullKey(key), q) }

func isHexSHA256(s string) bool {
	if len(s) != 64 {
		return false
	}
	_, err := hex.DecodeString(s)
	return err == nil
}

func headerSafe(v string) string {
	for i := 0; i < len(v); i++ {
		if v[i] < 0x20 || v[i] > 0x7e {
			return ""
		}
	}
	return v
}

func (c *S3) putHeaders(opt PutOptions) http.Header {
	h := http.Header{}
	if v := headerSafe(opt.ContentType); v != "" {
		h.Set("Content-Type", v)
	}
	if v := headerSafe(opt.ContentDisposition); v != "" {
		h.Set("Content-Disposition", v)
	}
	if v := headerSafe(opt.CacheControl); v != "" {
		h.Set("Cache-Control", v)
	}
	return h
}

// do 发送已签名请求；网络错误和 5xx 最多重试 c.retries 次。body 必须可 Seek（重试时回到起点）。
func (c *S3) do(ctx context.Context, method string, u *url.URL, hdr http.Header, body io.ReadSeeker, length int64, payloadHash string) (*http.Response, error) {
	var start int64
	if body != nil {
		start, _ = body.Seek(0, io.SeekCurrent)
	}
	var lastErr error
	for attempt := 0; attempt <= c.retries; attempt++ {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(c.backoff * time.Duration(attempt)):
			}
			if body != nil {
				if _, err := body.Seek(start, io.SeekStart); err != nil {
					return nil, err
				}
			}
		}
		req, err := http.NewRequestWithContext(ctx, method, u.String(), nil)
		if err != nil {
			return nil, err
		}
		for k, vs := range hdr {
			req.Header[k] = append([]string(nil), vs...)
		}
		if body != nil && length > 0 {
			req.Body = io.NopCloser(io.LimitReader(body, length))
			req.ContentLength = length
		} else if method == http.MethodPut || method == http.MethodPost {
			req.Body, req.ContentLength = http.NoBody, 0
		}
		c.signer.SignRequest(req, payloadHash, c.now())
		resp, err := c.client.Do(req)
		if err != nil {
			lastErr = err
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			continue
		}
		if resp.StatusCode >= 500 && attempt < c.retries {
			lastErr = parseS3Error(resp)
			continue
		}
		return resp, nil
	}
	return nil, lastErr
}

// check 对非期望状态码的响应解析 XML 错误并返回。
func (c *S3) check(resp *http.Response, ok ...int) error {
	for _, s := range ok {
		if resp.StatusCode == s {
			return nil
		}
	}
	return parseS3Error(resp)
}

func drain(resp *http.Response) {
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<20))
	_ = resp.Body.Close()
}
