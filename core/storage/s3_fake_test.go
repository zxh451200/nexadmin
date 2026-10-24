package storage

import (
	"context"
	"crypto/md5"
	"crypto/sha256"
	"encoding/hex"
	"encoding/xml"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

type fakeObject struct {
	data        []byte
	contentType string
	disposition string
	modified    time.Time
}

type fakeUpload struct {
	key       string
	parts     map[int][]byte
	initiated time.Time
}

// fakeS3 是内存里的 S3 服务端：校验 SigV4、支持单次 PUT、Range GET、HEAD、DELETE、分片上传与 ListMultipartUploads。
type fakeS3 struct {
	t       *testing.T
	signer  Signer
	bucket  string
	mu      sync.Mutex
	objects map[string]*fakeObject
	uploads map[string]*fakeUpload
	reqs    []string // "METHOD host path?query"，供断言 URL 构造
	failPut int      // >0：第 N 片返回 403 AccessDenied
	nextID  int
}

func newFakeS3(t *testing.T, bucket string) *fakeS3 {
	return &fakeS3{
		t: t, bucket: bucket, objects: map[string]*fakeObject{}, uploads: map[string]*fakeUpload{},
		signer: Signer{AccessKey: "AK", SecretKey: "SK", Region: "us-east-1", Service: "s3"},
	}
}

func writeXMLError(w http.ResponseWriter, status int, code, msg string) {
	w.Header().Set("Content-Type", "application/xml")
	w.Header().Set("X-Amz-Request-Id", "REQ123")
	w.WriteHeader(status)
	fmt.Fprintf(w, `<?xml version="1.0" encoding="UTF-8"?><Error><Code>%s</Code><Message>%s</Message><Resource>/x</Resource><RequestId>REQ123</RequestId></Error>`, code, msg)
}

// verifySig 用请求里 SignedHeaders 指定的头重算签名，与 Authorization 比对。
func (f *fakeS3) verifySig(r *http.Request, body []byte) bool {
	auth := r.Header.Get("Authorization")
	i := strings.Index(auth, "SignedHeaders=")
	j := strings.Index(auth, ",Signature=")
	if i < 0 || j < 0 {
		return false
	}
	signed := strings.Split(auth[i+len("SignedHeaders="):j], ";")
	sum := sha256.Sum256(body)
	if got := r.Header.Get("X-Amz-Content-Sha256"); got != hex.EncodeToString(sum[:]) && got != emptyPayload {
		return false
	}
	t, err := time.Parse(amzTimeFormat, r.Header.Get("X-Amz-Date"))
	if err != nil {
		return false
	}
	clone := &http.Request{Method: r.Method, URL: r.URL, Host: r.Host, Header: http.Header{}}
	for _, h := range signed {
		if h != "host" {
			clone.Header[http.CanonicalHeaderKey(h)] = r.Header.Values(h)
		}
	}
	_, _, sig, sh := f.signer.details(clone, canonicalQuery(r.URL.Query()), r.Header.Get("X-Amz-Content-Sha256"), t)
	return sh == strings.Join(signed, ";") && strings.HasSuffix(auth, "Signature="+sig)
}

func (f *fakeS3) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(r.Body)
	f.mu.Lock()
	defer f.mu.Unlock()
	f.reqs = append(f.reqs, r.Method+" "+r.Host+" "+r.URL.RequestURI())
	if !f.verifySig(r, body) {
		writeXMLError(w, 403, "SignatureDoesNotMatch", "bad signature")
		return
	}
	path := strings.TrimPrefix(r.URL.EscapedPath(), "/")
	if strings.HasPrefix(path, f.bucket+"/") || path == f.bucket {
		path = strings.TrimPrefix(strings.TrimPrefix(path, f.bucket), "/")
	}
	key, _ := url.PathUnescape(path)
	q := r.URL.Query()
	switch {
	case r.Method == "POST" && q.Has("uploads"):
		f.nextID++
		id := "UP" + strconv.Itoa(f.nextID)
		f.uploads[id] = &fakeUpload{key: key, parts: map[int][]byte{}, initiated: time.Now().Add(-48 * time.Hour)}
		f.objects["~meta/"+id] = &fakeObject{contentType: r.Header.Get("Content-Type"), disposition: r.Header.Get("Content-Disposition")}
		fmt.Fprintf(w, `<InitiateMultipartUploadResult><Bucket>%s</Bucket><Key>%s</Key><UploadId>%s</UploadId></InitiateMultipartUploadResult>`, f.bucket, key, id)
	case r.Method == "PUT" && q.Has("partNumber"):
		n, _ := strconv.Atoi(q.Get("partNumber"))
		up := f.uploads[q.Get("uploadId")]
		if up == nil {
			writeXMLError(w, 404, "NoSuchUpload", "no upload")
			return
		}
		if f.failPut == n {
			writeXMLError(w, 403, "AccessDenied", "denied")
			return
		}
		up.parts[n] = body
		sum := md5.Sum(body)
		w.Header().Set("ETag", `"`+hex.EncodeToString(sum[:])+`"`)
	case r.Method == "POST" && q.Has("uploadId"):
		f.complete(w, key, q.Get("uploadId"), body)
	case r.Method == "DELETE" && q.Has("uploadId"):
		if _, ok := f.uploads[q.Get("uploadId")]; !ok {
			writeXMLError(w, 404, "NoSuchUpload", "no upload")
			return
		}
		delete(f.uploads, q.Get("uploadId"))
		w.WriteHeader(204)
	case r.Method == "GET" && q.Has("uploads"):
		f.listUploads(w, q.Get("prefix"))
	case r.Method == "PUT":
		f.objects[key] = &fakeObject{data: body, contentType: r.Header.Get("Content-Type"), disposition: r.Header.Get("Content-Disposition"), modified: time.Now()}
		w.Header().Set("ETag", `"abc"`)
	case r.Method == "DELETE":
		delete(f.objects, key)
		w.WriteHeader(204)
	case r.Method == "GET" || r.Method == "HEAD":
		f.get(w, r, key)
	default:
		writeXMLError(w, 405, "MethodNotAllowed", "nope")
	}
}

