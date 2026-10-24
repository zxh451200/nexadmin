package storage

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

func readAll(t *testing.T, o *Object) string {
	t.Helper()
	defer o.Body.Close()
	b, err := io.ReadAll(o.Body)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestS3_PutGetRangeStatDelete(t *testing.T) {
	c, fake := newTestS3(t, "bkt", "", true)
	ctx := context.Background()
	data := "hello, s3 world"
	opt := PutOptions{ContentType: "text/plain", ContentDisposition: `attachment; filename=a.txt`}
	if err := c.Put(ctx, "t1/20261017/123.txt", strings.NewReader(data), int64(len(data)), opt); err != nil {
		t.Fatal(err)
	}
	if o := fake.objects["t1/20261017/123.txt"]; o == nil || o.contentType != "text/plain" || o.disposition != opt.ContentDisposition {
		t.Fatalf("stored object/metadata wrong: %+v", o)
	}
	obj, err := c.Get(ctx, "t1/20261017/123.txt", nil)
	if err != nil || readAll(t, obj) != data || obj.Partial || obj.Info.Size != int64(len(data)) {
		t.Fatalf("full get: %v %+v", err, obj)
	}
	obj, err = c.Get(ctx, "t1/20261017/123.txt", &Range{Start: 7, End: 8})
	if err != nil {
		t.Fatal(err)
	}
	if !obj.Partial || obj.Start != 7 || obj.End != 8 || obj.Length != 2 || obj.Info.Size != int64(len(data)) || readAll(t, obj) != "s3" {
		t.Fatalf("range get: %+v", obj)
	}
	if _, err = c.Get(ctx, "t1/20261017/123.txt", &Range{Start: 100, End: 200}); !errors.Is(err, ErrRangeNotSatisfiable) {
		t.Fatalf("want 416, got %v", err)
	}
	st, err := c.Stat(ctx, "t1/20261017/123.txt")
	if err != nil || st.Size != int64(len(data)) || st.ContentType != "text/plain" || st.ETag != "abc" || st.LastModified.IsZero() {
		t.Fatalf("stat: %v %+v", err, st)
	}
	if ok, err := c.Exists(ctx, "t1/20261017/123.txt"); err != nil || !ok {
		t.Fatalf("exists: %v %v", ok, err)
	}
	if err := c.Delete(ctx, "t1/20261017/123.txt"); err != nil {
		t.Fatal(err)
	}
	if err := c.Delete(ctx, "t1/20261017/123.txt"); err != nil { // 删除不存在的对象不报错
		t.Fatal(err)
	}
	if _, err := c.Get(ctx, "t1/20261017/123.txt", nil); !errors.Is(err, ErrNotFound) {
		t.Fatalf("want ErrNotFound, got %v", err)
	}
	if _, err := c.Stat(ctx, "t1/20261017/123.txt"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("stat want ErrNotFound, got %v", err)
	}
	if ok, _ := c.Exists(ctx, "t1/20261017/123.txt"); ok {
		t.Fatal("exists after delete")
	}
}

func TestS3_PutStreamSmallAndEmptyBodyUnknownSize(t *testing.T) {
	c, fake := newTestS3(t, "bkt", "", true)
	// 不可 Seek 的 reader、未知大小：缓冲后单次 PUT
	if err := c.PutStream(context.Background(), "a/b.bin", io.MultiReader(strings.NewReader("abc")), PutOptions{}); err != nil {
		t.Fatal(err)
	}
	if string(fake.objects["a/b.bin"].data) != "abc" {
		t.Fatal("stream body mismatch")
	}
	if len(fake.uploads) != 0 {
		t.Fatal("small stream should not use multipart")
	}
}

func TestS3_URLConstruction(t *testing.T) {
	ctx := context.Background()
	cases := []struct {
		name       string
		bucket     string
		prefix     string
		pathStyle  bool
		wantHost   string
		wantTarget string
	}{
		{"path-style", "bkt", "", true, "s3.test:9000", "/bkt/t/1.txt"},
		{"path-style+prefix", "bkt", "/app/", true, "s3.test:9000", "/bkt/app/t/1.txt"},
		{"virtual-hosted", "bkt", "", false, "bkt.s3.test:9000", "/t/1.txt"},
		{"virtual-hosted+prefix", "bkt", "app", false, "bkt.s3.test:9000", "/app/t/1.txt"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c, fake := newTestS3(t, tc.bucket, tc.prefix, tc.pathStyle)
			if err := c.Put(ctx, "t/1.txt", strings.NewReader("x"), 1, PutOptions{}); err != nil {
				t.Fatal(err)
			}
			want := "PUT " + tc.wantHost + " " + tc.wantTarget
			if len(fake.reqs) != 1 || fake.reqs[0] != want {
				t.Fatalf("request line: got %v want %q", fake.reqs, want)
			}
			// 前缀对调用方透明
			if o, err := c.Get(ctx, "t/1.txt", nil); err != nil || readAll(t, o) != "x" {
				t.Fatalf("get via prefix: %v", err)
			}
		})
	}
}

