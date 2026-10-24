// Package storage 是对象存储抽象：本地目录与 S3 兼容存储（MinIO / 阿里云 OSS / 腾讯 COS / 七牛等）共用一个接口。
// S3 驱动用标准库手写 SigV4 签名，不依赖 minio-go / aws-sdk。
package storage

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"regexp"
	"strings"
	"time"
)

var (
	ErrNotFound            = errors.New("storage: object not found")
	ErrRangeNotSatisfiable = errors.New("storage: range not satisfiable")
	ErrNotSupported        = errors.New("storage: operation not supported by this driver")
	ErrInvalidKey          = errors.New("storage: invalid object key")
)

// PutOptions 是写入对象时附带的元数据。
type PutOptions struct {
	ContentType        string
	ContentDisposition string
	CacheControl       string
	// SHA256 是正文的十六进制 SHA-256（可选）。已知时 S3 驱动不再多读一遍文件去算哈希。
	SHA256 string
}

// PresignOptions 通过 response-* 查询参数覆盖下载时返回的头。
type PresignOptions struct {
	ContentType        string
	ContentDisposition string
}

// ObjectInfo 是对象元数据。Size 永远是整个对象的大小。
type ObjectInfo struct {
	Key                string
	Size               int64
	ContentType        string
	ContentDisposition string
	ETag               string
	LastModified       time.Time
}

// Range 表示一个字节范围（闭区间）。End < 0 表示读到末尾；Start < 0 表示取最后 -Start 个字节。
type Range struct{ Start, End int64 }

// Header 返回 HTTP Range 头的值。
func (r Range) Header() string {
	switch {
	case r.Start < 0:
		return fmt.Sprintf("bytes=-%d", -r.Start)
	case r.End < 0:
		return fmt.Sprintf("bytes=%d-", r.Start)
	}
	return fmt.Sprintf("bytes=%d-%d", r.Start, r.End)
}

// resolve 按对象大小算出实际的 [start, end]（闭区间）。
func (r Range) resolve(size int64) (int64, int64, error) {
	if size <= 0 {
		return 0, 0, ErrRangeNotSatisfiable
	}
	if r.Start < 0 {
		n := min(-r.Start, size)
		return size - n, size - 1, nil
	}
	end := r.End
	if r.Start >= size {
		return 0, 0, ErrRangeNotSatisfiable
	}
	if end < 0 || end >= size {
		end = size - 1
	}
	if end < r.Start {
		return 0, 0, ErrRangeNotSatisfiable
	}
	return r.Start, end, nil
}

// Object 是 Get 的结果，调用方必须关闭 Body。
// Partial 为 true 表示只返回了 [Start, End]（HTTP 206），Length 是 Body 的字节数。
type Object struct {
	Body       io.ReadCloser
	Info       ObjectInfo
	Length     int64
	Partial    bool
	Start, End int64
}

// Storage 是文件模块使用的存储接口。key 用斜杠分隔，不能以 / 开头，不能含 .. 段。
type Storage interface {
	// Put 写入已知大小的对象（size 必须与 r 实际长度一致）。
	Put(ctx context.Context, key string, r io.Reader, size int64, opt PutOptions) error
	// PutStream 写入未知大小的对象。
	PutStream(ctx context.Context, key string, r io.Reader, opt PutOptions) error
	// Get 读取对象；rng 为 nil 读全部。对象不存在返回 ErrNotFound，范围越界返回 ErrRangeNotSatisfiable。
	Get(ctx context.Context, key string, rng *Range) (*Object, error)
	// Delete 删除对象；对象不存在不算错误。
	Delete(ctx context.Context, key string) error
	Exists(ctx context.Context, key string) (bool, error)
	// Stat 返回元数据；对象不存在返回 ErrNotFound。
	Stat(ctx context.Context, key string) (*ObjectInfo, error)
	// PresignGet 生成限时下载链接；本地驱动返回 ErrNotSupported。
	PresignGet(ctx context.Context, key string, ttl time.Duration, opt PresignOptions) (string, error)
}

// Part 是分片上传里已完成的一片。
type Part struct {
	Number int
	ETag   string
}

