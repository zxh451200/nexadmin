package storage

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"
)

func TestLocal_Basics(t *testing.T) {
	l, err := NewLocal(filepath.Join(t.TempDir(), "files"))
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if err := l.Put(ctx, "1/202610/a.txt", strings.NewReader("0123456789"), 10, PutOptions{}); err != nil {
		t.Fatal(err)
	}
	if err := l.Put(ctx, "1/202610/short.txt", strings.NewReader("abc"), 10, PutOptions{}); err == nil {
		t.Fatal("size mismatch must fail")
	}
	if ok, _ := l.Exists(ctx, "1/202610/short.txt"); ok {
		t.Fatal("failed write must not leave an object")
	}
	if ents, _ := os.ReadDir(filepath.Join(l.root, "1", "202610")); len(ents) != 1 {
		t.Fatalf("temp files left behind: %v", ents)
	}
	for _, c := range []struct {
		rng        *Range
		want       string
		start, end int64
	}{
		{nil, "0123456789", 0, 9}, {&Range{2, 4}, "234", 2, 4}, {&Range{7, -1}, "789", 7, 9},
		{&Range{-3, -1}, "789", 7, 9}, {&Range{5, 99}, "56789", 5, 9},
	} {
		o, err := l.Get(ctx, "1/202610/a.txt", c.rng)
		if err != nil {
			t.Fatal(err)
		}
		b, _ := io.ReadAll(o.Body)
		o.Body.Close()
		if string(b) != c.want || o.Length != int64(len(c.want)) || o.Info.Size != 10 || (c.rng != nil && (!o.Partial || o.Start != c.start || o.End != c.end)) {
			t.Errorf("range %+v: got %q %+v", c.rng, b, o)
		}
	}
	if _, err := l.Get(ctx, "1/202610/a.txt", &Range{10, -1}); !errors.Is(err, ErrRangeNotSatisfiable) {
		t.Fatalf("got %v", err)
	}
	if _, err := l.Stat(ctx, "1/202610/none"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("got %v", err)
	}
	if _, err := l.PresignGet(ctx, "x/y", time.Minute, PresignOptions{}); !errors.Is(err, ErrNotSupported) {
		t.Fatalf("got %v", err)
	}
	if err := l.Delete(ctx, "1/202610/a.txt"); err != nil {
		t.Fatal(err)
	}
	if err := l.Delete(ctx, "1/202610/a.txt"); err != nil {
		t.Fatal("deleting a missing object is not an error")
	}
	for _, bad := range []string{"../etc/passwd", "a/../../b", "/abs", ""} {
		if _, err := l.Stat(ctx, bad); !errors.Is(err, ErrInvalidKey) {
			t.Errorf("%q should be rejected, got %v", bad, err)
		}
	}
}

func TestLocal_Adopt(t *testing.T) {
	l, _ := NewLocal(t.TempDir())
	src := filepath.Join(t.TempDir(), "x.part")
	_ = os.WriteFile(src, []byte("data"), 0o600)
	if err := l.Adopt(context.Background(), "1/20261017/7.bin", src); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(src); !os.IsNotExist(err) {
		t.Fatal("source should have been moved")
	}
	if st, err := l.Stat(context.Background(), "1/20261017/7.bin"); err != nil || st.Size != 4 {
		t.Fatalf("%v %+v", err, st)
	}
}

func TestObjectKey(t *testing.T) {
	now := time.Date(2026, 10, 17, 23, 0, 0, 0, time.FixedZone("x", 8*3600))
	got := ObjectKey("1001", "7123456789012345678", ".PNG", now)
	if got != "1001/20261017/7123456789012345678.png" {
		t.Fatalf("got %s", got)
	}
	for _, ext := range []string{".a/../b", ".x y", ".verylongextension", "png", "."} {
		if k := ObjectKey("1", "2", ext, now); !regexp.MustCompile(`^1/\d{8}/2$`).MatchString(k) {
			t.Errorf("unsafe ext %q leaked into key %q", ext, k)
		}
	}
}

func TestExpandEnvAndConfig(t *testing.T) {
	t.Setenv("NEXA_TEST_SK", "se$cret/+")
	if v, err := ExpandEnv("${NEXA_TEST_SK}"); err != nil || v != "se$cret/+" {
		t.Fatalf("%q %v", v, err)
	}
	if v, _ := ExpandEnv("plain$value"); v != "plain$value" {
		t.Fatalf("bare $ must stay: %q", v)
	}
	if _, err := ExpandEnv("${NEXA_TEST_NOT_SET_XYZ}"); err == nil {
		t.Fatal("unset var must error")
	}
	if (Config{Driver: "ftp"}).Validate() == nil || (Config{Driver: "s3"}).Validate() == nil || (Config{}).Validate() != nil {
		t.Fatal("validate")
	}
	st, err := New(Config{Driver: "s3", S3: S3Config{Endpoint: "http://minio:9000", Bucket: "b", AccessKey: "ak", SecretKey: "${NEXA_TEST_SK}"}}, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if s3, ok := st.(*S3); !ok || s3.signer.SecretKey != "se$cret/+" {
		t.Fatalf("env not expanded into driver: %#v", st)
	}
	if st, err := New(Config{}, t.TempDir()); err != nil {
		t.Fatal(err)
	} else if _, ok := st.(*Local); !ok {
		t.Fatalf("default driver should be local, got %T", st)
	}
}
