package storage

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

type initiateResult struct {
	XMLName  xml.Name `xml:"InitiateMultipartUploadResult"`
	UploadID string   `xml:"UploadId"`
}

type completeReq struct {
	XMLName xml.Name       `xml:"CompleteMultipartUpload"`
	Parts   []completePart `xml:"Part"`
}

type completePart struct {
	PartNumber int    `xml:"PartNumber"`
	ETag       string `xml:"ETag"`
}

// CreateMultipart 发起分片上传，返回 uploadId。
func (c *S3) CreateMultipart(ctx context.Context, key string, opt PutOptions) (string, error) {
	if err := ValidateKey(key); err != nil {
		return "", err
	}
	q := url.Values{"uploads": {""}}
	resp, err := c.do(ctx, http.MethodPost, c.objectURL(key, q), c.putHeaders(opt), nil, 0, emptyPayload)
	if err != nil {
		return "", err
	}
	if err := c.check(resp, http.StatusOK); err != nil {
		return "", err
	}
	defer resp.Body.Close()
	var res initiateResult
	if err := xml.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&res); err != nil || res.UploadID == "" {
		return "", fmt.Errorf("storage: bad CreateMultipartUpload response: %v", err)
	}
	return res.UploadID, nil
}

// UploadPart 上传一片（number 从 1 开始）。除最后一片外，S3 要求每片至少 5 MiB。
func (c *S3) UploadPart(ctx context.Context, key, uploadID string, number int, r io.Reader, size int64) (string, error) {
	if err := ValidateKey(key); err != nil {
		return "", err
	}
	if number < 1 || number > 10000 {
		return "", fmt.Errorf("storage: part number %d out of range 1-10000", number)
	}
	rs, ok := r.(io.ReadSeeker)
	if !ok {
		buf, err := io.ReadAll(io.LimitReader(r, size+1))
		if err != nil {
			return "", err
		}
		rs = bytes.NewReader(buf)
		if int64(len(buf)) != size {
			return "", fmt.Errorf("storage: part %d expected %d bytes, got %d", number, size, len(buf))
		}
	}
	start, _ := rs.Seek(0, io.SeekCurrent)
	h := sha256.New()
	if n, err := io.Copy(h, io.LimitReader(rs, size)); err != nil || n != size {
		return "", fmt.Errorf("storage: part %d read %d of %d bytes: %v", number, n, size, err)
	}
	if _, err := rs.Seek(start, io.SeekStart); err != nil {
		return "", err
	}
	q := url.Values{"partNumber": {strconv.Itoa(number)}, "uploadId": {uploadID}}
	resp, err := c.do(ctx, http.MethodPut, c.objectURL(key, q), nil, rs, size, hex.EncodeToString(h.Sum(nil)))
	if err != nil {
		return "", err
	}
	defer drain(resp)
	if err := c.check(resp, http.StatusOK); err != nil {
		return "", err
	}
	etag := resp.Header.Get("ETag")
	if etag == "" {
		return "", errors.New("storage: UploadPart response has no ETag")
	}
	return etag, nil
}

// CompleteMultipart 合并分片。S3 可能返回 200 但正文是 <Error>，这里也当作失败处理。
func (c *S3) CompleteMultipart(ctx context.Context, key, uploadID string, parts []Part) error {
	if err := ValidateKey(key); err != nil {
		return err
	}
	if len(parts) == 0 {
		return errors.New("storage: no parts to complete")
	}
	req := completeReq{}
	for _, p := range parts {
		req.Parts = append(req.Parts, completePart{PartNumber: p.Number, ETag: p.ETag})
	}
	body, err := xml.Marshal(req)
	if err != nil {
		return err
	}
	sum := sha256.Sum256(body)
	hdr := http.Header{"Content-Type": {"application/xml"}}
	q := url.Values{"uploadId": {uploadID}}
	resp, err := c.do(ctx, http.MethodPost, c.objectURL(key, q), hdr, bytes.NewReader(body), int64(len(body)), hex.EncodeToString(sum[:]))
	if err != nil {
		return err
	}
	if err := c.check(resp, http.StatusOK); err != nil {
		return err
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	var x s3ErrorXML
	if xml.Unmarshal(data, &x) == nil && x.Code != "" {
		return x.toError(resp.StatusCode)
	}
	return nil
}

// AbortMultipart 中止分片上传并释放已传的分片。ctx 被取消时仍会用独立的短超时尝试一次。
func (c *S3) AbortMultipart(ctx context.Context, key, uploadID string) error {
	if err := ValidateKey(key); err != nil {
		return err
	}
	actx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
	defer cancel()
	q := url.Values{"uploadId": {uploadID}}
	resp, err := c.do(actx, http.MethodDelete, c.objectURL(key, q), nil, nil, 0, emptyPayload)
	if err != nil {
		return err
	}
	defer drain(resp)
	if resp.StatusCode == http.StatusNotFound { // NoSuchUpload：已经没了
		return nil
	}
	return c.check(resp, http.StatusOK, http.StatusNoContent)
}

type listUploadsResult struct {
	XMLName            xml.Name `xml:"ListMultipartUploadsResult"`
	IsTruncated        bool     `xml:"IsTruncated"`
	NextKeyMarker      string   `xml:"NextKeyMarker"`
	NextUploadIDMarker string   `xml:"NextUploadIdMarker"`
	Uploads            []struct {
		Key       string    `xml:"Key"`
		UploadID  string    `xml:"UploadId"`
		Initiated time.Time `xml:"Initiated"`
	} `xml:"Upload"`
}

// AbortStale 中止 prefix 范围内超过 olderThan 且 match(去掉前缀的键) 为真的未完成分片上传。
func (c *S3) AbortStale(ctx context.Context, olderThan time.Duration, match func(key string) bool) (int, error) {
	aborted := 0
	var keyMarker, idMarker string
	for page := 0; page < 100; page++ {
		q := url.Values{"uploads": {""}, "max-uploads": {"1000"}}
		if c.prefix != "" {
			q.Set("prefix", c.prefix+"/")
		}
		if keyMarker != "" {
			q.Set("key-marker", keyMarker)
		}
		if idMarker != "" {
			q.Set("upload-id-marker", idMarker)
		}
		resp, err := c.do(ctx, http.MethodGet, c.buildURL("", q), nil, nil, 0, emptyPayload)
		if err != nil {
			return aborted, err
		}
		if err := c.check(resp, http.StatusOK); err != nil {
			return aborted, err
		}
		var res listUploadsResult
		err = xml.NewDecoder(io.LimitReader(resp.Body, 8<<20)).Decode(&res)
		_ = resp.Body.Close()
		if err != nil {
			return aborted, fmt.Errorf("storage: bad ListMultipartUploads response: %w", err)
		}
		for _, u := range res.Uploads {
			key := u.Key
			if c.prefix != "" {
				key = strings.TrimPrefix(key, c.prefix+"/")
			}
			if time.Since(u.Initiated) < olderThan || (match != nil && !match(key)) {
				continue
			}
			if err := c.AbortMultipart(ctx, key, u.UploadID); err != nil {
				return aborted, err
			}
			aborted++
		}
		if !res.IsTruncated {
			break
		}
		keyMarker, idMarker = res.NextKeyMarker, res.NextUploadIDMarker
	}
	return aborted, nil
}

var (
	_ Multipart    = (*S3)(nil)
	_ StaleAborter = (*S3)(nil)
)
