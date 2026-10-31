package ticket

import "testing"

func TestAttachmentRuleFor(t *testing.T) {
	cases := []struct {
		mime, name string
		ok         bool
		ext        string
	}{
		{"image/png", "x.png", true, ".png"},
		{"image/jpeg", "x.jpg", true, ".jpg"},
		{"application/pdf", "x.pdf", true, ".pdf"},
		{"application/zip", "a.docx", true, ".docx"},
		{"application/zip", "a.zip", true, ".zip"},
		{"application/x-msdownload", "a.exe", false, ""},
		{"text/html", "a.html", false, ""},
	}
	for _, c := range cases {
		rule, ok := attachmentRuleFor(c.mime, c.name)
		if ok != c.ok || (ok && rule.ext != c.ext) {
			t.Errorf("%s/%s: got ok=%v ext=%q", c.mime, c.name, ok, rule.ext)
		}
	}
}

func TestSafeFilename(t *testing.T) {
	cases := map[string]string{
		`..\..\etc/passwd`: "passwd",
		"/tmp/a\x00b.txt":  "ab.txt",
		"":                 "file",
		"..":               "file",
	}
	for in, want := range cases {
		if got := safeFilename(in); got != want {
			t.Errorf("safeFilename(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestRandomToken(t *testing.T) {
	a, b := randomToken(16), randomToken(16)
	if len(a) != 32 {
		t.Fatalf("token length = %d, want 32", len(a))
	}
	if a == b {
		t.Fatal("tokens must differ")
	}
}
