package main

import (
	"archive/zip"
	"bytes"
	"strings"
	"testing"
	"time"
)

// 批量签发核心：多插件各自独立序列号；重复/空/超限报错；任一插件失败整体失败。
func TestIssueBundleCore(t *testing.T) {
	s := newTestServer2(t)
	in := bundleReq{Plugins: []string{" cron", "audit", "cron ", ""}, Customer: "测试客户", Expires: "2027-12-31"}
	// 重复（cron 去重后再次出现）要报错。
	if _, _, err := s.issueBundleCore(in, time.Now()); err == nil || !strings.Contains(err.Error(), "重复") {
		t.Fatalf("duplicate plugin must fail: %v", err)
	}
	raws, recs, err := s.issueBundleCore(bundleReq{
		Plugins: []string{"cron", "audit", "codegen"}, Customer: "测试客户", Expires: "2027-12-31", Domains: "a.com", Note: "n",
	}, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if len(raws) != 3 || len(recs) != 3 {
		t.Fatalf("want 3, got %d/%d", len(raws), len(recs))
	}
	serials := map[string]bool{}
	for _, rec := range recs {
		if rec.Serial == "" || serials[rec.Serial] {
			t.Fatalf("serial must be unique and non-empty: %+v", rec)
		}
		serials[rec.Serial] = true
		if rec.Customer != "测试客户" || rec.Domains[0] != "a.com" {
			t.Errorf("bad record: %+v", rec)
		}
	}
	// 空 id 列表报错。
	if _, _, err := s.issueBundleCore(bundleReq{Customer: "x", Expires: "2027-12-31"}, time.Now()); err == nil {
		t.Fatal("empty plugins must fail")
	}
	// 非法插件 id 失败并点名。
	if _, _, err := s.issueBundleCore(bundleReq{Plugins: []string{"../evil"}, Customer: "x", Expires: "2027-12-31"}, time.Now()); err == nil || !strings.Contains(err.Error(), "../evil") {
		t.Fatalf("invalid id must fail naming it: %v", err)
	}
}

// bundleZip：包含每个插件的 .lic 与 LICENSES.txt 清单，是合法 zip。
func TestBundleZip(t *testing.T) {
	s := newTestServer2(t)
	raws, recs, err := s.issueBundleCore(bundleReq{
		Plugins: []string{"cron", "audit"}, Customer: "测试客户", Expires: "2027-12-31",
	}, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	zb, err := bundleZip("测试客户", recs, raws)
	if err != nil {
		t.Fatal(err)
	}
	zr, err := zip.NewReader(bytes.NewReader(zb), int64(len(zb)))
	if err != nil {
		t.Fatalf("not a valid zip: %v", err)
	}
	names := map[string]bool{}
	for _, f := range zr.File {
		names[f.Name] = true
	}
	for _, want := range []string{"cron.lic", "audit.lic", "LICENSES.txt"} {
		if !names[want] {
			t.Errorf("zip missing %s (has %v)", want, names)
		}
	}
	for _, f := range zr.File {
		if strings.HasSuffix(f.Name, ".lic") && f.UncompressedSize64 == 0 {
			t.Errorf("%s is empty", f.Name)
		}
	}
}

func TestSanitizeFilename(t *testing.T) {
	if got := sanitizeFilename("某某/公司:2026"); got != "------2026" {
		t.Errorf("sanitizeFilename = %q", got)
	}
	if got := sanitizeFilename("  "); got != "bundle" {
		t.Errorf("empty should fall back to bundle, got %q", got)
	}
}

// newTestServer2 生成带随机密钥与临时台账的 server（复用 main_test 的装配方式）。
func newTestServer2(t *testing.T) *server {
	t.Helper()
	s, _, _ := newTestServer(t, time.Now())
	return s
}
