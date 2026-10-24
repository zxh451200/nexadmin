package file

import (
	"context"
	"io"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"nexadmin/core/storage"
)

// memStore 是不带 Opener 的内存存储，用来走“后端代理”分支（与 S3 驱动行为一致）。
type memStore struct {
	data     map[string][]byte
	presign  string
	lastGets []*storage.Range
}

func (m *memStore) Put(_ context.Context, k string, r io.Reader, _ int64, _ storage.PutOptions) error {
	b, err := io.ReadAll(r)
	m.data[k] = b
	return err
}
func (m *memStore) PutStream(ctx context.Context, k string, r io.Reader, o storage.PutOptions) error {
	return m.Put(ctx, k, r, -1, o)
}
func (m *memStore) Get(_ context.Context, k string, rng *storage.Range) (*storage.Object, error) {
	b, ok := m.data[k]
	if !ok {
		return nil, storage.ErrNotFound
	}
	m.lastGets = append(m.lastGets, rng)
	info := storage.ObjectInfo{Key: k, Size: int64(len(b)), ETag: "e1"}
	if rng == nil {
		return &storage.Object{Body: io.NopCloser(strings.NewReader(string(b))), Info: info, Length: int64(len(b)), End: int64(len(b)) - 1}, nil
	}
	if rng.Start >= int64(len(b)) {
		return nil, storage.ErrRangeNotSatisfiable
	}
	end := int64(len(b)) - 1
	if rng.End >= 0 {
		end = min(rng.End, end)
	}
	body := io.NopCloser(strings.NewReader(string(b[rng.Start : end+1])))
	return &storage.Object{Body: body, Info: info, Length: end - rng.Start + 1, Partial: true, Start: rng.Start, End: end}, nil
}
func (m *memStore) Delete(_ context.Context, k string) error { delete(m.data, k); return nil }
func (m *memStore) Exists(_ context.Context, k string) (bool, error) {
	_, ok := m.data[k]
	return ok, nil
}
func (m *memStore) Stat(context.Context, string) (*storage.ObjectInfo, error) {
	return nil, storage.ErrNotFound
}
func (m *memStore) PresignGet(_ context.Context, k string, _ time.Duration, _ storage.PresignOptions) (string, error) {
	if m.presign == "" {
		return "", storage.ErrNotSupported
	}
	return m.presign + k, nil
}

func serveReq(h *Handler, f File, public bool, rangeHdr, query string) *httptest.ResponseRecorder {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest("GET", "/x?"+query, nil)
	if rangeHdr != "" {
		c.Request.Header.Set("Range", rangeHdr)
	}
	h.serve(c, f, nil, public)
	return w
}

func TestServe_S3ProxyRangeAndHeaders(t *testing.T) {
	m := &memStore{data: map[string][]byte{"1/20261017/9.mp4": []byte("0123456789")}}
	h := &Handler{svc: &Service{store: &store{blobs: m}}}
	f := File{Name: "录像 1.mp4", StoragePath: "1/20261017/9.mp4", MimeType: "video/mp4", Category: "video", Size: 10}

	w := serveReq(h, f, false, "", "")
	if w.Code != 200 || w.Body.String() != "0123456789" || w.Header().Get("Accept-Ranges") != "bytes" ||
		w.Header().Get("Content-Type") != "video/mp4" || w.Header().Get("X-Content-Type-Options") != "nosniff" ||
		w.Header().Get("Content-Disposition") != "" || w.Header().Get("ETag") != `"e1"` {
		t.Fatalf("full: %d %v %q", w.Code, w.Header(), w.Body.String())
	}
	w = serveReq(h, f, false, "bytes=2-4", "")
	if w.Code != 206 || w.Body.String() != "234" || w.Header().Get("Content-Range") != "bytes 2-4/10" || w.Header().Get("Content-Length") != "3" {
		t.Fatalf("range: %d %v %q", w.Code, w.Header(), w.Body.String())
	}
	if r := m.lastGets[len(m.lastGets)-1]; r == nil || r.Start != 2 || r.End != 4 {
		t.Fatalf("range not forwarded to storage: %+v", r)
	}
	if w = serveReq(h, f, false, "bytes=50-", ""); w.Code != 416 || w.Header().Get("Content-Range") != "bytes */10" {
		t.Fatalf("416: %d %v", w.Code, w.Header())
	}
	if w = serveReq(h, f, false, "bytes=0-1,3-4", ""); w.Code != 200 || w.Body.Len() != 10 {
		t.Fatalf("multi-range should be ignored: %d", w.Code)
	}
	w = serveReq(h, f, true, "", "download=1")
	if cd := w.Header().Get("Content-Disposition"); !strings.HasPrefix(cd, "attachment;") || !strings.Contains(cd, "filename*=utf-8''") {
		t.Fatalf("download disposition: %q", cd)
	}
	if w.Header().Get("Cache-Control") != "public, max-age=3600" {
		t.Fatalf("public cache: %v", w.Header())
	}
	f.StoragePath = "missing"
	if w = serveReq(h, f, false, "", ""); w.Code != 404 {
		t.Fatalf("missing: %d", w.Code)
	}
}

func TestServe_PresignRedirectOnlyForPrivate(t *testing.T) {
	m := &memStore{data: map[string][]byte{"k": []byte("abc")}, presign: "https://bkt.example/"}
	h := &Handler{svc: &Service{store: &store{blobs: m, presignRedirect: true}}}
	f := File{Name: "a.pdf", StoragePath: "k", MimeType: "application/pdf", Category: "doc", Size: 3}
	if w := serveReq(h, f, false, "", ""); w.Code != 302 || w.Header().Get("Location") != "https://bkt.example/k" {
		t.Fatalf("private should redirect: %d %v", w.Code, w.Header())
	}
	if w := serveReq(h, f, true, "", ""); w.Code != 200 {
		t.Fatalf("public must stay proxied (no bucket exposure): %d", w.Code)
	}
	h.svc.store.presignRedirect = false
	if w := serveReq(h, f, false, "", ""); w.Code != 200 {
		t.Fatalf("default is proxy: %d", w.Code)
	}
}

func TestServe_LocalKeepsServeContentRange(t *testing.T) {
	st, err := newStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	// 历史记录用旧的 <年月> 目录，必须仍可读取
	dir := filepath.Join(st.root, "files", "1", "202610")
	if err := os.MkdirAll(dir, 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "7.png"), []byte("0123456789"), 0o640); err != nil {
		t.Fatal(err)
	}
	h := &Handler{svc: &Service{store: st}}
	f := File{Name: "a.png", StoragePath: "1/202610/7.png", MimeType: "image/png", Category: "image", Size: 10}
	w := serveReq(h, f, false, "bytes=3-5", "")
	if w.Code != 206 || w.Body.String() != "345" || w.Header().Get("Content-Range") != "bytes 3-5/10" || w.Header().Get("Last-Modified") == "" {
		t.Fatalf("local range: %d %v %q", w.Code, w.Header(), w.Body.String())
	}
	f.StoragePath = "1/202610/none.png"
	if w = serveReq(h, f, false, "", ""); w.Code != 404 {
		t.Fatalf("missing local: %d", w.Code)
	}
}
