package file

import "testing"

func TestCheckContentRejectsExecutableDeclaredTypes(t *testing.T) {
	head := []byte("plain text body")
	for _, declared := range []string{"text/html", "application/xhtml+xml", "image/svg+xml", "text/javascript", "APPLICATION/JAVASCRIPT"} {
		m, err := checkContent("file", declared, "note.txt", head)
		if err != nil {
			t.Fatalf("checkContent(%q) unexpected error: %v", declared, err)
		}
		if m == declared {
			t.Fatalf("checkContent(%q) kept executable declared type %q", declared, m)
		}
	}
	m, err := checkContent("file", "application/zip", "a.txt", head)
	if err != nil || m != "application/zip" {
		t.Fatalf("safe declared type: got %q, %v", m, err)
	}
}
