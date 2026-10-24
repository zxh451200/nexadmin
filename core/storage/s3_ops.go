package storage

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

func (c *S3) Put(ctx context.Context, key string, r io.Reader, size int64, opt PutOptions) error {
	if size < 0 {
		return errors.New("storage: Put needs a non-negative size, use PutStream")
	}
	return c.put(ctx, key, r, size, opt)
}

func (c *S3) PutStream(ctx context.Context, key string, r io.Reader, opt PutOptions) error {
	return c.put(ctx, key, r, -1, opt)
}

// put 按大小选择单次 PUT 或分片上传。size < 0 表示未知，先读 threshold+1 字节判断。
func (c *S3) put(ctx context.Context, key string, r io.Reader, size int64, opt PutOptions) error {
	if err := ValidateKey(key); err != nil {
		return err
	}
	if size > c.threshold {
		return c.putMultipart(ctx, key, r, size, opt)
	}
	if rs, ok := r.(io.ReadSeeker); ok && size >= 0 {
		hash := strings.ToLower(opt.SHA256)
		if !isHexSHA256(hash) {
			start, _ := rs.Seek(0, io.SeekCurrent)
			h := sha256.New()
			if n, err := io.Copy(h, io.LimitReader(rs, size)); err != nil || n != size {
				return fmt.Errorf("storage: read body: got %d of %d bytes: %v", n, size, err)
			}
			if _, err := rs.Seek(start, io.SeekStart); err != nil {
				return err
			}
			hash = hex.EncodeToString(h.Sum(nil))
		}
		return c.putObject(ctx, key, rs, size, hash, opt)
	}
	limit := c.threshold + 1
	if size >= 0 {
		limit = size
	}
	buf, err := io.ReadAll(io.LimitReader(r, limit))
	if err != nil {
		return err
	}
	if size >= 0 && int64(len(buf)) != size {
		return fmt.Errorf("storage: expected %d bytes, got %d", size, len(buf))
	}
	if int64(len(buf)) > c.threshold { // 未知大小且超过阈值：已读部分接上剩余流，走分片
		return c.putMultipart(ctx, key, io.MultiReader(bytes.NewReader(buf), r), -1, opt)
	}
	sum := sha256.Sum256(buf)
	return c.putObject(ctx, key, bytes.NewReader(buf), int64(len(buf)), hex.EncodeToString(sum[:]), opt)
}

func (c *S3) putObject(ctx context.Context, key string, body io.ReadSeeker, size int64, hash string, opt PutOptions) error {
	resp, err := c.do(ctx, http.MethodPut, c.objectURL(key, nil), c.putHeaders(opt), body, size, hash)
	if err != nil {
		return err
	}
	defer drain(resp)
	return c.check(resp, http.StatusOK)
}

// putMultipart 顺序读 r，每次缓冲一个分片并上传；任何一步失败都 Abort，不留垃圾分片。
func (c *S3) putMultipart(ctx context.Context, key string, r io.Reader, size int64, opt PutOptions) (err error) {
	partSize := c.partSize
	if size > 0 { // 分片数上限 10000
		for (size+partSize-1)/partSize > 10000 {
			partSize *= 2
		}
	}
	uploadID, err := c.CreateMultipart(ctx, key, opt)
	if err != nil {
		return err
	}
	defer func() {
		if err != nil {
			_ = c.AbortMultipart(ctx, key, uploadID)
		}
	}()
	buf := make([]byte, partSize)
	var parts []Part
	var total int64
	for n := 1; ; n++ {
		m, rerr := io.ReadFull(r, buf)
		if rerr == io.EOF {
			break
		}
		if rerr != nil && !errors.Is(rerr, io.ErrUnexpectedEOF) {
			return rerr
		}
		etag, perr := c.UploadPart(ctx, key, uploadID, n, bytes.NewReader(buf[:m]), int64(m))
		if perr != nil {
			return perr
		}
		parts = append(parts, Part{Number: n, ETag: etag})
		total += int64(m)
		if rerr != nil { // ErrUnexpectedEOF：这是最后一片
			break
		}
	}
	if len(parts) == 0 || (size >= 0 && total != size) {
		return fmt.Errorf("storage: expected %d bytes, got %d", size, total)
	}
	return c.CompleteMultipart(ctx, key, uploadID, parts)
}

func objectInfoFrom(key string, h http.Header, size int64) ObjectInfo {
	info := ObjectInfo{
		Key: key, Size: size, ContentType: h.Get("Content-Type"), ContentDisposition: h.Get("Content-Disposition"),
		ETag: strings.Trim(h.Get("ETag"), `"`),
	}
	if t, err := http.ParseTime(h.Get("Last-Modified")); err == nil {
		info.LastModified = t
	}
	return info
}

