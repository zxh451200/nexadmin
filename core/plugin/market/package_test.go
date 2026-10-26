package market

import (
	"archive/zip"
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"nexadmin/core/plugin"
)

const pkgManifest = "id: cron\nname: 定时任务\nversion: 1.0.0\nedition: pro\nauthor: NexAdmin\n"

// repoZip 构造一个 GitHub zipball 形状的多插件仓库包：<repo>-<ref>/<id>/...
func repoZip(t *testing.T, files map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	names := make([]string, 0, len(files))
	for n := range files {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		w, err := zw.Create(n)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write([]byte(files[n])); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// withGitHub 把 GitHub 下载地址临时指向测试服务器。
func withGitHub(t *testing.T, handler http.HandlerFunc) {
	t.Helper()
	old := GitHubBaseURL
	srv := httptest.NewServer(handler)
	t.Cleanup(func() {
		GitHubBaseURL = old
		srv.Close()
	})
	GitHubBaseURL = srv.URL
}

func TestPackageFromRepo(t *testing.T) {
	var gotPath string
	body := repoZip(t, map[string]string{
		"nexadmin-plugins-v1/cron/plugin.yaml":                           pkgManifest,
		"nexadmin-plugins-v1/cron/backend/plugin.go":                     "package cron\n",
		"nexadmin-plugins-v1/cron/backend/migrations/mysql/001_init.sql": "SELECT 1;\n",
		"nexadmin-plugins-v1/cron/web/route.tsx":                         "export {}\n",
		"nexadmin-plugins-v1/cron/licenses/cron.lic":                     "license",
		"nexadmin-plugins-v1/cron/node_modules/x/index.js":               "noise",
		"nexadmin-plugins-v1/other/plugin.yaml":                          "id: other\nname: Other\nversion: 1.0.0\nedition: free\n",
		"nexadmin-plugins-v1/other/backend/plugin.go":                    "package other\n",
	})
	withGitHub(t, func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.Header().Set("Content-Type", "application/zip")
		_, _ = w.Write(body)
	})

	dst := filepath.Join(t.TempDir(), "cron-1.0.0.zip")
	e := Entry{ID: "cron", Edition: plugin.Pro, Version: "1.0.0", Repo: "acme/plugins", Ref: "v1", Subdir: "cron"}
	if err := Package(context.Background(), e, dst); err != nil {
		t.Fatalf("Package: %v", err)
	}
	if gotPath != "/repos/acme/plugins/zipball/v1" {
		t.Errorf("download path = %q", gotPath)
	}

	zr, err := zip.OpenReader(dst)
	if err != nil {
		t.Fatal(err)
	}
	defer zr.Close() //nolint:errcheck
	var names []string
	for _, f := range zr.File {
		names = append(names, f.Name)
	}
	want := []string{
		"plugin.yaml",
		"backend/migrations/mysql/001_init.sql",
		"backend/plugin.go",
		"web/route.tsx",
	}
	sort.Strings(names)
	sort.Strings(want)
	if strings.Join(names, ",") != strings.Join(want, ",") {
		t.Fatalf("zip entries = %v, want %v", names, want)
	}
	for _, f := range zr.File {
		if f.Name != "plugin.yaml" {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			t.Fatal(err)
		}
		b, _ := io.ReadAll(rc)
		rc.Close() //nolint:errcheck
		if !strings.Contains(string(b), "id: cron") {
			t.Errorf("manifest not at zip root: %q", b)
		}
	}
}

// 同一份源码重复打包必须字节一致：校验和可复现，跨实例/重复构建才能对比。
func TestPackageReproducible(t *testing.T) {
	body := repoZip(t, map[string]string{
		"repo/cron/plugin.yaml":       pkgManifest,
		"repo/cron/backend/plugin.go": "package cron\n",
	})
	withGitHub(t, func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write(body) })

	e := Entry{ID: "cron", Edition: plugin.Pro, Version: "1.0.0", Repo: "acme/plugins", Subdir: "cron"}
	dir := t.TempDir()
	a, b := filepath.Join(dir, "a.zip"), filepath.Join(dir, "b.zip")
	if err := Package(context.Background(), e, a); err != nil {
		t.Fatal(err)
	}
	if err := Package(context.Background(), e, b); err != nil {
		t.Fatal(err)
	}
	ba, err := os.ReadFile(a)
	if err != nil {
		t.Fatal(err)
	}
	bb, err := os.ReadFile(b)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(ba, bb) {
		t.Errorf("packages differ: %d vs %d bytes", len(ba), len(bb))
	}
}