// Multipart 由支持分片上传的驱动（S3）实现。
type Multipart interface {
	CreateMultipart(ctx context.Context, key string, opt PutOptions) (uploadID string, err error)
	UploadPart(ctx context.Context, key, uploadID string, number int, r io.Reader, size int64) (etag string, err error)
	CompleteMultipart(ctx context.Context, key, uploadID string, parts []Part) error
	AbortMultipart(ctx context.Context, key, uploadID string) error
}

// Opener 由本地驱动实现：返回可 Seek 的文件，调用方可直接用 http.ServeContent（Range / If-Modified-Since 全保留）。
type Opener interface {
	Open(ctx context.Context, key string) (io.ReadSeekCloser, *ObjectInfo, error)
}

// Adopter 由本地驱动实现：把同一磁盘上的文件直接改名成对象，免去一次拷贝。
type Adopter interface {
	Adopt(ctx context.Context, key, srcPath string) error
}

// StaleAborter 由 S3 驱动实现：中止超过 olderThan 且 match(key) 为真的未完成分片上传，返回中止数量。
type StaleAborter interface {
	AbortStale(ctx context.Context, olderThan time.Duration, match func(key string) bool) (int, error)
}

// ValidateKey 校验对象键。
func ValidateKey(key string) error {
	if key == "" || len(key) > 900 || strings.HasPrefix(key, "/") || strings.ContainsAny(key, "\\\x00") {
		return fmt.Errorf("%w: %q", ErrInvalidKey, key)
	}
	for _, seg := range strings.Split(key, "/") {
		if seg == "" || seg == "." || seg == ".." {
			return fmt.Errorf("%w: %q", ErrInvalidKey, key)
		}
	}
	return nil
}

// ObjectKey 生成对象键：<租户>/<yyyymmdd>/<雪花ID><扩展名>。只用 ID 和受限扩展名，永远不含用户原始文件名。
func ObjectKey(tenantID, id, ext string, now time.Time) string {
	return fmt.Sprintf("%s/%s/%s%s", tenantID, now.UTC().Format("20060102"), id, cleanExt(ext))
}

func cleanExt(ext string) string {
	ext = strings.ToLower(ext)
	if len(ext) < 2 || len(ext) > 10 || ext[0] != '.' {
		return ""
	}
	for _, r := range ext[1:] {
		if !(r >= 'a' && r <= 'z' || r >= '0' && r <= '9') {
			return ""
		}
	}
	return ext
}

// Config 是 storage.* 配置。
type Config struct {
	Driver string   `mapstructure:"driver"` // local（默认）| s3
	S3     S3Config `mapstructure:"s3"`
}

// Validate 只做结构检查（${ENV} 在 New 时才展开）。
func (c Config) Validate() error {
	switch c.Driver {
	case "", "local":
		return nil
	case "s3":
		if c.S3.Endpoint == "" || c.S3.Bucket == "" || c.S3.AccessKey == "" || c.S3.SecretKey == "" {
			return errors.New("config: storage.s3 needs endpoint, bucket, access_key and secret_key")
		}
		return nil
	}
	return fmt.Errorf("config: storage.driver must be local or s3, got %q", c.Driver)
}

// New 按配置创建驱动。localDir 是本地驱动的根目录。
func New(cfg Config, localDir string) (Storage, error) {
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	if cfg.Driver == "s3" {
		s3cfg, err := cfg.S3.expanded()
		if err != nil {
			return nil, err
		}
		return NewS3(s3cfg)
	}
	return NewLocal(localDir)
}

var envRef = regexp.MustCompile(`\$\{([A-Za-z_][A-Za-z0-9_]*)\}`)

// ExpandEnv 展开 ${NAME}；只认花括号形式（密钥里可能带 $）。引用了未设置的变量时报错。
func ExpandEnv(s string) (string, error) {
	var missing string
	out := envRef.ReplaceAllStringFunc(s, func(m string) string {
		name := m[2 : len(m)-1]
		v, ok := os.LookupEnv(name)
		if !ok && missing == "" {
			missing = name
		}
		return v
	})
	if missing != "" {
		return "", fmt.Errorf("config: environment variable %s is not set", missing)
	}
	return out, nil
}