// parseContentRange 解析 "bytes 0-9/100"。
func parseContentRange(v string) (start, end, total int64, ok bool) {
	var n int
	n, _ = fmt.Sscanf(v, "bytes %d-%d/%d", &start, &end, &total)
	return start, end, total, n == 3
}

func (c *S3) Get(ctx context.Context, key string, rng *Range) (*Object, error) {
	if err := ValidateKey(key); err != nil {
		return nil, err
	}
	hdr := http.Header{"Accept-Encoding": {"identity"}}
	if rng != nil {
		hdr.Set("Range", rng.Header())
	}
	resp, err := c.do(ctx, http.MethodGet, c.objectURL(key, nil), hdr, nil, 0, emptyPayload)
	if err != nil {
		return nil, err
	}
	switch resp.StatusCode {
	case http.StatusOK:
		return &Object{Body: resp.Body, Info: objectInfoFrom(key, resp.Header, resp.ContentLength), Length: resp.ContentLength, End: resp.ContentLength - 1}, nil
	case http.StatusPartialContent:
		s, e, total, ok := parseContentRange(resp.Header.Get("Content-Range"))
		if !ok {
			drain(resp)
			return nil, errors.New("storage: s3 returned 206 without a valid Content-Range")
		}
		return &Object{Body: resp.Body, Info: objectInfoFrom(key, resp.Header, total), Length: e - s + 1, Partial: true, Start: s, End: e}, nil
	case http.StatusRequestedRangeNotSatisfiable:
		drain(resp)
		return nil, ErrRangeNotSatisfiable
	}
	err = parseS3Error(resp)
	if errors.Is(err, ErrNotFound) {
		return nil, ErrNotFound
	}
	return nil, err
}

func (c *S3) Stat(ctx context.Context, key string) (*ObjectInfo, error) {
	if err := ValidateKey(key); err != nil {
		return nil, err
	}
	resp, err := c.do(ctx, http.MethodHead, c.objectURL(key, nil), nil, nil, 0, emptyPayload)
	if err != nil {
		return nil, err
	}
	defer drain(resp)
	switch resp.StatusCode {
	case http.StatusOK:
		info := objectInfoFrom(key, resp.Header, resp.ContentLength)
		if v := resp.Header.Get("Content-Length"); v != "" {
			info.Size, _ = strconv.ParseInt(v, 10, 64)
		}
		return &info, nil
	case http.StatusNotFound:
		return nil, ErrNotFound // HEAD 没有响应体，无法区分 NoSuchKey / NoSuchBucket
	}
	return nil, &S3Error{Status: resp.StatusCode, Message: "HEAD " + key, RequestID: resp.Header.Get("X-Amz-Request-Id")}
}

func (c *S3) Exists(ctx context.Context, key string) (bool, error) {
	if _, err := c.Stat(ctx, key); err != nil {
		if errors.Is(err, ErrNotFound) {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

func (c *S3) Delete(ctx context.Context, key string) error {
	if err := ValidateKey(key); err != nil {
		return err
	}
	resp, err := c.do(ctx, http.MethodDelete, c.objectURL(key, nil), nil, nil, 0, emptyPayload)
	if err != nil {
		return err
	}
	defer drain(resp)
	if resp.StatusCode == http.StatusNotFound {
		return nil
	}
	return c.check(resp, http.StatusOK, http.StatusNoContent)
}

// PresignGet 生成限时下载链接（ttl 1 秒 ~ 7 天）。
func (c *S3) PresignGet(ctx context.Context, key string, ttl time.Duration, opt PresignOptions) (string, error) {
	if err := ValidateKey(key); err != nil {
		return "", err
	}
	if ttl < time.Second || ttl > 7*24*time.Hour {
		return "", fmt.Errorf("storage: presign ttl must be between 1s and 7d, got %s", ttl)
	}
	q := url.Values{}
	if v := headerSafe(opt.ContentType); v != "" {
		q.Set("response-content-type", v)
	}
	if v := headerSafe(opt.ContentDisposition); v != "" {
		q.Set("response-content-disposition", v)
	}
	u := c.objectURL(key, q)
	req := &http.Request{Method: http.MethodGet, URL: u, Host: u.Host, Header: http.Header{}}
	return c.signer.Presign(req, ttl, c.now()), nil
}

var (
	_ Storage = (*S3)(nil)
)