func TestS3_IPEndpointFallsBackToPathStyle(t *testing.T) {
	c, err := NewS3(S3Config{Endpoint: "http://127.0.0.1:9000", Bucket: "b", AccessKey: "a", SecretKey: "s"})
	if err != nil || !c.pathStyle {
		t.Fatalf("IP endpoint should force path-style: %v %v", c, err)
	}
	c, _ = NewS3(S3Config{Endpoint: "oss-cn-hangzhou.aliyuncs.com", Bucket: "b", AccessKey: "a", SecretKey: "s"})
	if c.scheme != "https" || c.pathStyle || c.buildURL("k", nil).String() != "https://b.oss-cn-hangzhou.aliyuncs.com/k" {
		t.Fatalf("default scheme/virtual-hosted wrong: %s", c.buildURL("k", nil))
	}
	if _, err := NewS3(S3Config{Endpoint: "http://h/sub", Bucket: "b", AccessKey: "a", SecretKey: "s"}); err == nil {
		t.Fatal("endpoint with path should be rejected")
	}
}

func TestS3_KeyEncodingAndValidation(t *testing.T) {
	c, fake := newTestS3(t, "bkt", "", true)
	ctx := context.Background()
	if err := c.Put(ctx, "t/a b+c.txt", strings.NewReader("x"), 1, PutOptions{}); err != nil {
		t.Fatal(err)
	}
	if _, ok := fake.objects["t/a b+c.txt"]; !ok {
		t.Fatalf("keys with special chars must round-trip through SigV4: %v", fake.reqs)
	}
	for _, bad := range []string{"", "/x", "a/../b", "a//b", `a\b`} {
		if err := c.Put(ctx, bad, strings.NewReader("x"), 1, PutOptions{}); !errors.Is(err, ErrInvalidKey) {
			t.Errorf("key %q should be invalid, got %v", bad, err)
		}
	}
}

func TestS3_XMLErrorParsing(t *testing.T) {
	resp := &http.Response{StatusCode: 403, Header: http.Header{"X-Amz-Request-Id": {"HDR"}}, Body: io.NopCloser(strings.NewReader(
		`<?xml version="1.0"?><Error><Code>AccessDenied</Code><Message>Access Denied</Message><Resource>/b/k</Resource><RequestId>R1</RequestId></Error>`))}
	e := parseS3Error(resp)
	if e.Status != 403 || e.Code != "AccessDenied" || e.Message != "Access Denied" || e.Resource != "/b/k" || e.RequestID != "R1" {
		t.Fatalf("parsed: %+v", e)
	}
	if errors.Is(e, ErrNotFound) {
		t.Fatal("403 must not match ErrNotFound")
	}
	resp = &http.Response{StatusCode: 502, Header: http.Header{"X-Amz-Request-Id": {"HDR"}}, Body: io.NopCloser(strings.NewReader("<html>bad gateway</html>"))}
	if e := parseS3Error(resp); e.Status != 502 || e.Code != "" || e.RequestID != "HDR" || !strings.Contains(e.Message, "bad gateway") {
		t.Fatalf("non-xml: %+v", e)
	}
	nf := &S3Error{Status: 404, Code: "NoSuchBucket"}
	if errors.Is(nf, ErrNotFound) {
		t.Fatal("NoSuchBucket is a config error, not a missing object")
	}
	// 经由真实请求拿到的错误
	c, fake := newTestS3(t, "bkt", "", true)
	fake.signer.SecretKey = "other" // 服务端用别的密钥 → SignatureDoesNotMatch
	err := c.Put(context.Background(), "k", strings.NewReader("x"), 1, PutOptions{})
	var se *S3Error
	if !errors.As(err, &se) || se.Code != "SignatureDoesNotMatch" || se.Status != 403 || se.RequestID != "REQ123" {
		t.Fatalf("want SignatureDoesNotMatch, got %v", err)
	}
}

func TestS3_RetriesOn5xx(t *testing.T) {
	c, fake := newTestS3(t, "bkt", "", true)
	calls := 0
	orig := c.client.Transport
	c.client.Transport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
		calls++
		if calls == 1 {
			return &http.Response{StatusCode: 503, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(`<Error><Code>SlowDown</Code></Error>`))}, nil
		}
		return orig.RoundTrip(r)
	})
	if err := c.Put(context.Background(), "k", bytes.NewReader([]byte("retry")), 5, PutOptions{}); err != nil {
		t.Fatal(err)
	}
	if calls != 2 || string(fake.objects["k"].data) != "retry" {
		t.Fatalf("calls=%d body=%q", calls, fake.objects["k"].data)
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestS3_PresignGet(t *testing.T) {
	c, _ := newTestS3(t, "bkt", "app", false)
	c.now = func() time.Time { return awsTime }
	u, err := c.PresignGet(context.Background(), "t/1.png", time.Hour, PresignOptions{ContentType: "image/png", ContentDisposition: `inline; filename="1.png"`})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"http://bkt.s3.test:9000/app/t/1.png?", "X-Amz-Algorithm=AWS4-HMAC-SHA256", "X-Amz-Expires=3600", "X-Amz-SignedHeaders=host",
		"X-Amz-Credential=AK%2F20130524%2Fus-east-1%2Fs3%2Faws4_request", "response-content-type=image%2Fpng", "response-content-disposition=inline%3B%20filename%3D%221.png%22",
	} {
		if !strings.Contains(u, want) {
			t.Errorf("presigned URL missing %q: %s", want, u)
		}
	}
	if !strings.Contains(u, "&X-Amz-Signature=") || strings.Count(u, "X-Amz-Signature=") != 1 {
		t.Errorf("signature param: %s", u)
	}
	if _, err := c.PresignGet(context.Background(), "k", 8*24*time.Hour, PresignOptions{}); err == nil {
		t.Error("ttl > 7d must be rejected")
	}
}