func TestPackageRejectsMismatch(t *testing.T) {
	body := repoZip(t, map[string]string{
		"repo/cron/plugin.yaml":       pkgManifest,
		"repo/cron/backend/plugin.go": "package cron\n",
	})
	withGitHub(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(body)
	})
	dst := filepath.Join(t.TempDir(), "x.zip")
	e := Entry{ID: "audit", Edition: plugin.Pro, Version: "1.0.0", Repo: "acme/plugins", Subdir: "cron"}
	if err := Package(context.Background(), e, dst); err == nil || !strings.Contains(err.Error(), "不一致") {
		t.Fatalf("id mismatch not rejected: %v", err)
	}

	e.ID = "cron"
	e.Edition = plugin.Free
	if err := Package(context.Background(), e, dst); err == nil || !strings.Contains(err.Error(), "edition") {
		t.Fatalf("edition mismatch not rejected: %v", err)
	}

	e.Edition = plugin.Pro
	e.Version = "9.9.9"
	if err := Package(context.Background(), e, dst); err == nil || !strings.Contains(err.Error(), "版本") {
		t.Fatalf("version mismatch not rejected: %v", err)
	}
}

func TestPackageRejectsBadSubdir(t *testing.T) {
	old := GitHubBaseURL
	GitHubBaseURL = "http://127.0.0.1:1" // 不应被访问
	defer func() { GitHubBaseURL = old }()

	for _, sub := range []string{"../etc", "/abs", "a/../../b"} {
		e := Entry{ID: "cron", Edition: plugin.Pro, Version: "1.0.0", Repo: "acme/plugins", Subdir: sub}
		if err := Package(context.Background(), e, filepath.Join(t.TempDir(), "x.zip")); err == nil {
			t.Errorf("subdir %q accepted", sub)
		}
	}
}

func TestPackageRequiresBackendOrWeb(t *testing.T) {
	body := repoZip(t, map[string]string{
		"repo/cron/plugin.yaml": pkgManifest,
		"repo/cron/README.md":   "docs only",
	})
	withGitHub(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(body)
	})
	e := Entry{ID: "cron", Edition: plugin.Pro, Version: "1.0.0", Repo: "acme/plugins", Subdir: "cron"}
	err := Package(context.Background(), e, filepath.Join(t.TempDir(), "x.zip"))
	if err == nil || !strings.Contains(err.Error(), "backend") {
		t.Fatalf("empty plugin accepted: %v", err)
	}
}

// Package 失败时不能留下半包。
func TestPackageNoLeftoverOnError(t *testing.T) {
	body := repoZip(t, map[string]string{
		"repo/cron/plugin.yaml": "id: nope\nname: N\nversion: 1.0.0\nedition: pro\n",
	})
	withGitHub(t, func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write(body) })
	dst := filepath.Join(t.TempDir(), "x.zip")
	e := Entry{ID: "cron", Edition: plugin.Pro, Version: "1.0.0", Repo: "acme/plugins", Subdir: "cron"}
	if err := Package(context.Background(), e, dst); err == nil {
		t.Fatal("want error")
	}
	if _, err := os.Stat(dst); !os.IsNotExist(err) {
		t.Fatalf("partial package left behind: %v", err)
	}
}
