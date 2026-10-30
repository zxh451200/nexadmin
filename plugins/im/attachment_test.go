package im

import "testing"

func TestAttachmentRuleFor(t *testing.T) {
	cases := []struct {
		mime, name string
		ok         bool
		ext        string
		kind       string
	}{
		{"image/png", "a.png", true, ".png", KindImage},
		{"image/jpeg", "a.JPG", true, ".jpg", KindImage},
		{"application/pdf", "doc.pdf", true, ".pdf", KindFile},
		{"text/plain", "note.txt", true, ".txt", KindFile},
		{"application/zip", "sheet.xlsx", true, ".xlsx", KindFile},
		{"application/zip", "bundle.zip", true, ".zip", KindFile},
		{"text/html", "x.html", false, "", ""},
		{"application/x-msdownload", "x.exe", false, "", ""},
	}
	for _, c := range cases {
		rule, ok := attachmentRuleFor(c.mime, c.name)
		if ok != c.ok || rule.ext != c.ext || rule.kind != c.kind {
			t.Fatalf("%s/%s => %+v ok=%v", c.mime, c.name, rule, ok)
		}
	}
}

func TestSafeFilename(t *testing.T) {
	cases := map[string]string{
		"../../etc/passwd":  "passwd",
		`C:\tmp\报价单.pdf`: "报价单.pdf",
		"   ":              "file",
		"..":               "file",
	}
	for in, want := range cases {
		if got := safeFilename(in); got != want {
			t.Fatalf("safeFilename(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestAttachmentMeta(t *testing.T) {
	att := &Attachment{Token: "t1", Name: "a.png", Mime: "image/png", Size: 12}
	got := attachmentMeta(att)
	want := `{"attachment":{"mime":"image/png","name":"a.png","size":12,"token":"t1"}}`
	if got != want {
		t.Fatalf("meta = %s, want %s", got, want)
	}
}
