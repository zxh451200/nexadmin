package storage

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestS3_MultipartFlow(t *testing.T) {
	c, fake := newTestS3(t, "bkt", "app", true)
	ctx := context.Background()
	// 62 字节 > 阈值 25，分片 10 字节 → 7 片（6 满片 + 2 字节尾片）
	data := strings.Repeat("0123456789", 6) + "ab"
	opt := PutOptions{ContentType: "video/mp4", ContentDisposition: "inline"}
	if err := c.Put(ctx, "t/20261017/9.mp4", strings.NewReader(data), int64(len(data)), opt); err != nil {
		t.Fatal(err)
	}
	o := fake.objects["app/t/20261017/9.mp4"]
	if o == nil || string(o.data) != data || o.contentType != "video/mp4" || o.disposition != "inline" {
		t.Fatalf("assembled object wrong: %+v", o)
	}
	if len(fake.uploads) != 0 {
		t.Fatal("upload should be completed and removed")
	}
	var parts int
	for _, r := range fake.reqs {
		if strings.HasPrefix(r, "PUT") && strings.Contains(r, "partNumber=") {
			parts++
		}
	}
	if parts != 7 {
		t.Fatalf("want 7 UploadPart calls, got %d: %v", parts, fake.reqs)
	}
}

func TestS3_MultipartExactMultipleAndStream(t *testing.T) {
	c, fake := newTestS3(t, "bkt", "", true)
	ctx := context.Background()
	data := strings.Repeat("x", 40) // 恰好 4 个整片，不能多出空分片
	if err := c.Put(ctx, "a", strings.NewReader(data), 40, PutOptions{}); err != nil {
		t.Fatal(err)
	}
	if string(fake.objects["a"].data) != data {
		t.Fatal("exact multiple mismatch")
	}
	// 未知大小且超过阈值：先缓冲探测，再接上剩余流走分片
	big := strings.Repeat("y", 57)
	if err := c.PutStream(ctx, "b", strings.NewReader(big), PutOptions{}); err != nil {
		t.Fatal(err)
	}
	if string(fake.objects["b"].data) != big {
		t.Fatal("stream multipart mismatch")
	}
}

func TestS3_MultipartFailureAborts(t *testing.T) {
	c, fake := newTestS3(t, "bkt", "", true)
	fake.failPut = 3 // 第 3 片 403（不可重试）
	err := c.Put(context.Background(), "t/big.bin", strings.NewReader(strings.Repeat("z", 60)), 60, PutOptions{})
	var se *S3Error
	if !errors.As(err, &se) || se.Code != "AccessDenied" {
		t.Fatalf("want AccessDenied, got %v", err)
	}
	if len(fake.uploads) != 0 {
		t.Fatalf("failed upload must be aborted, still have %d", len(fake.uploads))
	}
	if _, ok := fake.objects["t/big.bin"]; ok {
		t.Fatal("no object should exist after failure")
	}
	var aborted bool
	for _, r := range fake.reqs {
		if strings.HasPrefix(r, "DELETE") && strings.Contains(r, "uploadId=") {
			aborted = true
		}
	}
	if !aborted {
		t.Fatalf("AbortMultipartUpload not sent: %v", fake.reqs)
	}
}

func TestS3_MultipartCancelledContextStillAborts(t *testing.T) {
	c, fake := newTestS3(t, "bkt", "", true)
	ctx, cancel := context.WithCancel(context.Background())
	r := &cancelAfter{data: []byte(strings.Repeat("c", 60)), cancel: cancel, after: 25}
	err := c.PutStream(ctx, "k", r, PutOptions{})
	if err == nil {
		t.Fatal("expected error after cancel")
	}
	if len(fake.uploads) != 0 {
		t.Fatalf("cancelled upload must still be aborted with a detached context, left %d", len(fake.uploads))
	}
}

type cancelAfter struct {
	data   []byte
	pos    int
	after  int
	cancel func()
}

func (r *cancelAfter) Read(p []byte) (int, error) {
	if r.pos >= len(r.data) {
		return 0, errors.New("EOF-ish")
	}
	n := copy(p, r.data[r.pos:min(r.pos+7, len(r.data))])
	r.pos += n
	if r.pos >= r.after {
		r.cancel()
	}
	return n, nil
}

func TestS3_MultipartAPIDirect(t *testing.T) {
	c, fake := newTestS3(t, "bkt", "", true)
	ctx := context.Background()
	id, err := c.CreateMultipart(ctx, "m/1", PutOptions{ContentType: "application/zip"})
	if err != nil || id == "" {
		t.Fatalf("create: %q %v", id, err)
	}
	e1, err := c.UploadPart(ctx, "m/1", id, 1, bytes.NewReader([]byte("AAAA")), 4)
	if err != nil {
		t.Fatal(err)
	}
	e2, err := c.UploadPart(ctx, "m/1", id, 2, strings.NewReader("BB"), 2) // 非 Seeker 的 reader 也可
	if err != nil {
		t.Fatal(err)
	}
	if err := c.CompleteMultipart(ctx, "m/1", id, []Part{{1, e1}, {2, e2}}); err != nil {
		t.Fatal(err)
	}
	if o := fake.objects["m/1"]; string(o.data) != "AAAABB" || o.contentType != "application/zip" {
		t.Fatalf("object: %+v", o)
	}
	// 顺序错误 → InvalidPart，错误来自 XML
	id2, _ := c.CreateMultipart(ctx, "m/2", PutOptions{})
	a, _ := c.UploadPart(ctx, "m/2", id2, 1, strings.NewReader("a"), 1)
	if err := c.CompleteMultipart(ctx, "m/2", id2, []Part{{2, a}}); err == nil || !strings.Contains(err.Error(), "InvalidPart") {
		t.Fatalf("want InvalidPart, got %v", err)
	}
	if err := c.AbortMultipart(ctx, "m/2", id2); err != nil {
		t.Fatal(err)
	}
	if err := c.AbortMultipart(ctx, "m/2", id2); err != nil { // NoSuchUpload 视为已中止
		t.Fatal(err)
	}
	if _, err := c.UploadPart(ctx, "m/1", id, 0, strings.NewReader("x"), 1); err == nil {
		t.Fatal("part number 0 must be rejected")
	}
}

func TestS3_AbortStale(t *testing.T) {
	c, fake := newTestS3(t, "bkt", "app", true)
	ctx := context.Background()
	mine, _ := c.CreateMultipart(ctx, "1/20261017/5.mp4", PutOptions{})
	other, _ := c.CreateMultipart(ctx, "backup/dump.tar", PutOptions{})
	n, err := c.AbortStale(ctx, 24*time.Hour, func(key string) bool { return strings.HasPrefix(key, "1/") })
	if err != nil || n != 1 {
		t.Fatalf("aborted %d, err %v", n, err)
	}
	if _, ok := fake.uploads[mine]; ok {
		t.Fatal("matching stale upload should be aborted")
	}
	if _, ok := fake.uploads[other]; !ok {
		t.Fatal("non-matching upload must be left alone")
	}
	if n, _ := c.AbortStale(ctx, 100*time.Hour, nil); n != 0 {
		t.Fatalf("young uploads must not be aborted, got %d", n)
	}
}