func (f *fakeS3) complete(w http.ResponseWriter, key, id string, body []byte) {
	up := f.uploads[id]
	var req completeReq
	if up == nil || xml.Unmarshal(body, &req) != nil {
		writeXMLError(w, 404, "NoSuchUpload", "no upload")
		return
	}
	var all []byte
	for i, p := range req.Parts {
		data, ok := up.parts[p.PartNumber]
		sum := md5.Sum(data)
		if !ok || p.PartNumber != i+1 || strings.Trim(p.ETag, `"`) != hex.EncodeToString(sum[:]) {
			writeXMLError(w, 400, "InvalidPart", "bad part")
			return
		}
		all = append(all, data...)
	}
	meta := f.objects["~meta/"+id]
	f.objects[key] = &fakeObject{data: all, contentType: meta.contentType, disposition: meta.disposition, modified: time.Now()}
	delete(f.uploads, id)
	fmt.Fprintf(w, `<CompleteMultipartUploadResult><Key>%s</Key></CompleteMultipartUploadResult>`, key)
}

func (f *fakeS3) listUploads(w http.ResponseWriter, prefix string) {
	ids := make([]string, 0, len(f.uploads))
	for id := range f.uploads {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	var b strings.Builder
	b.WriteString(`<ListMultipartUploadsResult><IsTruncated>false</IsTruncated>`)
	for _, id := range ids {
		if up := f.uploads[id]; strings.HasPrefix(up.key, prefix) {
			fmt.Fprintf(&b, `<Upload><Key>%s</Key><UploadId>%s</UploadId><Initiated>%s</Initiated></Upload>`, up.key, id, up.initiated.UTC().Format(time.RFC3339))
		}
	}
	b.WriteString(`</ListMultipartUploadsResult>`)
	io.WriteString(w, b.String())
}

func (f *fakeS3) get(w http.ResponseWriter, r *http.Request, key string) {
	o, ok := f.objects[key]
	if !ok {
		if r.Method == "HEAD" {
			w.WriteHeader(404)
			return
		}
		writeXMLError(w, 404, "NoSuchKey", "The specified key does not exist.")
		return
	}
	w.Header().Set("Content-Type", o.contentType)
	w.Header().Set("Content-Disposition", o.disposition)
	w.Header().Set("ETag", `"abc"`)
	w.Header().Set("Last-Modified", o.modified.UTC().Format(http.TimeFormat))
	data := o.data
	if rh := r.Header.Get("Range"); rh != "" && r.Method == "GET" {
		var s, e int64
		if _, err := fmt.Sscanf(rh, "bytes=%d-%d", &s, &e); err != nil || s >= int64(len(data)) {
			writeXMLError(w, 416, "InvalidRange", "The requested range is not satisfiable")
			return
		}
		e = min(e, int64(len(data))-1)
		w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", s, e, len(data)))
		w.WriteHeader(206)
		w.Write(data[s : e+1])
		return
	}
	w.Header().Set("Content-Length", strconv.Itoa(len(data)))
	if r.Method == "GET" {
		w.Write(data)
	}
}

// newTestS3 启动 fake 服务并返回驱动；所有主机名都被拨到 fake 上，因此可以测虚拟主机风格。
func newTestS3(t *testing.T, bucket, prefix string, pathStyle bool) (*S3, *fakeS3) {
	t.Helper()
	fake := newFakeS3(t, bucket)
	srv := httptest.NewServer(fake)
	t.Cleanup(srv.Close)
	tr := &http.Transport{DialContext: func(ctx context.Context, network, _ string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, network, srv.Listener.Addr().String())
	}}
	c, err := NewS3(S3Config{
		Endpoint: "http://s3.test:9000", Bucket: bucket, AccessKey: "AK", SecretKey: "SK", Prefix: prefix,
		PathStyle: pathStyle, HTTPClient: &http.Client{Transport: tr},
	})
	if err != nil {
		t.Fatal(err)
	}
	c.backoff, c.partSize, c.threshold = 0, 10, 25 // 测试里用小分片，fake 不强制 5MiB 下限
	return c, fake
}
