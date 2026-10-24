package file

import (
	"regexp"
	"strings"
	"testing"
	"time"

	"nexadmin/core/storage"
)

func TestFinalRel_NoUserFilename(t *testing.T) {
	now := time.Date(2026, 10, 17, 1, 0, 0, 0, time.UTC)
	got := finalRel("1001", "7123456789012345678", "../../我的 报告.PDF", now)
	if got != "1001/20261017/7123456789012345678.pdf" {
		t.Fatalf("got %s", got)
	}
	k := finalRel("1001", "7123456789012345678", "evil.p/../x", now)
	if !regexp.MustCompile(`^1001/\d{8}/7123456789012345678(\.[a-z0-9]+)?$`).MatchString(k) {
		t.Fatalf("unsafe: %s", k)
	}
	if !ourKey.MatchString(got) || ourKey.MatchString("backup/dump.tar") {
		t.Fatal("ourKey must match module keys only")
	}
}

func TestPutOptions(t *testing.T) {
	o := putOptions("报告 1.pdf", "application/pdf", "doc", "")
	if o.ContentType != "application/pdf" || !strings.HasPrefix(o.ContentDisposition, "inline;") {
		t.Fatalf("pdf inline: %+v", o)
	}
	for _, r := range o.ContentDisposition {
		if r > 0x7e {
			t.Fatalf("Content-Disposition must be ASCII: %q", o.ContentDisposition)
		}
	}
	if o := putOptions("a.zip", "application/zip", "other", ""); !strings.HasPrefix(o.ContentDisposition, "attachment;") {
		t.Fatalf("zip attachment: %+v", o)
	}
}

func TestParseByteRange(t *testing.T) {
	cases := map[string]*storage.Range{
		"bytes=0-9": {Start: 0, End: 9}, "bytes=5-": {Start: 5, End: -1}, "bytes=-3": {Start: -3, End: -1},
		"bytes=9-1": nil, "bytes=a-b": nil, "items=0-1": nil, "bytes=0-1,4-5": nil, "bytes=-0": nil,
	}
	for in, want := range cases {
		got := parseByteRange(in)
		if (got == nil) != (want == nil) || (got != nil && *got != *want) {
			t.Errorf("%q: got %+v want %+v", in, got, want)
		}
	}
}
